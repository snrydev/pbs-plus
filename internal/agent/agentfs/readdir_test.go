package agentfs

import (
	"context"
	"errors" // Import errors package
	"fmt"    // Added for large dir test
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv" // Added for large dir test
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

		// Read first entry and get its position
		firstEntry, err := stream.Readdirent(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, firstEntry.Name)
		firstPos := firstEntry.Off
		require.NotZero(t, firstPos, "Entry position should not be zero")

		// Read second entry
		secondEntry, err := stream.Readdirent(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, secondEntry.Name)

		// Seek back to first entry position
		err = stream.Seekdir(ctx, firstPos)
		require.NoError(t, err, "Seekdir to first entry position failed")

		// Read entry after seek - should match first entry
		entryAfterSeek, err := stream.Readdirent(ctx)
		require.NoError(t, err)
		assert.Equal(t, firstEntry.Name, entryAfterSeek.Name, "Entry after seek should match first entry")

		// Seek to invalid position
		err = stream.Seekdir(ctx, 999999)
		require.NoError(t, err, "Seekdir to invalid position should not error")

		// Reading after seeking to invalid position should return EOF
		_, err = stream.Readdirent(ctx)
		assert.ErrorIs(t, err, io.EOF, "Expected EOF after seeking to invalid position")
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
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled, "Seekdir should respect context cancellation")
	})

	t.Run("EntryPositionsAreConsistent", func(t *testing.T) {
		stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
		require.NoError(t, err)
		defer stream.Close()

		// Read all entries and store their positions
		entryPositions := make(map[string]uint64)
		for {
			entry, err := stream.Readdirent(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			entryPositions[entry.Name] = entry.Off
		}

		// Verify we have positions for all entries
		assert.Equal(t, len(expectedEntries), len(entryPositions), "Should have positions for all entries")

		// Seek to beginning
		err = stream.Seekdir(ctx, 0)
		require.NoError(t, err)

		// Read entries again and verify positions are the same
		for {
			entry, err := stream.Readdirent(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)

			expectedPos, exists := entryPositions[entry.Name]
			assert.True(t, exists, "Entry should have a recorded position")
			assert.Equal(t, expectedPos, entry.Off, "Entry position should be consistent")
		}
	})
}

func TestSeekableDirStream_LargeDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large directory test in short mode")
	}

	tempDir := t.TempDir()
	numEntries := 10000

	for i := 0; i < numEntries; i++ {
		name := fmt.Sprintf("file_%d.txt", i)
		path := filepath.Join(tempDir, name)
		err := os.WriteFile(path, []byte(strconv.Itoa(i)), 0644)
		require.NoError(t, err)
	}

	ctx := context.Background()
	dummyHandleID := uint64(2)
	dummyFlags := uint32(0)

	stream, err := OpendirHandle(dummyHandleID, tempDir, dummyFlags)
	require.NoError(t, err)
	require.NotNil(t, stream)
	defer stream.Close()

	count := 0
	var midEntry types.AgentDirEntry
	var midPos int

	// Read all entries, save the middle one for seeking tests
	allEntries := make([]types.AgentDirEntry, 0, numEntries)
	for {
		entry, err := stream.Readdirent(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)

		require.NotEqual(t, ".", entry.Name)
		require.NotEqual(t, "..", entry.Name)

		allEntries = append(allEntries, entry)
		count++

		if count == numEntries/2 {
			midEntry = entry
			midPos = count
		}

		if count > numEntries+10 {
			t.Fatalf("Read significantly more entries (%d) than expected (%d)", count, numEntries)
		}
	}

	assert.Equal(t, numEntries, count, "Should read all entries")

	// Test seeking to middle
	t.Run("SeekToMiddle", func(t *testing.T) {
		require.NotEmpty(t, midEntry.Name, "Middle entry should be saved")
		err = stream.Seekdir(ctx, midEntry.Off)
		require.NoError(t, err, "Seeking to middle position should succeed")

		// Read next entry after seek
		nextEntry, err := stream.Readdirent(ctx)
		require.NoError(t, err, "Reading after seek should succeed")

		// Should match the entry after our midpoint in the original read
		if midPos < len(allEntries) {
			expectedEntry := allEntries[midPos]
			assert.Equal(t, expectedEntry.Name, nextEntry.Name, "Entry after seek should match expected")
		}

		// Count remaining entries
		remainingCount := 1 // Already read one
		for {
			_, err := stream.Readdirent(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			remainingCount++
		}

		// Should have approximately half the entries remaining
		expectedRemaining := numEntries - midPos
		assert.InDelta(t, expectedRemaining, remainingCount, float64(expectedRemaining)*0.1,
			"Number of remaining entries should be approximately half")
	})

	// Test seeking to beginning after reading all
	t.Run("SeekToBeginningAfterReadingAll", func(t *testing.T) {
		err = stream.Seekdir(ctx, 0)
		require.NoError(t, err, "Seeking to beginning should succeed")

		// Read first entry
		firstEntry, err := stream.Readdirent(ctx)
		require.NoError(t, err, "Reading after seek should succeed")
		assert.Equal(t, allEntries[0].Name, firstEntry.Name, "First entry after seek should match original first entry")

		// Count all entries again
		countAfterSeek := 1 // Already read one
		for {
			_, err := stream.Readdirent(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			countAfterSeek++
		}

		assert.Equal(t, numEntries, countAfterSeek, "Should read all entries after seeking to beginning")
	})
}
