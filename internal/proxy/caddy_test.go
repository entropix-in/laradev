package proxy

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingRunner struct {
	calls [][]string
}

func (r *recordingRunner) Run(_ context.Context, args []string, _ io.Reader, stdout, _ io.Writer) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) >= 2 && args[0] == "network" && args[1] == "inspect" {
		return nil
	}
	if len(args) == 2 && args[0] == "inspect" && args[1] == "laradev-caddy" {
		return errors.New("not found")
	}
	if stdout != nil {
		_, _ = io.WriteString(stdout, "")
	}
	return nil
}

func TestCaddyContainerMountsConfigDirectory(t *testing.T) {
	args := caddyContainerArgs("/state/caddy/Caddyfile", "/state")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/state/caddy:/etc/caddy-config:ro") {
		t.Fatalf("missing Caddyfile directory bind mount: %v", args)
	}
	if !strings.Contains(joined, "/state/certs:/etc/caddy/certs:ro") {
		t.Fatalf("missing certificate bind mount: %v", args)
	}
	if !strings.Contains(joined, "--config /etc/caddy-config/Caddyfile --adapter caddyfile") {
		t.Fatalf("missing Caddy config path: %v", args)
	}
	if !strings.Contains(joined, "--label com.laradev.host-binding=0.0.0.0:443 -p 0.0.0.0:443:443") {
		t.Fatalf("Caddy is not published on all host interfaces: %v", args)
	}
}

func TestHasMountDestination(t *testing.T) {
	if !hasMountDestination("/data\n/etc/caddy-config\n", "/etc/caddy-config") {
		t.Fatal("expected Caddy config mount to be found")
	}
	if hasMountDestination("/data\n/etc/caddy-config-old\n", "/etc/caddy-config") {
		t.Fatal("unexpectedly matched a similarly named mount")
	}
}

func TestMergeProjectRoutesPreservesOtherWorktrees(t *testing.T) {
	routes := []Route{
		{Domain: "app.test", ProjectID: "project-a", WorktreeID: "worktree-a", Backend: "www-a", Port: 80},
		{Domain: "old.test", ProjectID: "project-b", WorktreeID: "worktree-b", Backend: "www-b", Port: 80},
	}
	desired := []Route{{Domain: "new.test", ProjectID: "project-a", WorktreeID: "worktree-a", Backend: "www-a", Port: 8080}}
	got := mergeProjectRoutes(routes, "project-a", "worktree-a", desired)
	if len(got) != 2 {
		t.Fatalf("got %d routes, want 2: %#v", len(got), got)
	}
	if got[0].Domain != "old.test" || got[1].Domain != "new.test" {
		t.Fatalf("unexpected merged routes: %#v", got)
	}
}

func TestMergeProjectRoutesRemovesAllRoutesWhenDesiredIsEmpty(t *testing.T) {
	routes := []Route{
		{Domain: "app.test", ProjectID: "project-a", WorktreeID: "worktree-a"},
		{Domain: "other.test", ProjectID: "project-a", WorktreeID: "worktree-b"},
	}
	got := mergeProjectRoutes(routes, "project-a", "worktree-a", nil)
	if len(got) != 1 || got[0].Domain != "other.test" {
		t.Fatalf("unexpected routes after removal: %#v", got)
	}
}

func TestEnsureCertificateInitializesMissingMkcertRoot(t *testing.T) {
	root := t.TempDir()
	bin := t.TempDir()
	mkcert := filepath.Join(bin, "mkcert")
	script := `#!/bin/sh
case "$1" in
  -CAROOT) printf '%s\n' "$MKCERT_ROOT" ;;
  -install) printf 'root certificate\n' > "$MKCERT_ROOT/rootCA.pem" ;;
  -cert-file) printf 'certificate\n' > "$2"; printf 'key\n' > "$4" ;;
esac
`
	if err := os.WriteFile(mkcert, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", bin+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("MKCERT_ROOT", root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		_ = os.Unsetenv("MKCERT_ROOT")
	})

	if err := (&Proxy{StateDir: t.TempDir()}).EnsureCertificate("app.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "rootCA.pem")); err != nil {
		t.Fatalf("mkcert root was not initialized: %v", err)
	}
}

func TestEnsureContainerCreatesNetworkBeforeContainer(t *testing.T) {
	runner := &recordingRunner{}
	p := &Proxy{Runner: runner, StateDir: t.TempDir()}
	if err := p.ensureContainer(context.Background()); err != nil {
		t.Fatalf("ensureContainer() error = %v", err)
	}
	if len(runner.calls) < 3 {
		t.Fatalf("expected network inspect, container inspect, and run calls: %v", runner.calls)
	}
	if got := strings.Join(runner.calls[0], " "); got != "network inspect laradev-proxy" {
		t.Fatalf("first call = %q", got)
	}
	last := strings.Join(runner.calls[len(runner.calls)-1], " ")
	if !strings.HasPrefix(last, "run -d --name laradev-caddy --network laradev-proxy") {
		t.Fatalf("last call does not create Caddy: %q", last)
	}
}
