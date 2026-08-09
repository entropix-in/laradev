package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rohan2388/laradev/internal/config"
	"github.com/rohan2388/laradev/internal/docker"
	"github.com/rohan2388/laradev/internal/host"
	"github.com/rohan2388/laradev/internal/state"
)

const (
	ContainerName = "laradev-dnsmasq"
	Image         = "dockurr/dnsmasq:latest"
	HostPort      = 15353
	ResolverFile  = "/etc/systemd/resolved.conf.d/laradev-dns.conf"
)

type Route struct {
	Domain    string `json:"domain"`
	ProjectID string `json:"project_id"`
}

type Manager struct {
	Docker   docker.CommandRunner
	Host     host.CommandRunner
	Input    io.Reader
	StateDir string
	Resolver *Resolver
}

func New(dockerRunner docker.CommandRunner, hostRunner host.CommandRunner, input io.Reader) (*Manager, error) {
	if dockerRunner == nil {
		dockerRunner = docker.Default()
	}
	if hostRunner == nil {
		hostRunner = host.Default()
	}
	dir, err := state.Dir()
	if err != nil {
		return nil, err
	}
	return &Manager{Docker: dockerRunner, Host: hostRunner, Input: input, StateDir: dir, Resolver: &Resolver{Runner: hostRunner, Input: input, Path: ResolverFile}}, nil
}

func (m *Manager) paths() (string, string, error) {
	dir := filepath.Join(m.StateDir, "dns")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "routes.json"), filepath.Join(dir, "dnsmasq.conf"), nil
}

func (m *Manager) Register(projectID string, domains []string) error {
	path, _, err := m.paths()
	if err != nil {
		return err
	}
	routes, err := loadRoutes(path)
	if err != nil {
		return err
	}
	filtered := routes[:0]
	for _, route := range routes {
		if route.ProjectID != projectID {
			filtered = append(filtered, route)
		}
	}
	for _, domain := range domains {
		if err := ValidateDomain(domain); err != nil {
			return err
		}
		filtered = append(filtered, Route{Domain: strings.ToLower(strings.TrimSpace(domain)), ProjectID: projectID})
	}
	return saveRoutes(path, uniqueRoutes(filtered))
}

func (m *Manager) Unregister(projectID string, domain string) error {
	path, _, err := m.paths()
	if err != nil {
		return err
	}
	routes, err := loadRoutes(path)
	if err != nil {
		return err
	}
	name := strings.ToLower(strings.TrimSpace(domain))
	filtered := routes[:0]
	for _, route := range routes {
		if route.ProjectID != projectID || route.Domain != name {
			filtered = append(filtered, route)
		}
	}
	return saveRoutes(path, uniqueRoutes(filtered))
}

func (m *Manager) SyncProject(projectID string, domains []string) error {
	return m.Register(projectID, domains)
}

func (m *Manager) Refresh(ctx context.Context) error {
	routesPath, configPath, err := m.paths()
	if err != nil {
		return err
	}
	routes, err := loadRoutes(routesPath)
	if err != nil {
		return err
	}
	active, err := m.activeProjects(ctx)
	if err != nil {
		return err
	}
	activeRoutes := make([]Route, 0, len(routes))
	for _, route := range routes {
		if active[route.ProjectID] {
			activeRoutes = append(activeRoutes, route)
		}
	}
	activeRoutes = uniqueRoutes(activeRoutes)
	if err := writeConfig(configPath, activeRoutes); err != nil {
		return err
	}
	if len(activeRoutes) == 0 {
		return m.stopContainer(ctx)
	}
	if err := m.preflightContainerPort(ctx); err != nil {
		return err
	}
	if err := m.Resolver.Ensure(ctx); err != nil {
		return err
	}
	if err := m.ensureContainer(ctx); err != nil {
		return err
	}
	return m.flushCaches(ctx)
}

func (m *Manager) Start(ctx context.Context) error { return m.Refresh(ctx) }

func (m *Manager) Stop(ctx context.Context) error {
	return m.stopContainer(ctx)
}

func (m *Manager) Status(ctx context.Context, w io.Writer) error {
	routesPath, configPath, err := m.paths()
	if err != nil {
		return err
	}
	routes, err := loadRoutes(routesPath)
	if err != nil {
		return err
	}
	active, err := m.activeProjects(ctx)
	if err != nil {
		return err
	}
	activeCount := 0
	for _, route := range routes {
		if active[route.ProjectID] {
			activeCount++
		}
	}
	state := "stopped"
	if err := m.Docker.Run(ctx, []string{"inspect", ContainerName}, nil, io.Discard, io.Discard); err == nil {
		state = "created"
		var status strings.Builder
		if err := m.Docker.Run(ctx, []string{"inspect", "--format", "{{.State.Status}}", ContainerName}, nil, &status, io.Discard); err == nil && strings.TrimSpace(status.String()) != "" {
			state = strings.TrimSpace(status.String())
		}
	}
	resolver := "not-installed"
	if m.Resolver.MatchesExisting() {
		resolver = "installed"
	}
	fmt.Fprintf(w, "DNSMASQ\n  container: %s\n  image: %s\n  state: %s\n  active routes: %d\n  config: %s\nRESOLVER\n  backend: systemd-resolved\n  integration: %s\n  file: %s\n", ContainerName, Image, state, activeCount, configPath, resolver, m.Resolver.Path)
	return nil
}
func (m *Manager) preflightContainerPort(ctx context.Context) error {
	if err := m.Docker.Run(ctx, []string{"inspect", ContainerName}, nil, io.Discard, io.Discard); err == nil {
		var labels strings.Builder
		if err := m.Docker.Run(ctx, []string{"inspect", "--format", `{{index .Config.Labels "com.laradev.managed"}}|{{index .Config.Labels "com.laradev.role"}}`, ContainerName}, nil, &labels, io.Discard); err != nil {
			return err
		}
		if strings.TrimSpace(labels.String()) != "true|dnsmasq" {
			return errors.New("refusing existing laradev-dnsmasq without laradev ownership labels")
		}
		return nil
	}
	tcp, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", HostPort))
	if err != nil {
		return fmt.Errorf("DNS TCP port %d is already in use", HostPort)
	}
	_ = tcp.Close()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: HostPort})
	if err != nil {
		return fmt.Errorf("DNS UDP port %d is already in use", HostPort)
	}
	_ = udp.Close()
	return nil
}

func (m *Manager) activeProjects(ctx context.Context) (map[string]bool, error) {
	out, err := docker.Resources{Runner: m.Docker}.Output(ctx, "ps", "-q", "--filter", "label=com.laradev.managed=true", "--filter", "label=com.laradev.role=www")
	if err != nil {
		return nil, fmt.Errorf("inspect active www containers: %w", err)
	}
	active := map[string]bool{}
	for _, id := range strings.Fields(out) {
		var projectID strings.Builder
		if err := m.Docker.Run(ctx, []string{"inspect", "--format", `{{index .Config.Labels "com.laradev.project-id"}}`, id}, nil, &projectID, io.Discard); err != nil {
			return nil, err
		}
		if value := strings.TrimSpace(projectID.String()); value != "" {
			active[value] = true
		}
	}
	return active, nil
}

func (m *Manager) ensureContainer(ctx context.Context) error {
	inspectErr := m.Docker.Run(ctx, []string{"inspect", ContainerName}, nil, io.Discard, io.Discard)
	if inspectErr != nil {
		args := []string{"run", "-d", "--name", ContainerName, "--label", "com.laradev.managed=true", "--label", "com.laradev.role=dnsmasq", "-p", fmt.Sprintf("127.0.0.1:%d:53/udp", HostPort), "-p", fmt.Sprintf("127.0.0.1:%d:53/tcp", HostPort)}
		_, configPath, err := m.paths()
		if err != nil {
			return err
		}
		args = append(args, "-v", configPath+":/etc/dnsmasq.conf:ro", Image)
		var stderr strings.Builder
		if err := m.Docker.Run(ctx, args, nil, io.Discard, &stderr); err != nil {
			return fmt.Errorf("create dnsmasq container: %s: %w", strings.TrimSpace(stderr.String()), err)
		}
		return nil
	}
	var labels strings.Builder
	if err := m.Docker.Run(ctx, []string{"inspect", "--format", `{{index .Config.Labels "com.laradev.managed"}}|{{index .Config.Labels "com.laradev.role"}}`, ContainerName}, nil, &labels, io.Discard); err != nil {
		return err
	}
	if strings.TrimSpace(labels.String()) != "true|dnsmasq" {
		return errors.New("refusing existing laradev-dnsmasq without laradev ownership labels")
	}
	if err := m.Docker.Run(ctx, []string{"restart", ContainerName}, nil, io.Discard, io.Discard); err != nil {
		if err := m.Docker.Run(ctx, []string{"start", ContainerName}, nil, io.Discard, io.Discard); err != nil {
			return fmt.Errorf("start dnsmasq container: %w", err)
		}
	}
	return nil
}

func (m *Manager) stopContainer(ctx context.Context) error {
	if err := m.Docker.Run(ctx, []string{"inspect", ContainerName}, nil, io.Discard, io.Discard); err != nil {
		return nil
	}
	var labels strings.Builder
	if err := m.Docker.Run(ctx, []string{"inspect", "--format", `{{index .Config.Labels "com.laradev.managed"}}|{{index .Config.Labels "com.laradev.role"}}`, ContainerName}, nil, &labels, io.Discard); err != nil {
		return err
	}
	if strings.TrimSpace(labels.String()) != "true|dnsmasq" {
		return errors.New("refusing existing laradev-dnsmasq without laradev ownership labels")
	}
	var status strings.Builder
	if err := m.Docker.Run(ctx, []string{"inspect", "--format", "{{.State.Status}}", ContainerName}, nil, &status, io.Discard); err != nil {
		return err
	}
	if strings.TrimSpace(status.String()) != "running" {
		return nil
	}
	return m.Docker.Run(ctx, []string{"stop", ContainerName}, nil, io.Discard, io.Discard)
}

func (m *Manager) flushCaches(ctx context.Context) error {
	return m.Host.Run(ctx, []string{"resolvectl", "flush-caches"}, m.Input, io.Discard, io.Discard)
}

func ValidateDomain(domain string) error {
	name := strings.ToLower(strings.TrimSpace(domain))
	if err := config.ValidateHostname(name); err != nil {
		return err
	}
	base := strings.TrimPrefix(name, "*.")
	if !strings.HasSuffix(base, ".test") {
		return fmt.Errorf("laradev DNS domains must use the .test suffix: %q", domain)
	}
	return nil
}

func writeConfig(path string, routes []Route) error {
	var b strings.Builder
	b.WriteString("no-resolv\nno-hosts\nno-poll\nbind-interfaces\nlisten-address=0.0.0.0\nport=53\nlocal=/test/\ncache-size=0\n")
	wildcards := make([]string, 0)
	for _, route := range routes {
		if strings.HasPrefix(route.Domain, "*.") {
			base := strings.TrimPrefix(route.Domain, "*.")
			wildcards = append(wildcards, base)
			continue
		}
		fmt.Fprintf(&b, "host-record=%s,127.0.0.1\n", route.Domain)
	}
	sort.Strings(wildcards)
	for _, base := range wildcards {
		fmt.Fprintf(&b, "address=/*.%s/127.0.0.1\n", base)
	}
	return atomicWrite(path, []byte(b.String()), 0600)
}

func loadRoutes(path string) ([]Route, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var routes []Route
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	return routes, json.Unmarshal(data, &routes)
}

func saveRoutes(path string, routes []Route) error {
	data, err := json.MarshalIndent(uniqueRoutes(routes), "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0600)
}

func uniqueRoutes(routes []Route) []Route {
	seen := map[string]bool{}
	out := make([]Route, 0, len(routes))
	for _, route := range routes {
		key := route.ProjectID + "\x00" + route.Domain
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		return out[i].ProjectID < out[j].ProjectID
	})
	return out
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".dnsmasq-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err == nil {
		err = d.Sync()
		_ = d.Close()
	}
	return err
}
