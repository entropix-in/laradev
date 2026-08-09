package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rohan2388/laradev/internal/config"
	"github.com/rohan2388/laradev/internal/project"
	"github.com/rohan2388/laradev/internal/prompt"
	"github.com/rohan2388/laradev/internal/proxy"
)

func BuildConfig(in io.Reader, out, errOut io.Writer) (config.Config, error) {
	id, err := config.GenerateProjectID()
	if err != nil {
		return config.Config{}, err
	}
	c := config.Defaults(id)
	p := prompt.New(in, out, errOut)
	if v, e := p.Choice("PHP", []string{"8.1", "8.2", "8.3", "8.4"}, "8.4"); e != nil {
		return c, e
	} else {
		c.Runtime.PHP = v
		c.Runtime.Image = "rohan2388/laravel-server:php" + v + "-node22"
	}
	if v, e := p.Choice("Node", []string{"22"}, "22"); e != nil {
		return c, e
	} else {
		c.Runtime.Node = v
	}
	if v, e := p.Choice("MySQL", []string{"yes", "no"}, "yes"); e != nil {
		return c, e
	} else {
		c.MySQL.Enabled = v == "yes"
	}
	if c.MySQL.Enabled {
		if c.MySQL.Database, err = p.String("Database", "www"); err != nil {
			return c, err
		}
		if c.MySQL.Username, err = p.String("Username", "user"); err != nil {
			return c, err
		}
		if c.MySQL.Password, err = p.Password("Database password", "password"); err != nil {
			return c, err
		}
		generated, genErr := config.GeneratePassword()
		if genErr != nil {
			return c, genErr
		}
		c.GeneratedRootPassword = generated
		if c.MySQL.RootPassword, err = p.Password("MySQL root password", generated); err != nil {
			return c, err
		}
	} else {
		c.PHPMyAdmin = config.PHPMyAdmin{}
	}
	ports, err := p.String("www ports (comma-separated host:container)", "80:80,5173:5173")
	if err != nil {
		return c, err
	}
	c.WWW.Ports = nil
	for _, v := range strings.Split(ports, ",") {
		if strings.TrimSpace(v) != "" {
			c.WWW.Ports = append(c.WWW.Ports, strings.TrimSpace(v))
		}
	}
	domain, err := p.String("Primary domain", "")
	if err != nil {
		return c, err
	}
	if domain != "" {
		port, er := p.String("www target port", "80")
		if er != nil {
			return c, er
		}
		n, er := strconv.Atoi(port)
		if er != nil || n < 1 || n > 65535 {
			return c, fmt.Errorf("invalid domain port")
		}
		c.Domains = []config.DomainRoute{{Name: strings.ToLower(domain), Port: uint16(n)}}
	}
	extra, err := p.String("Additional forwarded commands", "")
	if err != nil {
		return c, err
	}
	for _, v := range strings.Split(extra, ",") {
		if strings.TrimSpace(v) != "" {
			c.Commands.Forward = append(c.Commands.Forward, strings.TrimSpace(v))
		}
	}
	if err := c.Normalize(); err != nil {
		return c, err
	}
	return c, nil
}
func Init(ctx context.Context, dir string, in io.Reader, out, errOut io.Writer) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if st, e := os.Lstat(filepath.Join(abs, config.ConfigFile)); e == nil {
		_ = st
		return "", fmt.Errorf("%s already exists", config.ConfigFile)
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return "", err
	}
	c, err := BuildConfig(in, out, errOut)
	if err != nil {
		return "", err
	}
	if project.IsTracked(abs) {
		return "", errorsTracked()
	}
	if len(c.Domains) > 0 {
		p, er := proxy.New(nilRunner{})
		if er != nil {
			return "", er
		}
		for _, d := range c.Domains {
			if er := p.EnsureCertificate(d.Name); er != nil {
				return "", er
			}
		}
	}
	path := filepath.Join(abs, config.ConfigFile)
	if err := config.SaveAtomic(path, c); err != nil {
		return "", err
	}
	_ = project.EnsureGitExclude(abs)
	fmt.Fprintf(out, "initialized %s\nproject id: %s\n", path, c.Project.ID)
	if c.GeneratedRootPassword != "" {
		fmt.Fprintf(out, "generated MySQL root password: %s\n", c.GeneratedRootPassword)
	}
	return path, nil
}

type nilRunner struct{}

func (nilRunner) Run(context.Context, []string, io.Reader, io.Writer, io.Writer) error { return nil }
func errorsTracked() error {
	return fmt.Errorf(".laradev.yml is tracked; run git rm --cached .laradev.yml")
}
