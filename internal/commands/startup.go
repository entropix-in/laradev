package commands

import (
	"fmt"
	"io"

	"github.com/rohan2388/laradev/internal/project"
)

// PrintStartupGuide prints the container-side setup required after a successful up.
// Password values are intentionally not echoed; the configured value remains in .laradev.yml.
func PrintStartupGuide(c project.Context, w io.Writer) {
	fmt.Fprintln(w, "\nlaradev is ready")
	fmt.Fprintln(w, "────────────────────────────────────────────────────────")
	fmt.Fprintln(w, "Vite checklist (required before pnpm run dev):")
	fmt.Fprintln(w, "  server: {")
	fmt.Fprintln(w, "      host: '0.0.0.0',")
	fmt.Fprintln(w, "      strictPort: true,")
	fmt.Fprintln(w, "      hmr: { host: '127.0.0.1' },")
	fmt.Fprintln(w, "  }")
	fmt.Fprintln(w, "  Keep Vite on port 5173; do not let it fall back to 5174.")
	if c.Config.MySQL.Enabled {
		fmt.Fprintln(w, "\nMySQL environment inside the www container:")
		fmt.Fprintln(w, "  DB_CONNECTION=mysql")
		fmt.Fprintln(w, "  DB_HOST=mysql")
		fmt.Fprintln(w, "  DB_PORT=3306")
		fmt.Fprintf(w, "  DB_DATABASE=%s\n", c.Config.MySQL.Database)
		fmt.Fprintf(w, "  DB_USERNAME=%s\n", c.Config.MySQL.Username)
		fmt.Fprintln(w, "  DB_PASSWORD=<value of mysql.password in .laradev.yml>")
		fmt.Fprintln(w, "  Do not use 127.0.0.1 for DB_HOST from inside www.")
	}
	fmt.Fprintln(w, "\nNext steps:")
	fmt.Fprintln(w, "  pnpm run dev")
	fmt.Fprintln(w, "  Open the configured HTTPS domain in your browser.")
}
