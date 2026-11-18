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
	youtubeMinMonth = 8
	youtubeMinDay   = 19
)

type youtubeKind int

const (
	youtubeVideo youtubeKind = iota + 1
	youtubePlaylist
)

type youtubeRef struct {
	Kind       youtubeKind
	VideoID    string
	PlaylistID string
}

type youtubeEntry struct {
	ID    string
	Title string
	Index int
}

type youtubeLookup struct {
	Kind    youtubeKind
	ID      string
	Title   string
	Entries []youtubeEntry
}

type youtubeJob struct {
	VideoID string
	Title   string
	Output  string
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
	return []string{
		"--no-playlist",
		"--js-runtimes", "deno",
		"--newline",
		"-f", format,
		"--merge-output-format", "mp4",
		"--remux-video", "mp4",
		"-o", destination,
		"https://www.youtube.com/watch?v=" + videoID,
	}, nil
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
			ID:    entry.ID,
			Title: entry.Title,
			Index: index,
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
			VideoID: entry.ID,
			Title:   entry.Title,
			Output:  filepath.Join(folder, name),
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
		return fmt.Errorf("yt-dlp %s is too old; update to 2026.08.19 or newer", found)
	}
	return nil
}

func ensureYouTubeTools() error {
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return fmt.Errorf("yt-dlp is required on PATH for YouTube downloads")
	}
	if _, err := exec.LookPath("deno"); err != nil {
		return fmt.Errorf("deno is required on PATH for YouTube downloads")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is required on PATH for YouTube downloads")
	}

	out, err := exec.Command(ytdlp, "--version").Output()
	if err != nil {
		return fmt.Errorf("yt-dlp is required on PATH for YouTube downloads: %w", err)
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
	ref, err := parseYouTubeURL(raw)
	if err != nil {
		return youtubeLookup{}, err
	}
	if err := ensureYouTubeTools(); err != nil {
		return youtubeLookup{}, err
	}
	if log == nil {
		log = io.Discard
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = runYTDLP(ctx, youtubeLookupArgs(raw, ref.Kind == youtubePlaylist), &stdout, io.MultiWriter(log, &stderr))
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" && !errors.Is(err, errCancelled) {
			return youtubeLookup{}, fmt.Errorf("%w: %s", err, detail)
		}
		return youtubeLookup{}, err
	}
	return parseYouTubeDump(stdout.Bytes(), ref.Kind)
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

		args, err := youtubeDownloadArgs(job.VideoID, quality, job.Output)
		if err != nil {
			return err
		}
		label := strings.TrimSpace(job.Title)
		if label == "" {
			label = job.VideoID
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
		return fmt.Errorf("%d YouTube video(s) failed", failed)
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

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
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
