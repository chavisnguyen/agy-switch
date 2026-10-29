package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// cmdRun runs agy as a child process and monitors for rate limit errors in real-time.
// If a rate-limit occurs, it automatically switches profiles and auto-resumes the conversation.
func cmdRun(customArgs []string) {
	fmt.Println(colorize("🚀 agy-switch runner: Launching agy with Auto-Failover enabled...", ColorBold+ColorCyan))

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		rateLimitDetected := false
		userAborted := false
		var mu sync.Mutex

		// Prepare command arguments
		cmdArgs := make([]string, len(customArgs))
		copy(cmdArgs, customArgs)

		if attempt > 1 {
			// On retry after rate-limit, auto-resume conversation if not already specified
			hasContinue := false
			hasConversation := false
			for _, a := range cmdArgs {
				if a == "--continue" || a == "-c" {
					hasContinue = true
				}
				if a == "--conversation" || strings.HasPrefix(a, "--conversation=") {
					hasConversation = true
				}
			}
			if !hasContinue && !hasConversation {
				cmdArgs = append([]string{"--continue"}, cmdArgs...)
			}
		}

		cmd := exec.Command("agy", cmdArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Channel to stop background log monitor
		stopMonitor := make(chan struct{})

		// Start background log monitor for this agy run
		go func() {
			var currentLogFile *os.File
			var currentPath string
			var reader *bufio.Reader
			var partialLine strings.Builder

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

				resolved := getActiveLogFilePath()

				// If log path changed (agy rotated to a new session log), open the new file
				if resolved != currentPath || currentLogFile == nil {
					if currentLogFile != nil {
						_ = currentLogFile.Close()
					}
					f, err := os.Open(resolved)
					if err != nil {
						time.Sleep(200 * time.Millisecond)
						continue
					}
					currentLogFile = f
					currentPath = resolved
					partialLine.Reset()

					// If this is the newly created log for this session, read from beginning
					// Otherwise seek to end of existing file
					if attempt == 1 && reader == nil {
						_, _ = currentLogFile.Seek(0, io.SeekEnd)
					} else {
						_, _ = currentLogFile.Seek(0, io.SeekStart)
					}
					reader = bufio.NewReader(currentLogFile)
				}

				if reader == nil {
					time.Sleep(200 * time.Millisecond)
					continue
				}

				chunk, err := reader.ReadString('\n')
				if len(chunk) > 0 {
					partialLine.WriteString(chunk)
				}
				if err != nil {
					if err == io.EOF {
						time.Sleep(200 * time.Millisecond)
						continue
					}
					time.Sleep(300 * time.Millisecond)
					continue
				}

				line := partialLine.String()
				partialLine.Reset()

				if rateLimitRegex.MatchString(line) {
					mu.Lock()
					firstTrigger := !rateLimitDetected
					if firstTrigger {
						rateLimitDetected = true
					}
					mu.Unlock()

					if firstTrigger {
						handleRateLimitTrigger(line)

						// Give agy a moment to finish printing to screen, then terminate it
						// so cmd.Run() unblocks and runner can auto-resume in next turn
						go func() {
							time.Sleep(600 * time.Millisecond)
							if cmd.Process != nil {
								_ = cmd.Process.Signal(syscall.SIGTERM)
								time.Sleep(1200 * time.Millisecond)
								if cmd.Process != nil {
									_ = cmd.Process.Kill()
								}
							}
						}()
					}
				}
			}
		}()

		// Handle OS signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			for sig := range sigChan {
				mu.Lock()
				userAborted = true
				mu.Unlock()
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
		aborted := userAborted
		mu.Unlock()

		if aborted && !hitRateLimit {
			// User intentionally cancelled via Ctrl+C and no rate limit was hit
			return
		}

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
