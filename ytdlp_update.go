package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const ytdlpReleaseURL = "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"

func installedYTDLPVersion() (string, error) {
	out, err := exec.Command("yt-dlp", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("yt-dlp is not installed")
	}
	line := strings.TrimSpace(string(out))
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if _, _, _, ok := parseYouTubeToolVersion(line); !ok {
		return "", fmt.Errorf("unrecognized yt-dlp version %q", line)
	}
	return line, nil
}

func ytdlpVersionNewer(latest, current string) bool {
	latestYear, latestMonth, latestDay, latestOK := parseYouTubeToolVersion(latest)
	currentYear, currentMonth, currentDay, currentOK := parseYouTubeToolVersion(current)
	if !latestOK || !currentOK {
		return false
	}
	if latestYear != currentYear {
		return latestYear > currentYear
	}
	if latestMonth != currentMonth {
		return latestMonth > currentMonth
	}
	return latestDay > currentDay
}

func ytdlpStatusText(current, latest string, latestErr error) (string, bool) {
	if strings.TrimSpace(current) == "" {
		return "yt-dlp is not installed.", false
	}
	if latestErr != nil {
		return fmt.Sprintf("yt-dlp %s. Could not check for an update.", current), true
	}
	if ytdlpVersionNewer(latest, current) {
		return fmt.Sprintf("yt-dlp %s. %s is available.", current, latest), true
	}
	year, month, day, ok := parseYouTubeToolVersion(current)
	if !ok || !youtubeVersionAcceptable(year, month, day) {
		return fmt.Sprintf("yt-dlp %s is too old. Update it and try again.", current), true
	}
	return fmt.Sprintf("yt-dlp %s is up to date.", current), false
}

func ytdlpUsesBrew(binaryPath string) bool {
	resolved, err := filepath.EvalSymlinks(binaryPath)
	if err != nil || resolved == "" {
		resolved = binaryPath
	}
	return strings.Contains(resolved, string(filepath.Separator)+"Cellar"+string(filepath.Separator)+"yt-dlp"+string(filepath.Separator))
}

func ytdlpUpdateArgs(binaryPath, brewPath string) (string, []string, error) {
	if ytdlpUsesBrew(binaryPath) {
		if strings.TrimSpace(brewPath) == "" {
			return "", nil, fmt.Errorf("Homebrew yt-dlp is installed, but brew is not on PATH")
		}
		return brewPath, []string{"upgrade", "yt-dlp"}, nil
	}
	if strings.TrimSpace(binaryPath) == "" {
		return "", nil, fmt.Errorf("yt-dlp is not installed")
	}
	return binaryPath, []string{"-U"}, nil
}

func latestYTDLPVersion(body []byte) (string, error) {
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("reading the yt-dlp release: %w", err)
	}
	version := strings.TrimSpace(strings.TrimPrefix(release.TagName, "v"))
	if _, _, _, ok := parseYouTubeToolVersion(version); !ok {
		return "", fmt.Errorf("unrecognized yt-dlp release %q", release.TagName)
	}
	return version, nil
}

func fetchLatestYTDLPVersion(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ytdlpReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "crunchyroll-downloader")
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking yt-dlp releases: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("checking yt-dlp releases: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checking yt-dlp releases: %s", response.Status)
	}
	return latestYTDLPVersion(body)
}

func upgradeYTDLP(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	binary, err := exec.LookPath("yt-dlp")
	if err != nil {
		return "", fmt.Errorf("yt-dlp is not installed")
	}
	brew, _ := exec.LookPath("brew")
	name, args, err := ytdlpUpdateArgs(binary, brew)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", fmt.Errorf("updating yt-dlp: %w", err)
		}
		return text, fmt.Errorf("%s", lastOutputLine(text))
	}
	return text, nil
}

func lastOutputLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(text)
}
