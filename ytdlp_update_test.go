package main

import (
	"strings"
	"testing"
)

func TestYTDLPStatusText(t *testing.T) {
	t.Parallel()

	text, canUpdate := ytdlpStatusText("2026.08.19", "2026.11.23", nil)
	if !canUpdate || !strings.Contains(text, "2026.11.23 is available") {
		t.Fatalf("status = %q, update %v", text, canUpdate)
	}

	text, canUpdate = ytdlpStatusText("2026.11.23", "2026.11.23", nil)
	if canUpdate || !strings.Contains(text, "up to date") {
		t.Fatalf("current status = %q, update %v", text, canUpdate)
	}

	text, canUpdate = ytdlpStatusText("", "2026.11.23", nil)
	if canUpdate || !strings.Contains(text, "not installed") {
		t.Fatalf("missing status = %q, update %v", text, canUpdate)
	}
}

func TestLatestYTDLPVersion(t *testing.T) {
	t.Parallel()

	version, err := latestYTDLPVersion([]byte(`{"tag_name":"2026.11.23"}`))
	if err != nil || version != "2026.11.23" {
		t.Fatalf("latestYTDLPVersion() = %q, %v", version, err)
	}
	if _, err := latestYTDLPVersion([]byte(`{"tag_name":"nightly"}`)); err == nil {
		t.Fatal("nightly tag was accepted")
	}
}

func TestYTDLPUpdateArgs(t *testing.T) {
	t.Parallel()

	cellar := "/opt/homebrew/Cellar/yt-dlp/2026.11.23/bin/yt-dlp"
	name, args, err := ytdlpUpdateArgs(cellar, "/opt/homebrew/bin/brew")
	if err != nil || name != "/opt/homebrew/bin/brew" || joinArgs(args) != "upgrade yt-dlp" {
		t.Fatalf("brew update = %q %q, %v", name, joinArgs(args), err)
	}
	if _, _, err := ytdlpUpdateArgs(cellar, ""); err == nil {
		t.Fatal("brew install without brew succeeded")
	}

	name, args, err = ytdlpUpdateArgs("/usr/local/bin/yt-dlp", "")
	if err != nil || name != "/usr/local/bin/yt-dlp" || joinArgs(args) != "-U" {
		t.Fatalf("self update = %q %q, %v", name, joinArgs(args), err)
	}
}

func TestYTDLPVersionNewer(t *testing.T) {
	t.Parallel()

	if !ytdlpVersionNewer("2026.11.23", "2026.08.19") {
		t.Fatal("November was not newer than August")
	}
	if ytdlpVersionNewer("2026.11.23", "2026.11.23") {
		t.Fatal("the same release was treated as newer")
	}
	if ytdlpVersionNewer("2026.11.22", "2026.11.23") {
		t.Fatal("an older day was treated as newer")
	}
}
