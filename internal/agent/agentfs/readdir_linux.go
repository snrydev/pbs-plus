//go:build linux

package agentfs

import (
	"context"
	"io"
	"io/fs"
	"os"
	"sync"
	"syscall"

	"github.com/pbs-plus/pbs-plus/internal/agent/agentfs/types"
)

type SeekableDirStream struct {
	mu           sync.Mutex
	dir          *os.File
	entries      []fs.DirEntry
	currentIndex int
	lastErr      error
}

type FolderHandle struct {
	uint64
}

func OpendirHandle(handleId uint64, path string, flags uint32) (*SeekableDirStream, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !fi.IsDir() {
		f.Close()
		return nil, &os.PathError{Op: "OpendirHandle", Path: path, Err: syscall.ENOTDIR}
	}

	entries, err := f.ReadDir(-1)
	if err != nil {
		f.Close()
		return nil, err
	}

	stream := &SeekableDirStream{
		dir:          f,
		entries:      entries,
		currentIndex: 0,
		lastErr:      nil,
	}

	return stream, nil
}

func (ds *SeekableDirStream) Close() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.dir != nil {
		ds.dir.Close()
		ds.dir = nil
	}
	ds.entries = nil
	ds.currentIndex = 0
	ds.lastErr = syscall.EBADF
}

func (ds *SeekableDirStream) Readdirent(ctx context.Context) (types.AgentDirEntry, error) {
	select {
	case <-ctx.Done():
		return types.AgentDirEntry{}, ctx.Err()
	default:
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.lastErr != nil {
		err := ds.lastErr
		if err == io.EOF {
			ds.lastErr = nil
		}
		return types.AgentDirEntry{}, err
	}

	if ds.dir == nil {
		return types.AgentDirEntry{}, syscall.EBADF
	}

	for ds.currentIndex < len(ds.entries) {
		entry := ds.entries[ds.currentIndex]
		ds.currentIndex++

		entryType := entry.Type()

		if entryType&fs.ModeSymlink != 0 || entryType&fs.ModeDevice != 0 {
			continue
		}

		fuseEntry := types.AgentDirEntry{
			Name: entry.Name(),
			Mode: uint32(entryType),
		}

		if entryType.IsRegular() || entryType.IsDir() {
			info, err := entry.Info()
			if err == nil {
				fuseEntry.Mode = uint32(info.Mode())
			}
		}

		return fuseEntry, nil
	}

	ds.lastErr = io.EOF
	return types.AgentDirEntry{}, io.EOF
}

func (ds *SeekableDirStream) Seekdir(ctx context.Context, off uint64) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.dir == nil {
		return syscall.EBADF
	}

	if off == 0 {
		ds.currentIndex = 0
		ds.lastErr = nil
		return nil
	}

	return syscall.ENOSYS
}

func (ds *SeekableDirStream) Releasedir(ctx context.Context, releaseFlags uint32) {
	ds.Close()
}
