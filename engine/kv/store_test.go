package kv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveData1WritesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	if err := SaveData1(path, []byte("hello")); err != nil {
		t.Fatalf("SaveData1: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("file contents = %q, want %q", got, "hello")
	}
}

func TestSaveData2RenamesAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db")
	if err := SaveData2(path, []byte("hello")); err != nil {
		t.Fatalf("SaveData2: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("file contents = %q, want %q", got, "hello")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "db" {
		t.Fatalf("directory entries = %v, want only db", entries)
	}
}
