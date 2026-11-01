package main

import (
	"testing"
)

func TestExtractConversationID(t *testing.T) {
	tests := []struct {
		args     []string
		expected string
	}{
		{[]string{"--conversation=6c04bd0d-9dbd-4b97-a819-be2a1fd99e84"}, "6c04bd0d-9dbd-4b97-a819-be2a1fd99e84"},
		{[]string{"--conversation", "f51a3479-5307-41fc-b719-c8ae98bc1751"}, "f51a3479-5307-41fc-b719-c8ae98bc1751"},
		{[]string{"--continue", "-i"}, ""},
		{[]string{}, ""},
	}

	for _, tt := range tests {
		got := extractConversationID(tt.args)
		if got != tt.expected {
			t.Errorf("extractConversationID(%v) = %q; want %q", tt.args, got, tt.expected)
		}
	}
}

func TestHasContinueFlag(t *testing.T) {
	tests := []struct {
		args     []string
		expected bool
	}{
		{[]string{"-c"}, true},
		{[]string{"--continue"}, true},
		{[]string{"-c", "--conversation=123"}, true},
		{[]string{"--conversation=6c04bd0d-9dbd-4b97-a819-be2a1fd99e84"}, false},
		{[]string{"run", "hello"}, false},
		{[]string{}, false},
	}

	for _, tt := range tests {
		got := hasContinueFlag(tt.args)
		if got != tt.expected {
			t.Errorf("hasContinueFlag(%v) = %v; want %v", tt.args, got, tt.expected)
		}
	}
}

func TestConvUUIDRegex(t *testing.T) {
	validLines := []string{
		"I1031 20:50:39.578727 1 server.go:3316] GetConversationDetail: found conversation 6c04bd0d-9dbd-4b97-a819-be2a1fd99e84 (active=false)",
		"Resuming conversation f51a3479-5307-41fc-b719-c8ae98bc1751",
		"cascade_id:3af25384-af0b-4a1f-ac4e-56079cb72c20",
		"agy --conversation=2071c421-ee0c-4c8a-9af0-1d28d6c9983b",
		"I1101 18:03:47.720759 907 server.go:1241] Created conversation 3df6298b-fd81-457b-83c8-e6510b54ec51 (backend=BACKEND_TYPE_UNSPECIFIED)",
		"I1101 18:03:47.722144 907 server.go:1259] Starting conversation update stream for 3df6298b-fd81-457b-83c8-e6510b54ec51",
	}

	for _, line := range validLines {
		m := convUUIDRegex.FindStringSubmatch(line)
		if len(m) < 2 || m[1] == "" {
			t.Errorf("convUUIDRegex failed to match conversation UUID in line: %s", line)
		}
	}

	// Should not match standalone trajectory IDs
	invalidLine := "latency_breakdown.go:170] trajectory_id:7aaddd52-3b64-4c8d-b9f2-96726baadb12 total_latency_ms:3"
	if m := convUUIDRegex.FindStringSubmatch(invalidLine); len(m) > 1 {
		t.Errorf("convUUIDRegex matched trajectory_id when it should not: %v", m)
	}
}

func TestIsRateLimitError(t *testing.T) {
	// Authentic error lines that should trigger failover
	validErrors := []string{
		`E1031 17:59:50.369283 1044 errorreport.go:224] agent executor error: pre-invocation hook: context summarization: RESOURCE_EXHAUSTED (code 429): Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 150h1m5s.`,
		`E1031 20:49:52.880403 3946 errorreport.go:224] agent executor error: generating and executing: RESOURCE_EXHAUSTED (code 429): Individual quota reached.`,
		`W1031 20:44:39.350489 762 server_oauth.go:134] Account ineligible: Your current account is not eligible for Antigravity. Verify your account to continue.`,
	}

	for _, line := range validErrors {
		if !isRateLimitError(line) {
			t.Errorf("isRateLimitError failed to detect authentic error line: %s", line)
		}
	}

	// User inputs or false-positive logs containing rate limit phrases that MUST be ignored
	ignoredLines := []string{
		`I1101 18:03:15.282957 813 input_loop.go:106] HandleUserInput called with text: "⚠ Individual quota reached. Please upgrade your subscription"`,
		`I1101 18:02:09.898858 1387 input_loop.go:106] HandleUserInput called with text: "⚡ Rate limit encountered during this turn!"`,
		`I1101 18:03:15.338112 815 conversation_manager.go:903] Forwarding user message to conversation 693df97c (items=1, media=0)`,
		`I1101 18:03:15.338127 815 server.go:1833] Sending user message to conversation 693df97c (items=1, media=0)`,
		`I1101 18:03:15.339976 815 latency_breakdown.go:170] SEND_USER_CASCADE_MESSAGE_LATENCY latency breakdown: quota reached`,
	}

	for _, line := range ignoredLines {
		if isRateLimitError(line) {
			t.Errorf("isRateLimitError falsely triggered on user input or message forwarding: %s", line)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current  string
		latest   string
		expected bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.1", "v1.0.2", true},
		{"v1.0.2", "v1.0.1", false},
		{"v1.0.2", "v1.0.2", false},
		{"1.0.0", "1.0.1", true},
		{"v1.0.0", "v2.0.0", true},
		{"v2.0.0", "v1.9.9", false},
	}

	for _, tt := range tests {
		got := isNewerVersion(tt.current, tt.latest)
		if got != tt.expected {
			t.Errorf("isNewerVersion(%q, %q) = %v; want %v", tt.current, tt.latest, got, tt.expected)
		}
	}
}
