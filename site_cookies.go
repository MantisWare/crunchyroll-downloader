package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func videoCookieHost(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("unrecognized video URL: %s", raw)
	}
	return strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www."), nil
}

func cookieStorageHost(raw string) (string, error) {
	host, err := videoCookieHost(raw)
	if err != nil {
		return "", err
	}
	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be":
		return "youtube.com", nil
	default:
		return host, nil
	}
}

func videoCookiePath(raw string) (string, error) {
	host, err := cookieStorageHost(raw)
	if err != nil {
		return "", err
	}
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cookies", host+".txt"), nil
}

func existingVideoCookieFile(raw string) (string, bool) {
	path, err := videoCookiePath(raw)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return "", false
	}
	return path, true
}

func writeVideoCookies(raw string, cookies []*network.Cookie) error {
	host, err := videoCookieHost(raw)
	if err != nil {
		return err
	}
	lines := make([]string, 0, len(cookies))
	seen := map[string]bool{}
	for _, cookie := range cookies {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		key := cookie.Domain + "\t" + cookie.Path + "\t" + cookie.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		lines = append(lines, netscapeCookieLine(cookie))
	}
	if len(lines) == 0 {
		return fmt.Errorf("no cookies found for %s", host)
	}

	path, err := videoCookiePath(raw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating cookie folder: %w", err)
	}
	body := "# Netscape HTTP Cookie File\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("writing cookies: %w", err)
	}
	return nil
}

func netscapeCookieLine(cookie *network.Cookie) string {
	domain := cookie.Domain
	subdomains := "FALSE"
	if strings.HasPrefix(domain, ".") {
		subdomains = "TRUE"
	}
	path := cookie.Path
	if path == "" {
		path = "/"
	}
	secure := "FALSE"
	if cookie.Secure {
		secure = "TRUE"
	}
	expires := "0"
	if cookie.Expires > 0 {
		expires = strconv.FormatInt(int64(cookie.Expires), 10)
	}
	return strings.Join([]string{domain, subdomains, path, secure, expires, cookie.Name, cookie.Value}, "\t")
}

func readNetscapeCookies(path string) ([]*network.Cookie, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cookies []*network.Cookie
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 7 {
			continue
		}
		expires, _ := strconv.ParseFloat(parts[4], 64)
		cookies = append(cookies, &network.Cookie{
			Domain:   parts[0],
			Path:     parts[2],
			Secure:   parts[3] == "TRUE",
			Expires:  expires,
			Name:     parts[5],
			Value:    parts[6],
			HTTPOnly: false,
		})
	}
	return cookies, nil
}

func captureVisibleSiteCookies(ctx context.Context, pageURL string, finished <-chan struct{}) ([]*network.Cookie, error) {
	executable, err := findChromiumExecutable()
	if err != nil {
		return nil, err
	}
	profileDir, err := os.MkdirTemp("", "video-login-*")
	if err != nil {
		return nil, fmt.Errorf("creating temporary browser profile: %w", err)
	}
	defer os.RemoveAll(profileDir)

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, visibleBrowserOptions(executable, profileDir)...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	if err := chromedp.Run(browserCtx, chromedp.Navigate(pageURL)); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("opening the page: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-browserCtx.Done():
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errBrowserClosed
		case <-finished:
			return readBrowserCookies(browserCtx, pageURL)
		}
	}
}

func visibleBrowserOptions(executable, profileDir string) []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(executable),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.UserDataDir(profileDir),
		chromedp.WindowSize(1100, 800),
		chromedp.Flag("headless", false),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-extensions", false),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
	}
}

func readBrowserCookies(ctx context.Context, pageURL string) ([]*network.Cookie, error) {
	urls := []string{pageURL}
	if isYouTubeHost(pageURL) {
		urls = append(urls, "https://www.youtube.com/")
	}
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(actionCtx context.Context) error {
		var cookieErr error
		cookies, cookieErr = network.GetCookies().WithUrls(urls).Do(actionCtx)
		return cookieErr
	}))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return cookies, nil
}

func headlessBrowserOptions(executable, profileDir string) []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(executable),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.UserDataDir(profileDir),
		chromedp.WindowSize(1100, 800),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
	}
}
