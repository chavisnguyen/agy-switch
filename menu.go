package main

import (
	"fmt"
	"os"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"golang.org/x/term"
)

// The menu key handling uses atomicgo.dev/keyboard (the library pterm builds on): raw mode,
// escape-sequence parsing and terminal restore are its job, on every platform.

type menuKey int

const (
	keyNone menuKey = iota
	keyUp
	keyDown
	keyHome
	keyEnd
	keyPageUp
	keyPageDown
	keyEnter
	keyCancel
	keyDigit // keyEvent.digit holds 1-9
)

type keyEvent struct {
	key   menuKey
	digit int
}

// isTerminalStdin reports whether stdin and stdout are both real terminals.
func isTerminalStdin() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// translateKey maps a keyboard library key press to a menu key event.
func translateKey(k keys.Key) keyEvent {
	switch k.Code {
	case keys.Up, keys.CtrlP:
		return keyEvent{key: keyUp}
	case keys.Down, keys.CtrlN:
		return keyEvent{key: keyDown}
	case keys.Home:
		return keyEvent{key: keyHome}
	case keys.End:
		return keyEvent{key: keyEnd}
	case keys.PgUp:
		return keyEvent{key: keyPageUp}
	case keys.PgDown:
		return keyEvent{key: keyPageDown}
	case keys.Enter, keys.Space:
		return keyEvent{key: keyEnter}
	case keys.Esc, keys.CtrlC:
		return keyEvent{key: keyCancel}
	case keys.RuneKey:
		if k.AltPressed || len(k.Runes) != 1 {
			return keyEvent{}
		}
		switch r := k.Runes[0]; {
		case r == 'q' || r == 'Q':
			return keyEvent{key: keyCancel}
		case r == 'k' || r == 'K':
			return keyEvent{key: keyUp}
		case r == 'j' || r == 'J':
			return keyEvent{key: keyDown}
		case r == 'g':
			return keyEvent{key: keyHome}
		case r == 'G':
			return keyEvent{key: keyEnd}
		case r >= '1' && r <= '9':
			return keyEvent{key: keyDigit, digit: int(r - '0')}
		}
	}
	return keyEvent{}
}

// moveSelection applies a navigation key to the selected row.
func moveSelection(selected, count int, ev keyEvent) int {
	switch ev.key {
	case keyUp:
		return (selected - 1 + count) % count
	case keyDown:
		return (selected + 1) % count
	case keyHome:
		return 0
	case keyEnd:
		return count - 1
	case keyPageUp:
		if selected -= 5; selected < 0 {
			selected = 0
		}
	case keyPageDown:
		if selected += 5; selected >= count {
			selected = count - 1
		}
	}
	return selected
}

// runMenu runs an interactive list menu with arrow-key / vim-key navigation.
//
// render draws the whole menu for the given selected row and must be exactly totalLines lines
// tall (each ending in "\r\n"); runMenu moves the cursor back up before every redraw. It
// returns the chosen index, -1 if the user cancelled, or -2 if the terminal cannot be put into
// raw mode (callers should fall back to a plain text prompt).
func runMenu(count, initial, totalLines int, render func(selected int)) int {
	if count <= 0 {
		return -1
	}
	selected := initial
	if selected < 0 || selected >= count {
		selected = 0
	}

	result := -1
	fmt.Print("\033[?25l") // hide cursor
	defer fmt.Print("\033[?25h")
	render(selected)

	err := keyboard.Listen(func(k keys.Key) (bool, error) {
		ev := translateKey(k)
		switch ev.key {
		case keyNone:
			return false, nil
		case keyCancel:
			return true, nil
		case keyEnter:
			result = selected
			return true, nil
		case keyDigit:
			if ev.digit <= count {
				result = ev.digit - 1
				return true, nil
			}
			return false, nil
		}
		if next := moveSelection(selected, count, ev); next != selected {
			selected = next
			fmt.Printf("\033[%dA\r", totalLines)
			render(selected)
		}
		return false, nil
	})
	if err != nil {
		return -2
	}
	return result
}
