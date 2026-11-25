package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	pageSearchLimit = 20 * time.Second
	pageSearchQuiet = 2 * time.Second
)

var (
	errNoPageVideo        = errors.New("No video found on this page.")
	errPageVideoEncrypted = errors.New("This video is encrypted")
)

type pageMediaCandidate struct {
	URL       string
	FromVideo bool
	Manifest  bool
	File      bool
}

func choosePageMedia(candidates []pageMediaCandidate) (string, error) {
	var video string
	var manifest string
	var file string
	for _, candidate := range candidates {
		if absoluteHTTPURL(candidate.URL) == "" {
			continue
		}
		if candidate.FromVideo {
			video = candidate.URL
			continue
		}
		if candidate.Manifest {
			manifest = candidate.URL
			continue
		}
		if candidate.File {
			file = candidate.URL
		}
	}
	if video != "" {
		return video, nil
	}
	if manifest != "" {
		return manifest, nil
	}
	if file != "" {
		return file, nil
	}
	return "", errNoPageVideo
}

func requestMediaCandidate(rawURL, mime string) (pageMediaCandidate, bool) {
	if absoluteHTTPURL(rawURL) == "" {
		return pageMediaCandidate{}, false
	}
	path := strings.ToLower(rawURL)
	if query := strings.Index(path, "?"); query >= 0 {
		path = path[:query]
	}
	kind := strings.ToLower(strings.TrimSpace(mime))
	switch {
	case strings.HasSuffix(path, ".m3u8") || strings.Contains(kind, "mpegurl"):
		return pageMediaCandidate{URL: rawURL, Manifest: true}, true
	case strings.HasSuffix(path, ".mpd") || strings.Contains(kind, "dash"):
		return pageMediaCandidate{URL: rawURL, Manifest: true}, true
	case strings.HasSuffix(path, ".mp4") || kind == "video/mp4":
		return pageMediaCandidate{URL: rawURL, File: true}, true
	case strings.HasSuffix(path, ".webm") || kind == "video/webm":
		return pageMediaCandidate{URL: rawURL, File: true}, true
	default:
		return pageMediaCandidate{}, false
	}
}

func manifestBodyEncrypted(body string) bool {
	lower := strings.ToLower(body)
	markers := []string{
		"edef8ba9-79d6-4ace-a3c8-27dcd51d21ed",
		"com.widevine",
		"9a04f079-9840-4286-ab92-e65be0885f95",
		"com.microsoft.playready",
		"94ce86fb-07ff-4f43-adb8-93d2fa968ca2",
		"com.apple.streamingkeydelivery",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func searchPageForVideo(ctx context.Context, pageURL string, log io.Writer) (youtubeLookup, error) {
	if ctx != nil && ctx.Err() != nil {
		return youtubeLookup{}, errCancelled
	}
	executable, err := findChromiumExecutable()
	if err != nil {
		return youtubeLookup{}, err
	}
	profileDir, err := os.MkdirTemp("", "video-page-*")
	if err != nil {
		return youtubeLookup{}, fmt.Errorf("creating temporary browser profile: %w", err)
	}
	defer os.RemoveAll(profileDir)

	searchCtx, cancel := context.WithTimeout(ctx, pageSearchLimit+5*time.Second)
	defer cancel()
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(searchCtx, headlessBrowserOptions(executable, profileDir)...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	var (
		mu       sync.Mutex
		requests []pageMediaCandidate
	)
	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		response, ok := ev.(*network.EventResponseReceived)
		if !ok || response.Response == nil {
			return
		}
		candidate, matched := requestMediaCandidate(response.Response.URL, response.Response.MimeType)
		if !matched {
			return
		}
		mu.Lock()
		requests = append(requests, candidate)
		mu.Unlock()
	})

	actions := []chromedp.Action{network.Enable()}
	if path, ok := existingVideoCookieFile(pageURL); ok {
		saved, readErr := readNetscapeCookies(path)
		if readErr != nil {
			return youtubeLookup{}, readErr
		}
		for _, cookie := range saved {
			if cookie == nil || cookie.Name == "" {
				continue
			}
			actions = append(actions, network.SetCookie(cookie.Name, cookie.Value).
				WithDomain(cookie.Domain).
				WithPath(cookie.Path).
				WithSecure(cookie.Secure))
		}
	}
	actions = append(actions, chromedp.Navigate(pageURL))
	if err := chromedp.Run(browserCtx, actions...); err != nil {
		if searchCtx.Err() != nil && ctx != nil && ctx.Err() != nil {
			return youtubeLookup{}, errCancelled
		}
		return youtubeLookup{}, fmt.Errorf("opening the page: %w", err)
	}

	if err := waitForPageMedia(browserCtx, &mu, &requests); err != nil {
		return youtubeLookup{}, err
	}

	var videoURLs []string
	var title string
	err = chromedp.Run(browserCtx,
		chromedp.Evaluate(pageVideoSourceScript, &videoURLs),
		chromedp.Evaluate(`document.title`, &title),
	)
	if err != nil && ctx != nil && ctx.Err() != nil {
		return youtubeLookup{}, errCancelled
	}

	mu.Lock()
	candidates := append([]pageMediaCandidate{}, requests...)
	mu.Unlock()
	for _, videoURL := range videoURLs {
		if absoluteHTTPURL(videoURL) == "" {
			continue
		}
		candidates = append(candidates, pageMediaCandidate{URL: videoURL, FromVideo: true})
	}

	chosen, err := choosePageMedia(candidates)
	if err != nil {
		return youtubeLookup{}, err
	}
	if isManifestURL(chosen) {
		body, fetchErr := fetchManifest(ctx, chosen, pageURL)
		if fetchErr != nil {
			return youtubeLookup{}, fetchErr
		}
		if manifestBodyEncrypted(body) {
			return youtubeLookup{}, errPageVideoEncrypted
		}
	}

	host, hostErr := videoCookieHost(pageURL)
	if hostErr != nil {
		return youtubeLookup{}, hostErr
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = host
	}
	if log != nil {
		fmt.Fprintf(log, "Found video on the page: %s\n", title)
	}
	return youtubeLookup{
		Kind:  youtubeVideo,
		ID:    host,
		Title: title,
		Entries: []youtubeEntry{{
			ID:       host,
			Title:    title,
			MediaURL: chosen,
			PageURL:  pageURL,
		}},
	}, nil
}

const pageVideoSourceScript = `(() => {
  const urls = [];
  for (const video of document.querySelectorAll("video")) {
    if (video.currentSrc) urls.push(video.currentSrc);
    if (video.src) urls.push(video.src);
    for (const source of video.querySelectorAll("source")) {
      if (source.src) urls.push(source.src);
    }
  }
  return urls;
})()`

func waitForPageMedia(ctx context.Context, mu *sync.Mutex, requests *[]pageMediaCandidate) error {
	deadline := time.Now().Add(pageSearchLimit)
	var lastCount int
	var quietSince time.Time
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return errCancelled
		}
		mu.Lock()
		count := len(*requests)
		mu.Unlock()
		if count > 0 && count == lastCount {
			if quietSince.IsZero() {
				quietSince = time.Now()
			}
			if time.Since(quietSince) >= pageSearchQuiet {
				return nil
			}
		} else {
			quietSince = time.Time{}
			lastCount = count
		}
		if time.Now().After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errCancelled
		case <-ticker.C:
		}
	}
}

func isManifestURL(raw string) bool {
	candidate, ok := requestMediaCandidate(raw, "")
	return ok && candidate.Manifest
}

func fetchManifest(ctx context.Context, mediaURL, pageURL string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Referer", pageURL)
	if path, ok := existingVideoCookieFile(pageURL); ok {
		if header := cookieHeader(path, mediaURL); header != "" {
			req.Header.Set("Cookie", header)
		}
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("reading the video manifest: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading the video manifest: %w", err)
	}
	return string(body), nil
}

func cookieHeader(path, rawURL string) string {
	cookies, err := readNetscapeCookies(path)
	if err != nil {
		return ""
	}
	host, err := videoCookieHost(rawURL)
	if err != nil {
		return ""
	}
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
		if domain != host && !strings.HasSuffix(host, "."+domain) {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}
