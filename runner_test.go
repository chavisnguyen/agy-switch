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

func TestConvUUIDRegex(t *testing.T) {
	validLines := []string{
		"I1031 20:50:39.578727 1 server.go:3316] GetConversationDetail: found conversation 6c04bd0d-9dbd-4b97-a819-be2a1fd99e84 (active=false)",
		"Resuming conversation f51a3479-5307-41fc-b719-c8ae98bc1751",
		"cascade_id:3af25384-af0b-4a1f-ac4e-56079cb72c20",
		"agy --conversation=2071c421-ee0c-4c8a-9af0-1d28d6c9983b",
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
