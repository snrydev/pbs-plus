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
		mode |= 0644
	} else if mode&os.ModeDir != 0 {
		mode |= 0755
	}

	return uint32(mode)
}

var fileInfoPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 1024*1024)
	},
}

type SeekableDirStream struct {
	mu           sync.Mutex
	handle       syscall.Handle
	path         string
	buffer       []byte
	poolBuf      bool
	currBufOff   int
	bytesInBuf   uintptr
	eof          bool
	lastNtStatus uintptr
	position     uint64
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
	shareAccess := uintptr(FILE_SHARE_READ | FILE_SHARE_WRITE)
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
	defer func() {
		if ntStatus != STATUS_SUCCESS || ioStatusBlock.Information != FILE_OPENED {
			if handle != syscall.InvalidHandle {
				ntClose.Call(uintptr(handle))
			}
		}
	}()

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

	if ioStatusBlock.Information != FILE_OPENED {
		if handle != syscall.InvalidHandle {
			ntClose.Call(uintptr(handle))
		}
		return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: os.ErrNotExist}
	}

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
		handle:       handle,
		path:         path,
		buffer:       buffer,
		poolBuf:      true,
		currBufOff:   0,
		bytesInBuf:   0,
		eof:          false,
		lastNtStatus: STATUS_SUCCESS,
		position:     1,
	}

	return stream, nil
}

func (ds *SeekableDirStream) fillBuffer(restart bool) error {
	if ds == nil {
		return syscall.EBADF
	}
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

	ds.currBufOff = 0
	ds.bytesInBuf = 0

	var ioStatusBlock IoStatusBlock

	ntStatus, _, _ := ntQueryDirectoryFile.Call(
		uintptr(ds.handle),
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&ioStatusBlock)),
		uintptr(unsafe.Pointer(&ds.buffer[0])),
		uintptr(len(ds.buffer)),
		uintptr(1),
		uintptr(0),
		0,
		boolToUintptr(restart),
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

func (ds *SeekableDirStream) advanceToNextEntry(entry *FileDirectoryInformation) error {
	if ds == nil {
		return syscall.EBADF
	}
	nextOffsetDelta := int(entry.NextEntryOffset)
	if nextOffsetDelta <= 0 {
		ds.currBufOff = int(ds.bytesInBuf)
		return nil
	}
	nextAbsOffset := ds.currBufOff + nextOffsetDelta
	if nextAbsOffset <= ds.currBufOff || nextAbsOffset > int(ds.bytesInBuf) {
		ds.lastNtStatus = uintptr(syscall.EIO)
		ds.eof = true
		return syscall.EIO
	}
	ds.currBufOff = nextAbsOffset
	return nil
}

func (ds *SeekableDirStream) Readdirent(ctx context.Context) (types.AgentDirEntry, error) {
	if ds == nil {
		return types.AgentDirEntry{}, syscall.EBADF
	}
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
		if ds.currBufOff >= int(ds.bytesInBuf) {
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
			if ds.bytesInBuf == 0 || ds.currBufOff >= int(ds.bytesInBuf) {
				ds.eof = true
				return types.AgentDirEntry{}, io.EOF
			}
		}

		entryPtr := unsafe.Pointer(&ds.buffer[ds.currBufOff])
		entry := (*FileDirectoryInformation)(entryPtr)

		if ds.currBufOff+int(unsafe.Offsetof(entry.FileName)) > int(ds.bytesInBuf) {
			ds.lastNtStatus = uintptr(syscall.EIO)
			ds.eof = true
			return types.AgentDirEntry{}, syscall.EIO
		}

		// Filter out unwanted entries.
		if entry.FileAttributes&excludedAttrs != 0 {
			if err := ds.advanceToNextEntry(entry); err != nil {
				return types.AgentDirEntry{}, err
			}
			ds.position++
			continue
		}
		if entry.FileAttributes&(windows.FILE_ATTRIBUTE_SYSTEM|windows.FILE_ATTRIBUTE_HIDDEN) ==
			(windows.FILE_ATTRIBUTE_SYSTEM | windows.FILE_ATTRIBUTE_HIDDEN) {
			if err := ds.advanceToNextEntry(entry); err != nil {
				continue
			}
			ds.position++
			continue
		}

		fileNameBytes := entry.FileNameLength
		structBaseSize := int(unsafe.Offsetof(entry.FileName))
		if fileNameBytes > uint32(len(ds.buffer)) ||
			(ds.currBufOff+structBaseSize+int(fileNameBytes)) > int(ds.bytesInBuf) {
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
			if fileName == "." || fileName == ".." {
				if err := ds.advanceToNextEntry(entry); err != nil {
					return types.AgentDirEntry{}, err
				}
				ds.position++
				continue
			}
		} else {
			if fileName == "" {
				if err := ds.advanceToNextEntry(entry); err != nil {
					return types.AgentDirEntry{}, err
				}
				ds.position++
				continue
			}
		}

		mode := windowsAttributesToFileMode(entry.FileAttributes)
		fuseEntry := types.AgentDirEntry{
			Name: fileName,
			Mode: uint32(mode),
			Off:  ds.position, // use the current cookie
		}

		if err := ds.advanceToNextEntry(entry); err != nil {
			return types.AgentDirEntry{}, err
		}
		ds.position++ // increment the cookie for the next entry

		return fuseEntry, nil
	}
}

func (ds *SeekableDirStream) Seekdir(ctx context.Context, off uint64) error {
	if ds == nil {
		return syscall.EBADF
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.handle == syscall.InvalidHandle {
		return syscall.EBADF
	}

	// If seeking to the beginning (offset 0), reset everything.
	if off == 0 {
		ds.currBufOff = 0
		ds.bytesInBuf = 0
		ds.eof = false
		ds.lastNtStatus = STATUS_SUCCESS
		ds.position = 1 // reset cookie to 1

		err := ds.fillBuffer(true) // true to restart enumeration
		if errors.Is(err, io.EOF) {
			return nil // empty directory is not an error
		}
		return err
	}

	// For non-zero offsets, we restart and skip entries until the cookie equals off.
	ds.currBufOff = 0
	ds.bytesInBuf = 0
	ds.eof = false
	ds.lastNtStatus = STATUS_SUCCESS
	ds.position = 1 // reset cookie to 1

	err := ds.fillBuffer(true)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	// Skip entries until ds.position reaches the desired cookie.
	for ds.position < off {
		if ds.eof || ds.currBufOff >= int(ds.bytesInBuf) {
			if err := ds.fillBuffer(false); err != nil {
				if errors.Is(err, io.EOF) {
					// Reached end before the desired offset.
					return nil
				}
				return err
			}
		}

		entryPtr := unsafe.Pointer(&ds.buffer[ds.currBufOff])
		entry := (*FileDirectoryInformation)(entryPtr)

		if err := ds.advanceToNextEntry(entry); err != nil {
			return err
		}
		ds.position++
	}

	return nil
}

func (ds *SeekableDirStream) Close() {
	if ds == nil {
		return
	}
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

	ds.currBufOff = 0
	ds.bytesInBuf = 0
	ds.eof = true
	ds.lastNtStatus = uintptr(syscall.EBADF)
}

func (ds *SeekableDirStream) Releasedir(ctx context.Context, releaseFlags uint32) {
	if ds == nil {
		return
	}
	ds.Close()
}
