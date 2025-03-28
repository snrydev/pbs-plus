package arpcfs

import (
	"io"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/pbs-plus/pbs-plus/internal/agent/agentfs/types"
)

// ReadDirStream represents a directory stream
type ReadDirStream struct {
	fs     *ARPCFS
	handle types.HandleId
	closed bool
}

// OpenDirStream opens a directory for streaming reads
func (fs *ARPCFS) OpenDirStream(path string) (*ReadDirStream, error) {
	handle, err := fs.OpenDir(path, 0)
	if err != nil {
		return nil, err
	}

	return &ReadDirStream{
		fs:     fs,
		handle: handle,
		closed: false,
	}, nil
}

func (ds *ReadDirStream) Next() (*fuse.DirEntry, error) {
	if ds.closed {
		return nil, io.EOF
	}

	entry, err := ds.fs.Readdirent(ds.handle)
	if err != nil {
		return nil, err
	}

	return &entry, nil
}

func (ds *ReadDirStream) Close() error {
	if !ds.closed {
		ds.closed = true
		return ds.fs.CloseDir(ds.handle)
	}
	return nil
}
