package main

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestJobPrintfWritesToProvidedWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	env := jobEnv{log: &buf}
	jobPrintf(env, "hello %s\n", "world")
	if buf.String() != "hello world\n" {
		t.Fatalf("jobPrintf wrote %q, want %q", buf.String(), "hello world\n")
	}
}

func TestSeriesDirNameUsesProvidedDirectory(t *testing.T) {
	t.Parallel()

	info := EpisodeInfo{
		EpisodeMetadata: EpisodeMetadata{SeriesTitle: "Hell's Paradise"},
	}
	got := seriesDirName(info, "/tmp/out")
	want := filepath.Join("/tmp/out", "Hell's Paradise")
	if got != want {
		t.Fatalf("seriesDirName() = %q, want %q", got, want)
	}
}
