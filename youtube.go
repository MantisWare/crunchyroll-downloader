package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	youtubeMinYear  = 2026
	youtubeMinMonth = 11
	youtubeMinDay   = 23
)

type youtubeKind int

const (
	youtubeVideo youtubeKind = iota + 1
	youtubePlaylist
	youtubePage
)

type youtubeRef struct {
	Kind       youtubeKind
	VideoID    string
	PlaylistID string
}

type youtubeEntry struct {
	ID       string
	Title    string
	Index    int
	MediaURL string
	PageURL  string
}

type youtubeLookup struct {
	Kind    youtubeKind
	ID      string
	Title   string
	Entries []youtubeEntry
}

type youtubeJob struct {
	VideoID  string
	Title    string
	Output   string
	MediaURL string
	PageURL  string
}

var (
	youtubeVideoIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	youtubeProgressPattern = regexp.MustCompile(`\[download\]\s+([0-9]+(?:\.[0-9]+)?)%`)
	youtubeVersionPattern  = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)
)

func parseYouTubeURL(raw string) (youtubeRef, error) {
	raw = strings.TrimSpace(raw)
	if !isYouTubeHost(raw) {
		return youtubeRef{}, fmt.Errorf("unrecognized YouTube URL: %s", raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return youtubeRef{}, fmt.Errorf("unrecognized YouTube URL: %s", raw)
	}

	host := youtubeHost(parsed.Hostname())
	if host == "youtu.be" {
		id := firstPathSegment(parsed.Path)
		if !youtubeVideoIDPattern.MatchString(id) {
			return youtubeRef{}, fmt.Errorf("unrecognized YouTube URL: %s", raw)
		}
		return youtubeRef{Kind: youtubeVideo, VideoID: id}, nil
	}

	parts := splitPath(parsed.Path)
	if len(parts) >= 2 && parts[0] == "shorts" && youtubeVideoIDPattern.MatchString(parts[1]) {
		return youtubeRef{Kind: youtubeVideo, VideoID: parts[1]}, nil
	}
	if len(parts) >= 1 && parts[0] == "watch" {
		id := parsed.Query().Get("v")
		if youtubeVideoIDPattern.MatchString(id) {
			return youtubeRef{Kind: youtubeVideo, VideoID: id}, nil
		}
		return youtubeRef{}, fmt.Errorf("unrecognized YouTube URL: %s", raw)
	}
	if len(parts) >= 1 && parts[0] == "playlist" {
		list := strings.TrimSpace(parsed.Query().Get("list"))
		if list == "" {
			return youtubeRef{}, fmt.Errorf("unrecognized YouTube URL: %s", raw)
		}
		return youtubeRef{Kind: youtubePlaylist, PlaylistID: list}, nil
	}
	return youtubeRef{}, fmt.Errorf("unrecognized YouTube URL: %s", raw)
}

func classifyVideoURL(raw string) (youtubeRef, error) {
	raw = strings.TrimSpace(raw)
	if isYouTubeHost(raw) {
		return parseYouTubeURL(raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return youtubeRef{}, fmt.Errorf("unrecognized video URL: %s", raw)
	}
	if isCrunchyrollHost(parsed.Hostname()) {
		return youtubeRef{}, fmt.Errorf("use the Download tab for a Crunchyroll URL")
	}
	return youtubeRef{Kind: youtubePage}, nil
}

func isCrunchyrollHost(hostname string) bool {
	host := strings.TrimPrefix(strings.ToLower(hostname), "www.")
	return host == "crunchyroll.com" || strings.HasSuffix(host, ".crunchyroll.com")
}

func isOtherVideoPage(raw string) bool {
	ref, err := classifyVideoURL(raw)
	return err == nil && ref.Kind == youtubePage
}

func isYouTubeHost(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	switch youtubeHost(parsed.Hostname()) {
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be":
		return true
	default:
		return false
	}
}

func youtubeHost(hostname string) string {
	return strings.TrimPrefix(strings.ToLower(hostname), "www.")
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func firstPathSegment(path string) string {
	parts := splitPath(path)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func youtubeQualityHeight(quality string) (int, error) {
	switch quality {
	case "1080p":
		return 1080, nil
	case "720p":
		return 720, nil
	case "480p":
		return 480, nil
	case "360p":
		return 360, nil
	default:
		return 0, fmt.Errorf("unsupported video quality %q", quality)
	}
}

func youtubeFormatSelector(quality string) (string, error) {
	height, err := youtubeQualityHeight(quality)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("bv*[height<=%d]+ba/b[height<=%d]/b", height, height), nil
}

func youtubeLookupArgs(raw string, playlist bool) []string {
	args := []string{"--dump-single-json", "--no-download", "--js-runtimes", "deno"}
	if playlist {
		args = append(args, "--flat-playlist")
	} else {
		args = append(args, "--no-playlist")
	}
	return append(args, raw)
}

func youtubeDownloadArgs(videoID, quality, destination string) ([]string, error) {
	format, err := youtubeFormatSelector(quality)
	if err != nil {
		return nil, err
	}
	watch := "https://www.youtube.com/watch?v=" + videoID
	args := []string{
		"--no-playlist",
		"--js-runtimes", "deno",
		"--newline",
		"-f", format,
		"--merge-output-format", "mp4",
		"--remux-video", "mp4",
		"-o", destination,
		watch,
	}
	return insertCookieArg(args, watch), nil
}

func downloadArgsForJob(job youtubeJob, quality string) ([]string, error) {
	if job.MediaURL != "" {
		return mediaDownloadArgs(job.MediaURL, job.PageURL, quality, job.Output)
	}
	if job.PageURL != "" && !isYouTubeHost(job.PageURL) {
		return mediaDownloadArgs(job.PageURL, "", quality, job.Output)
	}
	if !youtubeVideoIDPattern.MatchString(job.VideoID) {
		label := strings.TrimSpace(job.Title)
		if label == "" {
			label = "this entry"
		}
		return nil, fmt.Errorf("no video address for %s", label)
	}
	return youtubeDownloadArgs(job.VideoID, quality, job.Output)
}

func mediaDownloadArgs(target, pageURL, quality, destination string) ([]string, error) {
	format, err := youtubeFormatSelector(quality)
	if err != nil {
		return nil, err
	}
	args := []string{
		"--no-playlist",
		"--js-runtimes", "deno",
		"--newline",
		"-f", format,
		"--merge-output-format", "mp4",
		"--remux-video", "mp4",
	}
	if pageURL != "" && target != pageURL {
		args = append(args, "--referer", pageURL)
	}
	args = append(args, "-o", destination, target)
	args = insertCookieArg(args, pageURL)
	if !strings.Contains(strings.Join(args, "\n"), "--cookies") {
		args = insertCookieArg(args, target)
	}
	return args, nil
}

func insertCookieArg(args []string, raw string) []string {
	path, ok := existingVideoCookieFile(raw)
	if !ok {
		return args
	}
	return withCookieFile(args, path)
}

func withCookieFile(args []string, path string) []string {
	if path == "" {
		return args
	}
	if len(args) == 0 {
		return []string{"--cookies", path}
	}
	last := args[len(args)-1]
	if strings.Contains(last, "://") && !previousTakesValue(args) {
		out := append([]string{}, args[:len(args)-1]...)
		return append(out, "--cookies", path, last)
	}
	return append(args, "--cookies", path)
}

func previousTakesValue(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[len(args)-2] {
	case "--referer", "--cookies", "-o", "--output", "-f", "--format", "--js-runtimes", "--merge-output-format", "--remux-video", "--compat-options":
		return true
	default:
		return false
	}
}

type ytdlpDump struct {
	ID      string            `json:"id"`
	Title   string            `json:"title"`
	Entries []json.RawMessage `json:"entries"`
}

type ytdlpEntry struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	PlaylistIndex int    `json:"playlist_index"`
	URL           string `json:"url"`
	WebpageURL    string `json:"webpage_url"`
}

func parseYouTubeDump(data []byte, kind youtubeKind) (youtubeLookup, error) {
	var dump ytdlpDump
	if err := json.Unmarshal(jsonPayload(data), &dump); err != nil {
		return youtubeLookup{}, fmt.Errorf("reading yt-dlp info: %w", err)
	}

	lookup := youtubeLookup{Kind: kind, ID: dump.ID, Title: dump.Title}
	if kind == youtubeVideo {
		if dump.ID == "" {
			return youtubeLookup{}, fmt.Errorf("yt-dlp returned no video id")
		}
		lookup.Entries = []youtubeEntry{{ID: dump.ID, Title: dump.Title}}
		return lookup, nil
	}

	kept := 0
	for _, raw := range dump.Entries {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		var entry ytdlpEntry
		if err := json.Unmarshal(trimmed, &entry); err != nil {
			return youtubeLookup{}, fmt.Errorf("reading playlist entry: %w", err)
		}
		if entry.ID == "" {
			continue
		}
		kept++
		index := entry.PlaylistIndex
		if index <= 0 {
			index = kept
		}
		lookup.Entries = append(lookup.Entries, youtubeEntry{
			ID:       entry.ID,
			Title:    entry.Title,
			Index:    index,
			MediaURL: nonYouTubeMediaURL(entry.WebpageURL, entry.URL),
		})
	}
	if len(lookup.Entries) == 0 {
		return youtubeLookup{}, fmt.Errorf("playlist has no videos")
	}
	return lookup, nil
}

func jsonPayload(data []byte) []byte {
	data = bytes.TrimSpace(data)
	start := bytes.IndexByte(data, '{')
	end := bytes.LastIndexByte(data, '}')
	if start >= 0 && end > start {
		return data[start : end+1]
	}
	return data
}

func youtubeJobs(dir string, lookup youtubeLookup, selected []string, quality string) ([]youtubeJob, error) {
	if _, err := youtubeQualityHeight(quality); err != nil {
		return nil, err
	}
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	want := make(map[string]bool, len(selected))
	for _, id := range selected {
		want[id] = true
	}

	jobs := make([]youtubeJob, 0, len(lookup.Entries))
	count := len(lookup.Entries)
	for _, entry := range lookup.Entries {
		if lookup.Kind == youtubePlaylist && !want[entry.ID] {
			continue
		}
		name := youtubeFileName(entry.Title, entry.ID, quality)
		folder := dir
		if lookup.Kind == youtubePlaylist {
			folder = filepath.Join(dir, youtubePlaylistFolder(lookup.Title, lookup.ID))
			name = youtubeIndexLabel(entry.Index, count) + " " + name
		}
		jobs = append(jobs, youtubeJob{
			VideoID:  entry.ID,
			Title:    entry.Title,
			Output:   filepath.Join(folder, name),
			MediaURL: entry.MediaURL,
			PageURL:  entry.PageURL,
		})
	}
	if lookup.Kind == youtubePlaylist && len(jobs) == 0 {
		return nil, fmt.Errorf("no videos selected")
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("yt-dlp returned no video id")
	}
	return jobs, nil
}

func youtubeFileName(title, fallbackID, quality string) string {
	name := sanitizeFilename(strings.TrimSpace(title))
	if name == "" {
		name = sanitizeFilename(strings.TrimSpace(fallbackID))
	}
	return name + " [" + quality + "].mp4"
}

func youtubePlaylistFolder(title, playlistID string) string {
	name := sanitizeFilename(strings.TrimSpace(title))
	if name == "" {
		name = sanitizeFilename(strings.TrimSpace(playlistID))
	}
	return name
}

func youtubeIndexLabel(index, count int) string {
	width := 2
	if count > 0 {
		digits := len(strconv.Itoa(count))
		if digits > width {
			width = digits
		}
	}
	if index < 1 {
		index = 1
	}
	return fmt.Sprintf("%0*d", width, index)
}

func youtubeProgressPercent(chunk string) (float64, bool) {
	matches := youtubeProgressPattern.FindAllStringSubmatch(chunk, -1)
	if len(matches) == 0 {
		return 0, false
	}
	pct, err := strconv.ParseFloat(matches[len(matches)-1][1], 64)
	if err != nil {
		return 0, false
	}
	return pct, true
}

func youtubeOverallProgress(done, total int, currentPct float64) float64 {
	if total <= 0 {
		return 0
	}
	if currentPct < 0 {
		currentPct = 0
	}
	if currentPct > 100 {
		currentPct = 100
	}
	value := (float64(done) + currentPct/100) / float64(total)
	if value > 1 {
		return 1
	}
	return value
}

func parseYouTubeToolVersion(line string) (int, int, int, bool) {
	match := youtubeVersionPattern.FindStringSubmatch(line)
	if match == nil {
		return 0, 0, 0, false
	}
	year, errYear := strconv.Atoi(match[1])
	month, errMonth := strconv.Atoi(match[2])
	day, errDay := strconv.Atoi(match[3])
	if errYear != nil || errMonth != nil || errDay != nil {
		return 0, 0, 0, false
	}
	return year, month, day, true
}

func youtubeVersionAcceptable(year, month, day int) bool {
	if year != youtubeMinYear {
		return year > youtubeMinYear
	}
	if month != youtubeMinMonth {
		return month > youtubeMinMonth
	}
	return day >= youtubeMinDay
}

func validateYouTubeToolVersion(output string) error {
	line := strings.TrimSpace(output)
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	year, month, day, ok := parseYouTubeToolVersion(line)
	if !ok || !youtubeVersionAcceptable(year, month, day) {
		found := line
		if found == "" {
			found = "unknown"
		}
		return fmt.Errorf("yt-dlp %s is too old; update to 2026.11.23 or newer", found)
	}
	return nil
}

func ensureYouTubeTools() error {
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return fmt.Errorf("yt-dlp is required on PATH for video downloads")
	}
	if _, err := exec.LookPath("deno"); err != nil {
		return fmt.Errorf("deno is required on PATH for video downloads")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is required on PATH for video downloads")
	}

	out, err := exec.Command(ytdlp, "--version").Output()
	if err != nil {
		return fmt.Errorf("yt-dlp is required on PATH for video downloads: %w", err)
	}
	return validateYouTubeToolVersion(string(out))
}

func urlsRequireCrunchyrollAuth(urls []string) bool {
	for _, raw := range urls {
		if isYouTubeHost(raw) {
			continue
		}
		if _, err := parseContentURL(raw); err == nil {
			return true
		}
	}
	return false
}

func lookupYouTube(ctx context.Context, raw string, log io.Writer) (youtubeLookup, error) {
	ref, err := classifyVideoURL(raw)
	if err != nil {
		return youtubeLookup{}, err
	}
	if err := ensureYouTubeTools(); err != nil {
		return youtubeLookup{}, err
	}
	if log == nil {
		log = io.Discard
	}
	if ref.Kind == youtubePage {
		return lookupVideoPage(ctx, raw, log)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := insertCookieArg(youtubeLookupArgs(raw, ref.Kind == youtubePlaylist), raw)
	err = runYTDLP(ctx, args, &stdout, &stderr)
	if err != nil {
		if errors.Is(err, errCancelled) {
			return youtubeLookup{}, err
		}
		shown := presentVideoError(strings.TrimSpace(stderr.String()), raw)
		if shown != "" && log != nil {
			fmt.Fprintln(log, shown)
		}
		if shown != "" {
			return youtubeLookup{}, fmt.Errorf("%w: %s", err, shown)
		}
		return youtubeLookup{}, err
	}
	return parseYouTubeDump(stdout.Bytes(), ref.Kind)
}

func lookupVideoPage(ctx context.Context, raw string, log io.Writer) (youtubeLookup, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := insertCookieArg(youtubeLookupArgs(raw, true), raw)
	err := runYTDLP(ctx, args, &stdout, &stderr)
	detail := strings.TrimSpace(stderr.String())
	if err != nil {
		if errors.Is(err, errCancelled) {
			return youtubeLookup{}, err
		}
		if shouldSearchPage(detail) {
			if log != nil {
				fmt.Fprintf(log, "Looking for a video on the page…\n")
			}
			found, searchErr := searchPageForVideo(ctx, raw, log)
			if searchErr != nil {
				return youtubeLookup{}, loginHintError(searchErr, raw)
			}
			return found, nil
		}
		if detail != "" {
			if log != nil {
				fmt.Fprintln(log, detail)
			}
			return youtubeLookup{}, fmt.Errorf("%w: %s", err, loginHint(detail, raw))
		}
		return youtubeLookup{}, err
	}
	if detail != "" && log != nil {
		fmt.Fprintln(log, detail)
	}

	kind := ytdlpResultKind(stdout.Bytes())
	lookup, err := parseYouTubeDump(stdout.Bytes(), kind)
	if err != nil {
		return youtubeLookup{}, err
	}
	if kind == youtubeVideo {
		for i := range lookup.Entries {
			lookup.Entries[i].PageURL = raw
			lookup.Entries[i].MediaURL = ""
		}
	}
	return lookup, nil
}

func shouldSearchPage(detail string) bool {
	markers := []string{
		"Unsupported URL",
		"CERTIFICATE_VERIFY_FAILED",
		"Unable to download webpage",
		"No video formats found",
		"There's no video",
	}
	for _, marker := range markers {
		if strings.Contains(detail, marker) {
			return true
		}
	}
	return false
}

func ytdlpResultKind(data []byte) youtubeKind {
	var probe struct {
		Entries json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(jsonPayload(data), &probe); err != nil {
		return youtubeVideo
	}
	trimmed := bytes.TrimSpace(probe.Entries)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return youtubePlaylist
	}
	return youtubeVideo
}

func nonYouTubeMediaURL(webpage, raw string) string {
	chosen := absoluteHTTPURL(webpage)
	if chosen == "" {
		chosen = absoluteHTTPURL(raw)
	}
	if chosen == "" || isYouTubeHost(chosen) {
		return ""
	}
	return chosen
}

func absoluteHTTPURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func presentVideoError(detail, raw string) string {
	if youtubePlayerCheckFailed(detail) {
		return "YouTube's player check failed. Update yt-dlp and try again."
	}
	detail = relevantYTDLPDetail(detail)
	if detail == "" {
		return ""
	}
	if strings.Contains(detail, "confirm your age") {
		if _, ok := existingVideoCookieFile(raw); ok {
			return "This video asks you to sign in to confirm your age. The saved YouTube sign-in was not accepted. Use Sign in again, then try again."
		}
		return "This video asks you to sign in to confirm your age. Use Sign in, then try again."
	}
	return loginHint(detail, raw)
}

func youtubePlayerCheckFailed(detail string) bool {
	lower := strings.ToLower(detail)
	markers := []string{
		"n challenge solving failed",
		"error solving n challenge",
		"nchallengeinput",
		"addondisposecallback",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func relevantYTDLPDetail(detail string) string {
	var kept []string
	for _, line := range strings.Split(detail, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "WARNING:") {
			continue
		}
		if strings.Contains(line, "To suppress this warning") || strings.Contains(line, "installed yt-dlp with pip") || strings.Contains(line, "strongly recommended") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func loginHint(detail, raw string) string {
	if !needsSignInHint(detail) {
		return detail
	}
	if _, ok := existingVideoCookieFile(raw); ok {
		return detail
	}
	return detail + "\nIf this site requires a login, use Sign in and try again."
}

func needsSignInHint(detail string) bool {
	lower := strings.ToLower(detail)
	markers := []string{
		"sign in",
		"confirm your age",
		"login required",
		"private video",
		"authentication",
		"cookies-from-browser",
		"use --cookies",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func loginHintError(err error, raw string) error {
	if err == nil || errors.Is(err, errPageVideoEncrypted) {
		return err
	}
	if _, ok := existingVideoCookieFile(raw); ok {
		return err
	}
	if errors.Is(err, errNoPageVideo) || strings.Contains(err.Error(), errNoPageVideo.Error()) {
		return fmt.Errorf("%w\nIf this site requires a login, use Sign in and try again.", err)
	}
	return err
}

func downloadYouTubeURL(env jobEnv, raw string) error {
	lookup, err := lookupYouTube(env.context(), raw, env.writer())
	if err != nil {
		return err
	}
	selected := make([]string, 0, len(lookup.Entries))
	if lookup.Kind == youtubePlaylist {
		for _, entry := range lookup.Entries {
			selected = append(selected, entry.ID)
		}
	}
	quality := env.videoQuality
	if quality == "" {
		quality = "1080p"
	}
	jobs, err := youtubeJobs(env.dir(), lookup, selected, quality)
	if err != nil {
		return err
	}
	return downloadYouTubeJobs(env, jobs, quality, nil)
}

func downloadYouTubeJobs(env jobEnv, jobs []youtubeJob, quality string, onProgress func(done, total int, pct float64)) error {
	if err := ensureYouTubeTools(); err != nil {
		return err
	}
	failed := 0
	total := len(jobs)
	for i, job := range jobs {
		if env.cancelled() {
			return errCancelled
		}
		ready, err := youtubeFileReady(job.Output)
		if err != nil {
			return err
		}
		if ready {
			jobPrintf(env, "Skipping finished file %s\n", job.Output)
			reportYouTubeProgress(onProgress, i+1, total, 100)
			continue
		}

		_ = os.Remove(job.Output + ".part")
		if info, statErr := os.Stat(job.Output); statErr == nil && info.Size() == 0 {
			_ = os.Remove(job.Output)
		}
		if err := os.MkdirAll(filepath.Dir(job.Output), 0o755); err != nil {
			return fmt.Errorf("creating output folder: %w", err)
		}

		label := strings.TrimSpace(job.Title)
		if label == "" {
			label = job.VideoID
		}
		args, err := downloadArgsForJob(job, quality)
		if err != nil {
			failed++
			jobPrintf(env, "Failed: %s: %s\n", label, err)
			continue
		}
		jobPrintf(env, "Downloading %s\nOutput: %s\n", label, job.Output)

		writer := &youtubeProgressWriter{
			dest: env.writer(),
			onPercent: func(pct float64) {
				reportYouTubeProgress(onProgress, i, total, pct)
			},
		}
		err = runYTDLP(env.context(), args, writer, writer)
		if errors.Is(err, errCancelled) {
			discardYouTubePartial(job.Output)
			return errCancelled
		}
		if err != nil {
			failed++
			jobPrintf(env, "Failed: %s: %s\n", label, err)
			discardYouTubePartial(job.Output)
			continue
		}
		reportYouTubeProgress(onProgress, i+1, total, 100)
	}
	if failed > 0 {
		return fmt.Errorf("%d video download(s) failed", failed)
	}
	return nil
}

func youtubeFileReady(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	return info.Size() > 0, nil
}

func discardYouTubePartial(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + ".part")
}

func reportYouTubeProgress(fn func(done, total int, pct float64), done, total int, pct float64) {
	if fn != nil {
		fn(done, total, pct)
	}
}

type youtubeProgressWriter struct {
	mu        sync.Mutex
	dest      io.Writer
	onPercent func(float64)
}

func (w *youtubeProgressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.dest != nil {
		if _, err := w.dest.Write(p); err != nil {
			return 0, err
		}
	}
	if w.onPercent != nil {
		if pct, ok := youtubeProgressPercent(string(p)); ok {
			w.onPercent(pct)
		}
	}
	return len(p), nil
}

func runYTDLP(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return errCancelled
	}

	bundle := trustedCertBundle()
	if extra := ytdlpTLSArgs(bundle); len(extra) > 0 {
		args = append(append([]string{}, extra...), args...)
	}
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.Env = withCertEnv(os.Environ(), bundle)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return errCancelled
		}
		return fmt.Errorf("yt-dlp: %w", err)
	}
	return nil
}
