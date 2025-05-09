//go:build linux

package backup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/pbs-plus/pbs-plus/internal/store"
	"github.com/pbs-plus/pbs-plus/internal/store/proxmox"
	"github.com/pbs-plus/pbs-plus/internal/store/types"
)

type NamespaceReq struct {
	Name   string `json:"name"`
	Parent string `json:"parent"`
}

type PBSStoreGroups struct {
	Owner string `json:"owner"`
}

type PBSStoreGroupsResponse struct {
	Data PBSStoreGroups `json:"data"`
}

func CreateNamespace(namespace string, job types.Job, storeInstance *store.Store) error {
	if storeInstance == nil {
		return fmt.Errorf("CreateNamespace: store is required")
	}

	if proxmox.Session.APIToken == nil {
		return fmt.Errorf("CreateNamespace: api token is required")
	}

	namespaceSplit := strings.Split(namespace, "/")

	for i, ns := range namespaceSplit {
		var reqBody []byte
		var err error

		if i == 0 {
			reqBody, err = json.Marshal(&NamespaceReq{
				Name: ns,
			})
			if err != nil {
				return fmt.Errorf("CreateNamespace: error creating req body -> %w", err)
			}
		} else {
			reqBody, err = json.Marshal(&NamespaceReq{
				Name:   ns,
				Parent: strings.Join(namespaceSplit[:i], "/"),
			})
			if err != nil {
				return fmt.Errorf("CreateNamespace: error creating req body -> %w", err)
			}
		}

		_ = proxmox.Session.ProxmoxHTTPRequest(
			http.MethodPost,
			fmt.Sprintf("/api2/json/admin/datastore/%s/namespace", job.Store),
			bytes.NewBuffer(reqBody),
			nil,
		)
	}

	job.Namespace = namespace
	err := storeInstance.Database.UpdateJob(nil, job)
	if err != nil {
		return fmt.Errorf("CreateNamespace: error updating job to namespace -> %w", err)
	}

	return nil
}

func CreateDBNamespace(namespace string, job types.DatabaseJob, storeInstance *store.Store) error {
	if storeInstance == nil {
		return fmt.Errorf("CreateNamespace: store is required")
	}

	if proxmox.Session.APIToken == nil {
		return fmt.Errorf("CreateNamespace: api token is required")
	}

	namespaceSplit := strings.Split(namespace, "/")

	for i, ns := range namespaceSplit {
		var reqBody []byte
		var err error

		if i == 0 {
			reqBody, err = json.Marshal(&NamespaceReq{
				Name: ns,
			})
			if err != nil {
				return fmt.Errorf("CreateNamespace: error creating req body -> %w", err)
			}
		} else {
			reqBody, err = json.Marshal(&NamespaceReq{
				Name:   ns,
				Parent: strings.Join(namespaceSplit[:i], "/"),
			})
			if err != nil {
				return fmt.Errorf("CreateNamespace: error creating req body -> %w", err)
			}
		}

		_ = proxmox.Session.ProxmoxHTTPRequest(
			http.MethodPost,
			fmt.Sprintf("/api2/json/admin/datastore/%s/namespace", job.Store),
			bytes.NewBuffer(reqBody),
			nil,
		)
	}

	job.Namespace = namespace
	err := storeInstance.Database.UpdateDatabaseJob(nil, job)
	if err != nil {
		return fmt.Errorf("CreateNamespace: error updating job to namespace -> %w", err)
	}

	return nil
}

func GetCurrentOwner(namespace string, datastore string, storeInstance *store.Store) (string, error) {
	if storeInstance == nil {
		return "", fmt.Errorf("GetCurrentOwner: store is required")
	}

	if proxmox.Session.APIToken == nil {
		return "", fmt.Errorf("GetCurrentOwner: api token is required")
	}

	params := url.Values{}
	params.Add("ns", namespace)

	groupsResp := PBSStoreGroupsResponse{}
	err := proxmox.Session.ProxmoxHTTPRequest(
		http.MethodGet,
		fmt.Sprintf("/api2/json/admin/datastore/%s/groups?%s", datastore, params.Encode()),
		nil,
		&groupsResp,
	)
	if err != nil {
		return "", fmt.Errorf("GetCurrentOwner: error creating http request -> %w", err)
	}

	return groupsResp.Data.Owner, nil
}

func SetDatastoreOwner(backupId string, datastore string, namespace string, storeInstance *store.Store, owner string) error {
	if storeInstance == nil {
		return fmt.Errorf("SetDatastoreOwner: store is required")
	}

	if proxmox.Session.APIToken == nil {
		return fmt.Errorf("SetDatastoreOwner: api token is required")
	}

	jobStore := fmt.Sprintf(
		"%s@localhost:%s",
		proxmox.Session.APIToken.TokenId,
		datastore,
	)

	cmdArgs := []string{
		"change-owner",
		fmt.Sprintf("host/%s", backupId),
		owner,
		"--repository",
		jobStore,
	}

	if namespace != "" {
		cmdArgs = append(cmdArgs, "--ns")
		cmdArgs = append(cmdArgs, namespace)
	}

	cmd := exec.Command("/usr/bin/proxmox-backup-client", cmdArgs...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("PBS_PASSWORD=%s", proxmox.Session.APIToken.Value))

	pbsStatus, err := proxmox.Session.GetPBSStatus()
	if err == nil {
		if fingerprint, ok := pbsStatus.Info["fingerprint"]; ok {
			cmd.Env = append(cmd.Env, fmt.Sprintf("PBS_FINGERPRINT=%s", fingerprint))
		}
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("SetDatastoreOwner: proxmox-backup-client change-owner error (%s) -> %w", cmd.String(), err)
	}

	return nil
}

func FixDatastore(backupId string, datastore string, namespace string, storeInstance *store.Store) error {
	newOwner := ""
	if proxmox.Session.APIToken != nil {
		newOwner = proxmox.Session.APIToken.TokenId
	}

	return SetDatastoreOwner(backupId, datastore, namespace, storeInstance, newOwner)
}
