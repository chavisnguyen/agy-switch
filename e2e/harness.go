package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// AgyTurn defines how the mock agy binary behaves on a particular execution turn.
type AgyTurn struct {
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	CliLog   string        `json:"cli_log"`
	ExitCode int           `json:"exit_code"`
	Delay    time.Duration `json:"delay"`
}

// AgyExecRecord records how mock agy was invoked by agys.
type AgyExecRecord struct {
	TurnIndex   int      `json:"turn_index"`
	Args        []string `json:"args"`
	ActiveToken string   `json:"active_token"`
	Timestamp   string   `json:"timestamp"`
}

type Harness struct {
	T           *testing.T
	RootDir     string // Project root where agys binary is located
	TempDir     string // Isolated sandbox HOME
	BinDir      string // Directory on PATH containing mock agy and mock security
	ProfilesDir string // $HOME/.gemini/antigravity-cli/profiles
	TokenPath   string // $HOME/.gemini/antigravity-cli/antigravity-oauth-token
	CliLogPath  string // $HOME/.gemini/antigravity-cli/cli.log
	AgysBin     string // Full path to agys executable
	StateFile   string // Path to mock agy turn state
	ExecLogFile string // Path to mock agy execution log
	mu          sync.Mutex
}

// NewHarness initializes a clean, fully isolated black-box testing sandbox.
func NewHarness(t *testing.T) *Harness {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	// If running inside e2e subdirectory, RootDir is parent
	rootDir := wd
	if filepath.Base(wd) == "e2e" {
		rootDir = filepath.Dir(wd)
	}

	agysBin := filepath.Join(rootDir, "agys")
	if _, err := os.Stat(agysBin); os.IsNotExist(err) {
		// Build agys binary first
		cmd := exec.Command("go", "build", "-o", "agys", ".")
		cmd.Dir = rootDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to build agys binary: %v\nOutput: %s", err, string(out))
		}
	}

	tempDir, err := os.MkdirTemp("", "agys-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp sandbox: %v", err)
	}

	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("Failed to create bin dir: %v", err)
	}

	geminiCliDir := filepath.Join(tempDir, ".gemini", "antigravity-cli")
	profilesDir := filepath.Join(geminiCliDir, "profiles")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatalf("Failed to create profiles dir: %v", err)
	}

	cliLogPath := filepath.Join(geminiCliDir, "cli.log")
	if err := os.WriteFile(cliLogPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to touch cli.log: %v", err)
	}

	h := &Harness{
		T:           t,
		RootDir:     rootDir,
		TempDir:     tempDir,
		BinDir:      binDir,
		ProfilesDir: profilesDir,
		TokenPath:   filepath.Join(geminiCliDir, "antigravity-oauth-token"),
		CliLogPath:  cliLogPath,
		AgysBin:     agysBin,
		StateFile:   filepath.Join(tempDir, "mock_agy_state.json"),
		ExecLogFile: filepath.Join(tempDir, "mock_agy_exec_log.json"),
	}

	h.setupMockSecurity()
	h.setupMockAgyBinary()

	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})

	return h
}

// setupMockSecurity creates a stub for macOS `security` CLI to isolate and prevent Keychain hangs.
func (h *Harness) setupMockSecurity() {
	mockSecPath := filepath.Join(h.BinDir, "security")
	script := `#!/bin/sh
exit 0
`
	if err := os.WriteFile(mockSecPath, []byte(script), 0755); err != nil {
		h.T.Fatalf("Failed to write mock security script: %v", err)
	}
}

// setupMockAgyBinary compiles or creates a scriptable mock for agy CLI.
func (h *Harness) setupMockAgyBinary() {
	mockAgySource := `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type AgyTurn struct {
	Stdout   string        ` + "`json:\"stdout\"`" + `
	Stderr   string        ` + "`json:\"stderr\"`" + `
	CliLog   string        ` + "`json:\"cli_log\"`" + `
	ExitCode int           ` + "`json:\"exit_code\"`" + `
	Delay    time.Duration ` + "`json:\"delay\"`" + `
}

type AgyExecRecord struct {
	TurnIndex   int      ` + "`json:\"turn_index\"`" + `
	Args        []string ` + "`json:\"args\"`" + `
	ActiveToken string   ` + "`json:\"active_token\"`" + `
	Timestamp   string   ` + "`json:\"timestamp\"`" + `
}

type State struct {
	CurrentTurn int       ` + "`json:\"current_turn\"`" + `
	Turns       []AgyTurn ` + "`json:\"turns\"`" + `
}

func main() {
	stateFile := os.Getenv("MOCK_AGY_STATE")
	execLogFile := os.Getenv("MOCK_AGY_EXEC_LOG")
	cliLogFile := os.Getenv("MOCK_AGY_CLI_LOG")
	tokenFile := os.Getenv("MOCK_AGY_TOKEN_FILE")

	if stateFile == "" {
		fmt.Println("Mock agy called without state file")
		os.Exit(0)
	}

	var state State
	data, err := os.ReadFile(stateFile)
	if err == nil {
		_ = json.Unmarshal(data, &state)
	}

	turnIdx := state.CurrentTurn
	var turn AgyTurn
	if turnIdx < len(state.Turns) {
		turn = state.Turns[turnIdx]
	}

	// Read active token
	var activeToken string
	if tokenData, err := os.ReadFile(tokenFile); err == nil {
		activeToken = string(tokenData)
	}

	// Record execution
	var records []AgyExecRecord
	if recData, err := os.ReadFile(execLogFile); err == nil {
		_ = json.Unmarshal(recData, &records)
	}
	records = append(records, AgyExecRecord{
		TurnIndex:   turnIdx,
		Args:        os.Args[1:],
		ActiveToken: activeToken,
		Timestamp:   time.Now().Format(time.RFC3339Nano),
	})
	if recBytes, err := json.MarshalIndent(records, "", "  "); err == nil {
		_ = os.WriteFile(execLogFile, recBytes, 0644)
	}

	// Advance turn for next run
	state.CurrentTurn = turnIdx + 1
	if stateBytes, err := json.MarshalIndent(state, "", "  "); err == nil {
		_ = os.WriteFile(stateFile, stateBytes, 0644)
	}

	// If turn has cli_log, append to cli.log after a brief delay so tailer is active
	if turn.CliLog != "" && cliLogFile != "" {
		go func() {
			time.Sleep(200 * time.Millisecond)
			f, err := os.OpenFile(cliLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				_, _ = f.WriteString(turn.CliLog + "\n")
				_ = f.Close()
			}
		}()
	}

	if turn.Stdout != "" {
		fmt.Fprint(os.Stdout, turn.Stdout)
	}
	if turn.Stderr != "" {
		fmt.Fprint(os.Stderr, turn.Stderr)
	}

	if turn.Delay > 0 {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		select {
		case <-time.After(turn.Delay):
		case <-sigCh:
			os.Exit(0)
		}
	}

	os.Exit(turn.ExitCode)
}
`
	srcPath := filepath.Join(h.TempDir, "mock_agy.go")
	if err := os.WriteFile(srcPath, []byte(mockAgySource), 0644); err != nil {
		h.T.Fatalf("Failed to write mock agy source: %v", err)
	}

	mockAgyBin := filepath.Join(h.BinDir, "agy")
	cmd := exec.Command("go", "build", "-o", mockAgyBin, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		h.T.Fatalf("Failed to build mock agy binary: %v, out: %s", err, string(out))
	}
}

// SetMockTurns sets up the scenario plan for subsequent mock agy executions.
func (h *Harness) SetMockTurns(turns []AgyTurn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	state := struct {
		CurrentTurn int       `json:"current_turn"`
		Turns       []AgyTurn `json:"turns"`
	}{
		CurrentTurn: 0,
		Turns:       turns,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		h.T.Fatalf("Failed to marshal mock turns: %v", err)
	}

	if err := os.WriteFile(h.StateFile, data, 0644); err != nil {
		h.T.Fatalf("Failed to write mock state: %v", err)
	}
	_ = os.WriteFile(h.ExecLogFile, []byte("[]"), 0644)
}

// GetMockExecutions returns all executions of mock agy.
func (h *Harness) GetMockExecutions() []AgyExecRecord {
	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := os.ReadFile(h.ExecLogFile)
	if err != nil {
		return nil
	}
	var records []AgyExecRecord
	_ = json.Unmarshal(data, &records)
	return records
}

// CreateFakeGoogleAccount generates a mock OAuth token with decoded email & name in JWT id_token.
func (h *Harness) CreateFakeGoogleAccount(profileName, email, name, tokenSecret string) error {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"email":"%s","name":"%s","sub":"123456789"}`, email, name)))
	idToken := fmt.Sprintf("%s.%s.fakesignature", header, payload)

	tokenJSON := map[string]interface{}{
		"access_token":  fmt.Sprintf("ya29.fake-%s-%s", profileName, tokenSecret),
		"refresh_token": fmt.Sprintf("1//fake-refresh-%s", profileName),
		"token_type":    "Bearer",
		"expiry":        time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		"id_token":      idToken,
	}

	data, err := json.MarshalIndent(tokenJSON, "", "  ")
	if err != nil {
		return err
	}

	profilePath := filepath.Join(h.ProfilesDir, profileName+".json")
	return os.WriteFile(profilePath, data, 0600)
}

// SetActiveAccount sets the currently active account.
func (h *Harness) SetActiveAccount(profileName string) error {
	profilePath := filepath.Join(h.ProfilesDir, profileName+".json")
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("profile %s not found: %w", profileName, err)
	}

	if err := os.WriteFile(h.TokenPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write active token: %w", err)
	}

	currentPath := filepath.Join(h.ProfilesDir, ".current")
	return os.WriteFile(currentPath, []byte(profileName), 0644)
}

// GetActiveProfileName reads the profile name recorded in .current file.
func (h *Harness) GetActiveProfileName() (string, error) {
	currentPath := filepath.Join(h.ProfilesDir, ".current")
	data, err := os.ReadFile(currentPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// GetActiveTokenContent returns the raw content of antigravity-oauth-token file.
func (h *Harness) GetActiveTokenContent() (string, error) {
	data, err := os.ReadFile(h.TokenPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RunAgys executes the agys CLI with isolated environment variables.
func (h *Harness) RunAgys(args ...string) (stdout string, stderr string, exitCode int, err error) {
	return h.RunAgysWithInput("", 15*time.Second, args...)
}

// RunAgysWithTimeout executes agys CLI with a timeout context.
func (h *Harness) RunAgysWithTimeout(timeout time.Duration, args ...string) (stdout string, stderr string, exitCode int, err error) {
	return h.RunAgysWithInput("", timeout, args...)
}

// RunAgysWithInput executes agys CLI with provided stdin input and timeout.
func (h *Harness) RunAgysWithInput(input string, timeout time.Duration, args ...string) (stdout string, stderr string, exitCode int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, h.AgysBin, args...)
	cmd.Dir = h.TempDir

	pathEnv := fmt.Sprintf("%s:%s", h.BinDir, os.Getenv("PATH"))
	cmd.Env = append(os.Environ(),
		"HOME="+h.TempDir,
		"PATH="+pathEnv,
		"MOCK_AGY_STATE="+h.StateFile,
		"MOCK_AGY_EXEC_LOG="+h.ExecLogFile,
		"MOCK_AGY_CLI_LOG="+h.CliLogPath,
		"MOCK_AGY_TOKEN_FILE="+h.TokenPath,
	)

	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	execErr := cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if execErr != nil {
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
		err = execErr
	} else {
		exitCode = 0
	}

	return stdout, stderr, exitCode, err
}
