//go:build windows

package fsutil

import (
	"fmt"
	"os"
)

// syncDir is a no-op on Windows. NTFS journals directory entries and flushes
// them together with the file data, so there is no separate directory fsync to
// issue. The directory is still checked so that a bad path fails the same way
// on every platform.
func syncDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("fsutil: stat directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("fsutil: %s is not a directory", dir)
	}
	return nil
}
