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

func (s *downloadSession) fetchContent(etp, rawURL, audioLang string) {
	s.setBusy(true)
	defer s.setBusy(false)

	if etp == "" {
		s.setStatus("etp_rt cookie is required. Use the info icon next to the cookie field for help.")
		return
	}

	parsed, err := parseContentURL(rawURL)
	if err != nil {
		s.setStatus(err.Error())
		return
	}

	if loginErr := loginWithCookie(etp); loginErr != nil {
		s.setStatus(loginErr.Error())
		return
	}

	s.applyOutputDir()
	s.app.persistSettings()
	s.parsed = parsed

	var result lookupResult
	var lookupErr error
	if parsed.Kind == kindWatch {
		result, lookupErr = s.lookupWatch(parsed.ID, audioLang)
	} else {
		result, lookupErr = s.lookupSeries(parsed.ID, audioLang)
	}
	if lookupErr != nil {
		s.setStatus(lookupErr.Error())
		return
	}
	result.kind = parsed.Kind
	result.lookedUpURL = rawURL

	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.applyLookup(result)
	})
}

func (s *downloadSession) lookupWatch(id, audioLang string) (lookupResult, error) {
	info, err := fetchEpisodeInfo(id, audioLang)
	if err != nil {
		return lookupResult{}, err
	}

	audio := audioLocalesFromEpisode(info)
	requested := pickPreferred(audio, audioLang)
	contentID, _, ok := resolveWatchEpisodeID(id, info, requested, s.dubFallbackChk.Checked)
	if !ok {
		contentID = id
	}

	stream, probeErr := probeStreamOptions(contentID)
	if probeErr != nil {
		jobPrintf(s.jobEnv(), "Could not inspect stream options: %s\n", probeErr)
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

func (s *downloadSession) lookupSeries(id, audioLang string) (lookupResult, error) {
	seasons, err := fetchSeasons(id, audioLang)
	if err != nil {
		return lookupResult{}, err
	}
	if len(seasons) == 0 {
		return lookupResult{}, fmt.Errorf("no seasons found for this series")
	}

	audio := audioLocalesFromSeasons(seasons)
	requested := pickPreferred(audio, audioLang)

	numbers := uniqueSeasonNumbers(seasons)
	seasonOptions := []string{seasonAllLabel}
	for _, n := range numbers {
		seasonOptions = append(seasonOptions, fmt.Sprintf("Season %d", n))
	}
	initialSeason := seasonAllLabel
	if len(numbers) > 0 {
		initialSeason = fmt.Sprintf("Season %d", numbers[0])
	}

	episodes, title := s.episodesForSeason(seasons, numbers, requested)
	if extra := audioLocalesFromEpisodes(episodes); len(extra) > 0 {
		audio = uniqueSorted(append(audio, extra...))
		requested = pickPreferred(audio, requested)
	}

	probeID := probeIDForEpisodes(episodes, requested, s.dubFallbackChk.Checked)
	var stream streamOptions
	if probeID != "" {
		probed, probeErr := probeStreamOptions(probeID)
		if probeErr != nil {
			jobPrintf(s.jobEnv(), "Could not inspect stream options: %s\n", probeErr)
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

func (s *downloadSession) episodesForSeason(seasons []Season, numbers []int, requestedAudio string) ([]SeasonEpisode, string) {
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

func (s *downloadSession) applyLookup(result lookupResult) {
	s.app.ignoreSelect = true
	defer func() { s.app.ignoreSelect = false }()

	s.lookedUpURL = result.lookedUpURL
	s.seasons = result.seasons
	s.parsed.Kind = result.kind
	desired := result.title
	if desired == "" && len(result.episodes) > 0 {
		desired = result.episodes[0].SeriesTitle
	}
	if desired != "" {
		s.tabTitle = uniqueTitle(truncateTitle(desired, tabTitleMaxRunes), s.app.otherTitles(s.tab))
	}

	audioCodes := result.audioCodes
	if len(audioCodes) == 0 {
		audioCodes = sortedLanguageCodes()
	}
	setSelectOptions(s.audioSel, languageLabels(audioCodes), languageLabel(s.app.defaults.audioLang))
	s.app.defaults.audioLang = languageCodeFromLabel(s.audioSel.Selected)

	subs := []string{"None"}
	if len(result.stream.Subtitles) > 0 {
		subs = append(subs, languageLabels(result.stream.Subtitles)...)
	} else {
		subs = append(subs, languageLabels(sortedLanguageCodes())...)
	}
	preferredSubs := "None"
	if s.app.defaults.subsLang != s.app.defaults.audioLang {
		preferredSubs = languageLabel(s.app.defaults.subsLang)
	}
	setSelectOptions(s.subsSel, subs, preferredSubs)
	if s.subsSel.Selected == "None" {
		s.app.defaults.subsLang = s.app.defaults.audioLang
	} else {
		s.app.defaults.subsLang = languageCodeFromLabel(s.subsSel.Selected)
	}

	video := result.stream.VideoQuality
	if len(video) == 0 {
		video = defaultVideoQualities
	}
	setSelectOptions(s.videoSel, video, s.app.defaults.videoQuality)
	s.app.defaults.videoQuality = s.videoSel.Selected

	audioQ := result.stream.AudioQuality
	if len(audioQ) == 0 {
		audioQ = defaultAudioQualities
	}
	setSelectOptions(s.audioQSel, audioQ, s.app.defaults.audioQuality)
	s.app.defaults.audioQuality = s.audioQSel.Selected

	if len(result.seasonOptions) > 0 {
		s.seasonSel.Options = result.seasonOptions
		s.seasonSel.SetSelected(result.initialSeason)
	}
	if result.kind == kindWatch {
		s.seasonSel.Disable()
		s.seasonRow.Hide()
	} else {
		s.seasonSel.Enable()
		s.seasonRow.Show()
	}

	s.optionsBox.Show()
	s.app.ensureWindowFits()
	s.renderEpisodes(result.title, result.episodes)
	s.app.refreshTabChrome(s)
	s.app.persistSettings()
	s.app.relayout()
}

func (s *downloadSession) reloadEpisodes() {
	if len(s.seasons) == 0 {
		return
	}
	s.setBusy(true)
	defer s.setBusy(false)

	requested := languageCodeFromLabel(s.audioSel.Selected)
	seasonN, oneSeason := s.selectedSeasonNumber()

	var episodes []SeasonEpisode
	var title string

	if oneSeason {
		list, ok := resolveSeasonEpisodes(s.seasons, seasonN, requested)
		if !ok {
			s.setStatus(fmt.Sprintf("Season %d has no %s audio available.", seasonN, requested))
			fyne.Do(func() {
				if !s.stillOpen() {
					return
				}
				s.renderEpisodes("", nil)
			})
			return
		}
		episodes = list
		if len(list) > 0 {
			title = fmt.Sprintf("%s · Season %d", list[0].SeriesTitle, seasonN)
		}
	} else {
		for _, season := range seasonsForAudio(s.seasons, requested) {
			list, ok := resolveSeasonEpisodes(s.seasons, season.SeasonNumber, requested)
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
		if !s.stillOpen() {
			return
		}
		if title != "" {
			s.tabTitle = uniqueTitle(truncateTitle(title, tabTitleMaxRunes), s.app.otherTitles(s.tab))
		}
		s.renderEpisodes(title, episodes)
		s.app.refreshTabChrome(s)
	})
}

func (s *downloadSession) downloadSelected(etp string, selected []SeasonEpisode) {
	if len(selected) == 0 {
		s.setStatus("Select at least one episode to download.")
		return
	}

	if etp == "" {
		s.setStatus("etp_rt cookie is required.")
		return
	}
	if loginErr := loginWithCookie(etp); loginErr != nil {
		s.setStatus(loginErr.Error())
		return
	}

	s.applyOutputDir()
	s.app.persistSettings()

	s.setBusy(true)
	defer s.setBusy(false)
	s.setStatus(fmt.Sprintf("Downloading %d episode(s)…", len(selected)))

	s.run.begin()
	defer s.run.end()

	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.state = tabRunning
		s.app.refreshTabChrome(s)
		s.stopBtn.Enable()
	})

	env := s.jobEnv()
	s.beginDownload(len(selected))
	defer s.endDownload()

	okCount := 0
	for i, episode := range selected {
		if env.cancelled() {
			break
		}

		s.startEpisodeProgress()
		s.setStatus(fmt.Sprintf("Downloading %s (%d of %d)…", episodeKey(episode), i+1, len(selected)))

		if downloadOneEpisode(env, episode) {
			okCount++
		}

		s.completeEpisodeProgress()
	}

	stopped := env.cancelled()
	msg := fmt.Sprintf("Finished: %d/%d episode(s) downloaded.", okCount, len(selected))
	if stopped {
		msg = fmt.Sprintf("Stopped: %d/%d episode(s) downloaded.", okCount, len(selected))
	}
	jobPrintln(env, msg)
	title := ""
	if len(s.episodes) > 0 {
		title = s.episodes[0].SeriesTitle
	}
	dialogTitle := "Download complete"
	if stopped {
		dialogTitle = "Download stopped"
	}
	s.setStatus(msg)
	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.stopBtn.Disable()
		if stopped {
			s.state = tabStopped
		} else {
			s.state = tabComplete
		}
		s.app.refreshTabChrome(s)
		s.renderEpisodes(title, s.episodes)
		if s.app.tabs != nil && s.app.tabs.Selected() == s.tab {
			dialog.ShowInformation(dialogTitle, msg, s.app.win)
		}
	})
}

func downloadOneEpisode(env jobEnv, episode SeasonEpisode) (ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			jobPrintf(env, "! Unexpected error on %s: %v\n", episodeKey(episode), recovered)
			ok = false
		}
	}()

	job, resolved := resolveSeasonEpisodeJob(env, episode, env.audioLang, env.dubFallback)
	if !resolved {
		return false
	}
	return downloadEpisode(env, job.id, job.info)
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
	*etpRt = etp
	return nil
}

func fetchEpisodeInfo(id, audioLang string) (info EpisodeInfo, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("failed to load episode: %v", recovered)
		}
	}()
	info = getEpisodeInfo(id, audioLang)
	return info, nil
}

func fetchSeasons(id, audioLang string) (seasons []Season, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("failed to load seasons: %v", recovered)
		}
	}()
	seasons = getSeasons(id, audioLang)
	return seasons, nil
}
