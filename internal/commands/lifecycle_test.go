package commands

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/entropix-in/laradev/internal/config"
	"github.com/entropix-in/laradev/internal/project"
)

type lifecycleInspectRunner struct{}

func (lifecycleInspectRunner) Run(_ context.Context, args []string, _ io.Reader, out, _ io.Writer) error {
	if len(args) >= 2 && args[0] == "inspect" && args[len(args)-1] != "{{json .}}" {
		return nil
	}
	if len(args) >= 2 && args[0] == "inspect" {
		_, _ = io.WriteString(out, `{"State":{"Status":"exited"},"Config":{"Image":"test","Env":[],"Labels":{}}}`)
	}
	return nil
}

func TestStatusReportsActualContainerState(t *testing.T) {
	c := project.Context{Config: config.Config{Project: config.Project{ID: "ldev_aaaaaaaaaaaa"}, Runtime: config.Runtime{Image: "runtime"}}, WorktreeID: "wt"}
	var out bytes.Buffer
	if err := (Lifecycle{Runner: lifecycleInspectRunner{}}).Status(context.Background(), &c, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "www\tproject\tlaradev-ldev_aaaaaaaaaaaa-www-wt\texited") {
		t.Fatalf("status did not report exited state:\n%s", out.String())
	}
}

func TestMySQLValuesAreEscaped(t *testing.T) {
	if got := mysqlString(`pa\ss'word`); got != `'pa\\ss\'word'` {
		t.Fatalf("mysqlString() = %q", got)
	}
	if got := mysqlIdentifier("db`name"); got != "`db``name`" {
		t.Fatalf("mysqlIdentifier() = %q", got)
	}
}
