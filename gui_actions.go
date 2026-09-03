//go:build gui

package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

type lookupResult struct {
	kind          contentKind
	title         string
	seasons       []Season
	seasonOptions []string
	initialSeason string
	audioCodes    []string
	stream        streamOptions
	episodes      []SeasonEpisode
	lookedUpURL   string
}

func (g *guiApp) fetchContent(etp, rawURL string) {
	g.setBusy(true)
	defer g.setBusy(false)

	if etp == "" {
		g.setStatus("etp_rt cookie is required. Use the info icon next to the cookie field for help.")
		return
	}

	parsed, err := parseContentURL(rawURL)
	if err != nil {
		g.setStatus(err.Error())
		return
	}

	if loginErr := loginWithCookie(etp); loginErr != nil {
		g.setStatus(loginErr.Error())
		return
	}

	g.applyOutputDir()
	g.persistSettings()
	g.parsed = parsed

	var result lookupResult
	var lookupErr error
	if parsed.Kind == kindWatch {
		result, lookupErr = g.lookupWatch(parsed.ID)
	} else {
		result, lookupErr = g.lookupSeries(parsed.ID)
	}
	if lookupErr != nil {
		g.setStatus(lookupErr.Error())
		return
	}
	result.kind = parsed.Kind
	result.lookedUpURL = rawURL

	fyne.Do(func() {
		g.applyLookup(result)
	})
}

func (g *guiApp) lookupWatch(id string) (lookupResult, error) {
	info, err := fetchEpisodeInfo(id)
	if err != nil {
		return lookupResult{}, err
	}

	audio := audioLocalesFromEpisode(info)
	*audioLang = pickPreferred(audio, *audioLang)
	contentID, ok := resolveWatchEpisodeID(id, info, *audioLang)
	if !ok {
		contentID = id
	}

	stream, probeErr := probeStreamOptions(contentID)
	if probeErr != nil {
		fmt.Printf("Could not inspect stream options: %s\n", probeErr)
	}

	return lookupResult{
		title:      info.EpisodeMetadata.SeriesTitle,
		audioCodes: audio,
		stream:     stream,
		episodes: []SeasonEpisode{
			episodeInfoToSeasonEpisode(id, info),
		},
		seasonOptions: []string{fmt.Sprintf("Episode S%02dE%02d", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber)},
		initialSeason: fmt.Sprintf("Episode S%02dE%02d", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber),
	}, nil
}

func (g *guiApp) lookupSeries(id string) (lookupResult, error) {
	seasons, err := fetchSeasons(id)
	if err != nil {
		return lookupResult{}, err
	}
	if len(seasons) == 0 {
		return lookupResult{}, fmt.Errorf("no seasons found for this series")
	}

	audio := audioLocalesFromSeasons(seasons)
	*audioLang = pickPreferred(audio, *audioLang)

	numbers := uniqueSeasonNumbers(seasons)
	seasonOptions := []string{seasonAllLabel}
	for _, n := range numbers {
		seasonOptions = append(seasonOptions, fmt.Sprintf("Season %d", n))
	}
	initialSeason := seasonAllLabel
	if len(numbers) > 0 {
		initialSeason = fmt.Sprintf("Season %d", numbers[0])
	}

	episodes, title := g.episodesForSeason(seasons, numbers, *audioLang)
	if extra := audioLocalesFromEpisodes(episodes); len(extra) > 0 {
		audio = uniqueSorted(append(audio, extra...))
		*audioLang = pickPreferred(audio, *audioLang)
	}

	probeID := probeIDForEpisodes(episodes, *audioLang)
	var stream streamOptions
	if probeID != "" {
		probed, probeErr := probeStreamOptions(probeID)
		if probeErr != nil {
			fmt.Printf("Could not inspect stream options: %s\n", probeErr)
		} else {
			stream = probed
		}
	}

	return lookupResult{
		title:         title,
		seasons:       seasons,
		seasonOptions: seasonOptions,
		initialSeason: initialSeason,
		audioCodes:    audio,
		stream:        stream,
		episodes:      episodes,
	}, nil
}

func (g *guiApp) episodesForSeason(seasons []Season, numbers []int, requestedAudio string) ([]SeasonEpisode, string) {
	if len(numbers) == 0 {
		return nil, ""
	}
	list, ok := resolveSeasonEpisodes(seasons, numbers[0], requestedAudio)
	if !ok {
		return nil, ""
	}
	title := ""
	if len(list) > 0 {
		title = fmt.Sprintf("%s · Season %d", list[0].SeriesTitle, numbers[0])
	}
	return list, title
}

func (g *guiApp) applyLookup(result lookupResult) {
	g.ignoreSelect = true
	defer func() { g.ignoreSelect = false }()

	g.lookedUpURL = result.lookedUpURL
	g.seasons = result.seasons
	g.parsed.Kind = result.kind

	audioCodes := result.audioCodes
	if len(audioCodes) == 0 {
		audioCodes = sortedLanguageCodes()
	}
	g.setSelectOptions(g.audioSel, languageLabels(audioCodes), languageLabel(*audioLang))
	*audioLang = languageCodeFromLabel(g.audioSel.Selected)

	subs := []string{"None"}
	if len(result.stream.Subtitles) > 0 {
		subs = append(subs, languageLabels(result.stream.Subtitles)...)
	} else {
		subs = append(subs, languageLabels(sortedLanguageCodes())...)
	}
	preferredSubs := "None"
	if *subtitlesLang != *audioLang {
		preferredSubs = languageLabel(*subtitlesLang)
	}
	g.setSelectOptions(g.subsSel, subs, preferredSubs)
	if g.subsSel.Selected == "None" {
		*subtitlesLang = *audioLang
	} else {
		*subtitlesLang = languageCodeFromLabel(g.subsSel.Selected)
	}

	video := result.stream.VideoQuality
	if len(video) == 0 {
		video = defaultVideoQualities
	}
	g.setSelectOptions(g.videoSel, video, *videoQuality)
	*videoQuality = g.videoSel.Selected

	audioQ := result.stream.AudioQuality
	if len(audioQ) == 0 {
		audioQ = defaultAudioQualities
	}
	g.setSelectOptions(g.audioQSel, audioQ, *audioQuality)
	*audioQuality = g.audioQSel.Selected

	if len(result.seasonOptions) > 0 {
		g.seasonSel.Options = result.seasonOptions
		g.seasonSel.SetSelected(result.initialSeason)
	}
	if result.kind == kindWatch {
		g.seasonSel.Disable()
		g.seasonRow.Hide()
	} else {
		g.seasonSel.Enable()
		g.seasonRow.Show()
	}

	g.optionsBox.Show()
	g.optionsBox.Refresh()
	g.ensureWindowFits()
	g.renderEpisodes(result.title, result.episodes)
	g.persistSettings()
}

func (g *guiApp) reloadEpisodes() {
	if len(g.seasons) == 0 {
		return
	}
	g.setBusy(true)
	defer g.setBusy(false)

	*audioLang = languageCodeFromLabel(g.audioSel.Selected)
	seasonN, oneSeason := g.selectedSeasonNumber()

	var episodes []SeasonEpisode
	var title string

	if oneSeason {
		list, ok := resolveSeasonEpisodes(g.seasons, seasonN, *audioLang)
		if !ok {
			g.setStatus(fmt.Sprintf("Season %d has no %s audio available.", seasonN, *audioLang))
			fyne.Do(func() {
				g.renderEpisodes("", nil)
			})
			return
		}
		episodes = list
		if len(list) > 0 {
			title = fmt.Sprintf("%s · Season %d", list[0].SeriesTitle, seasonN)
		}
	} else {
		for _, season := range seasonsForAudio(g.seasons, *audioLang) {
			list, ok := resolveSeasonEpisodes(g.seasons, season.SeasonNumber, *audioLang)
			if !ok {
				continue
			}
			episodes = append(episodes, list...)
			if title == "" && len(list) > 0 {
				title = list[0].SeriesTitle
			}
		}
	}

	fyne.Do(func() {
		g.renderEpisodes(title, episodes)
	})
}

func (g *guiApp) downloadSelected(etp string, selected []SeasonEpisode) {
	if len(selected) == 0 {
		g.setStatus("Select at least one episode to download.")
		return
	}

	if etp == "" {
		g.setStatus("etp_rt cookie is required.")
		return
	}
	if loginErr := loginWithCookie(etp); loginErr != nil {
		g.setStatus(loginErr.Error())
		return
	}

	*audioLang = languageCodeFromLabel(g.audioSel.Selected)
	if g.subsSel.Selected == "None" {
		*subtitlesLang = *audioLang
	} else {
		*subtitlesLang = languageCodeFromLabel(g.subsSel.Selected)
	}
	*videoQuality = g.videoSel.Selected
	*audioQuality = g.audioQSel.Selected
	g.applyOutputDir()
	g.persistSettings()

	g.setBusy(true)
	defer g.setBusy(false)
	g.setStatus(fmt.Sprintf("Downloading %d episode(s)…", len(selected)))

	beginCancellable()
	defer endCancellable()

	g.beginDownload(len(selected))
	defer g.endDownload()

	fyne.Do(func() {
		g.stopBtn.Enable()
	})

	okCount := 0
	for i, episode := range selected {
		if isCancelled() {
			break
		}

		g.startEpisodeProgress()
		g.setStatus(fmt.Sprintf("Downloading %s (%d of %d)…", episodeKey(episode), i+1, len(selected)))

		if downloadOneEpisode(episode) {
			okCount++
		}

		g.completeEpisodeProgress()
	}

	stopped := isCancelled()
	msg := fmt.Sprintf("Finished: %d/%d episode(s) downloaded.", okCount, len(selected))
	if stopped {
		msg = fmt.Sprintf("Stopped: %d/%d episode(s) downloaded.", okCount, len(selected))
	}
	fmt.Println(msg)
	title := ""
	if len(g.episodes) > 0 {
		title = g.episodes[0].SeriesTitle
	}
	dialogTitle := "Download complete"
	if stopped {
		dialogTitle = "Download stopped"
	}
	g.setStatus(msg)
	fyne.Do(func() {
		g.stopBtn.Disable()
		g.renderEpisodes(title, g.episodes)
		dialog.ShowInformation(dialogTitle, msg, g.win)
	})
}

// downloadOneEpisode isolates a single episode download so an unexpected
// failure inside the engine cannot take down the whole app.
func downloadOneEpisode(episode SeasonEpisode) (ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fmt.Printf("! Unexpected error on %s: %v\n", episodeKey(episode), recovered)
			ok = false
		}
	}()

	job, resolved := resolveSeasonEpisodeJob(episode, *audioLang)
	if !resolved {
		return false
	}
	return downloadEpisode(job.id, videoQuality, audioQuality, subtitlesLang, job.info)
}

func loginWithCookie(etp string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("login failed: %v", recovered)
		}
	}()
	if err := RefreshAccessToken(etp); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	// Keep the validated cookie available to automatic token refreshes later
	// in a long-running season download.
	*etpRt = etp
	return nil
}

func fetchEpisodeInfo(id string) (info EpisodeInfo, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("failed to load episode: %v", recovered)
		}
	}()
	info = getEpisodeInfo(id)
	return info, nil
}

func fetchSeasons(id string) (seasons []Season, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("failed to load seasons: %v", recovered)
		}
	}()
	seasons = getSeasons(id)
	return seasons, nil
}
