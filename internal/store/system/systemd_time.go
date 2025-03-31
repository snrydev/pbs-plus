//go:build linux

package system

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/store/constants"
	"github.com/pbs-plus/pbs-plus/internal/store/types"
)

func generateTimer(job types.Job) error {
	if strings.Contains(job.ID, "/") || strings.Contains(job.ID, "\\") || strings.Contains(job.ID, "..") {
		return fmt.Errorf("generateTimer: invalid job ID -> %s", job.ID)
	}

	content := fmt.Sprintf(`[Unit]
Description=%s Backup Job Timer

[Timer]
OnCalendar=%s
Persistent=false

[Install]
WantedBy=timers.target`, job.ID, job.Schedule)

	filePath := fmt.Sprintf("pbs-plus-job-%s.timer", strings.ReplaceAll(job.ID, " ", "-"))
	fullPath := filepath.Join(constants.TimerBasePath, filePath)

	file, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("generateTimer: error opening timer file -> %w", err)
	}
	defer file.Close()

	// Write the text to the file
	_, err = file.WriteString(content)
	if err != nil {
		return fmt.Errorf("generateTimer: error writing content to timer file -> %w", err)
	}

	return nil
}

func generateService(job types.Job) error {
	if strings.Contains(job.ID, "/") || strings.Contains(job.ID, "\\") || strings.Contains(job.ID, "..") {
		return fmt.Errorf("generateService: invalid job ID -> %s", job.ID)
	}

	content := fmt.Sprintf(`[Unit]
Description=%s Backup Job Service
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/bin/pbs-plus -job="%s"`, job.ID, job.ID)

	filePath := fmt.Sprintf("pbs-plus-job-%s.service", strings.ReplaceAll(job.ID, " ", "-"))
	fullPath := filepath.Join(constants.TimerBasePath, filePath)

	file, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("generateService: error opening service file -> %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		return fmt.Errorf("generateService: error writing content to service file -> %w", err)
	}

	return nil
}

func DeleteSchedule(id string) error {
	svcFilePath := fmt.Sprintf("pbs-plus-job-%s.service", strings.ReplaceAll(id, " ", "-"))
	svcFullPath := filepath.Join(constants.TimerBasePath, svcFilePath)

	timerFilePath := fmt.Sprintf("pbs-plus-job-%s.timer", strings.ReplaceAll(id, " ", "-"))
	timerFullPath := filepath.Join(constants.TimerBasePath, timerFilePath)

	cmd := exec.Command("/usr/bin/systemctl", "--user", "stop", timerFilePath)
	cmd.Env = os.Environ()
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("DeleteSchedule: error stopping timer -> %w", err)
	}

	err = os.RemoveAll(svcFullPath)
	if err != nil {
		return fmt.Errorf("DeleteSchedule: error deleting service -> %w", err)
	}

	err = os.RemoveAll(timerFullPath)
	if err != nil {
		return fmt.Errorf("DeleteSchedule: error deleting timer -> %w", err)
	}

	cmd = exec.Command("/usr/bin/systemctl", "--user", "daemon-reload")
	cmd.Env = os.Environ()
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("DeleteSchedule: error reloading daemon -> %w", err)
	}

	return nil
}

type TimerInfo struct {
	Next      time.Time
	Left      string
	Last      time.Time
	Passed    string
	Unit      string
	Activates string
}

var lastSchedMux sync.Mutex
var lastSchedUpdate time.Time
var lastSchedString []byte

func GetNextSchedule(job types.Job) (*time.Time, error) {
	var output []byte

	lastSchedMux.Lock()
	if !lastSchedUpdate.IsZero() && time.Now().Sub(lastSchedUpdate) <= 5*time.Second {
		output = lastSchedString
	} else {
		cmd := exec.Command("systemctl", "--user", "list-timers", "--all")
		cmd.Env = os.Environ()

		var err error
		output, err = cmd.Output()
		if err != nil {
			lastSchedMux.Unlock()
			return nil, fmt.Errorf("GetNextSchedule: error running systemctl command: %w", err)
		}

		lastSchedUpdate = time.Now()
		lastSchedString = output
	}
	lastSchedMux.Unlock()

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	layout := "Mon 2006-01-02 15:04:05 MST"

	// Look for both the primary timer and any retry timer entries.
	primaryTimer := fmt.Sprintf("pbs-plus-job-%s.timer", strings.ReplaceAll(job.ID, " ", "-"))
	retryPrefix := fmt.Sprintf("pbs-plus-job-%s-retry", strings.ReplaceAll(job.ID, " ", "-"))

	var nextTimes []time.Time
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, primaryTimer) ||
			strings.Contains(line, retryPrefix) {
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}

			nextStr := strings.Join(fields[0:4], " ")
			if strings.TrimSpace(nextStr) == "-" {
				continue
			}

			nextTime, err := time.Parse(layout, nextStr)
			if err != nil {
				return nil, fmt.Errorf("GetNextSchedule: error parsing time: %w", err)
			}

			nextTimes = append(nextTimes, nextTime)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("GetNextSchedule: error reading command output: %w", err)
	}

	if len(nextTimes) == 0 {
		return nil, nil
	}

	// Return the earliest of all the scheduled times.
	earliest := nextTimes[0]
	for _, t := range nextTimes {
		if t.Before(earliest) {
			earliest = t
		}
	}

	return &earliest, nil
}

func SetSchedule(job types.Job) error {
	if strings.Contains(job.ID, "/") || strings.Contains(job.ID, "\\") || strings.Contains(job.ID, "..") {
		return fmt.Errorf("SetSchedule: invalid job ID -> %s", job.ID)
	}

	svcPath := fmt.Sprintf("pbs-plus-job-%s.service", strings.ReplaceAll(job.ID, " ", "-"))
	fullSvcPath := filepath.Join(constants.TimerBasePath, svcPath)

	timerPath := fmt.Sprintf("pbs-plus-job-%s.timer", strings.ReplaceAll(job.ID, " ", "-"))
	fullTimerPath := filepath.Join(constants.TimerBasePath, timerPath)

	if job.Schedule == "" {
		cmd := exec.Command("/usr/bin/systemctl", "--user", "disable", "--now", timerPath)
		cmd.Env = os.Environ()
		_ = cmd.Run()

		_ = os.RemoveAll(fullSvcPath)
		_ = os.RemoveAll(fullTimerPath)
	} else {
		err := generateService(job)
		if err != nil {
			return fmt.Errorf("SetSchedule: error generating service -> %w", err)
		}

		err = generateTimer(job)
		if err != nil {
			return fmt.Errorf("SetSchedule: error generating timer -> %v", err)
		}
	}

	cmd := exec.Command("/usr/bin/systemctl", "--user", "daemon-reload")
	cmd.Env = os.Environ()
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("SetSchedule: error running daemon reload -> %v", err)
	}

	if job.Schedule == "" {
		return nil
	}

	cmd = exec.Command("/usr/bin/systemctl", "--user", "enable", "--now", timerPath)
	cmd.Env = os.Environ()
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("SetSchedule: error running enable -> %v", err)
	}

	return nil
}
