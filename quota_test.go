package main

import (
	"strings"
	"testing"
	"time"
)

func TestFormatQuotaReset(t *testing.T) {
	now := time.Now()

	// 1. Within 5 hours -> [5h]
	t2h := now.Add(2 * time.Hour).UTC().Format(time.RFC3339)
	out2h := formatQuotaReset(t2h)
	if !strings.Contains(out2h, "[5h]") {
		t.Errorf("formatQuotaReset(2h) = %q, expected to contain '[5h]'", out2h)
	}
	if !strings.Contains(out2h, "in 2h") && !strings.Contains(out2h, "in 1h59m") {
		t.Errorf("formatQuotaReset(2h) = %q, expected relative duration", out2h)
	}

	// 2. Beyond 5h15m (e.g. 4 days) -> [Weekly Cap ⚠️]
	t4d := now.Add(4 * 24 * time.Hour).UTC().Format(time.RFC3339)
	out4d := formatQuotaReset(t4d)
	if !strings.Contains(out4d, "[Weekly Cap ⚠️]") {
		t.Errorf("formatQuotaReset(4d) = %q, expected to contain '[Weekly Cap ⚠️]'", out4d)
	}
	if !strings.Contains(out4d, "4d left") {
		t.Errorf("formatQuotaReset(4d) = %q, expected '4d left'", out4d)
	}

	// 3. Past due -> reset pending
	tPast := now.Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	outPast := formatQuotaReset(tPast)
	if !strings.Contains(outPast, "reset pending") {
		t.Errorf("formatQuotaReset(past) = %q, expected 'reset pending'", outPast)
	}
}

func TestEvaluateProfileCandidate(t *testing.T) {
	now := time.Now()

	// Case 1: Healthy candidate in 5h cycle
	p1 := ProfileInfo{ProfileName: "prof1"}
	q1 := ProfileQuota{
		ClaudeRemaining: 0.85,
		ClaudeResetTime: now.Add(2 * time.Hour).UTC().Format(time.RFC3339),
	}
	c1 := evaluateProfileCandidate(p1, q1, now)
	if c1.IsLocked {
		t.Errorf("expected prof1 not to be locked, got %v (%s)", c1.IsLocked, c1.LockReason)
	}
	if c1.IsClaudeWeekly {
		t.Errorf("expected prof1 Claude not to be weekly locked")
	}

	// Case 2: Weekly locked with 0% Claude
	p2 := ProfileInfo{ProfileName: "prof2"}
	q2 := ProfileQuota{
		ClaudeRemaining: 0.0,
		ClaudeResetTime: now.Add(3 * 24 * time.Hour).UTC().Format(time.RFC3339),
		GeminiRemaining: 0.0,
		GeminiResetTime: now.Add(3 * 24 * time.Hour).UTC().Format(time.RFC3339),
	}
	c2 := evaluateProfileCandidate(p2, q2, now)
	if !c2.IsLocked {
		t.Errorf("expected prof2 to be locked due to Weekly Cap")
	}
	if !c2.IsClaudeWeekly {
		t.Errorf("expected prof2 Claude to be identified as weekly locked")
	}

	// Case 3: Profile on Cooldown
	p3 := ProfileInfo{ProfileName: "prof3", IsCoolingDown: true, CooldownRemain: "15m"}
	q3 := ProfileQuota{ClaudeRemaining: 1.0}
	c3 := evaluateProfileCandidate(p3, q3, now)
	if !c3.IsLocked {
		t.Errorf("expected prof3 to be locked due to Cooldown")
	}
}
