package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	crunchyrollLoginURL = "https://www.crunchyroll.com/login"
	browserLoginTimeout = 10 * time.Minute
	cookiePollInterval  = 500 * time.Millisecond
)

var errBrowserClosed = errors.New("browser was closed before sign-in completed")

// loginWithBrowser opens an isolated Chromium profile and waits for
// Crunchyroll to issue the authenticated etp_rt cookie. Credentials, 2FA
// codes, and CAPTCHA responses stay inside the browser.
func loginWithBrowser(ctx context.Context) (string, error) {
	executable, err := findChromiumExecutable()
	if err != nil {
		return "", err
	}

	profileDir, err := os.MkdirTemp("", "crunchyroll-login-*")
	if err != nil {
		return "", fmt.Errorf("creating temporary browser profile: %w", err)
	}
	defer os.RemoveAll(profileDir)

	allocatorOptions := []chromedp.ExecAllocatorOption{
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

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer cancelAllocator()

	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	if err := chromedp.Run(browserCtx, chromedp.Navigate(crunchyrollLoginURL)); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("opening Crunchyroll login: %w", err)
	}

	ticker := time.NewTicker(cookiePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-browserCtx.Done():
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", errBrowserClosed
		case <-ticker.C:
			cookie, found, cookieErr := readCrunchyrollSessionCookie(browserCtx)
			if cookieErr != nil {
				if browserCtx.Err() != nil {
					return "", errBrowserClosed
				}
				continue
			}
			if found {
				return cookie, nil
			}
		}
	}
}

func readCrunchyrollSessionCookie(ctx context.Context) (string, bool, error) {
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(actionCtx context.Context) error {
		var cookieErr error
		cookies, cookieErr = network.GetCookies().
			WithUrls([]string{"https://www.crunchyroll.com/"}).
			Do(actionCtx)
		return cookieErr
	}))
	if err != nil {
		return "", false, err
	}

	cookie, found := etpRtFromCookies(cookies)
	return cookie, found, nil
}

func etpRtFromCookies(cookies []*network.Cookie) (string, bool) {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == "etp_rt" && cookie.Value != "" {
			return cookie.Value, true
		}
	}
	return "", false
}

func findChromiumExecutable() (string, error) {
	for _, candidate := range chromiumPathCandidates() {
		if filepath.IsAbs(candidate) {
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				return candidate, nil
			}
			continue
		}

		path, err := exec.LookPath(candidate)
		if err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf(
		"no supported browser found; install Google Chrome, Microsoft Edge, Brave, or Chromium",
	)
}

func chromiumPathCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			filepath.Join(userHomeDir(), "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
			filepath.Join(userHomeDir(), "Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"),
			filepath.Join(userHomeDir(), "Applications/Brave Browser.app/Contents/MacOS/Brave Browser"),
			filepath.Join(userHomeDir(), "Applications/Chromium.app/Contents/MacOS/Chromium"),
		}
	case "windows":
		return windowsChromiumCandidates()
	default:
		return []string{
			"google-chrome",
			"google-chrome-stable",
			"microsoft-edge",
			"microsoft-edge-stable",
			"brave-browser",
			"brave",
			"chromium",
			"chromium-browser",
		}
	}
}

func windowsChromiumCandidates() []string {
	var candidates []string
	roots := []string{
		os.Getenv("LOCALAPPDATA"),
		os.Getenv("PROGRAMFILES"),
		os.Getenv("PROGRAMFILES(X86)"),
	}
	relativePaths := []string{
		filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join("BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		filepath.Join("Chromium", "Application", "chrome.exe"),
	}

	for _, root := range roots {
		if root == "" {
			continue
		}
		for _, relativePath := range relativePaths {
			candidates = append(candidates, filepath.Join(root, relativePath))
		}
	}

	return append(candidates, "chrome", "msedge", "brave", "chromium")
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
