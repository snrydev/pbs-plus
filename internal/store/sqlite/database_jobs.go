//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/store/constants"
	"github.com/pbs-plus/pbs-plus/internal/store/proxmox"
	"github.com/pbs-plus/pbs-plus/internal/store/system"
	"github.com/pbs-plus/pbs-plus/internal/store/types"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
	"github.com/pbs-plus/pbs-plus/internal/utils"
	_ "modernc.org/sqlite"
)

// generateUniqueDatabaseJobID produces a unique job id based on the job’s target.
func (database *Database) generateUniqueDatabaseJobID(job types.DatabaseJob) (string, error) {
	baseID := utils.Slugify(job.Target)
	if baseID == "" {
		return "", fmt.Errorf("invalid target: slugified value is empty")
	}

	for idx := 0; idx < maxAttempts; idx++ {
		var newID string
		if idx == 0 {
			newID = baseID
		} else {
			newID = fmt.Sprintf("%s-%d", baseID, idx)
		}
		var exists int
		err := database.readDb.
			QueryRow("SELECT 1 FROM database_jobs WHERE id = ? LIMIT 1", newID).
			Scan(&exists)

		if errors.Is(err, sql.ErrNoRows) {
			return newID, nil
		}
		if err != nil {
			return "", fmt.Errorf(
				"generateUniqueDatabaseJobID: error checking job existence: %w", err)
		}
	}
	return "", fmt.Errorf("failed to generate a unique job ID after %d attempts",
		maxAttempts)
}

// CreateDatabaseJob creates a new job record and adds any associated exclusions.
func (database *Database) CreateDatabaseJob(tx *sql.Tx, job types.DatabaseJob) (err error) {
	var commitNeeded bool = false
	if tx == nil {
		tx, err = database.writeDb.BeginTx(context.Background(), &sql.TxOptions{})
		if err != nil {
			return fmt.Errorf("CreateDatabaseJob: failed to begin transaction: %w", err)
		}
		defer func() {
			if p := recover(); p != nil {
				_ = tx.Rollback() // Rollback on panic
				panic(p)          // Re-panic after rollback
			} else if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("CreateDatabaseJob: failed to rollback transaction: %w", rbErr)).Write()
				}
			} else if commitNeeded {
				if cErr := tx.Commit(); cErr != nil {
					err = fmt.Errorf("CreateDatabaseJob: failed to commit transaction: %w", cErr) // Assign commit error back
					syslog.L.Error(err).Write()
				}
			} else {
				// Rollback if commit isn't explicitly needed (e.g., early return without error)
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("CreateDatabaseJob: failed to rollback transaction: %w", rbErr)).Write()
				}
			}
		}()
	}

	if job.ID == "" {
		id, err := database.generateUniqueDatabaseJobID(job)
		if err != nil {
			return fmt.Errorf("CreateDatabaseJob: failed to generate unique id -> %w", err)
		}
		job.ID = id
	}

	if job.Target == "" {
		return errors.New("target is empty")
	}
	if job.Store == "" {
		return errors.New("datastore is empty")
	}
	if !utils.IsValidID(job.ID) && job.ID != "" {
		return fmt.Errorf("CreateDatabaseJob: invalid id string -> %s", job.ID)
	}
	if !utils.IsValidNamespace(job.Namespace) && job.Namespace != "" {
		return fmt.Errorf("invalid namespace string: %s", job.Namespace)
	}
	if err := utils.ValidateOnCalendar(job.Schedule); err != nil && job.Schedule != "" {
		return fmt.Errorf("invalid schedule string: %s", job.Schedule)
	}
	if job.RetryInterval <= 0 {
		job.RetryInterval = 1
	}
	if job.Retry < 0 {
		job.Retry = 0
	}

	_, err = tx.Exec(`
        INSERT INTO database_jobs (
            id, store, target, schedule, comment,
            notification_mode, namespace, current_pid, last_run_upid, last_successful_upid, retry,
            retry_interval
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, job.ID, job.Store, job.Target,
		job.Schedule, job.Comment, job.NotificationMode, job.Namespace, job.CurrentPID,
		job.LastRunUpid, job.LastSuccessfulUpid, job.Retry, job.RetryInterval)
	if err != nil {
		return fmt.Errorf("CreateDatabaseJob: error inserting job: %w", err)
	}

	if err = system.SetSchedule("db-"+job.ID, job.Schedule); err != nil {
		syslog.L.Error(fmt.Errorf("CreateDatabaseJob: failed to set schedule: %w", err)).
			WithField("id", job.ID).
			Write()
		// Decide if schedule setting failure should rollback the DB transaction
		// return fmt.Errorf("CreateDatabaseJob: failed to set schedule: %w", err) // Uncomment to rollback
	}

	commitNeeded = true
	return nil
}

// GetDatabaseJob retrieves a job by id and assembles its exclusions.
func (database *Database) GetDatabaseJob(id string) (types.DatabaseJob, error) {
	query := `
        SELECT
            j.id, j.store, j.target, j.schedule, j.comment,
            j.notification_mode, j.namespace, j.current_pid, j.last_run_upid, j.last_successful_upid,
            j.retry, j.retry_interval
        FROM database_jobs j
        LEFT JOIN database_targets t ON j.target = t.name
        WHERE j.id = ?
    `
	rows, err := database.readDb.Query(query, id)
	if err != nil {
		return types.DatabaseJob{}, fmt.Errorf("GetDatabaseJob: error querying job data: %w", err)
	}
	defer rows.Close()

	var job types.DatabaseJob
	var found bool = false

	for rows.Next() {
		found = true
		err := rows.Scan(
			&job.ID, &job.Store,
			&job.Target, &job.Schedule, &job.Comment,
			&job.NotificationMode, &job.Namespace, &job.CurrentPID, &job.LastRunUpid,
			&job.LastSuccessfulUpid, &job.Retry, &job.RetryInterval,
		)
		if err != nil {
			syslog.L.Error(fmt.Errorf("GetDatabaseJob: error scanning job data: %w", err)).
				WithField("id", id).
				Write()
			return types.DatabaseJob{}, fmt.Errorf("GetDatabaseJob: error scanning job data for id %s: %w", id, err)
		}
	}

	if err = rows.Err(); err != nil {
		return types.DatabaseJob{}, fmt.Errorf("GetDatabaseJob: error iterating job results: %w", err)
	}

	if !found {
		return types.DatabaseJob{}, sql.ErrNoRows
	}

	return job, nil
}

// populateDatabaseJobExtras fills in details not directly from the database tables.
func (database *Database) populateDatabaseJobExtras(job *types.DatabaseJob) {
	if job.LastRunUpid != "" {
		task, err := proxmox.Session.GetTaskByUPID(job.LastRunUpid)
		if err == nil {
			job.LastRunEndtime = task.EndTime
			if task.Status == "stopped" {
				job.LastRunState = task.ExitStatus
				job.Duration = task.EndTime - task.StartTime
			} else if task.StartTime > 0 {
				job.Duration = time.Now().Unix() - task.StartTime
			}
		}
	}
	if job.LastSuccessfulUpid != "" {
		if successTask, err := proxmox.Session.GetTaskByUPID(job.LastSuccessfulUpid); err == nil {
			job.LastSuccessfulEndtime = successTask.EndTime
		}
	}

	if nextSchedule, err := system.GetNextSchedule("db-" + (*job).ID); err == nil && nextSchedule != nil {
		job.NextRun = nextSchedule.Unix()
	}
}

// UpdateDatabaseJob updates an existing job and its exclusions.
func (database *Database) UpdateDatabaseJob(tx *sql.Tx, job types.DatabaseJob) (err error) {
	var commitNeeded bool = false
	if tx == nil {
		tx, err = database.writeDb.BeginTx(context.Background(), &sql.TxOptions{})
		if err != nil {
			return fmt.Errorf("UpdateDatabaseJob: failed to begin transaction: %w", err)
		}
		defer func() {
			if p := recover(); p != nil {
				_ = tx.Rollback()
				panic(p)
			} else if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("UpdateDatabaseJob: failed to rollback transaction: %w", rbErr)).Write()
				}
			} else if commitNeeded {
				if cErr := tx.Commit(); cErr != nil {
					err = fmt.Errorf("UpdateDatabaseJob: failed to commit transaction: %w", cErr)
					syslog.L.Error(err).Write()
				}
			} else {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("UpdateDatabaseJob: failed to rollback transaction: %w", rbErr)).Write()
				}
			}
		}()
	}

	if !utils.IsValidID(job.ID) && job.ID != "" {
		return fmt.Errorf("UpdateDatabaseJob: invalid id string -> %s", job.ID)
	}
	if job.Target == "" {
		return errors.New("target is empty")
	}
	if job.Store == "" {
		return errors.New("datastore is empty")
	}
	if job.RetryInterval <= 0 {
		job.RetryInterval = 1
	}
	if job.Retry < 0 {
		job.Retry = 0
	}
	if !utils.IsValidNamespace(job.Namespace) && job.Namespace != "" {
		return fmt.Errorf("invalid namespace string: %s", job.Namespace)
	}
	if err := utils.ValidateOnCalendar(job.Schedule); err != nil && job.Schedule != "" {
		return fmt.Errorf("invalid schedule string: %s", job.Schedule)
	}

	_, err = tx.Exec(`
        UPDATE database_jobs SET store = ?, target = ?,
            schedule = ?, comment = ?, notification_mode = ?,
            namespace = ?, current_pid = ?, last_run_upid = ?, retry = ?,
            retry_interval = ?, last_successful_upid = ?
        WHERE id = ?
    `, job.Store, job.Target,
		job.Schedule, job.Comment, job.NotificationMode, job.Namespace,
		job.CurrentPID, job.LastRunUpid, job.Retry, job.RetryInterval,
		job.LastSuccessfulUpid, job.ID)
	if err != nil {
		return fmt.Errorf("UpdateDatabaseJob: error updating job: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM exclusions WHERE job_id = ?`, job.ID)
	if err != nil {
		return fmt.Errorf("UpdateDatabaseJob: error removing old exclusions for job %s: %w", job.ID, err)
	}

	if err = system.SetSchedule("db-"+job.ID, job.Schedule); err != nil {
		syslog.L.Error(fmt.Errorf("UpdateDatabaseJob: failed to set schedule: %w", err)).
			WithField("id", job.ID).
			Write()
		// Decide if schedule setting failure should rollback the DB transaction
		// return fmt.Errorf("UpdateDatabaseJob: failed to set schedule: %w", err) // Uncomment to rollback
	}

	if job.LastRunUpid != "" {
		go database.linkDatabaseJobLog(job.ID, job.LastRunUpid)
	}

	commitNeeded = true
	return nil
}

// linkDatabaseJobLog handles the asynchronous log linking.
func (database *Database) linkDatabaseJobLog(jobID, upid string) {
	jobLogsPath := filepath.Join(constants.DatabaseJobLogsBasePath, jobID)
	if err := os.MkdirAll(jobLogsPath, 0755); err != nil {
		syslog.L.Error(fmt.Errorf("linkDatabaseJobLog: failed to create log dir: %w", err)).
			WithField("id", jobID).
			Write()
		return
	}

	jobLogPath := filepath.Join(jobLogsPath, upid)
	if _, err := os.Lstat(jobLogPath); err != nil && !os.IsNotExist(err) {
		syslog.L.Error(fmt.Errorf("linkDatabaseJobLog: failed to stat potential symlink: %w", err)).
			WithField("path", jobLogPath).
			Write()
		return
	}

	origLogPath, err := proxmox.GetLogPath(upid)
	if err != nil {
		syslog.L.Error(fmt.Errorf("linkDatabaseJobLog: failed to get original log path: %w", err)).
			WithField("id", jobID).
			WithField("upid", upid).
			Write()
		return
	}

	if _, err := os.Stat(origLogPath); err != nil {
		syslog.L.Error(fmt.Errorf("linkDatabaseJobLog: original log path does not exist or error stating: %w", err)).
			WithField("orig_path", origLogPath).
			WithField("id", jobID).
			Write()
		return
	}

	err = os.Symlink(origLogPath, jobLogPath)
	if err != nil {
		syslog.L.Error(fmt.Errorf("linkDatabaseJobLog: failed to create symlink: %w", err)).
			WithField("id", jobID).
			WithField("source", origLogPath).
			WithField("link", jobLogPath).
			Write()
	}
}

// GetAllDatabaseJobs returns all job records.
func (database *Database) GetAllDatabaseJobs() ([]types.DatabaseJob, error) {
	query := `
        SELECT
            j.id, j.store, j.target, j.schedule, j.comment,
            j.notification_mode, j.namespace, j.current_pid, j.last_run_upid, j.last_successful_upid,
            j.retry, j.retry_interval
        FROM database_jobs j
        LEFT JOIN database_targets t ON j.target = t.name
        ORDER BY j.id
    `
	rows, err := database.readDb.Query(query)
	if err != nil {
		return nil, fmt.Errorf("GetAllDatabaseJobs: error querying jobs: %w", err)
	}
	defer rows.Close()

	jobsMap := make(map[string]*types.DatabaseJob)
	var jobOrder []string

	for rows.Next() {
		var jobID, store, target, schedule, comment, notificationMode, namespace, lastRunUpid, lastSuccessfulUpid string
		var retry int
		var retryInterval int
		var currentPID int

		err := rows.Scan(
			&jobID, &store, &target, &schedule, &comment,
			&notificationMode, &namespace, &currentPID, &lastRunUpid, &lastSuccessfulUpid,
			&retry, &retryInterval,
		)
		if err != nil {
			syslog.L.Error(fmt.Errorf("GetAllDatabaseJobs: error scanning row: %w", err)).Write()
			continue
		}

		job, exists := jobsMap[jobID]
		if !exists {
			job = &types.DatabaseJob{
				ID:                 jobID,
				Store:              store,
				Target:             target,
				Schedule:           schedule,
				Comment:            comment,
				NotificationMode:   notificationMode,
				Namespace:          namespace,
				CurrentPID:         currentPID,
				LastRunUpid:        lastRunUpid,
				LastSuccessfulUpid: lastSuccessfulUpid,
				Retry:              retry,
				RetryInterval:      retryInterval,
			}
			jobsMap[jobID] = job
			jobOrder = append(jobOrder, jobID)
			database.populateDatabaseJobExtras(job) // Populate non-SQL extras once per job
		}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetAllDatabaseJobs: error iterating job results: %w", err)
	}

	jobs := make([]types.DatabaseJob, len(jobOrder))
	for i, jobID := range jobOrder {
		job := jobsMap[jobID]
		jobs[i] = *job
	}

	return jobs, nil
}

func (database *Database) GetAllQueuedDatabaseJobs() ([]types.DatabaseJob, error) {
	query := `
        SELECT
            j.id, j.store, j.target, j.schedule, j.comment,
            j.notification_mode, j.namespace, j.current_pid, j.last_run_upid, j.last_successful_upid,
            j.retry, j.retry_interval, t.host
        FROM database_jobs j
        LEFT JOIN database_targets t ON j.target = t.name
				WHERE j.last_run_upid LIKE "%pbsplusgen-queue%"
        ORDER BY j.id
    `
	rows, err := database.readDb.Query(query)
	if err != nil {
		return nil, fmt.Errorf("GetAllQueuedDatabaseJobs: error querying jobs: %w", err)
	}
	defer rows.Close()

	jobsMap := make(map[string]*types.DatabaseJob)
	var jobOrder []string

	for rows.Next() {
		var jobID, store, target, schedule, comment, notificationMode, namespace, lastRunUpid, lastSuccessfulUpid string
		var retry int
		var retryInterval int
		var currentPID int
		var targetHost string

		err := rows.Scan(
			&jobID, &store, &target, &schedule, &comment,
			&notificationMode, &namespace, &currentPID, &lastRunUpid, &lastSuccessfulUpid,
			&retry, &retryInterval, &targetHost,
		)
		if err != nil {
			syslog.L.Error(fmt.Errorf("GetAllQueuedDatabaseJobs: error scanning row: %w", err)).Write()
			continue
		}

		job, exists := jobsMap[jobID]
		if !exists {
			job = &types.DatabaseJob{
				ID:                 jobID,
				Store:              store,
				Target:             target,
				Schedule:           schedule,
				Comment:            comment,
				NotificationMode:   notificationMode,
				Namespace:          namespace,
				CurrentPID:         currentPID,
				LastRunUpid:        lastRunUpid,
				LastSuccessfulUpid: lastSuccessfulUpid,
				Retry:              retry,
				RetryInterval:      retryInterval,
				TargetHost:         targetHost,
			}
			jobsMap[jobID] = job
			jobOrder = append(jobOrder, jobID)
			database.populateDatabaseJobExtras(job) // Populate non-SQL extras once per job
		}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetAllQueuedDatabaseJobs: error iterating job results: %w", err)
	}

	jobs := make([]types.DatabaseJob, len(jobOrder))
	for i, jobID := range jobOrder {
		job := jobsMap[jobID]
		jobs[i] = *job
	}

	return jobs, nil
}

// DeleteDatabaseJob deletes a job and any related exclusions.
func (database *Database) DeleteDatabaseJob(tx *sql.Tx, id string) (err error) {
	var commitNeeded bool = false
	if tx == nil {
		tx, err = database.writeDb.BeginTx(context.Background(), &sql.TxOptions{})
		if err != nil {
			return fmt.Errorf("DeleteDatabaseJob: failed to begin transaction: %w", err)
		}
		defer func() {
			if p := recover(); p != nil {
				_ = tx.Rollback()
				panic(p)
			} else if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("DeleteDatabaseJob: failed to rollback transaction: %w", rbErr)).Write()
				}
			} else if commitNeeded {
				if cErr := tx.Commit(); cErr != nil {
					err = fmt.Errorf("DeleteDatabaseJob: failed to commit transaction: %w", cErr)
					syslog.L.Error(err).Write()
				}
			} else {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("DeleteDatabaseJob: failed to rollback transaction: %w", rbErr)).Write()
				}
			}
		}()
	}

	// Delete the job itself
	res, err := tx.Exec("DELETE FROM database_jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("DeleteDatabaseJob: error deleting job %s: %w", id, err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	// Filesystem and system operations outside transaction
	jobLogsPath := filepath.Join(constants.DatabaseJobLogsBasePath, id)
	if err := os.RemoveAll(jobLogsPath); err != nil {
		if !os.IsNotExist(err) {
			syslog.L.Error(fmt.Errorf("DeleteDatabaseJob: failed removing job logs: %w", err)).
				WithField("id", id).
				Write()
			// Decide if this failure should prevent commit (if tx was passed in)
			// or just be logged. Currently just logged.
		}
	}

	if err := system.DeleteSchedule(id); err != nil {
		syslog.L.Error(fmt.Errorf("DeleteDatabaseJob: failed deleting schedule: %w", err)).
			WithField("id", id).
			Write()
		// Decide if this failure should prevent commit or just be logged.
	}

	commitNeeded = true
	return nil
}
