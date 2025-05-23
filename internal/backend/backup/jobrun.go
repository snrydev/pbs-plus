//go:build linux

package backup

import (
	"context"
	"errors"
	"sync"

	"github.com/pbs-plus/pbs-plus/internal/store"
	"github.com/pbs-plus/pbs-plus/internal/store/proxmox"
	"github.com/pbs-plus/pbs-plus/internal/store/types"
)

// Sentinel error values.
var (
	ErrJobMutexCreation = errors.New("failed to create job mutex")
	ErrOneInstance      = errors.New("a job is still running; only one instance allowed")

	ErrStdoutTempCreation = errors.New("failed to create stdout temp file")

	ErrBackupMutexCreation = errors.New("failed to create backup mutex")
	ErrBackupMutexLock     = errors.New("failed to lock backup mutex")

	ErrAPITokenRequired = errors.New("API token is required")

	ErrTargetGet         = errors.New("failed to get target")
	ErrTargetNotFound    = errors.New("target does not exist")
	ErrTargetUnreachable = errors.New("target unreachable")

	ErrMountInitialization  = errors.New("mount initialization error")
	ErrPrepareBackupCommand = errors.New("failed to prepare backup command")

	ErrTaskMonitoringInitializationFailed = errors.New("task monitoring initialization failed")
	ErrTaskMonitoringTimedOut             = errors.New("task monitoring initialization timed out")

	ErrProxmoxBackupClientStart = errors.New("proxmox-backup-client start error")

	ErrNilTask               = errors.New("received nil task")
	ErrTaskDetectionFailed   = errors.New("task detection failed")
	ErrTaskDetectionTimedOut = errors.New("task detection timed out")

	ErrJobStatusUpdateFailed = errors.New("failed to update job status")

	ErrInvalidOperationMode = errors.New("invalid operation mode")
)

type BackupType string

const (
	Disk     BackupType = "disk"
	Database BackupType = "database"
)

// BackupOperation encapsulates a backup operation.
type BackupOperation struct {
	Task      proxmox.Task
	queueTask *proxmox.QueuedTask
	waitGroup *sync.WaitGroup
	err       error

	mode BackupType

	job      types.Job
	dbJob    types.DatabaseJob
	dbTarget types.DatabaseTarget

	storeInstance   *store.Store
	skipCheck       bool
	web             bool
	extraExclusions []string
	lock            *sync.Mutex
}

// Wait blocks until the backup operation is complete.
func (b *BackupOperation) Wait() error {
	if b.waitGroup != nil {
		b.waitGroup.Wait()
	}
	return b.err
}

func NewJob(
	job types.Job,
	storeInstance *store.Store,
	skipCheck bool,
	web bool,
	extraExclusions []string,
) (*BackupOperation, error) {
	return &BackupOperation{
		job:             job,
		mode:            Disk,
		storeInstance:   storeInstance,
		skipCheck:       skipCheck,
		web:             web,
		extraExclusions: extraExclusions,
	}, nil
}

func NewDatabaseJob(
	job types.DatabaseJob,
	target types.DatabaseTarget,
	storeInstance *store.Store,
	skipCheck bool,
	web bool,
	extraExclusions []string,
) (*BackupOperation, error) {
	return &BackupOperation{
		dbJob:         job,
		dbTarget:      target,
		mode:          Database,
		storeInstance: storeInstance,
		web:           web,
	}, nil
}

func (op *BackupOperation) Execute(ctx context.Context) error {
	switch op.mode {
	case Disk:
		return op.executeDiskJob(ctx)
	case Database:
		return op.executeDatabaseJob(ctx)
	}

	return ErrInvalidOperationMode
}
