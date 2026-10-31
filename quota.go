package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	oauthTokenURL  = "https://oauth2.googleapis.com/token"
	modelsEndpoint = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	userAgent      = "antigravity/1.0.0 darwin/arm64"
)

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

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
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

	accessToken, err := getFreshAccessToken(ctx, tokFile.Token.RefreshToken, tokFile.Token.AccessToken)
	if err != nil {
		return ProfileQuota{Err: err}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", modelsEndpoint, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return ProfileQuota{Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ProfileQuota{Err: err}
	}
	defer resp.Body.Close()

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

	return quota
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
	} else if pct > 0 {
		return colorize("🟡 "+str, ColorYellow)
	}
	return colorize("🔴   0.0%", ColorRed)
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
