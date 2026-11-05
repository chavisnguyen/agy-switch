package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

type menuKey int

const (
	keyUp menuKey = iota + 1
	keyDown
	keyHome
	keyEnd
	keyPageUp
	keyPageDown
	keyEnter
	keyCancel
	keyDigit // key.digit holds 1-9
)

type keyEvent struct {
	key   menuKey
	digit int
}

// parseKeys decodes raw terminal input into key events. It handles several keys arriving in
// one read (key repeat, pasted input) and unknown escape sequences (swallowed, never mistaken
// for Esc). An escape sequence cut off at the end of buf is returned in rest so the caller can
// prepend it to the next read. If timedOut is true no more input is coming, so a trailing lone
// Esc is a real Esc key and a trailing partial sequence is dropped.
func parseKeys(buf []byte, timedOut bool) (events []keyEvent, rest []byte) {
	for i := 0; i < len(buf); {
		b := buf[i]
		if b == 0x1b {
			if i+1 >= len(buf) {
				if timedOut {
					events = append(events, keyEvent{key: keyCancel})
					return events, nil
				}
				return events, buf[i:]
			}
			intro := buf[i+1]
			if intro != '[' && intro != 'O' {
				i += 2 // Alt+<key>: ignore
				continue
			}
			// CSI ("ESC [ params final") or SS3 ("ESC O final")
			j := i + 2
			for j < len(buf) && buf[j] >= 0x20 && buf[j] <= 0x3f {
				j++
			}
			if j >= len(buf) {
				if timedOut {
					return events, nil
				}
				return events, buf[i:]
			}
			params := string(buf[i+2 : j])
			switch buf[j] {
			case 'A':
				events = append(events, keyEvent{key: keyUp})
			case 'B':
				events = append(events, keyEvent{key: keyDown})
			case 'H':
				events = append(events, keyEvent{key: keyHome})
			case 'F':
				events = append(events, keyEvent{key: keyEnd})
			case '~':
				switch params {
				case "1", "7":
					events = append(events, keyEvent{key: keyHome})
				case "4", "8":
					events = append(events, keyEvent{key: keyEnd})
				case "5":
					events = append(events, keyEvent{key: keyPageUp})
				case "6":
					events = append(events, keyEvent{key: keyPageDown})
				}
			}
			i = j + 1
			continue
		}

		switch {
		case b == 3, b == 'q', b == 'Q': // Ctrl+C, q
			events = append(events, keyEvent{key: keyCancel})
		case b == 10, b == 13, b == ' ':
			events = append(events, keyEvent{key: keyEnter})
		case b == 16, b == 'k', b == 'K': // Ctrl+P, vim up
			events = append(events, keyEvent{key: keyUp})
		case b == 14, b == 'j', b == 'J': // Ctrl+N, vim down
			events = append(events, keyEvent{key: keyDown})
		case b == 'g':
			events = append(events, keyEvent{key: keyHome})
		case b == 'G':
			events = append(events, keyEvent{key: keyEnd})
		case b >= '1' && b <= '9':
			events = append(events, keyEvent{key: keyDigit, digit: int(b - '0')})
		}
		i++
	}
	return events, nil
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

	restore, err := enterRawMode()
	if err != nil {
		return -2
	}
	defer func() {
		restore()
		fmt.Print("\033[?25h") // show cursor
	}()

	// Guarantee a clean terminal on OS interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	go func() {
		<-sigChan
		restore()
		fmt.Print("\033[?25h\r\n")
		os.Exit(1)
	}()

	fmt.Print("\033[?25l") // hide cursor
	render(selected)

	buf := make([]byte, 64)
	var pending []byte
	for {
		n, err := os.Stdin.Read(buf)
		timedOut := false
		if err != nil {
			if err != io.EOF {
				return -1
			}
			timedOut = true // VMIN=0 read timeout (no input for 100ms)
		}
		if n == 0 {
			timedOut = true
		}
		pending = append(pending, buf[:n]...)
		if len(pending) == 0 {
			continue
		}

		var events []keyEvent
		events, pending = parseKeys(pending, timedOut)

		before := selected
		for _, ev := range events {
			switch ev.key {
			case keyCancel:
				return -1
			case keyEnter:
				return selected
			case keyDigit:
				if ev.digit <= count {
					return ev.digit - 1
				}
			case keyUp:
				selected = (selected - 1 + count) % count
			case keyDown:
				selected = (selected + 1) % count
			case keyHome:
				selected = 0
			case keyEnd:
				selected = count - 1
			case keyPageUp:
				if selected -= 5; selected < 0 {
					selected = 0
				}
			case keyPageDown:
				if selected += 5; selected >= count {
					selected = count - 1
				}
			}
		}
		if selected != before {
			fmt.Printf("\033[%dA\r", totalLines)
			render(selected) // one redraw per batch, however many keys arrived
		}
	}
}
