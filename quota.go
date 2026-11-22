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
	Timeout: 6 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   3 * time.Second,
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
		TLSHandshakeTimeout:   3 * time.Second,
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
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
	Disabled          bool    `json:"disabled"`
}

type QuotaGroup struct {
	DisplayName string        `json:"displayName"`
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
	if tokFile.Token.AccessToken != "" && tokFile.Token.Expiry != "" {
		if exp, err := time.Parse(time.RFC3339, tokFile.Token.Expiry); err == nil {
			if time.Until(exp) > 2*time.Minute {
				accessToken = tokFile.Token.AccessToken
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

	if resp.StatusCode == http.StatusUnauthorized && tokFile.Token.RefreshToken != "" {
		// Retry once with refreshed access token
		if freshToken, err := getFreshAccessToken(ctx, tokFile.Token.RefreshToken, ""); err == nil {
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
	// Claude quota: check claude-sonnet-4-6 or claude-opus-4-6-thinking
	if m, ok := modelsResp.Models["claude-sonnet-4-6"]; ok && m.QuotaInfo != nil {
		quota.ClaudeRemaining = m.QuotaInfo.RemainingFraction
		quota.ClaudeResetTime = m.QuotaInfo.ResetTime
	} else if m, ok := modelsResp.Models["claude-opus-4-6-thinking"]; ok && m.QuotaInfo != nil {
		quota.ClaudeRemaining = m.QuotaInfo.RemainingFraction
		quota.ClaudeResetTime = m.QuotaInfo.ResetTime
	}

	// Gemini quota: check gemini-3.8-flash-tiered or gemini-2.5-pro
	if m, ok := modelsResp.Models["gemini-3.8-flash-tiered"]; ok && m.QuotaInfo != nil {
		quota.GeminiRemaining = m.QuotaInfo.RemainingFraction
		quota.GeminiResetTime = m.QuotaInfo.ResetTime
	} else if m, ok := modelsResp.Models["gemini-2.5-pro"]; ok && m.QuotaInfo != nil {
		quota.GeminiRemaining = m.QuotaInfo.RemainingFraction
		quota.GeminiResetTime = m.QuotaInfo.ResetTime
	}

	// Best-effort: fetch the explicit 5h/weekly bucket split. If it fails (older
	// account type, network hiccup, etc.) the fields above already carry a usable
	// single-window fallback, so we don't surface this as a hard error.
	if summary, err := fetchQuotaSummary(ctx, accessToken); err == nil {
		applyQuotaSummary(&quota, summary)
	}

	return quota
}

// fetchQuotaSummary calls retrieveUserQuotaSummary, which (unlike
// fetchAvailableModels) reports the 5h and weekly buckets separately for both
// the Gemini group and the Claude/GPT group.
func fetchQuotaSummary(ctx context.Context, accessToken string) (*QuotaSummaryResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", quotaSummaryURL, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", quotaSummaryUserAgent)

	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("quota summary api error (%d): %s", resp.StatusCode, string(body))
	}

	var summary QuotaSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

// applyQuotaSummary maps the bucketId values from retrieveUserQuotaSummary
// onto the Claude/Gemini 5h+weekly fields of a ProfileQuota.
func applyQuotaSummary(quota *ProfileQuota, summary *QuotaSummaryResponse) {
	for _, g := range summary.Groups {
		for _, b := range g.Buckets {
			w := QuotaWindow{Remaining: b.RemainingFraction, ResetTime: b.ResetTime, Disabled: b.Disabled}
			switch b.BucketID {
			case "3p-5h":
				quota.Claude5h = w
				quota.ClaudeHasSummary = true
			case "3p-weekly":
				quota.ClaudeWeekly = w
				quota.ClaudeHasSummary = true
			case "gemini-5h":
				quota.Gemini5h = w
				quota.GeminiHasSummary = true
			case "gemini-weekly":
				quota.GeminiWeekly = w
				quota.GeminiHasSummary = true
			}
		}
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
		return colorize("N/A", ColorDim)
	}
	pct := fraction * 100
	str := fmt.Sprintf("%5.1f%%", pct)
	if pct >= 50 {
		return colorize("🟢 "+str, ColorGreen)
	} else if pct >= 10 {
		return colorize("🟡 "+str, ColorYellow)
	}
	return colorize("🔴 "+str, ColorRed)
}

func formatResetTime(resetStr string) string {
	if resetStr == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, resetStr)
	if err != nil {
		return resetStr
	}
	local := t.Local()
	return local.Format("02 Jan 15:04")
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

// formatBucketReset formats a reset timestamp for a bucket whose window
// (5h vs weekly) is already known, instead of inferring it from the time
// remaining (which formatQuotaReset does for the single-window fallback).
func formatBucketReset(resetStr string, isWeekly bool) string {
	if resetStr == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, resetStr)
	if err != nil {
		return fmt.Sprintf(" (resets %s)", resetStr)
	}

	diff := time.Until(t)
	local := t.Local()

	if !isWeekly {
		if diff <= 0 {
			return fmt.Sprintf(" (resets %s [5h])", local.Format("15:04"))
		}
		h := int(diff.Hours())
		m := int(diff.Minutes()) % 60
		durStr := fmt.Sprintf("in %dh%02dm", h, m)
		if h == 0 {
			durStr = fmt.Sprintf("in %dm", m)
		}
		return fmt.Sprintf(" (resets %s %s [5h])", local.Format("15:04"), durStr)
	}

	if diff <= 0 {
		return fmt.Sprintf(" (resets %s [1w])", local.Format("02 Jan 15:04"))
	}
	days := int((diff + 12*time.Hour).Hours() / 24)
	leftStr := ""
	if days > 1 {
		leftStr = fmt.Sprintf("%dd left", days)
	} else if days == 1 {
		leftStr = "1d left"
	} else {
		leftStr = fmt.Sprintf("%dh left", int(diff.Hours()))
	}
	return fmt.Sprintf(" (resets %s, %s [1w])", local.Format("02 Jan 15:04"), leftStr)
}

// formatQuotaBar renders a fixed-width progress bar for a remaining fraction
// (0..1), colored by severity (green/yellow/red), for compact side-by-side
// 5h vs weekly comparison in `agys status`.
func formatQuotaBar(remaining float64) string {
	const barWidth = 20
	pct := remaining * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct/100*float64(barWidth) + 0.5)
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	color := ColorGreen
	if pct < 10 {
		color = ColorRed
	} else if pct < 50 {
		color = ColorYellow
	}
	return colorize(bar, color)
}

// quotaStatusEmoji returns the same severity emoji used by formatQuotaBadge,
// standalone so bar-style lines can place it next to the bar instead of the percentage.
func quotaStatusEmoji(remaining float64) string {
	pct := remaining * 100
	if pct >= 50 {
		return "🟢"
	} else if pct >= 10 {
		return "🟡"
	}
	return "🔴"
}

// formatBucketResetCompact is like formatBucketReset but without the
// "(resets ... [5h/1w])" wrapper, for use next to a bar where the window
// label is already shown separately.
func formatBucketResetCompact(resetStr string, isWeekly bool) string {
	if resetStr == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, resetStr)
	if err != nil {
		return resetStr
	}

	diff := time.Until(t)
	local := t.Local()

	if !isWeekly {
		if diff <= 0 {
			return fmt.Sprintf("reset pending (%s)", local.Format("15:04"))
		}
		h := int(diff.Hours())
		m := int(diff.Minutes()) % 60
		if h > 0 {
			return fmt.Sprintf("in %dh%02dm (%s)", h, m, local.Format("15:04"))
		}
		return fmt.Sprintf("in %dm (%s)", m, local.Format("15:04"))
	}

	if diff <= 0 {
		return fmt.Sprintf("reset pending (%s)", local.Format("02 Jan"))
	}
	if diff >= 24*time.Hour {
		d := int(diff.Hours()) / 24
		h := int(diff.Hours()) % 24
		return fmt.Sprintf("%dd%02dh left (%s)", d, h, local.Format("02 Jan"))
	}
	return fmt.Sprintf("%dh left (%s)", int(diff.Hours()), local.Format("02 Jan"))
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
	if len(profiles) <= 1 {
		return "", ""
	}

	quotas := fetchAllProfilesQuota(profiles, 3500*time.Millisecond)
	if len(quotas) == 0 {
		return "", ""
	}

	now := time.Now()
	var candidates []SmartCandidate
	for _, p := range profiles {
		if excludeCurrent && p.ProfileName == activeProfile {
			continue
		}
		q := quotas[p.ProfileName]
		if q.Err != nil {
			continue
		}
		cand := evaluateProfileCandidate(p, q, now)
		candidates = append(candidates, cand)
	}

	if len(candidates) == 0 {
		return "", ""
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
		return urgentCand.Profile.ProfileName, reason
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
		return best5h.Profile.ProfileName, reason
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
		return bestGemini.Profile.ProfileName, reason
	}

	return "", ""
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
