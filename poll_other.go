//go:build !darwin && !linux

package main

import (
	"errors"
	"time"
)

// No raw-mode menu on other platforms: callers fall back to the plain text prompt.
const menuSupported = false

func waitInput(fd int, timeout time.Duration) (bool, error) {
	return false, errors.New("interactive menu not supported on this platform")
}
