package proxy

import (
	"context"
	"errors"
	"io"
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
