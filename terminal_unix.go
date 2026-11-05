//go:build darwin || linux

package main

import (
	"os"
	"syscall"
	"unsafe"
)

func getTermios(fd uintptr) (*syscall.Termios, error) {
	var t syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(ioctlGetTermios), uintptr(unsafe.Pointer(&t))); errno != 0 {
		return nil, errno
	}
	return &t, nil
}

func setTermios(fd uintptr, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(ioctlSetTermios), uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}

// isTerminalStdin reports whether stdin is a real terminal. Unlike a char-device check it
// is false for /dev/null, which is also a character device.
func isTerminalStdin() bool {
	_, err := getTermios(os.Stdin.Fd())
	return err == nil
}

// enterRawMode puts the terminal attached to stdin into a mode suitable for single-key menus
// (no line buffering, no echo) and returns a function that restores the previous mode.
//
// It talks to the terminal driver directly. Shelling out to `stty` is fragile: the child only
// affects the terminal if it inherits stdin, and a missing inherit fails silently, leaving the
// terminal echoing "^[[B" for every arrow key.
//
// Reads use VMIN=0/VTIME=1, so Read returns after at most 100ms even with no input. That lets
// the key parser tell a lone Esc apart from the start of an escape sequence.
func enterRawMode() (restore func(), err error) {
	fd := os.Stdin.Fd()
	orig, err := getTermios(fd)
	if err != nil {
		return nil, err
	}
	raw := *orig
	raw.Lflag &^= syscall.ICANON | syscall.ECHO
	raw.Cc[syscall.VMIN] = 0
	raw.Cc[syscall.VTIME] = 1
	if err := setTermios(fd, &raw); err != nil {
		return nil, err
	}
	return func() { _ = setTermios(fd, orig) }, nil
}
