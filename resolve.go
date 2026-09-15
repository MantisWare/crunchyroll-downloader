package main

import (
	"fmt"
	"slices"
)

// resolveWatchEpisodeID returns the content ID to download and the audio
// language it carries, which is the requested one unless a dub fallback was
// needed.
func resolveWatchEpisodeID(contentID string, info EpisodeInfo, requestedAudio string, allowFallback bool) (string, string, bool) {
	if info.EpisodeMetadata.AudioLocale == requestedAudio {
		return contentID, requestedAudio, true
	}

	if len(info.EpisodeMetadata.Versions) > 0 {
		correctGuidI := slices.IndexFunc(info.EpisodeMetadata.Versions, func(v *DubVersion) bool {
			return v != nil && v.AudioLocale == requestedAudio
		})
		if correctGuidI != -1 {
			return info.EpisodeMetadata.Versions[correctGuidI].GUID, requestedAudio, true
		}
	}

	if info.EpisodeMetadata.SeasonID == "" {
		return watchDubFallback(episodeInfoToSeasonEpisode(contentID, info), info, requestedAudio, allowFallback)
	}

	fmt.Printf("Version list unavailable, looking up %s dub via season...\n", requestedAudio)
	episodes := getSeasonEpisodes(info.EpisodeMetadata.SeasonID, requestedAudio)
	epIdx := slices.IndexFunc(episodes, func(e SeasonEpisode) bool {
		return e.EpisodeNumber == info.EpisodeMetadata.EpisodeNumber
	})
	if epIdx == -1 {
		return watchDubFallback(episodeInfoToSeasonEpisode(contentID, info), info, requestedAudio, allowFallback)
	}

	ep := episodes[epIdx]
	if ep.AudioLocale == requestedAudio {
		return ep.ID, requestedAudio, true
	}
	if len(ep.Versions) > 0 {
		dubIdx := slices.IndexFunc(ep.Versions, func(v *DubVersion) bool {
			return v != nil && v.AudioLocale == requestedAudio
		})
		if dubIdx != -1 {
			return ep.Versions[dubIdx].GUID, requestedAudio, true
		}
	}

	return watchDubFallback(ep, info, requestedAudio, allowFallback)
}

// watchDubFallback resolves a single episode whose requested dub is missing,
// either by using the default audio or by reporting the episode as skipped.
func watchDubFallback(candidate SeasonEpisode, info EpisodeInfo, requestedAudio string, allowFallback bool) (string, string, bool) {
	if allowFallback {
		if id, locale, ok := fallbackDub(candidate); ok {
			fmt.Printf("! Episode has no %s dub, downloading %s instead...\n", requestedAudio, languageLabel(locale))
			return id, locale, true
		}
	}

	printWatchUnavailableDubs(info, requestedAudio)
	return "", "", false
}

func printWatchUnavailableDubs(info EpisodeInfo, requestedAudio string) {
	fmt.Printf("! Episode has no %s dub available.\n", requestedAudio)
	if len(info.EpisodeMetadata.Versions) == 0 {
		return
	}
	fmt.Print("  Available dubs: ")
	for i, v := range info.EpisodeMetadata.Versions {
		if v == nil {
			continue
		}
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(audioName(v.AudioLocale))
	}
	fmt.Println()
}

func episodeInfoToSeasonEpisode(id string, info EpisodeInfo) SeasonEpisode {
	return SeasonEpisode{
		ID:                 id,
		Versions:           info.EpisodeMetadata.Versions,
		SeasonNumber:       info.EpisodeMetadata.SeasonNumber,
		EpisodeNumber:      info.EpisodeMetadata.EpisodeNumber,
		SeriesTitle:        info.EpisodeMetadata.SeriesTitle,
		AudioLocale:        info.EpisodeMetadata.AudioLocale,
		Title:              info.Title,
		AvailabilityStarts: info.EpisodeMetadata.AvailabilityStarts,
	}
}

func uniqueSeasonNumbers(seasons []Season) []int {
	seen := make(map[int]bool)
	var numbers []int
	for _, season := range seasons {
		if seen[season.SeasonNumber] {
			continue
		}
		seen[season.SeasonNumber] = true
		numbers = append(numbers, season.SeasonNumber)
	}
	return numbers
}
