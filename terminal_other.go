//go:build !darwin && !linux

package main

import (
	"errors"
	"os"
)

func isTerminalStdin() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// enterRawMode is unsupported on this platform; callers fall back to a plain text prompt.
func enterRawMode() (func(), error) {
	return nil, errors.New("raw terminal mode is not supported on this platform")
}
