package dns

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

type recordingHostRunner struct {
	calls [][]string
}

func (r *recordingHostRunner) Run(_ context.Context, argv []string, _ io.Reader, _ io.Writer, _ io.Writer) error {
	r.calls = append(r.calls, append([]string(nil), argv...))
	return nil
}
func testOSRelease(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/os-release"
	if err := os.WriteFile(path, []byte("ID=ubuntu\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolverMatchingFileNeedsNoPrivilegedRefresh(t *testing.T) {
	path := t.TempDir() + "/laradev-dns.conf"
	if err := os.WriteFile(path, []byte(resolverConfig), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingHostRunner{}
	resolver := &Resolver{Runner: runner, Path: path, OSReleasePath: testOSRelease(t)}
	if err := resolver.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != "systemctl is-active --quiet systemd-resolved" {
		t.Fatalf("unexpected host calls: %#v", runner.calls)
	}
}

func TestResolverRejectsUnownedExistingFile(t *testing.T) {
	path := t.TempDir() + "/laradev-dns.conf"
	if err := os.WriteFile(path, []byte("[Resolve]\nDNS=8.8.8.8\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingHostRunner{}
	resolver := &Resolver{Runner: runner, Path: path, OSReleasePath: testOSRelease(t)}
	if err := resolver.Ensure(context.Background()); err == nil {
		t.Fatal("Ensure unexpectedly accepted an unowned resolver file")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("unexpected privileged calls: %#v", runner.calls)
	}
}
