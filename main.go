package main

import (
	"bufio"
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
	_, tokenPath, _, _, _, _, _ := getPaths()
	profiles, _ := getAllProfiles()
	activeInfo, _ := readTokenInfo(tokenPath)

	fmt.Println()
	fmt.Println(colorize("📋 AGY ACCOUNTS & PROFILES", ColorBold+ColorCyan))
	fmt.Println(colorize(strings.Repeat("═", 70), ColorDim))

	if len(profiles) == 0 {
		fmt.Println(colorize("No profiles saved yet.", ColorYellow))
		fmt.Printf("Tip: Run %s to save your active account.\n\n", colorize("agy-switch save <name>", ColorGreen))
		return
	}

	header := fmt.Sprintf("%-4s %-14s %-26s %-14s %s", "ID", "PROFILE", "EMAIL", "STATUS", "TOKEN")
	fmt.Println(header)
	fmt.Println(colorize(strings.Repeat("─", 70), ColorDim))

	anyActive := false
	for idx, p := range profiles {
		status := colorize("○ Ready", ColorDim)
		if p.IsActive {
			status = colorize("● ACTIVE", ColorBold+ColorGreen)
			anyActive = true
		} else if p.IsCoolingDown {
			status = colorize("🟡 "+p.CooldownRemain, ColorYellow)
		}

		ver := colorize("🟢 "+p.StatusToken, ColorGreen)
		if !p.IsValid {
			ver = colorize("🔴 "+p.StatusToken, ColorRed)
		}

		pName := p.ProfileName
		if p.IsActive {
			pName = colorize(pName, ColorBold)
		}

		sttStr := fmt.Sprintf("[%2d]", idx+1)
		line := fmt.Sprintf("%s %s %s %s %s",
			padRight(sttStr, 4),
			padRight(pName, 14),
			padRight(p.Email, 26),
			padRight(status, 14),
			ver,
		)
		fmt.Println(line)
	}

	fmt.Println(colorize(strings.Repeat("─", 70), ColorDim))

	if !anyActive && activeInfo != nil {
		fmt.Println(colorize(fmt.Sprintf("⚠️  Active account (%s) is not saved to any profile.", activeInfo.Email), ColorYellow))
		fmt.Printf("   Run: %s to save this profile.\n", colorize("agy-switch save <name>", ColorGreen))
	}
	fmt.Println()
}

func cmdSave(name string) {
	if err := ensureDirs(); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	_, tokenPath, profilesDir, currentFile, _, _, _ := getPaths()
	activeInfo, err := readTokenInfo(tokenPath)
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

	targetPath := filepath.Join(profilesDir, name+".json")
	if _, err := os.Stat(targetPath); err == nil {
		fmt.Printf("Profile '%s' already exists. Overwrite? [y/N]: ", name)
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Cancelled.")
			return
		}
	}

	if err := copyFile(tokenPath, targetPath, 0600); err != nil {
		fmt.Printf(colorize("❌ Error saving profile file: %v\n", ColorRed), err)
		return
	}
	_ = os.WriteFile(currentFile, []byte(name), 0600)

	fmt.Println(colorize(fmt.Sprintf("✅ Profile saved successfully: %s", name), ColorBold+ColorGreen))
	fmt.Printf("   Email: %s\n", colorize(activeInfo.Email, ColorCyan))
	fmt.Printf("   Name : %s\n", activeInfo.Name)
}

func cmdSwitch(name string) {
	name = sanitizeName(name)
	_, tokenPath, _, _, _, _, _ := getPaths()

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

	info, _ := readTokenInfo(tokenPath)
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

	_, tokenPath, _, _, _, _, _ := getPaths()
	info, _ := readTokenInfo(tokenPath)
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
	_, tokenPath, _, _, _, _, _ := getPaths()
	info, err := readTokenInfo(tokenPath)
	if err != nil {
		fmt.Println(colorize("❌ No active account found on agy CLI.", ColorRed))
		return
	}

	profiles, _ := getAllProfiles()
	matchedProfile := ""
	for _, p := range profiles {
		if p.IsActive {
			matchedProfile = p.ProfileName
			break
		}
	}

	fmt.Println()
	fmt.Println(colorize("👤 ACTIVE AGY ACCOUNT", ColorBold+ColorCyan))
	fmt.Println(colorize(strings.Repeat("─", 45), ColorDim))
	if matchedProfile != "" {
		fmt.Printf("Profile     : %s\n", colorize(matchedProfile, ColorBold+ColorGreen))
	} else {
		fmt.Printf("Profile     : %s\n", colorize("Unsaved profile", ColorYellow))
	}
	fmt.Printf("Email       : %s\n", colorize(info.Email, ColorBold+ColorCyan))
	fmt.Printf("User Name   : %s\n", info.Name)

	statusDisp := colorize("🟢 "+info.StatusToken, ColorGreen)
	if !info.IsValid {
		statusDisp = colorize("🔴 "+info.StatusToken, ColorRed)
	}
	fmt.Printf("Token Status: %s\n", statusDisp)
	fmt.Printf("Path        : %s\n", tokenPath)
	fmt.Println()
}

func cmdInteractive() {
	profiles, _ := getAllProfiles()
	if len(profiles) == 0 {
		cmdList()
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

	cliHome, tokenPath, profilesDir, currentFile, _, _, _ := getPaths()
	tempActiveBak := filepath.Join(cliHome, fmt.Sprintf("active_before_new_%d", time.Now().Unix()))

	// Backup current token file & current Keychain entry
	hadActiveFile := false
	if _, err := os.Stat(tokenPath); err == nil {
		_ = copyFile(tokenPath, tempActiveBak, 0600)
		hadActiveFile = true
	}
	keychainBackup := getKeychainPassword()

	// Defer restoration in case of interruption or cancellation
	loginCompleted := false
	cleanupRollback := func() {
		if !loginCompleted {
			if hadActiveFile {
				_ = copyFile(tempActiveBak, tokenPath, 0600)
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
	_ = os.Remove(tokenPath)
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
	if fi, err := os.Stat(tokenPath); err == nil && fi.Size() > 0 {
		hasFile = true
	}

	if hasFile || hasNewKeychain {
		targetPath := filepath.Join(profilesDir, name+".json")
		email := "N/A"

		if hasFile {
			// Token file on disk was written by agy and contains the full OIDC id_token
			_ = copyFile(tokenPath, targetPath, 0600)
			info, _ := readTokenInfo(tokenPath)
			if info != nil {
				email = info.Email
			}
			// Sync keychain from file
			var tokFile OAuthTokenFile
			data, _ := os.ReadFile(tokenPath)
			if err := json.Unmarshal(data, &tokFile); err == nil {
				_ = syncKeychainFromTokenFile(tokFile, email)
			}
		} else if hasNewKeychain {
			// agy wrote to Keychain only
			tokFile, emailFromKeyring, err := parseKeychainToken(newKeychain)
			if err == nil && tokFile != nil {
				data, _ := json.MarshalIndent(tokFile, "", "  ")
				_ = os.WriteFile(targetPath, data, 0600)
				_ = os.WriteFile(tokenPath, data, 0600)
				email = emailFromKeyring
			}
		}

		_ = os.WriteFile(currentFile, []byte(name), 0600)
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
	_, _, profilesDir, currentFile, _, _, _ := getPaths()
	target := filepath.Join(profilesDir, name+".json")

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
		if b, err := os.ReadFile(currentFile); err == nil {
			if strings.TrimSpace(string(b)) == name {
				_ = os.Remove(currentFile)
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
		fmt.Println(colorize("❌ Usage: agy-switch rename <old_name> <new_name>", ColorRed))
		return
	}

	if strings.EqualFold(oldName, newName) {
		fmt.Println(colorize("ℹ️  Old and new profile names are identical.", ColorYellow))
		return
	}

	_, _, profilesDir, currentFile, _, _, _ := getPaths()
	oldPath := filepath.Join(profilesDir, oldName+".json")
	newPath := filepath.Join(profilesDir, newName+".json")

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
	if b, err := os.ReadFile(currentFile); err == nil {
		if strings.TrimSpace(string(b)) == oldName {
			_ = os.WriteFile(currentFile, []byte(newName), 0600)
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
		fmt.Println("\nTo clear: " + colorize("agy-switch cooldown clear", ColorCyan))
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

	targets := []string{
		"/opt/homebrew/bin/agy-switch",
		filepath.Join(os.Getenv("HOME"), ".local", "bin", "agy-switch"),
		"/usr/local/bin/agy-switch",
	}

	installed := false
	for _, target := range targets {
		dir := filepath.Dir(target)
		if err := os.MkdirAll(dir, 0755); err != nil {
			continue
		}

		_ = os.Remove(target)
		if err := os.Symlink(execPath, target); err == nil {
			fmt.Println(colorize(fmt.Sprintf("✅ Global shortcut installed at: %s", target), ColorBold+ColorGreen))
			fmt.Printf("👉 You can now run '%s' from any terminal session.\n", colorize("agy-switch", ColorCyan))
			installed = true
			break
		}
	}

	if !installed {
		fmt.Println(colorize("⚠️  Could not create symlink in system bin directory. You can add this alias to ~/.zshrc:", ColorYellow))
		fmt.Println(colorize(fmt.Sprintf(`echo "alias agy-switch='%s'" >> ~/.zshrc`, execPath), ColorCyan))
		fmt.Println(colorize("source ~/.zshrc", ColorCyan))
	}
}

func printHelp() {
	fmt.Println(`agy-switch (Golang): Fast & lightweight multi-account switcher for Google Antigravity CLI

CORE COMMANDS:
  agy-switch                         Interactive menu to choose and switch accounts
  agy-switch <profile_name>          Instantly switch to the specified profile
  agy-switch next                    Rotate to the next available profile (Round-Robin)
  agy-switch list (ls)               List all saved profiles and token statuses
  agy-switch save [name] (add)       Save current active session as a profile
  agy-switch switch <name> (use)     Switch to specified profile
  agy-switch current (whoami, status)Display active account details
  agy-switch login [name]            Log in to a new Google account and save profile
  agy-switch rename <old> <new> (mv) Rename a saved profile
  agy-switch remove <name> (rm)      Delete a profile
  agy-switch cooldown [clear|<name>] View or reset rate-limit cooldown status

AUTO-SWITCH & FAILOVER:
  agy-switch run [args...]           Smart wrapper: runs agy with auto-switch & --continue resume
  agy-switch watch [start|stop|status]
                                     Log watcher daemon: monitors 429/ResourceExhausted errors,
                                     swaps tokens automatically and sends macOS notifications

UTILITY:
  agy-switch install                 Create a global symlink in /opt/homebrew/bin
  agy-switch help (-h, --help)       Show this help message`)
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		cmdInteractive()
		return
	}

	switch args[0] {
	case "list", "ls":
		cmdList()
	case "save", "add":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		cmdSave(name)
	case "switch", "use":
		if len(args) < 2 {
			fmt.Println(colorize("❌ Please provide profile name: agy-switch switch <name>", ColorRed))
			return
		}
		cmdSwitch(args[1])
	case "next":
		cmdNext()
	case "current", "whoami", "status":
		cmdCurrent()
	case "login":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		cmdLoginNew(name)
	case "rename", "mv":
		if len(args) < 3 {
			fmt.Println(colorize("❌ Please specify both old and new names: agy-switch rename <old> <new>", ColorRed))
			return
		}
		cmdRename(args[1], args[2])
	case "remove", "rm", "delete":
		if len(args) < 2 {
			fmt.Println(colorize("❌ Please provide profile name: agy-switch rm <name>", ColorRed))
			return
		}
		cmdRemove(args[1])
	case "watch":
		cmdWatch(args[1:])
	case "run":
		cmdRun(args[1:])
	case "cooldown":
		cmdCooldown(args[1:])
	case "install":
		cmdInstall()
	case "help", "--help", "-h":
		printHelp()
	default:
		// Shortcut syntax: `agy-switch <profile_name>`
		cmdSwitch(args[0])
	}
}
