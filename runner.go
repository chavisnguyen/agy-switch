package main

import (
	"bufio"
	"context"
	"encoding/json"
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

var convUUIDRegex = regexp.MustCompile(`(?i)(?:Streaming conversation|Resuming conversation|Created conversation|Starting conversation update stream for|found conversation[:\s]+|conversation(?:_id)?[:\s=]+|cascade_id[:=]|--conversation[= ])\s*([a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})`)

func hasContinueFlag(args []string) bool {
	for _, a := range args {
		if a == "-c" || a == "--continue" {
			return true
		}
	}
	return false
}

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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sqlite3", "-readonly", "-cmd", ".timeout 3000", "-batch", "-noheader", dbPath, query)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getLatestConversationID() string {
	cliHome, _, _, _, _, _, _ := getPaths()

	// 1. Check cache/last_conversations.json for current working directory (most accurate for active session)
	if cwd, err := os.Getwd(); err == nil {
		cachePath := filepath.Join(cliHome, "cache", "last_conversations.json")
		if data, err := os.ReadFile(cachePath); err == nil {
			var m map[string]string
			if json.Unmarshal(data, &m) == nil {
				if id, ok := m[cwd]; ok && id != "" {
					return id
				}
			}
		}
	}

	// 2. Fallback to conversation_summaries.db filtered by current workspace
	dbPath := filepath.Join(cliHome, "conversation_summaries.db")
	if _, err := os.Stat(dbPath); err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		query := fmt.Sprintf("SELECT conversation_id FROM conversation_summaries WHERE workspace_uris LIKE '%%%s%%' AND conversation_id != '' ORDER BY last_modified_time DESC LIMIT 1;", strings.ReplaceAll(cwd, "'", ""))
		cmd := exec.CommandContext(ctx, "sqlite3", "-readonly", "-cmd", ".timeout 3000", "-batch", "-noheader", dbPath, query)
		if out, err := cmd.Output(); err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out))
		}
	}

	// 3. Fallback to global latest conversation
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	cmd2 := exec.CommandContext(ctx2, "sqlite3", "-readonly", "-cmd", ".timeout 3000", "-batch", "-noheader", dbPath, "SELECT conversation_id FROM conversation_summaries WHERE conversation_id != '' ORDER BY last_modified_time DESC LIMIT 1;")
	out, err := cmd2.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func resetTerminal() {
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		cmd := exec.Command("stty", "sane", "opost", "onlcr")
		cmd.Stdin = tty
		cmd.Stdout = tty
		cmd.Stderr = tty
		_ = cmd.Run()
		tty.Close()
	} else {
		cmd := exec.Command("stty", "sane")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
	fmt.Print("\033[?25h\r") // Show cursor and return carriage to column 0
}

func printContinueHint(convID string) {
	resetTerminal()
	if convID == "" {
		convID = getLatestConversationID()
	}
	if convID == "" {
		return
	}

	fmt.Print("\r\n")
	fmt.Print(colorize("To continue this conversation with auto-failover, run:", ColorBold) + "\r\n")
	fmt.Printf("  %s\r\n\r\n", colorize("agys run --conversation="+convID, ColorBold+ColorCyan))
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
	fmt.Print(colorize("🚀 agys runner: Launching agy with Auto-Failover enabled...", ColorBold+ColorCyan) + "\r\n")

	activeConvID := extractConversationID(customArgs)
	if activeConvID == "" && hasContinueFlag(customArgs) {
		activeConvID = getLatestConversationID()
	}

	profiles, _ := getAllProfiles()
	maxRetries := len(profiles)
	if maxRetries < 3 {
		maxRetries = 3
	}
	for attempt := 1; attempt <= maxRetries; attempt++ {
		rateLimitDetected := false
		userAborted := false
		lastErrorLine := ""
		var mu sync.Mutex

		// Prepare command arguments
		cmdArgs := make([]string, len(customArgs))
		copy(cmdArgs, customArgs)

		if attempt > 1 {
			// On retry after rate-limit, prioritize resuming the EXACT conversation that hit the limit
			mu.Lock()
			currentID := activeConvID
			mu.Unlock()

			if currentID != "" {
				var filtered []string
				for i := 0; i < len(cmdArgs); i++ {
					a := cmdArgs[i]
					if a == "--continue" || a == "-c" {
						continue
					}
					if strings.HasPrefix(a, "--conversation=") {
						continue
					}
					if a == "--conversation" {
						i++ // skip arg value
						continue
					}
					filtered = append(filtered, a)
				}
				cmdArgs = append(filtered, "--conversation="+currentID)
			}
		}

		mu.Lock()
		targetConvID := activeConvID
		mu.Unlock()

		if targetConvID != "" {
			if initTitle := getConversationTitle(targetConvID); initTitle != "" {
				setTerminalTitle(initTitle)
			}
		}

		preLaunchLogFile := getActiveLogFilePath()

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
					cID := activeConvID
					mu.Unlock()

					if cID == "" {
						continue
					}

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

					// If this is a pre-existing log file from previous session/attempt, seek to end
					// so we only monitor fresh lines and never re-read stale errors.
					// If agy rotated to a new session log file for this run, read from the beginning.
					if resolved == preLaunchLogFile {
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

				// Detect conversation UUID if not yet set (ignore user input echoes)
				if !strings.Contains(line, "HandleUserInput") && !strings.Contains(line, "input_loop.go") {
					mu.Lock()
					if activeConvID == "" {
						if m := convUUIDRegex.FindStringSubmatch(line); len(m) > 1 {
							activeConvID = m[1]
						}
					}
					mu.Unlock()
				}

				if isRateLimitError(line) {
					mu.Lock()
					firstTrigger := !rateLimitDetected
					if firstTrigger {
						rateLimitDetected = true
						lastErrorLine = line
					}
					mu.Unlock()

					if firstTrigger {
						// Give agy a moment to finish printing to screen, then terminate it
						// so cmd.Run() unblocks and runner can auto-resume in next turn
						go func() {
							time.Sleep(500 * time.Millisecond)
							if cmd.Process != nil {
								_ = cmd.Process.Signal(syscall.SIGTERM)
								time.Sleep(1000 * time.Millisecond)
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
		time.Sleep(200 * time.Millisecond)
		close(stopMonitor)
		signal.Stop(sigChan)
		close(sigChan)

		// Always reset terminal immediately after agy exits/terminates
		resetTerminal()

		mu.Lock()
		hitRateLimit := rateLimitDetected
		aborted := userAborted
		cID := activeConvID
		if cID == "" && !hitRateLimit {
			cID = getLatestConversationID()
		}
		mu.Unlock()

		if aborted {
			// User intentionally cancelled via Ctrl+C
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
		resetTerminal()
		fmt.Print("\r\n")
		fmt.Print(colorize("════════════════════════════════════════════════════════════", ColorYellow) + "\r\n")
		fmt.Print(colorize("⚡ Rate limit encountered during this turn!", ColorBold+ColorYellow) + "\r\n")
		fmt.Print(colorize("🔄 Automatically switching to the next account and resuming...", ColorBold+ColorGreen) + "\r\n")
		fmt.Print(colorize("════════════════════════════════════════════════════════════", ColorYellow) + "\r\n\r\n")

		// Switch profile now that agy child process has completely terminated
		mu.Lock()
		errLine := lastErrorLine
		mu.Unlock()
		handleRateLimitTrigger(errLine)

		time.Sleep(1 * time.Second)
	}

	resetTerminal()
	fmt.Print(colorize("❌ Max retry attempts reached or all accounts exhausted.", ColorBold+ColorRed) + "\r\n")
}
