package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/entropix-in/laradev/internal/config"
	"github.com/entropix-in/laradev/internal/project"
)

func TestPrintStartupGuideIncludesViteAndDatabaseSetup(t *testing.T) {
	c := project.Context{Config: config.Config{MySQL: config.MySQL{Enabled: true, Database: "appdb", Username: "appuser", Password: "super-secret"}}}
	var out bytes.Buffer
	PrintStartupGuide(c, &out)
	text := out.String()
	for _, want := range []string{"host: '0.0.0.0'", "strictPort: true", "hmr: { host: '127.0.0.1' }", "DB_HOST=mysql", "DB_PORT=3306", "DB_DATABASE=appdb", "DB_USERNAME=appuser", "DB_PASSWORD=<value of mysql.password in .laradev.yml>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("startup guide missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "super-secret") {
		t.Fatalf("startup guide leaked database password:\n%s", text)
	}
}
