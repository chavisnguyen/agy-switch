package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Data Structures
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
	matchedOne := false
	now := time.Now().Unix()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == ".cooldown.json" {
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

		// Cooldown check
		if exp, exists := cooldowns[pName]; exists && exp > now {
			rem := time.Duration(exp-now) * time.Second
			info.IsCoolingDown = true
			info.CooldownRemain = fmt.Sprintf("%dm left", int(rem.Minutes()))
		}

		info.IsActive = isActive
		profiles = append(profiles, *info)
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
	var selected string
	for offset := 1; offset <= len(profiles); offset++ {
		candidateIdx := (activeIdx + offset) % len(profiles)
		candidate := profiles[candidateIdx]

		if candidateIdx == activeIdx {
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

	// Backup current token
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
	return nil
}
