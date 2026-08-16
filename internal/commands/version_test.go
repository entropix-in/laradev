package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/entropix-in/laradev/internal/version"
)

func TestPrintVersion(t *testing.T) {
	oldVersion := version.Value
	version.Value = "v1.2.3"
	t.Cleanup(func() { version.Value = oldVersion })

	var out bytes.Buffer
	if err := printVersion(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "laradev v1.2.3 ") {
		t.Fatalf("unexpected version output: %q", out.String())
	}
}
