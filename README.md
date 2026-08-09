# laradev

`laradev` is a Docker-backed Laravel development CLI for Linux/amd64. It gives each project worktree an isolated PHP/Node www container, optional MySQL and phpMyAdmin services, Docker-routed HTTPS domains through a shared Caddy container, and UID/GID `1000:1000` command forwarding.

The project is designed for a host with Docker but does not require a host Go, PHP, Composer, Node, npm, or pnpm installation for project work. Go builds run in the pinned Docker toolchain defined by the root `Makefile`.

## What it manages

- Laravel project configuration in a root `.laradev.yml` file.
- A deterministic project identity such as `ldev_a9297f5de664`.
- A separate www container for each Git worktree.
- Shared per-project MySQL and phpMyAdmin containers.
- Persistent named MySQL data volumes that are never removed by normal lifecycle commands.
- A global Docker Caddy container named `laradev-caddy` for HTTPS routes.
- Host-side command shims for `php`, `composer`, `node`, `npm`, and `pnpm`.
- Certificates and route state under `$XDG_CONFIG_HOME/laradev` or `$HOME/.config/laradev`.

## Requirements

- Linux/amd64 is the supported build target.
- Docker Engine must be running and usable by the current user.
- Docker images for the selected Laravel runtime, for example:
  - `rohan2388/laravel-server:php8.1-node22`
  - `rohan2388/laravel-server:php8.2-node22`
  - `rohan2388/laravel-server:php8.3-node22`
  - `rohan2388/laravel-server:php8.4-node22`
- Ubuntu with `systemd-resolved` active is required for automatic `.test`
  split-DNS routing.
- The first active `.test` route uses `sudo` once to install the single
  laradev-owned systemd-resolved drop-in. Later domain changes do not require
  `sudo`.
- Docker can pull `dockurr/dnsmasq:latest` for the managed DNS container.
- `mkcert` is required only for HTTPS certificate generation.
- `make` is required for the Dockerized build workflow.

The default generated configuration publishes www ports `80:80` and `5173:5173`
on localhost. phpMyAdmin defaults to `127.0.0.1:88`.

## Build, test, and install

Clone or enter this repository, then run:

```bash
make fmt
make test
make build
```

The commands run Go inside `golang:1.23-alpine` with `CGO_ENABLED=0`, `GOOS=linux`, and `GOARCH=amd64`. Build output is written to `bin/laradev`.

Install the binary and default forwarding shims:

```bash
make dev BIN_DIR="$HOME/.local/bin"
export PATH="$HOME/.local/bin:$PATH"
```

The installer writes a manifest at `$HOME/.local/bin/.laradev-install.json` and installs these default entrypoints:

```text
laradev
php
composer
node
npm
pnpm
```

Use a temporary directory to test installation without changing your normal bin directory:

```bash
TMP_BIN="$(mktemp -d)"
make dev BIN_DIR="$TMP_BIN"
PATH="$TMP_BIN:$PATH" laradev -h
```

## Help

Every command supports short and long help:

```bash
laradev -h
laradev --help
laradev help domain
laradev domain -h
laradev domain add -h
laradev command add --help
```

## Create a project configuration

Initialize an existing Laravel directory interactively:

```bash
cd /path/to/my-project
laradev init
```

Or initialize another directory:

```bash
laradev init /path/to/my-project
```

The prompts configure:

1. PHP version: `8.1`, `8.2`, `8.3`, or `8.4`.
2. Node version: currently `22`.
3. MySQL enabled or disabled.
4. MySQL database, username, password, and root password when enabled.
5. Host-to-container www port mappings.
6. An optional primary domain and internal www target port.
7. Additional forwarded command basenames.

The configuration is validated and atomically written as `.laradev.yml` with mode `0600`. When Git is available, `/.laradev.yml` is added to the common Git `info/exclude`; `.gitignore` is not modified.

## Scaffold a new Laravel project

`new` creates or uses an empty directory, then runs Composer and the frontend build inside the selected Docker image:

```bash
laradev new /path/to/myapp
```

Override the Composer Laravel constraint when needed:

```bash
laradev new /path/to/myapp --laravel-version '^12.0'
```

The default Laravel constraint is selected from PHP:

| PHP | Default Laravel constraint |
| --- | --- |
| 8.1 | `^10.0` |
| 8.2 | `^12.0` |
| 8.3 | `^13.0` |
| 8.4 | `^13.0` |

The sequence is:

1. Prompt for the project configuration.
2. Run Composer `create-project` as UID/GID `1000:1000`.
3. Run `npm install` as UID/GID `1000:1000`.
4. Run `npm run build` as UID/GID `1000:1000`.
5. Write `.laradev.yml` only after those steps succeed.

`new` does not start Docker services automatically. Run `laradev up` afterward.

## Start and inspect services

From the project root or any child directory:

```bash
laradev up
laradev status
```

### First-run checklist

`laradev up` starts the containers, but two application-side settings are
required before your Laravel app behaves correctly. Think of this as the
**container handshake**: Vite must listen on the Docker network, and Laravel
must use Docker's MySQL service name.

#### 1. Configure Vite before `pnpm run dev`

In `vite.config.js`, use this exact server configuration:

```js
server: {
    host: '0.0.0.0',
    strictPort: true,
    hmr: {
        host: '127.0.0.1',
    },
},
```

Why every line matters:

- `host: '0.0.0.0'` makes Vite reachable through Docker's published port.
- `strictPort: true` prevents Vite from silently moving to `5174` when `5173`
  is occupied. laradev publishes `5173`, not arbitrary fallback ports.
- `hmr.host: '127.0.0.1'` makes the browser connect to the host-published
  Vite server for HMR.

Then start the stack and Vite:

```bash
laradev up
pnpm run dev
```

You should see Vite report `http://localhost:5173/`. If it reports
`5174` or another port, stop the process already using `5173` before
continuing.

#### 2. Configure Laravel's database connection

These values are for the Laravel app **inside the www container**:

```dotenv
DB_CONNECTION=mysql
DB_HOST=mysql
DB_PORT=3306
DB_DATABASE=www
DB_USERNAME=user
DB_PASSWORD=password
```

Use the actual `database`, `username`, and `password` values from the
`mysql` section of `.laradev.yml` if you changed the defaults. The important
detail is `DB_HOST=mysql`: that is the Docker network alias. Do not use
`127.0.0.1`, `localhost`, or the phpMyAdmin host port from inside www.
MySQL port `3306` is intentionally not published to the host.

After changing `.env`, clear Laravel's cached configuration if necessary:

```bash
laradev exec php artisan config:clear
laradev exec php artisan migrate
```

`laradev up` prints this checklist after a successful startup. It shows the
safe database fields and points `DB_PASSWORD` to your local `.laradev.yml`
value without echoing the password into terminal history or logs.

`up` creates or starts:

- `laradev-<project-id>-www-<worktree-id>`
- `laradev-<project-id>-mysql` when MySQL is enabled
- `laradev-<project-id>-phpmyadmin` when MySQL is enabled
- `laradev-caddy` when domains are configured

The current worktree is mounted at `/app`. www is published only on `127.0.0.1`. MySQL port `3306` is not published to the host.

`status` shows project identity, service state, configured ports, and domains. Run it outside a project to list managed containers globally.

## Stop versus down

Pause a project without deleting containers:

```bash
laradev stop
```

`stop` preserves container IDs, configuration, networks, and the MySQL data volume. A later `laradev up` can use `docker start` when configuration still matches.

Remove project containers while preserving data:

```bash
laradev down
```

`down` removes the current worktree www container and removes shared project containers only when no other worktree is using them. It preserves the MySQL named volume, project/proxy networks, certificates, and Caddy state.

Stop all managed containers, including Caddy:

```bash
laradev stop-all
```

Remove managed containers whose labeled project paths no longer exist:

```bash
laradev cleanup
```

`cleanup` does not remove MySQL or Caddy volumes and does not touch resources without valid laradev ownership labels.

## Execute commands and open shells

Forwarded commands run inside www as UID/GID `1000:1000` with `HOME=/tmp/laradev-home`:

```bash
laradev exec php artisan migrate
laradev exec composer install
laradev exec node --version
laradev exec npm run build
laradev exec pnpm run dev
```

Open shells directly:

```bash
laradev sh
laradev bash
```

The shell commands do not auto-start www. `bash` does not silently fall back to `sh`.

The current host working directory is mapped into `/app/<relative-path>`. Paths that escape the current worktree are rejected.

## Command shims and forwarding allowlists

List commands configured for the current project:

```bash
laradev command list
```

Add a custom basename:

```bash
laradev command add artisan-helper
```

If the name already resolves to a host executable, explicitly allow a shim to shadow it:

```bash
laradev command add id --shadow-host
```

Remove a custom command:

```bash
laradev command remove artisan-helper
```

The mandatory commands `php`, `composer`, `node`, `npm`, and `pnpm` cannot be removed. A command must be a basename, not a path, and `laradev`, `sh`, and `bash` are reserved.

Install or refresh shims:

```bash
laradev install
laradev install --bin-dir "$HOME/.local/bin"
laradev install --shadow-host id
```

Repeated installs replace only resources owned by the laradev install manifest. Unrelated files are refused rather than overwritten.

When a shim is invoked outside a project, laradev removes managed install directories from the fallback search and executes the host command if one exists. Inside a project, a configured command is forwarded into www.

## Domains and HTTPS

List configured domains:

```bash
laradev domain list
```

Add a domain targeting container port 80:

```bash
laradev domain add myapp.test
```

Add a domain targeting another internal port, such as a WebSocket server:

```bash
laradev domain add ws.myapp.test --port 8080
```

Remove a domain:

```bash
laradev domain remove ws.myapp.test
```

`domain add` validates the hostname, ensures a `mkcert` certificate, writes the
route to the canonical project config, and updates the DNS manifest. The port
is the TCP port inside www; it does not need to be published in `www.ports` for
Caddy routing.

### Explicit `.test` DNS

laradev runs `dockurr/dnsmasq:latest` in the managed container
`laradev-dnsmasq`, bound only to `127.0.0.1:15353` for both TCP and UDP. A
route is sent to dnsmasq only when its project www container is running.

The first active `.test` route installs
`/etc/systemd/resolved.conf.d/laradev-dns.conf` with `sudo`; this is the only
host resolver file laradev owns. The drop-in routes only `~test` to dnsmasq.
Later `domain add`, `domain remove`, and refresh operations do not require
`sudo`. laradev never edits `/etc/hosts`, `/etc/resolv.conf`, or
NetworkManager profiles.

Use the manual controls:

```bash
laradev dns status
laradev dns start
laradev dns refresh
laradev dns stop
```

An apex and wildcard are separate entries:

```bash
laradev domain add mydomain.test
laradev domain add '*.mydomain.test'
```

The first command routes only `mydomain.test`; the second routes supported
subdomains such as `api.mydomain.test`. The wildcard does not synthesize or
replace the apex. Unregistered `.test` names are not mapped to
`127.0.0.1`. `domain remove` removes exactly the named entry.

The global proxy uses:

- Container: `laradev-caddy`
- Network: `laradev-proxy`
- Host binding: `127.0.0.1:443:443`
- Caddyfile: `$HOME/.config/laradev/caddy/Caddyfile`
- Route manifest: `$HOME/.config/laradev/caddy/routes.json`
- Certificates: `$HOME/.config/laradev/certs/<domain>/`

## Vite development server

The default `www.ports` includes `5173:5173`. Vite must listen on all container interfaces so Docker can forward that port. A Laravel Vite config should include:

```js
export default defineConfig({
    // plugins...
    server: {
        host: '0.0.0.0',
        strictPort: true,
        hmr: {
            host: '127.0.0.1',
        },
    },
});
```

Then run:

```bash
laradev up
pnpm run dev
```

`strictPort: true` is important: without it, Vite silently changes to `5174` or another port when `5173` is occupied, while laradev still publishes only `5173`.

## Project configuration

A typical `.laradev.yml` looks like:

```yaml
version: 1
project:
  id: ldev_a9297f5de664
runtime:
  php: "8.4"
  node: "22"
  image: rohan2388/laravel-server:php8.4-node22
  base_dir: /app
  webroot: /app/public
www:
  ports:
    - "80:80"
    - "5173:5173"
commands:
  forward:
    - composer
    - node
    - npm
    - php
    - pnpm
domains:
  - name: myapp.test
    port: 80
mysql:
  enabled: true
  image: mysql:8.0
  database: www
  username: user
  password: password
  root_password: generated-value
phpmyadmin:
  host_port: 88
```

Important rules:

- PHP must be `8.1`, `8.2`, `8.3`, or `8.4`.
- Node must be `22`.
- The runtime image must match the selected PHP and Node versions.
- www must publish container port `80`.
- Host and container ports must be unique and in `1..65535`.
- Domain names are normalized to lowercase and cannot be IPs, wildcards, paths, or schemes.
- MySQL credentials must be non-empty when MySQL is enabled.
- Passwords are not printed by `status` or stored in Docker labels.

## Troubleshooting

### Vite says `Port 5173 is in use`

Find and stop the existing dev server, then restart Vite:

```bash
laradev sh -lc 'ps -ef | grep vite'
laradev sh -lc 'kill <pid>'
pnpm run dev
```

With `strictPort: true`, Vite exits instead of switching to an unpublished port.

### Verify port publication

```bash
docker port "$(docker ps -q --filter name=-www-)" 5173/tcp
curl http://127.0.0.1:5173/@vite/client
```

### Verify service state

```bash
laradev status
docker ps -a --filter label=com.laradev.managed=true
```

### Docker is unavailable

Check that the daemon is running and the current user can access it:

```bash
docker version
```

### Project files are not writable in the container

laradev deliberately does not run `chown` on host files. Ensure the worktree is readable and writable by numeric UID/GID `1000:1000`.

## Development layout

```text
cmd/laradev/              CLI entrypoint
internal/config/          YAML model, validation, atomic persistence
internal/project/         project/worktree discovery
internal/docker/          argv-only Docker runner and resource helpers
internal/commands/        lifecycle, exec, install, init, and scaffold commands
internal/proxy/           Caddy routes and certificates
internal/state/           protected user state paths
internal/lock/            cross-process locks
internal/prompt/          interactive prompts
Makefile                  Dockerized format, test, build, and install workflow
```

## License

No license has been selected yet.
