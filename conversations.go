package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
		limit = 25
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
			// Workspace name or custom directory pattern
			targetDir = a
		}
	}

	if !isAll {
		targetDir = strings.TrimRight(targetDir, "/")
	}

	convs, activeID, err := getFolderConversations(targetDir, isAll, 25)
	if err != nil {
		fmt.Printf(colorize("❌ Error fetching conversations: %v\n", ColorRed), err)
		return
	}

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

	// Table header
	header := fmt.Sprintf("%-5s %-38s %-7s %-18s %-18s", "ID", "TITLE", "STEPS", "LAST ACTIVE", "CONV ID")
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
		row := fmt.Sprintf("%-5s %-38s %-7s %-18s %-18s%s",
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

	// If direct choice was passed in args (e.g. `agys conv 2`)
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

	if listOnly {
		fmt.Println()
		return
	}

	// Interactive prompt
	fmt.Printf("\n%s (or press Enter to cancel): ", colorize(fmt.Sprintf("👉 Enter number to resume (1-%d)", len(convs)), ColorBold+ColorCyan))

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
