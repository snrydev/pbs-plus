//go:build linux
// +build linux

package jobrpc

import (
	"context"
	"fmt"
	"net"
	"net/rpc"
	"os"

	"github.com/pbs-plus/pbs-plus/internal/backend/backup"
	"github.com/pbs-plus/pbs-plus/internal/store"
	"github.com/pbs-plus/pbs-plus/internal/store/types"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
)

type QueueArgs struct {
	Job             types.Job
	DatabaseJob     types.DatabaseJob
	DatabaseTarget  types.DatabaseTarget
	Mode            backup.BackupType
	SkipCheck       bool
	Web             bool
	ExtraExclusions []string
}

type QueueReply struct {
	Status  int
	Message string
}

type JobRPCService struct {
	ctx     context.Context
	Store   *store.Store
	Manager *backup.Manager
}

func (s *JobRPCService) Queue(args *QueueArgs, reply *QueueReply) error {
	if args.Mode == backup.Disk {
		job, err := backup.NewJob(args.Job, s.Store, args.SkipCheck, args.Web, args.ExtraExclusions)
		if err != nil {
			reply.Status = 500
			reply.Message = err.Error()
			return nil
		}

		s.Manager.Enqueue(job)
	} else if args.Mode == backup.Database {
		job, err := backup.NewDatabaseJob(args.DatabaseJob, args.DatabaseTarget, s.Store, args.SkipCheck, args.Web, args.ExtraExclusions)
		if err != nil {
			reply.Status = 500
			reply.Message = err.Error()
			return nil
		}

		s.Manager.Enqueue(job)
	}

	reply.Status = 200

	return nil
}

func StartJobRPCServer(watcher chan struct{}, ctx context.Context, socketPath string, manager *backup.Manager, storeInstance *store.Store) error {
	// Remove any stale socket file.
	_ = os.RemoveAll(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", socketPath, err)
	}

	service := &JobRPCService{
		ctx:     ctx,
		Store:   storeInstance,
		Manager: manager,
	}

	// Register the RPC service.
	if err := rpc.Register(service); err != nil {
		return fmt.Errorf("failed to register rpc service: %v", err)
	}

	// Start accepting connections.
	ready := make(chan struct{})

	go func() {
		if watcher != nil {
			defer close(watcher)
		}
		close(ready)
		rpc.Accept(listener)
	}()

	syslog.L.Info().
		WithMessage("RPC server listening").
		WithField("socket", socketPath).
		Write()

	<-ready

	return nil
}

func RunJobRPCServer(ctx context.Context, socketPath string, manager *backup.Manager, storeInstance *store.Store) error {
	watcher := make(chan struct{}, 1)
	err := StartJobRPCServer(watcher, ctx, socketPath, manager, storeInstance)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		syslog.L.Info().
			WithMessage("rpc mount server shutting down due to context cancellation").
			WithField("socket", socketPath).
			Write()
		_ = os.Remove(socketPath)
	case <-watcher:
		syslog.L.Info().
			WithMessage("rpc mount server shut down unexpectedly").
			WithField("socket", socketPath).
			Write()
	}

	return nil
}
