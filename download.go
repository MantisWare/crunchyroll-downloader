package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	widevine "github.com/iyear/gowidevine"
	"github.com/unki2aut/go-mpd"
)

const maxWorkers = 10

func buildUrl(base, representationId, file string, partNum *int64) string {
	if partNum != nil {
		file = strings.ReplaceAll(file, "$Number$", fmt.Sprintf("%05d", *partNum))
		file = strings.ReplaceAll(file, "$Number%05d$", fmt.Sprintf("%05d", *partNum))
	}
	return base + strings.ReplaceAll(file, "$RepresentationID$", representationId)
}

func downloadPart(url string) ([]byte, error) {
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Origin", "https://static.crunchyroll.com")
		req.Header.Set("Referer", "https://static.crunchyroll.com/")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if attempt < maxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, err)
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			if attempt < maxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("failed after %d retries, status: %d", maxRetries, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			if attempt < maxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("failed reading body after %d retries: %w", maxRetries, err)
		}
		return body, nil
	}
	return nil, fmt.Errorf("failed after %d retries", maxRetries)
}

func getFilename(set *mpd.AdaptationSet) string {
	if set == nil {
		f, _ := os.CreateTemp("", "crdl-subs-*.ass")
		return f.Name()
	}
	for _, representation := range set.Representations {
		if representation.Height != nil {
			f, _ := os.CreateTemp("", "crdl-video-*.mp4")
			return f.Name()
		} else if representation.Bandwidth != nil {
			f, _ := os.CreateTemp("", "crdl-audio-*.mp3")
			return f.Name()
		}
	}
	return ""
}

type segmentJob struct {
	index int
	url   string
}

func downloadParts(baseUrl, representationId *string, set *mpd.AdaptationSet) (string, error) {
	initUrl := buildUrl(*baseUrl, *representationId, *set.SegmentTemplate.Initialization, nil)
	initData, err := downloadPart(initUrl)
	if err != nil {
		return "", err
	}

	timeline := expandTimeline(set.SegmentTemplate.SegmentTimeline.S, 1)
	total := len(timeline)
	results := make([][]byte, total)
	var downloadErr error
	var errOnce sync.Once
	var done atomic.Int64

	jobs := make(chan segmentJob, total)
	var wg sync.WaitGroup

	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				data, err := downloadPart(job.url)
				if err != nil {
					errOnce.Do(func() { downloadErr = err })
					return
				}
				results[job.index] = data
				count := done.Add(1)
				fmt.Printf("\rDownloaded %v of %v segments (%v%%)", count, total, (100*count)/int64(total))
			}
		}()
	}

	for i, item := range timeline {
		url := buildUrl(*baseUrl, *representationId, *set.SegmentTemplate.Media, &item)
		jobs <- segmentJob{index: i, url: url}
	}
	close(jobs)
	wg.Wait()

	if downloadErr != nil {
		return "", downloadErr
	}

	fmt.Println("\nFinished downloading!")

	var parts []byte
	parts = append(parts, initData...)
	for _, data := range results {
		parts = append(parts, data...)
	}

	filename := getFilename(set)
	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if err = widevine.DecryptMP4Auto(io.NopCloser(bytes.NewReader(parts)), keys, file); err != nil {
		return "", fmt.Errorf("widevine.DecryptMP4Auto: %w", err)
	}

	return filename, nil
}

func downloadSubs(url string) string {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Origin", "https://static.crunchyroll.com")
	req.Header.Set("Referer", "https://static.crunchyroll.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	filename := getFilename(nil)
	file, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	file.Write(body)
	file.Close()

	return filename
}

func downloadEpisode(contentId string, videoQuality, audioQuality, subtitlesLang *string, info EpisodeInfo) {
	sanitize := func(s string) string {
		illegal := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|"}
		res := s
		for _, char := range illegal {
			res = strings.ReplaceAll(res, char, "_")
		}
		return strings.TrimRight(res, " .")
	}

	cleanSeriesTitle := sanitize(info.EpisodeMetadata.SeriesTitle)

	if _, err := os.Stat(cleanSeriesTitle); err != nil {
		_ = os.MkdirAll(cleanSeriesTitle, 0777)
	}

	outputFile := fmt.Sprintf("%s/%s S%02vE%02v [%s].mkv",
		cleanSeriesTitle,
		cleanSeriesTitle,
		info.EpisodeMetadata.SeasonNumber,
		info.EpisodeMetadata.EpisodeNumber,
		*videoQuality,
	)

	if _, err := os.Stat(outputFile); err == nil {
		fmt.Printf("Episode %v is already downloaded, skipping...\n", info.EpisodeMetadata.EpisodeNumber)
		return
	}

	var episode Episode
	var err error

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		episode, err = getEpisode(contentId)
		if err == nil {
			break
		}

		if strings.Contains(err.Error(), "TOO_MANY_ACTIVE_STREAMS") && attempt < maxRetries {
			wait := time.Duration(attempt) * 10 * time.Second
			fmt.Printf("Too many active streams, waiting %v before retry (%d/%d)...\n", wait, attempt, maxRetries)
			time.Sleep(wait)
			continue
		}

		fmt.Printf("! Error fetching episode S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
		return
	}

	defer func() {
		if episode.Token != "" {
			if success := deleteStream(contentId, episode.Token); !success {
				fmt.Println("Warning: failed to release the playback stream.")
			}
		}
	}()

	fmt.Printf("Downloading: %s (S%02vE%02v) from %s\n", info.Title, info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, info.EpisodeMetadata.SeriesTitle)

	manifest := parseManifest(episode.ManifestURL)
	pssh := getPssh(manifest)
	if pssh == nil {
		fmt.Printf("! PSSH not found for S%02vE%02v, skipping...\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
		return
	}
	videoSet := manifest.Period[0].AdaptationSets[0]
	audioSet := manifest.Period[0].AdaptationSets[1]

	err = getLicense(*pssh, contentId, episode.Token)
	if err != nil {
		fmt.Printf("! License error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
		return
	}

	var subsFile string
	if *audioLang == *subtitlesLang {
		fmt.Println("Skipping subtitles (audio language matches subtitle language)")
	} else if subtitles := episode.Subtitles[*subtitlesLang]; subtitles != nil {
		fmt.Printf("Downloading subtitles for %s language...\n", languageNames[*subtitlesLang])
		subsFile = downloadSubs(subtitles.URL)
		fmt.Println("Downloaded subtitles!")
	}

	baseUrl, representationId := getBaseUrl(videoSet, true, *videoQuality)
	if baseUrl == nil {
		fmt.Printf("! Failed to get the video base URL for S%02vE%02v, check -video-quality\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
		return
	}
	videoFile, err := downloadParts(baseUrl, representationId, videoSet)
	if err != nil {
		fmt.Printf("! Video download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
		return
	}

	audioBaseUrl, audioRepresentationId := getBaseUrl(audioSet, false, *audioQuality)
	if audioBaseUrl == nil {
		fmt.Printf("! Failed to get the audio base URL for S%02vE%02v, check -audio-quality\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
		_ = os.Remove(videoFile)
		return
	}
	audioFile, err := downloadParts(audioBaseUrl, audioRepresentationId, audioSet)
	if err != nil {
		fmt.Printf("! Audio download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
		_ = os.Remove(videoFile)
		return
	}

	mergeEverything(videoFile, audioFile, subsFile, outputFile, subtitlesLang, info)
}

func downloadSeason(videoQuality, audioQuality, subtitlesLang *string, episodes []SeasonEpisode) {
	fmt.Printf("Downloading season %v of %s (%v episodes)\n\n", episodes[0].SeasonNumber, episodes[0].SeriesTitle, len(episodes))

	for _, episode := range episodes {
		episodeId := episode.ID

		if episode.AudioLocale != *audioLang {
			correctGuidI := slices.IndexFunc(episode.Versions, func(v *DubVersion) bool {
				return v.AudioLocale == *audioLang
			})

			if correctGuidI == -1 {
				fmt.Printf("! Episode %v has no %s dub available, skipping...\n", episode.EpisodeNumber, *audioLang)
				continue
			}
			episodeId = episode.Versions[correctGuidI].GUID
		}

		info := EpisodeInfo{
			EpisodeMetadata: EpisodeMetadata{
				SeriesTitle:        episode.SeriesTitle,
				SeasonNumber:       episode.SeasonNumber,
				EpisodeNumber:      episode.EpisodeNumber,
				AudioLocale:        *audioLang,
				Versions:           episode.Versions,
				AvailabilityStarts: episode.AvailabilityStarts,
			},
			Title: episode.Title,
		}
		downloadEpisode(episodeId, videoQuality, audioQuality, subtitlesLang, info)
	}
}
