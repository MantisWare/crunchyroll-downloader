//go:build gui

package main

import (
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type downloadSession struct {
	app   *guiApp
	tab   *container.TabItem
	seq   int
	run   cancellable
	state tabRunState

	urlEntry       *widget.Entry
	outEntry       *widget.Entry
	audioSel       *widget.Select
	subsSel        *widget.Select
	dubFallbackChk *widget.Check
	videoSel       *widget.Select
	audioQSel      *widget.Select
	seasonSel      *widget.Select
	optionsBox     *fyne.Container
	seasonRow      *fyne.Container
	status         *widget.Label
	logEntry       *widget.Entry
	logRaw         string
	episodeBox     *fyne.Container
	lookupBtn      *widget.Button
	dlBtn          *widget.Button
	resetBtn       *widget.Button
	stopBtn        *widget.Button
	progress       *widget.ProgressBar

	progressMu sync.Mutex
	dlActive   bool
	dlTotal    int
	dlDone     int
	dlPhase    int
	dlPhasePct float64

	parsed       parsedContent
	lookedUpURL  string
	seasons      []Season
	episodes     []SeasonEpisode
	checks       []*widget.Check
	checkedKeys  map[string]bool
	hasSelection bool
	currentTitle string
	tabTitle     string
	busyMu       sync.Mutex
	busy         bool
}

func (s *downloadSession) stillOpen() bool {
	if s.app == nil || s.tab == nil {
		return false
	}
	return s.app.sessionFor(s.tab) == s
}

func (s *downloadSession) kind() tabKind { return tabKindDownload }

func (s *downloadSession) tabItem() *container.TabItem { return s.tab }

func (s *downloadSession) isBusy() bool {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	return s.busy
}

func (s *downloadSession) cancel() { s.run.request() }

func (s *downloadSession) runState() tabRunState { return s.state }

func (s *downloadSession) title() string {
	if s.tabTitle != "" {
		return s.tabTitle
	}
	return numberedSessionTitle(tabKindDownload, s.seq)
}

func (g *guiApp) newDownloadSession() *downloadSession {
	g.downloadSeq++
	s := &downloadSession{
		app:         g,
		seq:         g.downloadSeq,
		checkedKeys: make(map[string]bool),
	}
	s.build()
	return s
}

func (s *downloadSession) build() {
	s.urlEntry = widget.NewEntry()
	s.urlEntry.SetPlaceHolder("https://www.crunchyroll.com/series/... or /watch/...")
	s.urlEntry.OnChanged = func(v string) {
		s.onURLChanged(v)
	}

	s.outEntry = widget.NewEntry()
	s.outEntry.SetText(s.app.defaultOutputDir())
	s.outEntry.OnChanged = func(v string) {
		if v != "" {
			s.app.defaults.outputDir = v
			outputDir = v
		}
		if !s.app.ignoreSelect {
			s.app.persistSettings()
		}
	}

	s.audioSel = widget.NewSelect(nil, func(label string) {
		s.app.defaults.audioLang = languageCodeFromLabel(label)
		if s.app.ignoreSelect {
			return
		}
		s.app.persistSettings()
		if s.parsed.Kind == kindSeries && len(s.seasons) > 0 && !s.isBusy() {
			go s.reloadEpisodes()
		}
	})
	s.audioSel.PlaceHolder = "Lookup a URL first"

	s.dubFallbackChk = widget.NewCheck("Use default if missing", func(checked bool) {
		s.app.defaults.dubFallback = checked
		if !s.app.ignoreSelect {
			s.app.persistSettings()
		}
	})
	s.dubFallbackChk.SetChecked(s.app.defaults.dubFallback)

	s.subsSel = widget.NewSelect([]string{"None"}, func(label string) {
		if label == "None" {
			s.app.defaults.subsLang = s.app.defaults.audioLang
		} else {
			s.app.defaults.subsLang = languageCodeFromLabel(label)
		}
		if !s.app.ignoreSelect {
			s.app.persistSettings()
		}
	})
	s.subsSel.PlaceHolder = "Lookup a URL first"
	s.subsSel.SetSelected("None")

	s.videoSel = widget.NewSelect(defaultVideoQualities, func(v string) {
		s.app.defaults.videoQuality = v
		if s.app.ignoreSelect {
			return
		}
		s.app.persistSettings()
		s.refreshEpisodeLabels()
	})
	s.videoSel.PlaceHolder = "Lookup a URL first"
	if s.app.defaults.videoQuality != "" {
		s.videoSel.SetSelected(pickPreferred(defaultVideoQualities, s.app.defaults.videoQuality))
	}

	s.audioQSel = widget.NewSelect(defaultAudioQualities, func(v string) {
		s.app.defaults.audioQuality = v
		if !s.app.ignoreSelect {
			s.app.persistSettings()
		}
	})
	s.audioQSel.PlaceHolder = "Lookup a URL first"
	if s.app.defaults.audioQuality != "" {
		s.audioQSel.SetSelected(pickPreferred(defaultAudioQualities, s.app.defaults.audioQuality))
	}

	s.seasonSel = widget.NewSelect([]string{seasonAllLabel}, func(_ string) {
		if s.app.ignoreSelect {
			return
		}
		if s.parsed.Kind == kindSeries && len(s.seasons) > 0 && !s.isBusy() {
			go s.reloadEpisodes()
		}
	})
	s.seasonSel.SetSelected(seasonAllLabel)
	s.seasonSel.Disable()

	s.status = widget.NewLabel("Paste a Crunchyroll URL, then click Lookup.")
	s.status.Wrapping = fyne.TextWrapWord

	s.logEntry = widget.NewMultiLineEntry()
	s.logEntry.Disable()
	s.logEntry.SetPlaceHolder("Download progress appears here…")
	s.logEntry.Wrapping = fyne.TextWrapWord

	s.episodeBox = container.NewVBox()
	s.lookupBtn = widget.NewButtonWithIcon("Lookup", theme.SearchIcon(), func() {
		etp := s.app.cookie()
		rawURL := strings.TrimSpace(s.urlEntry.Text)
		audioLang := s.app.defaults.audioLang
		if s.audioSel.Selected != "" {
			audioLang = languageCodeFromLabel(s.audioSel.Selected)
		}
		go s.fetchContent(etp, rawURL, audioLang)
	})
	s.lookupBtn.Importance = widget.HighImportance
	s.lookupBtn.Disable()
	s.dlBtn = widget.NewButtonWithIcon("Download selected", theme.DownloadIcon(), func() {
		etp := s.app.cookie()
		selected := s.selectedEpisodes()
		go s.downloadSelected(etp, selected)
	})
	s.dlBtn.Importance = widget.HighImportance
	s.dlBtn.Disable()

	s.resetBtn = widget.NewButtonWithIcon("Reset", theme.ViewRefreshIcon(), func() {
		s.resetForNewURL()
	})

	s.stopBtn = widget.NewButtonWithIcon("Stop", theme.CancelIcon(), func() {
		s.run.request()
		s.stopBtn.Disable()
		s.setStatus("Stopping — finishing the current segment…")
	})
	s.stopBtn.Importance = widget.DangerImportance
	s.stopBtn.Disable()

	s.progress = widget.NewProgressBar()
	s.progress.TextFormatter = s.progressText
	s.progress.Hide()
}

func (s *downloadSession) content() fyne.CanvasObject {
	browse := widget.NewButtonWithIcon("Browse", theme.FolderIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			s.outEntry.SetText(uri.Path())
			s.app.defaults.outputDir = uri.Path()
			outputDir = uri.Path()
		}, s.app.win)
	})

	s.seasonRow = container.NewVBox(widget.NewForm(
		widget.NewFormItem("Season", s.seasonSel),
	))
	s.optionsBox = container.NewVBox(
		widget.NewSeparator(),
		container.NewGridWithColumns(2,
			widget.NewForm(
				widget.NewFormItem("Audio / dub", container.NewBorder(nil, nil, nil, s.dubFallbackChk, s.audioSel)),
				widget.NewFormItem("Video quality", s.videoSel),
			),
			widget.NewForm(
				widget.NewFormItem("Subtitles", s.subsSel),
				widget.NewFormItem("Audio quality", s.audioQSel),
			),
		),
		s.seasonRow,
	)
	s.optionsBox.Hide()

	form := container.NewVBox(
		labeled("Crunchyroll URL", container.NewBorder(nil, nil, nil, s.lookupBtn, s.urlEntry)),
		labeled("Save downloads to", container.NewBorder(nil, nil, nil, browse, s.outEntry)),
		s.optionsBox,
	)

	selectAll := widget.NewButton("Select all", func() { s.setAllChecks(true) })
	selectNone := widget.NewButton("Select none", func() { s.setAllChecks(false) })

	s.episodeBox.Add(widget.NewLabel("Look up a URL to see episodes here."))

	episodeCard := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Episodes", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewHBox(selectAll, selectNone),
		),
		nil, nil, nil,
		container.NewScroll(s.episodeBox),
	)

	logCard := container.NewBorder(
		widget.NewLabelWithStyle("Progress", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		s.logEntry,
	)

	split := container.NewHSplit(episodeCard, logCard)
	split.SetOffset(0.48)

	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, 340))

	bottom := container.NewVBox(
		s.progress,
		container.NewBorder(nil, nil, s.status, container.NewHBox(s.resetBtn, s.stopBtn, s.dlBtn)),
	)

	return container.NewPadded(container.NewBorder(
		form, bottom, nil, nil, container.NewStack(floor, split),
	))
}

func (s *downloadSession) jobEnv() jobEnv {
	audio := languageCodeFromLabel(s.audioSel.Selected)
	subs := audio
	if s.subsSel.Selected != "" && s.subsSel.Selected != "None" {
		subs = languageCodeFromLabel(s.subsSel.Selected)
	}
	return jobEnv{
		ctx:           s.run.context(),
		log:           sessionLogWriter{append: s.appendLog},
		outputDir:     s.outEntry.Text,
		videoQuality:  s.videoSel.Selected,
		audioQuality:  s.audioQSel.Selected,
		subtitlesLang: subs,
		audioLang:     audio,
		dubFallback:   s.dubFallbackChk.Checked,
	}
}

func (s *downloadSession) setBusy(busy bool) {
	s.busyMu.Lock()
	s.busy = busy
	s.busyMu.Unlock()
	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		if busy {
			s.lookupBtn.Disable()
			s.dlBtn.Disable()
			s.resetBtn.Disable()
			return
		}
		s.stopBtn.Disable()
		s.resetBtn.Enable()
		if strings.TrimSpace(s.urlEntry.Text) != "" {
			s.lookupBtn.Enable()
		}
		if len(s.episodes) > 0 {
			s.dlBtn.Enable()
		}
	})
}

func (s *downloadSession) resetForNewURL() {
	if s.isBusy() {
		return
	}

	s.urlEntry.SetText("")
	s.clearLog()
	s.hideProgress()
	s.tabTitle = ""
	s.state = tabIdle
	s.status.SetText("Paste a Crunchyroll URL, then click Lookup.")
	s.app.refreshTabChrome(s)
}

func (s *downloadSession) setStatus(text string) {
	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.status.SetText(text)
	})
}

func (s *downloadSession) setAllChecks(selected bool) {
	for _, check := range s.checks {
		check.SetChecked(selected)
	}
}

func (s *downloadSession) selectedEpisodes() []SeasonEpisode {
	var selected []SeasonEpisode
	for i, check := range s.checks {
		if i >= len(s.episodes) {
			break
		}
		if check.Checked {
			selected = append(selected, s.episodes[i])
		}
	}
	return selected
}

func episodeKey(episode SeasonEpisode) string {
	return fmt.Sprintf("S%02dE%02d", episode.SeasonNumber, episode.EpisodeNumber)
}

func (s *downloadSession) checkedForEpisode(key string) bool {
	if !s.hasSelection {
		return true
	}
	remembered, ok := s.checkedKeys[key]
	if !ok {
		return true
	}
	return remembered
}

func (s *downloadSession) refreshEpisodeLabels() {
	if len(s.episodes) == 0 {
		return
	}
	s.renderEpisodes(s.currentTitle, s.episodes)
}

func (s *downloadSession) renderEpisodes(title string, episodes []SeasonEpisode) {
	s.episodes = episodes
	s.currentTitle = title
	s.checks = make([]*widget.Check, 0, len(episodes))
	s.episodeBox.RemoveAll()

	if len(episodes) == 0 {
		s.episodeBox.Add(widget.NewLabel("No episodes found for this selection."))
		s.dlBtn.Disable()
		s.status.SetText(title)
		s.episodeBox.Refresh()
		return
	}

	quality := s.videoSel.Selected
	dir := s.outEntry.Text
	for i := range episodes {
		ep := episodes[i]
		key := episodeKey(ep)
		label := fmt.Sprintf("S%02dE%02d  %s", ep.SeasonNumber, ep.EpisodeNumber, ep.Title)
		info := EpisodeInfo{
			Title: ep.Title,
			EpisodeMetadata: EpisodeMetadata{
				SeriesTitle:   ep.SeriesTitle,
				SeasonNumber:  ep.SeasonNumber,
				EpisodeNumber: ep.EpisodeNumber,
			},
		}
		if _, ok := findCompleteEpisodeFile(info, quality, dir); ok {
			label += "  (downloaded)"
		}

		checked := s.checkedForEpisode(key)
		check := widget.NewCheck(label, nil)
		check.SetChecked(checked)
		check.OnChanged = func(value bool) {
			s.checkedKeys[key] = value
		}
		s.checkedKeys[key] = checked
		s.checks = append(s.checks, check)
		s.episodeBox.Add(check)
	}

	s.hasSelection = true
	s.dlBtn.Enable()
	s.status.SetText(fmt.Sprintf("%s — %d episode(s)", title, len(episodes)))
	s.episodeBox.Refresh()
}

func (s *downloadSession) selectedSeasonNumber() (int, bool) {
	if s.seasonSel.Selected == "" || s.seasonSel.Selected == seasonAllLabel {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s.seasonSel.Selected, "Season "))
	if err != nil {
		return 0, false
	}
	return n, true
}

func (s *downloadSession) applyOutputDir() {
	dir := s.outEntry.Text
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if _, err := os.Stat(dir); err != nil {
			_ = os.MkdirAll(dir, 0755)
		}
	}
	s.app.defaults.outputDir = dir
}

func (s *downloadSession) onURLChanged(v string) {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		s.lookupBtn.Disable()
		s.resetLookupState()
		return
	}
	s.lookupBtn.Enable()
	if trimmed != s.lookedUpURL {
		s.resetLookupState()
	}
}

func (s *downloadSession) resetLookupState() {
	s.lookedUpURL = ""
	s.parsed = parsedContent{}
	s.seasons = nil
	s.episodes = nil
	s.checks = nil
	s.currentTitle = ""
	s.tabTitle = ""
	s.checkedKeys = make(map[string]bool)
	s.hasSelection = false
	if s.optionsBox != nil {
		s.optionsBox.Hide()
	}
	if s.episodeBox != nil {
		s.episodeBox.RemoveAll()
		s.episodeBox.Add(widget.NewLabel("Look up a URL to see episodes here."))
		s.episodeBox.Refresh()
	}
	if s.dlBtn != nil {
		s.dlBtn.Disable()
	}
	if s.status != nil && strings.TrimSpace(s.urlEntry.Text) != "" {
		s.status.SetText("Click Lookup to load audio, subtitles, quality, and seasons for this URL.")
	}
	s.app.refreshTabChrome(s)
	s.app.relayout()
}
