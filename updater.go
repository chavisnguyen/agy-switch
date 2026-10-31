package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var Version = "v1.0.4"

const (
	repoOwner = "chavisnguyen"
	repoName  = "agy-switch"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

type updateCache struct {
	LastCheck     int64  `json:"last_check"`
	LatestVersion string `json:"latest_version"`
}

func getUpdateCachePath() string {
	cliHome, _, _, _, _, _, _ := getPaths()
	return filepath.Join(cliHome, ".agys_update.json")
}

func fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "agys-updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// isNewerVersion returns true if remote version string is strictly newer than local
func isNewerVersion(local, remote string) bool {
	l := strings.TrimPrefix(local, "v")
	r := strings.TrimPrefix(remote, "v")
	if l == r {
		return false
	}

	lParts := strings.Split(l, ".")
	rParts := strings.Split(r, ".")

	maxLen := len(lParts)
	if len(rParts) > maxLen {
		maxLen = len(rParts)
	}

	for i := 0; i < maxLen; i++ {
		var lNum, rNum int
		if i < len(lParts) {
			fmt.Sscanf(lParts[i], "%d", &lNum)
		}
		if i < len(rParts) {
			fmt.Sscanf(rParts[i], "%d", &rNum)
		}
		if rNum > lNum {
			return true
		}
		if rNum < lNum {
			return false
		}
	}
	return false
}

func cmdVersion() {
	fmt.Printf("agys version %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
}

func cmdUpdate() {
	fmt.Println(colorize(fmt.Sprintf("==> Checking for updates (current: %s)...", Version), ColorCyan))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		fmt.Printf("%s Could not check for updates: %v\n", colorize("❌", ColorRed), err)
		return
	}

	latest := strings.TrimSpace(rel.TagName)
	if !isNewerVersion(Version, latest) {
		fmt.Printf("%s agys is already up to date (%s).\n", colorize("✓", ColorGreen), Version)
		return
	}

	fmt.Printf("%s New version available: %s -> %s\n", colorize("🌟", ColorYellow), Version, colorize(latest, ColorBold+ColorGreen))

	// Find path of current executable
	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("%s Failed to find executable path: %v\n", colorize("❌", ColorRed), err)
		return
	}
	if realPath, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = realPath
	}

	assetName := fmt.Sprintf("agys-%s-%s", runtime.GOOS, runtime.GOARCH)
	downloadURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", repoOwner, repoName, latest, assetName)

	fmt.Println(colorize(fmt.Sprintf("==> Downloading %s...", assetName), ColorCyan))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		fmt.Printf("%s Failed to create download request: %v\n", colorize("❌", ColorRed), err)
		return
	}
	req.Header.Set("User-Agent", "agys-updater")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("%s Download failed: %v\n", colorize("❌", ColorRed), err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("%s Asset not found (%d). Please check GitHub Release %s.\n", colorize("❌", ColorRed), resp.StatusCode, latest)
		return
	}

	// Write to temporary file in same directory as binary for atomic rename
	targetDir := filepath.Dir(exePath)
	tempFile, err := os.CreateTemp(targetDir, ".agys-new-*")
	if err != nil {
		// If cannot write to target directory (e.g. permission error), try system temp
		tempFile, err = os.CreateTemp("", "agys-new-*")
		if err != nil {
			fmt.Printf("%s Failed to create temp file: %v\n", colorize("❌", ColorRed), err)
			return
		}
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		tempFile.Close()
		fmt.Printf("%s Failed to save downloaded binary: %v\n", colorize("❌", ColorRed), err)
		return
	}
	tempFile.Close()

	if err := os.Chmod(tempPath, 0755); err != nil {
		fmt.Printf("%s Failed to set executable permissions: %v\n", colorize("❌", ColorRed), err)
		return
	}

	// Replace the binary
	// On Unix/macOS, remove existing file or rename over it
	os.Remove(exePath)
	if err := os.Rename(tempPath, exePath); err != nil {
		// If rename fails (e.g. cross-device link), try reading & writing
		in, errCopy := os.Open(tempPath)
		if errCopy == nil {
			defer in.Close()
			out, errOut := os.OpenFile(exePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if errOut == nil {
				_, errCopy = io.Copy(out, in)
				out.Close()
			}
		}
		if errCopy != nil {
			fmt.Printf("%s Permission denied when writing to %s.\n", colorize("❌", ColorRed), exePath)
			fmt.Printf("   Please run with sudo: %s\n", colorize("sudo agys update", ColorCyan))
			return
		}
	}

	if runtime.GOOS == "darwin" {
		_ = exec.Command("codesign", "-s", "-", "--force", exePath).Run()
	}

	// Update local cache
	saveUpdateCache(updateCache{
		LastCheck:     time.Now().Unix(),
		LatestVersion: latest,
	})

	fmt.Println()
	fmt.Printf("%s Successfully updated agys to %s!\n", colorize("🎉", ColorGreen), colorize(latest, ColorBold+ColorGreen))
	fmt.Printf("   Location: %s\n\n", colorize(exePath, ColorCyan))
}

func loadUpdateCache() (updateCache, bool) {
	cachePath := getUpdateCachePath()
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return updateCache{}, false
	}
	var cache updateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return updateCache{}, false
	}
	return cache, true
}

func saveUpdateCache(cache updateCache) {
	cachePath := getUpdateCachePath()
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	_ = os.WriteFile(cachePath, data, 0644)
}

// CheckUpdateNotice checks if an update notice should be printed (non-blocking, fast)
func CheckUpdateNotice() {
	cache, found := loadUpdateCache()
	now := time.Now().Unix()

	// If cached version is newer, print notice
	if found && isNewerVersion(Version, cache.LatestVersion) {
		fmt.Println()
		fmt.Printf("%s A new version of agys is available: %s -> %s\n",
			colorize("💡", ColorYellow),
			Version,
			colorize(cache.LatestVersion, ColorBold+ColorGreen))
		fmt.Printf("   Run %s to upgrade.\n\n", colorize("agys update", ColorCyan))
	}

	// Refresh cache in background if older than 24h
	if !found || (now-cache.LastCheck) > 86400 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rel, err := fetchLatestRelease(ctx)
			if err == nil && rel != nil {
				saveUpdateCache(updateCache{
					LastCheck:     time.Now().Unix(),
					LatestVersion: strings.TrimSpace(rel.TagName),
				})
			}
		}()
	}
}
