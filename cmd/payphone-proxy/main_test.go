package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDatabaseDirectoryCreatesPrivateDatabaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "payments.db")
	if err := ensureDatabaseDirectory(path); err != nil {
		t.Fatalf("ensureDatabaseDirectory error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("database mode = %o, want 600", got)
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat database directory: %v", err)
	}
	if got := directory.Mode().Perm(); got != 0750 {
		t.Fatalf("database directory mode = %o, want 750", got)
	}
}

func TestEnsureDatabaseDirectoryTightensExistingDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0777); err != nil {
		t.Fatalf("chmod test directory: %v", err)
	}
	path := filepath.Join(directory, "payments.db")
	if err := ensureDatabaseDirectory(path); err != nil {
		t.Fatalf("ensureDatabaseDirectory error = %v", err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0750 {
		t.Fatalf("existing directory mode = %o, want 750", got)
	}
}
