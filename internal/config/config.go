package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const ConfigFile = ".laradev.yml"

var (
	projectIDRE = regexp.MustCompile(`^ldev_[0-9a-f]{12}$`)
	commandRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+\-]*$`)
	phpVersions = map[string]bool{"8.1": true, "8.2": true, "8.3": true, "8.4": true}
	mandatory   = []string{"php", "composer", "node", "npm", "pnpm"}
)

type Config struct {
	Version               int           `yaml:"version"`
	Project               Project       `yaml:"project"`
	Runtime               Runtime       `yaml:"runtime"`
	WWW                   WWW           `yaml:"www"`
	Commands              Commands      `yaml:"commands"`
	Domains               []DomainRoute `yaml:"domains"`
	MySQL                 MySQL         `yaml:"mysql"`
	PHPMyAdmin            PHPMyAdmin    `yaml:"phpmyadmin"`
	GeneratedRootPassword string        `yaml:"-"`
}

type Project struct {
	ID string `yaml:"id"`
}
type Runtime struct {
	PHP     string `yaml:"php"`
	Node    string `yaml:"node"`
	Image   string `yaml:"image"`
	BaseDir string `yaml:"base_dir"`
	Webroot string `yaml:"webroot"`
}
type WWW struct {
	Ports []string `yaml:"ports"`
}
type Commands struct {
	Forward []string `yaml:"forward"`
}
type DomainRoute struct {
	Name string `yaml:"name"`
	Port uint16 `yaml:"port"`
}
type MySQL struct {
	Enabled      bool   `yaml:"enabled"`
	Image        string `yaml:"image"`
	Database     string `yaml:"database"`
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	RootPassword string `yaml:"root_password"`
}
type PHPMyAdmin struct {
	HostPort uint16 `yaml:"host_port"`
}

func Defaults(id string) Config {
	return Config{Version: 1, Project: Project{ID: id}, Runtime: Runtime{PHP: "8.4", Node: "22", Image: "rohan2388/laravel-server:php8.4-node22", BaseDir: "/app", Webroot: "/app/public"}, WWW: WWW{Ports: []string{"80:80", "5173:5173"}}, Commands: Commands{Forward: append([]string(nil), mandatory...)}, MySQL: MySQL{Enabled: true, Image: "mysql:8.0", Database: "www", Username: "user", Password: "password"}, PHPMyAdmin: PHPMyAdmin{HostPort: 88}}
}

func GenerateProjectID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ldev_" + hex.EncodeToString(b), nil
}

func GeneratePassword() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

func (c *Config) Normalize() error {
	for i := range c.Domains {
		c.Domains[i].Name = strings.ToLower(strings.TrimSpace(c.Domains[i].Name))
	}
	for i := range c.WWW.Ports {
		c.WWW.Ports[i] = strings.TrimSpace(c.WWW.Ports[i])
		if !strings.Contains(c.WWW.Ports[i], ":") {
			c.WWW.Ports[i] += ":" + c.WWW.Ports[i]
		}
	}
	seen := map[string]bool{}
	for _, v := range c.Commands.Forward {
		seen[v] = true
	}
	c.Commands.Forward = c.Commands.Forward[:0]
	for v := range seen {
		c.Commands.Forward = append(c.Commands.Forward, v)
	}
	sort.Strings(c.Commands.Forward)
	return c.Validate()
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	if !projectIDRE.MatchString(c.Project.ID) {
		return fmt.Errorf("invalid project.id %q", c.Project.ID)
	}
	if !phpVersions[c.Runtime.PHP] {
		return fmt.Errorf("runtime.php must be one of 8.1, 8.2, 8.3, 8.4")
	}
	if c.Runtime.Node != "22" {
		return errors.New("runtime.node must be 22")
	}
	wantImage := "rohan2388/laravel-server:php" + c.Runtime.PHP + "-node" + c.Runtime.Node
	if c.Runtime.Image != wantImage {
		return fmt.Errorf("runtime.image must be %q", wantImage)
	}
	if c.Runtime.BaseDir != "/app" || c.Runtime.Webroot != "/app/public" {
		return errors.New("runtime base_dir/webroot must be /app and /app/public")
	}
	if len(c.WWW.Ports) == 0 {
		return errors.New("www.ports must include container port 80")
	}
	hostPorts, containerPorts := map[uint16]bool{}, map[uint16]bool{}
	has80 := false
	for _, p := range c.WWW.Ports {
		h, t, err := ParsePort(p)
		if err != nil {
			return fmt.Errorf("invalid www port %q: %w", p, err)
		}
		if hostPorts[h] {
			return fmt.Errorf("duplicate host port %d", h)
		}
		if containerPorts[t] {
			return fmt.Errorf("duplicate container port %d", t)
		}
		hostPorts[h], containerPorts[t] = true, true
		if t == 80 {
			has80 = true
		}
	}
	if !has80 {
		return errors.New("www.ports must include container port 80")
	}
	if c.MySQL.Enabled {
		if c.MySQL.Image == "" {
			return errors.New("mysql.image is required")
		}
		if c.MySQL.Database == "" || c.MySQL.Username == "" || c.MySQL.Password == "" || c.MySQL.RootPassword == "" {
			return errors.New("mysql credentials must be non-empty")
		}
		if c.PHPMyAdmin.HostPort < 1 {
			return errors.New("phpmyadmin.host_port must be 1..65535")
		}
		if hostPorts[c.PHPMyAdmin.HostPort] {
			return fmt.Errorf("phpmyadmin host port %d collides with www", c.PHPMyAdmin.HostPort)
		}
	}
	domains := map[string]bool{}
	for _, d := range c.Domains {
		if err := ValidateHostname(d.Name); err != nil {
			return err
		}
		if d.Port < 1 {
			return fmt.Errorf("domain %q port must be 1..65535", d.Name)
		}
		if domains[d.Name] {
			return fmt.Errorf("duplicate domain %q", d.Name)
		}
		domains[d.Name] = true
	}
	cmds := map[string]bool{}
	for _, cmd := range c.Commands.Forward {
		if err := ValidateCommand(cmd); err != nil {
			return err
		}
		if cmds[cmd] {
			return fmt.Errorf("duplicate forwarding command %q", cmd)
		}
		cmds[cmd] = true
	}
	for _, cmd := range mandatory {
		if !cmds[cmd] {
			return fmt.Errorf("forwarding command %q is mandatory", cmd)
		}
	}
	return nil
}

func ParsePort(s string) (uint16, uint16, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("expected host:container")
	}
	h, err := parsePortNumber(parts[0])
	if err != nil {
		return 0, 0, err
	}
	t, err := parsePortNumber(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return h, t, nil
}
func parsePortNumber(s string) (uint16, error) {
	var n uint64
	if s == "" {
		return 0, errors.New("empty port")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("port must be numeric")
		}
		n = n*10 + uint64(r-'0')
		if n > 65535 {
			return 0, errors.New("port out of range")
		}
	}
	if n == 0 {
		return 0, errors.New("port out of range")
	}
	return uint16(n), nil
}

func ValidateCommand(s string) error {
	if s == "laradev" || s == "sh" || s == "bash" || !commandRE.MatchString(s) {
		return fmt.Errorf("invalid forwarding command %q", s)
	}
	return nil
}
func MandatoryCommands() []string { return append([]string(nil), mandatory...) }

func ValidateHostname(name string) error {
	if name == "" || strings.ContainsAny(name, "/:?*[]%") || strings.Contains(name, "..") || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("invalid domain %q", name)
	}
	if net.ParseIP(name) != nil || strings.HasPrefix(name, "*.") {
		return fmt.Errorf("invalid domain %q", name)
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("invalid domain %q", name)
		}
		for _, r := range label {
			if !(r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return fmt.Errorf("invalid domain %q", name)
			}
		}
	}
	return nil
}

func Load(path string) (Config, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return Config{}, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return Config{}, errors.New("refusing symlink config")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Config{}, err
	}
	if err := c.Normalize(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func SaveAtomic(path string, c Config) error {
	if err := c.Normalize(); err != nil {
		return err
	}
	if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing symlink config")
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".laradev.yml.tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err == nil {
		err = d.Sync()
		_ = d.Close()
	}
	return err
}

func InitFingerprint(c Config) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d", c.MySQL.Image, c.MySQL.Database, c.MySQL.Username, c.MySQL.Password, c.MySQL.RootPassword, "%", c.Version)
	return hex.EncodeToString(h.Sum(nil))
}
func Redact(s string, c Config) string {
	for _, v := range []string{c.MySQL.Password, c.MySQL.RootPassword} {
		if v != "" {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return s
}
