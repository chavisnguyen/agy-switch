package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	oauthTokenURL   = "https://oauth2.googleapis.com/token"
	modelsEndpoint  = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	quotaSummaryURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
	userAgent       = "antigravity/1.0.0 darwin/arm64"
	// quotaSummaryUserAgent must look like a recent Antigravity Hub build or the
	// backend silently omits the "Gemini Models" bucket group from the response.
	quotaSummaryUserAgent = "antigravity/hub/2.24.0 (aidev_client; os_type=darwin; arch=arm64; cl=1000335682)"
)

var quotaHTTPClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   4 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			// Prefer IPv4 first to avoid hanging on broken IPv6 routes
			if conn, err := dialer.DialContext(ctx, "tcp4", addr); err == nil {
				return conn, nil
			}
			return dialer.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   4 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

func getOAuthCreds() (string, string) {
	xorKey := byte(0x5A)
	encCID := []byte{107, 106, 109, 107, 106, 106, 108, 106, 108, 106, 111, 99, 107, 119, 46, 55, 50, 41, 41, 51, 52, 104, 50, 104, 107, 54, 57, 40, 63, 104, 105, 111, 44, 46, 53, 54, 53, 48, 50, 110, 61, 110, 106, 105, 63, 42, 116, 59, 42, 42, 41, 116, 61, 53, 53, 61, 54, 63, 47, 41, 63, 40, 57, 53, 52, 46, 63, 52, 46, 116, 57, 53, 55}
	encSec := []byte{29, 21, 25, 9, 10, 2, 119, 17, 111, 98, 28, 13, 8, 110, 98, 108, 22, 62, 22, 16, 107, 55, 22, 24, 98, 41, 2, 25, 110, 32, 108, 43, 30, 27, 60}

	cid := make([]byte, len(encCID))
	for i, b := range encCID {
		cid[i] = b ^ xorKey
	}
	sec := make([]byte, len(encSec))
	for i, b := range encSec {
		sec[i] = b ^ xorKey
	}
	return string(cid), string(sec)
}

type ProfileQuota struct {
	ClaudeRemaining float64
	ClaudeResetTime string
	GeminiRemaining float64
	GeminiResetTime string
	Err             error

	// Separate 5h / weekly buckets from retrieveUserQuotaSummary, best-effort.
	// *HasSummary indicates whether that model's group was present in the response.
	Claude5h         QuotaWindow
	ClaudeWeekly     QuotaWindow
	ClaudeHasSummary bool
	Gemini5h         QuotaWindow
	GeminiWeekly     QuotaWindow
	GeminiHasSummary bool
	Groups           []QuotaGroup
}

// QuotaWindow holds a single quota bucket (either the 5h or the weekly window).
type QuotaWindow struct {
	Remaining float64
	ResetTime string
	// Disabled is set by the server when the window does not currently apply
	// (e.g. the 5h bucket once the weekly limit has been hit).
	Disabled bool
}

type QuotaBucket struct {
	BucketID          string  `json:"bucketId"`
	DisplayName       string  `json:"displayName"`
	Description       string  `json:"description"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
	Disabled          bool    `json:"disabled"`
}

type QuotaGroup struct {
	DisplayName string        `json:"displayName"`
	Description string        `json:"description"`
	Buckets     []QuotaBucket `json:"buckets"`
}

type QuotaSummaryResponse struct {
	Groups []QuotaGroup `json:"groups"`
}

type ModelQuotaInfo struct {
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
}

type ModelDetail struct {
	DisplayName string          `json:"displayName"`
	QuotaInfo   *ModelQuotaInfo `json:"quotaInfo"`
}

type AvailableModelsResponse struct {
	Models map[string]ModelDetail `json:"models"`
}

func getFreshAccessToken(ctx context.Context, refreshToken, fallbackAccessToken string) (string, error) {
	if refreshToken == "" {
		if fallbackAccessToken != "" {
			return fallbackAccessToken, nil
		}
		return "", fmt.Errorf("no refresh_token available")
	}

	data := url.Values{}
	cid, csec := getOAuthCreds()
	data.Set("client_id", cid)
	data.Set("client_secret", csec)
	data.Set("refresh_token", refreshToken)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, "POST", oauthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		if fallbackAccessToken != "" {
			return fallbackAccessToken, nil
		}
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if fallbackAccessToken != "" {
			return fallbackAccessToken, nil
		}
		return "", fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var res struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.AccessToken, nil
}

func fetchProfileQuota(ctx context.Context, tokenFile string) ProfileQuota {
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return ProfileQuota{Err: err}
	}

	var tokFile OAuthTokenFile
	if err := json.Unmarshal(raw, &tokFile); err != nil {
		return ProfileQuota{Err: err}
	}

	accessToken := ""
	usedCachedToken := false
	if tokFile.Token.AccessToken != "" && tokFile.Token.Expiry != "" {
		if exp, err := time.Parse(time.RFC3339, tokFile.Token.Expiry); err == nil {
			if time.Until(exp) > 2*time.Minute {
				accessToken = tokFile.Token.AccessToken
				usedCachedToken = true
			}
		}
	}

	if accessToken == "" {
		freshToken, err := getFreshAccessToken(ctx, tokFile.Token.RefreshToken, tokFile.Token.AccessToken)
		if err != nil {
			return ProfileQuota{Err: err}
		}
		accessToken = freshToken
	}

	// Primary path: retrieveUserQuotaSummary returns both 5h and weekly buckets in
	// a single call (same source used by `agy -p /usage`).
	summary, statusCode, summaryErr := fetchQuotaSummary(ctx, accessToken)
	if statusCode == http.StatusUnauthorized && usedCachedToken && tokFile.Token.RefreshToken != "" {
		if freshToken, err := getFreshAccessToken(ctx, tokFile.Token.RefreshToken, ""); err == nil {
			accessToken = freshToken
			usedCachedToken = false
			summary, statusCode, summaryErr = fetchQuotaSummary(ctx, accessToken)
		}
	}
	if summaryErr == nil && summary != nil {
		var quota ProfileQuota
		applyQuotaSummary(&quota, summary)
		if quota.ClaudeHasSummary || quota.GeminiHasSummary {
			return quota
		}
	}

	// Fallback path: fetchAvailableModels for older accounts or if quota summary is unavailable.
	fallbackQuota := fetchAvailableModelsQuota(ctx, accessToken, tokFile.Token.RefreshToken, usedCachedToken)
	if fallbackQuota.Err != nil && summaryErr != nil {
		return ProfileQuota{Err: summaryErr}
	}
	return fallbackQuota
}

func fetchAvailableModelsQuota(ctx context.Context, accessToken, refreshToken string, allowRetry bool) ProfileQuota {
	req, err := http.NewRequestWithContext(ctx, "POST", modelsEndpoint, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return ProfileQuota{Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		return ProfileQuota{Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && allowRetry && refreshToken != "" {
		if freshToken, err := getFreshAccessToken(ctx, refreshToken, ""); err == nil {
			req2, err2 := http.NewRequestWithContext(ctx, "POST", modelsEndpoint, bytes.NewBuffer([]byte("{}")))
			if err2 == nil {
				req2.Header.Set("Authorization", "Bearer "+freshToken)
				req2.Header.Set("Content-Type", "application/json")
				req2.Header.Set("User-Agent", userAgent)
				if resp2, err3 := quotaHTTPClient.Do(req2); err3 == nil {
					defer resp2.Body.Close()
					resp = resp2
				}
			}
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ProfileQuota{Err: fmt.Errorf("api error (%d): %s", resp.StatusCode, string(body))}
	}

	var modelsResp AvailableModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return ProfileQuota{Err: err}
	}

	var quota ProfileQuota
	if m, ok := modelsResp.Models["claude-sonnet-4-6"]; ok && m.QuotaInfo != nil {
		quota.ClaudeRemaining = m.QuotaInfo.RemainingFraction
		quota.ClaudeResetTime = cleanResetTime(m.QuotaInfo.ResetTime)
	} else if m, ok := modelsResp.Models["claude-opus-4-6-thinking"]; ok && m.QuotaInfo != nil {
		quota.ClaudeRemaining = m.QuotaInfo.RemainingFraction
		quota.ClaudeResetTime = cleanResetTime(m.QuotaInfo.ResetTime)
	} else {
		for id, m := range modelsResp.Models {
			if strings.HasPrefix(id, "claude-") && m.QuotaInfo != nil {
				quota.ClaudeRemaining = m.QuotaInfo.RemainingFraction
				quota.ClaudeResetTime = cleanResetTime(m.QuotaInfo.ResetTime)
				break
			}
		}
	}

	if m, ok := modelsResp.Models["gemini-3.8-flash-tiered"]; ok && m.QuotaInfo != nil {
		quota.GeminiRemaining = m.QuotaInfo.RemainingFraction
		quota.GeminiResetTime = cleanResetTime(m.QuotaInfo.ResetTime)
	} else if m, ok := modelsResp.Models["gemini-2.5-pro"]; ok && m.QuotaInfo != nil {
		quota.GeminiRemaining = m.QuotaInfo.RemainingFraction
		quota.GeminiResetTime = cleanResetTime(m.QuotaInfo.ResetTime)
	} else {
		for id, m := range modelsResp.Models {
			if strings.HasPrefix(id, "gemini-") && m.QuotaInfo != nil {
				quota.GeminiRemaining = m.QuotaInfo.RemainingFraction
				quota.GeminiResetTime = cleanResetTime(m.QuotaInfo.ResetTime)
				break
			}
		}
	}

	return quota
}

// fetchQuotaSummary calls retrieveUserQuotaSummary, which reports the 5h and
// weekly buckets separately for both the Gemini group and the Claude/GPT group.
func fetchQuotaSummary(ctx context.Context, accessToken string) (*QuotaSummaryResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", quotaSummaryURL, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", quotaSummaryUserAgent)

	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("quota summary api error (%d): %s", resp.StatusCode, string(body))
	}

	var summary QuotaSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, resp.StatusCode, err
	}
	return &summary, resp.StatusCode, nil
}

// cleanResetTime drops placeholder epoch timestamps (e.g. 1970-01-01T00:00:00Z)
// returned for untouched quota buckets.
func cleanResetTime(resetStr string) string {
	resetStr = strings.TrimSpace(resetStr)
	if resetStr == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, resetStr); err == nil && t.Year() <= 1970 {
		return ""
	}
	return resetStr
}

func deriveEffectiveWindow(has5h bool, fiveH QuotaWindow, hasWeekly bool, weekly QuotaWindow) (float64, string) {
	switch {
	case has5h && hasWeekly:
		if fiveH.Disabled || weekly.Remaining < 0.05 {
			return weekly.Remaining, weekly.ResetTime
		}
		rem := fiveH.Remaining
		if weekly.Remaining < rem {
			rem = weekly.Remaining
		}
		return rem, fiveH.ResetTime
	case has5h:
		if fiveH.Disabled {
			return 0, fiveH.ResetTime
		}
		return fiveH.Remaining, fiveH.ResetTime
	case hasWeekly:
		return weekly.Remaining, weekly.ResetTime
	default:
		return 0, ""
	}
}

// applyQuotaSummary maps the bucketId values from retrieveUserQuotaSummary
// onto the Claude/Gemini 5h+weekly and effective single-window fields of ProfileQuota.
func applyQuotaSummary(quota *ProfileQuota, summary *QuotaSummaryResponse) {
	quota.Groups = make([]QuotaGroup, 0, len(summary.Groups))
	var hasClaude5h, hasClaudeWeekly, hasGemini5h, hasGeminiWeekly bool
	for _, g := range summary.Groups {
		cleanedGroup := QuotaGroup{
			DisplayName: g.DisplayName,
			Description: g.Description,
			Buckets:     make([]QuotaBucket, 0, len(g.Buckets)),
		}
		groupLower := strings.ToLower(g.DisplayName)
		for _, b := range g.Buckets {
			b.ResetTime = cleanResetTime(b.ResetTime)
			cleanedGroup.Buckets = append(cleanedGroup.Buckets, b)

			w := QuotaWindow{
				Remaining: b.RemainingFraction,
				ResetTime: b.ResetTime,
				Disabled:  b.Disabled,
			}
			switch b.BucketID {
			case "3p-5h":
				quota.Claude5h = w
				hasClaude5h = true
			case "3p-weekly":
				quota.ClaudeWeekly = w
				hasClaudeWeekly = true
			case "gemini-5h":
				quota.Gemini5h = w
				hasGemini5h = true
			case "gemini-weekly":
				quota.GeminiWeekly = w
				hasGeminiWeekly = true
			default:
				if strings.Contains(groupLower, "claude") {
					if b.Window == "5h" {
						quota.Claude5h = w
						hasClaude5h = true
					} else if b.Window == "weekly" {
						quota.ClaudeWeekly = w
						hasClaudeWeekly = true
					}
				} else if strings.Contains(groupLower, "gemini") {
					if b.Window == "5h" {
						quota.Gemini5h = w
						hasGemini5h = true
					} else if b.Window == "weekly" {
						quota.GeminiWeekly = w
						hasGeminiWeekly = true
					}
				}
			}
		}
		if len(cleanedGroup.Buckets) > 0 {
			quota.Groups = append(quota.Groups, cleanedGroup)
		}
	}

	if hasClaude5h || hasClaudeWeekly {
		quota.ClaudeHasSummary = true
		quota.ClaudeRemaining, quota.ClaudeResetTime = deriveEffectiveWindow(hasClaude5h, quota.Claude5h, hasClaudeWeekly, quota.ClaudeWeekly)
	}
	if hasGemini5h || hasGeminiWeekly {
		quota.GeminiHasSummary = true
		quota.GeminiRemaining, quota.GeminiResetTime = deriveEffectiveWindow(hasGemini5h, quota.Gemini5h, hasGeminiWeekly, quota.GeminiWeekly)
	}
}

func fetchAllProfilesQuota(profiles []ProfileInfo, timeout time.Duration) map[string]ProfileQuota {
	results := make(map[string]ProfileQuota)
	var mu sync.Mutex
	var wg sync.WaitGroup

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, p := range profiles {
		wg.Add(1)
		go func(prof ProfileInfo) {
			defer wg.Done()
			q := fetchProfileQuota(ctx, prof.FilePath)
			mu.Lock()
			results[prof.ProfileName] = q
			mu.Unlock()
		}(p)
	}

	wg.Wait()
	return results
}

func formatQuotaBadge(fraction float64, err error) string {
	if err != nil {
		return colorize("N/A", ColorAgyMuted)
	}
	pct := fraction * 100
	str := fmt.Sprintf("%6.2f%%", pct)
	return colorize(str, agyQuotaColor(fraction, false))
}

func agyQuotaColor(remaining float64, disabled bool) string {
	pct := remaining * 100
	if disabled || pct < 10 {
		return ColorAgyCoral
	}
	if pct < 25 {
		return ColorAgyAmber
	}
	return ColorAgyLime
}

// formatAgyUsageBar renders an AGY `/usage`-style solid progress bar:
// `    [█████████████████████████████████░░░░░░░] 81.15%`
// using #bcdf59/#ffd166/#ff6188 for remaining and solid #414042 blocks for the empty track.
func formatAgyUsageBar(remaining float64, disabled bool) string {
	const barWidth = 50
	if disabled {
		remaining = 0
	}
	pct := remaining * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	filled := int(pct/100*float64(barWidth) + 0.5)
	if pct > 0 && filled == 0 {
		filled = 1
	}
	if pct < 99.995 && filled == barWidth {
		filled = barWidth - 1
	}
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	barColor := agyQuotaColor(remaining, disabled)
	filledStr := colorize(strings.Repeat("█", filled), barColor)
	emptyStr := strings.Repeat("░", empty)
	if useColor() {
		emptyStr = colorize(strings.Repeat("█", empty), ColorAgyTrack)
	}

	openBr := colorize("[", ColorAgyText)
	closeBr := colorize("]", ColorAgyText)
	pctStr := colorize(fmt.Sprintf("%.2f%%", pct), ColorAgyMuted)
	return fmt.Sprintf("    %s%s%s%s %s", openBr, filledStr, emptyStr, closeBr, pctStr)
}

// formatAgyRefreshStatus returns the subtitle below an AGY `/usage` progress bar
// (e.g. "Refreshes in 6d 14h 53m", "Refreshes in 53m", or "Quota available") and its color.
func formatAgyRefreshStatus(remaining float64, resetStr string, disabled bool, now time.Time) (string, string) {
	if disabled {
		return "Weekly limit reached", ColorAgyCoral
	}
	color := agyQuotaColor(remaining, false)
	if remaining >= 0.99995 {
		return "Quota available", color
	}
	if resetStr != "" {
		if t, err := time.Parse(time.RFC3339, resetStr); err == nil && t.Year() > 1970 {
			diff := t.Sub(now)
			if diff <= 0 {
				return "Refreshing soon", color
			}
			totalMins := int((diff + 30*time.Second) / time.Minute)
			if totalMins < 1 {
				return "Refreshes in < 1m", color
			}
			days := totalMins / (24 * 60)
			hours := (totalMins % (24 * 60)) / 60
			mins := totalMins % 60
			var parts []string
			if days > 0 {
				parts = append(parts, fmt.Sprintf("%dd", days))
			}
			if hours > 0 {
				parts = append(parts, fmt.Sprintf("%dh", hours))
			}
			if mins > 0 || len(parts) == 0 {
				parts = append(parts, fmt.Sprintf("%dm", mins))
			}
			return "Refreshes in " + strings.Join(parts, " "), color
		}
	}
	if remaining <= 0 {
		return "Limit reached", ColorAgyCoral
	}
	return "Quota available", color
}

func quotaGroupsForDisplay(q ProfileQuota) []QuotaGroup {
	if len(q.Groups) > 0 {
		return q.Groups
	}
	return []QuotaGroup{
		{
			DisplayName: "Gemini Models",
			Buckets: []QuotaBucket{
				{DisplayName: "Limit Remaining", RemainingFraction: q.GeminiRemaining, ResetTime: q.GeminiResetTime},
			},
		},
		{
			DisplayName: "Claude and GPT models",
			Buckets: []QuotaBucket{
				{DisplayName: "Limit Remaining", RemainingFraction: q.ClaudeRemaining, ResetTime: q.ClaudeResetTime},
			},
		},
	}
}

// formatQuotaErrorShort summarizes a quota fetch error into a short tag for UI display.
func formatQuotaErrorShort(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(msg, "Verify your account"):
		return "verify account"
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "timeout"
	case strings.Contains(lower, "no refresh_token"):
		return "no token"
	case strings.Contains(lower, "invalid_grant"):
		return "token expired"
	case strings.Contains(lower, "token refresh failed"):
		return "auth refresh failed"
	case strings.Contains(lower, "api error (401)") || strings.Contains(lower, "api error (403)"):
		return "auth error"
	case strings.Contains(lower, "api error (429)"):
		return "rate limited"
	case strings.Contains(lower, "api error (5"):
		return "server error"
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "connection") || strings.Contains(lower, "dial tcp") || strings.Contains(lower, "network"):
		return "network error"
	default:
		return "unavailable"
	}
}

func formatQuotaReset(resetStr string) string {
	if resetStr == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, resetStr)
	if err != nil {
		return fmt.Sprintf(" (resets %s)", resetStr)
	}

	diff := time.Until(t)
	local := t.Local()

	if diff <= 0 {
		return fmt.Sprintf(" (resets %s [5h reset pending])", local.Format("15:04"))
	}

	// 5-hour cycle threshold: 5h 15m
	if diff <= 5*time.Hour+15*time.Minute {
		h := int(diff.Hours())
		m := int(diff.Minutes()) % 60
		durStr := ""
		if h > 0 {
			durStr = fmt.Sprintf("in %dh%02dm", h, m)
		} else {
			durStr = fmt.Sprintf("in %dm", m)
		}
		return fmt.Sprintf(" (resets %s %s [5h])", local.Format("15:04"), durStr)
	}

	// Weekly Cap (diff > 5h15m)
	days := int((diff + 12*time.Hour).Hours() / 24)
	leftStr := ""
	if days > 1 {
		leftStr = fmt.Sprintf("%dd left", days)
	} else if days == 1 {
		leftStr = "1d left"
	} else {
		leftStr = fmt.Sprintf("%dh left", int(diff.Hours()))
	}
	return fmt.Sprintf(" (resets %s, %s [Weekly Cap ⚠️])", local.Format("02 Jan 15:04"), leftStr)
}

type SmartCandidate struct {
	Profile         ProfileInfo
	Quota           ProfileQuota
	ClaudeRemaining float64
	ClaudeDiff      time.Duration
	IsClaudeWeekly  bool
	GeminiRemaining float64
	GeminiDiff      time.Duration
	IsGeminiWeekly  bool
	IsLocked        bool
	LockReason      string
}

// applyBindingWindow overrides the single-window values with the weekly bucket
// when it is nearly exhausted (< 5%), since that is what actually limits usage.
func applyBindingWindow(remaining *float64, diff *time.Duration, isWeekly *bool, hasSummary bool, fiveH, weekly QuotaWindow, now time.Time) {
	if !hasSummary || weekly.Remaining >= 0.05 {
		return
	}
	*remaining = weekly.Remaining
	*isWeekly = true
	if t, err := time.Parse(time.RFC3339, weekly.ResetTime); err == nil {
		*diff = t.Sub(now)
	}
}

func evaluateProfileCandidate(p ProfileInfo, q ProfileQuota, now time.Time) SmartCandidate {
	c := SmartCandidate{
		Profile:         p,
		Quota:           q,
		ClaudeRemaining: q.ClaudeRemaining,
		GeminiRemaining: q.GeminiRemaining,
	}

	if q.ClaudeResetTime != "" {
		if t, err := time.Parse(time.RFC3339, q.ClaudeResetTime); err == nil {
			c.ClaudeDiff = t.Sub(now)
			if c.ClaudeDiff > 5*time.Hour+15*time.Minute {
				c.IsClaudeWeekly = true
			}
		}
	}

	if q.GeminiResetTime != "" {
		if t, err := time.Parse(time.RFC3339, q.GeminiResetTime); err == nil {
			c.GeminiDiff = t.Sub(now)
			if c.GeminiDiff > 5*time.Hour+15*time.Minute {
				c.IsGeminiWeekly = true
			}
		}
	}

	// When the 5h/weekly split is known, an exhausted weekly bucket blocks the
	// model regardless of how full the 5h bucket looks.
	applyBindingWindow(&c.ClaudeRemaining, &c.ClaudeDiff, &c.IsClaudeWeekly, q.ClaudeHasSummary, q.Claude5h, q.ClaudeWeekly, now)
	applyBindingWindow(&c.GeminiRemaining, &c.GeminiDiff, &c.IsGeminiWeekly, q.GeminiHasSummary, q.Gemini5h, q.GeminiWeekly, now)

	if p.IsCoolingDown {
		c.IsLocked = true
		c.LockReason = fmt.Sprintf("Cooldown (%s left)", p.CooldownRemain)
	} else if c.IsClaudeWeekly && c.ClaudeRemaining < 0.05 && (c.IsGeminiWeekly && c.GeminiRemaining < 0.05) {
		c.IsLocked = true
		c.LockReason = "Weekly Cap locked (0% quota)"
	} else if c.ClaudeRemaining < 0.05 && c.GeminiRemaining < 0.05 {
		c.IsLocked = true
		c.LockReason = "Quota depleted (< 5%)"
	}

	return c
}

// selectSmartProfile picks the best profile to switch to or burn.
// If excludeCurrent is true, it only considers non-active profiles.
func selectSmartProfile(profiles []ProfileInfo, activeProfile string, excludeCurrent bool) (string, string) {
	name, reason, _ := selectSmartProfileDetailed(profiles, activeProfile, excludeCurrent)
	return name, reason
}

// exhaustionInfo reports whether every considered account is out of quota.
// All is only true when every account's quota was fetched successfully and none
// has a usable Claude or Gemini bucket; an unreachable account counts as unknown.
type exhaustionInfo struct {
	All         bool
	SoonestName string
	Soonest     time.Duration // time until the first account recovers; 0 when unknown
}

// candidateUsable reports whether any model has quota left, ignoring cooldowns:
// a cooldown is a local guess, the quota is what the server actually enforces.
func candidateUsable(c SmartCandidate) bool {
	return c.ClaudeRemaining >= 0.05 || c.GeminiRemaining >= 0.05
}

// candidateRecovery is the shortest time until one of the candidate's exhausted
// windows resets, or 0 when no reset time is known.
func candidateRecovery(c SmartCandidate) time.Duration {
	var best time.Duration
	for _, d := range []time.Duration{c.ClaudeDiff, c.GeminiDiff} {
		if d > 0 && (best == 0 || d < best) {
			best = d
		}
	}
	return best
}

func selectSmartProfileDetailed(profiles []ProfileInfo, activeProfile string, excludeCurrent bool) (string, string, exhaustionInfo) {
	var ex exhaustionInfo
	if len(profiles) <= 1 {
		return "", "", ex
	}

	quotas := fetchAllProfilesQuota(profiles, 8*time.Second)
	if len(quotas) == 0 {
		return "", "", ex
	}

	now := time.Now()
	var candidates []SmartCandidate
	unknown := false
	for _, p := range profiles {
		if excludeCurrent && p.ProfileName == activeProfile {
			continue
		}
		q := quotas[p.ProfileName]
		if q.Err != nil {
			unknown = true
			continue
		}
		cand := evaluateProfileCandidate(p, q, now)
		candidates = append(candidates, cand)
	}

	if len(candidates) == 0 {
		return "", "", ex
	}

	if !unknown {
		ex.All = true
		for _, c := range candidates {
			if candidateUsable(c) {
				ex.All = false
				break
			}
			if d := candidateRecovery(c); d > 0 && (ex.Soonest == 0 || d < ex.Soonest) {
				ex.Soonest = d
				ex.SoonestName = c.Profile.ProfileName
			}
		}
	}

	// 1. Check for URGENT BURN (Claude expiring within 90 minutes with remaining >= 10%)
	var urgentCand *SmartCandidate
	for i := range candidates {
		c := &candidates[i]
		if c.IsLocked || c.IsClaudeWeekly || c.ClaudeRemaining < 0.10 {
			continue
		}
		if c.ClaudeDiff > 0 && c.ClaudeDiff <= 90*time.Minute {
			if urgentCand == nil || c.ClaudeDiff < urgentCand.ClaudeDiff {
				urgentCand = c
			}
		}
	}
	if urgentCand != nil {
		m := int(urgentCand.ClaudeDiff.Minutes())
		reason := fmt.Sprintf("🔥 Claude %.1f%% expiring soonest (in %dm [5h]) - burn before reset!", urgentCand.ClaudeRemaining*100, m)
		return urgentCand.Profile.ProfileName, reason, ex
	}

	// 2. Normal 5h cycle Claude candidates (healthy, !IsClaudeWeekly, ClaudeRemaining >= 0.05)
	var best5h *SmartCandidate
	for i := range candidates {
		c := &candidates[i]
		if c.IsLocked || c.IsClaudeWeekly || c.ClaudeRemaining < 0.05 {
			continue
		}
		if best5h == nil {
			best5h = c
			continue
		}
		// If reset diff is significantly closer (> 30m difference), pick closer one to burn
		if best5h.ClaudeDiff-c.ClaudeDiff > 30*time.Minute {
			best5h = c
		} else if c.ClaudeDiff-best5h.ClaudeDiff <= 30*time.Minute {
			// Similar reset window: prefer higher quota to balance consumption and protect weekly cap
			if c.ClaudeRemaining > best5h.ClaudeRemaining {
				best5h = c
			}
		}
	}
	if best5h != nil {
		h := int(best5h.ClaudeDiff.Hours())
		m := int(best5h.ClaudeDiff.Minutes()) % 60
		durStr := fmt.Sprintf("%dh%02dm", h, m)
		reason := fmt.Sprintf("Claude %.1f%% in [5h] cycle (resets in %s) - optimal balance", best5h.ClaudeRemaining*100, durStr)
		return best5h.Profile.ProfileName, reason, ex
	}

	// 3. Fallback to Gemini candidates if Claude is exhausted across accounts
	var bestGemini *SmartCandidate
	for i := range candidates {
		c := &candidates[i]
		if c.IsLocked || c.IsGeminiWeekly || c.GeminiRemaining < 0.05 {
			continue
		}
		if bestGemini == nil || c.GeminiRemaining > bestGemini.GeminiRemaining {
			bestGemini = c
		}
	}
	if bestGemini != nil {
		reason := fmt.Sprintf("Gemini %.1f%% available (Claude depleted)", bestGemini.GeminiRemaining*100)
		return bestGemini.Profile.ProfileName, reason, ex
	}

	return "", "", ex
}

// fetchRawQuotaSummary returns the unparsed retrieveUserQuotaSummary body for
// a token file, pretty-printed when it is valid JSON. Debug aid for `agys quota-raw`.
func fetchRawQuotaSummary(ctx context.Context, tokenFile string) (string, error) {
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", err
	}
	var tf OAuthTokenFile
	if err := json.Unmarshal(raw, &tf); err != nil {
		return "", err
	}
	accessToken, err := getFreshAccessToken(ctx, tf.Token.RefreshToken, tf.Token.AccessToken)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", quotaSummaryURL, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", quotaSummaryUserAgent)
	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api error (%d): %s", resp.StatusCode, string(body))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		return pretty.String(), nil
	}
	return string(body), nil
}
