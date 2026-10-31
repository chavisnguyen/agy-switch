package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ConversationSummary struct {
	ConversationID   string `json:"conversation_id"`
	Title            string `json:"title"`
	StepCount        int    `json:"step_count"`
	LastModifiedTime string `json:"last_modified_time"`
	WorkspaceURIs    string `json:"workspace_uris,omitempty"`
}

func parseConversationTime(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

func formatRelativeTime(isoStr string) string {
	t, err := parseConversationTime(isoStr)
	if err != nil {
		if len(isoStr) >= 16 {
			return isoStr[:16]
		}
		return isoStr
	}

	if t.IsZero() || t.Year() <= 1 {
		return "-"
	}

	diff := time.Since(t)
	if diff < 0 {
		return "Just now"
	}
	if diff < time.Minute {
		return "Just now"
	}
	if diff < time.Hour {
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	}
	if diff < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	}
	if diff < 48*time.Hour {
		return "Yesterday"
	}
	if diff < 7*24*time.Hour {
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	}
	return t.Local().Format("02 Jan 15:04")
}

func getFolderConversations(targetDir string, isAll bool, limit int) ([]ConversationSummary, string, error) {
	cliHome, _, _, _, _, _, _ := getPaths()
	dbPath := filepath.Join(cliHome, "conversation_summaries.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, "", fmt.Errorf("conversation database not found at %s", dbPath)
	}

	// 1. Get active conversation ID from last_conversations.json
	activeID := ""
	cachePath := filepath.Join(cliHome, "cache", "last_conversations.json")
	if data, err := os.ReadFile(cachePath); err == nil {
		var m map[string]string
		if json.Unmarshal(data, &m) == nil {
			if id, ok := m[targetDir]; ok {
				activeID = id
			}
		}
	}

	if limit <= 0 {
		limit = 20
	}

	var query string
	if isAll {
		query = fmt.Sprintf("SELECT conversation_id, title, step_count, last_modified_time, workspace_uris FROM conversation_summaries WHERE conversation_id != '' ORDER BY last_modified_time DESC LIMIT %d;", limit)
	} else {
		cleanDir := strings.ReplaceAll(targetDir, "'", "''")
		query = fmt.Sprintf("SELECT conversation_id, title, step_count, last_modified_time, workspace_uris FROM conversation_summaries WHERE workspace_uris LIKE '%%%s%%' AND conversation_id != '' ORDER BY last_modified_time DESC LIMIT %d;", cleanDir, limit)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sqlite3", "-json", dbPath, query)
	out, err := cmd.Output()
	if err != nil {
		return nil, activeID, fmt.Errorf("sqlite3 query failed: %w", err)
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, activeID, nil
	}

	var list []ConversationSummary
	if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
		return nil, activeID, fmt.Errorf("failed to parse conversations json: %w", err)
	}

	return list, activeID, nil
}

func truncateString(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

func getSttyState() string {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func restoreSttyState(state string) {
	if state != "" {
		cmd := exec.Command("stty", state)
		cmd.Stdin = os.Stdin
		_ = cmd.Run()
	} else {
		cmd := exec.Command("stty", "sane")
		cmd.Stdin = os.Stdin
		_ = cmd.Run()
	}
}

func isTerminalStdin() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func selectConversationInteractive(convs []ConversationSummary, activeID string, isAll bool, targetDir string) int {
	if len(convs) == 0 {
		return -1
	}

	selectedIdx := 0
	if activeID != "" {
		for i, c := range convs {
			if c.ConversationID == activeID {
				selectedIdx = i
				break
			}
		}
	}

	savedState := getSttyState()
	defer func() {
		restoreSttyState(savedState)
		fmt.Print("\033[?25h") // Show cursor
	}()

	// Guarantee clean restore on OS interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	go func() {
		<-sigChan
		restoreSttyState(savedState)
		fmt.Print("\033[?25h\r\n")
		os.Exit(1)
	}()

	// Raw mode: non-canonical, no echo, min 1 byte, 100ms timeout
	_ = exec.Command("stty", "-icanon", "-echo", "min", "1", "time", "1").Run()
	fmt.Print("\033[?25l") // Hide cursor

	totalLines := len(convs) + 7

	render := func(firstRender bool) {
		if !firstRender {
			fmt.Printf("\033[%dA\r", totalLines)
		}

		if isAll {
			fmt.Print(colorize("💬 ALL RECENT AGY CONVERSATIONS", ColorBold+ColorCyan) + "\033[K\r\n")
		} else {
			fmt.Print(colorize(fmt.Sprintf("💬 CONVERSATIONS FOR: %s", targetDir), ColorBold+ColorCyan) + "\033[K\r\n")
		}
		fmt.Print(colorize(strings.Repeat("═", 92), ColorDim) + "\033[K\r\n")

		header := fmt.Sprintf("  %-5s %-38s %-7s %-18s %-18s", "ID", "TITLE", "STEPS", "LAST ACTIVE", "CONV ID")
		fmt.Print(header + "\033[K\r\n")
		fmt.Print(colorize(strings.Repeat("─", 92), ColorDim) + "\033[K\r\n")

		for idx, c := range convs {
			isSelected := (idx == selectedIdx)
			isActive := (activeID != "" && c.ConversationID == activeID) || (!isAll && activeID == "" && idx == 0)

			numTag := fmt.Sprintf("[%d]", idx+1)
			title := c.Title
			if title == "" {
				title = "(Untitled conversation)"
			}
			title = truncateString(title, 36)
			relTime := formatRelativeTime(c.LastModifiedTime)

			shortUUID := c.ConversationID
			if len(shortUUID) > 13 {
				shortUUID = shortUUID[:8] + "…"
			}

			activeTag := " "
			if isActive {
				activeTag = colorize("●", ColorGreen)
			}

			pointer := "  "
			if isSelected {
				pointer = colorize("❯ ", ColorBold+ColorCyan)
				numTag = colorize(numTag, ColorBold+ColorCyan)
				title = colorize(title, ColorBold)
			} else {
				numTag = colorize(numTag, ColorDim)
			}

			stepStr := fmt.Sprintf("%d", c.StepCount)
			row := fmt.Sprintf("%s%-5s %-38s %-7s %-18s %-18s%s",
				pointer,
				numTag,
				title,
				stepStr,
				relTime,
				shortUUID,
				activeTag,
			)
			fmt.Print(row + "\033[K\r\n")
		}

		fmt.Print(colorize(strings.Repeat("─", 92), ColorDim) + "\033[K\r\n")
		if activeID != "" {
			fmt.Print(colorize("● indicates current active session for this folder", ColorDim) + "\033[K\r\n")
		} else {
			fmt.Print("\033[K\r\n")
		}

		helpText := fmt.Sprintf("%s • %s • %s • %s",
			colorize("↑/↓ or j/k: navigate", ColorCyan),
			colorize("Enter: resume", ColorBold+ColorGreen),
			colorize("1-9: jump", ColorYellow),
			colorize("q/Esc: cancel", ColorDim),
		)
		fmt.Print(helpText + "\033[K\r\n")
	}

	render(true)

	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return -1
		}
		if n == 0 {
			continue
		}

		// Single byte key
		if n == 1 {
			b := buf[0]
			switch b {
			case 3, 27, 'q', 'Q': // Ctrl+C, Esc, q
				return -1
			case 10, 13, ' ': // Enter, Space
				return selectedIdx
			case 'k', 'K': // vim up
				if selectedIdx > 0 {
					selectedIdx--
				} else {
					selectedIdx = len(convs) - 1
				}
				render(false)
			case 'j', 'J': // vim down
				if selectedIdx < len(convs)-1 {
					selectedIdx++
				} else {
					selectedIdx = 0
				}
				render(false)
			case 'g': // top
				selectedIdx = 0
				render(false)
			case 'G': // bottom
				selectedIdx = len(convs) - 1
				render(false)
			default:
				if b >= '1' && b <= '9' {
					num := int(b - '0')
					if num <= len(convs) {
						return num - 1 // immediate select
					}
				}
			}
			continue
		}

		// Multi-byte sequence (arrow keys, home, end)
		if buf[0] == 27 {
			if n >= 3 && (buf[1] == '[' || buf[1] == 'O') {
				switch buf[2] {
				case 'A': // Up
					if selectedIdx > 0 {
						selectedIdx--
					} else {
						selectedIdx = len(convs) - 1
					}
					render(false)
				case 'B': // Down
					if selectedIdx < len(convs)-1 {
						selectedIdx++
					} else {
						selectedIdx = 0
					}
					render(false)
				case 'H': // Home
					selectedIdx = 0
					render(false)
				case 'F': // End
					selectedIdx = len(convs) - 1
					render(false)
				}
			}
		}
	}
}

func printConversationsTable(convs []ConversationSummary, activeID string, isAll bool, targetDir string) {
	fmt.Println()
	if isAll {
		fmt.Println(colorize("💬 ALL RECENT AGY CONVERSATIONS", ColorBold+ColorCyan))
	} else {
		fmt.Println(colorize(fmt.Sprintf("💬 CONVERSATIONS FOR: %s", targetDir), ColorBold+ColorCyan))
	}
	fmt.Println(colorize(strings.Repeat("═", 92), ColorDim))

	if len(convs) == 0 {
		fmt.Println(colorize("No conversations found.", ColorYellow))
		fmt.Println("Tip: Start a new conversation in this directory with " + colorize("agys run", ColorGreen))
		fmt.Println()
		return
	}

	header := fmt.Sprintf("  %-5s %-38s %-7s %-18s %-18s", "ID", "TITLE", "STEPS", "LAST ACTIVE", "CONV ID")
	fmt.Println(header)
	fmt.Println(colorize(strings.Repeat("─", 92), ColorDim))

	for idx, c := range convs {
		numTag := fmt.Sprintf("[%d]", idx+1)
		title := c.Title
		if title == "" {
			title = "(Untitled conversation)"
		}
		title = truncateString(title, 36)

		relTime := formatRelativeTime(c.LastModifiedTime)
		isActive := (activeID != "" && c.ConversationID == activeID) || (!isAll && activeID == "" && idx == 0)

		activeTag := ""
		if isActive {
			activeTag = colorize(" ●", ColorGreen)
			numTag = colorize(numTag, ColorBold+ColorGreen)
			title = colorize(title, ColorBold)
		} else {
			numTag = colorize(numTag, ColorCyan)
		}

		shortUUID := c.ConversationID
		if len(shortUUID) > 13 {
			shortUUID = shortUUID[:8] + "…"
		}

		stepStr := fmt.Sprintf("%d", c.StepCount)
		row := fmt.Sprintf("  %-5s %-38s %-7s %-18s %-18s%s",
			numTag,
			title,
			stepStr,
			relTime,
			shortUUID,
			activeTag,
		)
		fmt.Println(row)
	}

	fmt.Println(colorize(strings.Repeat("─", 92), ColorDim))
	if activeID != "" {
		fmt.Println(colorize("● indicates current active session for this folder", ColorDim))
	}
	fmt.Println()
}

func cmdConversations(args []string) {
	targetDir, _ := os.Getwd()
	listOnly := false
	isAll := false
	directChoice := -1

	for _, a := range args {
		if a == "-l" || a == "--list" {
			listOnly = true
		} else if a == "-a" || a == "--all" {
			isAll = true
		} else if num, err := strconv.Atoi(a); err == nil && num > 0 {
			directChoice = num
		} else if strings.HasPrefix(a, "~/") {
			home, _ := os.UserHomeDir()
			targetDir = filepath.Join(home, a[2:])
		} else if fi, err := os.Stat(a); err == nil && fi.IsDir() {
			if abs, err := filepath.Abs(a); err == nil {
				targetDir = abs
			}
		} else if !strings.HasPrefix(a, "-") {
			targetDir = a
		}
	}

	if !isAll {
		targetDir = strings.TrimRight(targetDir, "/")
	}

	convs, activeID, err := getFolderConversations(targetDir, isAll, 20)
	if err != nil {
		fmt.Printf(colorize("❌ Error fetching conversations: %v\n", ColorRed), err)
		return
	}

	if len(convs) == 0 {
		printConversationsTable(convs, activeID, isAll, targetDir)
		return
	}

	// 1. Direct argument passed (e.g. `agys conv 2`)
	if directChoice > 0 {
		if directChoice <= len(convs) {
			selected := convs[directChoice-1]
			fmt.Println()
			fmt.Println(colorize(fmt.Sprintf("🚀 Resuming [%d]: %s", directChoice, selected.Title), ColorBold+ColorGreen))
			cmdRun([]string{"--conversation=" + selected.ConversationID})
			return
		}
		fmt.Println(colorize(fmt.Sprintf("❌ Invalid choice #%d. Range is 1-%d.", directChoice, len(convs)), ColorRed))
		return
	}

	// 2. List only
	if listOnly {
		printConversationsTable(convs, activeID, isAll, targetDir)
		return
	}

	// 3. Interactive arrow navigation if terminal
	if isTerminalStdin() {
		choiceIdx := selectConversationInteractive(convs, activeID, isAll, targetDir)
		if choiceIdx < 0 {
			fmt.Println(colorize("Cancelled.", ColorYellow))
			return
		}
		selected := convs[choiceIdx]
		fmt.Println(colorize(fmt.Sprintf("🚀 Resuming [%d]: %s", choiceIdx+1, selected.Title), ColorBold+ColorGreen))
		cmdRun([]string{"--conversation=" + selected.ConversationID})
		return
	}

	// 4. Fallback for non-TTY / pipe: plain text prompt
	printConversationsTable(convs, activeID, isAll, targetDir)
	fmt.Printf("%s (or press Enter to cancel): ", colorize(fmt.Sprintf("👉 Enter number to resume (1-%d)", len(convs)), ColorBold+ColorCyan))

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" || strings.ToLower(input) == "q" || strings.ToLower(input) == "exit" || strings.ToLower(input) == "cancel" {
		fmt.Println("Cancelled.")
		return
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(convs) {
		fmt.Println(colorize("❌ Invalid selection.", ColorRed))
		return
	}

	selected := convs[choice-1]
	fmt.Println()
	fmt.Println(colorize(fmt.Sprintf("🚀 Resuming [%d]: %s", choice, selected.Title), ColorBold+ColorGreen))
	cmdRun([]string{"--conversation=" + selected.ConversationID})
}
