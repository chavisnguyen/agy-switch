package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultExportFilename = "agys-accounts.json"

type ExportedProfile struct {
	Name  string          `json:"name"`
	Email string          `json:"email,omitempty"`
	Raw   json.RawMessage `json:"token_file"`
}

type ExportBundle struct {
	Version       int               `json:"version"`
	ExportedAt    string            `json:"exported_at"`
	ActiveProfile string            `json:"active_profile,omitempty"`
	Profiles      []ExportedProfile `json:"profiles"`
}

func expandUserPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func parseTransferArgs(args []string) (filePath string, overwrite bool, unknownFlag string) {
	for _, a := range args {
		switch a {
		case "-f", "--force", "--overwrite":
			overwrite = true
		default:
			if strings.HasPrefix(a, "-") {
				if unknownFlag == "" {
					unknownFlag = a
				}
				continue
			}
			if filePath == "" {
				filePath = expandUserPath(a)
			}
		}
	}
	return filePath, overwrite, unknownFlag
}

// syncActiveTokenToSavedProfile copies the live active token back into its matching
// profile file on disk so any access_token refresh done by agy is captured before export.
func syncActiveTokenToSavedProfile() {
	paths := getPaths()
	curData, err := os.ReadFile(paths.CurrentFile)
	if err != nil {
		return
	}
	activeName := strings.TrimSpace(string(curData))
	if activeName == "" {
		return
	}
	profPath := filepath.Join(paths.ProfilesDir, activeName+".json")
	if _, err := os.Stat(profPath); err != nil {
		return
	}
	if tokInfo, err := readTokenInfo(paths.TokenPath); err == nil && tokInfo != nil && tokInfo.RefreshToken != "" {
		_ = copyFile(paths.TokenPath, profPath, 0600)
	}
}

func cmdExport(args []string) {
	outPath, force, unknownFlag := parseTransferArgs(args)
	if unknownFlag != "" {
		fmt.Println(colorize(fmt.Sprintf("❌ Unknown flag: %s (Usage: agys export [file] [--overwrite])", unknownFlag), ColorRed))
		return
	}
	if outPath == "" {
		outPath = defaultExportFilename
	}

	syncActiveTokenToSavedProfile()

	profiles, err := getAllProfiles()
	if err != nil {
		fmt.Printf(colorize("❌ Error reading profiles: %v\n", ColorRed), err)
		return
	}
	if len(profiles) == 0 {
		fmt.Println(colorize("❌ No saved profiles found to export.", ColorYellow))
		fmt.Printf("   Tip: Run %s first to save your active account.\n", colorize("agys save <name>", ColorGreen))
		return
	}

	if !force {
		if _, err := os.Stat(outPath); err == nil {
			fmt.Printf("File '%s' already exists. Overwrite? [y/N]: ", outPath)
			reader := bufio.NewReader(os.Stdin)
			confirm, _ := reader.ReadString('\n')
			ans := strings.ToLower(strings.TrimSpace(confirm))
			if ans != "y" && ans != "yes" {
				fmt.Println("Cancelled.")
				return
			}
		}
	}

	bundle, err := buildExportBundle(profiles)
	if err != nil {
		fmt.Printf(colorize("❌ Failed to prepare export bundle: %v\n", ColorRed), err)
		return
	}

	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fmt.Printf(colorize("❌ Failed to encode export bundle: %v\n", ColorRed), err)
		return
	}
	data = append(data, '\n')

	outDir := filepath.Dir(outPath)
	if outDir != "" && outDir != "." {
		if err := os.MkdirAll(outDir, 0700); err != nil {
			fmt.Printf(colorize("❌ Failed to create output directory: %v\n", ColorRed), err)
			return
		}
	}

	if err := atomicWriteFile(outPath, data, 0600); err != nil {
		fmt.Printf(colorize("❌ Failed to write export file: %v\n", ColorRed), err)
		return
	}

	absOut := outPath
	if abs, err := filepath.Abs(outPath); err == nil {
		absOut = abs
	}

	fmt.Println()
	fmt.Println(colorize(fmt.Sprintf("📦 Exported %d account(s) successfully!", len(bundle.Profiles)), ColorBold+ColorGreen))
	fmt.Printf("   File   : %s\n", colorize(absOut, ColorCyan))
	if bundle.ActiveProfile != "" {
		fmt.Printf("   Active : %s\n", bundle.ActiveProfile)
	}
	fmt.Printf("   Notice : %s\n", colorize("This file contains raw OAuth tokens (mode 0600). Delete it after importing on your other machine.", ColorYellow))
	fmt.Println()
}

func buildExportBundle(profiles []ProfileInfo) (*ExportBundle, error) {
	bundle := &ExportBundle{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Profiles:   make([]ExportedProfile, 0, len(profiles)),
	}

	for _, p := range profiles {
		raw, err := os.ReadFile(p.FilePath)
		if err != nil {
			return nil, fmt.Errorf("reading profile '%s': %w", p.ProfileName, err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			return nil, fmt.Errorf("invalid JSON in profile '%s': %w", p.ProfileName, err)
		}

		email := p.Email
		if email == "Unknown email" {
			email = ""
		}
		bundle.Profiles = append(bundle.Profiles, ExportedProfile{
			Name:  p.ProfileName,
			Email: email,
			Raw:   json.RawMessage(compact.Bytes()),
		})
		if p.IsActive {
			bundle.ActiveProfile = p.ProfileName
		}
	}

	return bundle, nil
}

func cmdImport(args []string) {
	inPath, overwriteAll, unknownFlag := parseTransferArgs(args)
	if unknownFlag != "" {
		fmt.Println(colorize(fmt.Sprintf("❌ Unknown flag: %s (Usage: agys import <file> [--overwrite])", unknownFlag), ColorRed))
		return
	}
	if inPath == "" {
		fmt.Println(colorize("❌ Please specify the bundle file to import: agys import <file> [--overwrite]", ColorRed))
		return
	}

	raw, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Printf(colorize("❌ Cannot read import file '%s': %v\n", ColorRed), inPath, err)
		return
	}

	var bundle ExportBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		fmt.Printf(colorize("❌ Invalid bundle JSON: %v\n", ColorRed), err)
		return
	}
	if len(bundle.Profiles) == 0 {
		fmt.Println(colorize("❌ Bundle contains no profiles to import.", ColorRed))
		return
	}

	if err := ensureDirs(); err != nil {
		fmt.Printf(colorize("❌ Error preparing profiles directory: %v\n", ColorRed), err)
		return
	}

	paths := getPaths()
	hadActiveBefore := false
	if info, err := readTokenInfo(paths.TokenPath); err == nil && info != nil {
		hadActiveBefore = true
	}
	prevActiveName := currentActiveProfileName()

	reader := bufio.NewReader(os.Stdin)
	added := 0
	overwritten := 0
	skipped := 0
	overwrotePrevActive := false
	var importedNames []string

	for _, item := range bundle.Profiles {
		safeName := sanitizeName(item.Name)
		if safeName == "" || safeName != item.Name {
			fmt.Printf(colorize("⚠️  Skipping invalid profile name %q.\n", ColorYellow), item.Name)
			skipped++
			continue
		}

		var tokCheck OAuthTokenFile
		if len(item.Raw) == 0 || json.Unmarshal(item.Raw, &tokCheck) != nil {
			fmt.Printf(colorize("⚠️  Skipping profile '%s': invalid token JSON.\n", ColorYellow), safeName)
			skipped++
			continue
		}

		var pretty bytes.Buffer
		if err := json.Indent(&pretty, item.Raw, "", "  "); err != nil {
			skipped++
			continue
		}
		pretty.WriteByte('\n')

		targetPath := filepath.Join(paths.ProfilesDir, safeName+".json")
		_, statErr := os.Stat(targetPath)
		exists := (statErr == nil)

		if exists && !overwriteAll {
			label := safeName
			if item.Email != "" {
				label = fmt.Sprintf("%s (%s)", safeName, item.Email)
			}
			fmt.Printf("Profile '%s' already exists. Overwrite? [y/N/a=all]: ", label)
			ansRaw, _ := reader.ReadString('\n')
			ans := strings.ToLower(strings.TrimSpace(ansRaw))
			if ans == "a" || ans == "all" {
				overwriteAll = true
			} else if ans != "y" && ans != "yes" {
				skipped++
				continue
			}
		}

		if err := atomicWriteFile(targetPath, pretty.Bytes(), 0600); err != nil {
			fmt.Printf(colorize("❌ Failed writing profile '%s': %v\n", ColorRed), safeName, err)
			skipped++
			continue
		}

		importedNames = append(importedNames, safeName)
		if exists {
			overwritten++
			if prevActiveName != "" && strings.EqualFold(prevActiveName, safeName) {
				overwrotePrevActive = true
			}
		} else {
			added++
		}
	}

	// Activate an account if this machine had none yet, or refresh live credentials if
	// the already-active profile was overwritten during import.
	activated := ""
	if !hadActiveBefore && len(importedNames) > 0 {
		candidate := sanitizeName(bundle.ActiveProfile)
		if candidate != "" {
			if _, err := os.Stat(filepath.Join(paths.ProfilesDir, candidate+".json")); err != nil {
				candidate = ""
			}
		}
		if candidate == "" {
			candidate = importedNames[0]
		}
		if err := switchProfileByName(candidate); err == nil {
			activated = candidate
		}
	} else if overwrotePrevActive && prevActiveName != "" {
		if err := switchProfileByName(prevActiveName); err == nil {
			activated = prevActiveName
		}
	}

	fmt.Println()
	fmt.Println(colorize("✅ Import complete!", ColorBold+ColorGreen))
	fmt.Printf("   Added       : %d\n", added)
	fmt.Printf("   Overwritten : %d\n", overwritten)
	fmt.Printf("   Skipped     : %d\n", skipped)
	if activated != "" {
		fmt.Printf("   Active      : %s\n", colorize(activated, ColorBold+ColorCyan))
	} else if cur := currentActiveProfileName(); cur != "" {
		fmt.Printf("   Active      : %s (unchanged)\n", colorize(cur, ColorCyan))
	}
	fmt.Println()
}
