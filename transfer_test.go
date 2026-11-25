package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSampleProfile(t *testing.T, path, email, accessTok, refreshTok string) {
	t.Helper()
	tf := OAuthTokenFile{
		Token: TokenDetails{
			AccessToken:  accessTok,
			RefreshToken: refreshTok,
			TokenType:    "Bearer",
		},
		AuthMethod:   "consumer",
		IDTokenEmail: email,
	}
	b, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestExportAndImportRoundTrip(t *testing.T) {
	t.Setenv("AGYS_SKIP_KEYCHAIN", "1")
	homeA := t.TempDir()
	t.Setenv("HOME", homeA)
	if err := ensureDirs(); err != nil {
		t.Fatal(err)
	}
	pathsA := getPaths()
	writeSampleProfile(t, filepath.Join(pathsA.ProfilesDir, "work.json"), "work@example.com", "acc-work", "ref-work")
	writeSampleProfile(t, filepath.Join(pathsA.ProfilesDir, "personal.json"), "me@example.com", "acc-me", "ref-me")
	if err := switchProfileByName("personal"); err != nil {
		t.Fatal(err)
	}

	bundlePath := filepath.Join(t.TempDir(), "accounts.json")
	cmdExport([]string{bundlePath})

	fi, err := os.Stat(bundlePath)
	if err != nil {
		t.Fatalf("expected bundle file to exist: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected bundle permissions 0600, got %o", fi.Mode().Perm())
	}

	// Import onto clean machine B
	homeB := t.TempDir()
	t.Setenv("HOME", homeB)
	cmdImport([]string{bundlePath})

	profilesB, err := getAllProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profilesB) != 2 {
		t.Fatalf("expected 2 profiles on machine B, got %d", len(profilesB))
	}
	if got := currentActiveProfileName(); got != "personal" {
		t.Errorf("expected active profile 'personal' on machine B, got %q", got)
	}
}

func TestImportRejectsPathTraversalAndCorruptToken(t *testing.T) {
	t.Setenv("AGYS_SKIP_KEYCHAIN", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)

	bundlePath := filepath.Join(t.TempDir(), "malicious.json")
	malicious := `{
  "version": 1,
  "exported_at": "2026-11-25T00:00:00Z",
  "profiles": [
    {"name": "../../../escape", "token_file": {"token":{"access_token":"x"}}},
    {"name": "bad_json", "token_file": "not-an-object"}
  ]
}`
	if err := os.WriteFile(bundlePath, []byte(malicious), 0600); err != nil {
		t.Fatal(err)
	}

	cmdImport([]string{"--overwrite", bundlePath})

	profiles, _ := getAllProfiles()
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles imported from malicious bundle, got %d", len(profiles))
	}
	escaped := filepath.Join(home, "escape.json")
	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Fatalf("path traversal escaped profiles directory: %s", escaped)
	}
}

func TestParseTransferArgs(t *testing.T) {
	p, force, unk := parseTransferArgs([]string{"--overwrite", "my.json"})
	if p != "my.json" || !force || unk != "" {
		t.Errorf("unexpected parse result: p=%q force=%v unk=%q", p, force, unk)
	}
	p, force, unk = parseTransferArgs([]string{"out.json", "-f"})
	if p != "out.json" || !force || unk != "" {
		t.Errorf("unexpected parse result: p=%q force=%v unk=%q", p, force, unk)
	}
	_, _, unk = parseTransferArgs([]string{"--bad-flag"})
	if !strings.Contains(unk, "--bad-flag") {
		t.Errorf("expected unknown flag error, got %q", unk)
	}
}
