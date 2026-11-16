//go:build !windows

package fsutil

import (
	"fmt"
	"os"
)

// syncDir fsyncs dir itself. Directory handles are not portable to Windows,
// so that platform gets its own implementation in sync_windows.go.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("fsutil: open directory %s: %w", dir, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsutil: sync directory %s: %w", dir, err)
	}
	return nil
}
