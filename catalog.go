package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/unki2aut/go-mpd"
)

type streamOptions struct {
	Subtitles    []string
	VideoQuality []string
	AudioQuality []string
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func audioLocalesFromSeasons(seasons []Season) []string {
	var locales []string
	for _, season := range seasons {
		if season.AudioLocale != "" {
			locales = append(locales, season.AudioLocale)
		}
		locales = append(locales, season.AudioLocales...)
		for _, version := range season.Versions {
			if version != nil && version.AudioLocale != "" {
				locales = append(locales, version.AudioLocale)
			}
		}
	}
	return uniqueSorted(locales)
}

func audioLocalesFromEpisode(info EpisodeInfo) []string {
	locales := []string{info.EpisodeMetadata.AudioLocale}
	for _, version := range info.EpisodeMetadata.Versions {
		if version != nil && version.AudioLocale != "" {
			locales = append(locales, version.AudioLocale)
		}
	}
	return uniqueSorted(locales)
}

func audioLocalesFromEpisodes(episodes []SeasonEpisode) []string {
	var locales []string
	for _, episode := range episodes {
		if episode.AudioLocale != "" {
			locales = append(locales, episode.AudioLocale)
		}
		for _, version := range episode.Versions {
			if version != nil && version.AudioLocale != "" {
				locales = append(locales, version.AudioLocale)
			}
		}
	}
	return uniqueSorted(locales)
}

func languageLabels(codes []string) []string {
	labels := make([]string, 0, len(codes))
	for _, code := range codes {
		labels = append(labels, languageLabel(code))
	}
	return labels
}

func pickPreferred(options []string, preferred string) string {
	if len(options) == 0 {
		return ""
	}
	if preferred != "" {
		for _, option := range options {
			if option == preferred {
				return option
			}
		}
	}
	return options[0]
}

func videoQualitiesFromSet(set *mpd.AdaptationSet) []string {
	if set == nil {
		return nil
	}
	var qualities []string
	for _, representation := range set.Representations {
		if representation.Height == nil {
			continue
		}
		qualities = append(qualities, fmt.Sprintf("%dp", *representation.Height))
	}
	return sortQualitiesDesc(uniqueSorted(qualities), "p")
}

func audioQualitiesFromSet(set *mpd.AdaptationSet) []string {
	if set == nil {
		return nil
	}
	var qualities []string
	for _, representation := range set.Representations {
		qualities = append(qualities, audioQualityFromRepresentation(representation.ID, representation.Bandwidth))
	}
	return sortQualitiesDesc(uniqueSorted(qualities), "k")
}

func audioQualityFromRepresentation(id *string, bandwidth *uint64) string {
	if id != nil {
		for _, quality := range []string{"192k", "128k", "96k"} {
			if strings.Contains(*id, quality) {
				return quality
			}
		}
	}
	if bandwidth == nil {
		return ""
	}
	kbps := *bandwidth / 1000
	if kbps >= 192 {
		return "192k"
	}
	if kbps >= 128 {
		return "128k"
	}
	if kbps >= 96 {
		return "96k"
	}
	if kbps == 0 {
		return ""
	}
	return fmt.Sprintf("%dk", kbps)
}

func qualitiesFromOnDemand(sets []onDemandAdaptationSet, video bool) []string {
	var qualities []string
	for _, set := range sets {
		if set.IsVideo != video {
			continue
		}
		for _, rep := range set.Representations {
			if video {
				if rep.Height > 0 {
					qualities = append(qualities, fmt.Sprintf("%dp", rep.Height))
				}
				continue
			}
			qualities = append(qualities, audioQualityFromRepresentation(&rep.ID, &rep.Bandwidth))
		}
	}
	suffix := "k"
	if video {
		suffix = "p"
	}
	return sortQualitiesDesc(uniqueSorted(qualities), suffix)
}

func sortQualitiesDesc(qualities []string, suffix string) []string {
	sort.Slice(qualities, func(i, j int) bool {
		left, _ := strconv.Atoi(strings.TrimSuffix(qualities[i], suffix))
		right, _ := strconv.Atoi(strings.TrimSuffix(qualities[j], suffix))
		return left > right
	})
	return qualities
}

func probeStreamOptions(contentID string) (opts streamOptions, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("failed to inspect streams: %v", recovered)
		}
	}()

	episode, getErr := getEpisode(contentID)
	if getErr != nil {
		return streamOptions{}, getErr
	}
	if episode.Token != "" {
		defer deleteStream(contentID, episode.Token)
	}

	for locale := range episode.Subtitles {
		if locale != "" {
			opts.Subtitles = append(opts.Subtitles, locale)
		}
	}
	opts.Subtitles = uniqueSorted(opts.Subtitles)

	manifest, raw, manifestErr := parseManifest(episode.ManifestURL)
	if manifestErr != nil {
		return opts, manifestErr
	}
	if isOnDemand(manifest) {
		sets, parseErr := parseOnDemand(raw)
		if parseErr != nil {
			return opts, nil
		}
		opts.VideoQuality = qualitiesFromOnDemand(sets, true)
		opts.AudioQuality = qualitiesFromOnDemand(sets, false)
		return opts, nil
	}

	videoSet, audioSet := findAdaptationSets(manifest)
	opts.VideoQuality = videoQualitiesFromSet(videoSet)
	opts.AudioQuality = audioQualitiesFromSet(audioSet)
	return opts, nil
}

func probeIDForEpisodes(episodes []SeasonEpisode, requestedAudio string) string {
	if len(episodes) == 0 {
		return ""
	}
	job, ok := resolveSeasonEpisodeJob(episodes[0], requestedAudio)
	if ok {
		return job.id
	}
	return episodes[0].ID
}
