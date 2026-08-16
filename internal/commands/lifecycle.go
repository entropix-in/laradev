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
	"time"

	"github.com/entropix-in/laradev/internal/config"
	"github.com/entropix-in/laradev/internal/dns"
	"github.com/entropix-in/laradev/internal/docker"
	"github.com/entropix-in/laradev/internal/host"
	"github.com/entropix-in/laradev/internal/project"
	"github.com/entropix-in/laradev/internal/proxy"
)

const managedLabel = "com.laradev.managed=true"

type Lifecycle struct {
	Runner docker.CommandRunner
	Host   host.CommandRunner
	Input  io.Reader
}

func (l Lifecycle) resources() docker.Resources { return docker.Resources{Runner: l.Runner} }
func (l Lifecycle) dnsManager() (*dns.Manager, error) {
	return dns.New(l.Runner, l.Host, l.Input)
}
func (l Lifecycle) refreshDNS(ctx context.Context) error {
	manager, err := l.dnsManager()
	if err != nil {
		return err
	}
	return manager.Refresh(ctx)
}
func (l Lifecycle) names(c project.Context) (string, string, string, string) {
	id := c.Config.Project.ID
	return "laradev-" + id + "-network", "laradev-" + id + "-mysql", "laradev-" + id + "-phpmyadmin", "laradev-" + id + "-www-" + c.WorktreeID
}
func (l Lifecycle) Up(ctx context.Context, c project.Context) error {
	r := l.resources()
	if err := r.DockerAvailable(ctx); err != nil {
		return err
	}
	network, mysql, pma, www := l.names(c)
	if err := l.ensureNetwork(ctx, network, c); err != nil {
		return err
	}
	if c.Config.MySQL.Enabled {
		if err := l.ensureMySQL(ctx, c, network, mysql, pma); err != nil {
			return err
		}
	}
	routes, _ := json.Marshal(c.Config.Domains)
	labels := []string{"--label", managedLabel, "--label", "com.laradev.project-id=" + c.Config.Project.ID, "--label", "com.laradev.role=www", "--label", "com.laradev.worktree-id=" + c.WorktreeID, "--label", "com.laradev.project-root=" + c.ProjectRoot, "--label", "com.laradev.worktree-root=" + c.WorktreeRoot, "--label", "com.laradev.routes=" + string(routes), "--label", "com.laradev.host-bindings=" + strings.Join(c.Config.WWW.Ports, ",")}
	if err := l.ensureWWW(ctx, c, network, www, labels); err != nil {
		return err
	}
	if err := l.Runner.Run(ctx, []string{"exec", "--user", "1000:1000", "--workdir", "/app", "-e", "HOME=/tmp/laradev-home", www, "/bin/sh", "-c", "test -r /app && test -w /app && mkdir -p /tmp/laradev-home"}, nil, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("worktree is not readable/writable by UID/GID 1000: %w", err)
	}
	rs := make([]proxy.Route, 0, len(c.Config.Domains))
	for _, d := range c.Config.Domains {
		rs = append(rs, proxy.Route{Domain: d.Name, ProjectID: c.Config.Project.ID, WorktreeID: c.WorktreeID, Backend: www, Port: d.Port})
	}
	if len(rs) > 0 {
		p, err := proxy.New(l.Runner)
		if err != nil {
			return err
		}
		if err := p.EnsureNetwork(ctx); err != nil {
			return err
		}
		var connectErr strings.Builder
		if err := l.Runner.Run(ctx, []string{"network", "connect", "laradev-proxy", www}, nil, io.Discard, &connectErr); err != nil {
			networks, inspectErr := r.Output(ctx, "inspect", "--format", "{{json .NetworkSettings.Networks}}", www)
			if inspectErr != nil || !strings.Contains(networks, `"laradev-proxy"`) {
				return fmt.Errorf("attach www to Caddy network: %s: %w", strings.TrimSpace(connectErr.String()), err)
			}
		}
		if err := p.ReconcileProject(ctx, c.Config.Project.ID, c.WorktreeID, rs); err != nil {
			return err
		}
	} else {
		p, err := proxy.New(l.Runner)
		if err != nil {
			return err
		}
		if err := p.ReconcileProject(ctx, c.Config.Project.ID, c.WorktreeID, nil); err != nil {
			return err
		}
	}
	manager, err := l.dnsManager()
	if err != nil {
		return err
	}
	if err := manager.SyncProject(c.Config.Project.ID, domainNames(c.Config.Domains)); err != nil {
		return err
	}
	if err := manager.Refresh(ctx); err != nil {
		return err
	}
	return nil
}
func (l Lifecycle) ensureNetwork(ctx context.Context, name string, c project.Context) error {
	if err := l.Runner.Run(ctx, []string{"inspect", name}, nil, io.Discard, io.Discard); err == nil {
		return nil
	}
	return l.Runner.Run(ctx, []string{"network", "create", "--label", managedLabel, "--label", "com.laradev.project-id=" + c.Config.Project.ID, "--label", "com.laradev.role=network", name}, nil, io.Discard, io.Discard)
}
func (l Lifecycle) ensureMySQL(ctx context.Context, c project.Context, network, mysql, pma string) error {
	r := l.resources()
	fp := config.InitFingerprint(c.Config)
	volume := "laradev-" + c.Config.Project.ID + "-mysql-data"
	if err := r.Run(ctx, "volume", "inspect", volume); err != nil {
		if err := l.Runner.Run(ctx, []string{"volume", "create", "--label", managedLabel, "--label", "com.laradev.project-id=" + c.Config.Project.ID, "--label", "com.laradev.role=mysql", "--label", "com.laradev.mysql-init-fingerprint=" + fp, volume}, nil, io.Discard, io.Discard); err != nil {
			return err
		}
	}
	existing, inspectErr := r.Inspect(ctx, mysql)
	if inspectErr != nil {
		if err := l.createMySQL(ctx, c, network, mysql, volume, fp); err != nil {
			return fmt.Errorf("mysql start failed: %w", configError(err, c.Config))
		}
	} else {
		envDrift := existing.Env["MYSQL_DATABASE"] != c.Config.MySQL.Database || existing.Env["MYSQL_USER"] != c.Config.MySQL.Username || existing.Env["MYSQL_PASSWORD"] != c.Config.MySQL.Password || existing.Env["MYSQL_ROOT_PASSWORD"] != c.Config.MySQL.RootPassword
		imageDrift := existing.Image != c.Config.MySQL.Image
		fingerprintDrift := existing.Labels["com.laradev.mysql-init-fingerprint"] != fp
		if envDrift {
			if existing.State == "running" {
				if err := l.Runner.Run(ctx, []string{"stop", mysql}, nil, io.Discard, io.Discard); err != nil {
					return fmt.Errorf("stop mysql for credential migration: %w", err)
				}
			}
			if err := l.migrateMySQL(ctx, c, volume); err != nil {
				return err
			}
		}
		if envDrift || imageDrift || fingerprintDrift {
			if err := l.Runner.Run(ctx, []string{"rm", "-f", mysql}, nil, io.Discard, io.Discard); err != nil {
				return fmt.Errorf("recreate mysql container: %w", err)
			}
			if err := l.createMySQL(ctx, c, network, mysql, volume, fp); err != nil {
				return fmt.Errorf("mysql start failed: %w", configError(err, c.Config))
			}
		} else if existing.State != "running" {
			if err := l.Runner.Run(ctx, []string{"start", mysql}, nil, io.Discard, io.Discard); err != nil {
				return err
			}
		}
	}
	pmaResource, pmaErr := r.Inspect(ctx, pma)
	pmaPortLabel := fmt.Sprintf("%d", c.Config.PHPMyAdmin.HostPort)
	if pmaErr != nil {
		if err := l.createPHPMyAdmin(ctx, c, network, pma); err != nil {
			return fmt.Errorf("phpMyAdmin start failed: %w", configError(err, c.Config))
		}
	} else if pmaResource.Labels["com.laradev.phpmyadmin-host-port"] != pmaPortLabel {
		if err := l.Runner.Run(ctx, []string{"rm", "-f", pma}, nil, io.Discard, io.Discard); err != nil {
			return fmt.Errorf("recreate phpMyAdmin container: %w", err)
		}
		if err := l.createPHPMyAdmin(ctx, c, network, pma); err != nil {
			return fmt.Errorf("phpMyAdmin start failed: %w", configError(err, c.Config))
		}
	} else {
		if pmaResource.State != "running" {
			if err := l.Runner.Run(ctx, []string{"start", pma}, nil, io.Discard, io.Discard); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l Lifecycle) createMySQL(ctx context.Context, c project.Context, network, mysql, volume, fp string) error {
	args := []string{"run", "-d", "--name", mysql, "--network", network, "--network-alias", "mysql", "--label", managedLabel, "--label", "com.laradev.project-id=" + c.Config.Project.ID, "--label", "com.laradev.role=mysql", "--label", "com.laradev.project-root=" + c.ProjectRoot, "--label", "com.laradev.mysql-init-fingerprint=" + fp, "-e", "MYSQL_DATABASE=" + c.Config.MySQL.Database, "-e", "MYSQL_USER=" + c.Config.MySQL.Username, "-e", "MYSQL_PASSWORD=" + c.Config.MySQL.Password, "-e", "MYSQL_ROOT_PASSWORD=" + c.Config.MySQL.RootPassword, "-e", "MYSQL_ROOT_HOST=%", "-v", volume + ":/var/lib/mysql", "--health-cmd", "mysqladmin ping -h localhost -u root -p$${MYSQL_ROOT_PASSWORD}", "--health-interval", "5s", "--health-timeout", "3s", "--health-retries", "20", c.Config.MySQL.Image}
	return l.Runner.Run(ctx, args, nil, io.Discard, io.Discard)
}

func (l Lifecycle) createPHPMyAdmin(ctx context.Context, c project.Context, network, pma string) error {
	args := []string{"run", "-d", "--name", pma, "--network", network, "--label", managedLabel, "--label", "com.laradev.project-id=" + c.Config.Project.ID, "--label", "com.laradev.role=phpmyadmin", "--label", "com.laradev.phpmyadmin-host-port=" + fmt.Sprintf("%d", c.Config.PHPMyAdmin.HostPort), "-e", "PMA_HOST=mysql", "-e", "PMA_PORT=3306", "-e", "PMA_USER=root", "-e", "PMA_PASSWORD=" + c.Config.MySQL.RootPassword, "-p", fmt.Sprintf("127.0.0.1:%d:80", c.Config.PHPMyAdmin.HostPort), "phpmyadmin:5-apache"}
	return l.Runner.Run(ctx, args, nil, io.Discard, io.Discard)
}

func (l Lifecycle) migrateMySQL(ctx context.Context, c project.Context, volume string) error {
	sql := fmt.Sprintf("FLUSH PRIVILEGES; CREATE DATABASE IF NOT EXISTS %s; CREATE USER IF NOT EXISTS %s@'%%' IDENTIFIED BY %s; ALTER USER %s@'%%' IDENTIFIED BY %s; GRANT ALL PRIVILEGES ON %s.* TO %s@'%%'; CREATE USER IF NOT EXISTS 'root'@'%%' IDENTIFIED BY %s; ALTER USER 'root'@'%%' IDENTIFIED BY %s; CREATE USER IF NOT EXISTS 'root'@'localhost' IDENTIFIED BY %s; ALTER USER 'root'@'localhost' IDENTIFIED BY %s; FLUSH PRIVILEGES;", mysqlIdentifier(c.Config.MySQL.Database), mysqlString(c.Config.MySQL.Username), mysqlString(c.Config.MySQL.Password), mysqlString(c.Config.MySQL.Username), mysqlString(c.Config.MySQL.Password), mysqlIdentifier(c.Config.MySQL.Database), mysqlString(c.Config.MySQL.Username), mysqlString(c.Config.MySQL.RootPassword), mysqlString(c.Config.MySQL.RootPassword), mysqlString(c.Config.MySQL.RootPassword), mysqlString(c.Config.MySQL.RootPassword))
	script := `set -eu
mysqld --user=mysql --skip-grant-tables --skip-networking >/tmp/laradev-mysqld.log 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true' EXIT
i=0
until mysqladmin --protocol=socket --socket=/var/run/mysqld/mysqld.sock ping >/dev/null 2>&1; do
  if ! kill -0 "$pid" 2>/dev/null || [ "$i" -ge 60 ]; then
    cat /tmp/laradev-mysqld.log
    exit 1
  fi
  i=$((i + 1))
  sleep 1
done
mysql --protocol=socket --socket=/var/run/mysqld/mysqld.sock -uroot --execute "$LARADEV_SQL"
kill "$pid"
wait "$pid" || true
`
	maintenance := []string{"run", "--rm", "--name", "laradev-mysql-maintenance-" + c.Config.Project.ID, "--network", "none", "--entrypoint", "sh", "-e", "LARADEV_SQL=" + sql, "-v", volume + ":/var/lib/mysql", c.Config.MySQL.Image, "-c", script}
	maintenanceCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := l.Runner.Run(maintenanceCtx, maintenance, nil, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("mysql credential migration failed: %w", configError(err, c.Config))
	}
	return nil
}

func mysqlString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "'", "\\'")
	value = strings.ReplaceAll(value, "\x00", "\\0")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\x1a", "\\Z")
	return "'" + value + "'"
}

func mysqlIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}
func (l Lifecycle) ensureWWW(ctx context.Context, c project.Context, network, www string, labels []string) error {
	r := l.resources()
	if existing, err := r.Inspect(ctx, www); err == nil {
		expected := map[string]string{}
		for i := 0; i+1 < len(labels); i += 2 {
			if strings.HasPrefix(labels[i], "--label") {
				parts := strings.SplitN(labels[i+1], "=", 2)
				if len(parts) == 2 {
					expected[parts[0]] = parts[1]
				}
			}
		}
		if docker.Managed(existing.Labels) && docker.LabelsMatch(existing.Labels, expected) {
			if existing.State != "running" {
				return l.Runner.Run(ctx, []string{"start", www}, nil, io.Discard, io.Discard)
			}
			return nil
		}
		if !docker.Managed(existing.Labels) || existing.Labels["com.laradev.role"] != "www" || existing.Labels["com.laradev.project-id"] != c.Config.Project.ID {
			return fmt.Errorf("refusing conflicting unmanaged www container %s", www)
		}
		if err := l.Runner.Run(ctx, []string{"rm", "-f", www}, nil, io.Discard, io.Discard); err != nil {
			return fmt.Errorf("unable to recreate drifted www container %s: %w", www, err)
		}
	}
	args := []string{"run", "-d", "--name", www, "--network", network}
	for _, p := range c.Config.Domains {
		_ = p
	}
	for _, label := range labels {
		args = append(args, label)
	}
	for _, mapping := range c.Config.WWW.Ports {
		h, t, _ := config.ParsePort(mapping)
		args = append(args, "-p", fmt.Sprintf("0.0.0.0:%d:%d", h, t))
	}
	args = append(args, "-v", c.WorktreeRoot+":/app", "-w", "/app", "-e", "WEB_DOCUMENT_ROOT=/app/public")
	if c.Config.MySQL.Enabled {
		args = append(args, "-e", "DB_CONNECTION=mysql", "-e", "DB_HOST=mysql", "-e", "DB_PORT=3306", "-e", "DB_DATABASE="+c.Config.MySQL.Database, "-e", "DB_USERNAME="+c.Config.MySQL.Username, "-e", "DB_PASSWORD="+c.Config.MySQL.Password)
	}
	args = append(args, c.Config.Runtime.Image)
	if err := l.Runner.Run(ctx, args, nil, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("www start failed: %w", configError(err, c.Config))
	}
	return nil
}
func configError(err error, c config.Config) error { return errors.New(config.Redact(err.Error(), c)) }
func (l Lifecycle) Stop(ctx context.Context, c project.Context) error {
	r := l.resources()
	if err := r.DockerAvailable(ctx); err != nil {
		return err
	}
	_, _, pma, www := l.names(c)
	if err := l.stopIfExists(ctx, www); err != nil {
		return err
	}
	out, err := r.Output(ctx, "ps", "-q", "--filter", "label=com.laradev.project-id="+c.Config.Project.ID, "--filter", "label=com.laradev.role=www")
	if err != nil {
		return fmt.Errorf("find project worktrees: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		if err := l.stopIfExists(ctx, pma); err != nil {
			return err
		}
		mysql := "laradev-" + c.Config.Project.ID + "-mysql"
		if err := l.stopIfExists(ctx, mysql); err != nil {
			return err
		}
	}
	if err := l.refreshDNS(ctx); err != nil {
		return err
	}
	return nil
}
func (l Lifecycle) Down(ctx context.Context, c project.Context) error {
	if err := l.Stop(ctx, c); err != nil {
		return err
	}
	_, mysql, pma, www := l.names(c)
	if err := l.removeIfExists(ctx, www); err != nil {
		return err
	}
	out, err := l.resources().Output(ctx, "ps", "-aq", "--filter", "label=com.laradev.project-id="+c.Config.Project.ID, "--filter", "label=com.laradev.role=www")
	if err != nil {
		return fmt.Errorf("find project worktrees: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		if err := l.removeIfExists(ctx, pma); err != nil {
			return err
		}
		if err := l.removeIfExists(ctx, mysql); err != nil {
			return err
		}
	}
	return nil
}
func (l Lifecycle) StopAll(ctx context.Context) error {
	out, err := l.resources().Output(ctx, "ps", "-q", "--filter", "label=com.laradev.managed=true")
	if err != nil {
		return err
	}
	for _, id := range strings.Fields(out) {
		if err := l.Runner.Run(ctx, []string{"stop", id}, nil, io.Discard, io.Discard); err != nil {
			return fmt.Errorf("stop managed container %s: %w", id, err)
		}
	}
	if err := l.refreshDNS(ctx); err != nil {
		return err
	}
	return nil
}
func (l Lifecycle) Cleanup(ctx context.Context) error {
	out, err := l.resources().Output(ctx, "ps", "-aq", "--filter", "label=com.laradev.managed=true")
	if err != nil {
		return err
	}
	for _, id := range strings.Fields(out) {
		if err := l.Runner.Run(ctx, []string{"rm", "-f", id}, nil, io.Discard, io.Discard); err != nil {
			return fmt.Errorf("remove managed container %s: %w", id, err)
		}
	}
	if err := l.refreshDNS(ctx); err != nil {
		return err
	}
	return nil
}

func (l Lifecycle) containerExists(ctx context.Context, name string) (bool, error) {
	out, err := l.resources().Output(ctx, "ps", "-aq", "--filter", "name=^/?"+name+"$")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (l Lifecycle) stopIfExists(ctx context.Context, name string) error {
	exists, err := l.containerExists(ctx, name)
	if err != nil {
		return fmt.Errorf("inspect %s before stopping: %w", name, err)
	}
	if !exists {
		return nil
	}
	if err := l.Runner.Run(ctx, []string{"stop", name}, nil, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("stop %s: %w", name, err)
	}
	return nil
}

func (l Lifecycle) removeIfExists(ctx context.Context, name string) error {
	exists, err := l.containerExists(ctx, name)
	if err != nil {
		return fmt.Errorf("inspect %s before removal: %w", name, err)
	}
	if !exists {
		return nil
	}
	if err := l.Runner.Run(ctx, []string{"rm", "-f", name}, nil, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}
func (l Lifecycle) Status(ctx context.Context, c *project.Context, w io.Writer) error {
	fmt.Fprintf(w, "PROJECT %s\nCONFIG %s\nWORKTREE %s\nIMAGE %s\n", c.Config.Project.ID, c.ConfigPath, c.WorktreeRoot, c.Config.Runtime.Image)
	fmt.Fprintln(w, "SERVICES")
	fmt.Fprintln(w, "SERVICE\tSCOPE\tCONTAINER\tSTATE\tHEALTH\tPORTS")
	_, mysql, pma, www := l.names(*c)
	for _, x := range []struct{ role, name string }{{"www", www}, {"mysql", mysql}, {"phpmyadmin", pma}, {"caddy", "laradev-caddy"}} {
		state := "not-created"
		if resource, err := l.resources().Inspect(ctx, x.name); err == nil {
			state = resource.State
		}
		if x.role == "phpmyadmin" && !c.Config.MySQL.Enabled {
			state = "disabled"
		}
		fmt.Fprintf(w, "%s\tproject\t%s\t%s\t-\t-\n", x.role, x.name, state)
	}
	fmt.Fprintln(w, "DOMAINS")
	fmt.Fprintln(w, "DOMAIN\tTARGET\tBACKEND\tROUTE\tTLS")
	for _, d := range c.Config.Domains {
		fmt.Fprintf(w, "%s\twww:%d\t%s\t%s\tready\n", d.Name, d.Port, www, d.Name)
	}
	return nil
}
func (l Lifecycle) StatusGlobal(ctx context.Context, w io.Writer) error {
	out, err := l.resources().Output(ctx, "ps", "-a", "--filter", "label=com.laradev.managed=true", "--format", "{{.Names}}\t{{.Label \"com.laradev.project-id\"}}\t{{.State}}")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "PROJECTS")
	fmt.Fprintln(w, "CONTAINER\tPROJECT\tSTATE")
	if strings.TrimSpace(out) != "" {
		_, _ = io.WriteString(w, out)
		if !strings.HasSuffix(out, "\n") {
			_, _ = io.WriteString(w, "\n")
		}
	}
	return nil
}
func (l Lifecycle) WaitHealthy(ctx context.Context, name string) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if err := l.Runner.Run(ctx, []string{"inspect", "--format", "{{.State.Health.Status}}", name}, nil, io.Discard, io.Discard); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("container health check timed out")
}
func sortedRoutes(c config.Config) []config.DomainRoute {
	d := append([]config.DomainRoute(nil), c.Domains...)
	sort.Slice(d, func(i, j int) bool { return d[i].Name < d[j].Name })
	return d
}
func worktreePath(c project.Context) string { p, _ := filepath.Abs(c.WorktreeRoot); return p }
func _unused()                              { _ = context.Background(); _ = os.ErrNotExist; _ = worktreePath }
