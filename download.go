package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	widevine "github.com/iyear/gowidevine"
	"github.com/unki2aut/go-mpd"
)

const maxWorkers = 10
const minCompleteEpisodeBytes int64 = 256 * 1024
const seasonRetryPasses = 2

var seasonEpisodeName = regexp.MustCompile(`(?i)S(\d+)E(\d+)`)

func sanitizeFilename(s string) string {
	illegal := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|"}
	res := s
	for _, char := range illegal {
		res = strings.ReplaceAll(res, char, "_")
	}
	return strings.TrimRight(res, " .")
}

func episodeOutputFile(info EpisodeInfo, videoQuality string) string {
	cleanSeriesTitle := sanitizeFilename(info.EpisodeMetadata.SeriesTitle)
	dir := filepath.Join(outputDir, cleanSeriesTitle)
	return filepath.Join(dir, fmt.Sprintf("%s S%02vE%02v [%s].mkv",
		cleanSeriesTitle,
		info.EpisodeMetadata.SeasonNumber,
		info.EpisodeMetadata.EpisodeNumber,
		videoQuality,
	))
}

func episodeOutputComplete(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Size() >= minCompleteEpisodeBytes
}

func seriesDirName(info EpisodeInfo) string {
	return filepath.Join(outputDir, sanitizeFilename(info.EpisodeMetadata.SeriesTitle))
}

func parseSeasonEpisode(filename string) (int, int, bool) {
	matches := seasonEpisodeName.FindStringSubmatch(filename)
	if len(matches) != 3 {
		return 0, 0, false
	}
	season, seasonErr := strconv.Atoi(matches[1])
	episode, episodeErr := strconv.Atoi(matches[2])
	if seasonErr != nil || episodeErr != nil {
		return 0, 0, false
	}
	return season, episode, true
}

// findCompleteEpisodeFile looks for an existing MKV for this season/episode in
// the series folder, including files that do not match the exact output name.
func findCompleteEpisodeFile(info EpisodeInfo, videoQuality string) (string, bool) {
	expected := episodeOutputFile(info, videoQuality)
	if episodeOutputComplete(expected) {
		return expected, true
	}

	dir := seriesDirName(info)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	wantSeason := info.EpisodeMetadata.SeasonNumber
	wantEpisode := info.EpisodeMetadata.EpisodeNumber
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".mkv") {
			continue
		}
		season, episode, ok := parseSeasonEpisode(name)
		if !ok || season != wantSeason || episode != wantEpisode {
			continue
		}
		path := filepath.Join(dir, name)
		if episodeOutputComplete(path) {
			return path, true
		}
	}

	return "", false
}

type seasonEpisodeJob struct {
	id   string
	info EpisodeInfo
}

func printUnavailableDubs(episode SeasonEpisode, audioLang string) {
	fmt.Printf("! Episode %v has no %s dub available, skipping...\n", episode.EpisodeNumber, audioLang)
	if len(episode.Versions) > 0 {
		fmt.Print("  Available dubs: ")
		for i, v := range episode.Versions {
			if v == nil {
				continue
			}
			if i > 0 {
				fmt.Print(", ")
			}
			name := languageNames[v.AudioLocale]
			if name == "" {
				name = v.AudioLocale
			}
			fmt.Print(name)
		}
		fmt.Println()
		return
	}
	if episode.AudioLocale != "" {
		name := languageNames[episode.AudioLocale]
		if name == "" {
			name = episode.AudioLocale
		}
		fmt.Printf("  Available audio: %s\n", name)
	}
}

func resolveSeasonEpisodeJob(episode SeasonEpisode, audioLang string) (seasonEpisodeJob, bool) {
	episodeId := episode.ID
	if episode.AudioLocale == audioLang {
		// Already the requested dub.
	} else if correctGuidI := slices.IndexFunc(episode.Versions, func(v *DubVersion) bool {
		return v != nil && v.AudioLocale == audioLang
	}); correctGuidI != -1 {
		episodeId = episode.Versions[correctGuidI].GUID
	} else if episode.AudioLocale == "" && len(episode.Versions) == 0 {
		// Language-specific season whose episode metadata omits audio_locale.
	} else {
		printUnavailableDubs(episode, audioLang)
		return seasonEpisodeJob{}, false
	}

	return seasonEpisodeJob{
		id: episodeId,
		info: EpisodeInfo{
			EpisodeMetadata: EpisodeMetadata{
				SeriesTitle:        episode.SeriesTitle,
				SeasonNumber:       episode.SeasonNumber,
				EpisodeNumber:      episode.EpisodeNumber,
				AudioLocale:        audioLang,
				Versions:           episode.Versions,
				AvailabilityStarts: episode.AvailabilityStarts,
			},
			Title: episode.Title,
		},
	}, true
}

func incompleteSeasonJobs(jobs []seasonEpisodeJob, videoQuality string) []seasonEpisodeJob {
	var missing []seasonEpisodeJob
	for _, job := range jobs {
		if _, ok := findCompleteEpisodeFile(job.info, videoQuality); !ok {
			missing = append(missing, job)
		}
	}
	return missing
}

func formatEpisodeNumbers(jobs []seasonEpisodeJob) string {
	parts := make([]string, 0, len(jobs))
	for _, job := range jobs {
		parts = append(parts, fmt.Sprintf("E%02v", job.info.EpisodeMetadata.EpisodeNumber))
	}
	return strings.Join(parts, ", ")
}

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
	if baseUrl == nil || representationId == nil || set == nil {
		return "", fmt.Errorf("missing base URL, representation ID, or adaptation set")
	}

	template := resolveSegmentTemplate(set, *representationId)
	if template == nil || template.Initialization == nil || template.Media == nil {
		return "", fmt.Errorf("manifest is missing SegmentTemplate (initialization/media)")
	}
	if template.SegmentTimeline == nil {
		return "", fmt.Errorf("manifest is missing SegmentTimeline")
	}

	initUrl := buildUrl(*baseUrl, *representationId, *template.Initialization, nil)
	initData, err := downloadPart(initUrl)
	if err != nil {
		return "", err
	}

	startNumber := int64(1)
	if template.StartNumber != nil {
		startNumber = int64(*template.StartNumber)
	}
	timeline := expandTimeline(template.SegmentTimeline.S, startNumber)
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
		url := buildUrl(*baseUrl, *representationId, *template.Media, &item)
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

func downloadEpisode(contentId string, videoQuality, audioQuality, subtitlesLang *string, info EpisodeInfo) bool {
	outputFile := episodeOutputFile(info, *videoQuality)
	if existing, ok := findCompleteEpisodeFile(info, *videoQuality); ok {
		fmt.Printf("Episode %v is already downloaded (%s), skipping...\n", info.EpisodeMetadata.EpisodeNumber, filepath.Base(existing))
		return true
	}
	if err := os.MkdirAll(filepath.Dir(outputFile), 0777); err != nil {
		fmt.Printf("! Failed to create output directory: %s\n", err)
		return false
	}
	if _, err := os.Stat(outputFile); err == nil {
		fmt.Printf("Episode %v is incomplete (%s), re-downloading...\n", info.EpisodeMetadata.EpisodeNumber, outputFile)
		_ = os.Remove(outputFile)
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
		return false
	}

	defer func() {
		if episode.Token != "" {
			if success := deleteStream(contentId, episode.Token); !success {
				fmt.Println("Warning: failed to release the playback stream.")
			}
		}
	}()

	fmt.Printf("Downloading: %s (S%02vE%02v) from %s\n", info.Title, info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, info.EpisodeMetadata.SeriesTitle)

	manifest, rawManifest := parseManifest(episode.ManifestURL)
	videoSet, audioSet := findAdaptationSets(manifest)
	if videoSet == nil || audioSet == nil {
		fmt.Printf("! Could not find video/audio adaptation sets for S%02vE%02v\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
		return false
	}

	pssh := getPssh(manifest, rawManifest)
	if pssh == nil && !isOnDemand(manifest) {
		baseUrl, representationId := getBaseUrl(videoSet, true, *videoQuality)
		if baseUrl != nil && representationId != nil {
			template := resolveSegmentTemplate(videoSet, *representationId)
			if template != nil && template.Initialization != nil {
				initUrl := buildUrl(*baseUrl, *representationId, *template.Initialization, nil)
				if initData, initErr := downloadPart(initUrl); initErr == nil {
					pssh = extractWidevinePsshFromInit(initData)
				}
			}
		}
	}
	if pssh == nil && isOnDemand(manifest) {
		sets, parseErr := parseOnDemand(rawManifest)
		if parseErr == nil {
			for _, set := range sets {
				if !set.IsVideo {
					continue
				}
				rep, ok := selectOnDemandRepresentation(set, *videoQuality, true)
				if !ok || rep.InitRange == "" {
					continue
				}
				initStart, initEnd, rangeErr := parseByteRange(rep.InitRange)
				if rangeErr != nil {
					continue
				}
				if initData, initErr := downloadRange(rep.BaseURL, initStart, initEnd); initErr == nil {
					pssh = extractWidevinePsshFromInit(initData)
					if pssh != nil {
						break
					}
				}
			}
		}
	}
	if pssh == nil {
		fmt.Printf("! PSSH not found for S%02vE%02v, skipping...\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
		return false
	}

	err = getLicense(*pssh, contentId, episode.Token)
	if err != nil {
		fmt.Printf("! License error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
		return false
	}

	var subsFile string
	if *audioLang == *subtitlesLang {
		fmt.Println("Skipping subtitles (audio language matches subtitle language)")
	} else if subtitles := episode.Subtitles[*subtitlesLang]; subtitles != nil {
		fmt.Printf("Downloading subtitles for %s language...\n", languageNames[*subtitlesLang])
		subsFile = downloadSubs(subtitles.URL)
		fmt.Println("Downloaded subtitles!")
	}

	var videoFile string
	var audioFile string

	if isOnDemand(manifest) {
		sets, parseErr := parseOnDemand(rawManifest)
		if parseErr != nil {
			fmt.Printf("! Failed to parse on-demand manifest for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, parseErr)
			return false
		}
		videoFile, err = downloadOnDemandAdaptation(sets, true, *videoQuality)
		if err != nil {
			fmt.Printf("! Video download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
			return false
		}
		audioFile, err = downloadOnDemandAdaptation(sets, false, *audioQuality)
		if err != nil {
			fmt.Printf("! Audio download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
			_ = os.Remove(videoFile)
			return false
		}
	} else {
		baseUrl, representationId := getBaseUrl(videoSet, true, *videoQuality)
		if baseUrl == nil {
			fmt.Printf("! Failed to get the video base URL for S%02vE%02v, check -video-quality\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
			return false
		}
		videoFile, err = downloadParts(baseUrl, representationId, videoSet)
		if err != nil {
			fmt.Printf("! Video download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
			return false
		}

		audioBaseUrl, audioRepresentationId := getBaseUrl(audioSet, false, *audioQuality)
		if audioBaseUrl == nil {
			fmt.Printf("! Failed to get the audio base URL for S%02vE%02v, check -audio-quality\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
			_ = os.Remove(videoFile)
			return false
		}
		audioFile, err = downloadParts(audioBaseUrl, audioRepresentationId, audioSet)
		if err != nil {
			fmt.Printf("! Audio download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
			_ = os.Remove(videoFile)
			return false
		}
	}

	if err := mergeEverything(videoFile, audioFile, subsFile, outputFile, subtitlesLang, info); err != nil {
		fmt.Printf("! Mux error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
		_ = os.Remove(outputFile)
		_ = os.Remove(videoFile)
		_ = os.Remove(audioFile)
		_ = os.Remove(subsFile)
		return false
	}

	return episodeOutputComplete(outputFile)
}

func downloadSeason(videoQuality, audioQuality, subtitlesLang *string, episodes []SeasonEpisode) {
	if len(episodes) == 0 {
		return
	}

	fmt.Printf("Downloading season %v of %s (%v episodes)\n\n", episodes[0].SeasonNumber, episodes[0].SeriesTitle, len(episodes))

	var jobs []seasonEpisodeJob
	for _, episode := range episodes {
		job, ok := resolveSeasonEpisodeJob(episode, *audioLang)
		if !ok {
			continue
		}
		jobs = append(jobs, job)
	}

	if len(jobs) == 0 {
		fmt.Println("No episodes available to download for this season.")
		return
	}

	var toDownload []seasonEpisodeJob
	present := 0
	for _, job := range jobs {
		existing, ok := findCompleteEpisodeFile(job.info, *videoQuality)
		if ok {
			present++
			fmt.Printf("Episode %v already present (%s), skipping...\n", job.info.EpisodeMetadata.EpisodeNumber, filepath.Base(existing))
			continue
		}
		toDownload = append(toDownload, job)
	}

	if len(toDownload) == 0 {
		fmt.Printf("Season %v already complete (%v files found). Nothing to download.\n", episodes[0].SeasonNumber, present)
		return
	}

	if present > 0 {
		fmt.Printf("%v of %v episodes already present. Downloading %v remaining...\n\n", present, len(jobs), len(toDownload))
	}

	runJobs := func(toRun []seasonEpisodeJob) {
		for _, job := range toRun {
			downloadEpisode(job.id, videoQuality, audioQuality, subtitlesLang, job.info)
		}
	}

	runJobs(toDownload)

	for pass := 1; pass <= seasonRetryPasses; pass++ {
		missing := incompleteSeasonJobs(jobs, *videoQuality)
		if len(missing) == 0 {
			fmt.Printf("Season %v complete (%v episodes).\n", episodes[0].SeasonNumber, len(jobs))
			return
		}

		wait := time.Duration(pass) * 15 * time.Second
		fmt.Printf("\n%v episode(s) missing or incomplete: %s\n", len(missing), formatEpisodeNumbers(missing))
		fmt.Printf("Waiting %v then retrying (pass %d/%d)...\n\n", wait, pass, seasonRetryPasses)
		time.Sleep(wait)
		runJobs(missing)
	}

	stillMissing := incompleteSeasonJobs(jobs, *videoQuality)
	if len(stillMissing) == 0 {
		fmt.Printf("Season %v complete after retries (%v episodes).\n", episodes[0].SeasonNumber, len(jobs))
		return
	}

	fmt.Printf("! Season %v still missing %v episode(s) after retries: %s\n",
		episodes[0].SeasonNumber, len(stillMissing), formatEpisodeNumbers(stillMissing))
}
