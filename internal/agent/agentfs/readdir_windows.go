//go:build windows

package agentfs

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/pbs-plus/pbs-plus/internal/agent/agentfs/types"
	"golang.org/x/sys/windows"
)

const (
	FILE_LIST_DIRECTORY          = 0x0001
	FILE_SHARE_READ              = 0x00000001
	FILE_SHARE_WRITE             = 0x00000002
	FILE_SHARE_DELETE            = 0x00000004
	OPEN_EXISTING                = 3
	FILE_DIRECTORY_FILE          = 0x00000001
	FILE_SYNCHRONOUS_IO_NONALERT = 0x00000020
	OBJ_CASE_INSENSITIVE         = 0x00000040
	STATUS_SUCCESS               = 0x00000000
	STATUS_NO_SUCH_FILE          = 0xC000000F // Used for seeking errors potentially
	STATUS_NO_MORE_FILES         = 0x80000006
)

type UnicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

type ObjectAttributes struct {
	Length                   uint32
	RootDirectory            uintptr
	ObjectName               *UnicodeString
	Attributes               uint32
	SecurityDescriptor       uintptr
	SecurityQualityOfService uintptr
}

type IoStatusBlock struct {
	Status      int32
	Information uintptr
}

type FileDirectoryInformation struct {
	NextEntryOffset uint32
	FileIndex       uint32
	CreationTime    int64
	LastAccessTime  int64
	LastWriteTime   int64
	ChangeTime      int64
	EndOfFile       int64
	AllocationSize  int64
	FileAttributes  uint32
	FileNameLength  uint32
	FileName        uint16
}

var (
	ntdll                = syscall.NewLazyDLL("ntdll.dll")
	ntCreateFile         = ntdll.NewProc("NtCreateFile")
	ntQueryDirectoryFile = ntdll.NewProc("NtQueryDirectoryFile")
	ntClose              = ntdll.NewProc("NtClose")
	ntWriteFile          = ntdll.NewProc("NtWriteFile")
	rtlInitUnicodeString = ntdll.NewProc("RtlInitUnicodeString")
)

func convertToNTPath(path string) string {
	if len(path) >= 4 && path[:4] == "\\??\\" {
		return path
	}

	if len(path) >= 2 && path[1] == ':' {
		return "\\??\\" + path
	}
	return "\\??\\" + path
}

func boolToInt(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

const (
	FILE_ATTRIBUTE_UNPINNED = 0x00100000
	FILE_ATTRIBUTE_PINNED   = 0x00080000
)

const (
	excludedAttrs = windows.FILE_ATTRIBUTE_REPARSE_POINT |
		windows.FILE_ATTRIBUTE_DEVICE |
		windows.FILE_ATTRIBUTE_OFFLINE |
		windows.FILE_ATTRIBUTE_VIRTUAL |
		windows.FILE_ATTRIBUTE_RECALL_ON_OPEN |
		windows.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS |
		windows.FILE_ATTRIBUTE_ENCRYPTED |
		FILE_ATTRIBUTE_UNPINNED | FILE_ATTRIBUTE_PINNED
)

// windowsAttributesToFileMode converts Windows file attributes to Go's os.FileMode
func windowsAttributesToFileMode(attrs uint32) uint32 {
	var mode os.FileMode = 0

	// Check for directory
	if attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		mode |= os.ModeDir
	}

	// Check for symlink (reparse point)
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		mode |= os.ModeSymlink
	}

	// Check for device file
	if attrs&windows.FILE_ATTRIBUTE_DEVICE != 0 {
		mode |= os.ModeDevice
	}

	// Set regular file permissions (approximation on Windows)
	if mode == 0 {
		// It's a regular file
		mode |= 0644 // Default permission for files
	} else if mode&os.ModeDir != 0 {
		// It's a directory
		mode |= 0755 // Default permission for directories
	}

	return uint32(mode)
}

var fileInfoPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 64*1024)
	},
}

// SeekableDirStream implements DirStream, FileReaddirenter, FileSeekdirer,
// and FileReleasedirer for Windows directories using NT APIs.
type SeekableDirStream struct {
	mu            sync.Mutex
	handle        uintptr       // Directory handle from NtCreateFile
	buffer        []byte        // Buffer to hold directory entries from NtQueryDirectoryFile
	poolBuf       bool          // Indicates if the buffer came from the pool
	currentOffset int           // Current read offset within the buffer
	bytesInBuf    uintptr       // Number of valid bytes currently in the buffer
	eof           bool          // True if NtQueryDirectoryFile returned STATUS_NO_MORE_FILES
	lastStatus    syscall.Errno // Stores the last error encountered during next/fillBuffer
}

type FolderHandle struct {
	uint64
}

func OpendirHandle(handleId uint64, path string, flags uint32) (*SeekableDirStream, error) {
	ntPath := convertToNTPath(path)

	pathUTF16, err := syscall.UTF16PtrFromString(ntPath)
	if err != nil {
		// syscall.UTF16PtrFromString already returns an error
		return nil, err
	}

	var unicodeString UnicodeString
	rtlInitUnicodeString.Call(
		uintptr(unsafe.Pointer(&unicodeString)),
		uintptr(unsafe.Pointer(pathUTF16)),
	)

	var objectAttributes ObjectAttributes
	objectAttributes.Length = uint32(unsafe.Sizeof(objectAttributes))
	objectAttributes.ObjectName = &unicodeString
	objectAttributes.Attributes = OBJ_CASE_INSENSITIVE

	var handle uintptr
	var ioStatusBlock IoStatusBlock

	desiredAccess := uintptr(FILE_LIST_DIRECTORY | syscall.SYNCHRONIZE)

	status, _, _ := ntCreateFile.Call(
		uintptr(unsafe.Pointer(&handle)),
		desiredAccess,
		uintptr(unsafe.Pointer(&objectAttributes)),
		uintptr(unsafe.Pointer(&ioStatusBlock)),
		0,
		0,
		FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,
		OPEN_EXISTING,
		FILE_DIRECTORY_FILE|FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)

	if status != STATUS_SUCCESS {
		// Map common errors, otherwise return a generic one
		errno := syscall.Errno(status)
		switch errno {
		case syscall.ERROR_FILE_NOT_FOUND, syscall.ERROR_PATH_NOT_FOUND:
			return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: os.ErrNotExist}
		case syscall.ERROR_ACCESS_DENIED:
			return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: os.ErrPermission}
		default:
			return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: errno}
		}
	}

	bufInterface := fileInfoPool.Get()
	buffer, ok := bufInterface.([]byte)
	if !ok {
		ntClose.Call(handle)
		// This is an internal logic error, fmt.Errorf is okay here.
		return nil, fmt.Errorf("invalid buffer type from pool")
	}

	stream := &SeekableDirStream{
		handle:        handle,
		buffer:        buffer,
		poolBuf:       true,
		currentOffset: 0,
		bytesInBuf:    0,
		eof:           false,
		lastStatus:    0,
	}

	return stream, nil
}

func (ds *SeekableDirStream) fillBuffer(restart bool) error {
	if ds.eof {
		return nil
	}

	ds.currentOffset = 0
	ds.bytesInBuf = 0

	var ioStatusBlock IoStatusBlock

	status, _, _ := ntQueryDirectoryFile.Call(
		ds.handle,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&ioStatusBlock)),
		uintptr(unsafe.Pointer(&ds.buffer[0])),
		uintptr(len(ds.buffer)),
		uintptr(1), // FileDirectoryInformation class
		uintptr(0), // ReturnSingleEntry = FALSE
		0,          // FileName = NULL
		uintptr(boolToInt(restart)),
	)

	if status == STATUS_NO_MORE_FILES {
		ds.eof = true
		ds.bytesInBuf = 0
		return nil // Not an error, signifies end of directory
	}

	if status != STATUS_SUCCESS {
		ds.eof = true
		ds.bytesInBuf = 0
		ds.lastStatus = syscall.Errno(status)
		// Return the syscall error directly
		return ds.lastStatus
	}

	ds.bytesInBuf = ioStatusBlock.Information
	ds.lastStatus = 0
	return nil
}

func (ds *SeekableDirStream) hasNext() bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.currentOffset < int(ds.bytesInBuf) {
		return true
	}

	if ds.eof {
		return false
	}

	// Try to fill buffer if empty and not EOF
	err := ds.fillBuffer(false)
	if err != nil {
		// Store the error, hasNext should return false on error
		ds.lastStatus = err.(syscall.Errno)
		return false
	}

	// Check again after potentially filling the buffer
	return ds.currentOffset < int(ds.bytesInBuf) && !ds.eof
}

func (ds *SeekableDirStream) next() (types.AgentDirEntry, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Check if buffer needs refilling or if we hit EOF/error
	if ds.currentOffset >= int(ds.bytesInBuf) {
		if ds.lastStatus != 0 {
			// Return the previously stored error
			err := ds.lastStatus
			ds.lastStatus = 0 // Consume the error
			return types.AgentDirEntry{}, err
		}
		if ds.eof {
			return types.AgentDirEntry{}, io.EOF
		}
		// If buffer is empty but no EOF and no prior error, try filling
		err := ds.fillBuffer(false)
		if err != nil {
			ds.lastStatus = err.(syscall.Errno) // Store error
			return types.AgentDirEntry{}, err
		}
		// After filling, check again
		if ds.currentOffset >= int(ds.bytesInBuf) {
			if ds.eof {
				return types.AgentDirEntry{}, io.EOF
			}
			// Should not happen if fillBuffer succeeded without EOF
			ds.lastStatus = syscall.EIO
			return types.AgentDirEntry{}, syscall.EIO
		}
	}

	entryPtr := unsafe.Pointer(&ds.buffer[ds.currentOffset])
	entry := (*FileDirectoryInformation)(entryPtr)

	// Basic sanity check on the entry structure size
	if ds.currentOffset+int(unsafe.Sizeof(FileDirectoryInformation{})) >
		int(ds.bytesInBuf) {
		ds.lastStatus = syscall.EIO
		return types.AgentDirEntry{}, syscall.EIO // Buffer underflow/corruption
	}

	// Validate FileNameLength before using it
	if entry.FileNameLength == 0 || entry.FileNameLength > uint32(len(ds.buffer)) {
		ds.lastStatus = syscall.EIO
		return types.AgentDirEntry{}, syscall.EIO // Invalid data
	}

	// Ensure the full entry including filename fits within the buffer bounds
	entrySizeWithFilename := int(unsafe.Offsetof(entry.FileName)) +
		int(entry.FileNameLength)
	if ds.currentOffset+entrySizeWithFilename > int(ds.bytesInBuf) {
		ds.lastStatus = syscall.EIO
		return types.AgentDirEntry{}, syscall.EIO // Filename exceeds buffer
	}

	// Extract filename
	fileNameLenInChars := entry.FileNameLength / 2 // UTF-16 characters
	fileNamePtr := unsafe.Pointer(uintptr(entryPtr) +
		unsafe.Offsetof(entry.FileName))
	fileNameSlice := unsafe.Slice((*uint16)(fileNamePtr), fileNameLenInChars)
	fileName := string(utf16.Decode(fileNameSlice))

	// Calculate next entry offset
	nextOffsetDelta := int(entry.NextEntryOffset)
	if nextOffsetDelta < 0 {
		ds.lastStatus = syscall.EIO
		return types.AgentDirEntry{}, syscall.EIO // Invalid offset
	}

	// Advance currentOffset
	if entry.NextEntryOffset == 0 {
		// This entry is the last one in the current buffer
		ds.currentOffset = int(ds.bytesInBuf)
	} else {
		nextAbsOffset := ds.currentOffset + nextOffsetDelta
		// Check if the next offset is valid within the buffer
		if nextAbsOffset <= ds.currentOffset || nextAbsOffset > int(ds.bytesInBuf) {
			ds.lastStatus = syscall.EIO
			return types.AgentDirEntry{}, syscall.EIO // Invalid next position
		}
		ds.currentOffset = nextAbsOffset
	}

	// Skip "." and ".." entries recursively
	if fileName == "." || fileName == ".." {
		// Need to unlock before recursive call and lock after
		ds.mu.Unlock()
		defer ds.mu.Lock()
		return ds.next() // Tail call optimization might not apply, potential stack depth issue?
	}

	// Skip entries with excluded attributes
	if entry.FileAttributes&excludedAttrs != 0 {
		ds.mu.Unlock()
		defer ds.mu.Lock()
		return ds.next()
	}

	// Convert attributes to file mode
	mode := windowsAttributesToFileMode(entry.FileAttributes)

	fuseEntry := types.AgentDirEntry{
		Name: fileName,
		Mode: mode,
	}

	ds.lastStatus = 0 // Reset last error status on successful read
	return fuseEntry, nil
}

func (ds *SeekableDirStream) Close() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.handle != 0 && ds.handle != uintptr(syscall.InvalidHandle) {
		ntClose.Call(ds.handle)
		ds.handle = uintptr(syscall.InvalidHandle)
	}

	if ds.poolBuf && ds.buffer != nil {
		fileInfoPool.Put(ds.buffer)
		ds.buffer = nil
		ds.poolBuf = false
	}

	// Reset state
	ds.currentOffset = 0
	ds.bytesInBuf = 0
	ds.eof = true
	ds.lastStatus = 0
}

func (ds *SeekableDirStream) Readdirent(ctx context.Context) (types.AgentDirEntry, error) {
	select {
	case <-ctx.Done():
		return types.AgentDirEntry{}, ctx.Err()
	default:
	}

	// Use hasNext and next which handle locking and buffer filling
	if ds.hasNext() {
		entry, err := ds.next()
		// next() returns io.EOF or syscall errors appropriately
		return entry, err
	}

	// If hasNext is false, check the reason
	ds.mu.Lock()
	lastErr := ds.lastStatus
	isEof := ds.eof
	ds.mu.Unlock()

	if lastErr != 0 {
		return types.AgentDirEntry{}, lastErr // Return stored error
	}
	if isEof {
		return types.AgentDirEntry{}, io.EOF // End of directory reached cleanly
	}

	// Should not be reachable if hasNext/Next logic is correct
	// but return EIO as a fallback generic I/O error.
	return types.AgentDirEntry{}, syscall.EIO
}

func (ds *SeekableDirStream) Seekdir(ctx context.Context, off uint64) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if off == 0 {
		// Reset state and refill buffer from the beginning
		ds.currentOffset = 0
		ds.bytesInBuf = 0
		ds.eof = false
		ds.lastStatus = 0
		// fillBuffer returns syscall.Errno on failure
		return ds.fillBuffer(true) // restart scan
	}

	// Seeking to arbitrary offsets is not supported by NtQueryDirectoryFile
	return syscall.ENOSYS // Operation not supported
}

func (ds *SeekableDirStream) Releasedir(ctx context.Context, releaseFlags uint32) {
	ds.Close()
}
