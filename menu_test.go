package main

import (
	"testing"

	"atomicgo.dev/keyboard/keys"
)

func runeKey(r rune) keys.Key { return keys.Key{Code: keys.RuneKey, Runes: []rune{r}} }

func TestTranslateKey(t *testing.T) {
	cases := []struct {
		name string
		in   keys.Key
		want keyEvent
	}{
		{"arrow up", keys.Key{Code: keys.Up}, keyEvent{key: keyUp}},
		{"arrow down", keys.Key{Code: keys.Down}, keyEvent{key: keyDown}},
		{"ctrl+p / ctrl+n", keys.Key{Code: keys.CtrlP}, keyEvent{key: keyUp}},
		{"ctrl+n", keys.Key{Code: keys.CtrlN}, keyEvent{key: keyDown}},
		{"home / end", keys.Key{Code: keys.Home}, keyEvent{key: keyHome}},
		{"end", keys.Key{Code: keys.End}, keyEvent{key: keyEnd}},
		{"page up", keys.Key{Code: keys.PgUp}, keyEvent{key: keyPageUp}},
		{"page down", keys.Key{Code: keys.PgDown}, keyEvent{key: keyPageDown}},
		{"enter", keys.Key{Code: keys.Enter}, keyEvent{key: keyEnter}},
		{"space", keys.Key{Code: keys.Space}, keyEvent{key: keyEnter}},
		{"esc", keys.Key{Code: keys.Esc}, keyEvent{key: keyCancel}},
		{"ctrl+c", keys.Key{Code: keys.CtrlC}, keyEvent{key: keyCancel}},
		{"q", runeKey('q'), keyEvent{key: keyCancel}},
		{"vim j", runeKey('j'), keyEvent{key: keyDown}},
		{"vim k", runeKey('k'), keyEvent{key: keyUp}},
		{"g / G", runeKey('G'), keyEvent{key: keyEnd}},
		{"digit", runeKey('7'), keyEvent{key: keyDigit, digit: 7}},
		{"zero is not a jump", runeKey('0'), keyEvent{}},
		{"unknown letter", runeKey('x'), keyEvent{}},
		{"alt+q ignored", keys.Key{Code: keys.RuneKey, Runes: []rune{'q'}, AltPressed: true}, keyEvent{}},
	}
	for _, c := range cases {
		if got := translateKey(c.in); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestMoveSelection(t *testing.T) {
	cases := []struct {
		name       string
		sel, count int
		key        menuKey
		want       int
	}{
		{"down", 0, 3, keyDown, 1},
		{"down wraps", 2, 3, keyDown, 0},
		{"up wraps", 0, 3, keyUp, 2},
		{"home", 4, 6, keyHome, 0},
		{"end", 0, 6, keyEnd, 5},
		{"page down clamps", 3, 6, keyPageDown, 5},
		{"page up clamps", 2, 6, keyPageUp, 0},
	}
	for _, c := range cases {
		if got := moveSelection(c.sel, c.count, keyEvent{key: c.key}); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
