package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseConversationTime(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"2026-10-31 16:55:07.626304+00:00", false},
		{"2026-10-31 16:55:07+00:00", false},
		{"2026-10-31T16:55:07Z", false},
		{"invalid-time", true},
	}

	for _, tt := range tests {
		_, err := parseConversationTime(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseConversationTime(%q) error = %v; wantErr %v", tt.input, err, tt.wantErr)
		}
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Now()

	// 30 seconds ago -> "Just now"
	t30s := now.Add(-30 * time.Second).UTC().Format("2006-01-02 15:04:05.999999-07:00")
	if got := formatRelativeTime(t30s); got != "Just now" {
		t.Errorf("formatRelativeTime(30s) = %q, want 'Just now'", got)
	}

	// 15 minutes ago -> "15m ago"
	t15m := now.Add(-15 * time.Minute).UTC().Format("2006-01-02 15:04:05.999999-07:00")
	if got := formatRelativeTime(t15m); got != "15m ago" {
		t.Errorf("formatRelativeTime(15m) = %q, want '15m ago'", got)
	}

	// 3 hours ago -> "3h ago"
	t3h := now.Add(-3 * time.Hour).UTC().Format("2006-01-02 15:04:05.999999-07:00")
	if got := formatRelativeTime(t3h); got != "3h ago" {
		t.Errorf("formatRelativeTime(3h) = %q, want '3h ago'", got)
	}

	// 30 hours ago -> "Yesterday"
	t30h := now.Add(-30 * time.Hour).UTC().Format("2006-01-02 15:04:05.999999-07:00")
	if got := formatRelativeTime(t30h); got != "Yesterday" {
		t.Errorf("formatRelativeTime(30h) = %q, want 'Yesterday'", got)
	}

	// 3 days ago -> "3d ago"
	t3d := now.Add(-3 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05.999999-07:00")
	if got := formatRelativeTime(t3d); got != "3d ago" {
		t.Errorf("formatRelativeTime(3d) = %q, want '3d ago'", got)
	}

	// Zero time -> "-"
	if got := formatRelativeTime("0001-01-01 00:00:00+00:00"); got != "-" {
		t.Errorf("formatRelativeTime(zero) = %q, want '-'", got)
	}
}

func TestTruncateString(t *testing.T) {
	s := "This is a very long title that exceeds the limit and needs truncation"
	truncated := truncateString(s, 20)
	if len([]rune(truncated)) > 20 {
		t.Errorf("truncateString length %d > 20", len([]rune(truncated)))
	}
	if !strings.HasSuffix(truncated, "…") {
		t.Errorf("truncateString should end with ellipsis: %s", truncated)
	}

	short := "Short"
	if truncateString(short, 20) != "Short" {
		t.Errorf("truncateString modified short string")
	}

	withNewlines := "Line 1\nLine 2\r\nLine 3"
	if strings.Contains(truncateString(withNewlines, 30), "\n") {
		t.Errorf("truncateString did not replace newlines")
	}
}
