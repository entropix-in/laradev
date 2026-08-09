package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rohan2388/laradev/internal/config"
	"github.com/rohan2388/laradev/internal/docker"
	"github.com/rohan2388/laradev/internal/lock"
	"github.com/rohan2388/laradev/internal/project"
	"github.com/rohan2388/laradev/internal/proxy"
	"github.com/rohan2388/laradev/internal/state"
)

type App struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Runner docker.CommandRunner
}

func (a App) runner() docker.CommandRunner {
	if a.Runner != nil {
		return a.Runner
	}
	return docker.Default()
}
func (a App) Run(args []string) error {
	if len(args) == 0 {
		return writeHelp(a.Out, nil)
	}
	if args[0] == "help" {
		return writeHelp(a.Out, args[1:])
	}
	if isHelpArg(args[0]) {
		return writeHelp(a.Out, nil)
	}
	for i := 1; i < len(args); i++ {
		if isHelpArg(args[i]) {
			return writeHelp(a.Out, args[:i])
		}
	}
	ctx := context.Background()
	lockPath, lockErr := state.Dir()
	if lockErr != nil {
		return lockErr
	}
	mutating := args[0] == "init" || args[0] == "new" || args[0] == "up" || args[0] == "stop" || args[0] == "down" || args[0] == "install" || args[0] == "stop-all" || args[0] == "cleanup" || args[0] == "command" || args[0] == "domain"
	if mutating || args[0] == "status" {
		exclusive := mutating
		held, err := lock.Acquire(ctx, filepath.Join(lockPath, "laradev.lock"), exclusive, map[bool]time.Duration{true: 30 * time.Second, false: 5 * time.Second}[exclusive])
		if err != nil {
			return fmt.Errorf("laradev state is busy: %w", err)
		}
		defer held.Close()
	}
	cwd, _ := os.Getwd()
	r := a.runner()
	switch args[0] {
	case "init":
		dir := cwd
		if len(args) > 1 {
			dir = args[1]
		}
		_, err := Init(ctx, dir, a.In, a.Out, a.Err)
		return err
	case "new":
		if len(args) < 2 {
			return errors.New("usage: laradev new <directory>")
		}
		version := ""
		for i := 2; i < len(args); i++ {
			if args[i] == "--laravel-version" && i+1 < len(args) {
				version = args[i+1]
				i++
			}
		}
		return NewProject(ctx, args[1], version, r, a.In, a.Out, a.Err)
	case "up", "stop", "down", "status":
		c, err := project.Resolve(cwd)
		if err != nil {
			if args[0] == "status" {
				return (Lifecycle{Runner: r}).StatusGlobal(ctx, a.Out)
			}
			return err
		}
		_ = project.EnsureGitExclude(c.ProjectRoot)
		l := Lifecycle{Runner: r}
		switch args[0] {
		case "up":
			if err := l.Up(ctx, c); err != nil {
				return err
			}
			PrintStartupGuide(c, a.Out)
			return nil
		case "stop":
			return l.Stop(ctx, c)
		case "down":
			return l.Down(ctx, c)
		default:
			return l.Status(ctx, &c, a.Out)
		}
	case "stop-all":
		return (Lifecycle{Runner: r}).StopAll(ctx)
	case "cleanup":
		return (Lifecycle{Runner: r}).Cleanup(ctx)
	case "exec":
		if len(args) < 2 {
			return errors.New("usage: laradev exec <command> [args]")
		}
		return (ExecOptions{Runner: r, In: a.In, Out: a.Out, Err: a.Err}).Forward(ctx, args[1], args[2:], cwd)
	case "sh", "bash":
		shell := "/bin/" + args[0]
		return (ExecOptions{Runner: r, In: a.In, Out: a.Out, Err: a.Err}).Shell(ctx, shell, args[1:], cwd)
	case "install":
		return a.install(args[1:], cwd)
	case "command":
		return a.command(args[1:], cwd)
	case "domain":
		return a.domain(args[1:], cwd)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func (a App) install(args []string, cwd string) error {
	binDir := ""
	var extra []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--bin-dir" && i+1 < len(args) {
			binDir = args[i+1]
			i++
		} else if args[i] == "--shadow-host" && i+1 < len(args) {
			extra = append(extra, args[i+1])
			i++
		}
	}
	return Install(context.Background(), binDir, extra, cwd, a.In, a.Out, a.Err)
}
func (a App) command(args []string, cwd string) error {
	if len(args) == 0 {
		return errors.New("usage: laradev command list|add|remove")
	}
	c, err := project.Resolve(cwd)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		for _, n := range c.Config.Commands.Forward {
			fmt.Fprintln(a.Out, n)
		}
		return nil
	case "add":
		if len(args) < 2 {
			return errors.New("usage: laradev command add <name>")
		}
		n := args[1]
		if err := config.ValidateCommand(n); err != nil {
			return err
		}
		for _, v := range c.Config.Commands.Forward {
			if v == n {
				return errors.New("command already configured")
			}
		}
		c.Config.Commands.Forward = append(c.Config.Commands.Forward, n)
		if err := c.Config.Normalize(); err != nil {
			return err
		}
		if err := config.SaveAtomic(c.ConfigPath, c.Config); err != nil {
			return err
		}
		return project.EnsureGitExclude(c.ProjectRoot)
	case "remove":
		if len(args) < 2 {
			return errors.New("usage: laradev command remove <name>")
		}
		n := args[1]
		for _, m := range config.MandatoryCommands() {
			if n == m {
				return errors.New("cannot remove mandatory command")
			}
		}
		found := false
		out := c.Config.Commands.Forward[:0]
		for _, v := range c.Config.Commands.Forward {
			if v == n {
				found = true
			} else {
				out = append(out, v)
			}
		}
		if !found {
			return errors.New("command is not configured")
		}
		c.Config.Commands.Forward = out
		if err := c.Config.Normalize(); err != nil {
			return err
		}
		return config.SaveAtomic(c.ConfigPath, c.Config)
	}
	return errors.New("unknown command subcommand")
}
func (a App) domain(args []string, cwd string) error {
	c, err := project.Resolve(cwd)
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "list" {
		fmt.Fprintln(a.Out, "DOMAIN\tTARGET\tURL\tCERTIFICATE\tBACKEND")
		stateDir, stateErr := state.Dir()
		if stateErr != nil {
			return stateErr
		}
		for _, d := range c.Config.Domains {
			cert := filepath.Join(stateDir, "certs", d.Name, d.Name+".pem")
			backend := "stopped"
			fmt.Fprintf(a.Out, "%s\twww:%d\thttps://%s\t%s\t%s\n", d.Name, d.Port, d.Name, cert, backend)
		}
		return nil
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return errors.New("usage: laradev domain add <hostname> [--port N]")
		}
		name := strings.ToLower(args[1])
		port := uint16(80)
		for i := 2; i < len(args); i++ {
			if args[i] == "--port" && i+1 < len(args) {
				n, e := strconv.Atoi(args[i+1])
				if e != nil || n < 1 || n > 65535 {
					return errors.New("invalid port")
				}
				port = uint16(n)
				i++
			}
		}
		p, e := proxy.New(a.runner())
		if e != nil {
			return e
		}
		if e = p.EnsureCertificate(name); e != nil {
			return e
		}
		if e = p.AddDomain(&c.Config, name, port); e != nil {
			return e
		}
		if e = config.SaveAtomic(c.ConfigPath, c.Config); e != nil {
			return e
		}
		_ = project.EnsureGitExclude(c.ProjectRoot)
		fmt.Fprintf(a.Out, "127.0.0.1 %s\nhttps://%s\n", name, name)
		return nil
	case "remove":
		if len(args) < 2 {
			return errors.New("usage: laradev domain remove <hostname>")
		}
		name := strings.ToLower(args[1])
		found := false
		ds := c.Config.Domains[:0]
		for _, d := range c.Config.Domains {
			if d.Name == name {
				found = true
			} else {
				ds = append(ds, d)
			}
		}
		if !found {
			return errors.New("domain is not configured")
		}
		c.Config.Domains = ds
		return config.SaveAtomic(c.ConfigPath, c.Config)
	}
	return errors.New("unknown domain subcommand")
}
func Dynamic(name string, args []string, a App) error {
	cwd, _ := os.Getwd()
	if c, e := project.Resolve(cwd); e == nil {
		for _, cmd := range c.Config.Commands.Forward {
			if cmd == name {
				return (ExecOptions{Runner: a.runner(), In: a.In, Out: a.Out, Err: a.Err}).Forward(context.Background(), name, args, cwd)
			}
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s: command not found", name)
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.In, a.Out, a.Err
	return cmd.Run()
}
func parseExit(err error) int {
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return 1
}
func _appUnused() { _ = filepath.Separator; _ = parseExit }
