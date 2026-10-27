package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// cmdRun runs agy as a child process and monitors for rate limit errors in real-time.
// If a rate-limit occurs, it automatically switches profiles and offers / executes auto-resume.
func cmdRun(customArgs []string) {
	fmt.Println(colorize("🚀 agy-switch runner: Launching agy with Auto-Failover enabled...", ColorBold+ColorCyan))

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		rateLimitDetected := false
		var mu sync.Mutex

		// Stop channel for the background log monitor
		stopMonitor := make(chan struct{})

		// Start background log monitor for this agy run
		go func() {
			var currentLogFile *os.File
			var reader *bufio.Reader

			resolved := getActiveLogFilePath()
			f, err := os.Open(resolved)
			if err == nil {
				currentLogFile = f
				// Seek to end of log
				_, _ = currentLogFile.Seek(0, io.SeekEnd)
				reader = bufio.NewReader(currentLogFile)
			}
			defer func() {
				if currentLogFile != nil {
					_ = currentLogFile.Close()
				}
			}()

			for {
				select {
				case <-stopMonitor:
					return
				default:
				}

				if reader == nil {
					time.Sleep(300 * time.Millisecond)
					continue
				}

				line, err := reader.ReadString('\n')
				if err != nil {
					time.Sleep(300 * time.Millisecond)
					continue
				}

				if rateLimitRegex.MatchString(line) {
					mu.Lock()
					if !rateLimitDetected {
						rateLimitDetected = true
						handleRateLimitTrigger(line)
					}
					mu.Unlock()
				}
			}
		}()

		// Prepare command arguments
		cmdArgs := customArgs
		if attempt > 1 {
			// On retry after rate-limit, auto-resume conversation if not already specified
			hasContinue := false
			for _, a := range cmdArgs {
				if a == "--continue" || a == "-c" {
					hasContinue = true
					break
				}
			}
			if !hasContinue {
				cmdArgs = append([]string{"--continue"}, cmdArgs...)
			}
		}

		cmd := exec.Command("agy", cmdArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Handle OS signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			for sig := range sigChan {
				if cmd.Process != nil {
					_ = cmd.Process.Signal(sig)
				}
			}
		}()

		runErr := cmd.Run()
		close(stopMonitor)
		signal.Stop(sigChan)

		mu.Lock()
		hitRateLimit := rateLimitDetected
		mu.Unlock()

		if !hitRateLimit {
			// Normal exit (no rate limit hit)
			if runErr != nil {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					os.Exit(exitErr.ExitCode())
				}
			}
			return
		}

		// Rate limit was detected during run
		fmt.Println()
		fmt.Println(colorize("════════════════════════════════════════════════════════════", ColorYellow))
		fmt.Println(colorize("⚡ Rate limit encountered during this turn!", ColorBold+ColorYellow))
		fmt.Println(colorize("🔄 Automatically switching to the next account and resuming...", ColorBold+ColorGreen))
		fmt.Println(colorize("════════════════════════════════════════════════════════════", ColorYellow))
		fmt.Println()
		time.Sleep(1 * time.Second)
	}

	fmt.Println(colorize("❌ Max retry attempts reached or all accounts exhausted.", ColorBold+ColorRed))
}
