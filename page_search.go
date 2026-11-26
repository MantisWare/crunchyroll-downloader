package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
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
	errManyPageVideos     = errors.New("This page has more than one video.")
	errPageVideoEncrypted = errors.New("This video is encrypted")
	mediaURLInText        = regexp.MustCompile(`https?://[^\s"'<>\\]{8,1000}\.(?:m3u8|mpd|mp4|webm)`)
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

func choosePageMediaForEmbeds(candidates []pageMediaCandidate, embeds []string) (string, error) {
	tokens := embedTokens(embeds)
	if len(tokens) == 0 {
		return "", errNoPageVideo
	}
	matched := make([]pageMediaCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		for _, token := range tokens {
			if strings.Contains(candidate.URL, token) {
				matched = append(matched, candidate)
				break
			}
		}
	}
	if len(matched) == 0 {
		return "", errNoPageVideo
	}
	return choosePageMedia(matched)
}

func selectPageMedia(candidates []pageMediaCandidate, embeds []string) (string, error) {
	if len(embedTokens(embeds)) > 0 {
		return choosePageMediaForEmbeds(candidates, embeds)
	}
	if ambiguousPageMedia(candidates) {
		return "", errManyPageVideos
	}
	return choosePageMedia(candidates)
}

func pageMediaSettled(candidates []pageMediaCandidate, embeds []string) bool {
	if len(embedTokens(embeds)) > 0 {
		_, err := choosePageMediaForEmbeds(candidates, embeds)
		return err == nil
	}
	if ambiguousPageMedia(candidates) {
		return false
	}
	_, err := choosePageMedia(candidates)
	return err == nil
}

func ambiguousPageMedia(candidates []pageMediaCandidate) bool {
	var manifests int
	var files int
	for _, candidate := range candidates {
		if absoluteHTTPURL(candidate.URL) == "" || candidate.FromVideo {
			continue
		}
		if candidate.Manifest {
			manifests++
		}
		if candidate.File {
			files++
		}
	}
	return manifests > 1 || (manifests == 0 && files > 1)
}

func embedTokens(embeds []string) []string {
	var tokens []string
	seen := map[string]bool{}
	for _, embed := range embeds {
		if !isPlayerEmbed(embed) {
			continue
		}
		parsed, err := url.Parse(embed)
		if err != nil {
			continue
		}
		for _, part := range strings.Split(parsed.EscapedPath(), "/") {
			if len(part) < 8 || seen[part] {
				continue
			}
			seen[part] = true
			tokens = append(tokens, part)
		}
	}
	return tokens
}

func isPlayerEmbed(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.ToLower(parsed.EscapedPath())
	switch {
	case strings.Contains(host, "mediadelivery.net") || strings.Contains(host, "b-cdn.net"):
		return true
	case strings.Contains(host, "player.vimeo.com") || strings.Contains(host, "wistia") || strings.Contains(host, "cloudflarestream.com"):
		return true
	case strings.Contains(path, "/embed/") || strings.HasSuffix(path, "/embed"):
		return true
	default:
		return false
	}
}

func extractMediaURLs(body string) []string {
	normalized := strings.ReplaceAll(body, `\/`, "/")
	normalized = strings.ReplaceAll(normalized, `\u0026`, "&")
	normalized = strings.ReplaceAll(normalized, "&amp;", "&")
	matches := mediaURLInText.FindAllString(normalized, -1)
	urls := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		media := strings.TrimRight(match, ".,);]")
		if seen[media] || absoluteHTTPURL(media) == "" {
			continue
		}
		seen[media] = true
		urls = append(urls, media)
	}
	return urls
}

func responseMightHoldMedia(mime string, size float64) bool {
	if size > 2_000_000 {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(mime))
	return strings.Contains(kind, "json") || kind == "text/plain"
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

	searchCtx, cancel := context.WithTimeout(ctx, pageSearchLimit+20*time.Second)
	defer cancel()
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(searchCtx, headlessBrowserOptions(executable, profileDir)...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	var (
		mu       sync.Mutex
		requests []pageMediaCandidate
		pending  []network.RequestID
	)
	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		response, ok := ev.(*network.EventResponseReceived)
		if !ok || response.Response == nil {
			return
		}
		if candidate, matched := requestMediaCandidate(response.Response.URL, response.Response.MimeType); matched {
			mu.Lock()
			requests = append(requests, candidate)
			mu.Unlock()
		}
		if responseMightHoldMedia(response.Response.MimeType, response.Response.EncodedDataLength) {
			mu.Lock()
			pending = append(pending, response.RequestID)
			mu.Unlock()
		}
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
				WithURL(pageURL).
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

	snapshot, err := watchPageForMedia(browserCtx, &mu, &requests, &pending)
	if err != nil {
		return youtubeLookup{}, err
	}

	mu.Lock()
	candidates := append([]pageMediaCandidate{}, requests...)
	mu.Unlock()

	chosen, err := selectPageMedia(candidates, snapshot.Embeds)
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
	title := strings.TrimSpace(snapshot.Title)
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

const pageSnapshotScript = `(() => {
  const videos = [];
  const embeds = [];
  for (const video of document.querySelectorAll("video")) {
    if (video.currentSrc) videos.push(video.currentSrc);
    if (video.src) videos.push(video.src);
    for (const source of video.querySelectorAll("source")) {
      if (source.src) videos.push(source.src);
    }
  }
  for (const frame of document.querySelectorAll("iframe")) {
    const src = frame.src || "";
    if (src.indexOf("http://") === 0 || src.indexOf("https://") === 0) embeds.push(src);
  }
  return {videos: videos, embeds: embeds, title: document.title || ""};
})()`

type pageSnapshot struct {
	Videos []string `json:"videos"`
	Embeds []string `json:"embeds"`
	Title  string   `json:"title"`
}

func watchPageForMedia(ctx context.Context, mu *sync.Mutex, requests *[]pageMediaCandidate, pending *[]network.RequestID) (pageSnapshot, error) {
	deadline := time.Now().Add(pageSearchLimit)
	var quietSince time.Time
	var snapshot pageSnapshot
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return pageSnapshot{}, errCancelled
		}
		absorbResponseBodies(ctx, mu, requests, pending)
		next, evalErr := readPageSnapshot(ctx)
		if evalErr != nil {
			if ctx.Err() != nil {
				return pageSnapshot{}, errCancelled
			}
		} else {
			snapshot = next
			rememberVideoSources(mu, requests, snapshot.Videos)
		}

		mu.Lock()
		candidates := append([]pageMediaCandidate{}, *requests...)
		mu.Unlock()
		if pageMediaSettled(candidates, snapshot.Embeds) {
			if quietSince.IsZero() {
				quietSince = time.Now()
			}
			if time.Since(quietSince) >= pageSearchQuiet {
				return snapshot, nil
			}
		} else {
			quietSince = time.Time{}
		}
		if time.Now().After(deadline) {
			return snapshot, nil
		}
		select {
		case <-ctx.Done():
			return pageSnapshot{}, errCancelled
		case <-ticker.C:
		}
	}
}

func readPageSnapshot(ctx context.Context) (pageSnapshot, error) {
	var snapshot pageSnapshot
	err := chromedp.Run(ctx, chromedp.Evaluate(pageSnapshotScript, &snapshot))
	return snapshot, err
}

func absorbResponseBodies(ctx context.Context, mu *sync.Mutex, requests *[]pageMediaCandidate, pending *[]network.RequestID) {
	mu.Lock()
	ids := append([]network.RequestID{}, *pending...)
	*pending = nil
	mu.Unlock()
	if len(ids) == 0 {
		return
	}

	found := make([]pageMediaCandidate, 0)
	for _, id := range ids {
		var body []byte
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(actionCtx context.Context) error {
			var bodyErr error
			body, bodyErr = network.GetResponseBody(id).Do(actionCtx)
			return bodyErr
		}))
		if err != nil || len(body) == 0 {
			continue
		}
		for _, media := range extractMediaURLs(string(body)) {
			candidate, ok := requestMediaCandidate(media, "")
			if !ok {
				continue
			}
			found = append(found, candidate)
		}
	}
	if len(found) == 0 {
		return
	}
	mu.Lock()
	*requests = append(*requests, found...)
	mu.Unlock()
}

func rememberVideoSources(mu *sync.Mutex, requests *[]pageMediaCandidate, videos []string) {
	fresh := make([]pageMediaCandidate, 0, len(videos))
	for _, videoURL := range videos {
		if absoluteHTTPURL(videoURL) == "" {
			continue
		}
		fresh = append(fresh, pageMediaCandidate{URL: videoURL, FromVideo: true})
	}
	mu.Lock()
	kept := make([]pageMediaCandidate, 0, len(*requests))
	for _, candidate := range *requests {
		if candidate.FromVideo {
			continue
		}
		kept = append(kept, candidate)
	}
	*requests = append(kept, fresh...)
	mu.Unlock()
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
