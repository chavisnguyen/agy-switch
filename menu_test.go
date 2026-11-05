package main

import (
	"reflect"
	"testing"
)

func keys(ks ...menuKey) []keyEvent {
	var out []keyEvent
	for _, k := range ks {
		out = append(out, keyEvent{key: k})
	}
	return out
}

func TestParseKeys(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		timedOut bool
		want     []keyEvent
		wantRest string
	}{
		{"arrow down", "\x1b[B", false, keys(keyDown), ""},
		{"arrow up", "\x1b[A", false, keys(keyUp), ""},
		{"application mode arrows", "\x1bOA\x1bOB", false, keys(keyUp, keyDown), ""},
		{"several keys in one read", "\x1b[B\x1b[B\x1b[A", false, keys(keyDown, keyDown, keyUp), ""},
		{"page up/down", "\x1b[5~\x1b[6~", false, keys(keyPageUp, keyPageDown), ""},
		{"home/end variants", "\x1b[H\x1b[F\x1b[1~\x1b[4~\x1b[7~\x1b[8~", false, keys(keyHome, keyEnd, keyHome, keyEnd, keyHome, keyEnd), ""},
		{"modified arrow keeps direction", "\x1b[1;5A", false, keys(keyUp), ""},
		{"unknown sequence is swallowed, not Esc", "\x1b[I", false, nil, ""},
		{"alt+key ignored", "\x1bj", false, nil, ""},
		{"lone esc waits for more input", "\x1b", false, nil, "\x1b"},
		{"lone esc after timeout cancels", "\x1b", true, keys(keyCancel), ""},
		{"partial CSI waits", "\x1b[", false, nil, "\x1b["},
		{"partial CSI dropped after timeout", "\x1b[", true, nil, ""},
		{"event then partial tail", "\x1b[B\x1b", false, keys(keyDown), "\x1b"},
		{"vim keys", "jkgG", false, keys(keyDown, keyUp, keyHome, keyEnd), ""},
		{"ctrl n/p", "\x0e\x10", false, keys(keyDown, keyUp), ""},
		{"enter variants", "\r\n ", false, keys(keyEnter, keyEnter, keyEnter), ""},
		{"cancel keys", "qQ\x03", false, keys(keyCancel, keyCancel, keyCancel), ""},
		{"digits", "37", false, []keyEvent{{key: keyDigit, digit: 3}, {key: keyDigit, digit: 7}}, ""},
		{"unrelated bytes ignored", "xyz0", false, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, rest := parseKeys([]byte(c.in), c.timedOut)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("events = %+v, want %+v", got, c.want)
			}
			if string(rest) != c.wantRest {
				t.Errorf("rest = %q, want %q", rest, c.wantRest)
			}
		})
	}
}

// An escape sequence split across two reads must still decode as one key.
func TestParseKeysSplitAcrossReads(t *testing.T) {
	ev, rest := parseKeys([]byte("\x1b"), false)
	if len(ev) != 0 || string(rest) != "\x1b" {
		t.Fatalf("first read: events=%v rest=%q", ev, rest)
	}
	ev, rest = parseKeys(append(rest, []byte("[B")...), false)
	if !reflect.DeepEqual(ev, keys(keyDown)) || len(rest) != 0 {
		t.Fatalf("second read: events=%v rest=%q, want a single Down", ev, rest)
	}
}
