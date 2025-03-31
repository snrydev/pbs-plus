//go:build linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/auth/certificates"
	"github.com/pbs-plus/pbs-plus/internal/auth/server"
	"github.com/pbs-plus/pbs-plus/internal/auth/token"
	"github.com/pbs-plus/pbs-plus/internal/backend/backup"
	"github.com/pbs-plus/pbs-plus/internal/proxy"
	"github.com/pbs-plus/pbs-plus/internal/proxy/controllers/agents"
	"github.com/pbs-plus/pbs-plus/internal/proxy/controllers/arpc"
	"github.com/pbs-plus/pbs-plus/internal/proxy/controllers/exclusions"
	"github.com/pbs-plus/pbs-plus/internal/proxy/controllers/jobs"
	"github.com/pbs-plus/pbs-plus/internal/proxy/controllers/plus"
	"github.com/pbs-plus/pbs-plus/internal/proxy/controllers/targets"
	"github.com/pbs-plus/pbs-plus/internal/proxy/controllers/tokens"
	mw "github.com/pbs-plus/pbs-plus/internal/proxy/middlewares"
	rpcmount "github.com/pbs-plus/pbs-plus/internal/proxy/rpc"
	"github.com/pbs-plus/pbs-plus/internal/store"
	"github.com/pbs-plus/pbs-plus/internal/store/constants"
	"github.com/pbs-plus/pbs-plus/internal/store/proxmox"
	"github.com/pbs-plus/pbs-plus/internal/store/system"
	"github.com/pbs-plus/pbs-plus/internal/syslog"

	"net/http/pprof"

	// By default, it sets `GOMEMLIMIT` to 90% of cgroup's memory limit.
	// This is equivalent to `memlimit.SetGoMemLimitWithOpts(memlimit.WithLogger(slog.Default()))`
	// To disable logging, use `memlimit.SetGoMemLimitWithOpts` directly.
	_ "github.com/KimMachineGun/automemlimit"
)

var Version = "v0.0.0"

func main() {
	mainCtx, mainCancel := context.WithCancel(context.Background())
	defer mainCancel()

	proxmox.InitializeProxmox()

	jobRun := flag.String("job", "", "Job ID to execute")
	retryAttempts := flag.String("retry", "", "Current attempt number")
	flag.Parse()

	argsWithoutProg := os.Args[1:]

	if len(argsWithoutProg) > 0 && argsWithoutProg[0] == "clean-task-logs" {
		fmt.Println("WARNING: You are about to remove all junk logs recursively from:")
		fmt.Println("         /var/log/proxmox-backup/tasks")
		fmt.Println()
		fmt.Println("All log entries with the following substrings will be removed if found in any log file:")
		for _, substr := range backup.JunkSubstrings {
			fmt.Printf(" - %s\n", substr)
		}
		fmt.Println()
		fmt.Println("If this is not what you intend, press Ctrl+C within the next 10 seconds to cancel.")
		fmt.Println()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt)

		cancelChan := make(chan struct{})
		go func() {
			<-sigChan
			fmt.Println("\nOperation cancelled by user.")
			close(cancelChan)
		}()

		for i := 10; i > 0; i-- {
			select {
			case <-cancelChan:
				// User cancelled the operation.
				return
			default:
				fmt.Printf("Proceeding in %d seconds...\n", i)
				time.Sleep(1 * time.Second)
			}
		}

		fmt.Println("Proceeding with log cleanup...")

		removed, err := backup.RemoveJunkLogsRecursively("/var/log/proxmox-backup/tasks")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Successfully removed %d of junk lines from all task logs files.\n", removed)
		return
	}

	storeInstance, err := store.Initialize(mainCtx, nil)
	if err != nil {
		syslog.L.Error(err).WithMessage("failed to initialize store").Write()
		return
	}

	apiToken, err := proxmox.GetAPITokenFromFile()
	if err != nil {
		syslog.L.Error(err).WithMessage("failed to get token from file").Write()
	}
	proxmox.Session.APIToken = apiToken

	// Handle single job execution
	if *jobRun != "" {
		if proxmox.Session.APIToken == nil {
			return
		}

		jobTask, err := storeInstance.Database.GetJob(*jobRun)
		if err != nil {
			syslog.L.Error(err).Write()
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if retryAttempts == nil || *retryAttempts == "" {
			system.RemoveAllRetrySchedules(jobTask)
		}

		op, err := backup.RunBackup(ctx, jobTask, storeInstance, true)
		if err != nil {
			syslog.L.Error(err).WithField("jobId", jobTask.ID).Write()

			if !errors.Is(err, backup.ErrOneInstance) {
				if task, err := proxmox.GenerateTaskErrorFile(jobTask, err, []string{"Error handling from a scheduled job run request", "Job ID: " + jobTask.ID, "Source Mode: " + jobTask.SourceMode}); err != nil {
					syslog.L.Error(err).WithField("jobId", jobTask.ID).Write()
				} else {
					// Update job status
					latestJob, err := storeInstance.Database.GetJob(jobTask.ID)
					if err != nil {
						latestJob = jobTask
					}

					latestJob.LastRunUpid = task.UPID
					latestJob.LastRunState = task.Status
					latestJob.LastRunEndtime = task.EndTime

					err = storeInstance.Database.UpdateJob(nil, latestJob)
					if err != nil {
						syslog.L.Error(err).WithField("jobId", latestJob.ID).WithField("upid", task.UPID).Write()
					}
				}
				if err := system.SetRetrySchedule(jobTask); err != nil {
					syslog.L.Error(err).WithField("jobId", jobTask.ID).Write()
				}
			}
		}

		if waitErr := op.Wait(); waitErr != nil {
			syslog.L.Error(waitErr).Write()
		}

		return
	}

	if err = storeInstance.MigrateLegacyData(); err != nil {
		syslog.L.Error(err).WithMessage("error migrating legacy database").Write()
		return
	}

	if err := proxy.ModifyPBSJavascript(); err != nil {
		syslog.L.Error(err).WithMessage("failed to mount modified proxmox-backup-gui.js").Write()
		return
	}

	certOpts := certificates.DefaultOptions()
	generator, err := certificates.NewGenerator(certOpts)
	if err != nil {
		syslog.L.Error(err).WithMessage("failed to initialize certificate generator").Write()
		return
	}

	csrfKey, err := os.ReadFile("/etc/proxmox-backup/csrf.key")
	if err != nil {
		syslog.L.Error(err).WithMessage("failed to read csrf.key").Write()
		return
	}

	serverConfig := server.DefaultConfig()
	serverConfig.CertFile = filepath.Join(certOpts.OutputDir, "server.crt")
	serverConfig.KeyFile = filepath.Join(certOpts.OutputDir, "server.key")
	serverConfig.CAFile = filepath.Join(certOpts.OutputDir, "ca.crt")
	serverConfig.CAKey = filepath.Join(certOpts.OutputDir, "ca.key")
	serverConfig.TokenSecret = string(csrfKey)

	if err := generator.ValidateExistingCerts(); err != nil {
		if err := generator.GenerateCA(); err != nil {
			syslog.L.Error(err).WithMessage("failed to generate certificate").Write()
			return
		}

		if err := generator.GenerateCert("server"); err != nil {
			syslog.L.Error(err).WithMessage("failed to generate certificate").Write()
			return
		}
	}

	if err := serverConfig.Validate(); err != nil {
		syslog.L.Error(err).WithMessage("failed to validate server config").Write()
		return
	}

	storeInstance.CertGenerator = generator

	err = os.Chown(serverConfig.KeyFile, 0, 34)
	if err != nil {
		syslog.L.Error(err).WithMessage("failed to change cert key permissions").Write()
		return
	}

	err = os.Chown(serverConfig.CertFile, 0, 34)
	if err != nil {
		syslog.L.Error(err).WithMessage("failed to change cert permissions").Write()
		return
	}

	err = serverConfig.Mount()
	if err != nil {
		syslog.L.Error(err).WithMessage("failed to mount new certificate for mTLS").Write()
		return
	}
	defer func() {
		_ = serverConfig.Unmount()
	}()

	// Initialize token manager
	tokenManager, err := token.NewManager(token.Config{
		TokenExpiration: serverConfig.TokenExpiration,
		SecretKey:       serverConfig.TokenSecret,
	})
	if err != nil {
		syslog.L.Error(err).WithMessage("failed to initialize token manager").Write()
		return
	}
	storeInstance.Database.TokenManager = tokenManager

	// Setup HTTP server
	tlsConfig, err := serverConfig.LoadTLSConfig()
	if err != nil {
		return
	}

	caRenewalCtx, cancelRenewal := context.WithCancel(context.Background())
	defer cancelRenewal()
	go func() {
		for {
			select {
			case <-caRenewalCtx.Done():
				return
			case <-time.After(time.Hour):
				if err := generator.ValidateExistingCerts(); err != nil {
					if err := generator.GenerateCA(); err != nil {
						syslog.L.Error(err).WithMessage("failed to generate CA").Write()
					}

					if err := generator.GenerateCert("server"); err != nil {
						syslog.L.Error(err).WithMessage("failed to generate server certificate").Write()
					}
				}

			}
		}
	}()

	// Unmount and remove all stale mount points
	// Get all mount points under the base path
	mountPoints, err := filepath.Glob(filepath.Join(os.TempDir(), "pbsplus-*"))
	if err != nil {
		syslog.L.Error(err).WithMessage("failed to find agent mount base path").Write()
	}

	// Unmount each one
	for _, mountPoint := range mountPoints {
		umount := exec.Command("umount", "-lf", mountPoint)
		umount.Env = os.Environ()
		if err := umount.Run(); err != nil {
			// Optionally handle individual unmount errors
			syslog.L.Error(err).WithMessage("failed to unmount some mounted agents").Write()
		}

		if err := os.RemoveAll(mountPoint); err != nil {
			syslog.L.Error(err).WithMessage("failed to remove directory").Write()
		}
	}

	rpcCtx, rpcCancel := context.WithCancel(context.Background())
	defer rpcCancel()

	go func() {
		for {
			select {
			case <-rpcCtx.Done():
				syslog.L.Error(rpcCtx.Err()).WithMessage("rpc server cancelled")
				return
			default:
				if err := rpcmount.StartRPCServer(constants.MountSocketPath, storeInstance); err != nil {
					syslog.L.Error(err).WithMessage("rpc server failed, restarting")
				}
			}
		}
	}()

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/plus/token", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, plus.TokenHandler(storeInstance))))
	mux.HandleFunc("/api2/json/plus/version", mw.AgentOrServer(storeInstance, mw.CORS(storeInstance, plus.VersionHandler(storeInstance, Version))))
	mux.HandleFunc("/api2/json/plus/binary", mw.CORS(storeInstance, plus.DownloadBinary(storeInstance, Version)))
	mux.HandleFunc("/api2/json/plus/updater-binary", mw.CORS(storeInstance, plus.DownloadUpdater(storeInstance, Version)))
	mux.HandleFunc("/api2/json/plus/binary/checksum", mw.AgentOrServer(storeInstance, mw.CORS(storeInstance, plus.DownloadChecksum(storeInstance, Version))))
	mux.HandleFunc("/api2/json/d2d/backup", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, jobs.D2DJobHandler(storeInstance))))
	mux.HandleFunc("/api2/json/d2d/target", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, targets.D2DTargetHandler(storeInstance))))
	mux.HandleFunc("/api2/json/d2d/target/agent", mw.AgentOnly(storeInstance, mw.CORS(storeInstance, targets.D2DTargetAgentHandler(storeInstance))))
	mux.HandleFunc("/api2/json/d2d/token", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, tokens.D2DTokenHandler(storeInstance))))
	mux.HandleFunc("/api2/json/d2d/exclusion", mw.AgentOrServer(storeInstance, mw.CORS(storeInstance, exclusions.D2DExclusionHandler(storeInstance))))
	mux.HandleFunc("/api2/json/d2d/agent-log", mw.AgentOnly(storeInstance, mw.CORS(storeInstance, agents.AgentLogHandler(storeInstance))))

	// ExtJS routes with path parameters
	mux.HandleFunc("/api2/extjs/d2d/backup/{job}", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, jobs.ExtJsJobRunHandler(storeInstance))))
	mux.HandleFunc("/api2/extjs/config/d2d-target", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, targets.ExtJsTargetHandler(storeInstance))))
	mux.HandleFunc("/api2/extjs/config/d2d-target/{target}", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, targets.ExtJsTargetSingleHandler(storeInstance))))
	mux.HandleFunc("/api2/extjs/config/d2d-token", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, tokens.ExtJsTokenHandler(storeInstance))))
	mux.HandleFunc("/api2/extjs/config/d2d-token/{token}", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, tokens.ExtJsTokenSingleHandler(storeInstance))))
	mux.HandleFunc("/api2/extjs/config/d2d-exclusion", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, exclusions.ExtJsExclusionHandler(storeInstance))))
	mux.HandleFunc("/api2/extjs/config/d2d-exclusion/{exclusion}", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, exclusions.ExtJsExclusionSingleHandler(storeInstance))))
	mux.HandleFunc("/api2/extjs/config/disk-backup-job", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, jobs.ExtJsJobHandler(storeInstance))))
	mux.HandleFunc("/api2/extjs/config/disk-backup-job/{job}", mw.ServerOnly(storeInstance, mw.CORS(storeInstance, jobs.ExtJsJobSingleHandler(storeInstance))))

	// aRPC route
	mux.HandleFunc("/plus/arpc", mw.AgentOnly(storeInstance, arpc.ARPCHandler(storeInstance)))

	// Agent auth routes
	mux.HandleFunc("/plus/agent/bootstrap", mw.CORS(storeInstance, agents.AgentBootstrapHandler(storeInstance)))
	mux.HandleFunc("/plus/agent/renew", mw.AgentOnly(storeInstance, mw.CORS(storeInstance, agents.AgentRenewHandler(storeInstance))))
	mux.HandleFunc("/plus/agent/install/win", mw.CORS(storeInstance, plus.AgentInstallScriptHandler(storeInstance, Version)))

	// pprof routes
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	server := &http.Server{
		Addr:           serverConfig.Address,
		Handler:        mux,
		TLSConfig:      tlsConfig,
		ReadTimeout:    serverConfig.ReadTimeout,
		WriteTimeout:   serverConfig.WriteTimeout,
		IdleTimeout:    serverConfig.IdleTimeout,
		MaxHeaderBytes: serverConfig.MaxHeaderBytes,
	}

	syslog.L.Info().WithMessage("starting proxy server on :8008").Write()
	if err := server.ListenAndServeTLS(serverConfig.CertFile, serverConfig.KeyFile); err != nil {
		syslog.L.Error(err).WithMessage("http server failed")
	}
}
