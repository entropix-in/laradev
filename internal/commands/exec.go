package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/entropix-in/laradev/internal/docker"
	"github.com/entropix-in/laradev/internal/project"
)

type ExecOptions struct {
	Runner docker.CommandRunner
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
}

func (o ExecOptions) Forward(ctx context.Context, command string, args []string, cwd string) error {
	c, err := project.Resolve(cwd)
	if err != nil {
		return err
	}
	allowed := false
	for _, v := range c.Config.Commands.Forward {
		if v == command {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("command %q is not configured for forwarding", command)
	}
	return o.run(ctx, c, command, args, cwd)
}
func (o ExecOptions) run(ctx context.Context, c project.Context, command string, args []string, cwd string) error {
	rel, err := filepath.Rel(c.WorktreeRoot, cwd)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("working directory escapes worktree")
	}
	workdir := "/app"
	if rel != "." {
		workdir = "/app/" + filepath.ToSlash(rel)
	}
	_, tty := o.In.(*os.File)
	if f, ok := o.In.(*os.File); ok {
		tty = term.IsTerminal(int(f.Fd()))
	}
	argv := []string{"exec", "--user", "1000:1000", "--workdir", workdir, "--env", "HOME=/tmp/laradev-home", "--interactive"}
	if tty {
		argv = append(argv, "--tty")
	}
	argv = append(argv, "laradev-"+c.Config.Project.ID+"-www-"+c.WorktreeID, command)
	argv = append(argv, args...)
	return o.Runner.Run(ctx, argv, o.In, o.Out, o.Err)
}
func (o ExecOptions) Shell(ctx context.Context, shell string, args []string, cwd string) error {
	c, err := project.Resolve(cwd)
	if err != nil {
		return err
	}
	if shell != "/bin/sh" && shell != "/bin/bash" {
		return errors.New("unsupported shell")
	}
	return o.run(ctx, c, shell, args, cwd)
}
