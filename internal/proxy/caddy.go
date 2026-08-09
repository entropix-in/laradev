package proxy

import (
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rohan2388/laradev/internal/config"
	"github.com/rohan2388/laradev/internal/docker"
	"github.com/rohan2388/laradev/internal/state"
)

type Route struct {
	Domain     string `json:"domain"`
	ProjectID  string `json:"project_id"`
	WorktreeID string `json:"worktree_id"`
	Backend    string `json:"backend"`
	Port       uint16 `json:"port"`
}
type Proxy struct {
	Runner   docker.CommandRunner
	StateDir string
}

func New(r docker.CommandRunner) (*Proxy, error) {
	d, err := state.Dir()
	if err != nil {
		return nil, err
	}
	return &Proxy{Runner: r, StateDir: d}, nil
}
func (p *Proxy) paths() (string, string, error) {
	d := filepath.Join(p.StateDir, "caddy")
	if err := os.MkdirAll(d, 0700); err != nil {
		return "", "", err
	}
	return filepath.Join(d, "Caddyfile"), filepath.Join(d, "routes.json"), nil
}

func (p *Proxy) EnsureCertificate(name string) error {
	if err := config.ValidateHostname(name); err != nil {
		return err
	}
	certDir := filepath.Join(p.StateDir, "certs", name)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return err
	}
	cert, key := filepath.Join(certDir, name+".pem"), filepath.Join(certDir, name+"-key.pem")
	carootOut, err := exec.Command("mkcert", "-CAROOT").Output()
	if err != nil {
		return fmt.Errorf("mkcert is required: %w", err)
	}
	caroot := strings.TrimSpace(string(carootOut))
	root, err := os.ReadFile(filepath.Join(caroot, "rootCA.pem"))
	if err != nil {
		return fmt.Errorf("read mkcert root: %w", err)
	}
	sum := sha256.Sum256(root)
	markerPath := filepath.Join(p.StateDir, "mkcert-trust.json")
	var marker struct {
		CAROOT     string `json:"caroot"`
		RootSHA256 string `json:"root_sha256"`
	}
	markerBytes, markerErr := os.ReadFile(markerPath)
	if markerErr == nil {
		_ = json.Unmarshal(markerBytes, &marker)
	}
	trustCurrent := markerErr == nil && marker.CAROOT == caroot && marker.RootSHA256 == hex.EncodeToString(sum[:])
	if !trustCurrent {
		if err := exec.Command("mkcert", "-install").Run(); err != nil {
			return fmt.Errorf("mkcert -install: %w", err)
		}
		markerData, _ := json.Marshal(struct {
			CAROOT     string `json:"caroot"`
			RootSHA256 string `json:"root_sha256"`
		}{caroot, hex.EncodeToString(sum[:])})
		if err := os.WriteFile(markerPath, markerData, 0600); err != nil {
			return err
		}
	}
	if validCertificate(cert, key, name) {
		_ = os.Chmod(cert, 0644)
		_ = os.Chmod(key, 0600)
		return nil
	}
	cmd := exec.Command("mkcert", "-cert-file", cert, "-key-file", key, name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkcert certificate: %s", strings.TrimSpace(string(out)))
	}
	_ = os.Chmod(cert, 0644)
	_ = os.Chmod(key, 0600)
	return nil
}
func validCertificate(certPath, keyPath, name string) bool {
	b, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return false
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil || time.Until(c.NotAfter) < 24*time.Hour {
		return false
	}
	matched := false
	for _, d := range c.DNSNames {
		if strings.EqualFold(d, name) {
			matched = true
		}
	}
	if !matched {
		return false
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return false
	}
	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return false
	}
	var private interface{ Public() crypto.PublicKey }
	if parsed, parseErr := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); parseErr == nil {
		private, _ = parsed.(interface{ Public() crypto.PublicKey })
	} else if parsed, parseErr := x509.ParseECPrivateKey(keyBlock.Bytes); parseErr == nil {
		private = parsed
	} else if parsed, parseErr := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); parseErr == nil {
		private = parsed
	}
	if private == nil {
		return false
	}
	certPub, certErr := x509.MarshalPKIXPublicKey(c.PublicKey)
	keyPub, keyErr := x509.MarshalPKIXPublicKey(private.Public())
	return certErr == nil && keyErr == nil && string(certPub) == string(keyPub)
}

func Render(routes []Route) string {
	sort.Slice(routes, func(i, j int) bool { return routes[i].Domain < routes[j].Domain })
	var b strings.Builder
	for _, r := range routes {
		fmt.Fprintf(&b, "%s {\n    tls /etc/caddy/certs/%s/%s.pem /etc/caddy/certs/%s/%s-key.pem\n    reverse_proxy %s:%d\n}\n", r.Domain, r.Domain, r.Domain, r.Domain, r.Domain, r.Backend, r.Port)
	}
	return b.String()
}
func (p *Proxy) Reconcile(ctx context.Context, routes []Route) error {
	caddyfile, manifest, err := p.paths()
	if err != nil {
		return err
	}
	for _, r := range routes {
		if err := p.EnsureCertificate(r.Domain); err != nil {
			return err
		}
	}
	data := []byte(Render(routes))
	tmp, err := os.CreateTemp(filepath.Dir(caddyfile), ".Caddyfile-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	_ = tmp.Close()
	if err != nil {
		return err
	}
	if err := p.Runner.Run(ctx, []string{"run", "--rm", "-v", tmpName + ":/etc/caddy/Caddyfile:ro", "-v", filepath.Join(p.StateDir, "certs") + ":/etc/caddy/certs:ro", "caddy:2-alpine", "caddy", "validate", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"}, nil, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("Caddy validation failed: %w", err)
	}
	if err := os.Rename(tmpName, caddyfile); err != nil {
		return err
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Domain != routes[j].Domain {
			return routes[i].Domain < routes[j].Domain
		}
		return routes[i].WorktreeID < routes[j].WorktreeID
	})
	m, _ := json.Marshal(routes)
	if err := os.WriteFile(manifest, append(m, '\n'), 0600); err != nil {
		return err
	}
	if len(routes) == 0 {
		return p.Runner.Run(ctx, []string{"stop", "laradev-caddy"}, nil, io.Discard, io.Discard)
	}
	return p.ensureContainer(ctx)
}
func (p *Proxy) EnsureNetwork(ctx context.Context) error {
	if err := p.Runner.Run(ctx, []string{"network", "inspect", "laradev-proxy"}, nil, io.Discard, io.Discard); err == nil {
		return nil
	}
	var stderr strings.Builder
	if err := p.Runner.Run(ctx, []string{"network", "create", "--label", "com.laradev.managed=true", "--label", "com.laradev.role=caddy", "laradev-proxy"}, nil, io.Discard, &stderr); err != nil {
		return fmt.Errorf("create Caddy network: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

func (p *Proxy) ensureContainer(ctx context.Context) error {
	if err := p.EnsureNetwork(ctx); err != nil {
		return err
	}
	caddyfile, _, err := p.paths()
	if err != nil {
		return err
	}
	create := func() error {
		var stderr strings.Builder
		args := caddyContainerArgs(caddyfile, p.StateDir)
		if err := p.Runner.Run(ctx, args, nil, io.Discard, &stderr); err != nil {
			return fmt.Errorf("create Caddy container: %s: %w", strings.TrimSpace(stderr.String()), err)
		}
		return nil
	}
	if err := p.Runner.Run(ctx, []string{"inspect", "laradev-caddy"}, nil, io.Discard, io.Discard); err != nil {
		return create()
	}
	var ownership strings.Builder
	if err := p.Runner.Run(ctx, []string{"inspect", "--format", `{{index .Config.Labels "com.laradev.managed"}}|{{index .Config.Labels "com.laradev.role"}}`, "laradev-caddy"}, nil, &ownership, io.Discard); err != nil {
		return fmt.Errorf("inspect Caddy ownership: %w", err)
	}
	if strings.TrimSpace(ownership.String()) != "true|caddy" {
		return errors.New("refusing existing laradev-caddy container without laradev Caddy ownership labels")
	}
	var startErr strings.Builder
	if err := p.Runner.Run(ctx, []string{"start", "laradev-caddy"}, nil, io.Discard, &startErr); err != nil {
		if removeErr := p.Runner.Run(ctx, []string{"rm", "-f", "laradev-caddy"}, nil, io.Discard, io.Discard); removeErr != nil {
			return fmt.Errorf("start Caddy container: %s: %w", strings.TrimSpace(startErr.String()), err)
		}
		return create()
	}
	var reloadErr strings.Builder
	if err := p.Runner.Run(ctx, []string{"exec", "laradev-caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"}, nil, io.Discard, &reloadErr); err != nil {
		return fmt.Errorf("reload Caddy: %s: %w", strings.TrimSpace(reloadErr.String()), err)
	}
	return nil
}

func caddyContainerArgs(caddyfile, stateDir string) []string {
	return []string{"run", "-d", "--name", "laradev-caddy", "--network", "laradev-proxy", "--label", "com.laradev.managed=true", "--label", "com.laradev.role=caddy", "-p", "127.0.0.1:443:443", "-v", caddyfile + ":/etc/caddy/Caddyfile:ro", "-v", filepath.Join(stateDir, "certs") + ":/etc/caddy/certs:ro", "-v", "laradev-caddy-data:/data", "-v", "laradev-caddy-config:/config", "caddy:2-alpine"}
}
func LoadRoutes(stateDir string) ([]Route, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, "caddy", "routes.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, nil
	}
	var r []Route
	return r, json.Unmarshal(b, &r)
}
func (p *Proxy) AddDomain(c *config.Config, name string, port uint16) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := config.ValidateHostname(name); err != nil {
		return err
	}
	if port == 0 {
		port = 80
	}
	for _, d := range c.Domains {
		if d.Name == name {
			return errors.New("domain already exists")
		}
	}
	c.Domains = append(c.Domains, config.DomainRoute{Name: name, Port: port})
	return c.Normalize()
}
