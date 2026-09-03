package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type SeasonEpisodes struct {
	Data []SeasonEpisode `json:"data"`
}

type SeasonEpisode struct {
	ID                 string        `json:"id"`
	Versions           []*DubVersion `json:"versions"`
	SeasonNumber       int           `json:"season_number"`
	EpisodeNumber      int           `json:"episode_number"`
	SeriesTitle        string        `json:"series_title"`
	AudioLocale        string        `json:"audio_locale"`
	Title              string        `json:"title"`
	AvailabilityStarts string        `json:"availability_starts"`
}

func getSeasonEpisodes(contentId string) []SeasonEpisode {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://www.crunchyroll.com/content/v2/cms/seasons/%s/episodes?preferred_audio_language=%s&locale=en-US", contentId, *audioLang), nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
	resp, err := DoRequest(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	var episodes SeasonEpisodes
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if err = json.Unmarshal(body, &episodes); err != nil {
		panic(err)
	}

	return episodes.Data
}

type Seasons struct {
	Data []Season `json:"data"`
}

type Season struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	SeasonNumber int           `json:"season_number"`
	AudioLocale  string        `json:"audio_locale"`
	AudioLocales []string      `json:"audio_locales"`
	Versions     []*DubVersion `json:"versions"`
}

func getSeasons(contentId string) []Season {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://www.crunchyroll.com/content/v2/cms/series/%s/seasons?force_locale=&preferred_audio_language=%s&locale=en-US", contentId, *audioLang), nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
	resp, err := DoRequest(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	var seasons Seasons
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if err = json.Unmarshal(body, &seasons); err != nil {
		panic(err)
	}

	return seasons.Data
}

func seasonMatchesAudio(season Season, audioLang string) bool {
	if season.AudioLocale == audioLang {
		return true
	}
	// Multi-locale audio_locales lists are often attached to the original
	// (usually Japanese) season and do not mean this season ID is the dub.
	if len(season.AudioLocales) == 1 && season.AudioLocales[0] == audioLang {
		return true
	}
	return false
}

func episodeHasAudio(episode SeasonEpisode, audioLang string) bool {
	if episode.AudioLocale == audioLang {
		return true
	}
	for _, version := range episode.Versions {
		if version != nil && version.AudioLocale == audioLang {
			return true
		}
	}
	return false
}

type seasonCandidate struct {
	ID      string
	Trusted bool // selected via exact locale or version GUID for the requested audio
}

func seasonCandidates(seasons []Season, seasonNumber int, audioLang string) []seasonCandidate {
	var matched []Season
	for _, season := range seasons {
		if season.SeasonNumber == seasonNumber {
			matched = append(matched, season)
		}
	}
	if len(matched) == 0 {
		return nil
	}

	var candidates []seasonCandidate
	seen := make(map[string]bool)
	add := func(id string, trusted bool) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		candidates = append(candidates, seasonCandidate{ID: id, Trusted: trusted})
	}

	for _, season := range matched {
		if seasonMatchesAudio(season, audioLang) {
			add(season.ID, true)
		}
	}
	for _, season := range matched {
		for _, version := range season.Versions {
			if version != nil && version.AudioLocale == audioLang {
				add(version.GUID, true)
			}
		}
	}
	// Fallbacks: other same-number seasons and their version GUIDs.
	for _, season := range matched {
		add(season.ID, false)
		for _, version := range season.Versions {
			if version != nil {
				add(version.GUID, version.AudioLocale == audioLang)
			}
		}
	}

	return candidates
}

func collectAvailableAudio(seasons []Season, seasonNumber int, episodes []SeasonEpisode) []string {
	available := make(map[string]bool)
	for _, episode := range episodes {
		if episode.AudioLocale != "" {
			available[episode.AudioLocale] = true
		}
		for _, version := range episode.Versions {
			if version != nil && version.AudioLocale != "" {
				available[version.AudioLocale] = true
			}
		}
	}
	for _, season := range seasons {
		if season.SeasonNumber != seasonNumber {
			continue
		}
		if season.AudioLocale != "" {
			available[season.AudioLocale] = true
		}
		for _, locale := range season.AudioLocales {
			if locale != "" {
				available[locale] = true
			}
		}
		for _, version := range season.Versions {
			if version != nil && version.AudioLocale != "" {
				available[version.AudioLocale] = true
			}
		}
	}

	locales := make([]string, 0, len(available))
	for locale := range available {
		locales = append(locales, locale)
	}
	return locales
}

func printAvailableAudio(locales []string) {
	if len(locales) == 0 {
		return
	}
	fmt.Print("  Available audio: ")
	for i, locale := range locales {
		if i > 0 {
			fmt.Print(", ")
		}
		name := languageNames[locale]
		if name == "" {
			name = locale
		}
		fmt.Printf("%s (%s)", name, locale)
	}
	fmt.Println()
}

// resolveSeasonEpisodes finds episodes for seasonNumber in the requested audio language.
// Crunchyroll often lists each dub as its own season with the same season_number.
func resolveSeasonEpisodes(seasons []Season, seasonNumber int, audioLang string) ([]SeasonEpisode, bool) {
	candidates := seasonCandidates(seasons, seasonNumber, audioLang)
	if len(candidates) == 0 {
		return nil, false
	}

	var probed []SeasonEpisode
	for _, candidate := range candidates {
		episodes := getSeasonEpisodes(candidate.ID)
		if len(episodes) == 0 {
			continue
		}
		probed = episodes

		// Trusted IDs were chosen for this audio language at the season/version
		// level. Episode metadata is sometimes blank or still lists the original
		// locale, so accept them without requiring episodeHasAudio.
		if candidate.Trusted || episodeHasAudio(episodes[0], audioLang) {
			return episodes, true
		}
	}

	fmt.Printf("! Season %v has no %s audio available.\n", seasonNumber, audioLang)
	printAvailableAudio(collectAvailableAudio(seasons, seasonNumber, probed))
	return nil, false
}

// seasonsForAudio returns one season entry per season_number, resolved for audioLang.
func seasonsForAudio(seasons []Season, audioLang string) []Season {
	seen := make(map[int]bool)
	var result []Season

	for _, season := range seasons {
		if seen[season.SeasonNumber] {
			continue
		}
		seen[season.SeasonNumber] = true

		candidates := seasonCandidates(seasons, season.SeasonNumber, audioLang)
		if len(candidates) == 0 {
			continue
		}
		chosen := candidates[0]
		for _, candidate := range candidates {
			if candidate.Trusted {
				chosen = candidate
				break
			}
		}
		result = append(result, Season{
			ID:           chosen.ID,
			SeasonNumber: season.SeasonNumber,
			Title:        season.Title,
		})
	}

	return result
}
