//go:build linux

package mount

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"os/exec"
	"strings"
	"time"

	rpcmount "github.com/pbs-plus/pbs-plus/internal/proxy/rpc"
	"github.com/pbs-plus/pbs-plus/internal/store"
	"github.com/pbs-plus/pbs-plus/internal/store/constants"
	"github.com/pbs-plus/pbs-plus/internal/store/types"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
)

type AgentMount struct {
	JobId    string
	Hostname string
	Drive    string
	Path     string
}

func Mount(storeInstance *store.Store, job types.Job, target types.Target) (*AgentMount, error) {
	// Parse target information
	splittedTargetName := strings.Split(target.Name, " - ")
	targetHostname := splittedTargetName[0]
	agentPath := strings.TrimPrefix(target.Path, "agent://")
	agentPathParts := strings.Split(agentPath, "/")
	agentDrive := agentPathParts[1]

	agentMount := &AgentMount{
		JobId:    job.ID,
		Hostname: targetHostname,
		Drive:    agentDrive,
	}

	// Create mount directory if it doesn't exist
	path, err := os.MkdirTemp("", "pbsplus-"+job.ID)
	if err != nil {
		agentMount.CloseMount()
		return nil, fmt.Errorf("Mount: error creating directory \"%s\" -> %w", agentMount.Path, err)
	}

	// Setup mount path
	agentMount.Path = path

	// Try mounting with retries
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	errCleanup := func() {
		agentMount.CloseMount()
		agentMount.Unmount()
	}

	args := &rpcmount.BackupArgs{
		JobId:          job.ID,
		TargetHostname: targetHostname,
		Drive:          agentDrive,
		MountPath:      agentMount.Path,
	}
	var reply rpcmount.BackupReply

	conn, err := net.DialTimeout("unix", constants.MountSocketPath, 5*time.Minute)
	if err != nil {
		errCleanup()
		return nil, fmt.Errorf("failed to dial RPC server: %w", err)
	} else {
		rpcClient := rpc.NewClient(conn)
		err = rpcClient.Call("MountRPCService.Backup", args, &reply)
		rpcClient.Close()
		if err != nil {
			errCleanup()
			return nil, fmt.Errorf("failed to call backup RPC: %w", err)
		}
		if reply.Status != 200 {
			errCleanup()
			return nil, fmt.Errorf("backup RPC returned an error %d: %s", reply.Status, reply.Message)
		}
	}

	isAccessible := false
	checkTimeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

checkLoop:
	for {
		select {
		case <-checkTimeout:
			break checkLoop
		case <-ticker.C:
			if _, err := os.ReadDir(agentMount.Path); err == nil {
				isAccessible = true
				break checkLoop
			}
		}
	}
	if !isAccessible {
		errCleanup()
		return nil, fmt.Errorf("Mount: mounted directory not accessible after timeout")
	}
	return agentMount, nil
}

func (a *AgentMount) Unmount() {
	if a.Path == "" {
		return
	}

	// First try a clean unmount
	umount := exec.Command("umount", "-lf", a.Path)
	umount.Env = os.Environ()
	err := umount.Run()
	if err == nil {
		_ = os.RemoveAll(a.Path)
	}
}

func (a *AgentMount) CloseMount() {
	args := &rpcmount.CleanupArgs{
		JobId:          a.JobId,
		TargetHostname: a.Hostname,
		Drive:          a.Drive,
	}
	var reply rpcmount.CleanupReply

	conn, err := net.DialTimeout("unix", constants.MountSocketPath, 5*time.Minute)
	if err != nil {
		return
	}
	rpcClient := rpc.NewClient(conn)
	defer rpcClient.Close()

	if err := rpcClient.Call("MountRPCService.Cleanup", args, &reply); err != nil {
		syslog.L.Error(err).WithFields(map[string]interface{}{"hostname": a.Hostname, "drive": a.Drive}).Write()
	}
}
