package arpcfs

import (
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/agent/agentfs/types"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
)

func (fs *ARPCFS) OpenDir(filename string, flags uint32) (types.HandleId, error) {
	if fs.session == nil {
		syslog.L.Error(os.ErrInvalid).
			WithMessage("arpc session is nil").
			WithJob(fs.JobId).
			Write()
		return 0, syscall.ENOENT
	}

	var resp types.HandleId
	req := types.OpenDirReq{
		Path:  filename,
		Flags: flags,
	}

	raw, err := fs.session.CallMsgWithTimeout(1*time.Minute, fs.JobId+"/OpenDir", &req)
	if err != nil {
		if !strings.HasSuffix(req.Path, ".pxarexclude") {
			syslog.L.Error(err).
				WithField("path", req.Path).
				WithJob(fs.JobId).
				Write()
		}
		return 0, syscall.ENOENT
	}

	err = resp.Decode(raw)
	if err != nil {
		if !strings.HasSuffix(req.Path, ".pxarexclude") {
			syslog.L.Error(err).
				WithField("path", req.Path).
				WithJob(fs.JobId).
				Write()
		}
		return 0, syscall.ENOENT
	}

	atomic.AddInt64(&fs.folderCount, 1)

	return resp, nil
}

func (fs *ARPCFS) Readdirent(fh types.HandleId) (types.AgentDirEntry, error) {
	if fs.session == nil {
		syslog.L.Error(os.ErrInvalid).
			WithMessage("arpc session is nil").
			WithJob(fs.JobId).
			Write()
		return types.AgentDirEntry{}, syscall.ENOENT
	}

	var resp types.AgentDirEntry
	raw, err := fs.session.CallMsgWithTimeout(1*time.Minute, fs.JobId+"/Readdirent", &fh)
	if err != nil {
		return types.AgentDirEntry{}, syscall.ENOENT
	}

	err = resp.Decode(raw)
	if err != nil {
		return types.AgentDirEntry{}, syscall.ENOENT
	}

	return resp, nil
}

func (fs *ARPCFS) SeekDir(fh types.HandleId, off uint64) error {
	if fs.session == nil {
		syslog.L.Error(os.ErrInvalid).
			WithMessage("arpc session is nil").
			WithJob(fs.JobId).
			Write()
		return syscall.ENOENT
	}

	req := types.DirSeekReq{
		FolderHandleId: uint64(fh),
		Offset:         off,
	}

	_, err := fs.session.CallMsgWithTimeout(1*time.Minute, fs.JobId+"/SeekDir", &req)
	if err != nil {
		return err
	}

	return nil
}

func (fs *ARPCFS) CloseDir(fh types.HandleId) error {
	if fs.session == nil {
		syslog.L.Error(os.ErrInvalid).
			WithMessage("arpc session is nil").
			WithJob(fs.JobId).
			Write()
		return syscall.ENOENT
	}

	_, err := fs.session.CallMsgWithTimeout(1*time.Minute, fs.JobId+"/CloseDir", &fh)
	if err != nil {
		return err
	}

	return nil
}
