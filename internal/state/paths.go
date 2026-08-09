package state

import (
	"errors"
	"os"
	"path/filepath"
)

func Dir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	d := filepath.Join(base, "laradev")
	if st, err := os.Lstat(d); err == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", errors.New("laradev state path is not a directory or is a symlink")
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(d, 0700); err != nil {
			return "", err
		}
	} else {
		return "", err
	}
	if err := os.Chmod(d, 0700); err != nil {
		return "", err
	}
	return d, nil
}
func CaddyDir() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(d, "caddy")
	if err := os.MkdirAll(p, 0700); err != nil {
		return "", err
	}
	return p, nil
}
