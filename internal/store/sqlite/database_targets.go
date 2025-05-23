//go:build linux

package sqlite

import (
	"context"
	"crypto/aes"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pbs-plus/pbs-plus/internal/store/system"
	"github.com/pbs-plus/pbs-plus/internal/store/types"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
	_ "modernc.org/sqlite"
)

// CreateDatabaseTarget inserts a new target or updates if it exists.
func (database *Database) CreateDatabaseTarget(tx *sql.Tx, target types.DatabaseTarget) (err error) {
	var commitNeeded bool = false
	if tx == nil {
		tx, err = database.writeDb.BeginTx(context.Background(), &sql.TxOptions{})
		if err != nil {
			return fmt.Errorf("CreateDatabaseTarget: failed to begin transaction: %w", err)
		}
		defer func() {
			if p := recover(); p != nil {
				_ = tx.Rollback()
				panic(p)
			} else if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("CreateDatabaseTarget: failed to rollback transaction: %w", rbErr)).Write()
				}
			} else if commitNeeded {
				if cErr := tx.Commit(); cErr != nil {
					err = fmt.Errorf("CreateDatabaseTarget: failed to commit transaction: %w", cErr)
					syslog.L.Error(err).Write()
				}
			} else {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("CreateDatabaseTarget: failed to rollback transaction: %w", rbErr)).Write()
				}
			}
		}()
	}

	if target.Host == "" {
		return fmt.Errorf("target host empty")
	}
	if target.Type == "" {
		return fmt.Errorf("target type empty")
	}

	valid := true
	target.Type, valid = types.ParseDBType(string(target.Type))
	if !valid {
		return fmt.Errorf("target type invalid")
	}

	encPassword := ""

	encKey, err := system.GetDBKey()
	if err == nil {
		aes, err := aes.NewCipher([]byte(encKey))
		if err == nil {
			ciphertext := make([]byte, len(target.Password))
			aes.Encrypt(ciphertext, []byte(target.Password))

			encPassword = string(ciphertext)
		}
	}

	_, err = tx.Exec(`
        INSERT INTO database_targets (name, type, username, password, host, port, db_name)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `,
		target.Name, target.Type, target.Username, encPassword,
		target.Host, target.Port, target.DatabaseName,
	)
	if err != nil {
		// Use specific error check if possible, otherwise string contains is fallback
		// For modernc.org/sqlite, check for sqlite3.ErrConstraintUnique or similar
		if strings.Contains(err.Error(), "UNIQUE constraint failed") { // Consider more robust error checking
			err = database.UpdateDatabaseTarget(tx, target) // Use the existing transaction
			if err == nil {
				commitNeeded = true // Mark for commit if update succeeds
			}
			return err // Return the result of UpdateDatabaseTarget
		}
		return fmt.Errorf("CreateDatabaseTarget: error inserting target: %w", err)
	}

	commitNeeded = true
	return nil
}

// UpdateDatabaseTarget updates an existing target.
func (database *Database) UpdateDatabaseTarget(tx *sql.Tx, target types.DatabaseTarget) (err error) {
	var commitNeeded bool = false
	if tx == nil {
		tx, err = database.writeDb.BeginTx(context.Background(), &sql.TxOptions{})
		if err != nil {
			return fmt.Errorf("UpdateDatabaseTarget: failed to begin transaction: %w", err)
		}
		defer func() {
			if p := recover(); p != nil {
				_ = tx.Rollback()
				panic(p)
			} else if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("UpdateDatabaseTarget: failed to rollback transaction: %w", rbErr)).Write()
				}
			} else if commitNeeded {
				if cErr := tx.Commit(); cErr != nil {
					err = fmt.Errorf("UpdateDatabaseTarget: failed to commit transaction: %w", cErr)
					syslog.L.Error(err).Write()
				}
			} else {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("UpdateDatabaseTarget: failed to rollback transaction: %w", rbErr)).Write()
				}
			}
		}()
	}

	if target.Host == "" {
		return fmt.Errorf("target host empty")
	}
	if target.Type == "" {
		return fmt.Errorf("target type empty")
	}
	valid := true
	target.Type, valid = types.ParseDBType(string(target.Type))
	if !valid {
		return fmt.Errorf("target type invalid")
	}

	_, err = tx.Exec(`
        UPDATE database_targets SET
					type = ?, username = ?, host = ?,
					port = ?, db_name	= ?
        WHERE name = ?
    `,
		target.Type, target.Username,
		target.Host, target.Port, target.DatabaseName,
		target.Name,
	)
	if err != nil {
		return fmt.Errorf("UpdateDatabaseTarget: error updating target: %w", err)
	}

	commitNeeded = true
	return nil
}

func (database *Database) UpdateDatabaseTargetPassword(tx *sql.Tx, target types.DatabaseTarget) (err error) {
	var commitNeeded bool = false
	if tx == nil {
		tx, err = database.writeDb.BeginTx(context.Background(), &sql.TxOptions{})
		if err != nil {
			return fmt.Errorf("UpdateDatabaseTarget: failed to begin transaction: %w", err)
		}
		defer func() {
			if p := recover(); p != nil {
				_ = tx.Rollback()
				panic(p)
			} else if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("UpdateDatabaseTarget: failed to rollback transaction: %w", rbErr)).Write()
				}
			} else if commitNeeded {
				if cErr := tx.Commit(); cErr != nil {
					err = fmt.Errorf("UpdateDatabaseTarget: failed to commit transaction: %w", cErr)
					syslog.L.Error(err).Write()
				}
			} else {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("UpdateDatabaseTarget: failed to rollback transaction: %w", rbErr)).Write()
				}
			}
		}()
	}

	if target.Host == "" {
		return fmt.Errorf("target host empty")
	}
	if target.Type == "" {
		return fmt.Errorf("target type empty")
	}

	encPassword := ""

	encKey, err := system.GetDBKey()
	if err == nil {
		aes, err := aes.NewCipher([]byte(encKey))
		if err == nil {
			ciphertext := make([]byte, len(target.Password))
			aes.Encrypt(ciphertext, []byte(target.Password))

			encPassword = string(ciphertext)
		}
	}

	_, err = tx.Exec(`
        UPDATE database_targets SET
					password = ?
        WHERE name = ?
    `,
		encPassword, target.Name,
	)
	if err != nil {
		return fmt.Errorf("UpdateDatabaseTarget: error updating target: %w", err)
	}

	commitNeeded = true
	return nil
}

// DeleteDatabaseTarget removes a target.
func (database *Database) DeleteDatabaseTarget(tx *sql.Tx, name string) (err error) {
	var commitNeeded bool = false
	if tx == nil {
		tx, err = database.writeDb.BeginTx(context.Background(), &sql.TxOptions{})
		if err != nil {
			return fmt.Errorf("DeleteDatabaseTarget: failed to begin transaction: %w", err)
		}
		defer func() {
			if p := recover(); p != nil {
				_ = tx.Rollback()
				panic(p)
			} else if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("DeleteDatabaseTarget: failed to rollback transaction: %w", rbErr)).Write()
				}
			} else if commitNeeded {
				if cErr := tx.Commit(); cErr != nil {
					err = fmt.Errorf("DeleteDatabaseTarget: failed to commit transaction: %w", cErr)
					syslog.L.Error(err).Write()
				}
			} else {
				if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
					syslog.L.Error(fmt.Errorf("DeleteDatabaseTarget: failed to rollback transaction: %w", rbErr)).Write()
				}
			}
		}()
	}

	res, err := tx.Exec("DELETE FROM targets WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("DeleteDatabaseTarget: error deleting target: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		// Return sql.ErrNoRows if the target wasn't found
		return sql.ErrNoRows
	}

	commitNeeded = true
	return nil
}

// GetDatabaseTarget retrieves a target by name along with its associated job count.
func (database *Database) GetDatabaseTarget(name string) (types.DatabaseTarget, error) {
	row := database.readDb.QueryRow(`
        SELECT
            t.name, t.type, t.username, t.host, t.port, t.db_name
            COUNT(j.id) as job_count
        FROM database_targets t
        LEFT JOIN database_jobs j ON t.name = j.target
        WHERE t.name = ?
        GROUP BY t.name
    `, name) // Grouping by primary key (or unique key) is sufficient

	var target types.DatabaseTarget
	var jobCount int
	err := row.Scan(
		&target.Name, &target.Type, &target.Username, &target.Host,
		&target.Port, &target.DatabaseName,
		&jobCount,
	)
	if err != nil {
		// Don't wrap sql.ErrNoRows, return it directly
		if errors.Is(err, sql.ErrNoRows) {
			return types.DatabaseTarget{}, sql.ErrNoRows
		}
		return types.DatabaseTarget{}, fmt.Errorf("GetDatabaseTarget: error fetching target: %w", err)
	}

	target.JobCount = jobCount

	return target, nil
}

func (database *Database) GetDatabaseTargetCredentials(name string) (types.DatabaseTarget, error) {
	row := database.readDb.QueryRow(`
        SELECT
            t.name, t.username, t.password
        FROM database_targets t
        WHERE t.name = ?
        GROUP BY t.name
    `, name) // Grouping by primary key (or unique key) is sufficient

	var target types.DatabaseTarget
	err := row.Scan(
		&target.Name, &target.Username, &target.Password,
	)
	if err != nil {
		// Don't wrap sql.ErrNoRows, return it directly
		if errors.Is(err, sql.ErrNoRows) {
			return types.DatabaseTarget{}, sql.ErrNoRows
		}
		return types.DatabaseTarget{}, fmt.Errorf("GetDatabaseTarget: error fetching target: %w", err)
	}

	if target.Password != "" {
		encKey, err := system.GetDBKey()
		if err == nil {
			aes, err := aes.NewCipher([]byte(encKey))
			if err == nil {
				plain := make([]byte, len(target.Password))
				aes.Decrypt(plain, []byte(target.Password))

				target.Password = string(plain)
			}
		}
	}

	return target, nil
}

// GetAllDatabaseTargets returns all targets along with their associated job counts.
func (database *Database) GetAllDatabaseTargets() ([]types.DatabaseTarget, error) {
	rows, err := database.readDb.Query(`
        SELECT
            t.name, t.type, t.username, t.host, t.port, t.db_name
            COUNT(j.id) as job_count
        FROM database_targets t
        LEFT JOIN database_jobs j ON t.name = j.target
        GROUP BY t.name, t.type, t.username, t.host, t.port, t.db_name
        ORDER BY t.name
    `) // Group by all non-aggregated columns
	if err != nil {
		return nil, fmt.Errorf("GetAllDatabaseTargets: error querying targets: %w", err)
	}
	defer rows.Close()

	var targets []types.DatabaseTarget
	for rows.Next() {
		var target types.DatabaseTarget
		var jobCount int
		err := rows.Scan(
			&target.Name, &target.Type, &target.Username, &target.Host,
			&target.Port, &target.DatabaseName,
			&jobCount,
		)
		if err != nil {
			syslog.L.Error(fmt.Errorf("GetAllDatabaseTargets: error scanning target row: %w", err)).Write()
			continue
		}

		target.JobCount = jobCount

		targets = append(targets, target)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetAllDatabaseTargets: error iterating target rows: %w", err)
	}

	return targets, nil
}
