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
	syslog.L.Info().WithMessage(fmt.Sprintf("[DEBUG OpendirHandle] Input path: %s, NT Path: %s\n", path, ntPath)).Write()

	pathUTF16, err := syscall.UTF16PtrFromString(ntPath)
	if err != nil {
		syslog.L.Error(fmt.Errorf("UTF16PtrFromString error: %v\n", err)).Write()
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
	shareAccess := uintptr(FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE)
	createOptions := uintptr(FILE_DIRECTORY_FILE | FILE_SYNCHRONOUS_IO_NONALERT)

	status, _, errnoSyscall := ntCreateFile.Call(
		uintptr(unsafe.Pointer(&handle)),
		desiredAccess,
		uintptr(unsafe.Pointer(&objectAttributes)),
		uintptr(unsafe.Pointer(&ioStatusBlock)),
		0, // AllocationSize
		0, // FileAttributes
		shareAccess,
		OPEN_EXISTING,
		createOptions,
		0, // EaBuffer
		0, // EaLength
	)

	syslog.L.Info().WithMessage(fmt.Sprintf("[DEBUG OpendirHandle] NtCreateFile result - path: %s, status: 0x%X, handle: 0x%X, ioStatusBlock.Status: 0x%X, syscallErr: %v\n",
		path, status, handle, ioStatusBlock.Status, errnoSyscall)).Write()

	// Windows NT API can have status=0 but still have error in IoStatusBlock
	if status != STATUS_SUCCESS || ioStatusBlock.Status != 0 {
		syslog.L.Info().WithMessage(fmt.Sprintf("[DEBUG OpendirHandle] NtCreateFile FAILED - path: %s, status: 0x%X, ioStatusBlock.Status: 0x%X\n",
			path, status, ioStatusBlock.Status)).Write()

		// Use the most specific status for error
		effectiveStatus := status
		if ioStatusBlock.Status != 0 {
			effectiveStatus = uintptr(ioStatusBlock.Status)
		}

		ntStatusErr := fmt.Errorf("NTSTATUS 0x%X", effectiveStatus)
		switch effectiveStatus {
		case 0xC0000034, // STATUS_OBJECT_NAME_NOT_FOUND
			0xC000003A: // STATUS_OBJECT_PATH_NOT_FOUND
			ntStatusErr = os.ErrNotExist
		case 0xC0000022: // STATUS_ACCESS_DENIED
			ntStatusErr = os.ErrPermission
		case 0xC00000E3, // STATUS_NOT_A_DIRECTORY
			0xC0000103: // STATUS_FILE_IS_A_DIRECTORY (used when opening a file as dir)
			ntStatusErr = syscall.ENOTDIR
		}
		return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: ntStatusErr}
	}

	if handle == 0 || handle == uintptr(syscall.InvalidHandle) {
		syslog.L.Error(fmt.Errorf("NtCreateFile reported success (0x%X) but handle is invalid (0x%X) - path: %s\n", status, handle, path)).Write()
		return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: fmt.Errorf("reported success but got invalid handle")}
	}

	// If status is SUCCESS and handle is valid, proceed...
	syslog.L.Info().WithMessage(fmt.Sprintf("[DEBUG OpendirHandle] NtCreateFile SUCCESS - path: %s, handle: 0x%X\n", path, handle)).Write()

	bufInterface := fileInfoPool.Get()
	buffer, ok := bufInterface.([]byte)
	if !ok {
		// Clean up the handle if buffer allocation fails
		ntClose.Call(handle)
		syslog.L.Error(fmt.Errorf("Invalid buffer type from pool"))
		return nil, fmt.Errorf("invalid buffer type from pool")
	}

	stream := &SeekableDirStream{
		handle:        handle,
		buffer:        buffer,
		poolBuf:       true,
		currentOffset: 0,
		bytesInBuf:    0,
		eof:           false,
		lastStatus:    0, // Initialize lastStatus to 0 (no error)
	}

	return stream, nil
}

// Ensure fillBuffer uses the direct uintptr return for status checking
func (ds *SeekableDirStream) fillBuffer(restart bool) error {
	// Remove ds.mu.Lock() and ds.mu.Unlock() from here if callers always hold the lock

	if ds.eof {
		return nil // Already reached end
	}

	// Check handle validity *before* calling NtQueryDirectoryFile
	if ds.handle == 0 || ds.handle == uintptr(syscall.InvalidHandle) {
		ds.eof = true // Mark as EOF if handle is bad
		ds.lastStatus = syscall.EBADF
		return syscall.EBADF
	}
	// Check buffer validity
	if ds.buffer == nil {
		ds.eof = true
		ds.lastStatus = syscall.EFAULT // Or EBADF, indicates internal error
		return ds.lastStatus
	}
	if len(ds.buffer) == 0 {
		ds.eof = true
		ds.lastStatus = syscall.EFAULT // Buffer is unusable
		return ds.lastStatus
	}

	ds.currentOffset = 0
	ds.bytesInBuf = 0

	// We still need ioStatusBlock for the Information field (bytes read)
	var ioStatusBlock IoStatusBlock

	// Make the call - capture the direct status return (r1 uintptr)
	// The third return value (syscall.Errno) is less reliable here than r1.
	status_r1, _, _ := ntQueryDirectoryFile.Call(
		ds.handle,
		0,                                       // Event
		0,                                       // ApcRoutine
		0,                                       // ApcContext
		uintptr(unsafe.Pointer(&ioStatusBlock)), // Still needed for Information
		uintptr(unsafe.Pointer(&ds.buffer[0])),  // Access buffer[0] safely now
		uintptr(len(ds.buffer)),
		uintptr(1), // FileDirectoryInformation class
		uintptr(0), // ReturnSingleEntry = FALSE
		0,          // FileName = NULL
		uintptr(boolToInt(restart)),
	)

	// --- Use status_r1 (uintptr) for reliable status checking ---

	// Cast uintptr to uint32 for direct comparison with NTSTATUS constants
	statusValue := uint32(status_r1)

	// Check for STATUS_NO_MORE_FILES using the reliable statusValue
	if statusValue == STATUS_NO_MORE_FILES {
		ds.eof = true
		ds.bytesInBuf = 0
		ds.lastStatus = 0 // Not an error
		return nil        // Correctly signifies end of directory
	}

	// Check for any other non-success status
	if statusValue != STATUS_SUCCESS { // STATUS_SUCCESS is 0
		ds.eof = true // Assume EOF on error
		ds.bytesInBuf = 0
		// Store the error code. ds.lastStatus is syscall.Errno (uintptr), so direct assignment is okay.
		ds.lastStatus = syscall.Errno(status_r1)
		fmt.Printf("[DEBUG fillBuffer] NtQueryDirectoryFile FAIL - statusValue: 0x%X, mapped errno: %v\n", statusValue, ds.lastStatus)
		return ds.lastStatus
	}

	// --- If we reach here, statusValue was STATUS_SUCCESS ---

	// Get the number of bytes read from the IoStatusBlock
	ds.bytesInBuf = ioStatusBlock.Information
	ds.lastStatus = 0 // Reset last error
	ds.eof = false    // We got data

	// If status was SUCCESS but no bytes were returned, it implies end of directory.
	// This can happen if the directory becomes empty between calls, or if the buffer
	// was too small for even one entry (though we use a large buffer).
	if ds.bytesInBuf == 0 {
		ds.eof = true
		return nil // Treat as EOF condition
	}

	return nil
}

func (ds *SeekableDirStream) Readdirent(ctx context.Context) (types.AgentDirEntry, error) {
	select {
	case <-ctx.Done():
		return types.AgentDirEntry{}, ctx.Err()
	default:
	}

	ds.mu.Lock()
	// Check handle FIRST
	if ds.handle == 0 || ds.handle == uintptr(syscall.InvalidHandle) {
		ds.mu.Unlock()
		return types.AgentDirEntry{}, syscall.EBADF
	}

	// Check if buffer needs refilling or if we hit EOF/error
	if ds.currentOffset >= int(ds.bytesInBuf) {
		// If we are at the end of the buffer, check lastStatus and eof
		if ds.lastStatus != 0 {
			err := ds.lastStatus
			// ds.lastStatus = 0 // Don't consume error here, let hasNext/next handle it? No, Readdirent should return it.
			ds.mu.Unlock()
			return types.AgentDirEntry{}, err
		}
		if ds.eof {
			ds.mu.Unlock()
			return types.AgentDirEntry{}, io.EOF
		}

		// If buffer is empty but no EOF and no prior error, try filling
		fillErr := ds.fillBuffer(false) // fillBuffer now returns error
		if fillErr != nil {
			// fillBuffer already set ds.lastStatus and potentially ds.eof
			ds.mu.Unlock()
			// If fillErr was EOF, return EOF. Otherwise return the error.
			if errors.Is(fillErr, io.EOF) || (ds.eof && ds.lastStatus == 0) { // Check ds.eof too
				return types.AgentDirEntry{}, io.EOF
			}
			return types.AgentDirEntry{}, fillErr // Return the actual error from fillBuffer
		}

		// After filling, check again if we have data
		if ds.currentOffset >= int(ds.bytesInBuf) {
			// If fillBuffer succeeded but we still have no data, it must be EOF
			if ds.eof {
				ds.mu.Unlock()
				return types.AgentDirEntry{}, io.EOF
			}
			// Should not happen if fillBuffer logic is correct
			ds.lastStatus = syscall.EIO // Unexpected state
			ds.mu.Unlock()
			return types.AgentDirEntry{}, syscall.EIO
		}
	}

	// --- If we have data in the buffer, proceed to parse (logic from next()) ---

	entryPtr := unsafe.Pointer(&ds.buffer[ds.currentOffset])
	entry := (*FileDirectoryInformation)(entryPtr)

	// Basic sanity check
	if ds.currentOffset+int(unsafe.Offsetof(entry.FileName)) > int(ds.bytesInBuf) {
		ds.lastStatus = syscall.EIO
		ds.mu.Unlock()
		return types.AgentDirEntry{}, syscall.EIO // Buffer underflow/corruption
	}

	// Validate FileNameLength before using it
	fileNameBytes := entry.FileNameLength
	if fileNameBytes > uint32(len(ds.buffer)) || (ds.currentOffset+int(unsafe.Offsetof(entry.FileName))+int(fileNameBytes)) > int(ds.bytesInBuf) {
		ds.lastStatus = syscall.EIO
		ds.mu.Unlock()
		return types.AgentDirEntry{}, syscall.EIO // Invalid data or exceeds buffer
	}

	// Extract filename
	fileNameLenInChars := fileNameBytes / 2 // UTF-16 characters
	fileName := ""
	if fileNameLenInChars > 0 {
		fileNamePtr := unsafe.Pointer(uintptr(entryPtr) + unsafe.Offsetof(entry.FileName))
		fileNameSlice := unsafe.Slice((*uint16)(fileNamePtr), fileNameLenInChars)
		fileName = string(utf16.Decode(fileNameSlice))
	} else if fileNameBytes == 0 && entry.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		// Allow zero length for non-directories? Unlikely. Treat as error or skip?
		// Let's treat as error for now.
		ds.lastStatus = syscall.EIO
		ds.mu.Unlock()
		return types.AgentDirEntry{}, syscall.EIO // Zero length filename
	}

	// Calculate next entry offset and advance currentOffset
	nextOffsetDelta := int(entry.NextEntryOffset)
	nextAbsOffset := ds.currentOffset // Default if NextEntryOffset is 0

	if nextOffsetDelta < 0 {
		ds.lastStatus = syscall.EIO
		ds.mu.Unlock()
		return types.AgentDirEntry{}, syscall.EIO // Invalid offset
	} else if nextOffsetDelta > 0 {
		nextAbsOffset = ds.currentOffset + nextOffsetDelta
		// Check if the next offset is valid within the buffer
		if nextAbsOffset <= ds.currentOffset || nextAbsOffset > int(ds.bytesInBuf) {
			ds.lastStatus = syscall.EIO
			ds.mu.Unlock()
			return types.AgentDirEntry{}, syscall.EIO // Invalid next position
		}
	} else {
		// NextEntryOffset is 0, this is the last entry in the buffer
		nextAbsOffset = int(ds.bytesInBuf)
	}
	ds.currentOffset = nextAbsOffset // Advance offset

	// Skip "." and ".." entries
	if fileName == "." || fileName == ".." {
		ds.mu.Unlock()            // Unlock before recursive call
		return ds.Readdirent(ctx) // Tail call optimization unlikely, potential stack depth issue?
	}

	// Skip entries with excluded attributes
	if entry.FileAttributes&excludedAttrs != 0 {
		ds.mu.Unlock() // Unlock before recursive call
		return ds.Readdirent(ctx)
	}

	// Convert attributes to file mode
	mode := windowsAttributesToFileMode(entry.FileAttributes)

	fuseEntry := types.AgentDirEntry{
		Name: fileName,
		Mode: mode,
	}

	ds.lastStatus = 0 // Reset last error status on successful read *of this entry*
	ds.mu.Unlock()
	return fuseEntry, nil
}

func (ds *SeekableDirStream) Seekdir(ctx context.Context, off uint64) error {
	ds.mu.Lock()
	// Check handle FIRST
	if ds.handle == 0 || ds.handle == uintptr(syscall.InvalidHandle) {
		ds.mu.Unlock()
		return syscall.EBADF
	}

	if off == 0 {
		// Reset state
		ds.currentOffset = 0
		ds.bytesInBuf = 0
		ds.eof = false
		ds.lastStatus = 0
		// Call fillBuffer which now returns error and checks handle/buffer
		err := ds.fillBuffer(true) // restart scan
		ds.mu.Unlock()
		// If fillBuffer returned EOF, Seekdir(0) is successful.
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err // Return actual error from fillBuffer or nil if it was successful
	}

	// Seeking to arbitrary offsets is not supported
	ds.mu.Unlock()
	return syscall.ENOSYS
}

func (ds *SeekableDirStream) Close() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.handle != 0 && ds.handle != uintptr(syscall.InvalidHandle) {
		ntClose.Call(ds.handle)
	}
	// Mark handle as invalid *regardless* of whether ntClose succeeded
	ds.handle = uintptr(syscall.InvalidHandle) // Use InvalidHandle explicitly

	if ds.poolBuf && ds.buffer != nil {
		// Optionally clear buffer before putting back? Maybe not necessary.
		fileInfoPool.Put(ds.buffer)
	}
	ds.buffer = nil // Ensure buffer is nil after close
	ds.poolBuf = false

	// Reset state
	ds.currentOffset = 0
	ds.bytesInBuf = 0
	ds.eof = true                 // Mark as EOF on close
	ds.lastStatus = syscall.EBADF // Set last status to indicate closed state
}

// Releasedir just calls Close
func (ds *SeekableDirStream) Releasedir(ctx context.Context, releaseFlags uint32) {
	ds.Close()
}
