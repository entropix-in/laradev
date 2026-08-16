package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/entropix-in/laradev/internal/config"
)

type Context struct {
	Config       config.Config
	ConfigPath   string
	ProjectRoot  string
	WorktreeRoot string
	WorktreeID   string
}

func Resolve(cwd string) (Context, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return Context{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Context{}, err
	}
	for dir := abs; ; dir = filepath.Dir(dir) {
		p := filepath.Join(dir, config.ConfigFile)
		if _, err := os.Lstat(p); err == nil {
			c, err := config.Load(p)
			if err != nil {
				return Context{}, fmt.Errorf("load %s: %w", p, err)
			}
			canonical, err := filepath.EvalSymlinks(p)
			if err != nil {
				return Context{}, err
			}
			root, err := filepath.Abs(filepath.Dir(canonical))
			if err != nil {
				return Context{}, err
			}
			wt := gitTop(abs)
			if wt == "" {
				wt = root
			}
			wt, _ = filepath.EvalSymlinks(wt)
			return Context{Config: c, ConfigPath: canonical, ProjectRoot: root, WorktreeRoot: wt, WorktreeID: WorktreeID(wt)}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	// A linked worktree may not contain its own config; locate the sole canonical copy.
	top := gitTop(abs)
	if top == "" {
		return Context{}, errors.New("no .laradev.yml found; run laradev init")
	}
	common := gitOutput(top, "rev-parse", "--git-common-dir")
	if common == "" {
		return Context{}, errors.New("no .laradev.yml found; run laradev init")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(top, common)
	}
	common, _ = filepath.Abs(common)
	list := gitOutput(top, "worktree", "list", "--porcelain")
	var roots []string
	var current string
	for _, block := range strings.Split(strings.TrimSpace(list), "\n\n") {
		var path string
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "worktree ") {
				path = strings.TrimPrefix(line, "worktree ")
			}
		}
		if path == "" {
			continue
		}
		if path == top {
			current = path
		}
		if _, err := os.Lstat(filepath.Join(path, config.ConfigFile)); err == nil {
			roots = append(roots, path)
		}
	}
	if len(roots) == 0 {
		return Context{}, errors.New("no .laradev.yml found in repository worktrees; run laradev init in the primary worktree")
	}
	if len(roots) != 1 {
		return Context{}, errors.New("multiple .laradev.yml files found; remove extra copies and git rm --cached .laradev.yml if tracked")
	}
	configPath := filepath.Join(roots[0], config.ConfigFile)
	c, err := config.Load(configPath)
	if err != nil {
		return Context{}, err
	}
	canonical, err := filepath.EvalSymlinks(configPath)
	if err != nil {
		return Context{}, err
	}
	root := filepath.Dir(canonical)
	if current == "" {
		current = top
	}
	current, _ = filepath.EvalSymlinks(current)
	_ = common
	return Context{Config: c, ConfigPath: canonical, ProjectRoot: root, WorktreeRoot: current, WorktreeID: WorktreeID(current)}, nil
}

func WorktreeID(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:])[:12]
}
func gitTop(cwd string) string {
	out := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if out == "" {
		return ""
	}
	p, err := filepath.Abs(out)
	if err != nil {
		return ""
	}
	p, _ = filepath.EvalSymlinks(p)
	return p
}
func gitOutput(cwd string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func EnsureGitExclude(root string) error {
	common := gitOutput(root, "rev-parse", "--git-common-dir")
	if common == "" {
		fmt.Fprintln(os.Stderr, "warning: outside Git; keep .laradev.yml private")
		return nil
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	path := filepath.Join(common, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(data)
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "/.laradev.yml" {
			return nil
		}
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += "/.laradev.yml\n"
	return os.WriteFile(path, []byte(text), 0600)
}

func IsTracked(root string) bool {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", config.ConfigFile)
	cmd.Dir = root
	return cmd.Run() == nil
}
