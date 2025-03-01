//go:build windows

package vssfs

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"unsafe"

	"github.com/alphadose/haxmap"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/sonroyaalmerol/pbs-plus/internal/arpc"
	"github.com/sonroyaalmerol/pbs-plus/internal/utils/hashmap"
	"github.com/xtaci/smux"
	"golang.org/x/sys/windows"
)

// --- Types ---

type FileHandle struct {
	handle   windows.Handle
	path     string
	isDir    bool
	isClosed bool
}

type VSSFSServer struct {
	ctx             context.Context
	ctxCancel       context.CancelFunc
	jobId           string
	rootDir         string
	handles         *haxmap.Map[uint64, *FileHandle]
	readAtStatCache *haxmap.Map[uint64, *windows.ByHandleFileInformation]
	nextHandle      uint64
	arpcRouter      *arpc.Router
	bufferPool      *BufferPool
	dfsCache        *DFSCache
}

func NewVSSFSServer(jobId string, root string) *VSSFSServer {
	ctx, cancel := context.WithCancel(context.Background())
	s := &VSSFSServer{
		rootDir:         root,
		jobId:           jobId,
		handles:         hashmap.NewUint64[*FileHandle](),
		readAtStatCache: hashmap.NewUint64[*windows.ByHandleFileInformation](),
		ctx:             ctx,
		ctxCancel:       cancel,
		bufferPool:      NewBufferPool(),
		dfsCache:        NewDFSCache(),
	}

	return s
}

func (s *VSSFSServer) RegisterHandlers(r *arpc.Router) {
	r.Handle(s.jobId+"/OpenFile", s.handleOpenFile)
	r.Handle(s.jobId+"/Stat", s.handleStat)
	r.Handle(s.jobId+"/ReadDir", s.handleReadDir)
	r.Handle(s.jobId+"/ReadAt", s.handleReadAt)
	r.Handle(s.jobId+"/Close", s.handleClose)
	r.Handle(s.jobId+"/Fstat", s.handleFstat)
	r.Handle(s.jobId+"/FSstat", s.handleFsStat)

	s.arpcRouter = r
}

// Update the Close method so that the cache is stopped:
func (s *VSSFSServer) Close() {
	if s.arpcRouter != nil {
		r := s.arpcRouter
		r.CloseHandle(s.jobId + "/OpenFile")
		r.CloseHandle(s.jobId + "/Stat")
		r.CloseHandle(s.jobId + "/ReadDir")
		r.CloseHandle(s.jobId + "/ReadAt")
		r.CloseHandle(s.jobId + "/Close")
		r.CloseHandle(s.jobId + "/Fstat")
		r.CloseHandle(s.jobId + "/FSstat")
	}

	s.handles.Clear()
	atomic.StoreUint64(&s.nextHandle, 0)

	s.ctxCancel()
}

// --- Handlers ---

func (s *VSSFSServer) handleFsStat(req arpc.Request) (*arpc.Response, error) {
	// No payload expected.
	var totalBytes uint64
	err := windows.GetDiskFreeSpaceEx(
		windows.StringToUTF16Ptr(s.rootDir),
		nil,
		&totalBytes,
		nil,
	)
	if err != nil {
		return nil, err
	}

	statFs := &StatFS{
		Bsize:   uint64(4096), // Standard block size.
		Blocks:  uint64(totalBytes / 4096),
		Bfree:   0,
		Bavail:  0,
		Files:   uint64(1 << 20),
		Ffree:   0,
		NameLen: 255, // Typically supports long filenames.
	}

	fsStatBytes, err := statFs.MarshalMsg(nil)
	if err != nil {
		return nil, err
	}

	return &arpc.Response{
		Status: 200,
		Data:   fsStatBytes,
	}, nil
}

func (s *VSSFSServer) handleOpenFile(req arpc.Request) (*arpc.Response, error) {
	var payload OpenFileReq
	if _, err := payload.UnmarshalMsg(req.Payload); err != nil {
		return nil, err
	}

	// Disallow write operations.
	if payload.Flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		errStr := arpc.StringMsg("write operations not allowed")
		errBytes, err := errStr.MarshalMsg(nil)
		if err != nil {
			return nil, err
		}

		return &arpc.Response{
			Status: 403,
			Data:   errBytes,
		}, nil
	}

	path, err := s.abs(payload.Path)
	if err != nil {
		return nil, err
	}

	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}

	handle, err := windows.CreateFile(
		pathp,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_SEQUENTIAL_SCAN|windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return nil, err
	}

	fileHandle := &FileHandle{
		handle: handle,
		path:   path,
	}

	handleID := atomic.AddUint64(&s.nextHandle, 1)

	s.handles.Set(handleID, fileHandle)
	data := FileHandleId(handleID)

	dataBytes, err := data.MarshalMsg(nil)
	if err != nil {
		return nil, err
	}

	// Return the handle ID.
	return &arpc.Response{
		Status: 200,
		Data:   dataBytes,
	}, nil
}

// handleStat first checks the cache. If an entry is available it pops (removes)
// the CachedEntry and returns the stat info. Otherwise, it falls back to the
// Windows API‐based lookup.
func (s *VSSFSServer) handleStat(req arpc.Request) (*arpc.Response, error) {
	var payload StatReq
	if _, err := payload.UnmarshalMsg(req.Payload); err != nil {
		return nil, err
	}

	fullPath, err := s.abs(payload.Path)
	if err != nil {
		return nil, err
	}

	activeDir := filepath.Dir(fullPath)
	filename := filepath.Base(fullPath)

	if filename != ".pxarexclude" {
		// Try to look up from the DFSCache.
		if info, found := s.dfsCache.Lookup(activeDir, fullPath); found {
			// Return cached metadata.
			data, err := info.MarshalMsg(nil)
			if err != nil {
				return nil, err
			}
			return &arpc.Response{
				Status: 200,
				Data:   data,
			}, nil
		}
	}

	info, err := stat(fullPath)
	if err != nil {
		return nil, err
	}

	data, err := info.MarshalMsg(nil)
	if err != nil {
		return nil, err
	}
	return &arpc.Response{
		Status: 200,
		Data:   data,
	}, nil
}

// handleReadDir first attempts to serve the directory listing from the cache.
// It returns the cached DirEntries for that directory.
func (s *VSSFSServer) handleReadDir(req arpc.Request) (*arpc.Response, error) {
	var payload ReadDirReq
	if _, err := payload.UnmarshalMsg(req.Payload); err != nil {
		return nil, err
	}

	// Convert the provided path to Windows style and compute the full path.
	windowsDir := filepath.FromSlash(payload.Path)
	fullDirPath, err := s.abs(windowsDir)
	if err != nil {
		return nil, err
	}
	// If the payload is empty (or "."), use the root.
	if payload.Path == "." || payload.Path == "" {
		fullDirPath = s.rootDir
	}

	// Try to get a cached directory listing.
	entries, found := s.dfsCache.GetDirEntries(fullDirPath)
	if !found {
		// If not cached, perform the readdir (which is a Windows syscall).
		entries, err = readDir(fullDirPath)
		if err != nil {
			return nil, err
		}
		// Cache this result.
		s.dfsCache.PushDir(dirCacheEntry{
			dirPath: filepath.Clean(fullDirPath),
			entries: entries,
		})
	}

	// Marshal entries into bytes for the FUSE response.
	entryBytes, err := entries.MarshalMsg(nil)
	if err != nil {
		return nil, err
	}

	return &arpc.Response{
		Status: 200,
		Data:   entryBytes,
	}, nil
}

func (s *VSSFSServer) handleReadAt(req arpc.Request) (*arpc.Response, error) {
	var payload ReadAtReq
	if _, err := payload.UnmarshalMsg(req.Payload); err != nil {
		return nil, err
	}

	// Fast path for handle lookup with read lock
	handle, exists := s.handles.Get(uint64(payload.HandleID))
	if !exists || handle.isClosed {
		return nil, os.ErrNotExist
	}
	if handle.isDir {
		return nil, os.ErrNotExist
	}

	isDirectBuffer := req.Headers != nil && req.Headers["X-Direct-Buffer"] == "true"
	if !isDirectBuffer {
		return nil, os.ErrInvalid
	}

	// Get file size to properly detect EOF
	fileInfo, _ := s.readAtStatCache.GetOrCompute(uint64(payload.HandleID), func() *windows.ByHandleFileInformation {
		var tmpFileInfo windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(handle.handle, &tmpFileInfo); err != nil {
			return nil
		}

		return &tmpFileInfo
	})

	fileSize := int64(fileInfo.FileSizeHigh)<<32 | int64(fileInfo.FileSizeLow)

	// Check if the read starts beyond EOF
	if (fileSize) <= payload.Offset {
		// Return empty read with EOF flag
		meta := arpc.BufferMetadata{BytesAvailable: 0, EOF: true}
		data, err := meta.MarshalMsg(nil)
		if err != nil {
			return nil, err
		}

		return &arpc.Response{
			Status:    213,
			Data:      data,
			RawStream: func(stream *smux.Stream) {},
		}, nil
	}

	// Calculate how many bytes are actually available to read
	bytesAvailable := fileSize - payload.Offset
	bytesAvailable = min(bytesAvailable, int64(payload.Length))

	// Determine if this read will reach EOF
	isEOF := (payload.Offset + bytesAvailable) >= int64(fileSize)

	// Determine if this read would benefit from memory mapping
	const mmapThreshold = 64 * 1024 // 64KB
	useMmap := int(payload.Length) >= mmapThreshold && fileSize >= mmapThreshold

	var bytesRead uint32
	var mappedView uintptr
	var mapHandle windows.Handle
	var buf []byte

	if useMmap {
		// Try memory mapping for larger reads
		var err error
		mapHandle, err = windows.CreateFileMapping(handle.handle, nil, windows.PAGE_READONLY, 0, 0, nil)
		if err != nil {
			useMmap = false
		} else {
			// Map view of file at the requested offset
			viewSize := uint32(bytesAvailable)
			mappedView, err = windows.MapViewOfFile(mapHandle, windows.FILE_MAP_READ,
				uint32(payload.Offset>>32), uint32(payload.Offset&0xFFFFFFFF), uintptr(viewSize))

			if err != nil {
				windows.CloseHandle(mapHandle)
				useMmap = false
			} else {
				// Create slice from mapped memory - no copy needed
				mappedData := (*[1 << 30]byte)(unsafe.Pointer(mappedView))[:bytesAvailable:bytesAvailable]

				// We'll use the mapped memory directly - no need for buffer pool
				buf = mappedData
				bytesRead = uint32(bytesAvailable)
			}
		}
	}

	// Fall back to regular ReadFile if not using mmap or if mmap failed
	if !useMmap {
		// Get buffer from pool only if we're not using mmap
		buf = s.bufferPool.Get(payload.Length)

		var overlapped windows.Overlapped
		overlapped.Offset = uint32(payload.Offset & 0xFFFFFFFF)
		overlapped.OffsetHigh = uint32(payload.Offset >> 32)

		// Perform the actual file read
		err := windows.ReadFile(handle.handle, buf, &bytesRead, &overlapped)
		if err != nil && err != windows.ERROR_HANDLE_EOF {
			s.bufferPool.Put(buf)
			return nil, err
		}

		// Check if we've reached EOF based on bytes read vs requested
		if uint32(bytesAvailable) != bytesRead {
			// If we read fewer bytes than available, it's definitely EOF
			isEOF = true
		}
	}

	meta := arpc.BufferMetadata{BytesAvailable: int(bytesRead), EOF: isEOF}
	data, err := meta.MarshalMsg(nil)
	if err != nil {
		if useMmap {
			windows.UnmapViewOfFile(mappedView)
			windows.CloseHandle(mapHandle)
		} else {
			s.bufferPool.Put(buf)
		}
		return nil, err
	}

	streamRaw := func(stream *smux.Stream) {
		defer func() {
			if useMmap {
				windows.UnmapViewOfFile(mappedView)
				windows.CloseHandle(mapHandle)
			} else {
				s.bufferPool.Put(buf)
			}
		}()

		if _, err := stream.Write(buf[:bytesRead]); err != nil {
			return
		}
	}

	return &arpc.Response{
		Status:    213,
		Data:      data,
		RawStream: streamRaw,
	}, nil
}

func (s *VSSFSServer) handleClose(req arpc.Request) (*arpc.Response, error) {
	var payload CloseReq
	if _, err := payload.UnmarshalMsg(req.Payload); err != nil {
		return nil, err
	}

	handle, exists := s.handles.GetAndDel(uint64(payload.HandleID))
	if !exists || handle.isClosed {
		return nil, os.ErrNotExist
	}

	windows.CloseHandle(handle.handle)
	handle.isClosed = true

	s.readAtStatCache.Del(uint64(payload.HandleID))

	closed := arpc.StringMsg("closed")
	data, err := closed.MarshalMsg(nil)
	if err != nil {
		return nil, err
	}

	return &arpc.Response{Status: 200, Data: data}, nil
}

func (s *VSSFSServer) handleFstat(req arpc.Request) (*arpc.Response, error) {
	var payload FstatReq
	if _, err := payload.UnmarshalMsg(req.Payload); err != nil {
		return nil, err
	}

	handle, exists := s.handles.Get(uint64(payload.HandleID))
	if !exists || handle.isClosed {
		return nil, os.ErrNotExist
	}

	var fileInfo windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle.handle, &fileInfo); err != nil {
		return nil, err
	}

	info := createFileInfoFromHandleInfo(handle.path, &fileInfo)
	data, err := info.MarshalMsg(nil)
	if err != nil {
		return nil, err
	}
	return &arpc.Response{
		Status: 200,
		Data:   data,
	}, nil
}

func (s *VSSFSServer) abs(filename string) (string, error) {
	if filename == "" || filename == "." {
		return s.rootDir, nil
	}
	path, err := securejoin.SecureJoin(s.rootDir, filename)
	if err != nil {
		return "", err
	}
	return path, nil
}
