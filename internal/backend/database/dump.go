package database

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/store/types"
)

// checkBinaryExists returns the first found binary from the list, or error if none found.
func checkBinaryExists(binaries ...string) (string, error) {
	for _, bin := range binaries {
		path, err := exec.LookPath(bin)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("none of the required binaries found: %s", strings.Join(binaries, ", "))
}

func generateDumpFilename(dbType types.DBType, dbName string) string {
	timestamp := time.Now().Format("20060102_150405")
	name := dbName
	if name == "" || name == "*" {
		name = "all"
	}
	// Remove any path separators from dbName for safety
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	return fmt.Sprintf("%s_%s_%s.sql", dbType, name, timestamp)
}

func DumpDatabase(cfg types.DatabaseTarget) (string, error) {
	// Create a random temp directory
	tmpDir, err := os.MkdirTemp("", "dbdumpdir-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Generate the filename
	filename := generateDumpFilename(cfg.Type, cfg.DatabaseName)
	dumpPath := filepath.Join(tmpDir, filename)

	// Create the file
	tmpFile, err := os.Create(dumpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create dump file: %w", err)
	}
	defer tmpFile.Close()

	var cmd *exec.Cmd
	var bin string

	switch cfg.Type {
	case types.MySQL:
		bin, err = checkBinaryExists("mysqldump")
		if err != nil {
			return "", fmt.Errorf("mysqldump not found: %w", err)
		}

		if cfg.Port == 0 {
			cfg.Port = 3306
		}

		args := []string{
			"-h", cfg.Host,
			"-P", strconv.Itoa(cfg.Port),
			"-u", cfg.Username,
		}
		if cfg.DatabaseName != "" && cfg.DatabaseName != "*" {
			args = append(args, cfg.DatabaseName)
		} else {
			args = append(args, "--all-databases")
		}
		cmd = exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), fmt.Sprintf("MYSQL_PWD=%s", cfg.Password))

	case types.MariaDB:
		// Try mariadb-dump first, then mysqldump
		bin, err = checkBinaryExists("mariadb-dump", "mysqldump")
		if err != nil {
			return "", fmt.Errorf("mariadb-dump or mysqldump not found: %w", err)
		}

		if cfg.Port == 0 {
			cfg.Port = 3306
		}

		args := []string{
			"-h", cfg.Host,
			"-P", strconv.Itoa(cfg.Port),
			"-u", cfg.Username,
		}
		if cfg.DatabaseName != "" && cfg.DatabaseName != "*" {
			args = append(args, cfg.DatabaseName)
		} else {
			args = append(args, "--all-databases")
		}
		cmd = exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), fmt.Sprintf("MYSQL_PWD=%s", cfg.Password))

	case types.Postgres:
		// For all databases, use pg_dumpall; for one, use pg_dump
		if cfg.DatabaseName == "" || cfg.DatabaseName == "*" {
			bin, err = checkBinaryExists("pg_dumpall")
			if err != nil {
				return "", fmt.Errorf("pg_dumpall not found: %w", err)
			}

			if cfg.Port == 0 {
				cfg.Port = 5432
			}

			args := []string{
				"-h", cfg.Host,
				"-p", strconv.Itoa(cfg.Port),
				"-U", cfg.Username,
			}
			cmd = exec.Command(bin, args...)
		} else {
			bin, err = checkBinaryExists("pg_dump")
			if err != nil {
				return "", fmt.Errorf("pg_dump not found: %w", err)
			}

			if cfg.Port == 0 {
				cfg.Port = 5432
			}

			args := []string{
				"-h", cfg.Host,
				"-p", strconv.Itoa(cfg.Port),
				"-U", cfg.Username,
				"-d", cfg.DatabaseName,
			}
			cmd = exec.Command(bin, args...)
		}
		cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", cfg.Password))

	default:
		return "", errors.New("unsupported database type")
	}

	cmd.Stdout = tmpFile
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("dump command failed: %w", err)
	}

	return dumpPath, nil
}
