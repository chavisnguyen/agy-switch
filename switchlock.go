package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// switchLockTimeout bounds how long a runner waits for another runner's account switch.
// A switch can take several seconds (it queries every account's quota), so be generous.
const switchLockTimeout = 30 * time.Second

// acquireSwitchLock takes an exclusive cross-process lock so that only one `agys run`
// window changes the machine-wide active account at a time. It returns a release func.
//
// flock is released by the kernel when the holder dies, so a stale lock cannot wedge the
// others. If the lock still cannot be taken within timeout, we proceed unlocked rather than
// leave the session stuck on an exhausted account.
func acquireSwitchLock(timeout time.Duration) func() {
	if err := ensureDirs(); err != nil {
		return func() {}
	}
	f, err := os.OpenFile(filepath.Join(getPaths().ProfilesDir, ".switch.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return func() {}
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}
		}
		if time.Now().After(deadline) {
			fmt.Print(colorize("⚠️  Another agys window is still switching accounts; continuing without the switch lock.", ColorYellow) + "\r\n")
			_ = f.Close()
			return func() {}
		}
		time.Sleep(100 * time.Millisecond)
	}
}
