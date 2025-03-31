//go:build linux
// +build linux

package rpcmount

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/agent/agentfs/types"
	arpcfs "github.com/pbs-plus/pbs-plus/internal/backend/arpc"
	"github.com/pbs-plus/pbs-plus/internal/backend/arpc/mount"
	"github.com/pbs-plus/pbs-plus/internal/store"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
)

type BackupArgs struct {
	JobId          string
	TargetHostname string
	Drive          string
	MountPath      string
}

type BackupReply struct {
	Status     int
	Message    string
	BackupMode string
}

type CleanupArgs struct {
	JobId          string
	TargetHostname string
	Drive          string
}

type CleanupReply struct {
	Status  int
	Message string
}

type MountRPCService struct {
	Store *store.Store
}

func (s *MountRPCService) Backup(args *BackupArgs, reply *BackupReply) error {
	syslog.L.Info().
		WithMessage("Received backup request").
		WithFields(map[string]interface{}{
			"jobId":  args.JobId,
			"target": args.TargetHostname,
			"drive":  args.Drive,
		}).Write()

	// Retrieve the job from the database.
	job, err := s.Store.Database.GetJob(args.JobId)
	if err != nil {
		reply.Status = 404
		reply.Message = "MountHandler: Unable to get job from id"
		return fmt.Errorf("backup: %w", err)
	}

	// Create a context with a 2-minute timeout.
	ctx, cancel := context.WithTimeout(s.Store.Ctx, 5*time.Minute)
	defer cancel()

	// Retrieve the ARPC session for the target.
	arpcSess, exists := s.Store.ARPCSessionManager.GetSession(args.TargetHostname)
	if !exists {
		reply.Status = 500
		reply.Message = "MountHandler: Failed to send backup request to target -> unable to reach target"
		return errors.New(reply.Message)
	}

	// Prepare the backup request (using the types.BackupReq structure).
	backupReq := types.BackupReq{
		Drive:      args.Drive,
		JobId:      args.JobId,
		SourceMode: job.SourceMode,
	}

	// Call the target's backup method via ARPC.
	backupResp, err := arpcSess.CallContext(ctx, "backup", &backupReq)
	if err != nil || backupResp.Status != 200 {
		if err != nil {
			syslog.L.Error(err).WithMessage(backupResp.Message).Write()
		}
		reply.Status = backupResp.Status
		reply.Message = backupResp.Message
		return errors.New(reply.Message)
	}

	// Parse the backup response message (format: "backupMode|namespace").
	backupRespSplit := strings.Split(backupResp.Message, "|")
	backupMode := backupRespSplit[0]

	// If a namespace is provided in the backup response, update the job.
	if len(backupRespSplit) == 2 && backupRespSplit[1] != "" {
		job.Namespace = backupRespSplit[1]
		if err := s.Store.Database.UpdateJob(nil, job); err != nil {
			syslog.L.Error(err).WithField("namespace", backupRespSplit[1]).Write()
		}
	}

	// Retrieve or initialize an ARPCFS instance.
	// The child session key is "targetHostname|jobId".
	childKey := args.TargetHostname + "|" + args.JobId
	arpcFSRPC, exists := s.Store.ARPCSessionManager.GetSession(childKey)
	if !exists {
		reply.Status = 500
		reply.Message = "MountHandler: Failed to send backup request to target -> unable to reach child target"
		return errors.New(reply.Message)
	}
	arpcFS := arpcfs.NewARPCFS(s.Store.Ctx, arpcFSRPC, args.TargetHostname, args.JobId, backupMode)
	if arpcFS == nil {
		reply.Status = 500
		reply.Message = "MountHandler: Failed to send create ARPCFS"
		return errors.New(reply.Message)
	}

	store.CreateFSConnection(childKey, arpcFSRPC, arpcFS)

	if err := mount.Mount(arpcFS, args.MountPath); err != nil {
		syslog.L.Error(err).Write()
		reply.Status = 500
		reply.Message = fmt.Sprintf("MountHandler: Failed to create fuse connection for target -> %v", err)
		return fmt.Errorf("backup: %w", err)
	}

	// Set the reply values.
	reply.Status = 200
	reply.Message = backupMode + "|" + job.Namespace
	reply.BackupMode = backupMode

	syslog.L.Info().
		WithMessage("Backup successful").
		WithFields(map[string]interface{}{
			"jobId":  args.JobId,
			"mount":  args.MountPath,
			"backup": backupMode,
		}).Write()

	return nil
}

func (s *MountRPCService) Cleanup(args *CleanupArgs, reply *CleanupReply) error {
	syslog.L.Info().
		WithMessage("Received cleanup request").
		WithFields(map[string]interface{}{
			"jobId":  args.JobId,
			"target": args.TargetHostname,
			"drive":  args.Drive,
		}).Write()

	// Create a 30-second timeout context.
	ctx, cancel := context.WithTimeout(s.Store.Ctx, 5*time.Minute)
	defer cancel()

	// Try to acquire an ARPC session for the target.
	arpcSess, exists := s.Store.ARPCSessionManager.GetSession(args.TargetHostname)
	if !exists {
		reply.Status = 500
		reply.Message = "Failed to send closure request to target -> unable to reach target"
		return fmt.Errorf("cleanup: unable to reach target for job %s", args.JobId)
	}

	// Create a cleanup request (using the BackupReq type).
	cleanupReq := types.BackupReq{
		Drive: args.Drive,
		JobId: args.JobId,
	}

	// Instruct the target to perform its cleanup.
	cleanupResp, err := arpcSess.CallContext(ctx, "cleanup", &cleanupReq)
	if err != nil || cleanupResp.Status != 200 {
		if err != nil {
			err = errors.New(cleanupResp.Message)
		}
		reply.Status = cleanupResp.Status
		reply.Message = cleanupResp.Message
		return fmt.Errorf("cleanup: %w", err)
	}

	reply.Status = cleanupResp.Status
	reply.Message = "Cleanup successful"

	arpcSess.Close()

	syslog.L.Info().
		WithMessage("Cleanup successful").
		WithField("jobId", args.JobId).
		Write()

	return nil
}

func StartRPCServer(socketPath string, storeInstance *store.Store) error {
	// Remove any stale socket file.
	_ = os.RemoveAll(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", socketPath, err)
	}

	service := &MountRPCService{
		Store: storeInstance,
	}

	// Register the RPC service.
	if err := rpc.Register(service); err != nil {
		return fmt.Errorf("failed to register rpc service: %v", err)
	}

	// Start accepting connections.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rpc.Accept(listener)
	}()

	syslog.L.Info().
		WithMessage("RPC server listening").
		WithField("socket", socketPath).
		Write()

	wg.Wait()
	return nil
}
