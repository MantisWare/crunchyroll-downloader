package main

import "testing"

func TestEpisodeOutputNameIncludesTitle(t *testing.T) {
	t.Parallel()

	info := EpisodeInfo{
		Title: "Dawn and Confusion",
		EpisodeMetadata: EpisodeMetadata{
			SeriesTitle:   "Hell's Paradise",
			SeasonNumber:  1,
			EpisodeNumber: 2,
		},
	}

	actual := episodeOutputName(info, "1080p")
	expected := "Hell's Paradise S01E02 Dawn and Confusion [1080p].mkv"
	if actual != expected {
		t.Fatalf("episodeOutputName() = %q, want %q", actual, expected)
	}
}

func TestEpisodeOutputNameOmitsEmptyOrDuplicateTitle(t *testing.T) {
	t.Parallel()

	info := EpisodeInfo{
		Title: "Hell's Paradise",
		EpisodeMetadata: EpisodeMetadata{
			SeriesTitle:   "Hell's Paradise",
			SeasonNumber:  1,
			EpisodeNumber: 1,
		},
	}

	actual := episodeOutputName(info, "720p")
	expected := "Hell's Paradise S01E01 [720p].mkv"
	if actual != expected {
		t.Fatalf("episodeOutputName() = %q, want %q", actual, expected)
	}
}

func TestSanitizeFilenameReplacesIllegalCharacters(t *testing.T) {
	t.Parallel()

	actual := sanitizeFilename(`Dawn: "Confusion"?`)
	expected := "Dawn_ _Confusion__"
	if actual != expected {
		t.Fatalf("sanitizeFilename() = %q, want %q", actual, expected)
	}
}
