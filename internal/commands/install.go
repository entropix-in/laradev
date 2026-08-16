package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entropix-in/laradev/internal/config"
	"github.com/entropix-in/laradev/internal/project"
)

type Manifest struct {
	Version int      `json:"version"`
	Binary  string   `json:"binary"`
	Shims   []string `json:"shims"`
}

func readManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	if m.Version != 1 || m.Binary != "laradev" {
		return Manifest{}, errors.New("invalid laradev install manifest")
	}
	return m, nil
}
func (m Manifest) valid() bool {
	if m.Version != 1 || m.Binary != "laradev" {
		return false
	}
	seen := map[string]bool{}
	for _, s := range m.Shims {
		if seen[s] {
			return false
		}
		if config.ValidateCommand(s) != nil {
			return false
		}
		seen[s] = true
	}
	return true
}
func Install(ctx context.Context, binDir string, extra []string, cwd string, in io.Reader, out, errOut io.Writer) error {
	if binDir == "" {
		home, _ := os.UserHomeDir()
		binDir = filepath.Join(home, ".local", "bin")
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}
	source, err := os.Executable()
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, n := range []string{"php", "composer", "node", "npm", "pnpm"} {
		names[n] = true
	}
	if c, e := project.Resolve(cwd); e == nil {
		for _, n := range c.Config.Commands.Forward {
			names[n] = true
		}
	}
	for _, n := range extra {
		if err := config.ValidateCommand(n); err != nil {
			return err
		}
		names[n] = true
	}
	shims := make([]string, 0, len(names))
	for n := range names {
		if n != "laradev" && n != "sh" && n != "bash" {
			shims = append(shims, n)
		}
	}
	sort.Strings(shims)
	manifestPath := filepath.Join(binDir, ".laradev-install.json")
	if old, e := readManifest(manifestPath); e == nil && old.valid() {
		for _, n := range old.Shims {
			names[n] = true
		}
		shims = shims[:0]
		for n := range names {
			if n != "laradev" && n != "sh" && n != "bash" {
				shims = append(shims, n)
			}
		}
		sort.Strings(shims)
	}
	dest := filepath.Join(binDir, "laradev")
	if source != dest {
		if _, e := os.Lstat(dest); e == nil && !managedTarget(dest, binDir) {
			return fmt.Errorf("refusing unrelated existing %s", dest)
		}
		tmp, e := os.CreateTemp(binDir, ".laradev-bin-")
		if e != nil {
			return e
		}
		tmpName := tmp.Name()
		if e = tmp.Chmod(0755); e == nil {
			data, e2 := os.ReadFile(source)
			if e2 == nil {
				_, e = tmp.Write(data)
			} else {
				e = e2
			}
		}
		_ = tmp.Close()
		if e == nil {
			e = os.Rename(tmpName, dest)
		} else {
			_ = os.Remove(tmpName)
		}
		if e != nil {
			return e
		}
	}
	for _, n := range shims {
		if err := config.ValidateCommand(n); err != nil {
			return err
		}
		p := filepath.Join(binDir, n)
		rel, err := filepath.Rel(binDir, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Base(n) != n {
			return fmt.Errorf("shim path escapes installation directory: %s", n)
		}
		if st, e := os.Lstat(p); e == nil {
			if st.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("refusing unrelated existing %s", p)
			}
			target, _ := os.Readlink(p)
			if !strings.Contains(target, "laradev") {
				return fmt.Errorf("refusing unrelated existing %s", p)
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		_ = os.Remove(p)
		if e := os.Symlink("laradev", p); e != nil {
			return e
		}
	}
	data, _ := json.MarshalIndent(Manifest{Version: 1, Binary: "laradev", Shims: shims}, "", "  ")
	data = append(data, '\n')
	return os.WriteFile(manifestPath, data, 0644)
}
func managedTarget(path, dir string) bool {
	st, e := os.Lstat(path)
	if e != nil {
		return false
	}
	if st.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(path)
		return strings.Contains(target, "laradev")
	}
	return filepath.Clean(path) == filepath.Join(dir, "laradev")
}
func (m Manifest) ShimsSorted() []string {
	v := append([]string(nil), m.Shims...)
	sort.Strings(v)
	return v
}
func _installContext() { _ = context.Background(); _ = io.Discard }
