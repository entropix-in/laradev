package docker

import (
	"context"
	"io"
	"os/exec"
)

type CommandRunner interface {
	Run(context.Context, []string, io.Reader, io.Writer, io.Writer) error
}
type Runner struct{ Binary string }

func (r Runner) Run(ctx context.Context, argv []string, in io.Reader, out, errOut io.Writer) error {
	bin := r.Binary
	if bin == "" {
		bin = "docker"
	}
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errOut
	return cmd.Run()
}
func Default() Runner { return Runner{Binary: "docker"} }
