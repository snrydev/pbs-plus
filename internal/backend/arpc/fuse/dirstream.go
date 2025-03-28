package fuse

import (
	"io"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	arpcfs "github.com/pbs-plus/pbs-plus/internal/backend/arpc"
)

// dirStream implements fs.DirStream interface using ARPCFS's ReadDirStream
type dirStream struct {
	stream *arpcfs.ReadDirStream
}

func (ds *dirStream) HasNext() bool {
	if ds.stream == nil {
		return false
	}

	// Peek at next entry
	entry, err := ds.stream.Next()
	if err != nil {
		return false
	}

	// Put the entry back (you might need to add this functionality to ReadDirStream)
	// or cache it for the next Next() call
	return entry != nil
}

func (ds *dirStream) Next() (fuse.DirEntry, syscall.Errno) {
	if ds.stream == nil {
		return fuse.DirEntry{}, syscall.EINVAL
	}

	entry, err := ds.stream.Next()
	if err != nil {
		if err == io.EOF {
			return fuse.DirEntry{}, syscall.ENOENT
		}
		return fuse.DirEntry{}, fs.ToErrno(err)
	}

	return *entry, 0
}

func (ds *dirStream) Close() {
	if ds.stream != nil {
		_ = ds.stream.Close()
		ds.stream = nil
	}
}
