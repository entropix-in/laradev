package main

import (
	"fmt"
	"os"

	"github.com/rohan2388/laradev/internal/commands"
)

func main() {
	app := commands.App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	args := os.Args[1:]
	name := filepathBase(os.Args[0])
	var err error
	if name != "laradev" && name != "laradev.exe" {
		err = commands.Dynamic(name, args, app)
	} else {
		err = app.Run(args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
