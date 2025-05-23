//go:build linux

package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/store/constants"
)

func generateRetryTimer(id string, mode string, schedule string, attempt int) error {
	if strings.Contains(id, "/") ||
		strings.Contains(id, "\\") ||
		strings.Contains(id, "..") {
		return fmt.Errorf("generateRetryTimer: invalid job ID -> %s", id)
	}

	content := fmt.Sprintf(`[Unit]
Description=%s Backup Job Retry Timer (Attempt %d)

[Timer]
OnCalendar=%s
Persistent=false

[Install]
WantedBy=timers.target`, id, attempt, schedule)

	svcId := strings.ReplaceAll(id, " ", "-")
	if mode == "database" {
		svcId = "db-" + svcId
	}

	fileName := fmt.Sprintf("pbs-plus-job-%s-retry-%d.timer", svcId, attempt)
	fullPath := filepath.Join(constants.TimerBasePath, fileName)

	file, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("generateRetryTimer: error opening timer file -> %w", err)
	}
	defer file.Close()

	if _, err = file.WriteString(content); err != nil {
		return fmt.Errorf("generateRetryTimer: error writing content -> %w", err)
	}

	return nil
}

func generateRetryService(id string, mode string, attempt int, extraExclusions []string) error {
	if strings.Contains(id, "/") ||
		strings.Contains(id, "\\") ||
		strings.Contains(id, "..") {
		return fmt.Errorf("generateRetryService: invalid job ID -> %s", id)
	}

	execStartCmd := fmt.Sprintf(`/usr/bin/pbs-plus -job="%s" --job-mode="%s" -retry="%d"`, id, mode, attempt)

	for _, exclusion := range extraExclusions {
		if strings.Contains(exclusion, `"`) {
			continue
		}
		execStartCmd += fmt.Sprintf(` -skip="%s"`, exclusion)
	}

	content := fmt.Sprintf(`[Unit]
Description=%s Backup Job Retry Service (Attempt %d)
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
LimitNOFILE=1048576
ExecStart=%s`, id, attempt, execStartCmd)

	svcId := strings.ReplaceAll(id, " ", "-")
	if mode == "database" {
		svcId = "db-" + svcId
	}

	fileName := fmt.Sprintf("pbs-plus-job-%s-retry-%d.service", svcId, attempt)
	fullPath := filepath.Join(constants.TimerBasePath, fileName)

	file, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("generateRetryService: error opening service file -> %w", err)
	}
	defer file.Close()

	if _, err = file.WriteString(content); err != nil {
		return fmt.Errorf("generateRetryService: error writing content -> %w", err)
	}

	return nil
}

func RemoveAllRetrySchedules(id string, mode string) {
	svcId := strings.ReplaceAll(id, " ", "-")
	if mode == "database" {
		svcId = "db-" + svcId
	}

	retryPattern := filepath.Join(
		constants.TimerBasePath,
		fmt.Sprintf("pbs-plus-job-%s-retry-*.timer", svcId),
	)
	retryFiles, err := filepath.Glob(retryPattern)
	if err == nil {
		for _, file := range retryFiles {
			cmd := exec.Command("/usr/bin/systemctl", "disable", "--now", file)
			cmd.Env = os.Environ()
			_ = cmd.Run()
			_ = os.Remove(file)
		}
	}

	svcRetryPattern := filepath.Join(
		constants.TimerBasePath,
		fmt.Sprintf("pbs-plus-job-%s-retry-*.service", svcId),
	)
	svcRetryFiles, err := filepath.Glob(svcRetryPattern)
	if err == nil {
		for _, svcFile := range svcRetryFiles {
			_ = os.Remove(svcFile)
		}
	}

	cmd := exec.Command("/usr/bin/systemctl", "daemon-reload")
	cmd.Env = os.Environ()
	_ = cmd.Run()
}

func SetRetrySchedule(id string, mode string, maxRetry int, interval int, extraExclusions []string) error {
	svcId := strings.ReplaceAll(id, " ", "-")
	if mode == "database" {
		svcId = "db-" + svcId
	}

	retryPattern := filepath.Join(
		constants.TimerBasePath,
		fmt.Sprintf("pbs-plus-job-%s-retry-*.timer", svcId),
	)
	retryFiles, err := filepath.Glob(retryPattern)
	if err != nil {
		return fmt.Errorf("SetRetrySchedule: error globbing retry timer files: %w", err)
	}

	// Determine the current highest attempt number.
	currentAttempt := 0
	for _, file := range retryFiles {
		base := filepath.Base(file) // e.g. "pbs-plus-job-<jobID>-retry-1.timer"
		idx := strings.Index(base, "retry-")
		if idx < 0 {
			continue
		}
		attemptStrWithSuffix := base[idx+len("retry-"):]
		attemptStr := strings.TrimSuffix(attemptStrWithSuffix, ".timer")
		if attemptInt, err := strconv.Atoi(attemptStr); err == nil {
			if attemptInt > currentAttempt {
				currentAttempt = attemptInt
			}
		}
	}
	newAttempt := currentAttempt + 1
	if newAttempt > maxRetry {
		fmt.Printf("Job %s reached max retry count (%d). No further retry scheduled.\n",
			id, maxRetry)
		RemoveAllRetrySchedules(id, mode)
		return nil
	}

	// Now remove all existing retry timer files so that the new one is unique.
	for _, file := range retryFiles {
		cmd := exec.Command("/usr/bin/systemctl", "disable", "--now", file)
		cmd.Env = os.Environ()
		_ = cmd.Run()

		if err := os.Remove(file); err != nil {
			return fmt.Errorf("SetRetrySchedule: error removing old retry timer file %s: %w", file, err)
		}
	}

	// Also clear any existing retry service unit files.
	svcRetryPattern := filepath.Join(
		constants.TimerBasePath,
		fmt.Sprintf("pbs-plus-job-%s-retry-*.service", svcId),
	)
	svcRetryFiles, err := filepath.Glob(svcRetryPattern)
	if err != nil {
		return fmt.Errorf("SetRetrySchedule: error globbing retry service files: %w", err)
	}
	for _, svcFile := range svcRetryFiles {
		if err := os.Remove(svcFile); err != nil {
			return fmt.Errorf("SetRetrySchedule: error removing old retry service file %s: %w",
				svcFile, err)
		}
	}

	// Compute the new retry time
	retryTime := time.Now().Add(time.Duration(interval) * time.Minute)
	layout := "Mon 2006-01-02 15:04:05 MST"
	retrySchedule := retryTime.Format(layout)

	// Create the new retry service and timer unit files.
	if err := generateRetryService(id, mode, newAttempt, extraExclusions); err != nil {
		return fmt.Errorf("SetRetrySchedule: error generating retry service: %w", err)
	}
	if err := generateRetryTimer(id, mode, retrySchedule, newAttempt); err != nil {
		return fmt.Errorf("SetRetrySchedule: error generating retry timer: %w", err)
	}

	// Reload systemd daemon and enable the new retry timer.
	cmd := exec.Command("/usr/bin/systemctl", "daemon-reload")
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("SetRetrySchedule: error reloading daemon: %w", err)
	}

	timerFile := fmt.Sprintf("pbs-plus-job-%s-retry-%d.timer",
		svcId, newAttempt)
	cmd = exec.Command("/usr/bin/systemctl", "enable", "--now", timerFile)
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("SetRetrySchedule: error enabling retry timer: %w", err)
	}

	return nil
}
