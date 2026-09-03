package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var deviceId = uuid.NewString()

// tokenRefreshMargin is how far ahead of the real expiry a token is treated as
// stale. A season download makes many calls over a long stretch, so renewing
// early avoids tripping a 401 partway through.
const tokenRefreshMargin = 60 * time.Second

// defaultTokenLifetime applies when the token endpoint omits expires_in.
const defaultTokenLifetime = 5 * time.Minute

type CrunchyrollTokenResponse struct {
	AccessToken string `json:"access_token"`
	// ExpiresIn is the token lifetime in seconds.
	ExpiresIn int `json:"expires_in"`
	// Error carries the failure code when Crunchyroll rejects the exchange.
	Error string `json:"error"`
}

var (
	tokenMu sync.RWMutex
	token   = ""
	// renewAfter is when the stored token should be replaced, already adjusted
	// by tokenRefreshMargin.
	renewAfter time.Time
)

// currentToken returns the stored access token. Safe for concurrent readers.
func currentToken() string {
	tokenMu.RLock()
	defer tokenMu.RUnlock()
	return token
}

// tokenIsStale reports whether the stored token is missing or due for renewal.
func tokenIsStale() bool {
	tokenMu.RLock()
	defer tokenMu.RUnlock()
	return token == "" || !time.Now().Before(renewAfter)
}

// storeToken records a freshly issued token and when to replace it.
func storeToken(value string, lifetime time.Duration) {
	if lifetime <= 0 {
		lifetime = defaultTokenLifetime
	}

	margin := tokenRefreshMargin
	if margin >= lifetime {
		// Never mark a short-lived token stale the instant it is issued.
		margin = lifetime / 2
	}

	tokenMu.Lock()
	defer tokenMu.Unlock()
	token = value
	renewAfter = time.Now().Add(lifetime - margin)
}

// RefreshAccessToken exchanges the etp_rt cookie for a new access token and
// stores it for subsequent requests. It reports an error rather than panicking
// or yielding an empty token, so a failed exchange surfaces instead of leaving
// callers to retry forever with an unusable token.
func RefreshAccessToken(etpRt string) error {
	if etpRt == "" {
		return fmt.Errorf("missing etp_rt cookie")
	}

	body := url.Values{}
	body.Set("device_id", deviceId)
	body.Set("device_type", "Firefox on Linux")
	body.Set("grant_type", "etp_rt_cookie")

	req, err := http.NewRequest(http.MethodPost, "https://www.crunchyroll.com/auth/v1/token", strings.NewReader(body.Encode()))
	if err != nil {
		return fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Authorization", "Basic bm9haWhkZXZtXzZpeWcwYThsMHE6")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	req.AddCookie(&http.Cookie{Name: "device_id", Value: deviceId})
	req.AddCookie(&http.Cookie{Name: "etp_rt", Value: etpRt})

	resp, err := apiClient.Do(req)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	res, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("token endpoint is rate limiting us (429), wait a few minutes before retrying")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(res)))
	}

	// A rejected exchange still returns valid JSON, so an empty access_token
	// has to be treated as a failure. Accepting it produces a "Bearer " header
	// that is guaranteed to 401.
	var result CrunchyrollTokenResponse
	if err := json.Unmarshal(res, &result); err != nil {
		return fmt.Errorf("parsing token response: %w", err)
	}
	if result.Error != "" {
		return fmt.Errorf("token exchange rejected: %s", result.Error)
	}
	if result.AccessToken == "" {
		return fmt.Errorf("token endpoint returned no access token")
	}

	storeToken(result.AccessToken, time.Duration(result.ExpiresIn)*time.Second)
	return nil
}
