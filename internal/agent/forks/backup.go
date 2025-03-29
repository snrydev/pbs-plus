package forks

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containers/winquit/pkg/winquit"
	"github.com/pbs-plus/pbs-plus/internal/agent"
	"github.com/pbs-plus/pbs-plus/internal/agent/agentfs"
	"github.com/pbs-plus/pbs-plus/internal/agent/registry"
	"github.com/pbs-plus/pbs-plus/internal/agent/snapshots"
	"github.com/pbs-plus/pbs-plus/internal/arpc"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
	"github.com/pbs-plus/pbs-plus/internal/utils/safemap"
)

var (
	activeSessions *safemap.Map[string, *backupSession]
)

func init() {
	activeSessions = safemap.New[string, *backupSession]()
}

type backupSession struct {
	jobId    string
	ctx      context.Context
	cancel   context.CancelFunc
	store    *agent.BackupStore
	snapshot snapshots.Snapshot
	fs       *agentfs.AgentFSServer
	once     sync.Once
}

func (s *backupSession) Close() {
	s.once.Do(func() {
		if s.fs != nil {
			s.fs.Close()
		}
		if s.snapshot != (snapshots.Snapshot{}) && !s.snapshot.Direct && s.snapshot.Handler != nil {
			s.snapshot.Handler.DeleteSnapshot(s.snapshot)
		}
		if s.store != nil {
			_ = s.store.EndBackup(s.jobId)
		}
		activeSessions.Del(s.jobId)
		s.cancel()
	})
}

func CmdBackup() {
	// Define and parse flags.
	cmdMode := flag.String("cmdMode", "", "Cmd Mode")
	sourceMode := flag.String("sourceMode", "", "Backup source mode (e.g., direct or snapshot)")
	drive := flag.String("drive", "", "Drive or path for backup")
	jobId := flag.String("jobId", "", "Unique job identifier for the backup")
	flag.Parse()

	if *cmdMode != "backup" {
		return
	}

	// Validate required flags.
	if *sourceMode == "" || *drive == "" || *jobId == "" {
		fmt.Fprintln(os.Stderr, "Error: missing required flags: sourceMode, drive, and jobId are required")
		os.Exit(1)
	}

	// Establish connection to the server.
	serverUrl, err := registry.GetEntry(registry.CONFIG, "ServerURL", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid server URL: %v", err)
		return
	}
	uri, err := url.Parse(serverUrl.Value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid server URL: %v", err)
		return
	}

	tlsConfig, err := agent.GetTLSConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get TLS config for ARPC client: %v", err)
		return
	}

	headers := http.Header{}
	headers.Add("X-PBS-Plus-JobId", *jobId)

	rpcSess, err := arpc.ConnectToServer(context.Background(), false, uri.Host, headers, tlsConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to server: %v", err)
		return
	}
	rpcSess.SetRouter(arpc.NewRouter())

	// Start the long-running background RPC goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer rpcSess.Close()
		defer wg.Done()
		if err := rpcSess.Serve(); err != nil {
			if session, ok := activeSessions.Get(*jobId); ok {
				session.Close()
			}
		}
	}()

	// Call the Backup function.
	backupMode, err := Backup(rpcSess, *sourceMode, *drive, *jobId)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	fmt.Println(backupMode)

	done := make(chan os.Signal, 1)

	signal.Notify(done, syscall.SIGINT)
	winquit.SimulateSigTermOnQuit(done)

	go func() {
		<-done
		rpcSess.Close()
		if session, ok := activeSessions.Get(*jobId); ok {
			session.Close()
		}
	}()

	// Block here until the background RPC goroutine ends.
	wg.Wait()
}

func ExecBackup(sourceMode string, drive string, jobId string) (string, int, error) {
	execCmd, err := os.Executable()
	if err != nil {
		return "", -1, err
	}

	if sourceMode == "" {
		sourceMode = "snapshot"
	}

	// Prepare the flags as command-line arguments.
	args := []string{
		"--cmdMode=backup",
		"--sourceMode=" + sourceMode,
		"--drive=" + drive,
		"--jobId=" + jobId,
	}

	// Create the command.
	cmd := exec.Command(execCmd, args...)

	// Use a pipe to read stdout.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", -1, err
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", -1, err
	}

	if err := cmd.Start(); err != nil {
		return "", -1, err
	}

	pid := cmd.Process.Pid

	errScanner := bufio.NewScanner(stderrPipe)

	// Read only the first line that contains backupMode.
	scanner := bufio.NewScanner(stdoutPipe)
	var backupMode string
	if scanner.Scan() {
		backupMode = scanner.Text()
	} else {
		if errScanner.Scan() {
			return "", cmd.Process.Pid, fmt.Errorf("error from child process: %v", errScanner.Text())
		}
		return "", cmd.Process.Pid, fmt.Errorf("failed to read backup mode from child process")
	}

	// Optionally you could check for scanner.Err() here.
	if err := scanner.Err(); err != nil {
		return "", cmd.Process.Pid, err
	}

	// Detach from the child process so that ExecBackup doesn't wait for it to complete.
	if err := cmd.Process.Release(); err != nil {
		return "", cmd.Process.Pid, err
	}

	go monitorDetachedProcess(pid, jobId)

	return strings.TrimSpace(backupMode), cmd.Process.Pid, nil
}

func Backup(rpcSess *arpc.Session, sourceMode string, drive string, jobId string) (string, error) {
	store, err := agent.NewBackupStore()
	if err != nil {
		return "", err
	}
	if existingSession, ok := activeSessions.Get(jobId); ok {
		existingSession.Close()
		_ = store.EndBackup(jobId)
	}

	sessionCtx, cancel := context.WithCancel(context.Background())
	session := &backupSession{
		jobId:  jobId,
		ctx:    sessionCtx,
		cancel: cancel,
		store:  store,
	}
	activeSessions.Set(jobId, session)

	if hasActive, err := store.HasActiveBackupForJob(jobId); hasActive || err != nil {
		if err != nil {
			return "", err
		}
		_ = store.EndBackup(jobId)
	}

	if err := store.StartBackup(jobId); err != nil {
		session.Close()
		return "", err
	}

	var snapshot snapshots.Snapshot

	backupMode := sourceMode

	switch sourceMode {
	case "direct":
		path := drive
		if runtime.GOOS == "windows" {
			volName := filepath.VolumeName(fmt.Sprintf("%s:", drive))
			path = volName + "\\"
		}
		snapshot = snapshots.Snapshot{
			Path:        path,
			TimeStarted: time.Now(),
			SourcePath:  drive,
			Direct:      true,
		}
	default:
		var err error
		snapshot, err = snapshots.Manager.CreateSnapshot(jobId, drive)
		if err != nil && snapshot == (snapshots.Snapshot{}) {
			syslog.L.Error(err).WithMessage("Warning: VSS snapshot failed and has switched to direct backup mode.").Write()
			backupMode = "direct"

			path := drive
			if runtime.GOOS == "windows" {
				volName := filepath.VolumeName(fmt.Sprintf("%s:", drive))
				path = volName + "\\"
			}
			snapshot = snapshots.Snapshot{
				Path:        path,
				TimeStarted: time.Now(),
				SourcePath:  drive,
				Direct:      true,
			}
		}
	}

	session.snapshot = snapshot

	fs := agentfs.NewAgentFSServer(jobId, snapshot)
	if fs == nil {
		session.Close()
		return "", fmt.Errorf("fs is nil")
	}
	fs.RegisterHandlers(rpcSess.GetRouter())
	session.fs = fs

	return backupMode, nil
}

func monitorDetachedProcess(pid int, jobId string) {
	syslog.L.Info().WithMessage("Monitoring Backup Job process PID").WithFields(map[string]interface{}{"pid": pid, "jobId": jobId})

	store, _ := agent.NewBackupStore()

	// Check immediately first, in case it exited very quickly after release
	if !isProcessRunning(pid) {
		syslog.L.Info().WithMessage("Backup Job process exited, ending session").WithFields(map[string]interface{}{"pid": pid, "jobId": jobId})
		if existingSession, ok := activeSessions.Get(jobId); ok {
			existingSession.Close()
			if store != nil {
				_ = store.EndBackup(jobId)
			}
		}
		return
	}

	// Poll periodically
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for range ticker.C {
		if !isProcessRunning(pid) {
			syslog.L.Info().WithMessage("Backup Job process exited, ending session").WithFields(map[string]interface{}{"pid": pid, "jobId": jobId})
			if existingSession, ok := activeSessions.Get(jobId); ok {
				existingSession.Close()
				if store != nil {
					_ = store.EndBackup(jobId)
				}
			}
			return
		}
	}
}

func isProcessRunning(pid int) bool {
	// os.FindProcess is the most basic check. It always returns a Process object
	// on Unix, even if the PID doesn't exist. On Windows, it might error if
	// the process doesn't exist, but not reliably.
	process, err := os.FindProcess(pid)
	if err != nil {
		// On Windows, an error here *might* mean it's gone, but could also be permissions.
		// On Unix, this error is less likely unless PID is invalid (<0).
		// Conservatively assume it might still be running if we get an unexpected error.
		syslog.L.Error(err).WithMessage("Unable to find process").WithFields(map[string]interface{}{"pid": pid})
		// Let's return false on error, assuming it's likely gone or inaccessible.
		return false
	}

	// The portable way to check if a process exists is to send it signal 0.
	// This doesn't actually send a signal, but checks permissions/existence.
	// On Unix: Checks if the process exists and we have permission.
	// On Windows: Checks if the process handle is valid (which os.FindProcess gives us).
	//             This works even after Release() because FindProcess gets a *new* handle.
	err = process.Signal(syscall.Signal(0))

	// If err is nil, the process exists (or existed recently).
	// If err is os.ErrProcessDone (on Unix), the process is definitely gone.
	// If err is another error (e.g., permission denied on Unix, other errors on Windows),
	// the process might still exist but we can't signal it.
	// For simplicity, we treat nil error as "running" and any error as "not running or inaccessible".
	if err == nil {
		return true
	}

	// Specifically check for ErrProcessDone on Unix for a definitive "exited" state.
	if runtime.GOOS != "windows" && errors.Is(err, os.ErrProcessDone) {
		return false // Definitely exited on Unix
	}

	// For other errors or on Windows, log the error and assume not running/accessible.
	// Note: On Windows, err might be "Access is denied." or "The parameter is incorrect."
	// if the process is gone. It's not as clean as ErrProcessDone.
	// log.Printf("Signal 0 to PID %d failed: %v. Assuming not running.", pid, err)
	return false
}
