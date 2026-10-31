package main

import (
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
	"strings"
	"time"
)

// Data Structures
type OAuthTokenFile struct {
	Token        TokenDetails `json:"token"`
	AuthMethod   string       `json:"auth_method"`
	IDToken      string       `json:"id_token,omitempty"`
	IDTokenEmail string       `json:"id_token_email,omitempty"`
}

type TokenDetails struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	Expiry       string `json:"expiry"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
}

type JWTPayload struct {
	Email string  `json:"email"`
	Name  string  `json:"name"`
	Sub   string  `json:"sub"`
	Exp   float64 `json:"exp"`
}

type ProfileInfo struct {
	ProfileName    string
	Email          string
	Name           string
	Sub            string
	RefreshToken   string
	StatusToken    string
	IsValid        bool
	IsActive       bool
	IsCoolingDown  bool
	CooldownRemain string
	FilePath       string
	Hash           string
}

// System Paths
func getPaths() (cliHome, tokenPath, profilesDir, currentFile, backupPath, cooldownFile, pidFile string) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	cliHome = filepath.Join(home, ".gemini", "antigravity-cli")
	tokenPath = filepath.Join(cliHome, "antigravity-oauth-token")
	profilesDir = filepath.Join(cliHome, "profiles")
	currentFile = filepath.Join(profilesDir, ".current")
	backupPath = filepath.Join(cliHome, "antigravity-oauth-token.bak")
	cooldownFile = filepath.Join(profilesDir, ".cooldown.json")
	pidFile = filepath.Join(profilesDir, ".watch.pid")
	return
}

func ensureDirs() error {
	_, _, profilesDir, _, _, _, _ := getPaths()
	if err := os.MkdirAll(profilesDir, 0700); err != nil {
		return err
	}
	return os.Chmod(profilesDir, 0700)
}

// JWT & Token Parsing
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
	if (email == "Unknown email" || email == "") && tokenFile.IDTokenEmail != "" {
		email = tokenFile.IDTokenEmail
		if sub == "" {
			sub = tokenFile.IDTokenEmail
		}
	}

	statusToken := "Unknown"
	isValid := false

	if tokenFile.Token.RefreshToken != "" {
		statusToken = "Valid"
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

// Cooldown Management
func loadCooldowns() map[string]int64 {
	_, _, _, _, _, cooldownFile, _ := getPaths()
	result := make(map[string]int64)

	data, err := os.ReadFile(cooldownFile)
	if err != nil {
		return result
	}

	_ = json.Unmarshal(data, &result)

	// Clean expired cooldowns
	now := time.Now().Unix()
	cleaned := make(map[string]int64)
	changed := false
	for k, exp := range result {
		if exp > now {
			cleaned[k] = exp
		} else {
			changed = true
		}
	}
	if changed {
		saveCooldowns(cleaned)
	}
	return cleaned
}

func saveCooldowns(m map[string]int64) {
	_, _, _, _, _, cooldownFile, _ := getPaths()
	data, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(cooldownFile, data, 0600)
}

func setProfileCooldown(profileName string, duration time.Duration) {
	cooldowns := loadCooldowns()
	cooldowns[profileName] = time.Now().Add(duration).Unix()
	saveCooldowns(cooldowns)
}

func clearProfileCooldown(profileName string) {
	cooldowns := loadCooldowns()
	delete(cooldowns, profileName)
	saveCooldowns(cooldowns)
}

func clearAllCooldowns() {
	_, _, _, _, _, cooldownFile, _ := getPaths()
	_ = os.Remove(cooldownFile)
}

// Profiles Queries
func getAllProfiles() ([]ProfileInfo, error) {
	if err := ensureDirs(); err != nil {
		return nil, err
	}

	_, tokenPath, profilesDir, currentFile, _, _, _ := getPaths()
	activeInfo, _ := readTokenInfo(tokenPath)
	cooldowns := loadCooldowns()

	currentSaved := ""
	if b, err := os.ReadFile(currentFile); err == nil {
		currentSaved = strings.TrimSpace(string(b))
	}

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil, err
	}

	var profiles []ProfileInfo
	now := time.Now().Unix()

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		pName := strings.TrimSuffix(entry.Name(), ".json")
		fullPath := filepath.Join(profilesDir, entry.Name())
		info, err := readTokenInfo(fullPath)
		if err != nil {
			continue
		}

		info.ProfileName = pName

		// Cooldown check
		if exp, exists := cooldowns[pName]; exists && exp > now {
			rem := time.Duration(exp-now) * time.Second
			info.IsCoolingDown = true
			info.CooldownRemain = fmt.Sprintf("%dm left", int(rem.Minutes()))
		}

		profiles = append(profiles, *info)
	}

	// Match active profile based on actual active credentials first
	activeIdx := -1
	if activeInfo != nil {
		// Priority 1: Exact file hash match
		for idx, p := range profiles {
			if p.Hash != "" && p.Hash == activeInfo.Hash {
				if activeIdx == -1 || p.ProfileName == currentSaved {
					activeIdx = idx
				}
			}
		}
		// Priority 2: Refresh token match
		if activeIdx == -1 && activeInfo.RefreshToken != "" {
			for idx, p := range profiles {
				if p.RefreshToken != "" && p.RefreshToken == activeInfo.RefreshToken {
					if activeIdx == -1 || p.ProfileName == currentSaved {
						activeIdx = idx
					}
				}
			}
		}
		// Priority 3: Subject ID match
		if activeIdx == -1 && activeInfo.Sub != "" {
			for idx, p := range profiles {
				if p.Sub != "" && p.Sub == activeInfo.Sub {
					if activeIdx == -1 || p.ProfileName == currentSaved {
						activeIdx = idx
					}
				}
			}
		}
		// Priority 4: Email match
		if activeIdx == -1 && activeInfo.Email != "" && activeInfo.Email != "Unknown email" {
			for idx, p := range profiles {
				if strings.EqualFold(p.Email, activeInfo.Email) {
					if activeIdx == -1 || p.ProfileName == currentSaved {
						activeIdx = idx
					}
				}
			}
		}
	}

	// Fallback to currentSaved if active token couldn't be inspected
	if activeIdx == -1 && currentSaved != "" {
		for idx, p := range profiles {
			if p.ProfileName == currentSaved {
				activeIdx = idx
				break
			}
		}
	}

	if activeIdx >= 0 && activeIdx < len(profiles) {
		profiles[activeIdx].IsActive = true
		if profiles[activeIdx].ProfileName != currentSaved {
			_ = os.WriteFile(currentFile, []byte(profiles[activeIdx].ProfileName), 0600)
		}
	}

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].ProfileName < profiles[j].ProfileName
	})

	return profiles, nil
}

// Round-Robin Next Profile Switch
func switchNextProfile(reason string) (string, error) {
	profiles, err := getAllProfiles()
	if err != nil {
		return "", err
	}
	if len(profiles) == 0 {
		return "", fmt.Errorf("no profiles found to switch")
	}
	if len(profiles) == 1 {
		return "", fmt.Errorf("only 1 profile available; cannot switch")
	}

	// Find current active index
	activeIdx := -1
	for idx, p := range profiles {
		if p.IsActive {
			activeIdx = idx
			break
		}
	}

	// Round-robin selection: pick next non-cooldown profile
	startIdx := 0
	if activeIdx >= 0 {
		startIdx = activeIdx
	}

	var selected string
	for offset := 1; offset <= len(profiles); offset++ {
		candidateIdx := (startIdx + offset) % len(profiles)
		candidate := profiles[candidateIdx]

		if activeIdx >= 0 && candidateIdx == activeIdx {
			continue
		}

		if !candidate.IsCoolingDown {
			selected = candidate.ProfileName
			break
		}
	}

	// If all other profiles are in cooldown, pick the one with earliest cooldown expiry
	if selected == "" {
		cooldowns := loadCooldowns()
		earliestName := ""
		var earliestExp int64 = 0

		for _, p := range profiles {
			if p.IsActive {
				continue
			}
			exp := cooldowns[p.ProfileName]
			if earliestExp == 0 || exp < earliestExp {
				earliestExp = exp
				earliestName = p.ProfileName
			}
		}

		if earliestName != "" {
			rem := time.Until(time.Unix(earliestExp, 0))
			return "", fmt.Errorf("all alternative profiles are cooling down (earliest reset: %s in %dm)", earliestName, int(rem.Minutes()))
		}
		return "", fmt.Errorf("all profiles are currently in cooldown")
	}

	if err := switchProfileByName(selected); err != nil {
		return "", err
	}

	return selected, nil
}

func getKeychainPassword() string {
	out, err := exec.Command("security", "find-generic-password", "-s", "gemini", "-a", "antigravity", "-w").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func parseKeychainToken(rawKeychain string) (*OAuthTokenFile, string, error) {
	if !strings.HasPrefix(rawKeychain, "go-keyring-base64:") {
		return nil, "", fmt.Errorf("unknown keyring format")
	}
	encoded := strings.TrimPrefix(rawKeychain, "go-keyring-base64:")
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", err
	}

	type KeyringPayload struct {
		Token        TokenDetails `json:"token"`
		AuthMethod   string       `json:"auth_method"`
		IDTokenEmail string       `json:"id_token_email"`
	}

	var p KeyringPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, "", err
	}

	tokFile := OAuthTokenFile{
		Token:        p.Token,
		AuthMethod:   p.AuthMethod,
		IDTokenEmail: p.IDTokenEmail,
	}
	return &tokFile, p.IDTokenEmail, nil
}

func setKeychainPassword(pw string) error {
	if pw == "" {
		return nil
	}
	cmd := exec.Command("security", "add-generic-password", "-U", "-s", "gemini", "-a", "antigravity", "-w", pw)
	return cmd.Run()
}

func deleteKeychainPassword() error {
	cmd := exec.Command("security", "delete-generic-password", "-s", "gemini", "-a", "antigravity")
	return cmd.Run()
}

func syncKeychainFromTokenFile(tokenFile OAuthTokenFile, email string) error {
	type KeyringPayload struct {
		Token        TokenDetails `json:"token"`
		AuthMethod   string       `json:"auth_method"`
		IDTokenEmail string       `json:"id_token_email"`
	}

	authMethod := tokenFile.AuthMethod
	if authMethod == "" {
		authMethod = "consumer"
	}

	payload := KeyringPayload{
		Token:        tokenFile.Token,
		AuthMethod:   authMethod,
		IDTokenEmail: email,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	encoded := "go-keyring-base64:" + base64.StdEncoding.EncodeToString(data)
	return setKeychainPassword(encoded)
}

func switchProfileByName(name string) error {
	if err := ensureDirs(); err != nil {
		return err
	}

	name = sanitizeName(name)
	_, tokenPath, profilesDir, currentFile, backupPath, _, _ := getPaths()
	targetPath := filepath.Join(profilesDir, name+".json")

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return fmt.Errorf("profile '%s' not found", name)
	}

	// 1. Sync currently active token back to previous profile's file to preserve refreshed access tokens
	if curData, err := os.ReadFile(currentFile); err == nil {
		prevName := strings.TrimSpace(string(curData))
		if prevName != "" && prevName != name {
			prevPath := filepath.Join(profilesDir, prevName+".json")
			if _, err := os.Stat(prevPath); err == nil {
				if tokInfo, err := readTokenInfo(tokenPath); err == nil && tokInfo != nil && tokInfo.RefreshToken != "" {
					_ = copyFile(tokenPath, prevPath, 0600)
				}
			}
		}
	}

	// 2. Backup current token
	if _, err := os.Stat(tokenPath); err == nil {
		_ = copyFile(tokenPath, backupPath, 0600)
	}

	// Read & atomic write
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return fmt.Errorf("failed to read profile: %w", err)
	}

	if err := atomicWriteFile(tokenPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write active token: %w", err)
	}
	_ = os.WriteFile(currentFile, []byte(name), 0600)

	// Sync with macOS Keychain so both CLI & IDE stay in sync
	var tokFile OAuthTokenFile
	if err := json.Unmarshal(data, &tokFile); err == nil {
		info, _ := readTokenInfo(targetPath)
		email := ""
		if info != nil {
			email = info.Email
		}
		_ = syncKeychainFromTokenFile(tokFile, email)
	}

	return nil
}
