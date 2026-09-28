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

func episodeOutputName(info EpisodeInfo, videoQuality string) string {
	series := sanitizeFilename(info.EpisodeMetadata.SeriesTitle)
	name := fmt.Sprintf("%s S%02vE%02v", series, info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
	title := sanitizeFilename(info.Title)
	if title != "" && title != series {
		name += " " + title
	}
	return name + " [" + videoQuality + "].mkv"
}

func episodeOutputFile(info EpisodeInfo, videoQuality, dir string) string {
	return filepath.Join(seriesDirName(info, dir), episodeOutputName(info, videoQuality))
}

func episodeOutputComplete(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Size() >= minCompleteEpisodeBytes
}

func seriesDirName(info EpisodeInfo, dir string) string {
	if dir == "" {
		dir = outputDir
	}
	return filepath.Join(dir, sanitizeFilename(info.EpisodeMetadata.SeriesTitle))
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
func findCompleteEpisodeFile(info EpisodeInfo, videoQuality, dir string) (string, bool) {
	expected := episodeOutputFile(info, videoQuality, dir)
	if episodeOutputComplete(expected) {
		return expected, true
	}

	dir = seriesDirName(info, dir)
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

func printUnavailableDubs(env jobEnv, episode SeasonEpisode, audioLang string) {
	jobPrintf(env, "! Episode %v has no %s dub available, skipping...\n", episode.EpisodeNumber, audioLang)
	if len(episode.Versions) > 0 {
		jobPrint(env, "  Available dubs: ")
		for i, v := range episode.Versions {
			if v == nil {
				continue
			}
			if i > 0 {
				jobPrint(env, ", ")
			}
			jobPrint(env, audioName(v.AudioLocale))
		}
		jobPrintln(env)
		return
	}
	if episode.AudioLocale != "" {
		jobPrintf(env, "  Available audio: %s\n", audioName(episode.AudioLocale))
	}
}

// episodeAudioLocale reports the audio language actually being downloaded,
// which differs from the requested one once a dub fallback kicks in.
func episodeAudioLocale(info EpisodeInfo, audioLang string) string {
	if info.EpisodeMetadata.AudioLocale != "" {
		return info.EpisodeMetadata.AudioLocale
	}
	return audioLang
}

// fallbackDub picks the audio to download when the requested dub is missing:
// the episode's own audio when it has one, otherwise its first listed version.
func fallbackDub(episode SeasonEpisode) (id, locale string, ok bool) {
	if episode.AudioLocale != "" {
		return episode.ID, episode.AudioLocale, true
	}
	for _, version := range episode.Versions {
		if version != nil && version.AudioLocale != "" {
			return version.GUID, version.AudioLocale, true
		}
	}
	return "", "", false
}

func resolveSeasonEpisodeJob(env jobEnv, episode SeasonEpisode, audioLang string, allowFallback bool) (seasonEpisodeJob, bool) {
	episodeId := episode.ID
	audioLocale := audioLang
	if episode.AudioLocale == audioLang {
		// Already the requested dub.
	} else if correctGuidI := slices.IndexFunc(episode.Versions, func(v *DubVersion) bool {
		return v != nil && v.AudioLocale == audioLang
	}); correctGuidI != -1 {
		episodeId = episode.Versions[correctGuidI].GUID
	} else if episode.AudioLocale == "" && len(episode.Versions) == 0 {
		// Language-specific season whose episode metadata omits audio_locale.
	} else if fallbackId, fallbackLocale, hasFallback := fallbackDub(episode); allowFallback && hasFallback {
		episodeId = fallbackId
		audioLocale = fallbackLocale
		jobPrintf(env, "! Episode %v has no %s dub, downloading %s instead...\n", episode.EpisodeNumber, audioLang, languageLabel(fallbackLocale))
	} else {
		printUnavailableDubs(env, episode, audioLang)
		return seasonEpisodeJob{}, false
	}

	return seasonEpisodeJob{
		id: episodeId,
		info: EpisodeInfo{
			EpisodeMetadata: EpisodeMetadata{
				SeriesTitle:        episode.SeriesTitle,
				SeasonNumber:       episode.SeasonNumber,
				EpisodeNumber:      episode.EpisodeNumber,
				AudioLocale:        audioLocale,
				Versions:           episode.Versions,
				AvailabilityStarts: episode.AvailabilityStarts,
			},
			Title: episode.Title,
		},
	}, true
}

func incompleteSeasonJobs(jobs []seasonEpisodeJob, videoQuality, dir string) []seasonEpisodeJob {
	var missing []seasonEpisodeJob
	for _, job := range jobs {
		if _, ok := findCompleteEpisodeFile(job.info, videoQuality, dir); !ok {
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

func downloadPart(env jobEnv, url string) ([]byte, error) {
	ctx := env.context()
	maxRetries := 5

	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, errCancelled
		}
		if attempt > 0 && !sleepOrCancelCtx(ctx, time.Duration(attempt)*2*time.Second) {
			return nil, errCancelled
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Origin", "https://static.crunchyroll.com")
		req.Header.Set("Referer", "https://static.crunchyroll.com/")
		req.Header.Set("User-Agent", userAgent)
		resp, err := mediaClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, errCancelled
			}
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
			if ctx.Err() != nil {
				return nil, errCancelled
			}
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

func downloadParts(env jobEnv, baseUrl, representationId *string, set *mpd.AdaptationSet, licenseKeys []*widevine.Key) (string, error) {
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
	initData, err := downloadPart(env, initUrl)
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
				if env.cancelled() {
					errOnce.Do(func() { downloadErr = errCancelled })
					return
				}
				data, err := downloadPart(env, job.url)
				if err != nil {
					errOnce.Do(func() { downloadErr = err })
					return
				}
				results[job.index] = data
				count := done.Add(1)
				jobPrintf(env, "\rDownloaded %v of %v segments (%v%%)", count, total, (100*count)/int64(total))
			}
		}()
	}

	// The jobs channel is buffered to hold every segment, so this never blocks
	// even when the workers stop early.
	for i, item := range timeline {
		url := buildUrl(*baseUrl, *representationId, *template.Media, &item)
		jobs <- segmentJob{index: i, url: url}
	}
	close(jobs)
	wg.Wait()

	if downloadErr != nil {
		return "", downloadErr
	}
	if env.cancelled() {
		return "", errCancelled
	}

	jobPrintln(env, "\nFinished downloading!")

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
	if err = decryptMP4(initData, bytes.NewReader(parts), licenseKeys, file); err != nil {
		return "", fmt.Errorf("decryptMP4: %w", err)
	}

	return filename, nil
}

func downloadSubs(env jobEnv, url string) (string, error) {
	req, err := http.NewRequestWithContext(env.context(), http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating subtitle request: %w", err)
	}
	req.Header.Set("Origin", "https://static.crunchyroll.com")
	req.Header.Set("Referer", "https://static.crunchyroll.com/")
	req.Header.Set("User-Agent", userAgent)
	resp, err := mediaClient.Do(req)
	if err != nil {
		if env.cancelled() {
			return "", errCancelled
		}
		return "", fmt.Errorf("subtitle request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if env.cancelled() {
			return "", errCancelled
		}
		return "", fmt.Errorf("reading subtitles: %w", err)
	}

	filename := getFilename(nil)
	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("creating subtitle file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(body); err != nil {
		return "", fmt.Errorf("writing subtitles: %w", err)
	}

	return filename, nil
}

func downloadEpisode(env jobEnv, contentId string, info EpisodeInfo) bool {
	if env.cancelled() {
		return false
	}

	outputFile := episodeOutputFile(info, env.videoQuality, env.dir())
	if existing, ok := findCompleteEpisodeFile(info, env.videoQuality, env.dir()); ok {
		jobPrintf(env, "Episode %v is already downloaded (%s), skipping...\n", info.EpisodeMetadata.EpisodeNumber, filepath.Base(existing))
		return true
	}
	if err := os.MkdirAll(filepath.Dir(outputFile), 0777); err != nil {
		jobPrintf(env, "! Failed to create output directory: %s\n", err)
		return false
	}
	if _, err := os.Stat(outputFile); err == nil {
		jobPrintf(env, "Episode %v is incomplete (%s), re-downloading...\n", info.EpisodeMetadata.EpisodeNumber, outputFile)
		_ = os.Remove(outputFile)
	}

	playbackSlot.Lock()
	slotHeld := true
	unlockPlayback := func() {
		if !slotHeld {
			return
		}
		slotHeld = false
		playbackSlot.Unlock()
	}
	defer unlockPlayback()

	var episode Episode
	var err error

	maxRetries := 5
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if !slotHeld {
			playbackSlot.Lock()
			slotHeld = true
		}
		episode, err = getEpisode(contentId)
		if err == nil {
			break
		}

		if isRateLimitedPlaybackError(err) {
			jobPrintf(env, "! Crunchyroll rate-limited playback requests (%s). Wait 10–20 minutes, then try again.\n", err)
			return false
		}

		if isRetryablePlaybackError(err) && attempt < maxRetries {
			unlockPlayback()
			wait := time.Duration(attempt) * 10 * time.Second
			jobPrintf(env, "Too many active streams, waiting %v before retry (%d/%d)...\n", wait, attempt, maxRetries)
			if !sleepOrCancelCtx(env.context(), wait) {
				jobPrintln(env, "Stopped.")
				return false
			}
			continue
		}

		jobPrintf(env, "! Error fetching episode S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
		return false
	}

	releaseStream := func() {
		if episode.Token == "" {
			return
		}
		if success := deleteStream(contentId, episode.Token); !success {
			jobPrintln(env, "Warning: failed to release the playback stream.")
		}
		episode.Token = ""
	}
	defer releaseStream()

	jobPrintf(env, "Downloading: %s (S%02vE%02v) from %s\n", info.Title, info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, info.EpisodeMetadata.SeriesTitle)

	manifest, rawManifest, manifestErr := parseManifest(env, episode.ManifestURL)
	if manifestErr != nil {
		jobPrintf(env, "! Manifest error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, manifestErr)
		return false
	}

	videoSet, audioSet := findAdaptationSets(manifest)
	if videoSet == nil || audioSet == nil {
		jobPrintf(env, "! Could not find video/audio adaptation sets for S%02vE%02v\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
		return false
	}

	pssh := getPssh(manifest, rawManifest)
	if pssh == nil && !isOnDemand(manifest) {
		baseUrl, representationId := getBaseUrl(videoSet, true, env.videoQuality)
		if baseUrl != nil && representationId != nil {
			template := resolveSegmentTemplate(videoSet, *representationId)
			if template != nil && template.Initialization != nil {
				initUrl := buildUrl(*baseUrl, *representationId, *template.Initialization, nil)
				if initData, initErr := downloadPart(env, initUrl); initErr == nil {
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
				rep, ok := selectOnDemandRepresentation(env, set, env.videoQuality, true)
				if !ok || rep.InitRange == "" {
					continue
				}
				initStart, initEnd, rangeErr := parseByteRange(rep.InitRange)
				if rangeErr != nil {
					continue
				}
				if initData, initErr := downloadRange(env, rep.BaseURL, initStart, initEnd); initErr == nil {
					pssh = extractWidevinePsshFromInit(initData)
					if pssh != nil {
						break
					}
				}
			}
		}
	}
	if pssh == nil {
		jobPrintf(env, "! PSSH not found for S%02vE%02v, skipping...\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
		return false
	}

	licenseKeys, err := getLicense(*pssh, contentId, episode.Token)
	if err != nil {
		jobPrintf(env, "! License error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
		return false
	}

	// Free the account's stream slot before the long segment download so another
	// tab can start. The token is only required for the Widevine license.
	releaseStream()
	unlockPlayback()

	var subsFile string
	// Compare against the audio actually being downloaded: a dub fallback lands
	// on a different language, and then the subtitles are worth keeping.
	if episodeAudioLocale(info, env.audioLang) == env.subtitlesLang {
		jobPrintln(env, "Skipping subtitles (audio language matches subtitle language)")
	} else if subtitles := episode.Subtitles[env.subtitlesLang]; subtitles != nil {
		jobPrintf(env, "Downloading subtitles for %s language...\n", languageNames[env.subtitlesLang])
		subsFile, err = downloadSubs(env, subtitles.URL)
		if err != nil {
			jobPrintf(env, "! Subtitle download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
			return false
		}
		jobPrintln(env, "Downloaded subtitles!")
	}

	var videoFile string
	var audioFile string

	if isOnDemand(manifest) {
		sets, parseErr := parseOnDemand(rawManifest)
		if parseErr != nil {
			jobPrintf(env, "! Failed to parse on-demand manifest for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, parseErr)
			return false
		}
		videoFile, err = downloadOnDemandAdaptation(env, sets, true, env.videoQuality, licenseKeys)
		if err != nil {
			jobPrintf(env, "! Video download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
			return false
		}
		audioFile, err = downloadOnDemandAdaptation(env, sets, false, env.audioQuality, licenseKeys)
		if err != nil {
			jobPrintf(env, "! Audio download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
			_ = os.Remove(videoFile)
			return false
		}
	} else {
		baseUrl, representationId := getBaseUrl(videoSet, true, env.videoQuality)
		if baseUrl == nil {
			jobPrintf(env, "! Failed to get the video base URL for S%02vE%02v, check -video-quality\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
			return false
		}
		videoFile, err = downloadParts(env, baseUrl, representationId, videoSet, licenseKeys)
		if err != nil {
			jobPrintf(env, "! Video download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
			return false
		}

		audioBaseUrl, audioRepresentationId := getBaseUrl(audioSet, false, env.audioQuality)
		if audioBaseUrl == nil {
			jobPrintf(env, "! Failed to get the audio base URL for S%02vE%02v, check -audio-quality\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
			_ = os.Remove(videoFile)
			return false
		}
		audioFile, err = downloadParts(env, audioBaseUrl, audioRepresentationId, audioSet, licenseKeys)
		if err != nil {
			jobPrintf(env, "! Audio download error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
			_ = os.Remove(videoFile)
			return false
		}
	}

	if env.cancelled() {
		jobPrintf(env, "Stopped before muxing S%02vE%02v.\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)
		_ = os.Remove(videoFile)
		_ = os.Remove(audioFile)
		_ = os.Remove(subsFile)
		return false
	}

	if err := mergeEverything(env, videoFile, audioFile, subsFile, outputFile, info); err != nil {
		jobPrintf(env, "! Mux error for S%02vE%02v: %s\n", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, err)
		_ = os.Remove(outputFile)
		_ = os.Remove(videoFile)
		_ = os.Remove(audioFile)
		_ = os.Remove(subsFile)
		return false
	}

	return episodeOutputComplete(outputFile)
}

func downloadSeason(env jobEnv, episodes []SeasonEpisode) {
	if len(episodes) == 0 {
		return
	}

	jobPrintf(env, "Downloading season %v of %s (%v episodes)\n\n", episodes[0].SeasonNumber, episodes[0].SeriesTitle, len(episodes))

	var jobs []seasonEpisodeJob
	for _, episode := range episodes {
		job, ok := resolveSeasonEpisodeJob(env, episode, env.audioLang, env.dubFallback)
		if !ok {
			continue
		}
		jobs = append(jobs, job)
	}

	if len(jobs) == 0 {
		jobPrintln(env, "No episodes available to download for this season.")
		return
	}

	var toDownload []seasonEpisodeJob
	present := 0
	for _, job := range jobs {
		existing, ok := findCompleteEpisodeFile(job.info, env.videoQuality, env.dir())
		if ok {
			present++
			jobPrintf(env, "Episode %v already present (%s), skipping...\n", job.info.EpisodeMetadata.EpisodeNumber, filepath.Base(existing))
			continue
		}
		toDownload = append(toDownload, job)
	}

	if len(toDownload) == 0 {
		jobPrintf(env, "Season %v already complete (%v files found). Nothing to download.\n", episodes[0].SeasonNumber, present)
		return
	}

	if present > 0 {
		jobPrintf(env, "%v of %v episodes already present. Downloading %v remaining...\n\n", present, len(jobs), len(toDownload))
	}

	runJobs := func(toRun []seasonEpisodeJob) {
		for _, job := range toRun {
			if env.cancelled() {
				return
			}
			downloadEpisode(env, job.id, job.info)
		}
	}

	runJobs(toDownload)

	for pass := 1; pass <= seasonRetryPasses; pass++ {
		if env.cancelled() {
			jobPrintln(env, "Stopped.")
			return
		}

		missing := incompleteSeasonJobs(jobs, env.videoQuality, env.dir())
		if len(missing) == 0 {
			jobPrintf(env, "Season %v complete (%v episodes).\n", episodes[0].SeasonNumber, len(jobs))
			return
		}

		wait := time.Duration(pass) * 15 * time.Second
		jobPrintf(env, "\n%v episode(s) missing or incomplete: %s\n", len(missing), formatEpisodeNumbers(missing))
		jobPrintf(env, "Waiting %v then retrying (pass %d/%d)...\n\n", wait, pass, seasonRetryPasses)
		if !sleepOrCancelCtx(env.context(), wait) {
			jobPrintln(env, "Stopped.")
			return
		}
		runJobs(missing)
	}

	if env.cancelled() {
		jobPrintln(env, "Stopped.")
		return
	}

	stillMissing := incompleteSeasonJobs(jobs, env.videoQuality, env.dir())
	if len(stillMissing) == 0 {
		jobPrintf(env, "Season %v complete after retries (%v episodes).\n", episodes[0].SeasonNumber, len(jobs))
		return
	}

	jobPrintf(env, "! Season %v still missing %v episode(s) after retries: %s\n",
		episodes[0].SeasonNumber, len(stillMissing), formatEpisodeNumbers(stillMissing))
}
