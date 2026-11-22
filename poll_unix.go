//go:build darwin || linux

package main

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

const menuSupported = true

// waitInput reports whether fd has input to read within timeout (negative = wait forever).
func waitInput(fd int, timeout time.Duration) (bool, error) {
	ms := -1
	if timeout >= 0 {
		ms = int(timeout / time.Millisecond)
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, ms)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return n > 0, err
	}
}
