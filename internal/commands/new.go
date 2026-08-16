package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entropix-in/laradev/internal/config"
	"github.com/entropix-in/laradev/internal/lock"
	"github.com/entropix-in/laradev/internal/project"
	"github.com/entropix-in/laradev/internal/state"
)

func LaravelConstraint(php string) string {
	switch php {
	case "8.1":
		return "^10.0"
	case "8.2":
		return "^12.0"
	default:
		return "^13.0"
	}
}
func NewProject(ctx context.Context, target, override string, r dockerRunner, in io.Reader, out, errOut io.Writer) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	stateDir, err := state.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "new-locks"), 0700); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(abs))
	held, err := lock.Acquire(ctx, filepath.Join(stateDir, "new-locks", hex.EncodeToString(sum[:])+".lock"), true, 0)
	if err != nil {
		return errors.New("target is already being created")
	}
	defer held.Close()
	if st, e := os.Lstat(abs); e == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return fmt.Errorf("target must be a directory")
		}
		ents, _ := os.ReadDir(abs)
		if len(ents) > 0 {
			return fmt.Errorf("target must be empty")
		}
	} else if os.IsNotExist(e) {
		if err := os.MkdirAll(abs, 0755); err != nil {
			return err
		}
	} else {
		return e
	}
	c, err := BuildConfig(in, out, errOut)
	if err != nil {
		return err
	}
	constraint := LaravelConstraint(c.Runtime.PHP)
	if override != "" {
		constraint = override
	}
	if c.Runtime.PHP == "8.1" {
		fmt.Fprintln(errOut, "warning: Laravel 10 is out of security support")
	}
	if err := r.Run(ctx, []string{"run", "--rm", "--user", "1000:1000", "--env", "HOME=/tmp", "--volume", abs + ":/app", "--workdir", "/app", "--entrypoint", "composer", c.Runtime.Image, "create-project", "--no-interaction", "--prefer-dist", "laravel/laravel", "/app", constraint}, nil, io.Discard, errOut); err != nil {
		return fmt.Errorf("composer scaffold failed: %w", err)
	}
	if err := r.Run(ctx, []string{"run", "--rm", "--user", "1000:1000", "--env", "HOME=/tmp", "--volume", abs + ":/app", "--workdir", "/app", "--entrypoint", "npm", c.Runtime.Image, "install"}, nil, io.Discard, errOut); err != nil {
		return fmt.Errorf("npm install failed: %w", err)
	}
	if err := r.Run(ctx, []string{"run", "--rm", "--user", "1000:1000", "--env", "HOME=/tmp", "--volume", abs + ":/app", "--workdir", "/app", "--entrypoint", "npm", c.Runtime.Image, "run", "build"}, nil, io.Discard, errOut); err != nil {
		return fmt.Errorf("npm build failed: %w", err)
	}
	if err := config.SaveAtomic(filepath.Join(abs, config.ConfigFile), c); err != nil {
		return err
	}
	_ = project.EnsureGitExclude(abs)
	fmt.Fprintf(out, "created %s\n", abs)
	if c.GeneratedRootPassword != "" {
		fmt.Fprintf(out, "generated MySQL root password: %s\n", c.GeneratedRootPassword)
	}
	fmt.Fprintf(out, "cd %s && laradev up\n", abs)
	return nil
}

type dockerRunner interface {
	Run(context.Context, []string, io.Reader, io.Writer, io.Writer) error
}

func normalizeConstraint(s string) string { return strings.TrimSpace(s) }
