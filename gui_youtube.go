//go:build gui

package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type youtubeCheck struct {
	id    string
	check *widget.Check
}

type youtubeSession struct {
	app   *guiApp
	tab   *container.TabItem
	seq   int
	run   cancellable
	state tabRunState

	urlEntry     *widget.Entry
	outEntry     *widget.Entry
	videoSel     *widget.Select
	status       *widget.Label
	logEntry     *widget.Entry
	logRaw       string
	progress     *widget.ProgressBar
	lookupBtn    *widget.Button
	dlBtn        *widget.Button
	stopBtn      *widget.Button
	resetBtn     *widget.Button
	optionsBox   *fyne.Container
	listBox      *fyne.Container
	selectRow    *fyne.Container
	tabTitle     string
	lookedUpURL  string
	lookup       youtubeLookup
	checks       []youtubeCheck
	signInBtn    *widget.Button
	doneBtn      *widget.Button
	signInMu     sync.Mutex
	signInCancel context.CancelFunc
	signInDone   chan struct{}
	filling      bool

	mu   sync.Mutex
	busy bool
}

func (s *youtubeSession) stillOpen() bool {
	if s.app == nil || s.tab == nil {
		return false
	}
	return s.app.sessionFor(s.tab) == s
}

func (s *youtubeSession) kind() tabKind { return tabKindYouTube }

func (s *youtubeSession) tabItem() *container.TabItem { return s.tab }

func (s *youtubeSession) isBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

func (s *youtubeSession) cancel() { s.run.request() }

func (s *youtubeSession) runState() tabRunState { return s.state }

func (s *youtubeSession) title() string {
	if s.tabTitle != "" {
		return s.tabTitle
	}
	return numberedSessionTitle(tabKindYouTube, s.seq)
}

func (g *guiApp) newYouTubeSession() *youtubeSession {
	g.youtubeSeq++
	s := &youtubeSession{
		app: g,
		seq: g.youtubeSeq,
	}
	s.build()
	return s
}

func (s *youtubeSession) build() {
	s.urlEntry = widget.NewEntry()
	s.urlEntry.SetPlaceHolder("https://... video or playlist link")
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

	s.videoSel = widget.NewSelect(defaultVideoQualities, nil)
	s.videoSel.SetSelected(pickPreferred(defaultVideoQualities, s.app.defaults.videoQuality))
	s.videoSel.OnChanged = func(v string) {
		if s.app.ignoreSelect || v == "" {
			return
		}
		s.app.defaults.videoQuality = v
		s.app.persistSettings()
	}

	s.status = widget.NewLabel("Paste a video URL, then click Lookup.")
	s.status.Wrapping = fyne.TextWrapWord

	s.logEntry = widget.NewMultiLineEntry()
	s.logEntry.Disable()
	s.logEntry.SetPlaceHolder("Download progress appears here…")
	s.logEntry.Wrapping = fyne.TextWrapWord

	s.listBox = container.NewVBox()
	s.listBox.Add(widget.NewLabel("Look up a URL to see the video here."))

	s.lookupBtn = widget.NewButtonWithIcon("Lookup", theme.SearchIcon(), func() {
		rawURL := strings.TrimSpace(s.urlEntry.Text)
		go s.fetchContent(rawURL)
	})
	s.lookupBtn.Importance = widget.HighImportance
	s.lookupBtn.Disable()

	s.signInBtn = widget.NewButtonWithIcon("Sign in", theme.LoginIcon(), func() {
		s.startSignIn()
	})
	s.doneBtn = widget.NewButton("Done signing in", func() {
		s.completeSignIn()
	})
	s.doneBtn.Hide()

	s.dlBtn = widget.NewButtonWithIcon("Download", theme.DownloadIcon(), func() {
		lookup := s.lookup
		selected := s.selectedIDs()
		quality := s.videoSel.Selected
		dir := s.outEntry.Text
		go s.download(lookup, selected, quality, dir)
	})
	s.dlBtn.Importance = widget.HighImportance
	s.dlBtn.Hide()

	s.resetBtn = widget.NewButtonWithIcon("Reset", theme.ViewRefreshIcon(), func() {
		s.resetForNewURL()
	})

	s.stopBtn = widget.NewButtonWithIcon("Stop", theme.CancelIcon(), func() {
		s.run.request()
		s.stopBtn.Disable()
		s.setStatus("Stopping…")
	})
	s.stopBtn.Importance = widget.DangerImportance
	s.stopBtn.Disable()

	s.progress = widget.NewProgressBar()
	s.progress.Hide()
}

func (s *youtubeSession) content() fyne.CanvasObject {
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

	s.optionsBox = container.NewVBox(
		widget.NewSeparator(),
		widget.NewForm(widget.NewFormItem("Video quality", s.videoSel)),
	)
	s.optionsBox.Hide()

	selectAll := widget.NewButton("Select all", func() { s.setAllChecks(true) })
	selectNone := widget.NewButton("Select none", func() { s.setAllChecks(false) })
	s.selectRow = container.NewHBox(selectAll, selectNone)
	s.selectRow.Hide()

	form := container.NewVBox(
		labeled("Video URL", container.NewBorder(nil, nil, nil, container.NewHBox(s.signInBtn, s.doneBtn, s.lookupBtn), s.urlEntry)),
		labeled("Save downloads to", container.NewBorder(nil, nil, nil, browse, s.outEntry)),
		s.optionsBox,
	)

	listCard := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Videos", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			s.selectRow,
		),
		nil, nil, nil,
		container.NewScroll(s.listBox),
	)
	logCard := container.NewBorder(
		widget.NewLabelWithStyle("Progress", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		s.logEntry,
	)
	split := container.NewHSplit(listCard, logCard)
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

func (s *youtubeSession) fetchContent(rawURL string) {
	s.setBusy(true)
	defer s.setBusy(false)

	s.run.begin()
	defer s.run.end()

	if _, err := classifyVideoURL(rawURL); err != nil {
		s.setStatus(err.Error())
		return
	}

	s.setStatus("Looking up…")
	lookup, err := lookupYouTube(s.run.context(), rawURL, sessionLogWriter{append: s.appendLog})
	if err != nil {
		if errors.Is(err, errCancelled) {
			s.setRunState(tabStopped)
			s.setStatus("Stopped.")
			return
		}
		s.setStatus(err.Error())
		return
	}

	fyne.Do(func() {
		if !s.stillOpen() || strings.TrimSpace(s.urlEntry.Text) != rawURL {
			return
		}
		s.applyLookup(lookup, rawURL)
	})
}

func (s *youtubeSession) applyLookup(lookup youtubeLookup, rawURL string) {
	s.lookup = lookup
	s.lookedUpURL = rawURL
	desired := strings.TrimSpace(lookup.Title)
	if desired == "" {
		desired = lookup.ID
	}
	if desired != "" {
		s.tabTitle = uniqueTitle(truncateTitle(desired, tabTitleMaxRunes), s.app.otherTitles(s.tab))
	}
	s.optionsBox.Show()
	s.showEntries()
	s.refreshDownloadButton()
	if lookup.Kind == youtubePlaylist {
		s.status.SetText(fmt.Sprintf("%d videos. Choose the ones to download.", len(lookup.Entries)))
	} else {
		s.status.SetText("Ready to download.")
	}
	s.state = tabIdle
	s.app.refreshTabChrome(s)
	s.app.relayout()
	s.app.ensureWindowFits()
}

func (s *youtubeSession) showEntries() {
	s.listBox.RemoveAll()
	s.checks = nil
	if s.lookup.Kind == youtubePlaylist {
		s.selectRow.Show()
		s.filling = true
		for _, entry := range s.lookup.Entries {
			entry := entry
			title := strings.TrimSpace(entry.Title)
			if title == "" {
				title = entry.ID
			}
			label := youtubeIndexLabel(entry.Index, len(s.lookup.Entries)) + "  " + title
			check := widget.NewCheck(label, func(bool) {
				if s.filling {
					return
				}
				s.refreshDownloadButton()
			})
			check.SetChecked(true)
			s.checks = append(s.checks, youtubeCheck{id: entry.ID, check: check})
			s.listBox.Add(check)
		}
		s.filling = false
		s.listBox.Refresh()
		return
	}

	s.selectRow.Hide()
	title := strings.TrimSpace(s.lookup.Title)
	if title == "" && len(s.lookup.Entries) > 0 {
		title = s.lookup.Entries[0].ID
	}
	s.listBox.Add(widget.NewLabel(title))
	s.listBox.Refresh()
}

func (s *youtubeSession) download(lookup youtubeLookup, selected []string, quality, dir string) {
	s.setBusy(true)
	defer s.setBusy(false)

	s.run.begin()
	defer s.run.end()

	if len(selected) == 0 {
		s.setStatus("Choose at least one video.")
		return
	}

	s.prepareOutputDir(dir)
	jobs, err := youtubeJobs(dir, lookup, selected, quality)
	if err != nil {
		s.setStatus(err.Error())
		return
	}

	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.state = tabRunning
		s.app.refreshTabChrome(s)
		s.logRaw = ""
		s.logEntry.SetText("")
		s.progress.SetValue(0)
		s.progress.Show()
		s.stopBtn.Enable()
	})

	env := jobEnv{
		ctx:          s.run.context(),
		log:          sessionLogWriter{append: s.appendLog},
		outputDir:    dir,
		videoQuality: quality,
	}
	err = downloadYouTubeJobs(env, jobs, quality, func(done, total int, pct float64) {
		fyne.Do(func() {
			if !s.stillOpen() {
				return
			}
			s.progress.Show()
			s.progress.SetValue(youtubeOverallProgress(done, total, pct))
			shown := done
			if pct < 100 {
				shown = done + 1
			}
			if shown < 1 {
				shown = 1
			}
			if total > 0 && shown > total {
				shown = total
			}
			s.status.SetText(fmt.Sprintf("Downloading %d of %d…", shown, total))
		})
	})

	message := "Download finished."
	state := tabComplete
	if errors.Is(err, errCancelled) {
		message = "Stopped."
		state = tabStopped
	} else if err != nil {
		message = err.Error()
	}
	s.setRunState(state)
	s.setStatus(message)
}

func (s *youtubeSession) prepareOutputDir(dir string) {
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if _, err := os.Stat(dir); err != nil {
			_ = os.MkdirAll(dir, 0o755)
		}
	}
	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.app.defaults.outputDir = dir
		outputDir = dir
		if !s.app.ignoreSelect {
			s.app.persistSettings()
		}
	})
}

func (s *youtubeSession) selectedIDs() []string {
	if s.lookup.Kind == youtubeVideo && len(s.lookup.Entries) > 0 {
		return []string{s.lookup.Entries[0].ID}
	}
	ids := make([]string, 0, len(s.checks))
	for _, item := range s.checks {
		if item.check != nil && item.check.Checked {
			ids = append(ids, item.id)
		}
	}
	return ids
}

func (s *youtubeSession) setAllChecks(selected bool) {
	s.filling = true
	for _, item := range s.checks {
		item.check.SetChecked(selected)
	}
	s.filling = false
	s.refreshDownloadButton()
}

func (s *youtubeSession) refreshDownloadButton() {
	if s.lookup.Kind == 0 || len(s.lookup.Entries) == 0 {
		s.dlBtn.Hide()
		s.dlBtn.Disable()
		return
	}
	s.dlBtn.Show()
	if s.lookup.Kind == youtubePlaylist {
		s.dlBtn.SetText("Download selected")
		if len(s.selectedIDs()) == 0 {
			s.dlBtn.Disable()
			return
		}
	} else {
		s.dlBtn.SetText("Download")
	}
	s.dlBtn.Enable()
}

func (s *youtubeSession) setBusy(busy bool) {
	s.mu.Lock()
	s.busy = busy
	s.mu.Unlock()
	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		if busy {
			s.lookupBtn.Disable()
			s.signInBtn.Disable()
			s.doneBtn.Disable()
			s.dlBtn.Disable()
			s.resetBtn.Disable()
			s.stopBtn.Enable()
			return
		}
		s.stopBtn.Disable()
		s.resetBtn.Enable()
		s.signInBtn.Enable()
		s.doneBtn.Enable()
		if strings.TrimSpace(s.urlEntry.Text) != "" {
			s.lookupBtn.Enable()
		}
		s.refreshDownloadButton()
	})
}

func (s *youtubeSession) resetForNewURL() {
	if s.isBusy() {
		return
	}
	s.urlEntry.SetText("")
	s.clearLog()
	s.hideProgress()
	s.resetLookupState()
	s.status.SetText("Paste a video URL, then click Lookup.")
	s.state = tabIdle
	s.app.refreshTabChrome(s)
}

func (s *youtubeSession) onURLChanged(v string) {
	if s.isBusy() {
		return
	}
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		s.lookupBtn.Disable()
		s.resetLookupState()
		return
	}
	if !s.isBusy() {
		s.lookupBtn.Enable()
	}
	if trimmed != s.lookedUpURL {
		s.resetLookupState()
	}
}

func (s *youtubeSession) resetLookupState() {
	s.lookedUpURL = ""
	s.lookup = youtubeLookup{}
	s.checks = nil
	s.tabTitle = ""
	if s.optionsBox != nil {
		s.optionsBox.Hide()
	}
	if s.selectRow != nil {
		s.selectRow.Hide()
	}
	if s.listBox != nil {
		s.listBox.RemoveAll()
		s.listBox.Add(widget.NewLabel("Look up a URL to see the video here."))
		s.listBox.Refresh()
	}
	if s.dlBtn != nil {
		s.dlBtn.Hide()
		s.dlBtn.Disable()
	}
	s.app.refreshTabChrome(s)
	s.app.relayout()
}

func (s *youtubeSession) appendLog(chunk string) {
	if !s.stillOpen() {
		return
	}
	s.logRaw += chunk
	s.logEntry.SetText(renderLog(s.logRaw))
}

func (s *youtubeSession) clearLog() {
	s.logRaw = ""
	s.logEntry.SetText("")
}

func (s *youtubeSession) hideProgress() {
	s.progress.SetValue(0)
	s.progress.Hide()
}

func (s *youtubeSession) setStatus(text string) {
	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.status.SetText(text)
	})
}

func (s *youtubeSession) startSignIn() {
	rawURL := strings.TrimSpace(s.urlEntry.Text)
	if _, err := classifyVideoURL(rawURL); err != nil {
		s.setStatus(err.Error())
		return
	}

	s.signInMu.Lock()
	if s.signInCancel != nil {
		cancel := s.signInCancel
		s.signInMu.Unlock()
		cancel()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), browserLoginTimeout)
	done := make(chan struct{})
	s.signInCancel = cancel
	s.signInDone = done
	s.signInMu.Unlock()

	s.signInBtn.SetText("Cancel")
	s.signInBtn.SetIcon(theme.CancelIcon())
	s.doneBtn.Show()
	s.lookupBtn.Disable()
	s.setStatus("Sign in in the browser, then click Done signing in.")

	go func() {
		cookies, err := captureVisibleSiteCookies(ctx, rawURL, done)
		cancel()
		if err == nil {
			err = writeVideoCookies(rawURL, cookies)
		}
		host, _ := videoCookieHost(rawURL)

		s.signInMu.Lock()
		s.signInCancel = nil
		s.signInDone = nil
		s.signInMu.Unlock()

		fyne.Do(func() {
			if !s.stillOpen() {
				return
			}
			s.signInBtn.SetText("Sign in")
			s.signInBtn.SetIcon(theme.LoginIcon())
			s.doneBtn.Hide()
			if strings.TrimSpace(s.urlEntry.Text) != "" && !s.isBusy() {
				s.lookupBtn.Enable()
			}
			if err != nil {
				s.status.SetText(videoSignInStatus(err))
				return
			}
			s.status.SetText("Signed in to " + host)
		})
	}()
}

func (s *youtubeSession) completeSignIn() {
	s.signInMu.Lock()
	done := s.signInDone
	s.signInDone = nil
	s.signInMu.Unlock()
	if done != nil {
		close(done)
	}
}

func videoSignInStatus(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "Sign-in cancelled. Saved cookies were left unchanged."
	case errors.Is(err, context.DeadlineExceeded):
		return "Sign-in timed out after 10 minutes. Saved cookies were left unchanged."
	case errors.Is(err, errBrowserClosed):
		return "Browser closed before sign-in finished. Saved cookies were left unchanged."
	default:
		return err.Error()
	}
}

func (s *youtubeSession) setRunState(state tabRunState) {
	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.state = state
		s.app.refreshTabChrome(s)
	})
}
