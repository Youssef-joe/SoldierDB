package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirReportsMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "db")
	if err := SyncDir(path); err == nil {
		t.Fatal("SyncDir on a path whose directory does not exist returned nil")
	}
}

func TestSyncDirAcceptsExistingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SyncDir(path); err != nil {
		t.Fatalf("SyncDir: %v", err)
	}
}
