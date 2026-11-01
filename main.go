package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ANSI Colors
const (
	ColorReset  = "\033[0m"
	ColorBold   = "\033[1m"
	ColorDim    = "\033[2m"
	ColorGreen  = "\033[92m"
	ColorCyan   = "\033[96m"
	ColorYellow = "\033[93m"
	ColorRed    = "\033[91m"
)

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

func colorize(text, color string) string {
	if !useColor() {
		return text
	}
	return color + text + ColorReset
}

func visualLen(s string) int {
	clean := ansiRegex.ReplaceAllString(s, "")
	return len([]rune(clean))
}

func padRight(s string, width int) string {
	vl := visualLen(s)
	if vl >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vl)
}

// ==================== CLI COMMANDS ====================

func cmdList() {
	cmdCurrent()
}

func cmdSave(name string) {
	if err := ensureDirs(); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	paths := getPaths()
	activeInfo, err := readTokenInfo(paths.TokenPath)
	if err != nil {
		fmt.Println(colorize("❌ Error: Active token file not found at ~/.gemini/antigravity-cli/antigravity-oauth-token", ColorRed))
		fmt.Println("   Please run `agy` to log in before saving a profile.")
		return
	}

	reader := bufio.NewReader(os.Stdin)
	if name == "" {
		defaultName := "default"
		if strings.Contains(activeInfo.Email, "@") {
			defaultName = strings.Split(activeInfo.Email, "@")[0]
		}
		fmt.Printf("Enter profile name [Default: %s]: ", defaultName)
		input, _ := reader.ReadString('\n')
		name = strings.TrimSpace(input)
		if name == "" {
			name = defaultName
		}
	}

	name = sanitizeName(name)
	if name == "" {
		fmt.Println(colorize("❌ Invalid profile name. Only letters, numbers, _, and - are allowed.", ColorRed))
		return
	}

	targetPath := filepath.Join(paths.ProfilesDir, name+".json")
	if _, err := os.Stat(targetPath); err == nil {
		fmt.Printf("Profile '%s' already exists. Overwrite? [y/N]: ", name)
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Cancelled.")
			return
		}
	}

	// Verify account status with Google before saving
	fmt.Print(colorize("🔍 Verifying account status with Google...", ColorDim))
	ctxCheck, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCheck()
	q := fetchProfileQuota(ctxCheck, paths.TokenPath)
	if q.Err != nil && strings.Contains(q.Err.Error(), "Verify your account") {
		fmt.Println()
		fmt.Println(colorize("❌ Cannot save profile: Account requires security verification by Google (\"Verify your account\").", ColorBold+ColorRed))
		fmt.Println(colorize("   Please open your browser, complete Google's verification checkpoint, and try again.", ColorYellow))
		return
	}
	if q.Err == nil {
		fmt.Println(colorize(" ✓ Account healthy", ColorGreen))
	} else {
		fmt.Printf(colorize(" (Notice: %v)\n", ColorYellow), q.Err)
	}

	if err := copyFile(paths.TokenPath, targetPath, 0600); err != nil {
		fmt.Printf(colorize("❌ Error saving profile file: %v\n", ColorRed), err)
		return
	}
	_ = os.WriteFile(paths.CurrentFile, []byte(name), 0600)

	fmt.Println(colorize(fmt.Sprintf("✅ Profile saved successfully: %s", name), ColorBold+ColorGreen))
	fmt.Printf("   Email: %s\n", colorize(activeInfo.Email, ColorCyan))
	fmt.Printf("   Name : %s\n", activeInfo.Name)
}

func cmdSwitch(name string) {
	name = sanitizeName(name)
	paths := getPaths()

	profiles, _ := getAllProfiles()
	var targetProfile *ProfileInfo
	for _, p := range profiles {
		if strings.EqualFold(p.ProfileName, name) {
			targetProfile = &p
			break
		}
	}

	if targetProfile == nil {
		fmt.Println(colorize(fmt.Sprintf("❌ Profile not found: '%s'", name), ColorRed))
		cmdList()
		return
	}

	if targetProfile.IsActive {
		fmt.Println(colorize(fmt.Sprintf("ℹ️  Profile '%s' is ALREADY active. No switch needed.", name), ColorYellow))
		return
	}

	if targetProfile.IsCoolingDown {
		fmt.Println(colorize(fmt.Sprintf("⚠️  Note: Profile '%s' is currently in cooldown (%s remaining).", name, targetProfile.CooldownRemain), ColorYellow))
	}

	if err := switchProfileByName(targetProfile.ProfileName); err != nil {
		fmt.Printf(colorize("❌ Error switching profile: %v\n", ColorRed), err)
		return
	}

	info, _ := readTokenInfo(paths.TokenPath)
	email := "N/A"
	nameUser := "N/A"
	statusTok := "N/A"
	if info != nil {
		email = info.Email
		nameUser = info.Name
		statusTok = info.StatusToken
	}

	fmt.Println()
	fmt.Println(colorize(fmt.Sprintf("🚀 SWITCHED TO PROFILE: %s", strings.ToUpper(targetProfile.ProfileName)), ColorBold+ColorGreen))
	fmt.Println(colorize(strings.Repeat("═", 50), ColorDim))
	fmt.Printf("   Email : %s\n", colorize(email, ColorBold+ColorCyan))
	fmt.Printf("   Name  : %s\n", nameUser)
	fmt.Printf("   Token : %s\n", statusTok)
	fmt.Println(colorize(strings.Repeat("═", 50), ColorDim))
	fmt.Println(colorize("👉 You can now run `agy` with this account!", ColorGreen))
	fmt.Println()
}

func cmdNext() {
	nextName, err := switchNextProfile("Manual switch")
	if err != nil {
		fmt.Println(colorize(fmt.Sprintf("❌ Cannot switch: %v", err), ColorRed))
		return
	}

	paths := getPaths()
	info, _ := readTokenInfo(paths.TokenPath)
	email := "N/A"
	if info != nil {
		email = info.Email
	}

	fmt.Println()
	fmt.Println(colorize(fmt.Sprintf("🔄 ROTATED TO NEXT PROFILE: %s", strings.ToUpper(nextName)), ColorBold+ColorGreen))
	fmt.Printf("   Email: %s\n", colorize(email, ColorCyan))
	fmt.Println()
}

func cmdCurrent() {
	profiles, _ := getAllProfiles()
	if len(profiles) == 0 {
		fmt.Println()
		fmt.Println(colorize("❌ No saved profiles found.", ColorYellow))
		fmt.Printf("Tip: Run %s to save your active account.\n\n", colorize("agys save <name>", ColorGreen))
		return
	}

	quotas := fetchAllProfilesQuota(profiles, 5*time.Second)

	fmt.Println()
	fmt.Println(colorize("📊 AGY ACCOUNTS STATUS & QUOTA", ColorBold+ColorCyan))
	fmt.Println(colorize(strings.Repeat("═", 58), ColorDim))

	for idx, p := range profiles {
		q := quotas[p.ProfileName]

		badge := colorize("○ Ready", ColorDim)
		if p.IsActive {
			badge = colorize("● ACTIVE", ColorBold+ColorGreen)
		} else if p.IsCoolingDown {
			badge = colorize("🟡 "+p.CooldownRemain+" cooldown", ColorYellow)
		}

		pTitle := fmt.Sprintf("[%d] %s", idx+1, p.ProfileName)
		if p.IsActive {
			pTitle = colorize(pTitle, ColorBold)
		}

		fmt.Printf("%s (%s)\n", pTitle, badge)
		fmt.Printf("    Email  : %s\n", colorize(p.Email, ColorCyan))

		claudeStr := formatQuotaBadge(q.ClaudeRemaining, q.Err)
		if q.Err == nil && q.ClaudeResetTime != "" {
			claudeStr += colorize(formatQuotaReset(q.ClaudeResetTime), ColorDim)
		}
		fmt.Printf("    Claude : %s\n", claudeStr)

		geminiStr := formatQuotaBadge(q.GeminiRemaining, q.Err)
		if q.Err == nil && q.GeminiResetTime != "" {
			geminiStr += colorize(formatQuotaReset(q.GeminiResetTime), ColorDim)
		}
		fmt.Printf("    Gemini : %s\n", geminiStr)

		if idx < len(profiles)-1 {
			fmt.Println(colorize("    "+strings.Repeat("─", 50), ColorDim))
		}
	}

	fmt.Println(colorize(strings.Repeat("═", 58), ColorDim))
	fmt.Println()
	CheckUpdateNotice()
}

func cmdBurn() {
	profiles, _ := getAllProfiles()
	if len(profiles) == 0 {
		fmt.Println(colorize("❌ No saved profiles found.", ColorYellow))
		return
	}

	fmt.Println()
	fmt.Println(colorize("🔥 AGYS SMART BURN: 5H VS WEEKLY CAP OPTIMIZER", ColorBold+ColorCyan))
	fmt.Println(colorize(strings.Repeat("═", 88), ColorDim))
	fmt.Println("Scanning real-time quotas, 5h burst windows, and 7-day Weekly Caps...")
	fmt.Println()

	quotas := fetchAllProfilesQuota(profiles, 5*time.Second)
	now := time.Now()

	header := fmt.Sprintf("%-4s %-14s %-14s %-14s %-18s %-18s", "ID", "PROFILE", "CLAUDE", "GEMINI", "CYCLE", "RECOMMENDATION")
	fmt.Println(header)
	fmt.Println(colorize(strings.Repeat("─", 88), ColorDim))

	activeProfile := ""
	for _, p := range profiles {
		if p.IsActive {
			activeProfile = p.ProfileName
			break
		}
	}

	for idx, p := range profiles {
		q := quotas[p.ProfileName]
		cand := evaluateProfileCandidate(p, q, now)

		pName := p.ProfileName
		if p.IsActive {
			pName = colorize(pName, ColorBold)
		}

		claudeBadge := formatQuotaBadge(q.ClaudeRemaining, q.Err)
		geminiBadge := formatQuotaBadge(q.GeminiRemaining, q.Err)

		cycleTag := colorize("[5h Cycle]", ColorGreen)
		if cand.IsClaudeWeekly && cand.ClaudeRemaining < 0.05 {
			cycleTag = colorize("[Weekly Cap ⚠️]", ColorRed)
		} else if cand.IsClaudeWeekly {
			cycleTag = colorize("[Weekly Cap ⚠️]", ColorYellow)
		}

		recommendation := colorize("○ Eligible", ColorDim)
		if p.IsActive {
			recommendation = colorize("● ACTIVE", ColorBold+ColorGreen)
		} else if cand.IsLocked {
			recommendation = colorize("❌ "+cand.LockReason, ColorRed)
		} else if cand.IsClaudeWeekly && cand.ClaudeRemaining < 0.05 {
			recommendation = colorize("⚠️ Claude locked (Gemini only)", ColorYellow)
		} else if cand.ClaudeDiff > 0 && cand.ClaudeDiff <= 90*time.Minute && cand.ClaudeRemaining >= 0.10 {
			recommendation = colorize("🔥 URGENT BURN", ColorBold+ColorYellow)
		}

		sttStr := fmt.Sprintf("[%2d]", idx+1)
		line := fmt.Sprintf("%-4s %-14s %-14s %-14s %-18s %s",
			sttStr,
			padRight(pName, 14),
			padRight(claudeBadge, 14),
			padRight(geminiBadge, 14),
			padRight(cycleTag, 18),
			recommendation,
		)
		fmt.Println(line)
	}

	fmt.Println(colorize(strings.Repeat("─", 88), ColorDim))
	fmt.Println()

	// Find best burn candidate (including active profile to see if active is already best)
	bestOverall, reasonOverall := selectSmartProfile(profiles, "", false)
	if bestOverall == "" {
		fmt.Println(colorize("⚠️  No eligible candidate found with healthy quota. All accounts may be cooling down or exhausted.", ColorYellow))
		return
	}

	if bestOverall == activeProfile {
		fmt.Println(colorize(fmt.Sprintf("✅ Active account '%s' is ALREADY the optimal burn candidate!", activeProfile), ColorBold+ColorGreen))
		fmt.Printf("   Reason: %s\n\n", colorize(reasonOverall, ColorCyan))
		return
	}

	fmt.Println(colorize(fmt.Sprintf("🚀 Switching to optimal burn candidate: '%s'", strings.ToUpper(bestOverall)), ColorBold+ColorGreen))
	fmt.Printf("   Reason: %s\n\n", colorize(reasonOverall, ColorCyan))

	if err := switchProfileByName(bestOverall); err != nil {
		fmt.Println(colorize(fmt.Sprintf("❌ Switch failed: %v", err), ColorRed))
		return
	}

	fmt.Println(colorize(fmt.Sprintf("🎉 Successfully switched to '%s'!", bestOverall), ColorBold+ColorGreen))
	fmt.Println()
}

func selectProfileInteractive(profiles []ProfileInfo) int {
	if len(profiles) == 0 {
		return -1
	}

	selectedIdx := 0
	for i, p := range profiles {
		if p.IsActive {
			selectedIdx = i
			break
		}
	}

	quotas := fetchAllProfilesQuota(profiles, 3500*time.Millisecond)

	savedState := getSttyState()
	defer func() {
		restoreSttyState(savedState)
		fmt.Print("\033[?25h") // Show cursor
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	go func() {
		<-sigChan
		restoreSttyState(savedState)
		fmt.Print("\033[?25h\r\n")
		os.Exit(1)
	}()

	_ = exec.Command("stty", "-icanon", "-echo", "min", "1", "time", "1").Run()
	fmt.Print("\033[?25l") // Hide cursor

	totalLines := len(profiles) + 6

	render := func(firstRender bool) {
		if !firstRender {
			fmt.Printf("\033[%dA\r", totalLines)
		}

		fmt.Print(colorize("📋 SELECT AGY ACCOUNT", ColorBold+ColorCyan) + "\033[K\r\n")
		fmt.Print(colorize(strings.Repeat("═", 86), ColorDim) + "\033[K\r\n")

		header := fmt.Sprintf("  %-4s %-12s %-26s %-12s %-12s %-12s", "ID", "PROFILE", "EMAIL", "CLAUDE", "GEMINI", "STATUS")
		fmt.Print(header + "\033[K\r\n")
		fmt.Print(colorize(strings.Repeat("─", 86), ColorDim) + "\033[K\r\n")

		for idx, p := range profiles {
			isSelected := (idx == selectedIdx)
			q := quotas[p.ProfileName]

			status := colorize("○ Ready", ColorDim)
			if p.IsActive {
				status = colorize("● ACTIVE", ColorBold+ColorGreen)
			} else if p.IsCoolingDown {
				status = colorize("🟡 "+p.CooldownRemain, ColorYellow)
			} else if q.Err == nil && q.ClaudeRemaining < 0.05 && q.ClaudeResetTime != "" {
				if t, err := time.Parse(time.RFC3339, q.ClaudeResetTime); err == nil && time.Until(t) > 5*time.Hour+15*time.Minute {
					status = colorize("🔴 Weekly Cap", ColorRed)
				}
			}

			pName := p.ProfileName
			if p.IsActive {
				pName = colorize(pName, ColorBold)
			}

			claudeBadge := formatQuotaBadge(q.ClaudeRemaining, q.Err)
			geminiBadge := formatQuotaBadge(q.GeminiRemaining, q.Err)

			pointer := "  "
			sttStr := fmt.Sprintf("[%d]", idx+1)
			if isSelected {
				pointer = colorize("❯ ", ColorBold+ColorCyan)
				sttStr = colorize(sttStr, ColorBold+ColorCyan)
				pName = colorize(p.ProfileName, ColorBold)
			} else {
				sttStr = colorize(sttStr, ColorDim)
			}

			row := fmt.Sprintf("%s%-4s %-12s %-26s %-12s %-12s %s\033[K\r\n",
				pointer,
				padRight(sttStr, 4),
				padRight(pName, 12),
				padRight(p.Email, 26),
				padRight(claudeBadge, 12),
				padRight(geminiBadge, 12),
				status,
			)
			fmt.Print(row)
		}

		fmt.Print(colorize(strings.Repeat("─", 86), ColorDim) + "\033[K\r\n")
		helpText := fmt.Sprintf("%s • %s • %s • %s",
			colorize("↑/↓ or j/k: navigate", ColorCyan),
			colorize("Enter: switch", ColorBold+ColorGreen),
			colorize("1-9: jump", ColorYellow),
			colorize("q/Esc: cancel", ColorDim),
		)
		fmt.Print(helpText + "\033[K\r\n")
	}

	render(true)

	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return -1
		}
		if n == 0 {
			continue
		}

		if n == 1 {
			b := buf[0]
			switch b {
			case 3, 27, 'q', 'Q':
				return -1
			case 10, 13, ' ':
				return selectedIdx
			case 'k', 'K':
				if selectedIdx > 0 {
					selectedIdx--
				} else {
					selectedIdx = len(profiles) - 1
				}
				render(false)
			case 'j', 'J':
				if selectedIdx < len(profiles)-1 {
					selectedIdx++
				} else {
					selectedIdx = 0
				}
				render(false)
			case 'g':
				selectedIdx = 0
				render(false)
			case 'G':
				selectedIdx = len(profiles) - 1
				render(false)
			default:
				if b >= '1' && b <= '9' {
					num := int(b - '0')
					if num <= len(profiles) {
						return num - 1
					}
				}
			}
			continue
		}

		if buf[0] == 27 {
			if n >= 3 && (buf[1] == '[' || buf[1] == 'O') {
				switch buf[2] {
				case 'A': // Up
					if selectedIdx > 0 {
						selectedIdx--
					} else {
						selectedIdx = len(profiles) - 1
					}
					render(false)
				case 'B': // Down
					if selectedIdx < len(profiles)-1 {
						selectedIdx++
					} else {
						selectedIdx = 0
					}
					render(false)
				case 'H': // Home
					selectedIdx = 0
					render(false)
				case 'F': // End
					selectedIdx = len(profiles) - 1
					render(false)
				}
			}
		}
	}
}

func cmdInteractive() {
	profiles, _ := getAllProfiles()
	if len(profiles) == 0 {
		cmdList()
		return
	}

	if isTerminalStdin() {
		choiceIdx := selectProfileInteractive(profiles)
		if choiceIdx < 0 {
			fmt.Println("Exited.")
			return
		}
		cmdSwitch(profiles[choiceIdx].ProfileName)
		return
	}

	cmdList()
	fmt.Println(colorize("Select an account number to switch (or 'q' to quit):", ColorBold))
	fmt.Print(colorize("👉 Enter number: ", ColorCyan))

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	choice := strings.TrimSpace(input)

	if choice == "" || strings.ToLower(choice) == "q" || strings.ToLower(choice) == "quit" || strings.ToLower(choice) == "exit" {
		fmt.Println("Exited.")
		return
	}

	if idx, err := strconv.Atoi(choice); err == nil {
		if idx >= 1 && idx <= len(profiles) {
			cmdSwitch(profiles[idx-1].ProfileName)
			return
		}
	}

	for _, p := range profiles {
		if strings.EqualFold(p.ProfileName, choice) {
			cmdSwitch(p.ProfileName)
			return
		}
	}

	fmt.Println(colorize("❌ Invalid choice.", ColorRed))
}

func cmdLoginNew(name string) {
	if err := ensureDirs(); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	reader := bufio.NewReader(os.Stdin)
	if name == "" {
		fmt.Print("Enter profile name for the new account: ")
		input, _ := reader.ReadString('\n')
		name = strings.TrimSpace(input)
	}

	name = sanitizeName(name)
	if name == "" {
		fmt.Println(colorize("❌ Invalid profile name.", ColorRed))
		return
	}

	paths := getPaths()
	tempActiveBak := filepath.Join(paths.CLIHome, fmt.Sprintf("active_before_new_%d", time.Now().Unix()))

	// Backup current token file & current Keychain entry
	hadActiveFile := false
	if _, err := os.Stat(paths.TokenPath); err == nil {
		_ = copyFile(paths.TokenPath, tempActiveBak, 0600)
		hadActiveFile = true
	}
	keychainBackup := getKeychainPassword()

	// Defer restoration in case of interruption or cancellation
	loginCompleted := false
	cleanupRollback := func() {
		if !loginCompleted {
			if hadActiveFile {
				_ = copyFile(tempActiveBak, paths.TokenPath, 0600)
				_ = os.Remove(tempActiveBak)
			}
			if keychainBackup != "" {
				_ = setKeychainPassword(keychainBackup)
			}
		}
	}
	defer cleanupRollback()

	// Catch OS signals during login wizard to ensure rollback runs even on Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cleanupRollback()
		fmt.Println("\n" + colorize("Login cancelled. Previous account restored.", ColorYellow))
		os.Exit(1)
	}()
	defer signal.Stop(sigChan)

	// Temporarily clear credentials so agy starts genuine Google login
	_ = os.Remove(paths.TokenPath)
	_ = deleteKeychainPassword()

	fmt.Println()
	fmt.Println(colorize(fmt.Sprintf("🔑 STARTING LOGIN FOR PROFILE: %s", strings.ToUpper(name)), ColorBold+ColorCyan))
	fmt.Println("Launching `agy` to start Google browser authentication...")
	fmt.Printf("Once authentication finishes in your browser, type %s inside agy to complete.\n\n", colorize("/exit", ColorYellow))

	cmd := exec.Command("agy")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Println(colorize("⚠️  agy session ended or was interrupted.", ColorYellow))
	}

	newKeychain := getKeychainPassword()
	hasNewKeychain := (newKeychain != "")
	hasFile := false
	if fi, err := os.Stat(paths.TokenPath); err == nil && fi.Size() > 0 {
		hasFile = true
	}

	if hasFile || hasNewKeychain {
		targetPath := filepath.Join(paths.ProfilesDir, name+".json")
		email := "N/A"

		if hasFile {
			// Token file on disk was written by agy and contains the full OIDC id_token
			_ = copyFile(paths.TokenPath, targetPath, 0600)
			info, _ := readTokenInfo(paths.TokenPath)
			if info != nil {
				email = info.Email
			}
			// Sync keychain from file
			var tokFile OAuthTokenFile
			data, _ := os.ReadFile(paths.TokenPath)
			if err := json.Unmarshal(data, &tokFile); err == nil {
				_ = syncKeychainFromTokenFile(tokFile, email)
			}
		} else if hasNewKeychain {
			// agy wrote to Keychain only
			tokFile, emailFromKeyring, err := parseKeychainToken(newKeychain)
			if err == nil && tokFile != nil {
				data, _ := json.MarshalIndent(tokFile, "", "  ")
				_ = os.WriteFile(targetPath, data, 0600)
				_ = os.WriteFile(paths.TokenPath, data, 0600)
				email = emailFromKeyring
			}
		}

		// Verify account status with Google before finalizing profile
		fmt.Print(colorize("🔍 Verifying account status with Google...", ColorDim))
		ctxCheck, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCheck()
		q := fetchProfileQuota(ctxCheck, targetPath)
		if q.Err != nil && strings.Contains(q.Err.Error(), "Verify your account") {
			fmt.Println()
			fmt.Println(colorize("❌ Account rejected: Google requires security verification (\"Verify your account\").", ColorBold+ColorRed))
			fmt.Println(colorize("   Please open your browser, complete Google's verification checkpoint, and try again.", ColorYellow))
			_ = os.Remove(targetPath)
			return
		}
		if q.Err == nil {
			fmt.Println(colorize(" ✓ Account healthy", ColorGreen))
		} else {
			fmt.Printf(colorize(" (Notice: %v)\n", ColorYellow), q.Err)
		}

		_ = os.WriteFile(paths.CurrentFile, []byte(name), 0600)
		loginCompleted = true
		if hadActiveFile {
			_ = os.Remove(tempActiveBak)
		}

		fmt.Println()
		fmt.Println(colorize(fmt.Sprintf("🎉 Successfully logged in and saved profile '%s'!", name), ColorBold+ColorGreen))
		fmt.Printf("   Email: %s\n\n", colorize(email, ColorCyan))
	} else {
		fmt.Println(colorize("❌ Login was not completed. Restoring previous account...", ColorRed))
	}
}

func cmdRemove(name string) {
	name = sanitizeName(name)
	paths := getPaths()
	target := filepath.Join(paths.ProfilesDir, name+".json")

	if _, err := os.Stat(target); os.IsNotExist(err) {
		fmt.Println(colorize(fmt.Sprintf("❌ Profile not found: %s", name), ColorRed))
		return
	}

	fmt.Printf("Are you sure you want to delete profile '%s'? [y/N]: ", name)
	reader := bufio.NewReader(os.Stdin)
	confirm, _ := reader.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(confirm)) == "y" {
		_ = os.Remove(target)
		clearProfileCooldown(name)

		// Remove .current if it points to the deleted profile to prevent stale reference
		if b, err := os.ReadFile(paths.CurrentFile); err == nil {
			if strings.TrimSpace(string(b)) == name {
				_ = os.Remove(paths.CurrentFile)
			}
		}

		fmt.Println(colorize(fmt.Sprintf("🗑️  Deleted profile '%s'.", name), ColorGreen))
	} else {
		fmt.Println("Cancelled.")
	}
}

func cmdRename(oldName, newName string) {
	if err := ensureDirs(); err != nil {
		fmt.Printf("Error accessing directory: %v\n", err)
		return
	}

	oldName = sanitizeName(oldName)
	newName = sanitizeName(newName)

	if oldName == "" || newName == "" {
		fmt.Println(colorize("❌ Usage: agys rename <old_name> <new_name>", ColorRed))
		return
	}

	if strings.EqualFold(oldName, newName) {
		fmt.Println(colorize("ℹ️  Old and new profile names are identical.", ColorYellow))
		return
	}

	paths := getPaths()
	oldPath := filepath.Join(paths.ProfilesDir, oldName+".json")
	newPath := filepath.Join(paths.ProfilesDir, newName+".json")

	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		fmt.Println(colorize(fmt.Sprintf("❌ Profile '%s' not found.", oldName), ColorRed))
		cmdList()
		return
	}

	if _, err := os.Stat(newPath); err == nil {
		fmt.Println(colorize(fmt.Sprintf("❌ Profile '%s' already exists. Choose another name.", newName), ColorRed))
		return
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		fmt.Printf(colorize("❌ Failed to rename profile: %v\n", ColorRed), err)
		return
	}

	// Update .current if this was the active profile
	if b, err := os.ReadFile(paths.CurrentFile); err == nil {
		if strings.TrimSpace(string(b)) == oldName {
			_ = os.WriteFile(paths.CurrentFile, []byte(newName), 0600)
		}
	}

	// Migrate cooldown entry if exists
	cooldowns := loadCooldowns()
	if exp, exists := cooldowns[oldName]; exists {
		delete(cooldowns, oldName)
		cooldowns[newName] = exp
		saveCooldowns(cooldowns)
	}

	fmt.Println()
	fmt.Println(colorize(fmt.Sprintf("✅ Profile renamed from '%s' to '%s' successfully!", oldName, newName), ColorBold+ColorGreen))
	fmt.Println()
}

func cmdCooldown(args []string) {
	if len(args) == 0 {
		cooldowns := loadCooldowns()
		if len(cooldowns) == 0 {
			fmt.Println(colorize("No profiles are currently in cooldown.", ColorGreen))
			return
		}
		fmt.Println(colorize("🟡 Cooldown Profiles:", ColorBold+ColorYellow))
		for name, exp := range cooldowns {
			rem := time.Until(time.Unix(exp, 0))
			fmt.Printf("   - %s: %dm remaining\n", name, int(rem.Minutes()))
		}
		fmt.Println("\nTo clear: " + colorize("agys cooldown clear", ColorCyan))
		return
	}

	if args[0] == "clear" || args[0] == "reset" {
		clearAllCooldowns()
		fmt.Println(colorize("✅ Cleared all cooldowns! All profiles are now marked Ready.", ColorGreen))
	} else {
		clearProfileCooldown(args[0])
		fmt.Println(colorize(fmt.Sprintf("✅ Cleared cooldown for profile '%s'.", args[0]), ColorGreen))
	}
}

func cmdInstall() {
	execPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Error resolving executable path: %v\n", err)
		return
	}

	dirs := []string{
		"/opt/homebrew/bin",
		filepath.Join(os.Getenv("HOME"), ".local", "bin"),
		"/usr/local/bin",
	}

	installed := false
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			continue
		}

		targetAgys := filepath.Join(dir, "agys")
		targetAgySwitch := filepath.Join(dir, "agy-switch")

		_ = os.Remove(targetAgys)
		_ = os.Remove(targetAgySwitch)

		if err := os.Symlink(execPath, targetAgys); err == nil {
			_ = os.Symlink(execPath, targetAgySwitch)
			fmt.Println(colorize(fmt.Sprintf("✅ Global shortcut installed at: %s", targetAgys), ColorBold+ColorGreen))
			fmt.Printf("👉 You can now run '%s' (or '%s') from any terminal session.\n", colorize("agys", ColorCyan), colorize("agy-switch", ColorDim))
			installed = true
			break
		}
	}

	if !installed {
		fmt.Println(colorize("⚠️  Could not create symlink in system bin directory.", ColorYellow))
	}
}

func printHelp() {
	fmt.Println(`agys - Fast & lightweight multi-account manager & auto-failover runner for Google Antigravity CLI

RUN & CODE:
  agys run [args...]           Run agy with auto-switch & context resume on rate limits (429)
  agys conv [dir]              Interactive picker (↑/↓) to select & resume conversations

ACCOUNTS & QUOTAS:
  agys status                  View all accounts, real-time quotas, 5h cycles & Weekly Caps (alias: agys ls)
  agys <profile>               Instantly switch to the specified profile
  agys next                    Auto-switch to the optimal account (smart burn & balance)
  agys                         Interactive menu to choose and switch accounts

MANAGEMENT:
  agys login [name]            Log in to a new Google account and save profile
  agys save [name]             Save current active session as a profile
  agys rename <old> <new>      Rename a saved profile
  agys remove <name>           Delete a profile

UTILITY:
  agys update                  Update agys to the latest GitHub release
  agys version                 Show current installed version
  agys help                    Show this help message`)
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		cmdInteractive()
		return
	}

	switch args[0] {
	case "status", "ls", "list":
		cmdCurrent()
	case "next", "burn":
		cmdNext()
	case "run":
		cmdRun(args[1:])
	case "conv", "conversations", "chats":
		cmdConversations(args[1:])
	case "switch":
		if len(args) < 2 {
			fmt.Println(colorize("❌ Please provide profile name: agys switch <name>", ColorRed))
			return
		}
		cmdSwitch(args[1])
	case "login":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		cmdLoginNew(name)
	case "save":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		cmdSave(name)
	case "rename":
		if len(args) < 3 {
			fmt.Println(colorize("❌ Please specify both old and new names: agys rename <old> <new>", ColorRed))
			return
		}
		cmdRename(args[1], args[2])
	case "remove":
		if len(args) < 2 {
			fmt.Println(colorize("❌ Please provide profile name: agys remove <name>", ColorRed))
			return
		}
		cmdRemove(args[1])
	case "update", "upgrade":
		cmdUpdate()
	case "version", "--version", "-v":
		cmdVersion()
	case "install":
		cmdInstall()
	case "cooldown":
		cmdCooldown(args[1:])
	case "help", "--help", "-h":
		printHelp()
	default:
		if strings.HasPrefix(args[0], "-") {
			fmt.Printf("Unknown flag: %s\n\n", args[0])
			printHelp()
			os.Exit(1)
		}
		// Shortcut syntax: `agys <profile_name>`
		cmdSwitch(args[0])
	}
}
