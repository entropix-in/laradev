package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type Reader struct {
	In      io.Reader
	Out     io.Writer
	Err     io.Writer
	Scanner *bufio.Scanner
}

func New(in io.Reader, out, err io.Writer) *Reader {
	return &Reader{In: in, Out: out, Err: err, Scanner: bufio.NewScanner(in)}
}
func (r *Reader) ask(label, def string, hidden bool) (string, error) {
	if def != "" {
		fmt.Fprintf(r.Out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(r.Out, "%s: ", label)
	}
	if hidden {
		if f, ok := r.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(r.Out)
			if err != nil {
				return "", err
			}
			v := strings.TrimSpace(string(b))
			if v == "" {
				v = def
			}
			return v, nil
		}
	}
	if !r.Scanner.Scan() {
		return "", io.EOF
	}
	v := strings.TrimSpace(r.Scanner.Text())
	if v == "" {
		v = def
	}
	return v, nil
}
func (r *Reader) String(label, def string) (string, error)   { return r.ask(label, def, false) }
func (r *Reader) Password(label, def string) (string, error) { return r.ask(label, def, true) }
func (r *Reader) Choice(label string, choices []string, def string) (string, error) {
	for {
		v, err := r.ask(label+" ("+strings.Join(choices, "/")+")", def, false)
		if err != nil {
			return "", err
		}
		for _, c := range choices {
			if strings.EqualFold(v, c) {
				return strings.ToLower(v), nil
			}
		}
		fmt.Fprintf(r.Err, "invalid choice %q\n", v)
	}
}
