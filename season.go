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
	for _, locale := range season.AudioLocales {
		if locale == audioLang {
			return true
		}
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

func seasonCandidateIDs(seasons []Season, seasonNumber int, audioLang string) []string {
	var candidates []Season
	for _, season := range seasons {
		if season.SeasonNumber == seasonNumber {
			candidates = append(candidates, season)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	var ids []string
	seen := make(map[string]bool)
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	for _, season := range candidates {
		if seasonMatchesAudio(season, audioLang) {
			add(season.ID)
		}
	}
	for _, season := range candidates {
		for _, version := range season.Versions {
			if version != nil && version.AudioLocale == audioLang {
				add(version.GUID)
			}
		}
	}
	for _, season := range candidates {
		add(season.ID)
	}

	return ids
}

// resolveSeasonEpisodes finds episodes for seasonNumber in the requested audio language.
// Crunchyroll often lists each dub as its own season with the same season_number.
func resolveSeasonEpisodes(seasons []Season, seasonNumber int, audioLang string) ([]SeasonEpisode, bool) {
	ids := seasonCandidateIDs(seasons, seasonNumber, audioLang)
	if len(ids) == 0 {
		return nil, false
	}

	available := make(map[string]bool)
	for _, id := range ids {
		episodes := getSeasonEpisodes(id)
		if len(episodes) == 0 {
			continue
		}
		if episodeHasAudio(episodes[0], audioLang) {
			return episodes, true
		}
		available[episodes[0].AudioLocale] = true
		for _, version := range episodes[0].Versions {
			if version != nil {
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
			available[locale] = true
		}
		for _, version := range season.Versions {
			if version != nil {
				available[version.AudioLocale] = true
			}
		}
	}

	fmt.Printf("! Season %v has no %s audio available.\n", seasonNumber, audioLang)
	if len(available) > 0 {
		fmt.Print("  Available audio: ")
		first := true
		for locale := range available {
			if locale == "" {
				continue
			}
			if !first {
				fmt.Print(", ")
			}
			first = false
			name := languageNames[locale]
			if name == "" {
				name = locale
			}
			fmt.Printf("%s (%s)", name, locale)
		}
		fmt.Println()
	}
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

		ids := seasonCandidateIDs(seasons, season.SeasonNumber, audioLang)
		if len(ids) == 0 {
			continue
		}
		result = append(result, Season{
			ID:           ids[0],
			SeasonNumber: season.SeasonNumber,
			Title:        season.Title,
		})
	}

	return result
}
