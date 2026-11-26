//go:build gui

package main

import (
	"regexp"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
)

type sessionLogWriter struct {
	append func(chunk string)
}

func (w sessionLogWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	chunk := string(p)
	fyne.Do(func() {
		w.append(chunk)
	})
	return len(p), nil
}

func (s *downloadSession) appendLog(chunk string) {
	if !s.stillOpen() {
		return
	}
	s.logRaw = collapseDownloadProgress(s.logRaw + chunk)
	s.logEntry.SetText(renderLog(s.logRaw))
	s.noteLogProgress(chunk)
}

func (s *downloadSession) clearLog() {
	s.logRaw = ""
	s.logEntry.SetText("")
}

// renderLog prepares captured job output for display. Progress lines rewritten
// with carriage returns are collapsed first, then the order is flipped so the
// newest line sits at the top. Showing it newest-first means the interesting
// line is always in view without moving the caret to force a scroll, which is
// what used to fight the user's own scrolling.
func renderLog(raw string) string {
	text := normalizeCarriageReturns(raw)
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	slices.Reverse(lines)
	return strings.Join(lines, "\n")
}

var downloadPercentLine = regexp.MustCompile(`^\[download\]\s+[0-9]+(?:\.[0-9]+)?%`)

// collapseDownloadProgress keeps the latest yt-dlp percent line in a run.
// Each update used to stay in the log, and redrawing that growing history
// stalled the download between segments.
func collapseDownloadProgress(raw string) string {
	if raw == "" {
		return ""
	}
	body := raw
	tail := ""
	if !strings.HasSuffix(raw, "\n") {
		split := strings.LastIndex(raw, "\n")
		if split < 0 {
			return raw
		}
		body = raw[:split+1]
		tail = raw[split+1:]
	}

	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	built := make([]string, 0, len(lines))
	held := ""
	holding := false
	for _, line := range lines {
		if downloadPercentLine.MatchString(line) {
			held = line
			holding = true
			continue
		}
		if holding {
			built = append(built, held)
			holding = false
		}
		built = append(built, line)
	}
	if holding {
		built = append(built, held)
	}
	out := strings.Join(built, "\n")
	if strings.HasSuffix(body, "\n") {
		out += "\n"
	}
	return out + tail
}

func normalizeCarriageReturns(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if idx := strings.LastIndex(line, "\r"); idx >= 0 {
			lines[i] = line[idx+1:]
		}
	}
	return strings.Join(lines, "\n")
}
