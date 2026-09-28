package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type Episode struct {
	// Dash manifest file URL
	ManifestURL string `json:"url"`
	// List of .ass files
	Subtitles map[string]*Subtitle `json:"subtitles"`
	// Token to give to the Widevine CDM challenge
	Token string `json:"token"`
}

// playbackSlot serializes opening a Crunchyroll stream. Accounts only allow a
// small number of concurrent playback tokens, so two tabs calling /play while
// another download still holds a token would fail.
var playbackSlot sync.Mutex

type Subtitle struct {
	// Language represents a subtitle language in the "en-US" format
	Language string `json:"language"`
	// Direct URL to the .ass file
	URL string `json:"url"`
}

func getEpisode(id string) (Episode, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://www.crunchyroll.com/playback/v3/%s/web/firefox/play", id), nil)
	if err != nil {
		return Episode{}, fmt.Errorf("creating playback request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+currentToken())
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
	resp, err := DoRequest(req)
	if err != nil {
		return Episode{}, fmt.Errorf("playback request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Episode{}, fmt.Errorf("reading playback response: %w", err)
	}
	return parsePlaybackResponse(body)
}

type playbackResponse struct {
	ManifestURL string               `json:"url"`
	Subtitles   map[string]*Subtitle `json:"subtitles"`
	Token       string               `json:"token"`
	Error       json.RawMessage      `json:"error"`
	Code        string               `json:"code"`
	Message     string               `json:"message"`
	Reason      string               `json:"reason"`
}

func parsePlaybackResponse(body []byte) (Episode, error) {
	var raw playbackResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Episode{}, fmt.Errorf("parsing playback response: %w", err)
	}
	if msg := playbackErrorMessage(raw); msg != "" {
		return Episode{}, fmt.Errorf("crunchyroll: %s", msg)
	}
	return Episode{
		ManifestURL: raw.ManifestURL,
		Subtitles:   raw.Subtitles,
		Token:       raw.Token,
	}, nil
}

func playbackErrorMessage(raw playbackResponse) string {
	value, isError := decodePlaybackErrorField(raw.Error)
	if !isError && raw.Code == "" && raw.Message == "" && raw.Reason == "" {
		return ""
	}
	if !isError && raw.ManifestURL != "" {
		return ""
	}

	label := playbackErrorLabel(value, raw.Code)
	parts := uniqueNonEmpty(label, raw.Code, value, raw.Reason, raw.Message)
	return strings.Join(parts, ": ")
}

func playbackErrorLabel(value, code string) string {
	for _, candidate := range []string{code, value} {
		if isPlaybackRateLimitCode(candidate) {
			return "rate limited (too many requests)"
		}
	}
	return ""
}

func isPlaybackRateLimitCode(code string) bool {
	n, err := strconv.Atoi(strings.TrimSpace(code))
	if err != nil {
		return false
	}
	return n >= 4290 && n <= 4299
}

func decodePlaybackErrorField(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, asString != ""
	}

	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		if asNumber == "0" {
			return "", false
		}
		return asNumber.String(), true
	}

	var asBool bool
	if err := json.Unmarshal(raw, &asBool); err == nil {
		return "", asBool
	}

	var asObject map[string]any
	if err := json.Unmarshal(raw, &asObject); err == nil {
		code, _ := asObject["code"].(string)
		message, _ := asObject["message"].(string)
		joined := strings.Join(uniqueNonEmpty(code, message), ": ")
		return joined, joined != ""
	}

	return strings.TrimSpace(string(raw)), true
}

func uniqueNonEmpty(values ...string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func isRetryablePlaybackError(err error) bool {
	if err == nil {
		return false
	}
	if isRateLimitedPlaybackError(err) {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "TOO_MANY_ACTIVE_STREAMS") || strings.Contains(msg, "ACTIVE_STREAM")
}

func isRateLimitedPlaybackError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "rate limited") || strings.Contains(msg, "too many requests") {
		return true
	}
	trimmed := strings.TrimSpace(err.Error())
	trimmed = strings.TrimPrefix(trimmed, "crunchyroll: ")
	return isPlaybackRateLimitCode(trimmed)
}

type EpisodeMetadataResponse struct {
	Data []EpisodeInfo `json:"data"`
}

type EpisodeInfo struct {
	EpisodeMetadata EpisodeMetadata `json:"episode_metadata"`
	// Episode title
	Title string `json:"title"`
}

type EpisodeMetadata struct {
	AudioLocale   string `json:"audio_locale"`
	EpisodeNumber int    `json:"episode_number"`
	SeasonNumber  int    `json:"season_number"`
	SeriesTitle   string `json:"series_title"`
	SeasonID      string `json:"season_id"`
	SeriesID      string `json:"series_id"`
	// AvailabilityStarts represents the date when the episode was released on Crunchyroll
	AvailabilityStarts string        `json:"availability_starts"`
	Versions           []*DubVersion `json:"versions"`
}

type DubVersion struct {
	AudioLocale string `json:"audio_locale"`
	GUID        string `json:"guid"`
}

func getEpisodeInfo(id, audioLang string) EpisodeInfo {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://www.crunchyroll.com/content/v2/cms/objects/%s?ratings=true&preferred_audio_language=%s&locale=en-US", id, audioLang), nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer "+currentToken())
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
	resp, err := DoRequest(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var info EpisodeMetadataResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if err = json.Unmarshal(body, &info); err != nil {
		panic(err)
	}

	return info.Data[0]
}

// deleteStream removes the stream to make Crunchyroll think we "left" the playback
func deleteStream(contentId, sToken string) bool {
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("https://www.crunchyroll.com/playback/v1/token/%s/%s", contentId, sToken), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+currentToken())
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
	resp, err := DoRequest(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusNoContent
}
