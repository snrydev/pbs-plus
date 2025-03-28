package agentfs

import (
	"context"
	"errors" // Import errors package
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"testing"
	"time"

	"github.com/pbs-plus/pbs-plus/internal/agent/agentfs/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a standard test directory structure
func createTestDirStructure(t *testing.T, dir string) map[string]uint32 {
	t.Helper()
	// Store expected names and types (ModeDir or 0 for regular file)
	expected := make(map[string]uint32)

	// Files
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("test"), 0644))
	expected["file1.txt"] = 0 // CORRECT: Indicates regular file (no ModeDir bit)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file2.dat"), []byte("data"), 0600))
	expected["file2.dat"] = 0 // CORRECT: Indicates regular file

	// Directories
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir1"), 0755))
	expected["subdir1"] = uint32(os.ModeDir)

	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir2"), 0700))
	expected["subdir2"] = uint32(os.ModeDir)

	require.NoError(t, os.Mkdir(filepath.Join(dir, "empty_subdir"), 0777))
	expected["empty_subdir"] = uint32(os.ModeDir)

	return expected
}

func TestSeekableDirStream_CrossPlatform(t *testing.T) {
	tempDir := t.TempDir()
	expectedEntries := createTestDirStructure(t, tempDir)
	expectedNames := make([]string, 0, len(expectedEntries))
	for name := range expectedEntries {
		expectedNames = append(expectedNames, name)
	}
	sort.Strings(expectedNames) // For consistent comparison later

	ctx := context.Background()
	dummyHandleID := uint64(1)
	dummyFlags := uint32(0)
	dummyReleaseFlags := uint32(0)

	t.Run("OpenSuccess", func(t *testing.T) {
		stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
		require.NoError(t, err)
		require.NotNil(t, stream)
		assert.NotPanics(t, func() { stream.Close() })
	})

	t.Run("OpenNonExistent", func(t *testing.T) {
		nonExistentPath := filepath.Join(tempDir, "does_not_exist_dir")
		stream, err := OpendirHandle(dummyHandleID, nonExistentPath, dummyFlags)
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist) // Should work for both OS
		require.Nil(t, stream)
	})

	t.Run("OpenFileAsDir", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "file1.txt")
		stream, err := OpendirHandle(dummyHandleID, filePath, dummyFlags)
		require.Error(t, err)
		t.Logf("OpenFileAsDir error (%s): %v", runtime.GOOS, err)

		if runtime.GOOS == "linux" {
			// Explicitly check for os.PathError wrapping syscall.ENOTDIR
			var pathErr *os.PathError
			if assert.ErrorAs(t, err, &pathErr, "Error should be an *os.PathError on Linux") {
				assert.ErrorIs(t, pathErr.Err, syscall.ENOTDIR, "Underlying error should be ENOTDIR")
			}
			// Keep the original check as well, as it should ideally pass too
			assert.ErrorIs(t, err, syscall.ENOTDIR, "ErrorIs check for ENOTDIR")
		}
		// On Windows, just checking require.Error(t, err) is sufficient
		// as the specific error code might differ (e.g., STATUS_NOT_A_DIRECTORY)

		require.Nil(t, stream)
	})

	t.Run("ReadAllEntries", func(t *testing.T) {
		stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
		require.NoError(t, err)
		defer stream.Close()

		foundEntriesMap := make(map[string]types.AgentDirEntry)
		foundNames := []string{}
		count := 0
		for {
			entry, err := stream.Readdirent(ctx)
			if errors.Is(err, io.EOF) { // Use errors.Is for EOF check
				break
			}
			require.NoError(t, err, "Unexpected error during Readdirent")

			require.NotEqual(t, ".", entry.Name, "'.' should be skipped")
			require.NotEqual(t, "..", entry.Name, "'..' should be skipped")

			_, exists := foundEntriesMap[entry.Name]
			require.False(t, exists, "Duplicate entry found: %s", entry.Name)

			foundEntriesMap[entry.Name] = entry
			foundNames = append(foundNames, entry.Name)
			count++

			require.LessOrEqual(t, count, len(expectedEntries)+5, "Read too many entries, possible infinite loop")
		}

		assert.Equal(t, len(expectedEntries), len(foundEntriesMap), "Number of entries mismatch")

		sort.Strings(foundNames)
		assert.Equal(t, expectedNames, foundNames, "Entry names mismatch")

		for name, expectedType := range expectedEntries {
			foundEntry, ok := foundEntriesMap[name]
			assert.True(t, ok, "Expected entry not found in map: %s", name)
			if ok {
				// Compare only the directory type bit for cross-platform compatibility
				expectedIsDir := (expectedType & uint32(os.ModeDir)) != 0
				foundIsDir := (foundEntry.Mode & uint32(os.ModeDir)) != 0
				assert.Equal(t, expectedIsDir, foundIsDir, "Type mismatch for %s (ExpectedDir: %v, FoundDir: %v, FoundMode: %o)", name, expectedIsDir, foundIsDir, foundEntry.Mode)
			}
		}

		entry, err := stream.Readdirent(ctx)
		assert.ErrorIs(t, err, io.EOF, "Expected io.EOF after reading all entries")
		assert.Empty(t, entry, "Entry should be empty on io.EOF")

		entry, err = stream.Readdirent(ctx)
		assert.ErrorIs(t, err, io.EOF, "Expected io.EOF on subsequent call")
		assert.Empty(t, entry, "Entry should be empty on subsequent io.EOF")
	})

	t.Run("SeekToZero", func(t *testing.T) {
		stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
		require.NoError(t, err)
		defer stream.Close()

		firstEntry, err := stream.Readdirent(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, firstEntry.Name)

		err = stream.Seekdir(ctx, 0)
		require.NoError(t, err, "Seekdir(0) failed")

		count := 0
		readNamesAfterSeek := []string{}
		for {
			entry, err := stream.Readdirent(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			readNamesAfterSeek = append(readNamesAfterSeek, entry.Name)
			count++
			require.LessOrEqual(t, count, len(expectedEntries)+5)
		}
		assert.Equal(t, len(expectedEntries), count, "Entry count mismatch after seek 0")

		sort.Strings(readNamesAfterSeek)
		assert.Equal(t, expectedNames, readNamesAfterSeek, "Entry names mismatch after seek 0")

		_, err = stream.Readdirent(ctx)
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("SeekNonZero", func(t *testing.T) {
		stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
		require.NoError(t, err)
		defer stream.Close()

		err = stream.Seekdir(ctx, 123)
		require.Error(t, err)
		assert.ErrorIs(t, err, syscall.ENOSYS, "Expected ENOSYS for non-zero seek offset")
	})

	t.Run("OperationsAfterClose", func(t *testing.T) {
		stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
		require.NoError(t, err)

		stream.Releasedir(ctx, dummyReleaseFlags)

		_, err = stream.Readdirent(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, syscall.EBADF, "Expected EBADF on Readdirent after close")

		err = stream.Seekdir(ctx, 0)
		require.Error(t, err)
		assert.ErrorIs(t, err, syscall.EBADF, "Expected EBADF on Seekdir(0) after close")

		err = stream.Seekdir(ctx, 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, syscall.EBADF, "Expected EBADF on Seekdir(1) after close") // EBADF check likely happens first

		assert.NotPanics(t, func() { stream.Releasedir(ctx, dummyReleaseFlags) })
		assert.NotPanics(t, func() { stream.Close() })
	})

	t.Run("ContextCancellationReaddirent", func(t *testing.T) {
		stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
		require.NoError(t, err)
		defer stream.Close()

		ctxCancel, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = stream.Readdirent(ctxCancel)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("ContextTimeoutReaddirent", func(t *testing.T) {
		stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
		require.NoError(t, err)
		defer stream.Close()

		ctxTimeout, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(5 * time.Millisecond) // Ensure deadline passes

		_, err = stream.Readdirent(ctxTimeout)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("ContextCancellationSeekdir", func(t *testing.T) {
		stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
		require.NoError(t, err)
		defer stream.Close()

		ctxCancel, cancel := context.WithCancel(context.Background())
		cancel()

		err = stream.Seekdir(ctxCancel, 0)
		assert.NoError(t, err, "Seekdir(0) should not return context error")

		err = stream.Seekdir(ctxCancel, 1)
		assert.ErrorIs(t, err, syscall.ENOSYS, "Seekdir(1) should return ENOSYS, not context error")
	})
}
