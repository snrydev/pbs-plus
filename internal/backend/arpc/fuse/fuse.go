//go:build linux

package fuse

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/sonroyaalmerol/pbs-plus/internal/backend/arpc/types"
	"github.com/sonroyaalmerol/pbs-plus/internal/syslog"
)

type StatFSer interface {
	StatFS() (types.StatFS, error)
}

// CallHook is the callback called before every FUSE operation
type CallHook func(ctx context.Context) error

func newRoot(underlying billy.Basic, callHook CallHook) fs.InodeEmbedder {
	if callHook == nil {
		callHook = func(ctx context.Context) error {
			return nil
		}
	}
	return &BillyRoot{
		underlying: underlying,
		callHook:   callHook,
	}
}

// Mount mounts the billy filesystem at the specified mountpoint
func Mount(mountpoint string, fsName string, underlying billy.Basic, callHook CallHook) (*fuse.Server, error) {
	root := newRoot(underlying, callHook)

	timeout := 3 * time.Second

	options := &fs.Options{
		MountOptions: fuse.MountOptions{
			Debug:      true,
			FsName:     fsName,
			Name:       "pbsagent",
			AllowOther: true,
		},
		// Use sensible cache timeouts
		EntryTimeout:    &timeout,
		AttrTimeout:     &timeout,
		NegativeTimeout: &timeout,
	}

	server, err := fs.Mount(mountpoint, root, options)
	if err != nil {
		return nil, err
	}
	return server, nil
}

// BillyRoot is the root node of the filesystem
type BillyRoot struct {
	fs.Inode
	underlying billy.Basic
	callHook   CallHook
}

var _ = (fs.NodeGetattrer)((*BillyRoot)(nil))
var _ = (fs.NodeLookuper)((*BillyRoot)(nil))
var _ = (fs.NodeReaddirer)((*BillyRoot)(nil))
var _ = (fs.NodeOpener)((*BillyRoot)(nil))
var _ = (fs.NodeStatfser)((*BillyRoot)(nil))
var _ = (fs.NodeAccesser)((*BillyRoot)(nil))
var _ = (fs.NodeOpendirHandler)((*BillyRoot)(nil))
var _ = (fs.NodeStatxer)((*BillyRoot)(nil))

func (r *BillyRoot) Access(ctx context.Context, mask uint32) syscall.Errno {
	if err := r.callHook(ctx); err != nil {
		return fs.ToErrno(err)
	}

	// For read-only filesystem, deny write access (bit 1)
	if mask&2 != 0 { // 2 = write bit (traditional W_OK)
		return syscall.EROFS
	}

	return 0
}

func (r *BillyRoot) OpendirHandle(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if err := r.callHook(ctx); err != nil {
		return nil, 0, fs.ToErrno(err)
	}

	return &BillyDirHandle{
		root: r,
		path: "",
	}, fuse.FOPEN_CACHE_DIR, 0
}

func (r *BillyRoot) Statx(ctx context.Context, f fs.FileHandle, flags uint32, mask uint32, out *fuse.StatxOut) syscall.Errno {
	if err := r.callHook(ctx); err != nil {
		return fs.ToErrno(err)
	}

	// Get file stats the regular way, then populate StatxOut
	var attrOut fuse.AttrOut
	errno := r.Getattr(ctx, f, &attrOut)
	if errno != 0 {
		return errno
	}

	// Use actual STATX mask values
	const (
		STATX_TYPE  = 0x00000001 // Want stx_mode & S_IFMT
		STATX_MODE  = 0x00000002 // Want stx_mode & ~S_IFMT
		STATX_NLINK = 0x00000004 // Want stx_nlink
		STATX_SIZE  = 0x00000200 // Want stx_size
		STATX_MTIME = 0x00000020 // Want stx_mtime
	)

	// Set basic attributes
	out.Mask = STATX_TYPE | STATX_MODE | STATX_NLINK | STATX_SIZE
	out.Mode = uint16(attrOut.Mode)
	out.Size = attrOut.Size
	out.Nlink = attrOut.Nlink

	// Add timestamps if requested
	if mask&STATX_MTIME != 0 {
		out.Mask |= STATX_MTIME
		out.Mtime.Sec = attrOut.Mtime
		out.Mtime.Nsec = attrOut.Mtimensec
	}

	return 0
}

// Getattr implements NodeGetattrer
func (r *BillyRoot) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if err := r.callHook(ctx); err != nil {
		return fs.ToErrno(err)
	}

	fi, err := r.underlying.Stat("")
	if err != nil {
		return fs.ToErrno(err)
	}

	out.Mode = uint32(fi.Mode()) | syscall.S_IFDIR
	out.Size = uint64(fi.Size())
	mtime := fi.ModTime()
	out.SetTimes(nil, &mtime, nil)

	return 0
}

// Lookup implements NodeLookuper
func (r *BillyRoot) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if err := r.callHook(ctx); err != nil {
		return nil, fs.ToErrno(err)
	}

	childPath := name
	fi, err := r.underlying.Stat(childPath)
	if err != nil {
		return nil, fs.ToErrno(err)
	}

	node := &BillyNode{
		root: r,
		path: childPath,
	}

	mode := uint32(fi.Mode().Perm())
	if fi.IsDir() {
		mode |= syscall.S_IFDIR
	} else if fi.Mode()&os.ModeSymlink != 0 {
		mode |= syscall.S_IFLNK
	} else {
		mode |= syscall.S_IFREG
	}

	stable := fs.StableAttr{
		Mode: mode,
	}

	child := r.NewInode(ctx, node, stable)

	out.Mode = mode
	out.Size = uint64(fi.Size())
	mtime := fi.ModTime()
	out.SetTimes(nil, &mtime, nil)

	return child, 0
}

// Readdir implements NodeReaddirer
func (r *BillyRoot) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	if err := r.callHook(ctx); err != nil {
		return nil, fs.ToErrno(err)
	}

	if dfs, ok := r.underlying.(billy.Dir); ok {
		entries, err := dfs.ReadDir("")
		if err != nil {
			return nil, fs.ToErrno(err)
		}

		result := make([]fuse.DirEntry, 0, len(entries))
		for _, e := range entries {
			entryType := uint32(0) // DT_Unknown
			if e.IsDir() {
				entryType = syscall.DT_DIR
			} else if e.Mode()&os.ModeSymlink != 0 {
				entryType = syscall.DT_LNK
			} else {
				entryType = syscall.DT_REG
			}

			result = append(result, fuse.DirEntry{
				Name: e.Name(),
				Mode: entryType << 12, // Convert to type bits
			})
		}

		return fs.NewListDirStream(result), 0
	}

	return nil, syscall.ENOSYS
}

// Open implements NodeOpener
func (r *BillyRoot) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if err := r.callHook(ctx); err != nil {
		return nil, 0, fs.ToErrno(err)
	}

	return &BillyDirHandle{
		root: r,
		path: "",
	}, 0, 0
}

func (r *BillyRoot) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	if err := r.callHook(ctx); err != nil {
		return fs.ToErrno(err)
	}

	// Try to use StatFSer interface if available
	if statfser, ok := r.underlying.(StatFSer); ok {
		stats, err := statfser.StatFS()
		if err == nil {
			out.Blocks = stats.Blocks
			out.Bfree = stats.Bfree
			out.Bavail = stats.Bavail
			out.Files = stats.Files
			out.Ffree = stats.Ffree
			out.Bsize = uint32(stats.Bsize)
			out.NameLen = uint32(stats.NameLen)
			out.Frsize = uint32(stats.Bsize)
			return 0
		}
		// Fall through to defaults if error occurs
		syslog.L.Warnf("Failed to get StatFS info: %v", err)
	}

	// Fallback to reasonable defaults for a read-only filesystem
	out.Blocks = 1000000 // Just a reasonable number
	out.Bfree = 0        // No free blocks (read-only)
	out.Bavail = 0       // No available blocks (read-only)
	out.Files = 1000     // Reasonable number of inodes
	out.Ffree = 0        // No free inodes (read-only)
	out.Bsize = 4096     // Standard block size
	out.NameLen = 255    // Standard name length
	out.Frsize = 4096    // Fragment size

	return 0
}

// BillyNode represents a file or directory in the filesystem
type BillyNode struct {
	fs.Inode
	root *BillyRoot
	path string
}

var _ = (fs.NodeGetattrer)((*BillyNode)(nil))
var _ = (fs.NodeLookuper)((*BillyNode)(nil))
var _ = (fs.NodeReaddirer)((*BillyNode)(nil))
var _ = (fs.NodeOpener)((*BillyNode)(nil))
var _ = (fs.NodeReadlinker)((*BillyNode)(nil))
var _ = (fs.NodeStatfser)((*BillyNode)(nil))
var _ = (fs.NodeAccesser)((*BillyNode)(nil))
var _ = (fs.NodeOpendirHandler)((*BillyNode)(nil))
var _ = (fs.NodeReleaser)((*BillyNode)(nil))
var _ = (fs.NodeStatxer)((*BillyNode)(nil))

func (n *BillyNode) Access(ctx context.Context, mask uint32) syscall.Errno {
	if err := n.root.callHook(ctx); err != nil {
		return fs.ToErrno(err)
	}

	// For read-only filesystem, deny write access (bit 1)
	if mask&2 != 0 { // 2 = write bit (traditional W_OK)
		return syscall.EROFS
	}

	// Check if the file exists (that's sufficient for read-only fs)
	_, err := n.root.underlying.Stat(n.path)
	if err != nil {
		return fs.ToErrno(err)
	}

	return 0
}

func (n *BillyNode) OpendirHandle(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if err := n.root.callHook(ctx); err != nil {
		return nil, 0, fs.ToErrno(err)
	}

	// Return the directory handle with potential caching hint
	return &BillyDirHandle{
		root: n.root,
		path: n.path,
	}, fuse.FOPEN_CACHE_DIR, 0 // Enable directory caching for better performance
}

func (n *BillyNode) Release(ctx context.Context, f fs.FileHandle) syscall.Errno {
	if err := n.root.callHook(ctx); err != nil {
		return fs.ToErrno(err)
	}

	if fh, ok := f.(fs.FileReleaser); ok {
		return fh.Release(ctx)
	}

	return 0
}

func (n *BillyNode) Statx(ctx context.Context, f fs.FileHandle, flags uint32, mask uint32, out *fuse.StatxOut) syscall.Errno {
	if err := n.root.callHook(ctx); err != nil {
		return fs.ToErrno(err)
	}

	// Get file stats the regular way, then populate StatxOut
	var attrOut fuse.AttrOut
	errno := n.Getattr(ctx, f, &attrOut)
	if errno != 0 {
		return errno
	}

	// Use actual STATX mask values
	// These values come from Linux's statx flags in <linux/stat.h>
	const (
		STATX_TYPE  = 0x00000001 // Want stx_mode & S_IFMT
		STATX_MODE  = 0x00000002 // Want stx_mode & ~S_IFMT
		STATX_NLINK = 0x00000004 // Want stx_nlink
		STATX_SIZE  = 0x00000200 // Want stx_size
		STATX_MTIME = 0x00000020 // Want stx_mtime
	)

	// Set basic attributes
	out.Mask = STATX_TYPE | STATX_MODE | STATX_NLINK | STATX_SIZE
	out.Mode = uint16(attrOut.Mode)
	out.Size = attrOut.Size
	out.Nlink = attrOut.Nlink

	// Add timestamps if requested
	if mask&STATX_MTIME != 0 {
		out.Mask |= STATX_MTIME
		out.Mtime.Sec = attrOut.Mtime
		out.Mtime.Nsec = attrOut.Mtimensec
	}

	return 0
}

// Getattr implements NodeGetattrer
func (n *BillyNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if err := n.root.callHook(ctx); err != nil {
		return fs.ToErrno(err)
	}

	fi, err := n.root.underlying.Stat(n.path)
	if err != nil {
		return fs.ToErrno(err)
	}

	mode := uint32(fi.Mode().Perm())
	if fi.IsDir() {
		mode |= syscall.S_IFDIR
	} else if fi.Mode()&os.ModeSymlink != 0 {
		mode |= syscall.S_IFLNK
	} else {
		mode |= syscall.S_IFREG
	}

	out.Mode = mode
	out.Size = uint64(fi.Size())
	mtime := fi.ModTime()
	out.SetTimes(nil, &mtime, nil)

	return 0
}

// Lookup implements NodeLookuper
func (n *BillyNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if err := n.root.callHook(ctx); err != nil {
		return nil, fs.ToErrno(err)
	}

	childPath := filepath.Join(n.path, name)
	fi, err := n.root.underlying.Stat(childPath)
	if err != nil {
		return nil, fs.ToErrno(err)
	}

	childNode := &BillyNode{
		root: n.root,
		path: childPath,
	}

	mode := uint32(fi.Mode().Perm())
	if fi.IsDir() {
		mode |= syscall.S_IFDIR
	} else if fi.Mode()&os.ModeSymlink != 0 {
		mode |= syscall.S_IFLNK
	} else {
		mode |= syscall.S_IFREG
	}

	stable := fs.StableAttr{
		Mode: mode,
	}

	child := n.NewInode(ctx, childNode, stable)

	out.Mode = mode
	out.Size = uint64(fi.Size())
	mtime := fi.ModTime()
	out.SetTimes(nil, &mtime, nil)

	return child, 0
}

// Readdir implements NodeReaddirer
func (n *BillyNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	if err := n.root.callHook(ctx); err != nil {
		return nil, fs.ToErrno(err)
	}

	if dfs, ok := n.root.underlying.(billy.Dir); ok {
		entries, err := dfs.ReadDir(n.path)
		if err != nil {
			return nil, fs.ToErrno(err)
		}

		result := make([]fuse.DirEntry, 0, len(entries))
		for _, e := range entries {
			entryType := uint32(0) // DT_Unknown
			if e.IsDir() {
				entryType = syscall.DT_DIR
			} else if e.Mode()&os.ModeSymlink != 0 {
				entryType = syscall.DT_LNK
			} else {
				entryType = syscall.DT_REG
			}

			result = append(result, fuse.DirEntry{
				Name: e.Name(),
				Mode: entryType << 12, // Convert to type bits
			})
		}

		return fs.NewListDirStream(result), 0
	}

	return nil, syscall.ENOSYS
}

// Open implements NodeOpener
func (n *BillyNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if err := n.root.callHook(ctx); err != nil {
		return nil, 0, fs.ToErrno(err)
	}

	if n.IsDir() {
		return &BillyDirHandle{
			root: n.root,
			path: n.path,
		}, 0, 0
	}

	file, err := n.root.underlying.OpenFile(n.path, int(flags), 0)
	if err != nil {
		return nil, 0, fs.ToErrno(err)
	}

	return &BillyFileHandle{
		root: n.root,
		file: file,
	}, 0, 0
}

// Readlink implements NodeReadlinker
func (n *BillyNode) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
	if err := n.root.callHook(ctx); err != nil {
		return nil, fs.ToErrno(err)
	}

	if sfs, ok := n.root.underlying.(billy.Symlink); ok {
		target, err := sfs.Readlink(n.path)
		if err != nil {
			return nil, fs.ToErrno(err)
		}
		return []byte(target), 0
	}

	return nil, syscall.ENOSYS
}

func (n *BillyNode) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	return n.root.Statfs(ctx, out)
}

// BillyFileHandle handles file operations
type BillyFileHandle struct {
	root *BillyRoot
	file billy.File
}

var _ = (fs.FileReader)((*BillyFileHandle)(nil))
var _ = (fs.FileReleaser)((*BillyFileHandle)(nil))

// Read implements FileReader
func (fh *BillyFileHandle) Read(ctx context.Context, dest []byte, offset int64) (fuse.ReadResult, syscall.Errno) {
	if err := fh.root.callHook(ctx); err != nil {
		return nil, fs.ToErrno(err)
	}

	n, err := fh.file.ReadAt(dest, offset)
	if err != nil && err != io.EOF {
		return nil, fs.ToErrno(err)
	}

	return fuse.ReadResultData(dest[:n]), 0
}

// Release implements FileReleaser
func (fh *BillyFileHandle) Release(ctx context.Context) syscall.Errno {
	if err := fh.root.callHook(ctx); err != nil {
		return fs.ToErrno(err)
	}

	err := fh.file.Close()
	return fs.ToErrno(err)
}

// BillyDirHandle handles directory operations
type BillyDirHandle struct {
	root *BillyRoot
	path string
}
