package constants

import (
	"os"
	"path/filepath"
)

const (
	ProxyTargetURL   = "https://127.0.0.1:8007"        // The target server URL
	ModifiedFilePath = "/js/proxmox-backup-gui.js"     // The specific JS file to modify
	CertFile         = "/etc/proxmox-backup/proxy.pem" // Path to generated SSL certificate
	KeyFile          = "/etc/proxmox-backup/proxy.key" // Path to generated private key
	DbBasePath       = "/var/lib/proxmox-backup"
	LogsBasePath     = "/var/log/proxmox-backup"
	TaskLogsBasePath = LogsBasePath + "/tasks"
	JobLogsBasePath  = "/var/log/pbs-plus"
)

func getTimerBasePath() string {
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	timerDir := filepath.Join(xdgRuntimeDir, "systemd", "user")

	_ = os.MkdirAll(timerDir, 0700)

	return timerDir
}

var TimerBasePath = getTimerBasePath()

func getMountSocketPath() string {
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	socketPath := filepath.Join(xdgRuntimeDir, "pbs_agent_mount.sock")

	return socketPath
}

var MountSocketPath = getMountSocketPath()
