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
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const seasonAllLabel = "All seasons"

const cookieHelpText = `The easiest option is Sign in with browser. A temporary Chrome, Edge, Brave, or Chromium window opens so you can sign in directly with Crunchyroll. The app captures only the etp_rt session cookie and removes the temporary browser profile afterward.

Manual fallback:
Log in at crunchyroll.com, then copy the etp_rt cookie from Developer Tools.

Firefox: Storage → Cookies → etp_rt
Chrome / Edge: Application → Cookies → etp_rt`

var defaultVideoQualities = []string{"1080p", "720p", "480p", "360p"}
var defaultAudioQualities = []string{"192k", "128k", "96k"}

type guiApp struct {
	win        fyne.Window
	etpEntry   *widget.Entry
	urlEntry   *widget.Entry
	outEntry   *widget.Entry
	audioSel   *widget.Select
	subsSel    *widget.Select
	videoSel   *widget.Select
	audioQSel  *widget.Select
	seasonSel  *widget.Select
	optionsBox *fyne.Container
	seasonRow  *fyne.Container
	status     *widget.Label
	logEntry   *widget.Entry
	logRaw     string
	episodeBox *fyne.Container
	lookupBtn  *widget.Button
	dlBtn      *widget.Button
	resetBtn   *widget.Button
	stopBtn    *widget.Button
	signInBtn  *widget.Button
	progress   *widget.ProgressBar

	progressMu sync.Mutex
	dlActive   bool
	dlTotal    int
	dlDone     int
	dlPhase    int
	dlPhasePct float64

	browserLoginMu     sync.Mutex
	browserLoginCancel func()

	convertSourceEntry *widget.Entry
	convertVideoSel    *widget.Select
	convertAudioSel    *widget.Select
	convertStatus      *widget.Label
	convertLogEntry    *widget.Entry
	convertLogRaw      string
	convertProgress    *widget.ProgressBar
	convertBtn         *widget.Button
	convertStopBtn     *widget.Button
	convertClearBtn    *widget.Button
	convertPaths       []string
	convertFolder      string
	isConvertTab       bool
	convertMu          sync.Mutex
	convertActive      bool

	parsed       parsedContent
	lookedUpURL  string
	seasons      []Season
	episodes     []SeasonEpisode
	checks       []*widget.Check
	checkedKeys  map[string]bool
	hasSelection bool
	currentTitle string
	busy         bool
	ignoreSelect bool
}

func main() {
	cfg := loadAppConfig()
	*audioLang = cfg.AudioLang
	*subtitlesLang = cfg.SubsLang
	*videoQuality = cfg.VideoQuality
	*audioQuality = cfg.AudioQuality
	outputDir = cfg.OutputDir

	a := app.NewWithID("com.mantisware.crunchyroll-downloader")
	a.Settings().SetTheme(crunchyTheme{})

	g := &guiApp{checkedKeys: make(map[string]bool)}
	g.win = a.NewWindow("Crunchyroll Downloader")
	g.win.Resize(fyne.NewSize(1040, 820))
	// Applying a saved value to a Select fires OnChanged immediately, so the
	// handlers stay suppressed until every widget exists. Otherwise the convert
	// tab's defaults get saved over the loaded config before it is built.
	g.ignoreSelect = true
	g.build(cfg)
	g.buildConvert(cfg)
	g.ignoreSelect = false

	g.startLogCapture()
	g.win.SetOnClosed(func() {
		g.cancelBrowserLogin()
		g.persistSettings()
	})
	g.win.SetContent(g.layout())
	g.win.SetOnDropped(func(_ fyne.Position, uris []fyne.URI) {
		if g.isConvertTab {
			g.handleDroppedURIs(uris)
		}
	})
	g.win.ShowAndRun()
}

func (g *guiApp) build(cfg appConfig) {
	g.etpEntry = widget.NewPasswordEntry()
	g.etpEntry.SetPlaceHolder("etp_rt cookie from crunchyroll.com")
	g.etpEntry.SetText(cfg.EtpRt)
	g.etpEntry.OnChanged = func(_ string) {
		g.persistSettings()
	}
	g.signInBtn = widget.NewButtonWithIcon("Sign in with browser", theme.LoginIcon(), func() {
		g.startOrCancelBrowserLogin()
	})

	g.urlEntry = widget.NewEntry()
	g.urlEntry.SetPlaceHolder("https://www.crunchyroll.com/series/... or /watch/...")
	g.urlEntry.OnChanged = func(v string) {
		g.onURLChanged(v)
	}

	g.outEntry = widget.NewEntry()
	saveDir := cfg.OutputDir
	if saveDir == "" || saveDir == "." {
		if wd, err := os.Getwd(); err == nil {
			saveDir = wd
		} else {
			saveDir = "."
		}
	}
	g.outEntry.SetText(saveDir)
	outputDir = saveDir
	g.outEntry.OnChanged = func(v string) {
		if v != "" {
			outputDir = v
		}
		g.persistSettings()
	}

	g.audioSel = widget.NewSelect(nil, func(label string) {
		*audioLang = languageCodeFromLabel(label)
		if g.ignoreSelect {
			return
		}
		g.persistSettings()
		if g.parsed.Kind == kindSeries && len(g.seasons) > 0 && !g.busy {
			go g.reloadEpisodes()
		}
	})
	g.audioSel.PlaceHolder = "Lookup a URL first"

	g.subsSel = widget.NewSelect([]string{"None"}, func(label string) {
		if label == "None" {
			*subtitlesLang = *audioLang
		} else {
			*subtitlesLang = languageCodeFromLabel(label)
		}
		if !g.ignoreSelect {
			g.persistSettings()
		}
	})
	g.subsSel.PlaceHolder = "Lookup a URL first"
	g.subsSel.SetSelected("None")

	g.videoSel = widget.NewSelect(defaultVideoQualities, func(v string) {
		*videoQuality = v
		if g.ignoreSelect {
			return
		}
		g.persistSettings()
		// The "(downloaded)" markers are per-quality, so re-label the list.
		g.refreshEpisodeLabels()
	})
	g.videoSel.PlaceHolder = "Lookup a URL first"

	g.audioQSel = widget.NewSelect(defaultAudioQualities, func(v string) {
		*audioQuality = v
		if !g.ignoreSelect {
			g.persistSettings()
		}
	})
	g.audioQSel.PlaceHolder = "Lookup a URL first"

	g.seasonSel = widget.NewSelect([]string{seasonAllLabel}, func(_ string) {
		if g.ignoreSelect {
			return
		}
		if g.parsed.Kind == kindSeries && len(g.seasons) > 0 && !g.busy {
			go g.reloadEpisodes()
		}
	})
	g.seasonSel.SetSelected(seasonAllLabel)
	g.seasonSel.Disable()

	g.status = widget.NewLabel("Paste a Crunchyroll URL, then click Lookup.")
	g.status.Wrapping = fyne.TextWrapWord

	g.logEntry = widget.NewMultiLineEntry()
	g.logEntry.SetPlaceHolder("Download progress appears here…")
	g.logEntry.Wrapping = fyne.TextWrapWord

	g.episodeBox = container.NewVBox()
	g.lookupBtn = widget.NewButtonWithIcon("Lookup", theme.SearchIcon(), func() {
		etp := strings.TrimSpace(g.etpEntry.Text)
		rawURL := strings.TrimSpace(g.urlEntry.Text)
		go g.fetchContent(etp, rawURL)
	})
	g.lookupBtn.Importance = widget.HighImportance
	g.lookupBtn.Disable()
	g.dlBtn = widget.NewButtonWithIcon("Download selected", theme.DownloadIcon(), func() {
		etp := strings.TrimSpace(g.etpEntry.Text)
		selected := g.selectedEpisodes()
		go g.downloadSelected(etp, selected)
	})
	g.dlBtn.Importance = widget.HighImportance
	g.dlBtn.Disable()

	g.resetBtn = widget.NewButtonWithIcon("Reset", theme.ViewRefreshIcon(), func() {
		g.resetForNewURL()
	})

	g.stopBtn = widget.NewButtonWithIcon("Stop", theme.CancelIcon(), func() {
		requestCancel()
		g.stopBtn.Disable()
		g.setStatus("Stopping — finishing the current segment…")
	})
	g.stopBtn.Importance = widget.DangerImportance
	g.stopBtn.Disable()

	g.progress = widget.NewProgressBar()
	g.progress.TextFormatter = g.progressText
	g.progress.Hide()
}

func (g *guiApp) downloadTabContent() fyne.CanvasObject {
	browse := widget.NewButtonWithIcon("Browse", theme.FolderIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			g.outEntry.SetText(uri.Path())
			outputDir = uri.Path()
		}, g.win)
	})

	// Form rows keep the label beside the control, which halves the vertical
	// space the options take once they become visible.
	g.seasonRow = container.NewVBox(widget.NewForm(
		widget.NewFormItem("Season", g.seasonSel),
	))
	g.optionsBox = container.NewVBox(
		widget.NewSeparator(),
		container.NewGridWithColumns(2,
			widget.NewForm(
				widget.NewFormItem("Audio / dub", g.audioSel),
				widget.NewFormItem("Video quality", g.videoSel),
			),
			widget.NewForm(
				widget.NewFormItem("Subtitles", g.subsSel),
				widget.NewFormItem("Audio quality", g.audioQSel),
			),
		),
		g.seasonRow,
	)
	g.optionsBox.Hide()

	form := container.NewVBox(
		container.NewVBox(
			g.cookieLabel(),
			container.NewBorder(nil, nil, nil, g.signInBtn, g.etpEntry),
		),
		labeled("Crunchyroll URL", container.NewBorder(nil, nil, nil, g.lookupBtn, g.urlEntry)),
		labeled("Save downloads to", container.NewBorder(nil, nil, nil, browse, g.outEntry)),
		g.optionsBox,
	)

	selectAll := widget.NewButton("Select all", func() { g.setAllChecks(true) })
	selectNone := widget.NewButton("Select none", func() { g.setAllChecks(false) })

	g.episodeBox.Add(widget.NewLabel("Look up a URL to see episodes here."))

	episodeCard := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Episodes", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewHBox(selectAll, selectNone),
		),
		nil, nil, nil,
		container.NewScroll(g.episodeBox),
	)

	logCard := container.NewBorder(
		widget.NewLabelWithStyle("Progress", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		g.logEntry,
	)

	split := container.NewHSplit(episodeCard, logCard)
	split.SetOffset(0.48)

	// Floor for the episode/progress area so revealing the options grows the
	// window instead of squeezing the lists.
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, 340))

	bottom := container.NewVBox(
		g.progress,
		container.NewBorder(nil, nil, g.status, container.NewHBox(g.resetBtn, g.stopBtn, g.dlBtn)),
	)

	return container.NewPadded(container.NewBorder(
		form, bottom, nil, nil, container.NewStack(floor, split),
	))
}

func (g *guiApp) layout() fyne.CanvasObject {
	downloadTab := container.NewTabItemWithIcon("Download", theme.DownloadIcon(), g.downloadTabContent())
	convertTab := container.NewTabItemWithIcon("Convert", theme.MediaVideoIcon(), g.convertTabContent())
	tabs := container.NewAppTabs(downloadTab, convertTab)
	tabs.OnSelected = func(item *container.TabItem) {
		g.isConvertTab = item == convertTab
	}
	return tabs
}

// relayout re-runs the layout from the window root. Showing or hiding a
// container only toggles its Hidden flag, and refreshing that container only
// lays out its own children, so nothing tells the surrounding panes that the
// minimum size changed. Without this they keep their old geometry until the
// user resizes the window, and revealed controls draw on top of them.
func (g *guiApp) relayout() {
	if g.win == nil {
		return
	}
	if content := g.win.Content(); content != nil {
		content.Refresh()
	}
}

// ensureWindowFits grows the window when the content needs more room than the
// current size, so newly shown controls never compress the rest of the layout.
func (g *guiApp) ensureWindowFits() {
	content := g.win.Content()
	if content == nil {
		return
	}

	needed := content.MinSize()
	current := g.win.Canvas().Size()
	width := current.Width
	height := current.Height

	if needed.Width > width {
		width = needed.Width
	}
	if needed.Height > height {
		height = needed.Height
	}
	if width > current.Width || height > current.Height {
		g.win.Resize(fyne.NewSize(width, height))
	}
}

func labeled(title string, content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(widget.NewLabel(title), content)
}

func (g *guiApp) cookieLabel() fyne.CanvasObject {
	info := widget.NewButtonWithIcon("", theme.InfoIcon(), func() {
		help := widget.NewLabel(cookieHelpText)
		help.Wrapping = fyne.TextWrapWord
		d := dialog.NewCustom("Where to get etp_rt", "Got it", container.NewPadded(help), g.win)
		d.Resize(fyne.NewSize(520, 280))
		d.Show()
	})
	info.Importance = widget.LowImportance
	return container.NewHBox(
		widget.NewLabel("Account cookie (etp_rt)"),
		info,
	)
}

func (g *guiApp) persistSettings() {
	saveAppConfig(appConfig{
		EtpRt:               g.etpEntry.Text,
		AudioLang:           *audioLang,
		SubsLang:            *subtitlesLang,
		VideoQuality:        *videoQuality,
		AudioQuality:        *audioQuality,
		OutputDir:           g.outEntry.Text,
		ConvertVideoQuality: g.convertVideoQuality(),
		ConvertAudioQuality: g.convertAudioQuality(),
	})
}

func (g *guiApp) setBusy(busy bool) {
	g.busy = busy
	fyne.Do(func() {
		if busy {
			g.lookupBtn.Disable()
			g.dlBtn.Disable()
			g.resetBtn.Disable()
			g.convertBtn.Disable()
			g.convertClearBtn.Disable()
			return
		}
		g.stopBtn.Disable()
		g.resetBtn.Enable()
		g.convertClearBtn.Enable()
		if g.convertFolder != "" || len(g.convertPaths) > 0 {
			g.convertBtn.Enable()
		}
		if strings.TrimSpace(g.urlEntry.Text) != "" {
			g.lookupBtn.Enable()
		}
		if len(g.episodes) > 0 {
			g.dlBtn.Enable()
		}
	})
}

// resetForNewURL clears the looked-up title, episode list, log, and progress so
// a new URL can be pasted. Saved credentials and preferences are kept.
func (g *guiApp) resetForNewURL() {
	if g.busy {
		return
	}

	g.urlEntry.SetText("")
	g.clearLog()
	g.hideProgress()
	g.status.SetText("Paste a Crunchyroll URL, then click Lookup.")
}

func (g *guiApp) setStatus(text string) {
	fyne.Do(func() {
		g.status.SetText(text)
	})
}

func (g *guiApp) setAllChecks(selected bool) {
	for _, check := range g.checks {
		check.SetChecked(selected)
	}
}

func (g *guiApp) selectedEpisodes() []SeasonEpisode {
	var selected []SeasonEpisode
	for i, check := range g.checks {
		if i >= len(g.episodes) {
			break
		}
		if check.Checked {
			selected = append(selected, g.episodes[i])
		}
	}
	return selected
}

func episodeKey(episode SeasonEpisode) string {
	return fmt.Sprintf("S%02dE%02d", episode.SeasonNumber, episode.EpisodeNumber)
}

// checkedForEpisode returns the remembered checkbox state, defaulting to
// selected for episodes the user has not seen yet.
func (g *guiApp) checkedForEpisode(key string) bool {
	if !g.hasSelection {
		return true
	}
	remembered, ok := g.checkedKeys[key]
	if !ok {
		return true
	}
	return remembered
}

func (g *guiApp) refreshEpisodeLabels() {
	if len(g.episodes) == 0 {
		return
	}
	g.renderEpisodes(g.currentTitle, g.episodes)
}

func (g *guiApp) renderEpisodes(title string, episodes []SeasonEpisode) {
	g.episodes = episodes
	g.currentTitle = title
	g.checks = make([]*widget.Check, 0, len(episodes))
	g.episodeBox.RemoveAll()

	if len(episodes) == 0 {
		g.episodeBox.Add(widget.NewLabel("No episodes found for this selection."))
		g.dlBtn.Disable()
		g.status.SetText(title)
		g.episodeBox.Refresh()
		return
	}

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
		if _, ok := findCompleteEpisodeFile(info, *videoQuality); ok {
			label += "  (downloaded)"
		}

		checked := g.checkedForEpisode(key)
		check := widget.NewCheck(label, nil)
		check.SetChecked(checked)
		check.OnChanged = func(value bool) {
			g.checkedKeys[key] = value
		}
		g.checkedKeys[key] = checked
		g.checks = append(g.checks, check)
		g.episodeBox.Add(check)
	}

	g.hasSelection = true
	g.dlBtn.Enable()
	g.status.SetText(fmt.Sprintf("%s — %d episode(s)", title, len(episodes)))
	g.episodeBox.Refresh()
}

func (g *guiApp) selectedSeasonNumber() (int, bool) {
	if g.seasonSel.Selected == "" || g.seasonSel.Selected == seasonAllLabel {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(g.seasonSel.Selected, "Season "))
	if err != nil {
		return 0, false
	}
	return n, true
}

func (g *guiApp) applyOutputDir() {
	dir := g.outEntry.Text
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if _, err := os.Stat(dir); err != nil {
			_ = os.MkdirAll(dir, 0755)
		}
	}
	outputDir = dir
}

func (g *guiApp) onURLChanged(v string) {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		g.lookupBtn.Disable()
		g.resetLookupState()
		return
	}
	g.lookupBtn.Enable()
	if trimmed != g.lookedUpURL {
		g.resetLookupState()
	}
}

func (g *guiApp) resetLookupState() {
	g.lookedUpURL = ""
	g.parsed = parsedContent{}
	g.seasons = nil
	g.episodes = nil
	g.checks = nil
	g.currentTitle = ""
	g.checkedKeys = make(map[string]bool)
	g.hasSelection = false
	if g.optionsBox != nil {
		g.optionsBox.Hide()
	}
	if g.episodeBox != nil {
		g.episodeBox.RemoveAll()
		g.episodeBox.Add(widget.NewLabel("Look up a URL to see episodes here."))
		g.episodeBox.Refresh()
	}
	if g.dlBtn != nil {
		g.dlBtn.Disable()
	}
	if g.status != nil && strings.TrimSpace(g.urlEntry.Text) != "" {
		g.status.SetText("Click Lookup to load audio, subtitles, quality, and seasons for this URL.")
	}
	g.relayout()
}

func (g *guiApp) setSelectOptions(sel *widget.Select, options []string, preferred string) {
	if len(options) == 0 {
		return
	}
	sel.Options = options
	selected := pickPreferred(options, preferred)
	sel.SetSelected(selected)
	sel.Refresh()
}
