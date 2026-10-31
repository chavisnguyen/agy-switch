package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var rateLimitRegex = regexp.MustCompile(`(?i)(agent executor error.*(RESOURCE_EXHAUSTED|\b429\b)|streamGenerateContent.*(RESOURCE_EXHAUSTED|\b429\b)|RESOURCE_EXHAUSTED.*(code 429)|Individual quota reached|quota exceeded|exceeded your current quota|exceeded max quota|rate limit reached|rate limit exceeded|Verify your account to continue)`)

func sendMacOSNotification(title, message string) {
	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "Submarine"`,
		strings.ReplaceAll(message, `"`, `\"`),
		strings.ReplaceAll(title, `"`, `\"`),
	)
	_ = exec.Command("osascript", "-e", script).Run()
}

func getActiveLogFilePath() string {
	cliHome, _, _, _, _, _, _ := getPaths()
	symlinkPath := filepath.Join(cliHome, "cli.log")

	// Resolve symlink
	realPath, err := filepath.EvalSymlinks(symlinkPath)
	if err == nil && realPath != "" {
		return realPath
	}
	return symlinkPath
}

func handleRateLimitTrigger(sourceLine string) {
	_, tokenPath, profilesDir, _, _, _, _ := getPaths()

	// Cross-process debounce (prevents double-switching if both runner and daemon are active)
	lastSwitchFile := filepath.Join(profilesDir, ".last_switch")
	if data, err := os.ReadFile(lastSwitchFile); err == nil {
		if ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			if time.Now().Unix()-ts < 4 {
				return
			}
		}
	}
	_ = os.WriteFile(lastSwitchFile, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0600)

	activeInfo, _ := readTokenInfo(tokenPath)

	currentProfile := "unknown"
	if activeInfo != nil {
		profiles, _ := getAllProfiles()
		for _, p := range profiles {
			if p.IsActive {
				currentProfile = p.ProfileName
				break
			}
		}
		if (currentProfile == "unknown" || currentProfile == "") && activeInfo.Email != "" && activeInfo.Email != "Unknown email" {
			currentProfile = activeInfo.Email
		}
	}
	if currentProfile == "unknown" || currentProfile == "" || currentProfile == "Unknown email" {
		if raw, err := os.ReadFile(tokenPath); err == nil {
			var tf OAuthTokenFile
			if json.Unmarshal(raw, &tf) == nil && tf.IDTokenEmail != "" {
				currentProfile = tf.IDTokenEmail
			}
		}
		if currentProfile == "unknown" || currentProfile == "" || currentProfile == "Unknown email" {
			emailRegex := regexp.MustCompile(`email=([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`)
			if m := emailRegex.FindStringSubmatch(sourceLine); len(m) > 1 {
				currentProfile = m[1]
			}
		}
	}

	// 1. Mark current profile in cooldown for 30 minutes
	if currentProfile != "unknown" {
		setProfileCooldown(currentProfile, 30*time.Minute)
	}

	// 2. Switch to next profile
	nextName, err := switchNextProfile("Rate limit detected")
	if err != nil {
		alertMsg := fmt.Sprintf("Quota limit reached on '%s', but could not switch: %v", currentProfile, err)
		fmt.Println(colorize("❌ "+alertMsg, ColorBold+ColorRed))
		sendMacOSNotification("agy-switch: Quota Limit", alertMsg)
		return
	}

	successMsg := fmt.Sprintf("Quota limit on '%s'. Auto-switched to '%s'!", currentProfile, nextName)
	fmt.Println()
	fmt.Println(colorize("⚡ "+successMsg, ColorBold+ColorGreen))
	fmt.Printf("   Reason log: %s\n", colorize(strings.TrimSpace(sourceLine), ColorDim))

	// 3. Send macOS Notification
	sendMacOSNotification("agy-switch: Quota Auto-Switch", successMsg)
}

func watchLoop(stopChan <-chan struct{}) {
	cliHome, _, _, _, _, _, _ := getPaths()
	symlinkPath := filepath.Join(cliHome, "cli.log")

	fmt.Println(colorize("👀 agy-switch watch is ACTIVE", ColorBold+ColorCyan))
	fmt.Printf("   Monitoring: %s\n", colorize(symlinkPath, ColorGreen))
	fmt.Println(colorize("   Auto-switches accounts on HTTP 429 / RESOURCE_EXHAUSTED errors.", ColorDim))
	fmt.Println(colorize("   Press Ctrl+C to stop.\n", ColorDim))

	var currentLogFile *os.File
	var currentPath string
	var reader *bufio.Reader
	var lastTrigger time.Time
	var partialLine strings.Builder

	cleanup := func() {
		if currentLogFile != nil {
			_ = currentLogFile.Close()
		}
	}
	defer cleanup()

	for {
		select {
		case <-stopChan:
			fmt.Println("\nStopping watcher...")
			return
		default:
		}

		resolved := getActiveLogFilePath()

		// If log target changed (session rotated), open new target
		if resolved != currentPath || currentLogFile == nil {
			if currentLogFile != nil {
				_ = currentLogFile.Close()
			}
			f, err := os.Open(resolved)
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			}
			currentLogFile = f
			currentPath = resolved
			// Seek to end so we don't replay historical rate limits
			_, _ = currentLogFile.Seek(0, io.SeekEnd)
			reader = bufio.NewReader(currentLogFile)
			partialLine.Reset()
		}

		chunk, err := reader.ReadString('\n')
		if len(chunk) > 0 {
			partialLine.WriteString(chunk)
		}
		if err != nil {
			if err == io.EOF {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		line := partialLine.String()
		partialLine.Reset()

		// Check if line matches rate limit error
		if rateLimitRegex.MatchString(line) {
			// Debounce: prevent duplicate triggers within 5 seconds
			if time.Since(lastTrigger) > 5*time.Second {
				lastTrigger = time.Now()
				handleRateLimitTrigger(line)
			}
		}
	}
}

// Daemon Control: start / stop / status / foreground
func cmdWatch(args []string) {
	_, _, _, _, _, _, pidFile := getPaths()

	action := "foreground"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}

	switch action {
	case "start":
		// Check if already running
		if isDaemonRunning(pidFile) {
			pid, _ := readPID(pidFile)
			fmt.Println(colorize(fmt.Sprintf("ℹ️  Watcher daemon is already running (PID: %d).", pid), ColorYellow))
			return
		}

		// Re-exec self in background with hidden flag
		execPath, err := os.Executable()
		if err != nil {
			fmt.Printf("Error resolving executable: %v\n", err)
			return
		}

		cmd := exec.Command(execPath, "watch", "--daemon-internal")
		devNull, errDev := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if errDev == nil {
			cmd.Stdin = devNull
			cmd.Stdout = devNull
			cmd.Stderr = devNull
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true, // detach from terminal
		}

		if err := cmd.Start(); err != nil {
			fmt.Println(colorize(fmt.Sprintf("❌ Failed to start daemon: %v", err), ColorRed))
			return
		}

		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0600)
		fmt.Println(colorize(fmt.Sprintf("✅ Watcher daemon started in background (PID: %d).", cmd.Process.Pid), ColorBold+ColorGreen))
		fmt.Println("   It will automatically swap tokens and notify you when 429 errors occur.")
		fmt.Println("   Stop anytime with: " + colorize("agy-switch watch stop", ColorCyan))

	case "stop":
		if !isDaemonRunning(pidFile) {
			fmt.Println(colorize("ℹ️  Watcher daemon is not currently running.", ColorYellow))
			_ = os.Remove(pidFile)
			return
		}

		pid, err := readPID(pidFile)
		if err != nil {
			fmt.Println(colorize("❌ Could not read PID file.", ColorRed))
			return
		}

		proc, err := os.FindProcess(pid)
		if err == nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
		_ = os.Remove(pidFile)
		fmt.Println(colorize(fmt.Sprintf("🛑 Watcher daemon stopped (PID: %d).", pid), ColorBold+ColorGreen))

	case "status":
		if isDaemonRunning(pidFile) {
			pid, _ := readPID(pidFile)
			fmt.Println(colorize(fmt.Sprintf("● Watcher daemon is RUNNING (PID: %d)", pid), ColorBold+ColorGreen))
			fmt.Printf("   Active log: %s\n", getActiveLogFilePath())
		} else {
			fmt.Println(colorize("○ Watcher daemon is STOPPED", ColorDim))
			fmt.Println("   Start it with: " + colorize("agy-switch watch start", ColorCyan))
		}

	case "--daemon-internal":
		// Internal daemon loop without stdout terminal
		stopChan := make(chan struct{})
		watchLoop(stopChan)

	default:
		// Foreground interactive watcher
		stopChan := make(chan struct{})
		watchLoop(stopChan)
	}
}

func readPID(pidFile string) (int, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func isDaemonRunning(pidFile string) bool {
	pid, err := readPID(pidFile)
	if err != nil || pid <= 0 {
		return false
	}

	// Signal 0 checks process existence without killing it
	err = syscall.Kill(pid, 0)
	if err != nil {
		return false
	}

	// Double check process name on macOS to avoid recycled PID collisions
	out, errCmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if errCmd == nil {
		comm := strings.TrimSpace(string(out))
		if strings.Contains(comm, "agy-switch") {
			return true
		}
	}
	return false
}
