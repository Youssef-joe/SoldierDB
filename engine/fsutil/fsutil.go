// Package fsutil holds the durability primitives shared by the storage
// engine.
//
// Durability of a newly created or renamed file has two halves. The contents
// become durable once the file itself is synced, but the directory entry that
// names the file is separate metadata; a power cut after the file sync and
// before the next directory sync can leave the data on disk with no name
// pointing at it. SyncDir closes that window by fsyncing the parent directory
// after a create or a rename.
package fsutil

import (
	"path/filepath"
)

// SyncDir fsyncs the directory that holds path, making a create or rename of
// path durable. Call it after the file itself has been synced; on Windows it
// only checks that the directory exists, because NTFS journals directory
// entries with the file data.
func SyncDir(path string) error {
	return syncDir(filepath.Dir(path))
}
