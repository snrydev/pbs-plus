package arpcfs

import (
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
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

	return resp, nil
}

func (fs *ARPCFS) Readdirent(fh types.HandleId) (fuse.DirEntry, error) {
	if fs.session == nil {
		syslog.L.Error(os.ErrInvalid).
			WithMessage("arpc session is nil").
			WithJob(fs.JobId).
			Write()
		return fuse.DirEntry{}, syscall.ENOENT
	}

	var resp types.AgentDirEntry
	raw, err := fs.session.CallMsgWithTimeout(1*time.Minute, fs.JobId+"/Readdirent", &fh)
	if err != nil {
		return fuse.DirEntry{}, syscall.ENOENT
	}

	err = resp.Decode(raw)
	if err != nil {
		return fuse.DirEntry{}, syscall.ENOENT

	}

	mode := os.FileMode(resp.Mode)
	modeBits := uint32(0)

	switch {
	case mode.IsDir():
		modeBits = fuse.S_IFDIR
	case mode&os.ModeSymlink != 0:
		modeBits = fuse.S_IFLNK
	default:
		modeBits = fuse.S_IFREG
	}

	return fuse.DirEntry{
		Name: resp.Name,
		Mode: modeBits,
		Off:  resp.Off,
	}, nil
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
