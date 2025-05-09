//go:build linux

package database_targets

import (
	"github.com/pbs-plus/pbs-plus/internal/store/types"
)

type TargetsResponse struct {
	Data   []types.DatabaseTarget `json:"data"`
	Digest string                 `json:"digest"`
}

type TargetConfigResponse struct {
	Errors  map[string]string    `json:"errors"`
	Message string               `json:"message"`
	Data    types.DatabaseTarget `json:"data"`
	Status  int                  `json:"status"`
	Success bool                 `json:"success"`
}
