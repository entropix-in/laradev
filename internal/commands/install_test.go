package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRejectsShadowHostPathTraversal(t *testing.T) {
	binDir := t.TempDir()
	err := Install(nil, binDir, []string{"../outside"}, t.TempDir(), nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid forwarding command") {
		t.Fatalf("Install() error = %v, want invalid command error", err)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(binDir), "outside")); !os.IsNotExist(err) {
		t.Fatalf("unexpected path outside installation directory: %v", err)
	}
}

func TestManifestRejectsUnsafeShim(t *testing.T) {
	manifest := Manifest{Version: 1, Binary: "laradev", Shims: []string{"../outside"}}
	if manifest.valid() {
		t.Fatal("manifest.valid() accepted an unsafe shim")
	}
}
