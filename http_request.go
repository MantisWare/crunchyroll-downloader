package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0"

// maxAuthRetries bounds how many times one request may be replayed after a
// 401. Without a bound, a token the server keeps rejecting (revoked etp_rt
// cookie, or a rate-limited token endpoint handing back nothing usable) turns
// into an endless refresh loop that never makes progress.
const maxAuthRetries = 3

// sharedTransport bounds the connect and response phases so a silent server
// cannot wedge a download forever. Total transfer time is deliberately left
// unbounded: media segments can legitimately take a while on a slow link.
var sharedTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   15 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConns:          100,
	// The default of 2 forces constant reconnects while the segment workers run.
	MaxIdleConnsPerHost: maxWorkers + 4,
	ForceAttemptHTTP2:   true,
}

// mediaClient downloads manifests, segments and subtitles.
var mediaClient = &http.Client{Transport: sharedTransport}

// apiClient talks to the small JSON and auth endpoints, where a hard overall
// deadline is safe to apply.
var apiClient = &http.Client{
	Transport: sharedTransport,
	Timeout:   60 * time.Second,
}

// refreshMu collapses concurrent 401s into a single token fetch. The token
// endpoint is rate-limited, and a stampede against it is what escalates one
// expired token into a run of unusable ones.
var refreshMu sync.Mutex

// refreshToken swaps out the token that just got rejected. If another
// goroutine already replaced it while we waited for the lock, that result is
// reused instead of spending another call on the token endpoint.
func refreshToken(rejected string) (string, error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()

	if latest := currentToken(); latest != "" && latest != rejected {
		return latest, nil
	}

	if err := RefreshAccessToken(*etpRt); err != nil {
		return "", err
	}
	return currentToken(), nil
}

// bearerToken reads the token a request was signed with.
func bearerToken(req *http.Request) string {
	return strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
}

// replayable clones a request so it can be sent again, restoring the body for
// methods that carry one.
func replayable(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.Body == nil || req.Body == http.NoBody {
		return clone, nil
	}
	if req.GetBody == nil {
		return nil, fmt.Errorf("request body cannot be replayed")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("rewinding request body: %w", err)
	}
	clone.Body = body
	return clone, nil
}

// DoRequest sends an authenticated request, refreshing the access token and
// replaying the request when Crunchyroll rejects it as unauthorized.
func DoRequest(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	// If the stored token is already past its refresh point, renew it now
	// rather than spending a doomed request to discover the same thing.
	if bearerToken(req) != "" && tokenIsStale() {
		fresh, err := refreshToken(bearerToken(req))
		if err != nil {
			return nil, fmt.Errorf("refreshing access token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+fresh)
	}

	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return nil, errCancelled
		}

		resp, err := apiClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, errCancelled
			}
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}

		// Close the rejected response before retrying, otherwise every loop
		// strands its connection instead of returning it to the pool.
		rejected := bearerToken(req)
		resp.Body.Close()

		if attempt >= maxAuthRetries {
			return nil, fmt.Errorf(
				"still unauthorized after %d token refreshes: the etp_rt cookie is probably expired or revoked, sign in again",
				maxAuthRetries,
			)
		}

		retry, err := replayable(req)
		if err != nil {
			return nil, fmt.Errorf("cannot retry %s %s after 401: %w", req.Method, req.URL, err)
		}

		// Back off before the second and later refreshes. Hammering the token
		// endpoint is what turns a single expired token into a rate-limited
		// stream of rejections.
		if attempt > 0 && !sleepOrCancel(time.Duration(attempt)*3*time.Second) {
			return nil, errCancelled
		}

		fmt.Printf("Access token expired. Refetching one (attempt %d/%d)...\n", attempt+1, maxAuthRetries)
		fresh, err := refreshToken(rejected)
		if err != nil {
			return nil, fmt.Errorf("refreshing access token: %w", err)
		}

		retry.Header.Set("Authorization", "Bearer "+fresh)
		req = retry
	}
}
