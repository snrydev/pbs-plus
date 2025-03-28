//go:build windows

package agentfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/pbs-plus/pbs-plus/internal/agent/agentfs/types"
	"github.com/pbs-plus/pbs-plus/internal/syslog"
	"golang.org/x/sys/windows"
)

const (
	FILE_LIST_DIRECTORY          = 0x0001
	FILE_SHARE_READ              = 0x00000001
	FILE_SHARE_WRITE             = 0x00000002
	FILE_SHARE_DELETE            = 0x00000004
	OPEN_EXISTING                = 1
	FILE_DIRECTORY_FILE          = 0x00000001
	FILE_SYNCHRONOUS_IO_NONALERT = 0x00000020
	OBJ_CASE_INSENSITIVE         = 0x00000040
)

// NTSTATUS Constants
const (
	STATUS_SUCCESS               = 0x00000000
	STATUS_NO_MORE_FILES         = 0x80000006
	STATUS_NO_SUCH_FILE          = 0xC000000F
	STATUS_OBJECT_NAME_NOT_FOUND = 0xC0000034
	STATUS_OBJECT_PATH_NOT_FOUND = 0xC000003A
	STATUS_ACCESS_DENIED         = 0xC0000022
	STATUS_NOT_A_DIRECTORY       = 0xC00000E3
	STATUS_INVALID_INFO_CLASS    = 0xC0000103
)

// IoStatusBlock.Information values for CreateDisposition=OPEN_EXISTING
const (
	FILE_OPENED = 1
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

// FileDirectoryInformation mirrors the NT structure.
// Note: FileName is declared as a one-element array so that we can treat it
// as a flexible array member.
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
	FileName        [1]uint16
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

func boolToUintptr(b bool) uintptr {
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

// windowsAttributesToFileMode converts Windows file attributes to Go's os.FileMode.
func windowsAttributesToFileMode(attrs uint32) uint32 {
	var mode os.FileMode = 0

	// Check for directory.
	if attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		mode |= os.ModeDir
	}

	// Check for symlink (reparse point).
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		mode |= os.ModeSymlink
	}

	// Check for device file.
	if attrs&windows.FILE_ATTRIBUTE_DEVICE != 0 {
		mode |= os.ModeDevice
	}

	// Set regular file permissions (approximation on Windows).
	if mode == 0 {
		mode |= 0644 // Default permission for regular files.
	} else if mode&os.ModeDir != 0 {
		mode |= 0755 // Default permission for directories.
	}

	return uint32(mode)
}

var fileInfoPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 64*1024)
	},
}

type SeekableDirStream struct {
	mu            sync.Mutex
	handle        syscall.Handle // Use syscall.Handle for clarity.
	path          string         // Original path (for error messages).
	buffer        []byte         // Buffer for NtQueryDirectoryFile results.
	poolBuf       bool           // Whether the buffer came from a pool.
	currentOffset int            // Read offset within the current buffer.
	bytesInBuf    uintptr        // Number of valid bytes within the buffer.
	eof           bool           // True if STATUS_NO_MORE_FILES was returned.
	lastNtStatus  uintptr        // Last NTSTATUS code from NtQueryDirectoryFile.
}

// OpendirHandle opens a directory using NtCreateFile.
func OpendirHandle(handleId uint64, path string, flags uint32) (*SeekableDirStream, error) {
	ntPath := convertToNTPath(path)

	pathUTF16 := utf16.Encode([]rune(ntPath))
	if len(pathUTF16) == 0 || pathUTF16[len(pathUTF16)-1] != 0 {
		pathUTF16 = append(pathUTF16, 0)
	}

	var unicodeString UnicodeString
	rtlInitUnicodeString.Call(
		uintptr(unsafe.Pointer(&unicodeString)),
		uintptr(unsafe.Pointer(&pathUTF16[0])),
	)

	var objectAttributes ObjectAttributes
	objectAttributes.Length = uint32(unsafe.Sizeof(objectAttributes))
	objectAttributes.ObjectName = &unicodeString
	objectAttributes.Attributes = OBJ_CASE_INSENSITIVE

	var handle syscall.Handle
	var ioStatusBlock IoStatusBlock

	desiredAccess := uintptr(FILE_LIST_DIRECTORY | syscall.SYNCHRONIZE)
	shareAccess := uintptr(FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE)
	createOptions := uintptr(FILE_DIRECTORY_FILE | FILE_SYNCHRONOUS_IO_NONALERT)
	createDisposition := uintptr(OPEN_EXISTING)

	ntStatus, _, _ := ntCreateFile.Call(
		uintptr(unsafe.Pointer(&handle)),
		desiredAccess,
		uintptr(unsafe.Pointer(&objectAttributes)),
		uintptr(unsafe.Pointer(&ioStatusBlock)),
		0,
		0,
		shareAccess,
		createDisposition,
		createOptions,
		0,
		0,
	)

	// Always close invalid handles before returning
	defer func() {
		if ntStatus != STATUS_SUCCESS || ioStatusBlock.Information != FILE_OPENED {
			if handle != syscall.InvalidHandle {
				ntClose.Call(uintptr(handle))
			}
		}
	}()

	// Check for failure first
	if ntStatus != STATUS_SUCCESS {
		if handle != syscall.InvalidHandle {
			ntClose.Call(uintptr(handle))
		}
		var goErr error
		switch ntStatus {
		case STATUS_OBJECT_NAME_NOT_FOUND, STATUS_OBJECT_PATH_NOT_FOUND:
			goErr = os.ErrNotExist
		case STATUS_ACCESS_DENIED:
			goErr = os.ErrPermission
		case STATUS_NOT_A_DIRECTORY, STATUS_INVALID_INFO_CLASS:
			goErr = syscall.ENOTDIR
		default:
			goErr = fmt.Errorf("NTSTATUS 0x%X", ntStatus)
		}
		return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: goErr}
	}

	// Check IoStatusBlock.Information
	if ioStatusBlock.Information != FILE_OPENED {
		if handle != syscall.InvalidHandle {
			ntClose.Call(uintptr(handle))
		}
		return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: os.ErrNotExist}
	}

	// Check handle validity
	if handle == syscall.InvalidHandle {
		syslog.L.Error(fmt.Errorf("NtCreateFile reported success (0x%X) but handle is invalid - path: %s", ntStatus, path)).Write()
		return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: errors.New("reported success but got invalid handle")}
	}

	bufInterface := fileInfoPool.Get()
	buffer, ok := bufInterface.([]byte)
	if !ok || buffer == nil || len(buffer) == 0 {
		ntClose.Call(uintptr(handle))
		syslog.L.Error(fmt.Errorf("invalid buffer type or size from pool")).Write()
		if ok && buffer != nil {
			fileInfoPool.Put(buffer)
		}
		return nil, fmt.Errorf("failed to get valid buffer from pool")
	}

	stream := &SeekableDirStream{
		handle:        handle,
		path:          path,
		buffer:        buffer,
		poolBuf:       true,
		currentOffset: 0,
		bytesInBuf:    0,
		eof:           false,
		lastNtStatus:  STATUS_SUCCESS,
	}

	return stream, nil
}

// fillBuffer calls NtQueryDirectoryFile to refill the internal buffer.
func (ds *SeekableDirStream) fillBuffer(restart bool) error {
	if ds.eof {
		return io.EOF
	}

	if ds.handle == syscall.InvalidHandle {
		ds.eof = true
		ds.lastNtStatus = uintptr(syscall.EBADF)
		return syscall.EBADF
	}

	if ds.buffer == nil || len(ds.buffer) == 0 {
		ds.eof = true
		ds.lastNtStatus = uintptr(syscall.EFAULT)
		return syscall.EFAULT
	}

	ds.currentOffset = 0
	ds.bytesInBuf = 0

	var ioStatusBlock IoStatusBlock

	ntStatus, _, _ := ntQueryDirectoryFile.Call(
		uintptr(ds.handle),                      // Directory handle
		0,                                       // Event
		0,                                       // ApcRoutine
		0,                                       // ApcContext
		uintptr(unsafe.Pointer(&ioStatusBlock)), // I/O status block
		uintptr(unsafe.Pointer(&ds.buffer[0])),  // Buffer
		uintptr(len(ds.buffer)),                 // Buffer length
		uintptr(1),                              // FileInformationClass = FileDirectoryInformation
		uintptr(0),                              // ReturnSingleEntry = FALSE
		0,                                       // FileName filter (NULL)
		boolToUintptr(restart),                  // RestartScan
	)

	ds.lastNtStatus = ntStatus

	switch ntStatus {
	case STATUS_SUCCESS:
		ds.bytesInBuf = ioStatusBlock.Information
		if ds.bytesInBuf == 0 {
			ds.eof = true
			return io.EOF
		}
		ds.eof = false
		return nil

	case STATUS_NO_MORE_FILES:
		ds.eof = true
		ds.bytesInBuf = 0
		return io.EOF

	default:
		ds.eof = true
		ds.bytesInBuf = 0
		switch ntStatus {
		case STATUS_NO_SUCH_FILE:
			return os.ErrNotExist
		case STATUS_ACCESS_DENIED:
			return os.ErrPermission
		default:
			return fmt.Errorf("NtQueryDirectoryFile failed: NTSTATUS 0x%X", ntStatus)
		}
	}
}

// advanceToNextEntry updates the current reading position using the NextEntryOffset field.
func (ds *SeekableDirStream) advanceToNextEntry(entry *FileDirectoryInformation) error {
	nextOffsetDelta := int(entry.NextEntryOffset)
	if nextOffsetDelta <= 0 {
		ds.currentOffset = int(ds.bytesInBuf)
		return nil
	}
	nextAbsOffset := ds.currentOffset + nextOffsetDelta
	if nextAbsOffset <= ds.currentOffset || nextAbsOffset > int(ds.bytesInBuf) {
		ds.lastNtStatus = uintptr(syscall.EIO)
		ds.eof = true
		return syscall.EIO
	}
	ds.currentOffset = nextAbsOffset
	return nil
}

// Readdirent reads and returns the next directory entry.
func (ds *SeekableDirStream) Readdirent(ctx context.Context) (types.AgentDirEntry, error) {
	// Check if the context has been canceled.
	select {
	case <-ctx.Done():
		return types.AgentDirEntry{}, ctx.Err()
	default:
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.handle == syscall.InvalidHandle {
		return types.AgentDirEntry{}, syscall.EBADF
	}

	for {
		if ds.currentOffset >= int(ds.bytesInBuf) {
			if ds.eof {
				return types.AgentDirEntry{}, io.EOF
			}
			if ds.lastNtStatus != STATUS_SUCCESS && ds.lastNtStatus != STATUS_NO_MORE_FILES {
				return types.AgentDirEntry{}, fmt.Errorf("previous NtQueryDirectoryFile failed: NTSTATUS 0x%X", ds.lastNtStatus)
			}
			fillErr := ds.fillBuffer(false)
			if fillErr != nil {
				return types.AgentDirEntry{}, fillErr
			}
			if ds.bytesInBuf == 0 || ds.currentOffset >= int(ds.bytesInBuf) {
				ds.eof = true
				return types.AgentDirEntry{}, io.EOF
			}
		}

		entryPtr := unsafe.Pointer(&ds.buffer[ds.currentOffset])
		entry := (*FileDirectoryInformation)(entryPtr)

		// Sanity check: ensure that the base structure fits.
		if ds.currentOffset+int(unsafe.Offsetof(entry.FileName)) > int(ds.bytesInBuf) {
			ds.lastNtStatus = uintptr(syscall.EIO)
			ds.eof = true
			return types.AgentDirEntry{}, syscall.EIO
		}

		fileNameBytes := entry.FileNameLength
		structBaseSize := int(unsafe.Offsetof(entry.FileName))
		if fileNameBytes > uint32(len(ds.buffer)) ||
			(ds.currentOffset+structBaseSize+int(fileNameBytes)) > int(ds.bytesInBuf) {
			ds.lastNtStatus = uintptr(syscall.EIO)
			ds.eof = true
			return types.AgentDirEntry{}, syscall.EIO
		}

		fileNameLenInChars := fileNameBytes / 2
		var fileName string
		if fileNameLenInChars > 0 {
			fileNamePtr := unsafe.Pointer(uintptr(entryPtr) + unsafe.Offsetof(entry.FileName))
			fileNameSlice := unsafe.Slice((*uint16)(fileNamePtr), fileNameLenInChars)
			fileName = string(utf16.Decode(fileNameSlice))
		}

		// If filename is empty, skip this entry.
		if fileName == "" {
			if err := ds.advanceToNextEntry(entry); err != nil {
				return types.AgentDirEntry{}, err
			}
			continue
		}

		// Skip "." and "..".
		if fileName == "." || fileName == ".." {
			if err := ds.advanceToNextEntry(entry); err != nil {
				return types.AgentDirEntry{}, err
			}
			continue
		}

		// Skip entries with excluded attributes.
		if entry.FileAttributes&excludedAttrs != 0 {
			if err := ds.advanceToNextEntry(entry); err != nil {
				return types.AgentDirEntry{}, err
			}
			continue
		}

		mode := windowsAttributesToFileMode(entry.FileAttributes)
		fuseEntry := types.AgentDirEntry{
			Name: fileName,
			Mode: uint32(mode),
		}

		if err := ds.advanceToNextEntry(entry); err != nil {
			return types.AgentDirEntry{}, err
		}

		return fuseEntry, nil
	}
}

// Seekdir resets the directory stream position.
// Only an offset of 0 is supported; for off==0 the context cancellation is ignored.
// For any nonzero offset, the function immediately returns syscall.ENOSYS.
func (ds *SeekableDirStream) Seekdir(ctx context.Context, off uint64) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Check handle validity first
	if ds.handle == syscall.InvalidHandle {
		return syscall.EBADF
	}

	// For non-zero offsets, return ENOSYS
	if off != 0 {
		return syscall.ENOSYS
	}

	// For offset 0, proceed regardless of context state
	ds.currentOffset = 0
	ds.bytesInBuf = 0
	ds.eof = false
	ds.lastNtStatus = STATUS_SUCCESS

	err := ds.fillBuffer(true)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// Close releases the directory handle and returns the buffer to the pool.
func (ds *SeekableDirStream) Close() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.handle != syscall.InvalidHandle {
		ntClose.Call(uintptr(ds.handle))
		ds.handle = syscall.InvalidHandle
	}

	if ds.poolBuf && ds.buffer != nil {
		fileInfoPool.Put(ds.buffer)
	}
	ds.buffer = nil
	ds.poolBuf = false

	ds.currentOffset = 0
	ds.bytesInBuf = 0
	ds.eof = true
	ds.lastNtStatus = uintptr(syscall.EBADF)
}

// Releasedir releases the directory stream.
func (ds *SeekableDirStream) Releasedir(ctx context.Context, releaseFlags uint32) {
	ds.Close()
}
