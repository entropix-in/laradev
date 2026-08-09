package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostExecutableSkipsManagedShim(t *testing.T) {
	managed := t.TempDir()
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(managed, "laradev"), []byte("shim"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("laradev", filepath.Join(managed, "php")); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"version":1,"binary":"laradev","shims":["php"]}`)
	if err := os.WriteFile(filepath.Join(managed, ".laradev-install.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	hostPHP := filepath.Join(host, "php")
	if err := os.WriteFile(hostPHP, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", managed+string(os.PathListSeparator)+host); err != nil {
		t.Fatal(err)
	}
	defer os.Setenv("PATH", oldPath)
	got, err := hostExecutable("php")
	if err != nil {
		t.Fatal(err)
	}
	if got != hostPHP {
		t.Fatalf("hostExecutable returned %q, want %q", got, hostPHP)
	}
}

func TestHostExecutableDoesNotReturnOnlyManagedShim(t *testing.T) {
	managed := t.TempDir()
	if err := os.WriteFile(filepath.Join(managed, "laradev"), []byte("shim"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("laradev", filepath.Join(managed, "php")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managed, ".laradev-install.json"), []byte(`{"version":1,"binary":"laradev","shims":["php"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", managed); err != nil {
		t.Fatal(err)
	}
	defer os.Setenv("PATH", oldPath)
	if _, err := hostExecutable("php"); err == nil {
		t.Fatal("hostExecutable unexpectedly returned a managed shim")
	}
}
