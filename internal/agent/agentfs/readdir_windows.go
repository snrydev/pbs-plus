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
)

// NTSTATUS Constants
const (
	STATUS_SUCCESS               = 0x00000000
	STATUS_NO_MORE_FILES         = 0x80000006
	STATUS_NO_SUCH_FILE          = 0xC000000F // Can occur during query?
	STATUS_OBJECT_NAME_NOT_FOUND = 0xC0000034
	STATUS_OBJECT_PATH_NOT_FOUND = 0xC000003A
	STATUS_ACCESS_DENIED         = 0xC0000022
	STATUS_NOT_A_DIRECTORY       = 0xC00000E3
	STATUS_INVALID_INFO_CLASS    = 0xC0000103 // Often seen when treating file as dir
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

type SeekableDirStream struct {
	mu            sync.Mutex
	handle        syscall.Handle // Use syscall.Handle for clarity
	path          string         // Store original path for error messages
	buffer        []byte         // Buffer for NtQueryDirectoryFile results
	poolBuf       bool           // Indicates if the buffer came from the pool
	currentOffset int            // Read offset within the current buffer
	bytesInBuf    uintptr        // Valid bytes in the buffer
	eof           bool           // True if NtQueryDirectoryFile returned STATUS_NO_MORE_FILES
	lastNtStatus  uintptr        // Last NTSTATUS from NtQueryDirectoryFile
}

// OpendirHandle opens a directory using NtCreateFile.
func OpendirHandle(handleId uint64, path string, flags uint32) (*SeekableDirStream, error) {
	ntPath := convertToNTPath(path)
	syslog.L.Info().WithMessage(fmt.Sprintf("[DEBUG OpendirHandle] Input path: %s, NT Path: %s", path, ntPath)).Write()

	pathUTF16, err := syscall.UTF16PtrFromString(ntPath)
	if err != nil {
		return nil, &os.PathError{Op: "UTF16PtrFromString", Path: path, Err: err}
	}

	var unicodeString UnicodeString
	// RtlInitUnicodeString is safe to call directly
	rtlInitUnicodeString.Call(uintptr(unsafe.Pointer(&unicodeString)), uintptr(unsafe.Pointer(pathUTF16)))

	var objectAttributes ObjectAttributes
	objectAttributes.Length = uint32(unsafe.Sizeof(objectAttributes))
	objectAttributes.ObjectName = &unicodeString
	objectAttributes.Attributes = OBJ_CASE_INSENSITIVE // Case-insensitive lookup

	var handle syscall.Handle
	var ioStatusBlock IoStatusBlock

	// DesiredAccess: List directory contents and synchronize (needed for synchronous I/O)
	desiredAccess := uintptr(FILE_LIST_DIRECTORY | syscall.SYNCHRONIZE)
	// ShareAccess: Allow others to read, write, or delete while open
	shareAccess := uintptr(FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE)
	// CreateOptions:
	// - FILE_DIRECTORY_FILE: Ensure we are opening a directory.
	// - FILE_SYNCHRONOUS_IO_NONALERT: Perform synchronous I/O.
	createOptions := uintptr(FILE_DIRECTORY_FILE | FILE_SYNCHRONOUS_IO_NONALERT)
	// CreateDisposition: Fail if the directory doesn't exist.
	createDisposition := uintptr(OPEN_EXISTING)

	// Call NtCreateFile
	// The first return value (r1) is the NTSTATUS code (uintptr).
	// The third return value (err) is from GetLastError, unreliable for NT APIs.
	ntStatus, _, _ := ntCreateFile.Call(
		uintptr(unsafe.Pointer(&handle)),           // Pointer to receive handle
		desiredAccess,                              // Access mask
		uintptr(unsafe.Pointer(&objectAttributes)), // Object attributes (path)
		uintptr(unsafe.Pointer(&ioStatusBlock)),    // Pointer to I/O status block
		0,                                          // AllocationSize (not used for opening existing)
		0,                                          // FileAttributes (not used for opening existing)
		shareAccess,                                // Share access
		createDisposition,                          // Create disposition (OPEN_EXISTING)
		createOptions,                              // Create options (directory, synchronous)
		0,                                          // EaBuffer (optional)
		0,                                          // EaLength (optional)
	)

	syslog.L.Info().WithMessage(fmt.Sprintf(
		"[DEBUG OpendirHandle] NtCreateFile result - path: %s, ntStatus: 0x%X, handle: 0x%X, ioStatusBlock.Status: 0x%X, ioStatusBlock.Information: 0x%X",
		path, ntStatus, handle, ioStatusBlock.Status, ioStatusBlock.Information,
	)).Write()

	// --- Primary Status Check ---
	if ntStatus != STATUS_SUCCESS {
		syslog.L.Info().WithMessage(fmt.Sprintf("[DEBUG OpendirHandle] NtCreateFile FAILED - path: %s, ntStatus: 0x%X", path, ntStatus)).Write()
		// Map common NTSTATUS codes to Go errors
		var goErr error
		switch ntStatus {
		case STATUS_OBJECT_NAME_NOT_FOUND, STATUS_OBJECT_PATH_NOT_FOUND:
			goErr = os.ErrNotExist
		case STATUS_ACCESS_DENIED:
			goErr = os.ErrPermission
		case STATUS_NOT_A_DIRECTORY, STATUS_INVALID_INFO_CLASS: // Treat file-as-dir error as ENOTDIR
			goErr = syscall.ENOTDIR
		default:
			// Return a generic error including the NTSTATUS code
			goErr = fmt.Errorf("NTSTATUS 0x%X", ntStatus)
		}
		return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: goErr}
	}

	// --- Secondary Checks for Success Case ---

	// FIX: Check IoStatusBlock.Information for OPEN_EXISTING success.
	// If NtCreateFile returns SUCCESS but Information is not FILE_OPENED,
	// it might indicate the non-existent path anomaly.
	if ioStatusBlock.Information != FILE_OPENED {
		syslog.L.Warn().WithMessage(fmt.Sprintf(
			"[DEBUG OpendirHandle] NtCreateFile SUCCESS but IoStatusBlock.Information != FILE_OPENED (is 0x%X) - path: %s. Treating as Not Exist.",
			ioStatusBlock.Information, path,
		)).Write()
		// Close the potentially problematic handle before returning error
		if handle != syscall.InvalidHandle {
			ntClose.Call(uintptr(handle))
		}
		return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: os.ErrNotExist}
	}

	// Check if the handle is valid
	if handle == syscall.InvalidHandle {
		syslog.L.Error(fmt.Errorf("NtCreateFile reported success (0x%X) but handle is invalid - path: %s", ntStatus, path)).Write()
		// No need to close InvalidHandle
		return nil, &os.PathError{Op: "NtCreateFile", Path: path, Err: errors.New("reported success but got invalid handle")}
	}

	// --- Success: Prepare Stream ---
	syslog.L.Info().WithMessage(fmt.Sprintf("[DEBUG OpendirHandle] NtCreateFile SUCCESS - path: %s, handle: 0x%X", path, handle)).Write()

	bufInterface := fileInfoPool.Get()
	buffer, ok := bufInterface.([]byte)
	if !ok || buffer == nil || len(buffer) == 0 {
		ntClose.Call(uintptr(handle)) // Clean up handle
		syslog.L.Error(fmt.Errorf("invalid buffer type or size from pool")).Write()
		if ok && buffer != nil { // Return invalid buffer if possible
			fileInfoPool.Put(buffer)
		}
		return nil, fmt.Errorf("failed to get valid buffer from pool")
	}

	stream := &SeekableDirStream{
		handle:        handle,
		path:          path, // Store path for context
		buffer:        buffer,
		poolBuf:       true,
		currentOffset: 0,
		bytesInBuf:    0,
		eof:           false,
		lastNtStatus:  STATUS_SUCCESS, // Initialize status
	}

	return stream, nil
}

// fillBuffer calls NtQueryDirectoryFile to populate the internal buffer.
// It returns an error (including io.EOF) or nil.
// Assumes the caller holds the mutex.
func (ds *SeekableDirStream) fillBuffer(restart bool) error {
	if ds.eof {
		return io.EOF // Already reached end
	}

	// Check handle validity *before* calling NtQueryDirectoryFile
	if ds.handle == syscall.InvalidHandle {
		ds.eof = true
		ds.lastNtStatus = uintptr(syscall.EBADF) // Use EBADF for status
		return syscall.EBADF
	}
	// Check buffer validity
	if ds.buffer == nil || len(ds.buffer) == 0 {
		ds.eof = true
		ds.lastNtStatus = uintptr(syscall.EFAULT) // Indicate internal buffer error
		return syscall.EFAULT
	}

	ds.currentOffset = 0
	ds.bytesInBuf = 0

	var ioStatusBlock IoStatusBlock

	// Call NtQueryDirectoryFile
	// r1 is the NTSTATUS code (uintptr)
	ntStatus, _, _ := ntQueryDirectoryFile.Call(
		uintptr(ds.handle),                      // Directory handle
		0,                                       // Event (optional)
		0,                                       // ApcRoutine (optional)
		0,                                       // ApcContext (optional)
		uintptr(unsafe.Pointer(&ioStatusBlock)), // I/O status block
		uintptr(unsafe.Pointer(&ds.buffer[0])),  // Buffer
		uintptr(len(ds.buffer)),                 // Buffer length
		uintptr(1),                              // FileInformationClass = FileDirectoryInformation
		uintptr(0),                              // ReturnSingleEntry = FALSE
		0,                                       // FileName (optional filter, NULL here)
		boolToUintptr(restart),                  // RestartScan
	)

	ds.lastNtStatus = ntStatus // Store the status regardless

	// --- Check NTSTATUS ---
	switch ntStatus {
	case STATUS_SUCCESS:
		ds.bytesInBuf = ioStatusBlock.Information
		if ds.bytesInBuf == 0 {
			// Success but no bytes read means end of directory
			ds.eof = true
			return io.EOF
		}
		ds.eof = false // We got data
		return nil     // Success

	case STATUS_NO_MORE_FILES:
		ds.eof = true
		ds.bytesInBuf = 0
		return io.EOF // End of directory reached

	default:
		// Any other status is an error
		ds.eof = true // Assume EOF on error
		ds.bytesInBuf = 0
		syslog.L.Error(fmt.Errorf("[DEBUG fillBuffer] NtQueryDirectoryFile FAIL - path: %s, ntStatus: 0x%X", ds.path, ntStatus)).Write()

		// Map to a Go error if possible, otherwise return a generic one
		switch ntStatus {
		case STATUS_NO_SUCH_FILE: // Can happen if dir deleted between calls?
			return os.ErrNotExist
		case STATUS_ACCESS_DENIED:
			return os.ErrPermission
		default:
			// Use syscall.Errno for generic OS errors, mapping NTSTATUS might lose info
			// Consider returning a custom error type wrapping the NTSTATUS
			return fmt.Errorf("NtQueryDirectoryFile failed: NTSTATUS 0x%X", ntStatus)
		}
	}
}

// Readdirent reads the next directory entry.
func (ds *SeekableDirStream) Readdirent(ctx context.Context) (types.AgentDirEntry, error) {
	// Check context cancellation first
	select {
	case <-ctx.Done():
		return types.AgentDirEntry{}, ctx.Err()
	default:
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Check handle validity
	if ds.handle == syscall.InvalidHandle {
		return types.AgentDirEntry{}, syscall.EBADF
	}

	for { // Loop to handle buffer refills and skipping entries
		// Check if buffer needs refilling or if we previously hit EOF/error
		if ds.currentOffset >= int(ds.bytesInBuf) {
			if ds.eof {
				return types.AgentDirEntry{}, io.EOF
			}
			// If last operation had an error status (other than SUCCESS/NO_MORE_FILES)
			if ds.lastNtStatus != STATUS_SUCCESS && ds.lastNtStatus != STATUS_NO_MORE_FILES {
				// Return the error derived from the last status
				// This assumes fillBuffer already mapped it appropriately
				return types.AgentDirEntry{}, fmt.Errorf("previous NtQueryDirectoryFile failed: NTSTATUS 0x%X", ds.lastNtStatus)
			}

			// Try to fill the buffer
			fillErr := ds.fillBuffer(false) // restart = false
			if fillErr != nil {
				// fillBuffer returns io.EOF or another error
				return types.AgentDirEntry{}, fillErr
			}
			// If fillBuffer succeeded but returned no data (should be caught by fillErr == io.EOF)
			if ds.bytesInBuf == 0 || ds.currentOffset >= int(ds.bytesInBuf) {
				ds.eof = true // Mark EOF just in case
				return types.AgentDirEntry{}, io.EOF
			}
		}

		// --- Parse entry from buffer ---
		entryPtr := unsafe.Pointer(&ds.buffer[ds.currentOffset])
		entry := (*FileDirectoryInformation)(entryPtr)

		// --- Sanity Checks ---
		// Check if basic struct fits
		if ds.currentOffset+int(unsafe.Offsetof(entry.FileName)) > int(ds.bytesInBuf) {
			ds.lastNtStatus = uintptr(syscall.EIO)    // Indicate corruption
			ds.eof = true                             // Stop further reads
			return types.AgentDirEntry{}, syscall.EIO // Buffer underflow/corruption
		}
		// Check if FileNameLength is plausible
		fileNameBytes := entry.FileNameLength
		structBaseSize := int(unsafe.Offsetof(entry.FileName))
		if fileNameBytes > uint32(len(ds.buffer)) || (ds.currentOffset+structBaseSize+int(fileNameBytes)) > int(ds.bytesInBuf) {
			ds.lastNtStatus = uintptr(syscall.EIO)    // Indicate corruption
			ds.eof = true                             // Stop further reads
			return types.AgentDirEntry{}, syscall.EIO // Invalid data or exceeds buffer
		}

		// --- Extract Filename ---
		fileNameLenInChars := fileNameBytes / 2 // UTF-16 characters (WCHAR)
		fileName := ""
		if fileNameLenInChars > 0 {
			// Calculate pointer to the start of the filename
			fileNamePtr := unsafe.Pointer(uintptr(entryPtr) + unsafe.Offsetof(entry.FileName))
			// Create a slice of uint16 covering the filename
			fileNameSlice := unsafe.Slice((*uint16)(fileNamePtr), fileNameLenInChars)
			fileName = string(utf16.Decode(fileNameSlice))
		} else {
			// Zero-length filename? This shouldn't happen for valid entries. Skip it.
			// Log this? syslog.L.Warn()...("Zero-length filename encountered")
			goto nextEntry // Use goto for clarity in skipping logic
		}

		// --- Skip "." and ".." ---
		if fileName == "." || fileName == ".." {
			goto nextEntry
		}

		// --- Skip entries with excluded attributes ---
		// (e.g., reparse points, offline files, etc.)
		if entry.FileAttributes&excludedAttrs != 0 {
			goto nextEntry
		}

		// --- Prepare AgentDirEntry ---
		{
			mode := windowsAttributesToFileMode(entry.FileAttributes)
			fuseEntry := types.AgentDirEntry{
				Name: fileName,
				Mode: uint32(mode), // Convert os.FileMode to uint32
				// Ino, Size, etc. could be populated if needed from 'entry'
			}

			// --- Advance Offset for Next Read ---
			nextOffsetDelta := int(entry.NextEntryOffset)
			if nextOffsetDelta <= 0 {
				// This is the last entry in the *current* buffer
				ds.currentOffset = int(ds.bytesInBuf)
			} else {
				nextAbsOffset := ds.currentOffset + nextOffsetDelta
				// Sanity check the next offset
				if nextAbsOffset <= ds.currentOffset || nextAbsOffset > int(ds.bytesInBuf) {
					ds.lastNtStatus = uintptr(syscall.EIO) // Corruption
					ds.eof = true
					return types.AgentDirEntry{}, syscall.EIO // Invalid next offset
				}
				ds.currentOffset = nextAbsOffset
			}

			// Successfully parsed and filtered, return the entry
			return fuseEntry, nil
		}

	nextEntry:
		// --- Advance Offset if Skipping ---
		nextOffsetDelta := int(entry.NextEntryOffset)
		if nextOffsetDelta <= 0 {
			ds.currentOffset = int(ds.bytesInBuf)
		} else {
			nextAbsOffset := ds.currentOffset + nextOffsetDelta
			if nextAbsOffset <= ds.currentOffset || nextAbsOffset > int(ds.bytesInBuf) {
				ds.lastNtStatus = uintptr(syscall.EIO) // Corruption
				ds.eof = true
				return types.AgentDirEntry{}, syscall.EIO // Invalid next offset
			}
			ds.currentOffset = nextAbsOffset
		}
		// Loop back to process the next entry or refill buffer
	}
}

// Seekdir resets the directory stream position. Only offset 0 is supported.
func (ds *SeekableDirStream) Seekdir(ctx context.Context, off uint64) error {
	// Context check isn't strictly necessary for Seek(0) as it's fast,
	// but good practice. Seek(>0) fails immediately anyway.
	select {
	case <-ctx.Done():
		return ctx.Err() // Return context error if already cancelled
	default:
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Check handle validity
	if ds.handle == syscall.InvalidHandle {
		return syscall.EBADF
	}

	if off == 0 {
		// Reset internal state *before* calling fillBuffer
		ds.currentOffset = 0
		ds.bytesInBuf = 0
		ds.eof = false
		ds.lastNtStatus = STATUS_SUCCESS // Reset status before query

		// Call fillBuffer to restart the scan and get the first batch
		err := ds.fillBuffer(true) // restart = true

		// Seekdir(0) is successful even if the directory is empty (fillBuffer returns io.EOF)
		if errors.Is(err, io.EOF) {
			return nil
		}
		// Return any other error encountered during the initial query
		return err
	}

	// Seeking to non-zero offsets is not supported by NtQueryDirectoryFile directly
	return syscall.ENOSYS
}

// Close releases the directory handle and returns the buffer to the pool.
func (ds *SeekableDirStream) Close() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.handle != syscall.InvalidHandle {
		ntClose.Call(uintptr(ds.handle))
		ds.handle = syscall.InvalidHandle // Mark as closed
	}

	// Return buffer to pool if it came from there
	if ds.poolBuf && ds.buffer != nil {
		// Optional: Zero the buffer before returning? Might not be necessary.
		// for i := range ds.buffer { ds.buffer[i] = 0 }
		fileInfoPool.Put(ds.buffer)
	}
	ds.buffer = nil // Ensure buffer is nil after close
	ds.poolBuf = false

	// Reset state
	ds.currentOffset = 0
	ds.bytesInBuf = 0
	ds.eof = true                            // Mark as EOF on close
	ds.lastNtStatus = uintptr(syscall.EBADF) // Set status to indicate closed state
}

// Releasedir is typically called when the directory is no longer needed by the filesystem.
func (ds *SeekableDirStream) Releasedir(ctx context.Context, releaseFlags uint32) {
	ds.Close() // Just call Close
}
