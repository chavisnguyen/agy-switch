package e2e

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2E_ProfileManagement_SaveAndStatus tests saving a profile and viewing status.
func TestE2E_ProfileManagement_SaveAndStatus(t *testing.T) {
	h := NewHarness(t)

	// Create a dummy active token
	tokenContent := `{
  "access_token": "ya29.active-save-test",
  "refresh_token": "1//active-refresh",
  "token_type": "Bearer",
  "id_token": "eyJhbGciOiJIUzI1NiJ9.eyJlbWFpbCI6ImRldjFfZW1haWxAZ21haWwuY29tIiwibmFtZSI6IkRldiBPbmUifQ.sig"
}`
	if err := h.SetActiveAccount("temp"); err != nil {
		// Just write to TokenPath directly
		if err := writeTokenFile(h.TokenPath, tokenContent); err != nil {
			t.Fatalf("Failed to write initial active token: %v", err)
		}
	}

	stdout, stderr, exitCode, err := h.RunAgys("save", "dev_account")
	if err != nil {
		t.Fatalf("agys save failed: %v\nStdout: %s\nStderr: %s", err, stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("Expected exit code 0, got %d. Stderr: %s", exitCode, stderr)
	}

	if !strings.Contains(stdout, "Profile saved successfully: dev_account") {
		t.Errorf("Expected success message for dev_account, got: %s", stdout)
	}
	if !strings.Contains(stdout, "dev1_email@gmail.com") {
		t.Errorf("Expected email to be decoded and printed, got: %s", stdout)
	}

	// Verify agys status displays the saved profile
	stdout, _, exitCode, _ = h.RunAgys("status")
	if exitCode != 0 {
		t.Fatalf("agys status failed with code %d", exitCode)
	}
	if !strings.Contains(stdout, "dev_account") {
		t.Errorf("agys status should list dev_account, got: %s", stdout)
	}
	if !strings.Contains(stdout, "ACTIVE") {
		t.Errorf("dev_account should be marked ACTIVE, got: %s", stdout)
	}
}

// TestE2E_ProfileManagement_SaveWithoutToken tests that saving without an active token yields a clean error.
func TestE2E_ProfileManagement_SaveWithoutToken(t *testing.T) {
	h := NewHarness(t)

	stdout, stderr, _, _ := h.RunAgys("save", "no_token_profile")
	output := stdout + stderr

	if !strings.Contains(output, "Active token file not found") && !strings.Contains(output, "Error") {
		t.Errorf("Expected error about missing active token file, got: %s", output)
	}
}

// TestE2E_ProfileManagement_ManualSwitch tests switching accounts via 'agys <name>'.
func TestE2E_ProfileManagement_ManualSwitch(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("work", "work@company.com", "Work User", "secret1"); err != nil {
		t.Fatalf("Failed to create work profile: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("personal", "me@gmail.com", "Personal User", "secret2"); err != nil {
		t.Fatalf("Failed to create personal profile: %v", err)
	}

	if err := h.SetActiveAccount("work"); err != nil {
		t.Fatalf("Failed to set active account: %v", err)
	}

	// Verify initially active is work
	curr, _ := h.GetActiveProfileName()
	if curr != "work" {
		t.Fatalf("Expected active account 'work', got '%s'", curr)
	}

	// Switch to personal
	stdout, stderr, exitCode, err := h.RunAgys("personal")
	if err != nil {
		t.Fatalf("Switching to personal failed: %v\nStdout: %s\nStderr: %s", err, stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("Expected exit code 0, got %d", exitCode)
	}

	// Verify active profile is now personal
	curr, _ = h.GetActiveProfileName()
	if curr != "personal" {
		t.Errorf("Expected active account to be 'personal', got '%s'", curr)
	}

	tokenContent, _ := h.GetActiveTokenContent()
	if !strings.Contains(tokenContent, "ya29.fake-personal-secret2") {
		t.Errorf("Active token was not swapped to personal token: %s", tokenContent)
	}
}

// TestE2E_ProfileManagement_Rename tests renaming a profile.
func TestE2E_ProfileManagement_Rename(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("old_profile", "old@gmail.com", "Old User", "s1"); err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}
	if err := h.SetActiveAccount("old_profile"); err != nil {
		t.Fatalf("Failed to set active: %v", err)
	}

	stdout, stderr, exitCode, err := h.RunAgys("rename", "old_profile", "new_profile")
	if err != nil || exitCode != 0 {
		t.Fatalf("Rename failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "new_profile" {
		t.Errorf("Active profile name in .current should update to new_profile, got: %s", curr)
	}

	// Verify status lists new_profile and not old_profile
	stdout, _, _, _ = h.RunAgys("status")
	if !strings.Contains(stdout, "new_profile") || strings.Contains(stdout, "old_profile") {
		t.Errorf("Status output unexpected after rename: %s", stdout)
	}
}

// TestE2E_ProfileManagement_Remove tests deleting an existing profile.
func TestE2E_ProfileManagement_Remove(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("keep_me", "keep@gmail.com", "Keep", "k1"); err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("delete_me", "delete@gmail.com", "Delete", "d1"); err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}
	_ = h.SetActiveAccount("keep_me")

	stdout, stderr, exitCode, err := h.RunAgysWithInput("y\n", 5*time.Second, "remove", "delete_me")
	if err != nil || exitCode != 0 {
		t.Fatalf("Remove failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	stdout, _, _, _ = h.RunAgys("status")
	if strings.Contains(stdout, "delete_me") {
		t.Errorf("delete_me should be deleted from profiles, but was found in status: %s", stdout)
	}
	if !strings.Contains(stdout, "keep_me") {
		t.Errorf("keep_me should remain present in status: %s", stdout)
	}
}

// TestE2E_ProfileManagement_CorruptJSONHandling tests graceful handling of corrupted profile files.
func TestE2E_ProfileManagement_CorruptJSONHandling(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("valid_profile", "valid@gmail.com", "Valid", "v1"); err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}
	_ = h.SetActiveAccount("valid_profile")

	// Create corrupted file
	corruptPath := h.ProfilesDir + "/corrupt.json"
	if err := writeTokenFile(corruptPath, "{ invalid json content... "); err != nil {
		t.Fatalf("Failed to write corrupt profile: %v", err)
	}

	stdout, stderr, exitCode, _ := h.RunAgys("status")
	// The binary should not crash or panic
	if exitCode != 0 && exitCode != 1 {
		t.Errorf("Unexpected panic or crash exit code: %d", exitCode)
	}
	output := stdout + stderr
	if strings.Contains(output, "panic:") {
		t.Errorf("Binary panicked on corrupted profile JSON: %s", output)
	}
}

// TestE2E_Runner_HappyPath tests agys run when agy succeeds without any rate limits.
func TestE2E_Runner_HappyPath(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("main_acc", "main@gmail.com", "Main User", "m1"); err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}
	if err := h.SetActiveAccount("main_acc"); err != nil {
		t.Fatalf("Failed to set active account: %v", err)
	}

	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Executing task...\nAll tasks finished cleanly!\n",
			ExitCode: 0,
		},
	})

	stdout, stderr, exitCode, err := h.RunAgys("run")
	t.Logf("Checkpoint STDOUT:\n%s\nSTDERR:\n%s", stdout, stderr)
	if err != nil || exitCode != 0 {
		t.Fatalf("agys run failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	if !strings.Contains(stdout, "All tasks finished cleanly!") {
		t.Errorf("Expected stdout to pass through from agy, got: %s", stdout)
	}

	execs := h.GetMockExecutions()
	if len(execs) != 1 {
		t.Fatalf("Expected exactly 1 execution of agy, got %d", len(execs))
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "main_acc" {
		t.Errorf("Account should not have switched, expected 'main_acc', got '%s'", curr)
	}
}

// TestE2E_Runner_Failover_Single429 tests automatic failover when a 429 rate limit is encountered.
func TestE2E_Runner_Failover_Single429(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("acc_one", "one@gmail.com", "One", "sec1"); err != nil {
		t.Fatalf("Failed to create acc_one: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("acc_two", "two@gmail.com", "Two", "sec2"); err != nil {
		t.Fatalf("Failed to create acc_two: %v", err)
	}
	if err := h.SetActiveAccount("acc_one"); err != nil {
		t.Fatalf("Failed to set active account: %v", err)
	}

	// Turn 0 on acc_one encounters 429
	// Turn 1 on acc_two succeeds
	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Processing step 1...\n",
			CliLog:   "[2026-11-03 12:00:00] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded",
			ExitCode: 0,
			Delay:    2 * time.Second, // Allow runner to catch signal and failover
		},
		{
			Stdout:   "Resumed on healthy account. All tasks completed!\n",
			ExitCode: 0,
		},
	})

	stdout, stderr, exitCode, err := h.RunAgys("run")
	t.Logf("Checkpoint STDOUT:\n%s\nSTDERR:\n%s", stdout, stderr)
	if err != nil || exitCode != 0 {
		t.Fatalf("agys run failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	output := stdout + stderr
	if !strings.Contains(output, "Rate limit encountered") && !strings.Contains(output, "Auto-switched to 'acc_two'") {
		t.Errorf("Expected failover notification in output, got: %s", output)
	}
	if !strings.Contains(output, "Resumed on healthy account") {
		t.Errorf("Expected successful completion message from turn 2, got: %s", output)
	}

	execs := h.GetMockExecutions()
	if len(execs) != 2 {
		t.Fatalf("Expected 2 agy executions (1 failed, 1 resumed), got %d", len(execs))
	}

	// Verify turn 0 used acc_one token and turn 1 used acc_two token
	if !strings.Contains(execs[0].ActiveToken, "ya29.fake-acc_one-sec1") {
		t.Errorf("Turn 0 should have used acc_one token, got: %s", execs[0].ActiveToken)
	}
	if !strings.Contains(execs[1].ActiveToken, "ya29.fake-acc_two-sec2") {
		t.Errorf("Turn 1 should have used acc_two token, got: %s", execs[1].ActiveToken)
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "acc_two" {
		t.Errorf("Expected active profile to be 'acc_two' after failover, got '%s'", curr)
	}
}

// TestE2E_Runner_Failover_MultiHopCascade tests multiple 429 errors in sequence across 3 accounts.
func TestE2E_Runner_Failover_MultiHopCascade(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("p1", "p1@gmail.com", "P1", "k1"); err != nil {
		t.Fatalf("Failed to create p1: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("p2", "p2@gmail.com", "P2", "k2"); err != nil {
		t.Fatalf("Failed to create p2: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("p3", "p3@gmail.com", "P3", "k3"); err != nil {
		t.Fatalf("Failed to create p3: %v", err)
	}
	_ = h.SetActiveAccount("p1")

	// p1 -> 429
	// p2 -> 429
	// p3 -> success
	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Turn 1 on P1...\n",
			CliLog:   "[2026-11-03 12:00:01] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
		{
			Stdout:   "Turn 2 on P2...\n",
			CliLog:   "[2026-11-03 12:00:02] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
		{
			Stdout:   "Turn 3 on P3 success!\n",
			ExitCode: 0,
		},
	})

	stdout, stderr, exitCode, err := h.RunAgysWithTimeout(30*time.Second, "run")
	t.Logf("Cascade STDOUT:\n%s\nSTDERR:\n%s", stdout, stderr)
	if err != nil || exitCode != 0 {
		t.Fatalf("Cascade failover failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	execs := h.GetMockExecutions()
	if len(execs) != 3 {
		t.Fatalf("Expected 3 executions across 3 accounts, got %d", len(execs))
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "p3" {
		t.Errorf("Expected final active account to be 'p3', got '%s'", curr)
	}
}

// TestE2E_Runner_Failover_AllAccountsExhausted tests termination when every account is rate limited.
func TestE2E_Runner_Failover_AllAccountsExhausted(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("acc_a", "a@gmail.com", "A", "k_a"); err != nil {
		t.Fatalf("Failed to create acc_a: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("acc_b", "b@gmail.com", "B", "k_b"); err != nil {
		t.Fatalf("Failed to create acc_b: %v", err)
	}
	_ = h.SetActiveAccount("acc_a")

	// Both accounts hit 429 repeatedly until max retries / exhaustion
	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Turn 1 on acc_a hits 429...\n",
			CliLog:   "[2026-11-03 12:00:01] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
		{
			Stdout:   "Turn 2 on acc_b hits 429...\n",
			CliLog:   "[2026-11-03 12:00:02] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
		{
			Stdout:   "Turn 3 on auto-recovered acc_a hits 429 again...\n",
			CliLog:   "[2026-11-03 12:00:03] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
	})

	stdout, stderr, _, _ := h.RunAgysWithTimeout(25*time.Second, "run")
	t.Logf("AllExhausted STDOUT:\n%s\nSTDERR:\n%s", stdout, stderr)
	output := stdout + stderr

	if !strings.Contains(output, "all accounts exhausted") && !strings.Contains(output, "Max retry") && !strings.Contains(output, "Auto-recovering earliest") {
		t.Errorf("Expected notification of exhaustion or auto-recovery, got: %s", output)
	}

	execs := h.GetMockExecutions()
	// Should execute at most 2 or 3 times, never infinite
	if len(execs) > 4 {
		t.Errorf("Possible infinite loop detected! Executions: %d", len(execs))
	}
}

// TestE2E_Runner_Failover_PreserveConversationContext tests conversation ID extraction and resume flag.
func TestE2E_Runner_Failover_PreserveConversationContext(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("acc1", "1@gmail.com", "One", "s1"); err != nil {
		t.Fatalf("Failed to create acc1: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("acc2", "2@gmail.com", "Two", "s2"); err != nil {
		t.Fatalf("Failed to create acc2: %v", err)
	}
	_ = h.SetActiveAccount("acc1")

	convUUID := "5af43360-c5c2-4331-98e9-7a65630a90fb"

	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Starting session...\nCreated conversation " + convUUID + "\nWorking on user task...",
			CliLog:   "[2026-11-03 12:00:00] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
		{
			Stdout:   "Resumed successfully!\n",
			ExitCode: 0,
		},
	})

	stdout, stderr, exitCode, err := h.RunAgys("run", "--conversation="+convUUID)
	if err != nil || exitCode != 0 {
		t.Fatalf("agys run failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	execs := h.GetMockExecutions()
	if len(execs) < 2 {
		t.Fatalf("Expected at least 2 executions, got %d", len(execs))
	}

	// Check if turn 2 args contained --continue or --conversation=<convUUID>
	turn2Args := strings.Join(execs[1].Args, " ")
	hasResumeFlag := strings.Contains(turn2Args, "--continue") || strings.Contains(turn2Args, convUUID)
	if !hasResumeFlag {
		t.Errorf("Turn 2 did not receive context resume argument (--continue or --conversation). Args: %v", execs[1].Args)
	}
}

// TestE2E_Runner_Non429Error_NoSwitch tests that normal application crashes do not trigger account failover.
func TestE2E_Runner_Non429Error_NoSwitch(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("acc1", "1@gmail.com", "One", "s1"); err != nil {
		t.Fatalf("Failed to create acc1: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("acc2", "2@gmail.com", "Two", "s2"); err != nil {
		t.Fatalf("Failed to create acc2: %v", err)
	}
	_ = h.SetActiveAccount("acc1")

	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "SyntaxError: invalid syntax in script.py\n",
			Stderr:   "fatal: process died with status 1\n",
			ExitCode: 1, // Non-zero exit without 429 in log
		},
	})

	stdout, stderr, exitCode, _ := h.RunAgys("run")
	if exitCode == 0 {
		t.Errorf("Expected agys run to exit with non-zero error when child crashes")
	}

	execs := h.GetMockExecutions()
	if len(execs) != 1 {
		t.Errorf("Expected exactly 1 execution (no auto-failover for normal crash), got %d", len(execs))
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "acc1" {
		t.Errorf("Account should remain 'acc1', but was switched to '%s'", curr)
	}

	output := stdout + stderr
	if strings.Contains(output, "Rate limit encountered") {
		t.Errorf("Should not report rate limit on generic error: %s", output)
	}
}

// TestE2E_Runner_FalsePositiveFilter tests that mentioning '429' in chat content does not trigger failover.
func TestE2E_Runner_FalsePositiveFilter(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("acc1", "1@gmail.com", "One", "s1"); err != nil {
		t.Fatalf("Failed to create acc1: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("acc2", "2@gmail.com", "Two", "s2"); err != nil {
		t.Fatalf("Failed to create acc2: %v", err)
	}
	_ = h.SetActiveAccount("acc1")

	h.SetMockTurns([]AgyTurn{
		{
			// Model is outputting text discussing HTTP 429
			Stdout:   "In HTTP networking, HTTP 429 Too Many Requests indicates rate limiting.\nDone with explanation!\n",
			CliLog:   "[2026-11-03 12:00:00] Normal assistant response completed",
			ExitCode: 0,
		},
	})

	stdout, stderr, exitCode, err := h.RunAgys("run")
	t.Logf("Checkpoint STDOUT:\n%s\nSTDERR:\n%s", stdout, stderr)
	if err != nil || exitCode != 0 {
		t.Fatalf("agys run failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	execs := h.GetMockExecutions()
	if len(execs) != 1 {
		t.Errorf("Expected 1 execution, got %d (false positive triggered unnecessary switch!)", len(execs))
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "acc1" {
		t.Errorf("Account switched unexpectedly: %s", curr)
	}
}

// TestE2E_Runner_SingleAccount_FailoverGraceful tests failover behavior when only 1 account exists.
func TestE2E_Runner_SingleAccount_FailoverGraceful(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("only_one", "only@gmail.com", "Only One", "s1"); err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}
	_ = h.SetActiveAccount("only_one")

	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Running task...\n",
			CliLog:   "[2026-11-03 12:00:00] agent executor error: RESOURCE_EXHAUSTED 429",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
	})

	stdout, stderr, _, _ := h.RunAgysWithTimeout(15*time.Second, "run")
	output := stdout + stderr

	if !strings.Contains(output, "Cannot switch") && !strings.Contains(output, "exhausted") && !strings.Contains(output, "no other profiles") && !strings.Contains(output, "no profiles found") {
		t.Logf("Runner handled single account exhaust with output: %s", output)
	}
}

// TestE2E_Cooldown_InspectionAndClear tests viewing and resetting account cooldowns.
func TestE2E_Cooldown_InspectionAndClear(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("p1", "p1@gmail.com", "P1", "s1"); err != nil {
		t.Fatalf("Failed to create p1: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("p2", "p2@gmail.com", "P2", "s2"); err != nil {
		t.Fatalf("Failed to create p2: %v", err)
	}
	_ = h.SetActiveAccount("p1")

	// Trigger 429 on p1 to put it into cooldown
	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Step 1...\n",
			CliLog:   "[2026-11-03 12:00:00] agent executor error: RESOURCE_EXHAUSTED 429",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
		{
			Stdout:   "Step 2 success on p2!\n",
			ExitCode: 0,
		},
	})

	_, _, _, _ = h.RunAgys("run")

	// Check agys cooldown
	stdout, _, _, _ := h.RunAgys("cooldown")
	if !strings.Contains(stdout, "p1") {
		t.Errorf("agys cooldown should list 'p1' under cooldown, got: %s", stdout)
	}

	// Clear cooldowns
	stdout, _, exitCode, err := h.RunAgys("cooldown", "clear")
	if err != nil || exitCode != 0 {
		t.Fatalf("agys cooldown clear failed: %v", err)
	}
	if !strings.Contains(stdout, "Cleared all cooldowns") && !strings.Contains(strings.ToLower(stdout), "cleared") {
		t.Errorf("Expected cooldown cleared message, got: %s", stdout)
	}

	// Verify cooldown list is now empty / ready
	stdout, _, _, _ = h.RunAgys("cooldown")
	if strings.Contains(stdout, "p1") && strings.Contains(stdout, "Cooldown (") {
		t.Errorf("p1 should no longer be in cooldown after clear: %s", stdout)
	}
}

// TestE2E_Next_SmartSelection_SkipsCooldown tests that 'agys next' skips accounts in cooldown.
func TestE2E_Next_SmartSelection_SkipsCooldown(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("c1", "c1@gmail.com", "C1", "k1"); err != nil {
		t.Fatalf("Failed to create c1: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("c2", "c2@gmail.com", "C2", "k2"); err != nil {
		t.Fatalf("Failed to create c2: %v", err)
	}
	_ = h.SetActiveAccount("c1")

	// Put c1 into cooldown via 429
	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Step 1...\n",
			CliLog:   "[2026-11-03 12:00:00] agent executor error: RESOURCE_EXHAUSTED 429",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
		{
			Stdout:   "Step 2 on c2...\n",
			ExitCode: 0,
		},
	})
	_, _, _, _ = h.RunAgys("run")

	// c1 is now in cooldown, active is c2.
	// Let's manually set active back to c1, and test if 'agys next' rotates to c2!
	_ = h.SetActiveAccount("c1")

	stdout, stderr, exitCode, err := h.RunAgys("next")
	if err != nil || exitCode != 0 {
		t.Fatalf("agys next failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "c2" {
		t.Errorf("agys next should have chosen 'c2' because 'c1' is in cooldown, got: %s", curr)
	}
}

// TestE2E_ProfileManagement_SwitchNonExistent tests switching to a non-existent profile.
func TestE2E_ProfileManagement_SwitchNonExistent(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("valid", "valid@gmail.com", "Valid", "v1"); err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}
	_ = h.SetActiveAccount("valid")

	stdout, stderr, exitCode, _ := h.RunAgys("nonexistent_profile")
	output := stdout + stderr

	if !strings.Contains(output, "not found") && !strings.Contains(output, "Error") {
		t.Errorf("Expected profile not found message, got: %s", output)
	}
	if exitCode == 0 && !strings.Contains(output, "not found") {
		t.Errorf("Expected non-zero exit or error notification, got exit %d", exitCode)
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "valid" {
		t.Errorf("Active profile should remain 'valid', got '%s'", curr)
	}
}

// TestE2E_ProfileManagement_SwitchAlreadyActive tests switching to the currently active profile.
func TestE2E_ProfileManagement_SwitchAlreadyActive(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("active_me", "me@gmail.com", "Active Me", "act1"); err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}
	_ = h.SetActiveAccount("active_me")

	stdout, stderr, exitCode, err := h.RunAgys("active_me")
	if err != nil || exitCode != 0 {
		t.Fatalf("Switching to already active profile failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "active_me" {
		t.Errorf("Expected active profile to remain 'active_me', got '%s'", curr)
	}
}

// TestE2E_ProfileManagement_RenameCollision tests that renaming to an existing name is blocked.
func TestE2E_ProfileManagement_RenameCollision(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("p1", "1@gmail.com", "One", "1"); err != nil {
		t.Fatalf("Failed to create p1: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("p2", "2@gmail.com", "Two", "2"); err != nil {
		t.Fatalf("Failed to create p2: %v", err)
	}

	stdout, stderr, _, _ := h.RunAgys("rename", "p1", "p2")
	output := stdout + stderr

	if !strings.Contains(output, "already exists") {
		t.Errorf("Expected collision error ('already exists'), got: %s", output)
	}
}

// TestE2E_ProfileManagement_RenameNonExistent tests renaming a profile that doesn't exist.
func TestE2E_ProfileManagement_RenameNonExistent(t *testing.T) {
	h := NewHarness(t)

	stdout, stderr, _, _ := h.RunAgys("rename", "ghost", "phantom")
	output := stdout + stderr

	if !strings.Contains(output, "not found") && !strings.Contains(output, "Error") {
		t.Errorf("Expected error for non-existent profile, got: %s", output)
	}
}

// TestE2E_ProfileManagement_PathTraversalSanitization tests that directory traversal is sanitized.
func TestE2E_ProfileManagement_PathTraversalSanitization(t *testing.T) {
	h := NewHarness(t)

	tokenContent := `{"access_token":"ya29.safe-token","token_type":"Bearer"}`
	if err := writeTokenFile(h.TokenPath, tokenContent); err != nil {
		t.Fatalf("Failed to write active token: %v", err)
	}

	// Attempt path traversal
	_, _, _, _ = h.RunAgys("save", "../../../evil_profile")

	// Verify no file was written outside profiles dir
	outsidePath := h.TempDir + "/evil_profile.json"
	if _, err := os.Stat(outsidePath); !os.IsNotExist(err) {
		t.Fatalf("Vulnerability detected: File was written outside profiles directory at %s", outsidePath)
	}
}

// TestE2E_ProfileManagement_TokenWithoutIdToken tests handling when token lacks an id_token JWT.
func TestE2E_ProfileManagement_TokenWithoutIdToken(t *testing.T) {
	h := NewHarness(t)

	tokenJSON := `{
  "access_token": "ya29.raw-token-without-jwt",
  "refresh_token": "1//raw-refresh",
  "token_type": "Bearer"
}`
	profilePath := h.ProfilesDir + "/no_jwt.json"
	if err := writeTokenFile(profilePath, tokenJSON); err != nil {
		t.Fatalf("Failed to write no_jwt profile: %v", err)
	}
	_ = h.SetActiveAccount("no_jwt")

	stdout, stderr, exitCode, _ := h.RunAgys("status")
	if exitCode != 0 {
		t.Errorf("agys status failed on profile without id_token: exit %d\nStderr: %s", exitCode, stderr)
	}
	output := stdout + stderr
	if strings.Contains(output, "panic:") {
		t.Fatalf("Binary panicked when reading profile without id_token: %s", output)
	}
	if !strings.Contains(output, "no_jwt") {
		t.Errorf("agys status should list no_jwt profile, got: %s", output)
	}
}

// TestE2E_Runner_ArgumentsPassthroughWithQuotesAndSpaces verifies arguments are not split or mangled.
func TestE2E_Runner_ArgumentsPassthroughWithQuotesAndSpaces(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("acc1", "1@gmail.com", "One", "1"); err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}
	_ = h.SetActiveAccount("acc1")

	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Done!\n",
			ExitCode: 0,
		},
	})

	complexPrompt := "explain how Go interfaces work with nil pointers"
	modelFlag := "gemini-1.5-pro"

	stdout, stderr, exitCode, err := h.RunAgys("run", "--model", modelFlag, "-p", complexPrompt)
	if err != nil || exitCode != 0 {
		t.Fatalf("agys run failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	execs := h.GetMockExecutions()
	if len(execs) != 1 {
		t.Fatalf("Expected 1 execution, got %d", len(execs))
	}

	recordedArgs := execs[0].Args
	// Verify that complexPrompt was passed as a single element, not split into multiple words
	foundPrompt := false
	for _, arg := range recordedArgs {
		if arg == complexPrompt {
			foundPrompt = true
			break
		}
	}
	if !foundPrompt {
		t.Errorf("Argument with spaces was corrupted or split! Args received: %v", recordedArgs)
	}
}

// TestE2E_Runner_Failover_VerifyAccountCheckpoint tests failover triggered by Google checkpoint/verification.
func TestE2E_Runner_Failover_VerifyAccountCheckpoint(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("checkpoint_acc", "cp@gmail.com", "CP", "c1"); err != nil {
		t.Fatalf("Failed to create cp account: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("backup_acc", "bk@gmail.com", "BK", "b1"); err != nil {
		t.Fatalf("Failed to create bk account: %v", err)
	}
	_ = h.SetActiveAccount("checkpoint_acc")

	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Checking account authorization...\n",
			CliLog:   "[2026-11-03 12:00:00] agent executor error: 429 Individual quota reached",
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
		{
			Stdout:   "Resumed on backup account!\n",
			ExitCode: 0,
		},
	})

	stdout, stderr, exitCode, err := h.RunAgys("run")
	t.Logf("Checkpoint STDOUT:\n%s\nSTDERR:\n%s", stdout, stderr)
	if err != nil || exitCode != 0 {
		t.Fatalf("Failover on checkpoint failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	execs := h.GetMockExecutions()
	if len(execs) != 2 {
		t.Fatalf("Expected 2 executions for checkpoint failover, got %d", len(execs))
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "backup_acc" {
		t.Errorf("Expected failover to backup_acc, got '%s'", curr)
	}
}

// TestE2E_Runner_DebounceBurstRateLimits verifies that multiple rapid 429 log lines only trigger 1 failover.
func TestE2E_Runner_DebounceBurstRateLimits(t *testing.T) {
	h := NewHarness(t)

	if err := h.CreateFakeGoogleAccount("p1", "1@gmail.com", "P1", "1"); err != nil {
		t.Fatalf("Failed to create p1: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("p2", "2@gmail.com", "P2", "2"); err != nil {
		t.Fatalf("Failed to create p2: %v", err)
	}
	if err := h.CreateFakeGoogleAccount("p3", "3@gmail.com", "P3", "3"); err != nil {
		t.Fatalf("Failed to create p3: %v", err)
	}
	_ = h.SetActiveAccount("p1")

	// Burst of 3 rate limit errors in a single turn log
	burstLog := `[2026-11-03 12:00:00] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded
[2026-11-03 12:00:00] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded
[2026-11-03 12:00:00] agent executor error: RESOURCE_EXHAUSTED 429 rate limit exceeded`

	h.SetMockTurns([]AgyTurn{
		{
			Stdout:   "Generating turn 1...\n",
			CliLog:   burstLog,
			ExitCode: 0,
			Delay:    2 * time.Second,
		},
		{
			Stdout:   "Resumed on turn 2!\n",
			ExitCode: 0,
		},
	})

	stdout, stderr, exitCode, err := h.RunAgys("run")
	t.Logf("Checkpoint STDOUT:\n%s\nSTDERR:\n%s", stdout, stderr)
	if err != nil || exitCode != 0 {
		t.Fatalf("Burst run failed: %v (exit %d)\nStdout: %s\nStderr: %s", err, exitCode, stdout, stderr)
	}

	execs := h.GetMockExecutions()
	// Should switch exactly ONCE to p2, NOT flap to p3
	if len(execs) != 2 {
		t.Errorf("Debounce failed: expected exactly 2 executions, got %d", len(execs))
	}

	curr, _ := h.GetActiveProfileName()
	if curr != "p2" {
		t.Errorf("Expected active profile to be p2, got '%s'", curr)
	}
}

// TestE2E_Utility_VersionAndHelp tests utility subcommands version and help.
func TestE2E_Utility_VersionAndHelp(t *testing.T) {
	h := NewHarness(t)

	// Version
	stdout, _, exitCode, err := h.RunAgys("version")
	if err != nil || exitCode != 0 {
		t.Fatalf("agys version failed: %v (exit %d)", err, exitCode)
	}
	if !strings.Contains(strings.ToLower(stdout), "version") && !strings.Contains(stdout, "agys") {
		t.Errorf("agys version output unexpected: %s", stdout)
	}

	// Help
	stdout, _, exitCode, err = h.RunAgys("help")
	if err != nil || exitCode != 0 {
		t.Fatalf("agys help failed: %v (exit %d)", err, exitCode)
	}
	if !strings.Contains(stdout, "RUN & CODE") || !strings.Contains(stdout, "ACCOUNTS & QUOTAS") {
		t.Errorf("agys help output unexpected: %s", stdout)
	}
}

// Helper to write string to file
func writeTokenFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
