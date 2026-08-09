package commands

import (
	"fmt"
	"io"
)

func writeHelp(w io.Writer, args []string) error {
	_, err := io.WriteString(w, helpText(args))
	return err
}

func helpText(args []string) string {
	if len(args) == 0 {
		return topHelp
	}
	if len(args) > 1 {
		switch args[0] {
		case "command":
			switch args[1] {
			case "list":
				return commandListHelp
			case "add":
				return commandAddHelp
			case "remove":
				return commandRemoveHelp
			}
		case "domain":
			switch args[1] {
			case "list":
				return domainListHelp
			case "add":
				return domainAddHelp
			case "remove":
				return domainRemoveHelp
			}
		}
	}
	switch args[0] {
	case "init":
		return initHelp
	case "new":
		return newHelp
	case "up":
		return upHelp
	case "stop":
		return stopHelp
	case "down":
		return downHelp
	case "status":
		return statusHelp
	case "stop-all":
		return stopAllHelp
	case "cleanup":
		return cleanupHelp
	case "exec":
		return execHelp
	case "sh":
		return shHelp
	case "bash":
		return bashHelp
	case "install":
		return installHelp
	case "command":
		return commandHelp
	case "domain":
		return domainHelp
	default:
		return fmt.Sprintf("unknown command %q\n\n%s", args[0], topHelp)
	}
}

func isHelpArg(s string) bool { return s == "-h" || s == "--help" }

const topHelp = `laradev - isolated Laravel development environments

USAGE
  laradev <command> [options]
  <forwarded-command> [args...]

PROJECT COMMANDS
  init [directory]                 Create .laradev.yml interactively.
  new <directory>                  Scaffold a Laravel project, then write config.
  up                               Create or start the current project's services.
  stop                             Stop services without deleting containers or data.
  down                             Remove project containers; preserve MySQL data.
  status                           Show current project services and domains.
  stop-all                         Stop every managed container.
  cleanup                          Remove managed resources whose project paths are gone.

SHELL AND FORWARDING
  exec <command> [args...]         Run an allowed command in the www container.
  sh [args...]                     Open /bin/sh in the www container.
  bash [args...]                   Open /bin/bash in the www container.
  command <list|add|remove>        Manage per-project forwarded commands.

DOMAINS AND INSTALLATION
  domain <list|add|remove>         Manage HTTPS routes through the Docker Caddy proxy.
  install                          Install laradev and configured command shims.

GLOBAL HELP
  laradev -h
  laradev <command> -h
  laradev <command> <subcommand> -h

Run commands from a project directory unless the command says otherwise.
Project configuration is stored in .laradev.yml; passwords are not shown by status.
`

const initHelp = `USAGE
  laradev init [directory]

Interactively create a root .laradev.yml. The prompts select PHP, Node, MySQL,
local www port mappings, an optional primary domain, and extra forwarded commands.
The config is written atomically with mode 0600. Existing configs are never overwritten.

EXAMPLES
  laradev init
  laradev init ./my-project
`

const newHelp = `USAGE
  laradev new <directory> [--laravel-version <constraint>]

Create an empty or missing directory, scaffold Laravel with Composer in Docker,
run npm install and npm run build, then write .laradev.yml. The environment is
not started automatically; run laradev up afterward.

OPTIONS
  --laravel-version <constraint>  Override the PHP-derived Composer constraint.

EXAMPLES
  laradev new ./myapp
  laradev new ./myapp --laravel-version '^12.0'
`

const upHelp = `USAGE
  laradev up

Resolve the current project/worktree, validate Docker resources, and create or
start the www, MySQL, phpMyAdmin, and Caddy services required by .laradev.yml.
The current worktree is mounted at /app. MySQL data is kept in a named volume.

Run from the project root or any directory below it.
`

const stopHelp = `USAGE
  laradev stop

Stop the current worktree's www container. If no other worktree for the project
is running, shared MySQL and phpMyAdmin are stopped too. Containers, networks,
configuration, and the MySQL volume are preserved.
`

const downHelp = `USAGE
  laradev down

Remove the current worktree's www container. Shared project containers are
removed only when no other project worktree is running. The MySQL named volume,
networks, configuration, and Caddy state are preserved.
`

const statusHelp = `USAGE
  laradev status

Show project identity, canonical config/worktree roots, managed service state,
ports, configured domains, and active route information. Outside a project,
show all managed project containers.
`

const stopAllHelp = `USAGE
  laradev stop-all

Stop every container labeled as laradev-managed, including Caddy. Definitions,
networks, volumes, and containers remain available for a later laradev up.
Unrelated Docker resources are not changed.
`

const cleanupHelp = `USAGE
  laradev cleanup

Remove managed containers whose labeled project or worktree paths no longer
exist. Preserve MySQL and Caddy volumes. Unmanaged or uncertain resources are
left untouched.
`

const execHelp = `USAGE
  laradev exec <command> [args...]

Run a command from the current project's commands.forward allowlist inside the
current www container as UID/GID 1000:1000. The current host directory maps to
/app-relative workdir, HOME is /tmp/laradev-home, and arguments are passed without
a shell. The www container must already be running.

EXAMPLES
  laradev exec php artisan migrate
  laradev exec composer install
  laradev exec npm run dev
`

const shHelp = `USAGE
  laradev sh [args...]

Run /bin/sh directly in the current running www container as UID/GID 1000:1000.
The command does not start the container automatically.
`

const bashHelp = `USAGE
  laradev bash [args...]

Run /bin/bash directly in the current running www container as UID/GID 1000:1000.
There is no fallback to /bin/sh when bash is unavailable.
`

const installHelp = `USAGE
  laradev install [--bin-dir <directory>] [--shadow-host <name>...]

Install the current laradev binary and relative forwarding shims. Installation
uses a manifest so repeat installs replace only resources managed by laradev.
Mandatory shims are php, composer, node, npm, and pnpm. Additional host-command
shadowing requires --shadow-host.

OPTIONS
  --bin-dir <directory>          Installation directory; default $HOME/.local/bin.
  --shadow-host <name>           Permit an additional shim to shadow a host command.
                                 Repeat the option for multiple names.

EXAMPLES
  laradev install
  laradev install --bin-dir "$HOME/.local/bin"
  laradev install --shadow-host id
`

const commandHelp = `USAGE
  laradev command list
  laradev command add <name> [--shadow-host]
  laradev command remove <name>

Manage the current project's commands.forward allowlist. Forwarded commands are
available through laradev exec and installed basename shims. php, composer, node,
npm, and pnpm are mandatory and cannot be removed.

OPTIONS
  --shadow-host                   Allow command add to shadow a host executable.

EXAMPLES
  laradev command list
  laradev command add artisan-helper --shadow-host
  laradev command remove artisan-helper
`

const commandListHelp = `USAGE
  laradev command list

List mandatory and custom forwarded command basenames configured for this project.
`
const commandAddHelp = `USAGE
  laradev command add <name> [--shadow-host]

Add a valid basename to commands.forward. Names cannot contain paths or whitespace,
and laradev, sh, and bash are reserved. Use --shadow-host when the name resolves
to an existing host executable.
`
const commandRemoveHelp = `USAGE
  laradev command remove <name>

Remove a non-mandatory command from this project's allowlist. The global shim is
left in place because another project may still use it.
`

const domainHelp = `USAGE
  laradev domain list
  laradev domain add <hostname> [--port <www-port>]
  laradev domain remove <hostname>

Manage project HTTPS routes through the global Docker Caddy proxy. Domain names
are normalized to lowercase. The route port is the TCP port inside the www
container and does not need a host publication. Add requires mkcert.

EXAMPLES
  laradev domain list
  laradev domain add myapp.test
  laradev domain add ws.myapp.test --port 8080
  laradev domain remove ws.myapp.test
`

const domainListHelp = `USAGE
  laradev domain list

List configured domains, their www target port, certificate path, and backend state.
This command does not change configuration or Docker state.
`
const domainAddHelp = `USAGE
  laradev domain add <hostname> [--port <www-port>]

Validate and add a lowercase hostname route. The default target port is 80.
Certificates are generated or refreshed with mkcert before .laradev.yml is changed.
Print the required 127.0.0.1 host mapping after a successful add.
`
const domainRemoveHelp = `USAGE
  laradev domain remove <hostname>

Remove exactly one normalized hostname from the current project's configuration.
Cached certificate files are retained. Removing an unknown domain is an error.
`
