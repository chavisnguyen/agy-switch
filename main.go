package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

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

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

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

// OAuth Token Data Structure
type OAuthTokenFile struct {
	Token      TokenDetails `json:"token"`
	AuthMethod string       `json:"auth_method"`
	IDToken    string       `json:"id_token"`
}

type TokenDetails struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	Expiry       string `json:"expiry"`
}

type JWTPayload struct {
	Email string  `json:"email"`
	Name  string  `json:"name"`
	Sub   string  `json:"sub"`
	Exp   float64 `json:"exp"`
}

type ProfileInfo struct {
	ProfileName  string
	Email        string
	Name         string
	Sub          string
	RefreshToken string
	StatusToken  string
	IsValid      bool
	IsActive     bool
	FilePath     string
	Hash         string
}

// Paths
func getPaths() (cliHome, tokenPath, profilesDir, currentFile, backupPath string) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	cliHome = filepath.Join(home, ".gemini", "antigravity-cli")
	tokenPath = filepath.Join(cliHome, "antigravity-oauth-token")
	profilesDir = filepath.Join(cliHome, "profiles")
	currentFile = filepath.Join(profilesDir, ".current")
	backupPath = filepath.Join(cliHome, "antigravity-oauth-token.bak")
	return
}

func ensureDirs() error {
	_, _, profilesDir, _, _ := getPaths()
	if err := os.MkdirAll(profilesDir, 0700); err != nil {
		return err
	}
	return os.Chmod(profilesDir, 0700)
}

func decodeJWTPayload(idToken string) (*JWTPayload, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	segment := parts[1]
	if l := len(segment) % 4; l > 0 {
		segment += strings.Repeat("=", 4-l)
	}

	data, err := base64.URLEncoding.DecodeString(segment)
	if err != nil {
		data, err = base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, err
		}
	}

	var payload JWTPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func getFileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func atomicWriteFile(dst string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, "token_tmp_*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

func readTokenInfo(filePath string) (*ProfileInfo, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var tokenFile OAuthTokenFile
	if err := json.Unmarshal(raw, &tokenFile); err != nil {
		return nil, err
	}

	email := "Unknown email"
	name := "N/A"
	sub := ""
	var expSec int64 = 0

	if tokenFile.IDToken != "" {
		if jwt, err := decodeJWTPayload(tokenFile.IDToken); err == nil {
			if jwt.Email != "" {
				email = jwt.Email
			}
			if jwt.Name != "" {
				name = jwt.Name
			}
			sub = jwt.Sub
			expSec = int64(jwt.Exp)
		}
	}

	statusToken := "Unknown"
	isValid := false

	if tokenFile.Token.RefreshToken != "" {
		statusToken = "Valid (Auto-refresh)"
		isValid = true
	} else if expSec > 0 {
		expTime := time.Unix(expSec, 0)
		now := time.Now()
		if expTime.After(now) {
			diff := expTime.Sub(now)
			statusToken = fmt.Sprintf("%dh left", int(diff.Hours()))
			isValid = true
		} else {
			statusToken = "Expired"
			isValid = false
		}
	}

	hash, _ := getFileSHA256(filePath)

	return &ProfileInfo{
		Email:        email,
		Name:         name,
		Sub:          sub,
		RefreshToken: tokenFile.Token.RefreshToken,
		StatusToken:  statusToken,
		IsValid:      isValid,
		FilePath:     filePath,
		Hash:         hash,
	}, nil
}

func sanitizeName(name string) string {
	reg := regexp.MustCompile(`[^a-zA-Z0-9_\-]`)
	return strings.TrimSpace(reg.ReplaceAllString(name, ""))
}

func getAllProfiles() ([]ProfileInfo, error) {
	if err := ensureDirs(); err != nil {
		return nil, err
	}

	_, tokenPath, profilesDir, currentFile, _ := getPaths()
	activeInfo, _ := readTokenInfo(tokenPath)

	currentSaved := ""
	if b, err := os.ReadFile(currentFile); err == nil {
		currentSaved = strings.TrimSpace(string(b))
	}

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil, err
	}

	var profiles []ProfileInfo
	matchedOne := false

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		pName := strings.TrimSuffix(entry.Name(), ".json")
		fullPath := filepath.Join(profilesDir, entry.Name())
		info, err := readTokenInfo(fullPath)
		if err != nil {
			continue
		}

		info.ProfileName = pName
		isActive := false

		if activeInfo != nil {
			if currentSaved != "" && pName == currentSaved {
				isActive = true
				matchedOne = true
			} else if currentSaved == "" && !matchedOne {
				if info.Hash != "" && info.Hash == activeInfo.Hash {
					isActive = true
					matchedOne = true
				} else if info.RefreshToken != "" && info.RefreshToken == activeInfo.RefreshToken {
					isActive = true
					matchedOne = true
				} else if info.Sub != "" && info.Sub == activeInfo.Sub {
					isActive = true
					matchedOne = true
				}
			}
		}

		info.IsActive = isActive
		profiles = append(profiles, *info)
	}

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].ProfileName < profiles[j].ProfileName
	})

	return profiles, nil
}

// ==================== CLI COMMANDS ====================

func cmdList() {
	_, tokenPath, _, _, _ := getPaths()
	profiles, _ := getAllProfiles()
	activeInfo, _ := readTokenInfo(tokenPath)

	fmt.Println()
	fmt.Println(colorize("📋 AGY ACCOUNTS & PROFILES", ColorBold+ColorCyan))
	fmt.Println(colorize(strings.Repeat("═", 74), ColorDim))

	if len(profiles) == 0 {
		fmt.Println(colorize("No profiles saved yet.", ColorYellow))
		fmt.Printf("Tip: Run %s to save your active account.\n\n", colorize("agy-switch save <name>", ColorGreen))
		return
	}

	header := fmt.Sprintf("%-4s %-16s %-26s %-16s %s", "ID", "PROFILE", "EMAIL", "STATUS", "TOKEN")
	fmt.Println(header)
	fmt.Println(colorize(strings.Repeat("─", 74), ColorDim))

	anyActive := false
	for idx, p := range profiles {
		status := colorize("○ Ready", ColorDim)
		if p.IsActive {
			status = colorize("● ACTIVE", ColorBold+ColorGreen)
			anyActive = true
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
			padRight(pName, 16),
			padRight(p.Email, 26),
			padRight(status, 16),
			ver,
		)
		fmt.Println(line)
	}

	fmt.Println(colorize(strings.Repeat("─", 74), ColorDim))

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

	_, tokenPath, profilesDir, currentFile, _ := getPaths()
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
	if err := ensureDirs(); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	name = sanitizeName(name)
	_, tokenPath, profilesDir, currentFile, backupPath := getPaths()
	targetPath := filepath.Join(profilesDir, name+".json")

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		fmt.Println(colorize(fmt.Sprintf("❌ Profile not found: '%s'", name), ColorRed))
		cmdList()
		return
	}

	activeHash, _ := getFileSHA256(tokenPath)
	targetHash, _ := getFileSHA256(targetPath)
	if activeHash != "" && activeHash == targetHash {
		fmt.Println(colorize(fmt.Sprintf("ℹ️  Profile '%s' is ALREADY active. No switch needed.", name), ColorYellow))
		return
	}

	// Backup current active token
	if _, err := os.Stat(tokenPath); err == nil {
		_ = copyFile(tokenPath, backupPath, 0600)
	}

	// Read and atomic write
	data, err := os.ReadFile(targetPath)
	if err != nil {
		fmt.Printf(colorize("❌ Error reading profile: %v\n", ColorRed), err)
		return
	}

	if err := atomicWriteFile(tokenPath, data, 0600); err != nil {
		fmt.Printf(colorize("❌ Error writing active token: %v\n", ColorRed), err)
		return
	}
	_ = os.WriteFile(currentFile, []byte(name), 0600)

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
	fmt.Println(colorize(fmt.Sprintf("🚀 SWITCHED TO PROFILE: %s", strings.ToUpper(name)), ColorBold+ColorGreen))
	fmt.Println(colorize(strings.Repeat("═", 50), ColorDim))
	fmt.Printf("   Email : %s\n", colorize(email, ColorBold+ColorCyan))
	fmt.Printf("   Name  : %s\n", nameUser)
	fmt.Printf("   Token : %s\n", statusTok)
	fmt.Println(colorize(strings.Repeat("═", 50), ColorDim))
	fmt.Println(colorize("👉 You can now run `agy` with this account!", ColorGreen))
	fmt.Println()
}

func cmdCurrent() {
	_, tokenPath, _, _, _ := getPaths()
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

	cliHome, tokenPath, profilesDir, _, _ := getPaths()
	hadActive := false
	tempActiveBak := filepath.Join(cliHome, fmt.Sprintf("active_before_new_%d", time.Now().Unix()))

	if _, err := os.Stat(tokenPath); err == nil {
		_ = copyFile(tokenPath, tempActiveBak, 0600)
		hadActive = true
		_ = os.Remove(tokenPath)
	}

	fmt.Println()
	fmt.Println(colorize("🔑 LOG IN TO A NEW ACCOUNT", ColorBold+ColorCyan))
	fmt.Println("Launching `agy` to start Google browser login.")
	fmt.Printf("Once browser authentication finishes, type %s inside agy to complete.\n\n", colorize("/exit", ColorYellow))
	fmt.Print("Press [Enter] to launch agy...")
	_, _ = reader.ReadString('\n')

	cmd := exec.Command("agy")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Println(colorize("⚠️  agy process finished or was interrupted.", ColorYellow))
	}

	if _, err := os.Stat(tokenPath); err == nil {
		newInfo, _ := readTokenInfo(tokenPath)
		targetPath := filepath.Join(profilesDir, name+".json")
		_ = copyFile(tokenPath, targetPath, 0600)

		email := "N/A"
		if newInfo != nil {
			email = newInfo.Email
		}
		fmt.Println()
		fmt.Println(colorize(fmt.Sprintf("🎉 Successfully logged in and saved profile '%s'!", name), ColorBold+ColorGreen))
		fmt.Printf("   Email: %s\n", colorize(email, ColorCyan))
		if hadActive {
			_ = os.Remove(tempActiveBak)
		}
	} else {
		fmt.Println(colorize("❌ Login was not completed or failed.", ColorRed))
		if hadActive {
			fmt.Println("Restoring previous active account...")
			_ = copyFile(tempActiveBak, tokenPath, 0600)
			_ = os.Remove(tempActiveBak)
		}
	}
}

func cmdRemove(name string) {
	name = sanitizeName(name)
	_, _, profilesDir, _, _ := getPaths()
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
		fmt.Println(colorize(fmt.Sprintf("🗑️  Deleted profile '%s'.", name), ColorGreen))
	} else {
		fmt.Println("Cancelled.")
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

USAGE:
  agy-switch                         Interactive menu to choose and switch accounts
  agy-switch <profile_name>          Instantly switch to the specified profile
  agy-switch list (ls)               List all saved profiles and token statuses
  agy-switch save [name] (add)       Save current active session as a profile
  agy-switch switch <name> (use)     Switch to specified profile
  agy-switch current (whoami, status)Display active account details
  agy-switch login [name]            Log in to a new Google account and save profile
  agy-switch remove <name> (rm)      Delete a profile
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
	case "current", "whoami", "status":
		cmdCurrent()
	case "login":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		cmdLoginNew(name)
	case "remove", "rm", "delete":
		if len(args) < 2 {
			fmt.Println(colorize("❌ Please provide profile name: agy-switch rm <name>", ColorRed))
			return
		}
		cmdRemove(args[1])
	case "install":
		cmdInstall()
	case "help", "--help", "-h":
		printHelp()
	default:
		// Shortcut syntax: `agy-switch <profile_name>`
		cmdSwitch(args[0])
	}
}
