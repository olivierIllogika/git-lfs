package lfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type CallbackReader struct {
	C         CopyCallback
	TotalSize int64
	ReadSize  int64
	io.Reader
}

type CopyCallback func(totalSize int64, readSoFar int64, readSinceLast int) error

func (w *CallbackReader) Read(p []byte) (int, error) {
	n, err := w.Reader.Read(p)

	if n > 0 {
		w.ReadSize += int64(n)
	}

	if err == nil && w.C != nil {
		err = w.C(w.TotalSize, w.ReadSize, n)
	}

	return n, err
}

func CopyWithCallback(writer io.Writer, reader io.Reader, totalSize int64, cb CopyCallback) (int64, error) {
	if cb == nil {
		return io.Copy(writer, reader)
	}

	cbReader := &CallbackReader{
		C:         cb,
		TotalSize: totalSize,
		Reader:    reader,
	}
	return io.Copy(writer, cbReader)
}

func CopyCallbackFile(event, filename string, index, totalFiles int) (CopyCallback, *os.File, error) {
	logPath := Config.Getenv("GIT_LFS_PROGRESS")
	if len(logPath) == 0 || len(filename) == 0 || len(event) == 0 {
		return nil, nil, nil
	}

	if !filepath.IsAbs(logPath) {
		return nil, nil, fmt.Errorf("GIT_LFS_PROGRESS must be an absolute path")
	}

	cbDir := filepath.Dir(logPath)
	if err := os.MkdirAll(cbDir, 0755); err != nil {
		return nil, nil, wrapProgressError(err, event, logPath)
	}

	file, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, file, wrapProgressError(err, event, logPath)
	}

	var prevWritten int64

	cb := CopyCallback(func(total int64, written int64, current int) error {
		if written != prevWritten {
			_, err := file.Write([]byte(fmt.Sprintf("%s %d/%d %d/%d %s\n", event, index, totalFiles, written, total, filename)))
			file.Sync()
			prevWritten = written
			return wrapProgressError(err, event, logPath)
		}

		return nil
	})

	return cb, file, nil
}

func wrapProgressError(err error, event, filename string) error {
	if err != nil {
		return fmt.Errorf("Error writing Git LFS %s progress to %s: %s", event, filename, err.Error())
	}

	return nil
}

// Return whether a given filename passes the include / exclude path filters
// Only paths that are in includePaths and outside excludePaths are passed
// If includePaths is empty that filter always passes and the same with excludePaths
// Both path lists support wildcard matches
func FilenamePassesIncludeExcludeFilter(filename string, includePaths, excludePaths []string) bool {
	if len(includePaths) == 0 && len(excludePaths) == 0 {
		return true
	}

	// For Win32, because git reports files with / separators
	cleanfilename := filepath.Clean(filename)
	if len(includePaths) > 0 {
		matched := false
		for _, inc := range includePaths {
			matched, _ = filepath.Match(inc, filename)
			if !matched && IsWindows() {
				// Also Win32 match
				matched, _ = filepath.Match(inc, cleanfilename)
			}
			if !matched {
				// Also support matching a parent directory without a wildcard
				if strings.HasPrefix(cleanfilename, inc+string(filepath.Separator)) {
					matched = true
				}
			}
			if matched {
				break
			}

		}
		if !matched {
			return false
		}
	}

	if len(excludePaths) > 0 {
		for _, ex := range excludePaths {
			matched, _ := filepath.Match(ex, filename)
			if !matched && IsWindows() {
				// Also Win32 match
				matched, _ = filepath.Match(ex, cleanfilename)
			}
			if matched {
				return false
			}
			// Also support matching a parent directory without a wildcard
			if strings.HasPrefix(cleanfilename, ex+string(filepath.Separator)) {
				return false
			}

		}
	}

	return true
}

// Are we running on Windows? Need to handle some extra path shenanigans
func IsWindows() bool {
	return runtime.GOOS == "windows"
}
