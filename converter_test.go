package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConversionOutputName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		quality  string
		expected string
	}{
		{
			name:     "adds quality and mp4 extension",
			input:    "/videos/Episode 01.mkv",
			quality:  "720p",
			expected: "Episode 01 [720p].mp4",
		},
		{
			name:     "does not duplicate matching quality",
			input:    "/videos/Episode 01 [720p].mov",
			quality:  "720p",
			expected: "Episode 01 [720p].mp4",
		},
		{
			name:     "keeps episode title and replaces quality tag",
			input:    "/videos/Hell's Paradise S01E02 Dawn and Confusion [1080p].mkv",
			quality:  "720p",
			expected: "Hell's Paradise S01E02 Dawn and Confusion [720p].mp4",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual := conversionOutputName(test.input, test.quality, "")
			if actual != test.expected {
				t.Fatalf("conversionOutputName() = %q, want %q", actual, test.expected)
			}
		})
	}
}

func TestConversionJobsForFolder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"Episode 02.mkv", "Episode 01.mp4", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
	}

	jobs, err := conversionJobsForFolder(dir, "480p", "")
	if err != nil {
		t.Fatalf("conversionJobsForFolder() error = %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("conversionJobsForFolder() returned %d jobs, want 2", len(jobs))
	}

	expectedDir := filepath.Join(dir, "converted_480p")
	if filepath.Dir(jobs[0].Output) != expectedDir {
		t.Fatalf("output directory = %q, want %q", filepath.Dir(jobs[0].Output), expectedDir)
	}
	if jobs[0].Output != filepath.Join(expectedDir, "Episode 01 [480p].mp4") {
		t.Fatalf("first output = %q", jobs[0].Output)
	}
}

func TestConversionJobsForFilesDoesNotOverwriteInput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Episode 01 [720p].mp4")
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	jobs, err := conversionJobsForFiles([]string{path}, "720p", "")
	if err != nil {
		t.Fatalf("conversionJobsForFiles() error = %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("conversionJobsForFiles() returned %d jobs, want 1", len(jobs))
	}
	if jobs[0].Output == path {
		t.Fatal("conversion output must not overwrite its input")
	}
}

func TestConversionRotation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rotation string
		filter   string
		name     string
	}{
		{rotation: "", filter: "scale=-2:720", name: "Episode 01 [720p].mp4"},
		{rotation: "No change", filter: "scale=-2:720", name: "Episode 01 [720p].mp4"},
		{rotation: "90° clockwise", filter: "scale=-2:720,transpose=1", name: "Episode 01 [720p] [90cw].mp4"},
		{rotation: "180°", filter: "scale=-2:720,transpose=1,transpose=1", name: "Episode 01 [720p] [180].mp4"},
		{rotation: "90° counterclockwise", filter: "scale=-2:720,transpose=2", name: "Episode 01 [720p] [90ccw].mp4"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.rotation, func(t *testing.T) {
			t.Parallel()
			filter, err := conversionVideoFilter("720p", test.rotation)
			if err != nil {
				t.Fatalf("conversionVideoFilter() error = %v", err)
			}
			if filter != test.filter {
				t.Fatalf("conversionVideoFilter() = %q, want %q", filter, test.filter)
			}
			got := conversionOutputName("/videos/Episode 01.mkv", "720p", test.rotation)
			if got != test.name {
				t.Fatalf("conversionOutputName() = %q, want %q", got, test.name)
			}
		})
	}

	replaced := conversionOutputName("/videos/Episode 01 [1080p] [90ccw].mkv", "720p", "90° clockwise")
	if replaced != "Episode 01 [720p] [90cw].mp4" {
		t.Fatalf("replaced rotation name = %q", replaced)
	}
	if _, err := conversionVideoFilter("720p", "sideways"); err == nil {
		t.Fatal("unknown rotation was accepted")
	}
}
