//go:build gui

package main

import (
	"os"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
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

const tabTitleMaxRunes = 28

var defaultVideoQualities = []string{"1080p", "720p", "480p", "360p"}
var defaultAudioQualities = []string{"192k", "128k", "96k"}

type sessionDefaults struct {
	audioLang    string
	subsLang     string
	videoQuality string
	audioQuality string
	outputDir    string
	convertVideo string
	convertAudio string
	dubFallback  bool
}

type tabSession interface {
	kind() tabKind
	tabItem() *container.TabItem
	isBusy() bool
	cancel()
	runState() tabRunState
	title() string
}

type guiApp struct {
	win           fyne.Window
	etpEntry      *widget.Entry
	signInBtn     *widget.Button
	accountStatus *widget.Label

	tabs     *container.DocTabs
	sessions map[*container.TabItem]tabSession

	defaults     sessionDefaults
	ignoreSelect bool

	browserLoginMu     sync.Mutex
	browserLoginCancel func()

	downloadSeq int
	convertSeq  int

	// winSize is the last content size seen while the window was open. The
	// canvas stops reporting a usable size once the window closes, so the
	// value is cached rather than read at save time.
	winSize fyne.Size
}

func main() {
	cfg := loadAppConfig()
	*audioLang = cfg.AudioLang
	*subtitlesLang = cfg.SubsLang
	*videoQuality = cfg.VideoQuality
	*audioQuality = cfg.AudioQuality
	*dubFallback = cfg.DubFallback
	outputDir = cfg.OutputDir

	a := app.NewWithID("com.mantisware.crunchyroll-downloader")
	a.Settings().SetTheme(crunchyTheme{})

	g := &guiApp{
		sessions: make(map[*container.TabItem]tabSession),
		defaults: sessionDefaults{
			audioLang:    cfg.AudioLang,
			subsLang:     cfg.SubsLang,
			videoQuality: cfg.VideoQuality,
			audioQuality: cfg.AudioQuality,
			outputDir:    cfg.OutputDir,
			convertVideo: cfg.ConvertVideoQuality,
			convertAudio: cfg.ConvertAudioQuality,
			dubFallback:  cfg.DubFallback,
		},
	}
	g.win = a.NewWindow("Crunchyroll Downloader")
	g.winSize = fyne.NewSize(cfg.WindowWidth, cfg.WindowHeight)
	g.win.Resize(g.winSize)
	// Applying a saved value to a Select fires OnChanged immediately, so the
	// handlers stay suppressed until every widget exists. Otherwise a convert
	// tab's defaults get saved over the loaded config before it is built.
	g.ignoreSelect = true
	g.buildAccount(cfg)
	g.win.SetContent(g.layout())
	g.ignoreSelect = false

	g.win.SetCloseIntercept(func() {
		g.rememberWindowSize()
		g.win.Close()
	})
	g.win.SetOnClosed(func() {
		g.cancelBrowserLogin()
		g.persistSettings()
	})
	g.win.SetOnDropped(func(_ fyne.Position, uris []fyne.URI) {
		if sess, ok := g.selectedConvert(); ok {
			sess.handleDroppedURIs(uris)
		}
	})
	g.win.ShowAndRun()
}

func (g *guiApp) buildAccount(cfg appConfig) {
	g.etpEntry = widget.NewPasswordEntry()
	g.etpEntry.SetPlaceHolder("etp_rt cookie from crunchyroll.com")
	g.etpEntry.SetText(cfg.EtpRt)
	g.etpEntry.OnChanged = func(_ string) {
		g.persistSettings()
	}
	g.signInBtn = widget.NewButtonWithIcon("Sign in with browser", theme.LoginIcon(), func() {
		g.startOrCancelBrowserLogin()
	})
	g.accountStatus = widget.NewLabel("")
	g.accountStatus.Wrapping = fyne.TextWrapWord
	g.accountStatus.Hide()
}

func (g *guiApp) layout() fyne.CanvasObject {
	header := container.NewVBox(
		container.NewVBox(
			g.cookieLabel(),
			container.NewBorder(nil, nil, nil, g.signInBtn, g.etpEntry),
		),
		g.accountStatus,
	)
	return container.NewBorder(container.NewPadded(header), nil, nil, nil, g.buildTabs())
}

func (g *guiApp) persistSettings() {
	g.rememberWindowSize()
	*audioLang = g.defaults.audioLang
	*subtitlesLang = g.defaults.subsLang
	*videoQuality = g.defaults.videoQuality
	*audioQuality = g.defaults.audioQuality
	*dubFallback = g.defaults.dubFallback
	if g.defaults.outputDir != "" {
		outputDir = g.defaults.outputDir
	}
	saveAppConfig(appConfig{
		EtpRt:               g.etpEntry.Text,
		AudioLang:           g.defaults.audioLang,
		SubsLang:            g.defaults.subsLang,
		VideoQuality:        g.defaults.videoQuality,
		AudioQuality:        g.defaults.audioQuality,
		OutputDir:           g.defaults.outputDir,
		ConvertVideoQuality: g.defaults.convertVideo,
		ConvertAudioQuality: g.defaults.convertAudio,
		DubFallback:         g.defaults.dubFallback,
		WindowWidth:         g.winSize.Width,
		WindowHeight:        g.winSize.Height,
	})
}

func (g *guiApp) cookie() string {
	if g.etpEntry == nil {
		return ""
	}
	return strings.TrimSpace(g.etpEntry.Text)
}

func (g *guiApp) applyAccountStatus(text string) {
	if text == "" {
		g.accountStatus.SetText("")
		g.accountStatus.Hide()
		g.relayout()
		return
	}
	g.accountStatus.SetText(text)
	g.accountStatus.Show()
	g.relayout()
}

func (g *guiApp) setAccountStatus(text string) {
	fyne.Do(func() {
		g.applyAccountStatus(text)
	})
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

// rememberWindowSize caches the current content size so it can be saved later.
// Sizes are only recorded while the window is genuinely on screen, because a
// closing or minimised window reports a zero canvas.
func (g *guiApp) rememberWindowSize() {
	if g.win == nil {
		return
	}
	size := g.win.Canvas().Size()
	if size.Width > 0 && size.Height > 0 {
		g.winSize = size
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

func (g *guiApp) defaultOutputDir() string {
	saveDir := g.defaults.outputDir
	if saveDir == "" || saveDir == "." {
		if wd, err := os.Getwd(); err == nil {
			saveDir = wd
		} else {
			saveDir = "."
		}
	}
	return saveDir
}

func setSelectOptions(sel *widget.Select, options []string, preferred string) {
	if len(options) == 0 {
		return
	}
	sel.Options = options
	selected := pickPreferred(options, preferred)
	sel.SetSelected(selected)
	sel.Refresh()
}
