//go:build linux

package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/backend/mount"
	"github.com/pbs-plus/pbs-plus/internal/store/proxmox"
	"github.com/pbs-plus/pbs-plus/internal/store/system"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
	"github.com/pbs-plus/pbs-plus/internal/utils"
)

func (op *BackupOperation) executeDiskJob(ctx context.Context) error {
	var err error

	clientLogFile := syslog.CreateBackupLogger(op.job.ID)

	errorMonitorDone := make(chan struct{})

	var agentMount *mount.AgentMount

	errCleanUp := func() {
		utils.ClearIOStats(op.job.CurrentPID)
		op.job.CurrentPID = 0

		if op.lock != nil {
			op.lock.Unlock()
		}
		if agentMount != nil {
			agentMount.Unmount()
			agentMount.CloseMount()
		}
		if clientLogFile != nil {
			_ = clientLogFile.Close()
		}
		close(errorMonitorDone)
	}

	if proxmox.Session.APIToken == nil {
		errCleanUp()
		return ErrAPITokenRequired
	}

	target, err := op.storeInstance.Database.GetTarget(op.job.Target)
	if err != nil {
		errCleanUp()
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrTargetNotFound, op.job.Target)
		}
		return fmt.Errorf("%w: %v", ErrTargetGet, err)
	}

	isAgent := strings.HasPrefix(target.Path, "agent://")
	backupId, _ := getBackupId(isAgent, target.Name)

	if !op.skipCheck && isAgent {
		targetSplit := strings.Split(target.Name, " - ")
		_, exists := op.storeInstance.ARPCSessionManager.GetSession(targetSplit[0])
		if !exists {
			errCleanUp()
			return fmt.Errorf("%w: %s", ErrTargetUnreachable, op.job.Target)
		}
	} else if !op.skipCheck && !isAgent {
		_, err := os.Stat(target.Path)
		if err != nil {
			errCleanUp()
			return fmt.Errorf("%w: %s (%v)", ErrTargetUnreachable, op.job.Target, err)
		}
	}

	op.queueTask.UpdateDescription("mounting target to server")

	srcPath := target.Path
	if isAgent {
		if op.job.SourceMode == "snapshot" {
			op.queueTask.UpdateDescription("waiting for agent to finish snapshot")
		}

		agentMount, err = mount.Mount(op.storeInstance, op.job, target)
		if err != nil {
			errCleanUp()
			return fmt.Errorf("%w: %v", ErrMountInitialization, err)
		}
		srcPath = agentMount.Path

		// In case mount updates the job.
		latestAgent, err := op.storeInstance.Database.GetJob(op.job.ID)
		if err == nil {
			op.job = latestAgent
		}
	}
	srcPath = filepath.Join(srcPath, op.job.Subpath)

	op.queueTask.UpdateDescription("waiting for proxmox-backup-client to start")

	cmd, err := prepareBackupCommand(ctx, op.job, op.storeInstance, srcPath, isAgent, op.extraExclusions)
	if err != nil {
		errCleanUp()
		return fmt.Errorf("%w: %v", ErrPrepareBackupCommand, err)
	}

	readyChan := make(chan struct{})
	taskResultChan := make(chan proxmox.Task, 1)
	taskErrorChan := make(chan error, 1)

	monitorCtx, monitorCancel := context.WithTimeout(ctx, 20*time.Second)
	defer monitorCancel()

	syslog.L.Info().WithMessage("starting monitor goroutine").Write()
	go func() {
		defer syslog.L.Info().WithMessage("monitor goroutine closing").Write()

		task, err := proxmox.Session.GetJobTask(monitorCtx, readyChan, op.job.Store, backupId)
		if err != nil {
			syslog.L.Error(err).WithMessage("found error in getjobtask return").Write()

			select {
			case taskErrorChan <- err:
			case <-monitorCtx.Done():
			}
			return
		}

		syslog.L.Info().WithMessage("found task in getjobtask return").WithField("task", task.UPID).Write()

		select {
		case taskResultChan <- task:
		case <-monitorCtx.Done():
		}
	}()

	select {
	case <-readyChan:
	case err := <-taskErrorChan:
		monitorCancel()
		errCleanUp()
		return fmt.Errorf("%w: %v", ErrTaskMonitoringInitializationFailed, err)
	case <-monitorCtx.Done():
		errCleanUp()
		return fmt.Errorf("%w: %v", ErrTaskMonitoringTimedOut, monitorCtx.Err())
	}

	currOwner, _ := GetCurrentOwner(op.job.Namespace, op.job.Store, op.storeInstance)
	_ = FixDatastore(backupId, op.job.Store, op.job.Namespace, op.storeInstance)

	stdoutWriter := io.MultiWriter(clientLogFile.File, os.Stdout)
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stdoutWriter

	syslog.L.Info().WithMessage("starting backup job").WithField("args", cmd.Args).Write()
	if err := cmd.Start(); err != nil {
		monitorCancel()
		if currOwner != "" {
			_ = SetDatastoreOwner(backupId, op.job.Store, op.job.Namespace, op.storeInstance, currOwner)
		}
		errCleanUp()
		return fmt.Errorf("%w (%s): %v",
			ErrProxmoxBackupClientStart, cmd.String(), err)
	}

	if cmd.Process != nil {
		op.job.CurrentPID = cmd.Process.Pid
	}

	go monitorPBSClientLogs(clientLogFile.Path, cmd, errorMonitorDone)

	syslog.L.Info().WithMessage("waiting for task monitoring results").Write()
	var task proxmox.Task
	select {
	case task = <-taskResultChan:
	case err := <-taskErrorChan:
		monitorCancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		errCleanUp()
		if currOwner != "" {
			_ = SetDatastoreOwner(backupId, op.job.Store, op.job.Namespace, op.storeInstance, currOwner)
		}
		if os.IsNotExist(err) {
			return ErrNilTask
		}
		return fmt.Errorf("%w: %v", ErrTaskDetectionFailed, err)
	case <-monitorCtx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		errCleanUp()
		if currOwner != "" {
			_ = SetDatastoreOwner(backupId, op.job.Store, op.job.Namespace, op.storeInstance, currOwner)
		}
		return fmt.Errorf("%w: %v", ErrTaskDetectionTimedOut, monitorCtx.Err())
	}

	if err := updateJobStatus(false, 0, op.job, task, op.storeInstance); err != nil {
		errCleanUp()
		if currOwner != "" {
			_ = SetDatastoreOwner(backupId, op.job.Store, op.job.Namespace, op.storeInstance, currOwner)
		}
		return fmt.Errorf("%w: %v", ErrJobStatusUpdateFailed, err)
	}

	syslog.L.Info().WithMessage("task monitoring finished").WithField("task", task.UPID).Write()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	op.Task = task
	op.waitGroup = wg

	go func() {
		defer wg.Done()
		defer func() {
			if op.lock != nil {
				op.lock.Unlock()
			}
		}()

		if err := cmd.Wait(); err != nil {
			op.err = err
		}

		utils.ClearIOStats(op.job.CurrentPID)
		op.job.CurrentPID = 0

		close(errorMonitorDone)

		for _, extraExclusion := range op.extraExclusions {
			syslog.L.Warn().WithJob(op.job.ID).WithMessage(fmt.Sprintf("skipped %s due to an error from previous retry attempts", extraExclusion)).Write()
		}

		// Agent mount must still be connected after proxmox-backup-client terminates
		// If not, then the client side process crashed
		gracefulEnd := true
		if agentMount != nil {
			mountConnected := agentMount.IsConnected()
			if !mountConnected {
				gracefulEnd = false
			}
		}

		succeeded, cancelled, warningsNum, errorPath, err := processPBSProxyLogs(gracefulEnd, task.UPID, clientLogFile)
		if err != nil {
			syslog.L.Error(err).
				WithMessage("failed to process logs").
				Write()
		}

		if errorPath != "" {
			op.extraExclusions = append(op.extraExclusions, errorPath)
		}

		_ = clientLogFile.Close()

		if err := updateJobStatus(succeeded, warningsNum, op.job, task, op.storeInstance); err != nil {
			syslog.L.Error(err).
				WithMessage("failed to update job status - post cmd.Wait").
				Write()
		}

		if succeeded || cancelled {
			system.RemoveAllRetrySchedules(op.job.ID, string(Disk))
		} else {
			if err := system.SetRetrySchedule(op.job.ID, string(Disk), op.job.Retry, op.job.RetryInterval, op.extraExclusions); err != nil {
				syslog.L.Error(err).WithField("jobId", op.job.ID).Write()
			}
		}

		if currOwner != "" {
			_ = SetDatastoreOwner(backupId, op.job.Store, op.job.Namespace, op.storeInstance, currOwner)
		}

		if agentMount != nil {
			agentMount.Unmount()
			agentMount.CloseMount()
		}
	}()

	return nil
}
