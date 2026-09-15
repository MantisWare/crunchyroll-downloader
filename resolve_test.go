package main

import (
	"io"
	"testing"
)

func testJobEnv() jobEnv {
	return jobEnv{log: io.Discard}
}

func TestResolveSeasonEpisodeJobUsesRequestedDub(t *testing.T) {
	episode := SeasonEpisode{
		ID:            "japanese-guid",
		AudioLocale:   "ja-JP",
		EpisodeNumber: 21,
		Versions: []*DubVersion{
			{GUID: "japanese-guid", AudioLocale: "ja-JP"},
			{GUID: "english-guid", AudioLocale: "en-US"},
		},
	}

	job, ok := resolveSeasonEpisodeJob(testJobEnv(), episode, "en-US", false)
	if !ok {
		t.Fatal("resolveSeasonEpisodeJob() failed for an available dub")
	}
	if job.id != "english-guid" {
		t.Errorf("job.id = %q, want %q", job.id, "english-guid")
	}
	if job.info.EpisodeMetadata.AudioLocale != "en-US" {
		t.Errorf("audio locale = %q, want %q", job.info.EpisodeMetadata.AudioLocale, "en-US")
	}
}

func TestResolveSeasonEpisodeJobSkipsMissingDubWithoutFallback(t *testing.T) {
	episode := SeasonEpisode{
		ID:            "japanese-guid",
		AudioLocale:   "ja-JP",
		EpisodeNumber: 21,
	}

	if _, ok := resolveSeasonEpisodeJob(testJobEnv(), episode, "en-US", false); ok {
		t.Fatal("resolveSeasonEpisodeJob() resolved a missing dub with the fallback disabled")
	}
}

func TestResolveSeasonEpisodeJobFallsBackToDefaultDub(t *testing.T) {
	episode := SeasonEpisode{
		ID:            "japanese-guid",
		AudioLocale:   "ja-JP",
		EpisodeNumber: 21,
	}

	job, ok := resolveSeasonEpisodeJob(testJobEnv(), episode, "en-US", true)
	if !ok {
		t.Fatal("resolveSeasonEpisodeJob() skipped the episode instead of falling back")
	}
	if job.id != "japanese-guid" {
		t.Errorf("job.id = %q, want %q", job.id, "japanese-guid")
	}
	if job.info.EpisodeMetadata.AudioLocale != "ja-JP" {
		t.Errorf("audio locale = %q, want the locale actually downloaded (%q)", job.info.EpisodeMetadata.AudioLocale, "ja-JP")
	}
}

func TestResolveSeasonEpisodeJobFallsBackToFirstVersion(t *testing.T) {
	episode := SeasonEpisode{
		ID:            "listing-guid",
		EpisodeNumber: 21,
		Versions: []*DubVersion{
			nil,
			{GUID: "japanese-guid", AudioLocale: "ja-JP"},
		},
	}

	job, ok := resolveSeasonEpisodeJob(testJobEnv(), episode, "en-US", true)
	if !ok {
		t.Fatal("resolveSeasonEpisodeJob() skipped the episode instead of falling back")
	}
	if job.id != "japanese-guid" {
		t.Errorf("job.id = %q, want %q", job.id, "japanese-guid")
	}
	if job.info.EpisodeMetadata.AudioLocale != "ja-JP" {
		t.Errorf("audio locale = %q, want %q", job.info.EpisodeMetadata.AudioLocale, "ja-JP")
	}
}

func TestFallbackDubReportsNoAudio(t *testing.T) {
	if _, _, ok := fallbackDub(SeasonEpisode{ID: "guid"}); ok {
		t.Fatal("fallbackDub() reported audio for an episode without any")
	}
}
