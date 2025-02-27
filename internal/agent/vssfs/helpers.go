//go:build windows

package vssfs

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/sonroyaalmerol/pbs-plus/internal/arpc"
	"github.com/sonroyaalmerol/pbs-plus/internal/syslog"
	"github.com/sonroyaalmerol/pbs-plus/internal/utils"
	"github.com/valyala/fastjson"
	"golang.org/x/sys/windows"
)

func skipPathWithAttributes(attrs uint32) bool {
	return attrs&(windows.FILE_ATTRIBUTE_REPARSE_POINT|
		windows.FILE_ATTRIBUTE_DEVICE|
		windows.FILE_ATTRIBUTE_OFFLINE|
		windows.FILE_ATTRIBUTE_VIRTUAL|
		windows.FILE_ATTRIBUTE_RECALL_ON_OPEN|
		windows.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS) != 0
}

func mapWinError(err error, path string) error {
	switch err {
	case windows.ERROR_FILE_NOT_FOUND:
		return os.ErrNotExist
	case windows.ERROR_PATH_NOT_FOUND:
		return os.ErrNotExist
	case windows.ERROR_ACCESS_DENIED:
		return os.ErrPermission
	default:
		return &os.PathError{
			Op:   "access",
			Path: path,
			Err:  err,
		}
	}
}

func createFileInfoFromFindData(name string, fd *windows.Win32finddata) *VSSFileInfo {
	var mode fs.FileMode
	var isDir bool

	// Set base permissions
	if fd.FileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0 {
		mode = 0444 // Read-only for everyone
	} else {
		mode = 0666 // Read-write for everyone
	}

	// Add directory flag and execute permissions
	if fd.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		mode |= os.ModeDir | 0111 // Add execute bits for traversal
		isDir = true
		// Set directory-specific permissions
		mode = (mode & 0666) | 0111 | os.ModeDir // Final mode: drwxr-xr-x
	}

	size := int64(fd.FileSizeHigh)<<32 + int64(fd.FileSizeLow)
	modTime := time.Unix(0, fd.LastWriteTime.Nanoseconds())

	return &VSSFileInfo{
		Name:    name,
		Size:    size,
		Mode:    mode,
		ModTime: modTime.Unix(),
		IsDir:   isDir,
	}
}

func createFileInfoFromHandleInfo(path string, fd *windows.ByHandleFileInformation) *VSSFileInfo {
	var mode fs.FileMode
	var isDir bool

	// Set base permissions
	if fd.FileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0 {
		mode = 0444 // Read-only for everyone
	} else {
		mode = 0666 // Read-write for everyone
	}

	// Add directory flag and execute permissions
	if fd.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		mode |= os.ModeDir | 0111 // Add execute bits for traversal
		isDir = true
		// Set directory-specific permissions
		mode = (mode & 0666) | 0111 | os.ModeDir // Final mode: drwxr-xr-x
	}

	size := int64(fd.FileSizeHigh)<<32 + int64(fd.FileSizeLow)
	modTime := time.Unix(0, fd.LastWriteTime.Nanoseconds())

	return &VSSFileInfo{
		Name:    filepath.Base(path),
		Size:    size,
		Mode:    mode,
		ModTime: modTime.Unix(),
		IsDir:   isDir,
	}
}

// encodeJsonValue builds a JSON value using only the fastjson API.
func encodeJsonValue(v interface{}) *fastjson.Value {
	arena := new(fastjson.Arena)
	var value *fastjson.Value

	switch val := v.(type) {
	case string:
		value = arena.NewString(val)
	case int:
		value = arena.NewNumberInt(val)
	case int64:
		value = arena.NewNumberInt(int(val))
	case bool:
		if val {
			value = arena.NewTrue()
		} else {
			value = arena.NewFalse()
		}
	case uint64:
		value = arena.NewNumberFloat64(float64(val))
	case map[string]interface{}:
		obj := arena.NewObject()
		for key, v2 := range val {
			obj.Set(key, encodeJsonValue(v2))
		}
		value = obj
	case map[string]string:
		obj := arena.NewObject()
		for key, v2 := range val {
			obj.Set(key, arena.NewString(v2))
		}
		value = obj
	case map[string]uint64:
		obj := arena.NewObject()
		for key, v2 := range val {
			obj.Set(key, arena.NewNumberFloat64(float64(v2)))
		}
		value = obj
	case *utils.FSStat:
		obj := arena.NewObject()
		obj.Set("total_size", arena.NewNumberInt(int(val.TotalSize)))
		obj.Set("free_size", arena.NewNumberInt(int(val.FreeSize)))
		obj.Set("available_size", arena.NewNumberInt(int(val.AvailableSize)))
		obj.Set("total_files", arena.NewNumberInt(val.TotalFiles))
		obj.Set("free_files", arena.NewNumberInt(val.FreeFiles))
		obj.Set("available_files", arena.NewNumberInt(val.AvailableFiles))
		// Represent CacheHint as a number (nanoseconds)
		obj.Set("cache_hint", arena.NewNumberInt(int(val.CacheHint)))
		value = obj
	case *arpc.SerializableError:
		obj := arena.NewObject()
		obj.Set("error_type", arena.NewString(val.ErrorType))
		obj.Set("message", arena.NewString(val.Message))
		if val.Op != "" {
			obj.Set("op", arena.NewString(val.Op))
		}
		if val.Path != "" {
			obj.Set("path", arena.NewString(val.Path))
		}
		value = obj
	case VSSFileInfo:
		obj := arena.NewObject()
		obj.Set("name", arena.NewString(val.Name))
		obj.Set("size", arena.NewNumberInt(int(val.Size)))
		// Convert fs.FileMode to int
		obj.Set("mode", arena.NewNumberInt(int(val.Mode)))
		obj.Set("modTime", arena.NewNumberInt(int(val.ModTime)))
		if val.IsDir {
			obj.Set("isDir", arena.NewTrue())
		} else {
			obj.Set("isDir", arena.NewFalse())
		}
		value = obj
	case *VSSFileInfo:
		obj := arena.NewObject()
		obj.Set("name", arena.NewString(val.Name))
		obj.Set("size", arena.NewNumberInt(int(val.Size)))
		obj.Set("mode", arena.NewNumberInt(int(val.Mode)))
		obj.Set("modTime", arena.NewNumberInt(int(val.ModTime)))
		if val.IsDir {
			obj.Set("isDir", arena.NewTrue())
		} else {
			obj.Set("isDir", arena.NewFalse())
		}
		value = obj
	case []byte:
		// Encode the []byte as a base64 string so that clients unmarshaling
		// into a []byte field correctly decode it.
		encoded := base64.StdEncoding.EncodeToString(val)
		value = arena.NewString(encoded)
	default:
		// Handle slice types (e.g. []*VSSFileInfo) via reflection.
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice {
			arr := arena.NewArray()
			for i := 0; i < rv.Len(); i++ {
				arr.SetArrayItem(i, encodeJsonValue(rv.Index(i).Interface()))
			}
			value = arr
		} else {
			value = arena.NewString(fmt.Sprintf("%v", v))
		}
	}

	return value
}

// --- Error Response Helpers ---

func (s *VSSFSServer) respondError(method, drive string, err error) arpc.Response {
	if syslog.L != nil && err != os.ErrNotExist {
		syslog.L.Errorf("%s (%s): %v", method, drive, err)
	}
	// Wrap error and encode it using our new JSON encoder.
	return arpc.Response{
		Status: 500,
		Data:   encodeJsonValue(arpc.WrapError(err)),
	}
}

func (s *VSSFSServer) invalidRequest(method, drive string, err error) arpc.Response {
	if syslog.L != nil {
		syslog.L.Errorf("%s (%s): %v", method, drive, err)
	}
	return arpc.Response{
		Status: 400,
		Data:   encodeJsonValue(arpc.WrapError(os.ErrInvalid)),
	}
}

// --- Helper: fastjson decoding for request payloads ---

// getStringField safely extracts a string field from req.Payload.
func getStringField(v *fastjson.Value, field string) (string, error) {
	f := v.Get(field)
	if f == nil {
		return "", fmt.Errorf("field %s missing", field)
	}
	return string(f.GetStringBytes()), nil
}

// getIntField extracts an integer field from req.Payload.
func getIntField(v *fastjson.Value, field string) (int, error) {
	f := v.Get(field)
	if f == nil {
		return 0, fmt.Errorf("field %s missing", field)
	}
	return f.GetInt(), nil
}
