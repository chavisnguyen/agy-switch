package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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

// currentActiveProfileName returns the profile whose credentials are currently active
// machine-wide ("" if none can be determined).
func currentActiveProfileName() string {
	profiles, _ := getAllProfiles()
	for _, p := range profiles {
		if p.IsActive {
			return p.ProfileName
		}
	}
	return ""
}

// profileInCooldown reports whether name has an unexpired cooldown entry.
func profileInCooldown(name string) bool {
	exp, ok := loadCooldowns()[name]
	return ok && exp > time.Now().Unix()
}

// detectFallbackProfile guesses the exhausted account when the runner could not pin one at
// launch: the active profile, else the token's email, else an email found in the log line.
func detectFallbackProfile(sourceLine string) string {
	paths := getPaths()
	if name := currentActiveProfileName(); name != "" {
		return name
	}
	if info, _ := readTokenInfo(paths.TokenPath); info != nil && info.Email != "" && info.Email != "Unknown email" {
		return info.Email
	}
	if raw, err := os.ReadFile(paths.TokenPath); err == nil {
		var tf OAuthTokenFile
		if json.Unmarshal(raw, &tf) == nil && tf.IDTokenEmail != "" {
			return tf.IDTokenEmail
		}
	}
	emailRegex := regexp.MustCompile(`email=([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`)
	if m := emailRegex.FindStringSubmatch(sourceLine); len(m) > 1 {
		return m[1]
	}
	return ""
}

// handleRateLimitTrigger is called by a runner after its agy session hit a rate limit.
//
// sessionProfile is the account that session was launched with. It is what actually ran out
// of quota; the machine-wide active account may already be a different one (another window
// switched, or this window was started on an older account), so it must not be used to
// decide which account to cool down.
//
// The whole decision runs under a cross-process lock so concurrent windows cannot
// double-switch (each cooling down and abandoning the account the other just chose).
func handleRateLimitTrigger(sourceLine, sessionProfile string) {
	release := acquireSwitchLock(switchLockTimeout)
	defer release()

	exhausted := sessionProfile
	if exhausted == "" {
		exhausted = detectFallbackProfile(sourceLine)
	}
	label := exhausted
	if label == "" {
		label = "unknown"
	}

	// 1. Mark the account this session was using as exhausted for 30 minutes
	if exhausted != "" {
		setProfileCooldown(exhausted, 30*time.Minute)
	}

	// 2. If the machine was already moved off the exhausted account (by another window) onto a
	//    healthy one, just resume there instead of switching again.
	if active := currentActiveProfileName(); exhausted != "" && active != "" && active != exhausted && !profileInCooldown(active) {
		msg := fmt.Sprintf("Quota limit on '%s'. Account '%s' is already active; resuming on it.", label, active)
		fmt.Print("\r\n")
		fmt.Print(colorize("⚡ "+msg, ColorBold+ColorGreen) + "\r\n")
		fmt.Printf("   Reason log: %s\r\n", colorize(strings.TrimSpace(sourceLine), ColorDim))
		sendMacOSNotification("agys: Quota Auto-Switch", msg)
		return
	}

	// 3. Otherwise switch the machine to the next healthy account
	nextName, err := switchNextProfile("Rate limit detected")
	if err != nil {
		alertMsg := fmt.Sprintf("Quota limit reached on '%s', but could not switch: %v", label, err)
		fmt.Print(colorize("❌ "+alertMsg, ColorBold+ColorRed) + "\r\n")
		sendMacOSNotification("agys: Quota Limit", alertMsg)
		return
	}

	successMsg := fmt.Sprintf("Quota limit on '%s'. Auto-switched to '%s'!", label, nextName)
	fmt.Print("\r\n")
	fmt.Print(colorize("⚡ "+successMsg, ColorBold+ColorGreen) + "\r\n")
	fmt.Printf("   Reason log: %s\r\n", colorize(strings.TrimSpace(sourceLine), ColorDim))
	sendMacOSNotification("agys: Quota Auto-Switch", successMsg)
}
