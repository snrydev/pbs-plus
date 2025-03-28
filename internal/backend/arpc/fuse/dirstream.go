package fuse

import (
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/pbs-plus/pbs-plus/internal/agent/agentfs/types"
	arpcfs "github.com/pbs-plus/pbs-plus/internal/backend/arpc"
)

type dirStream struct {
	fs       *arpcfs.ARPCFS
	handle   types.HandleId
	closed   bool
	hasNext  bool
	nextErr  syscall.Errno
	nextItem *fuse.DirEntry
}

func (ds *dirStream) HasNext() bool {
	if ds.closed {
		return false
	}

	// If we already have a next item cached, return true
	if ds.nextItem != nil {
		return true
	}

	// Try to read the next entry
	entry, err := ds.fs.Readdirent(ds.handle)
	if err != nil {
		ds.hasNext = false
		ds.nextErr = fs.ToErrno(err)
		return false
	}

	// Cache the entry for the Next() call
	ds.nextItem = &entry
	ds.hasNext = true
	return true
}

func (ds *dirStream) Next() (fuse.DirEntry, syscall.Errno) {
	if ds.nextItem == nil {
		// This shouldn't happen if HasNext() is called properly
		return fuse.DirEntry{}, syscall.EINVAL
	}

	// Return the cached entry and clear it
	entry := *ds.nextItem
	ds.nextItem = nil
	return entry, 0
}

func (ds *dirStream) Close() {
	if !ds.closed {
		_ = ds.fs.CloseDir(ds.handle)
		ds.closed = true
	}
}
