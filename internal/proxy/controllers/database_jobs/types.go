//go:build linux

package database_jobs

import (
	"github.com/pbs-plus/pbs-plus/internal/store/types"
)

type JobsResponse struct {
	Data   []types.DatabaseJob `json:"data"`
	Digest string              `json:"digest"`
}

type JobConfigResponse struct {
	Errors  map[string]string `json:"errors"`
	Message string            `json:"message"`
	Data    types.DatabaseJob `json:"data"`
	Status  int               `json:"status"`
	Success bool              `json:"success"`
}

type JobRunResponse struct {
	Errors  map[string]string `json:"errors"`
	Message string            `json:"message"`
	Data    string            `json:"data"`
	Status  int               `json:"status"`
	Success bool              `json:"success"`
}
