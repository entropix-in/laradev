package dns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rohan2388/laradev/internal/host"
)

const (
	resolverConfig       = "# Managed by laradev. Do not edit.\n[Resolve]\nDNS=127.0.0.1:15353\nDomains=~test ~vm\n"
	legacyResolverConfig = "# Managed by laradev. Do not edit.\n[Resolve]\nDNS=127.0.0.1:15353\nDomains=~test\n"
)

type Resolver struct {
	Runner        host.CommandRunner
	Input         io.Reader
	Path          string
	OSReleasePath string
}

func (r *Resolver) Ensure(ctx context.Context) error {
	if err := r.preflight(ctx); err != nil {
		return err
	}
	if st, err := os.Lstat(r.Path); err == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
			return fmt.Errorf("refusing existing resolver path %s", r.Path)
		}
		data, readErr := os.ReadFile(r.Path)
		if readErr != nil {
			return readErr
		}
		if string(data) == resolverConfig {
			return nil
		}
		if string(data) != legacyResolverConfig {
			return fmt.Errorf("refusing existing resolver file %s: it is not laradev-owned", r.Path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(r.Path)
	if err := r.Runner.Run(ctx, []string{"sudo", "install", "-d", "-m", "0755", dir}, r.Input, io.Discard, os.Stderr); err != nil {
		return fmt.Errorf("install systemd-resolved directory: %w", err)
	}
	tmp, err := os.CreateTemp("", "laradev-resolved-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(resolverConfig); err != nil {
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
	if err := r.Runner.Run(ctx, []string{"sudo", "install", "-m", "0644", tmpName, r.Path}, r.Input, io.Discard, os.Stderr); err != nil {
		return fmt.Errorf("install systemd-resolved integration: %w", err)
	}
	if err := r.reload(ctx); err != nil {
		_ = r.removePrivileged(ctx)
		return fmt.Errorf("reload systemd-resolved after integration: %w", err)
	}
	return nil
}

func (r *Resolver) Remove(ctx context.Context) error {
	st, err := os.Lstat(r.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() || !r.MatchesExisting() {
		return fmt.Errorf("refusing to remove resolver file %s: it is not laradev-owned", r.Path)
	}
	if err := r.removePrivileged(ctx); err != nil {
		return err
	}
	if err := r.Runner.Run(ctx, []string{"systemctl", "is-active", "--quiet", "systemd-resolved"}, r.Input, io.Discard, io.Discard); err != nil {
		return nil
	}
	return r.reload(ctx)
}

func (r *Resolver) MatchesExisting() bool {
	data, err := os.ReadFile(r.Path)
	return err == nil && (string(data) == resolverConfig || string(data) == legacyResolverConfig)
}

func (r *Resolver) preflight(ctx context.Context) error {
	osReleasePath := r.OSReleasePath
	if osReleasePath == "" {
		osReleasePath = "/etc/os-release"
	}
	data, err := os.ReadFile(osReleasePath)
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), "ID=ubuntu") && !strings.Contains(string(data), "ID_LIKE=ubuntu") {
		return errors.New("laradev DNS integration supports Ubuntu only")
	}
	if err := r.Runner.Run(ctx, []string{"systemctl", "is-active", "--quiet", "systemd-resolved"}, r.Input, io.Discard, io.Discard); err != nil {
		return errors.New("systemd-resolved is not active")
	}
	return nil
}

func (r *Resolver) reload(ctx context.Context) error {
	if err := r.Runner.Run(ctx, []string{"sudo", "systemctl", "restart", "systemd-resolved"}, r.Input, io.Discard, os.Stderr); err != nil {
		return err
	}
	if err := r.Runner.Run(ctx, []string{"sudo", "resolvectl", "flush-caches"}, r.Input, io.Discard, os.Stderr); err != nil {
		return err
	}
	return nil
}

func (r *Resolver) removePrivileged(ctx context.Context) error {
	return r.Runner.Run(ctx, []string{"sudo", "rm", "-f", r.Path}, r.Input, io.Discard, os.Stderr)
}

func HostSupportsSystemdResolved() bool {
	_, err := exec.LookPath("resolvectl")
	return err == nil
}
