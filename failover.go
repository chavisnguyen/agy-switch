package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var rateLimitRegex = regexp.MustCompile(`(?i)(agent executor error.*(RESOURCE_EXHAUSTED|\b429\b)|streamGenerateContent.*(RESOURCE_EXHAUSTED|\b429\b)|RESOURCE_EXHAUSTED.*(code 429)|Individual quota reached|quota exceeded|exceeded your current quota|exceeded max quota|rate limit reached|rate limit exceeded|Verify your account to continue)`)

func isRateLimitError(line string) bool {
	// 1. Skip user input echoes, prompt logging, message forwarding, and internal metadata
	if strings.Contains(line, "HandleUserInput") ||
		strings.Contains(line, "input_loop.go") ||
		strings.Contains(line, "Forwarding user message") ||
		strings.Contains(line, "Sending user message") ||
		strings.Contains(line, "SEND_USER_CASCADE_MESSAGE") ||
		strings.Contains(line, "reconcile_summaries.go") ||
		strings.Contains(line, "store_client.go") {
		return false
	}

	// 2. Must be an actual error or warning from backend / agent executor / auth
	if !strings.HasPrefix(line, "E") && !strings.HasPrefix(line, "W") &&
		!strings.Contains(line, "errorreport.go") &&
		!strings.Contains(line, "agent executor error") &&
		!strings.Contains(line, "server_oauth.go") {
		return false
	}

	// 3. Match rate limit regex
	return rateLimitRegex.MatchString(line)
}

func sendMacOSNotification(title, message string) {
	if runtime.GOOS != "darwin" {
		return
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "Submarine"`,
		strings.ReplaceAll(message, `"`, `\"`),
		strings.ReplaceAll(title, `"`, `\"`),
	)
	_ = exec.Command("osascript", "-e", script).Run()
}

func getActiveLogFilePath() string {
	paths := getPaths()
	symlinkPath := filepath.Join(paths.CLIHome, "cli.log")

	// Resolve symlink
	realPath, err := filepath.EvalSymlinks(symlinkPath)
	if err == nil && realPath != "" {
		return realPath
	}
	return symlinkPath
}

func handleRateLimitTrigger(sourceLine string) {
	paths := getPaths()

	activeInfo, _ := readTokenInfo(paths.TokenPath)

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
		if raw, err := os.ReadFile(paths.TokenPath); err == nil {
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

	// Cross-process debounce: prevent duplicate triggers for the SAME profile within 3 seconds
	lastSwitchFile := filepath.Join(paths.ProfilesDir, ".last_switch")
	if data, err := os.ReadFile(lastSwitchFile); err == nil {
		content := strings.TrimSpace(string(data))
		parts := strings.Split(content, ":")
		if len(parts) == 2 {
			lastProf := parts[0]
			if ts, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				if lastProf == currentProfile && time.Now().Unix()-ts < 3 {
					return
				}
			}
		} else if ts, err := strconv.ParseInt(content, 10, 64); err == nil {
			if time.Now().Unix()-ts < 3 {
				return
			}
		}
	}
	_ = os.WriteFile(lastSwitchFile, []byte(fmt.Sprintf("%s:%d", currentProfile, time.Now().Unix())), 0600)

	// 1. Mark current profile in cooldown for 30 minutes
	if currentProfile != "unknown" {
		setProfileCooldown(currentProfile, 30*time.Minute)
	}

	// 2. Switch to next profile
	nextName, err := switchNextProfile("Rate limit detected")
	if err != nil {
		alertMsg := fmt.Sprintf("Quota limit reached on '%s', but could not switch: %v", currentProfile, err)
		fmt.Print(colorize("❌ "+alertMsg, ColorBold+ColorRed) + "\r\n")
		sendMacOSNotification("agys: Quota Limit", alertMsg)
		return
	}

	successMsg := fmt.Sprintf("Quota limit on '%s'. Auto-switched to '%s'!", currentProfile, nextName)
	fmt.Print("\r\n")
	fmt.Print(colorize("⚡ "+successMsg, ColorBold+ColorGreen) + "\r\n")
	fmt.Printf("   Reason log: %s\r\n", colorize(strings.TrimSpace(sourceLine), ColorDim))

	// 3. Send macOS Notification
	sendMacOSNotification("agys: Quota Auto-Switch", successMsg)
}
