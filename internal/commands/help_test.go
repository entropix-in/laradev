package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpDispatchDoesNotRequireProjectOrDocker(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "top short", args: []string{"-h"}, want: "laradev - isolated Laravel development environments"},
		{name: "top long", args: []string{"--help"}, want: "PROJECT COMMANDS"},
		{name: "dns", args: []string{"dns", "-h"}, want: "laradev dns start"},
		{name: "domain", args: []string{"domain", "-h"}, want: "laradev domain list"},
		{name: "domain add", args: []string{"domain", "add", "-h"}, want: "laradev domain add <hostname>"},
		{name: "command", args: []string{"command", "--help"}, want: "commands.forward"},
		{name: "command add", args: []string{"command", "add", "--help"}, want: "laradev command add <name>"},
		{name: "new", args: []string{"new", "-h"}, want: "--laravel-version"},
		{name: "install", args: []string{"install", "-h"}, want: "--bin-dir"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := (App{Out: &out}).Run(tt.args); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if !strings.Contains(out.String(), tt.want) {
				t.Fatalf("help output missing %q:\n%s", tt.want, out.String())
			}
		})
	}
}

func TestEmptyArgsPrintsTopHelp(t *testing.T) {
	var out bytes.Buffer
	if err := (App{Out: &out}).Run(nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.HasPrefix(out.String(), "laradev - isolated Laravel development environments") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}
