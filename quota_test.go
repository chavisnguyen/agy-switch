package main

import (
	"context"
	"errors"
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

func TestCandidateUsableAndRecovery(t *testing.T) {
	now := time.Now()
	rfc := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }
	week := QuotaWindow{Remaining: 0, ResetTime: rfc(2 * 24 * time.Hour)}
	disabled5h := QuotaWindow{Remaining: 1, Disabled: true}

	// Claude weekly gone, Gemini healthy: still usable.
	c := evaluateProfileCandidate(ProfileInfo{ProfileName: "a"}, ProfileQuota{
		ClaudeRemaining: 1, ClaudeHasSummary: true, Claude5h: disabled5h, ClaudeWeekly: week,
		GeminiRemaining: 1, GeminiResetTime: rfc(4 * time.Hour),
	}, now)
	if !candidateUsable(c) {
		t.Errorf("Gemini has quota, candidate should be usable")
	}

	// Both models out: not usable, recovery is the soonest reset.
	c = evaluateProfileCandidate(ProfileInfo{ProfileName: "b"}, ProfileQuota{
		ClaudeRemaining: 1, ClaudeHasSummary: true, Claude5h: disabled5h, ClaudeWeekly: week,
		GeminiRemaining: 0, GeminiResetTime: rfc(5 * time.Hour),
	}, now)
	if candidateUsable(c) {
		t.Errorf("both models exhausted, candidate should not be usable")
	}
	if d := candidateRecovery(c); d < 4*time.Hour || d > 5*time.Hour+time.Minute {
		t.Errorf("recovery = %v, want about 5h (Gemini reset is sooner than Claude weekly)", d)
	}

	if d := candidateRecovery(SmartCandidate{}); d != 0 {
		t.Errorf("no reset times should give 0, got %v", d)
	}
}

func TestFormatShortDuration(t *testing.T) {
	cases := map[time.Duration]string{
		42 * time.Minute:             "42m",
		3*time.Hour + 5*time.Minute:  "3h05m",
		2*24*time.Hour + 7*time.Hour: "2d07h",
	}
	for d, want := range cases {
		if got := formatShortDuration(d); got != want {
			t.Errorf("formatShortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestApplyQuotaSummaryDerivesEffectiveWindows(t *testing.T) {
	summary := &QuotaSummaryResponse{
		Groups: []QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []QuotaBucket{
					{BucketID: "gemini-weekly", Window: "weekly", RemainingFraction: 0.84, ResetTime: "2026-12-03T07:01:51Z"},
					{BucketID: "gemini-5h", Window: "5h", RemainingFraction: 0.78, ResetTime: "2026-11-26T17:01:51Z"},
				},
			},
			{
				DisplayName: "Claude and GPT models",
				Buckets: []QuotaBucket{
					{BucketID: "3p-weekly", Window: "weekly", RemainingFraction: 1.0, ResetTime: "1970-01-01T00:00:00Z"},
					{BucketID: "3p-5h", Window: "5h", RemainingFraction: 1.0, ResetTime: "1970-01-01T00:00:00Z"},
				},
			},
		},
	}

	var q ProfileQuota
	applyQuotaSummary(&q, summary)

	if !q.GeminiHasSummary || q.GeminiRemaining != 0.78 || q.GeminiResetTime != "2026-11-26T17:01:51Z" {
		t.Errorf("unexpected Gemini effective quota: %+v", q)
	}
	if !q.ClaudeHasSummary || q.ClaudeRemaining != 1.0 || q.ClaudeResetTime != "" {
		t.Errorf("expected unused Claude epoch reset time to be stripped, got: %+v", q)
	}

	// Weekly cap hit disables 5h bucket -> effective remaining should reflect weekly cap (0)
	lockedSummary := &QuotaSummaryResponse{
		Groups: []QuotaGroup{
			{
				DisplayName: "Claude and GPT models",
				Buckets: []QuotaBucket{
					{BucketID: "3p-weekly", Window: "weekly", RemainingFraction: 0, ResetTime: "2026-12-03T07:01:51Z"},
					{BucketID: "3p-5h", Window: "5h", RemainingFraction: 1.0, Disabled: true},
				},
			},
		},
	}
	var qLocked ProfileQuota
	applyQuotaSummary(&qLocked, lockedSummary)
	if qLocked.ClaudeRemaining != 0 || qLocked.ClaudeResetTime != "2026-12-03T07:01:51Z" {
		t.Errorf("expected weekly-locked ClaudeRemaining=0 with weekly reset time, got %+v", qLocked)
	}
}

func TestFormatQuotaErrorShort(t *testing.T) {
	if got := formatQuotaErrorShort(context.DeadlineExceeded); got != "timeout" {
		t.Errorf("got %q, want timeout", got)
	}
	if got := formatQuotaErrorShort(errors.New("quota summary api error (401): unauthorized")); got != "auth error" {
		t.Errorf("got %q, want auth error", got)
	}
	if got := formatQuotaErrorShort(errors.New("token refresh failed (400): invalid_grant")); got != "token expired" {
		t.Errorf("got %q, want token expired", got)
	}
}
