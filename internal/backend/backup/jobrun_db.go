//go:build linux

package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/backend/database"
	"github.com/pbs-plus/pbs-plus/internal/store/proxmox"
	"github.com/pbs-plus/pbs-plus/internal/store/system"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
	"github.com/pbs-plus/pbs-plus/internal/utils"
)

func (op *BackupOperation) executeDatabaseJob(ctx context.Context) error {
	var err error

	clientLogFile := syslog.CreateBackupLogger("db-" + op.dbJob.ID)

	errorMonitorDone := make(chan struct{})

	dumpDir := ""

	errCleanUp := func() {
		utils.ClearIOStats(op.dbJob.CurrentPID)
		op.dbJob.CurrentPID = 0

		if op.lock != nil {
			op.lock.Unlock()
		}
		if clientLogFile != nil {
			_ = clientLogFile.Close()
		}
		close(errorMonitorDone)
		if dumpDir != "" {
			_ = os.RemoveAll(dumpDir)
		}
	}

	if proxmox.Session.APIToken == nil {
		errCleanUp()
		return ErrAPITokenRequired
	}

	creds, err := op.storeInstance.Database.GetDatabaseTargetCredentials(op.dbJob.Target)
	if err != nil {
		errCleanUp()
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrTargetNotFound, op.dbJob.Target)
		}
		return fmt.Errorf("%w: %v", ErrTargetGet, err)
	}

	op.dbTarget.Username = creds.Username
	op.dbTarget.Password = creds.Password

	op.queueTask.UpdateDescription("dumping database to file")

	dumpPath, err := database.DumpDatabase(op.dbTarget)
	if err != nil {
		errCleanUp()
		return fmt.Errorf("%w: %v", ErrTargetGet, err)
	}
	dumpDir = filepath.Dir(dumpPath)

	op.queueTask.UpdateDescription("waiting for proxmox-backup-client to start")

	cmd, err := prepareDBBackupCommand(ctx, op.dbJob, op.dbTarget.Host, op.storeInstance, dumpDir)
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
		task, err := proxmox.Session.GetJobTask(monitorCtx, readyChan, op.dbJob.Store, op.dbTarget.Host)
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

	currOwner, _ := GetCurrentOwner(op.dbJob.Namespace, op.dbJob.Store, op.storeInstance)
	_ = FixDatastore(op.dbTarget.Host, op.dbJob.Store, op.dbJob.Namespace, op.storeInstance)

	stdoutWriter := io.MultiWriter(clientLogFile.File, os.Stdout)
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stdoutWriter

	syslog.L.Info().WithMessage("starting backup job").WithField("args", cmd.Args).Write()
	if err := cmd.Start(); err != nil {
		monitorCancel()
		if currOwner != "" {
			_ = SetDatastoreOwner(op.dbTarget.Host, op.dbJob.Store, op.dbJob.Namespace, op.storeInstance, currOwner)
		}
		errCleanUp()
		return fmt.Errorf("%w (%s): %v",
			ErrProxmoxBackupClientStart, cmd.String(), err)
	}

	if cmd.Process != nil {
		op.dbJob.CurrentPID = cmd.Process.Pid
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
			_ = SetDatastoreOwner(op.dbTarget.Host, op.dbJob.Store, op.dbJob.Namespace, op.storeInstance, currOwner)
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
			_ = SetDatastoreOwner(op.dbTarget.Host, op.dbJob.Store, op.dbJob.Namespace, op.storeInstance, currOwner)
		}
		return fmt.Errorf("%w: %v", ErrTaskDetectionTimedOut, monitorCtx.Err())
	}

	if err := updateDBJobStatus(false, 0, op.dbJob, task, op.storeInstance); err != nil {
		errCleanUp()
		if currOwner != "" {
			_ = SetDatastoreOwner(op.dbTarget.Host, op.dbJob.Store, op.dbJob.Namespace, op.storeInstance, currOwner)
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

		utils.ClearIOStats(op.dbJob.CurrentPID)
		op.dbJob.CurrentPID = 0

		close(errorMonitorDone)

		// Agent mount must still be connected after proxmox-backup-client terminates
		// If not, then the client side process crashed
		gracefulEnd := true
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

		if err := updateDBJobStatus(succeeded, warningsNum, op.dbJob, task, op.storeInstance); err != nil {
			syslog.L.Error(err).
				WithMessage("failed to update job status - post cmd.Wait").
				Write()
		}

		if succeeded || cancelled {
			system.RemoveAllRetrySchedules(op.dbJob.ID, string(Database))
		} else {
			if err := system.SetRetrySchedule(op.dbJob.ID, string(Database), op.dbJob.Retry, op.dbJob.RetryInterval, op.extraExclusions); err != nil {
				syslog.L.Error(err).WithField("jobId", op.dbJob.ID).Write()
			}
		}

		if currOwner != "" {
			_ = SetDatastoreOwner(op.dbTarget.Host, op.dbJob.Store, op.dbJob.Namespace, op.storeInstance, currOwner)
		}

		if dumpDir != "" {
			_ = os.RemoveAll(dumpDir)
		}
	}()

	return nil
}
