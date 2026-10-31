package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var convUUIDRegex = regexp.MustCompile(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`)

func extractConversationID(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--conversation=") {
			return strings.TrimPrefix(a, "--conversation=")
		}
	}
	for i, a := range args {
		if a == "--conversation" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func getConversationTitle(convID string) string {
	cliHome, _, _, _, _, _, _ := getPaths()
	dbPath := filepath.Join(cliHome, "conversation_summaries.db")
	if _, err := os.Stat(dbPath); err != nil {
		return ""
	}

	var query string
	if convID != "" {
		cleanID := strings.ReplaceAll(convID, "'", "")
		query = fmt.Sprintf("SELECT title FROM conversation_summaries WHERE conversation_id = '%s' AND title != '' LIMIT 1;", cleanID)
	} else {
		query = "SELECT title FROM conversation_summaries WHERE title != '' ORDER BY last_modified_time DESC LIMIT 1;"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sqlite3", "-batch", "-noheader", dbPath, query)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getLatestConversationID() string {
	cliHome, _, _, _, _, _, _ := getPaths()
	dbPath := filepath.Join(cliHome, "conversation_summaries.db")
	if _, err := os.Stat(dbPath); err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sqlite3", "-batch", "-noheader", dbPath, "SELECT conversation_id FROM conversation_summaries WHERE conversation_id != '' ORDER BY last_modified_time DESC LIMIT 1;")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func printContinueHint(convID string) {
	if convID == "" {
		convID = getLatestConversationID()
	}
	if convID == "" {
		return
	}

	fmt.Println()
	fmt.Println(colorize("To continue this conversation with auto-failover, run:", ColorBold))
	fmt.Printf("  %s\n\n", colorize("agys run --conversation="+convID, ColorBold+ColorCyan))
}

func setTerminalTitle(title string) {
	if title == "" {
		return
	}

	// 1. Standard ANSI OSC 2 escape sequence (supported by Ghostty, iTerm2, Kitty, Terminal.app)
	fmt.Fprintf(os.Stdout, "\033]2;%s\007", title)

	// 2. If running inside CMUX, also invoke cmux rename-tab for UI sync
	if os.Getenv("CMUX_SOCKET_PATH") != "" || os.Getenv("CMUX_TAB_ID") != "" {
		cmuxBin := os.Getenv("CMUX_BUNDLED_CLI_PATH")
		if cmuxBin == "" {
			cmuxBin = "cmux"
		}
		_ = exec.Command(cmuxBin, "rename-tab", title).Run()
	}
}

// cmdRun runs agy as a child process and monitors for rate limit errors in real-time.
// If a rate-limit occurs, it automatically switches profiles and auto-resumes the conversation.
func cmdRun(customArgs []string) {
	fmt.Println(colorize("🚀 agys runner: Launching agy with Auto-Failover enabled...", ColorBold+ColorCyan))

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

		targetConvID := extractConversationID(cmdArgs)
		if targetConvID != "" {
			if initTitle := getConversationTitle(targetConvID); initTitle != "" {
				setTerminalTitle(initTitle)
			}
		}

		cmd := exec.Command("agy", cmdArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Channel to stop background log & title monitors
		stopMonitor := make(chan struct{})

		// Start background title syncer to continuously sync tab title with conversation title
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			lastTitle := ""

			for {
				select {
				case <-stopMonitor:
					return
				case <-ticker.C:
					mu.Lock()
					cID := targetConvID
					mu.Unlock()

					title := getConversationTitle(cID)
					if title != "" && title != lastTitle {
						lastTitle = title
						setTerminalTitle(title)
					}
				}
			}
		}()

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

				// Detect conversation UUID if not yet set
				mu.Lock()
				if targetConvID == "" {
					if match := convUUIDRegex.FindString(line); match != "" {
						targetConvID = match
					}
				}
				mu.Unlock()

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
		cID := targetConvID
		mu.Unlock()

		if aborted && !hitRateLimit {
			// User intentionally cancelled via Ctrl+C and no rate limit was hit
			printContinueHint(cID)
			return
		}

		if !hitRateLimit {
			// Normal exit (no rate limit hit)
			printContinueHint(cID)
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
