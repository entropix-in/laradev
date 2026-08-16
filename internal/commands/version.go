package commands

import (
	"fmt"
	"io"
	"runtime"

	"github.com/entropix-in/laradev/internal/version"
)

func printVersion(w io.Writer) error {
	_, err := fmt.Fprintf(w, "laradev %s %s/%s\n", version.Value, runtime.GOOS, runtime.GOARCH)
	return err
}
