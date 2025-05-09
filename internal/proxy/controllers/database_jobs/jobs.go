//go:build linux

package database_jobs

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strconv"

	"github.com/pbs-plus/pbs-plus/internal/backend/backup"
	"github.com/pbs-plus/pbs-plus/internal/proxy/controllers"
	"github.com/pbs-plus/pbs-plus/internal/store"
	"github.com/pbs-plus/pbs-plus/internal/store/system"
	"github.com/pbs-plus/pbs-plus/internal/store/types"
	"github.com/pbs-plus/pbs-plus/internal/utils"
)

func DBJobHandler(storeInstance *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Invalid HTTP method", http.StatusBadRequest)
			return
		}

		allJobs, err := storeInstance.Database.GetAllDatabaseJobs()
		if err != nil {
			controllers.WriteErrorResponse(w, err)
			return
		}

		digest, err := utils.CalculateDigest(allJobs)
		if err != nil {
			controllers.WriteErrorResponse(w, err)
			return
		}

		toReturn := JobsResponse{
			Data:   allJobs,
			Digest: digest,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(toReturn)
	}
}

func ExtJsJobRunHandler(storeInstance *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := JobRunResponse{}
		if r.Method != http.MethodPost {
			http.Error(w, "Invalid HTTP method", http.StatusBadRequest)
			return
		}

		job, err := storeInstance.Database.GetDatabaseJob(utils.DecodePath(r.PathValue("job")))
		if err != nil {
			controllers.WriteErrorResponse(w, err)
			return
		}

		system.RemoveAllRetrySchedules(job.ID, string(backup.Database))

		execPath, err := os.Executable()
		if err != nil {
			controllers.WriteErrorResponse(w, err)
			return
		}

		cmd := exec.Command(execPath, "-job", job.ID, "--job-mode", string(backup.Database), "-web")
		cmd.Env = os.Environ()
		err = cmd.Start()
		if err != nil {
			controllers.WriteErrorResponse(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		response.Status = http.StatusOK
		response.Success = true
		json.NewEncoder(w).Encode(response)
		return
	}
}

func ExtJsJobHandler(storeInstance *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := JobConfigResponse{}
		if r.Method != http.MethodPost {
			http.Error(w, "Invalid HTTP method", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		err := r.ParseForm()
		if err != nil {
			controllers.WriteErrorResponse(w, err)
			return
		}

		retry, err := strconv.Atoi(r.FormValue("retry"))
		if err != nil {
			if r.FormValue("retry") == "" {
				retry = 0
			} else {
				controllers.WriteErrorResponse(w, err)
				return
			}
		}

		retryInterval, err := strconv.Atoi(r.FormValue("retry-interval"))
		if err != nil {
			if r.FormValue("retry-interval") == "" {
				retryInterval = 1
			} else {
				controllers.WriteErrorResponse(w, err)
				return
			}
		}

		newJob := types.DatabaseJob{
			ID:               r.FormValue("id"),
			Store:            r.FormValue("store"),
			Target:           r.FormValue("target"),
			Schedule:         r.FormValue("schedule"),
			Comment:          r.FormValue("comment"),
			Namespace:        r.FormValue("ns"),
			NotificationMode: r.FormValue("notification-mode"),
			Retry:            retry,
			RetryInterval:    retryInterval,
		}

		err = storeInstance.Database.CreateDatabaseJob(nil, newJob)
		if err != nil {
			controllers.WriteErrorResponse(w, err)
			return
		}

		response.Status = http.StatusOK
		response.Success = true
		json.NewEncoder(w).Encode(response)
	}
}

func ExtJsJobSingleHandler(storeInstance *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := JobConfigResponse{}
		if r.Method != http.MethodPut && r.Method != http.MethodGet && r.Method != http.MethodDelete {
			http.Error(w, "Invalid HTTP method", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPut {
			job, err := storeInstance.Database.GetDatabaseJob(utils.DecodePath(r.PathValue("job")))
			if err != nil {
				controllers.WriteErrorResponse(w, err)
				return
			}

			err = r.ParseForm()
			if err != nil {
				controllers.WriteErrorResponse(w, err)
				return
			}

			if r.FormValue("store") != "" {
				job.Store = r.FormValue("store")
			}
			if r.FormValue("target") != "" {
				job.Target = r.FormValue("target")
			}
			if r.FormValue("schedule") != "" {
				job.Schedule = r.FormValue("schedule")
			}
			if r.FormValue("comment") != "" {
				job.Comment = r.FormValue("comment")
			}
			if r.FormValue("notification-mode") != "" {
				job.NotificationMode = r.FormValue("notification-mode")
			}

			retry, err := strconv.Atoi(r.FormValue("retry"))
			if err != nil {
				retry = 0
			}

			retryInterval, err := strconv.Atoi(r.FormValue("retry-interval"))
			if err != nil {
				retryInterval = 1
			}

			job.Retry = retry
			job.RetryInterval = retryInterval
			job.Namespace = r.FormValue("ns")

			if delArr, ok := r.Form["delete"]; ok {
				for _, attr := range delArr {
					switch attr {
					case "store":
						job.Store = ""
					case "target":
						job.Target = ""
					case "schedule":
						job.Schedule = ""
					case "comment":
						job.Comment = ""
					case "ns":
						job.Namespace = ""
					case "retry":
						job.Retry = 0
					case "retry-interval":
						job.RetryInterval = 1
					case "notification-mode":
						job.NotificationMode = ""
					}
				}
			}

			err = storeInstance.Database.UpdateDatabaseJob(nil, job)
			if err != nil {
				controllers.WriteErrorResponse(w, err)
				return
			}

			response.Status = http.StatusOK
			response.Success = true
			json.NewEncoder(w).Encode(response)

			return
		}

		if r.Method == http.MethodGet {
			job, err := storeInstance.Database.GetDatabaseJob(utils.DecodePath(r.PathValue("job")))
			if err != nil {
				controllers.WriteErrorResponse(w, err)
				return
			}

			response.Status = http.StatusOK
			response.Success = true
			response.Data = job
			json.NewEncoder(w).Encode(response)

			return
		}

		if r.Method == http.MethodDelete {
			err := storeInstance.Database.DeleteDatabaseJob(nil, utils.DecodePath(r.PathValue("job")))
			if err != nil {
				controllers.WriteErrorResponse(w, err)
				return
			}

			response.Status = http.StatusOK
			response.Success = true
			json.NewEncoder(w).Encode(response)
			return
		}
	}
}
