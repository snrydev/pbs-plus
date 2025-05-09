//go:build linux

package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pbs-plus/pbs-plus/internal/store/proxmox"
	"github.com/pbs-plus/pbs-plus/internal/store/system"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
	"github.com/puzpuzpuz/xsync/v3"
)

type Manager struct {
	ctx         context.Context
	cancel      context.CancelFunc
	jobs        chan *BackupOperation
	mu          sync.Mutex
	executeMu   sync.Mutex
	locks       *xsync.MapOf[string, *sync.Mutex]
	runningJobs atomic.Int32

	maxConcurrentJobs int
	semaphore         chan struct{}
}

func NewManager(ctx context.Context, size int, maxConcurrent int) *Manager {
	if maxConcurrent <= 0 {
		maxConcurrent = 1 // Ensure at least one job can run
	}

	newCtx, cancel := context.WithCancel(ctx)
	jq := &Manager{
		ctx:               newCtx,
		cancel:            cancel,
		jobs:              make(chan *BackupOperation, size),
		mu:                sync.Mutex{},
		locks:             xsync.NewMapOf[string, *sync.Mutex](),
		maxConcurrentJobs: maxConcurrent,
		semaphore:         make(chan struct{}, maxConcurrent),
	}

	go jq.worker()

	return jq
}

func (jq *Manager) Enqueue(job *BackupOperation) {
	jq.mu.Lock()
	defer jq.mu.Unlock()

	if jq.ctx.Err() != nil {
		jq.CreateError(job, fmt.Errorf("Attempt to enqueue on closed queue"))
		return
	}

	switch job.mode {
	case Disk:
		lock, _ := jq.locks.LoadOrStore(job.job.ID, &sync.Mutex{})

		if locked := lock.TryLock(); !locked {
			jq.CreateError(job, ErrOneInstance)
			return
		}

		job.lock = lock

		targetName := strings.TrimSpace(strings.Split(job.job.Target, " - ")[0])
		queueTask, err := proxmox.GenerateQueuedTask(targetName, job.job.Store, job.web)
		if err != nil {
			syslog.L.Error(err).WithMessage("failed to create queue task, not fatal").Write()
		} else {
			if err := updateJobStatus(false, 0, job.job, queueTask.Task, job.storeInstance); err != nil {
				syslog.L.Error(err).WithMessage("failed to set queue task, not fatal").Write()
			}
		}

		job.queueTask = &queueTask
	case Database:
		lock, _ := jq.locks.LoadOrStore(job.dbJob.ID, &sync.Mutex{})

		if locked := lock.TryLock(); !locked {
			jq.CreateError(job, ErrOneInstance)
			return
		}

		job.lock = lock

		queueTask, err := proxmox.GenerateQueuedTask(job.dbTarget.Host, job.dbJob.Store, job.web)
		if err != nil {
			syslog.L.Error(err).WithMessage("failed to create queue task, not fatal").Write()
		} else {
			if err := updateDBJobStatus(false, 0, job.dbJob, queueTask.Task, job.storeInstance); err != nil {
				syslog.L.Error(err).WithMessage("failed to set queue task, not fatal").Write()
			}
		}

		job.queueTask = &queueTask
	}

	select {
	case jq.jobs <- job:
	default:
		jq.CreateError(job, fmt.Errorf("Queue is full. Job rejected."))
		job.lock.Unlock()
	}
}

func (jq *Manager) worker() {
	for {
		select {
		case <-jq.ctx.Done():
			return
		case op := <-jq.jobs:
			go func(opToRun *BackupOperation) {
				select {
				case <-jq.ctx.Done():
					opToRun.lock.Unlock()
					return
				case jq.semaphore <- struct{}{}:
				}

				defer func() {
					<-jq.semaphore
					opToRun.queueTask.Close()
					jq.runningJobs.Add(-1)
				}()

				jq.runningJobs.Add(1)

				// Acquire lock for sequential execution
				jq.executeMu.Lock()
				err := opToRun.Execute(jq.ctx)
				// Release lock immediately after Execute finishes
				jq.executeMu.Unlock()

				if err != nil {
					jq.CreateError(opToRun, err)
				} else {
					opToRun.Wait()
				}
			}(op)
		}
	}
}

func (jq *Manager) CreateError(op *BackupOperation, err error) {
	syslog.L.Error(err).WithField("jobId", op.job.ID).Write()

	datastore := ""
	backupId := ""
	jobId := ""
	sourceMode := ""

	switch op.mode {
	case Disk:
		jobId = op.job.ID
		targetName := strings.TrimSpace(strings.Split(op.job.Target, " - ")[0])
		backupId = targetName
		datastore = op.job.Store
		sourceMode = op.job.SourceMode
	case Database:
		jobId = op.dbJob.ID
		backupId = op.dbTarget.Host
		datastore = op.dbJob.Store
		sourceMode = string(op.dbTarget.Type)
	}

	if !errors.Is(err, ErrOneInstance) {
		if task, err := proxmox.GenerateTaskErrorFile(backupId, datastore, err, []string{"Error handling from a scheduled job run request", "Job ID: " + jobId, "Source Mode: " + sourceMode}); err != nil {
			syslog.L.Error(err).WithField("jobId", jobId).Write()
		} else {
			// Update job status
			if op.mode == Disk {
				latestJob, err := op.storeInstance.Database.GetJob(op.job.ID)
				if err != nil {
					latestJob = op.job
				}

				latestJob.LastRunUpid = task.UPID
				latestJob.LastRunState = task.Status
				latestJob.LastRunEndtime = task.EndTime

				err = op.storeInstance.Database.UpdateJob(nil, latestJob)
				if err != nil {
					syslog.L.Error(err).WithField("jobId", latestJob.ID).WithField("upid", task.UPID).Write()
				}

				if err := system.SetRetrySchedule(op.job.ID, string(Disk), op.job.Retry, op.job.RetryInterval, op.extraExclusions); err != nil {
					syslog.L.Error(err).WithField("jobId", op.job.ID).Write()
				}
			} else if op.mode == Database {
				latestJob, err := op.storeInstance.Database.GetDatabaseJob(op.dbJob.ID)
				if err != nil {
					latestJob = op.dbJob
				}

				latestJob.LastRunUpid = task.UPID
				latestJob.LastRunState = task.Status
				latestJob.LastRunEndtime = task.EndTime

				err = op.storeInstance.Database.UpdateDatabaseJob(nil, latestJob)
				if err != nil {
					syslog.L.Error(err).WithField("jobId", latestJob.ID).WithField("upid", task.UPID).Write()
				}

				if err := system.SetRetrySchedule(op.dbJob.ID, string(Database), op.dbJob.Retry, op.dbJob.RetryInterval, op.extraExclusions); err != nil {
					syslog.L.Error(err).WithField("jobId", op.job.ID).Write()
				}
			}
		}
	}
}

// Close waits for the worker to finish and closes the job queue.
func (jq *Manager) Close() {
	jq.mu.Lock()
	defer jq.mu.Unlock()

	jq.locks.Clear()
	jq.cancel()
}
