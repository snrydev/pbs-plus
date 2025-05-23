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
)

func generateTimer(id string, mode string, schedule string) error {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return fmt.Errorf("generateTimer: invalid id -> %s", id)
	}

	content := fmt.Sprintf(`[Unit]
Description=%s Backup Job Timer

[Timer]
OnCalendar=%s
Persistent=false

[Install]
WantedBy=timers.target`, id, schedule)

	svcId := strings.ReplaceAll(id, " ", "-")
	if mode == "database" {
		svcId = "db-" + svcId
	}

	filePath := fmt.Sprintf("pbs-plus-job-%s.timer", svcId)
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

func generateService(id string, mode string) error {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return fmt.Errorf("generateService: invalid id -> %s", id)
	}

	content := fmt.Sprintf(`[Unit]
Description=%s Backup Job Service
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/bin/pbs-plus -job="%s" --job-mode="%s"`, id, id, mode)

	svcId := strings.ReplaceAll(id, " ", "-")
	if mode == "database" {
		svcId = "db-" + svcId
	}

	filePath := fmt.Sprintf("pbs-plus-job-%s.service", svcId)
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

func DeleteSchedule(id string, mode string) error {
	svcId := strings.ReplaceAll(id, " ", "-")
	if mode == "database" {
		svcId = "db-" + svcId
	}

	svcFilePath := fmt.Sprintf("pbs-plus-job-%s.service", svcId)
	svcFullPath := filepath.Join(constants.TimerBasePath, svcFilePath)

	timerFilePath := fmt.Sprintf("pbs-plus-job-%s.timer", svcId)
	timerFullPath := filepath.Join(constants.TimerBasePath, timerFilePath)

	cmd := exec.Command("/usr/bin/systemctl", "stop", timerFilePath)
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

	cmd = exec.Command("/usr/bin/systemctl", "daemon-reload")
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

func GetNextSchedule(id string, mode string) (*time.Time, error) {
	var output []byte

	lastSchedMux.Lock()
	if !lastSchedUpdate.IsZero() && time.Now().Sub(lastSchedUpdate) <= 5*time.Second {
		output = lastSchedString
	} else {
		cmd := exec.Command("systemctl", "list-timers", "--all")
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

	svcId := strings.ReplaceAll(id, " ", "-")
	if mode == "database" {
		svcId = "db-" + svcId
	}

	// Look for both the primary timer and any retry timer entries.
	primaryTimer := fmt.Sprintf("pbs-plus-job-%s.timer", svcId)
	retryPrefix := fmt.Sprintf("pbs-plus-job-%s-retry", svcId)

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

func SetSchedule(id string, mode string, schedule string) error {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return fmt.Errorf("SetSchedule: invalid id -> %s", id)
	}

	svcId := strings.ReplaceAll(id, " ", "-")
	if mode == "database" {
		svcId = "db-" + svcId
	}

	svcPath := fmt.Sprintf("pbs-plus-job-%s.service", svcId)
	fullSvcPath := filepath.Join(constants.TimerBasePath, svcPath)

	timerPath := fmt.Sprintf("pbs-plus-job-%s.timer", svcId)
	fullTimerPath := filepath.Join(constants.TimerBasePath, timerPath)

	if schedule == "" {
		cmd := exec.Command("/usr/bin/systemctl", "disable", "--now", timerPath)
		cmd.Env = os.Environ()
		_ = cmd.Run()

		_ = os.RemoveAll(fullSvcPath)
		_ = os.RemoveAll(fullTimerPath)
	} else {
		err := generateService(id, mode)
		if err != nil {
			return fmt.Errorf("SetSchedule: error generating service -> %w", err)
		}

		err = generateTimer(id, mode, schedule)
		if err != nil {
			return fmt.Errorf("SetSchedule: error generating timer -> %v", err)
		}
	}

	cmd := exec.Command("/usr/bin/systemctl", "daemon-reload")
	cmd.Env = os.Environ()
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("SetSchedule: error running daemon reload -> %v", err)
	}

	if schedule == "" {
		return nil
	}

	cmd = exec.Command("/usr/bin/systemctl", "enable", "--now", timerPath)
	cmd.Env = os.Environ()
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("SetSchedule: error running enable -> %v", err)
	}

	return nil
}
