//go:build gui

package main

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var convertVideoQualities = []string{"1080p", "720p", "480p", "360p"}
var convertAudioQualities = []string{"320k", "256k", "192k", "128k", "96k"}

type convertSession struct {
	app   *guiApp
	tab   *container.TabItem
	seq   int
	run   cancellable
	state tabRunState

	sourceEntry   *widget.Entry
	videoSel      *widget.Select
	audioSel      *widget.Select
	status        *widget.Label
	logEntry      *widget.Entry
	logRaw        string
	progress      *widget.ProgressBar
	convertBtn    *widget.Button
	stopBtn       *widget.Button
	clearBtn      *widget.Button
	convertPaths  []string
	convertFolder string
	tabTitle      string

	mu     sync.Mutex
	active bool
	busy   bool
}

func (s *convertSession) stillOpen() bool {
	if s.app == nil || s.tab == nil {
		return false
	}
	return s.app.sessionFor(s.tab) == s
}

func (s *convertSession) kind() tabKind { return tabKindConvert }

func (s *convertSession) tabItem() *container.TabItem { return s.tab }

func (s *convertSession) isBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy || s.active
}

func (s *convertSession) cancel() { s.run.request() }

func (s *convertSession) runState() tabRunState { return s.state }

func (s *convertSession) title() string {
	if s.tabTitle != "" {
		return s.tabTitle
	}
	return numberedSessionTitle(tabKindConvert, s.seq)
}

func (g *guiApp) newConvertSession() *convertSession {
	g.convertSeq++
	s := &convertSession{
		app: g,
		seq: g.convertSeq,
	}
	s.build()
	return s
}

func (s *convertSession) build() {
	s.sourceEntry = widget.NewMultiLineEntry()
	s.sourceEntry.Disable()
	s.sourceEntry.SetMinRowsVisible(4)
	s.sourceEntry.SetPlaceHolder("Drop a video file or folder here, or use one of the buttons below.")

	s.videoSel = widget.NewSelect(convertVideoQualities, nil)
	s.videoSel.SetSelected(pickPreferred(convertVideoQualities, s.app.defaults.convertVideo))

	s.audioSel = widget.NewSelect(convertAudioQualities, nil)
	s.audioSel.SetSelected(pickPreferred(convertAudioQualities, s.app.defaults.convertAudio))

	s.status = widget.NewLabel("Choose a video file or folder to convert.")
	s.status.Wrapping = fyne.TextWrapWord

	s.logEntry = widget.NewMultiLineEntry()
	s.logEntry.Disable()
	s.logEntry.Wrapping = fyne.TextWrapWord
	s.logEntry.SetPlaceHolder("FFmpeg conversion output appears here…")

	s.progress = widget.NewProgressBar()
	s.progress.Hide()

	s.convertBtn = widget.NewButtonWithIcon("Convert", theme.MediaVideoIcon(), func() {
		jobs, err := s.currentConversionJobs()
		if err != nil {
			s.status.SetText(err.Error())
			return
		}
		videoQuality := s.convertVideoQuality()
		audioQuality := s.convertAudioQuality()
		go s.runConversion(jobs, videoQuality, audioQuality)
	})
	s.convertBtn.Importance = widget.HighImportance
	s.convertBtn.Disable()

	s.stopBtn = widget.NewButtonWithIcon("Stop", theme.CancelIcon(), func() {
		s.run.request()
		s.stopBtn.Disable()
		s.status.SetText("Stopping conversion…")
	})
	s.stopBtn.Importance = widget.DangerImportance
	s.stopBtn.Disable()

	s.clearBtn = widget.NewButtonWithIcon("Clear", theme.ContentClearIcon(), func() {
		s.clearConversion()
	})

	s.videoSel.OnChanged = func(v string) {
		if s.app.ignoreSelect {
			return
		}
		s.app.defaults.convertVideo = v
		s.app.persistSettings()
		s.refreshConvertSource()
		s.app.refreshTabChrome(s)
	}
	s.audioSel.OnChanged = func(v string) {
		if s.app.ignoreSelect {
			return
		}
		s.app.defaults.convertAudio = v
		s.app.persistSettings()
	}
}

func (s *convertSession) content() fyne.CanvasObject {
	selectFile := widget.NewButtonWithIcon("Choose file", theme.FileIcon(), func() {
		picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				s.status.SetText(fmt.Sprintf("Could not open file picker: %s", err))
				return
			}
			if reader == nil {
				return
			}
			path := reader.URI().Path()
			_ = reader.Close()
			s.setConversionFiles([]string{path})
		}, s.app.win)
		picker.Show()
	})

	selectFolder := widget.NewButtonWithIcon("Choose folder", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				s.status.SetText(fmt.Sprintf("Could not open folder picker: %s", err))
				return
			}
			if uri == nil {
				return
			}
			s.setConversionFolder(uri.Path())
		}, s.app.win)
	})

	dropHelp := widget.NewCard(
		"Input",
		"Drop one or more video files, or a folder, anywhere on this tab.",
		container.NewVBox(
			s.sourceEntry,
			container.NewHBox(selectFile, selectFolder, s.clearBtn),
		),
	)

	options := widget.NewForm(
		widget.NewFormItem("Video quality", s.videoSel),
		widget.NewFormItem("Audio quality", s.audioSel),
	)

	logFloor := canvas.NewRectangle(color.Transparent)
	logFloor.SetMinSize(fyne.NewSize(0, 300))
	logArea := container.NewStack(
		logFloor,
		container.NewBorder(
			widget.NewLabelWithStyle("FFmpeg output", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			nil, nil, nil,
			s.logEntry,
		),
	)

	bottom := container.NewVBox(
		s.progress,
		container.NewBorder(
			nil, nil,
			s.status,
			container.NewHBox(s.stopBtn, s.convertBtn),
		),
	)

	return container.NewPadded(container.NewBorder(
		container.NewVBox(dropHelp, options),
		bottom,
		nil,
		nil,
		logArea,
	))
}

func (s *convertSession) handleDroppedURIs(uris []fyne.URI) {
	if len(uris) == 0 || s.isConversionActive() {
		return
	}

	paths := make([]string, 0, len(uris))
	for _, uri := range uris {
		if uri == nil || uri.Path() == "" {
			continue
		}
		paths = append(paths, uri.Path())
	}
	if len(paths) == 0 {
		return
	}

	info, err := os.Stat(paths[0])
	if err != nil {
		s.status.SetText(fmt.Sprintf("Could not read dropped item: %s", err))
		return
	}
	if info.IsDir() {
		s.setConversionFolder(paths[0])
		return
	}
	s.setConversionFiles(paths)
}

func (s *convertSession) updateTabTitle() {
	desired := ""
	switch {
	case s.convertFolder != "":
		desired = filepath.Base(s.convertFolder)
	case len(s.convertPaths) == 1:
		desired = filepath.Base(s.convertPaths[0])
	case len(s.convertPaths) > 1:
		desired = fmt.Sprintf("%d files", len(s.convertPaths))
	}
	if desired == "" {
		s.tabTitle = ""
		return
	}
	s.tabTitle = uniqueTitle(truncateTitle(desired, tabTitleMaxRunes), s.app.otherTitles(s.tab))
}

func (s *convertSession) setConversionFiles(paths []string) {
	supported := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() && isSupportedVideo(path) {
			supported = append(supported, filepath.Clean(path))
		}
	}

	s.convertFolder = ""
	s.convertPaths = supported
	s.refreshConvertSource()
	s.updateTabTitle()
	s.app.refreshTabChrome(s)
	if len(supported) == 0 {
		s.status.SetText("No supported video files were selected.")
		return
	}
	s.status.SetText(fmt.Sprintf("%d file(s) ready to convert.", len(supported)))
}

func (s *convertSession) setConversionFolder(path string) {
	s.convertFolder = filepath.Clean(path)
	s.convertPaths = nil
	s.refreshConvertSource()
	s.updateTabTitle()
	s.app.refreshTabChrome(s)

	jobs, err := s.currentConversionJobs()
	if err != nil {
		s.status.SetText(err.Error())
		return
	}
	s.status.SetText(fmt.Sprintf("%d video file(s) found in the folder.", len(jobs)))
}

func (s *convertSession) refreshConvertSource() {
	switch {
	case s.convertFolder != "":
		output := filepath.Join(s.convertFolder, "converted_"+s.convertVideoQuality())
		s.sourceEntry.SetText(fmt.Sprintf("Folder: %s\nOutput: %s", s.convertFolder, output))
	case len(s.convertPaths) > 0:
		s.sourceEntry.SetText(strings.Join(s.convertPaths, "\n"))
	default:
		s.sourceEntry.SetText("")
	}

	if s.convertFolder != "" || len(s.convertPaths) > 0 {
		s.convertBtn.Enable()
	} else {
		s.convertBtn.Disable()
	}
}

func (s *convertSession) currentConversionJobs() ([]conversionJob, error) {
	videoQuality := s.convertVideoQuality()
	if s.convertFolder != "" {
		jobs, err := conversionJobsForFolder(s.convertFolder, videoQuality)
		if err != nil {
			return nil, err
		}
		if len(jobs) == 0 {
			return nil, fmt.Errorf("no supported video files found in %s", s.convertFolder)
		}
		return jobs, nil
	}

	jobs, err := conversionJobsForFiles(s.convertPaths, videoQuality)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("choose at least one supported video file")
	}
	return jobs, nil
}

func (s *convertSession) runConversion(jobs []conversionJob, videoQuality, audioQuality string) {
	s.setConvertBusy(true)
	defer s.setConvertBusy(false)

	s.run.begin()
	defer s.run.end()

	s.setConversionActive(true)
	defer s.setConversionActive(false)

	env := jobEnv{
		ctx: s.run.context(),
		log: sessionLogWriter{append: s.appendLog},
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

	completed := 0
	failed := 0
	for index, job := range jobs {
		if env.cancelled() {
			break
		}

		s.setConvertStatus(fmt.Sprintf(
			"Converting %s (%d of %d)…",
			filepath.Base(job.Input),
			index+1,
			len(jobs),
		))
		jobPrintf(env, "\nConverting: %s\nOutput: %s\n", job.Input, job.Output)

		err := convertVideo(env, job, videoQuality, audioQuality)
		if err != nil {
			if env.cancelled() {
				break
			}
			failed++
			jobPrintf(env, "Failed: %s\n", err)
		} else {
			completed++
			jobPrintf(env, "Done: %s\n", job.Output)
		}

		progress := float64(index+1) / float64(len(jobs))
		fyne.Do(func() {
			if !s.stillOpen() {
				return
			}
			s.progress.SetValue(progress)
		})
	}

	stopped := env.cancelled()
	message := fmt.Sprintf("Converted %d of %d file(s).", completed, len(jobs))
	if failed > 0 {
		message = fmt.Sprintf("Converted %d of %d file(s); %d failed.", completed, len(jobs), failed)
	}
	if stopped {
		message = fmt.Sprintf("Stopped after converting %d of %d file(s).", completed, len(jobs))
	}

	jobPrintln(env, message)
	s.setConvertStatus(message)
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
		title := "Conversion complete"
		if stopped {
			title = "Conversion stopped"
		}
		if s.app.tabs != nil && s.app.tabs.Selected() == s.tab {
			dialog.ShowInformation(title, message, s.app.win)
		}
	})
}

func (s *convertSession) setConvertBusy(busy bool) {
	s.mu.Lock()
	s.busy = busy
	s.mu.Unlock()

	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		if busy {
			s.convertBtn.Disable()
			s.clearBtn.Disable()
			return
		}

		s.clearBtn.Enable()
		if s.convertFolder != "" || len(s.convertPaths) > 0 {
			s.convertBtn.Enable()
		}
	})
}

func (s *convertSession) clearConversion() {
	if s.isConversionActive() {
		return
	}
	s.convertFolder = ""
	s.convertPaths = nil
	s.sourceEntry.SetText("")
	s.logRaw = ""
	s.logEntry.SetText("")
	s.progress.SetValue(0)
	s.progress.Hide()
	s.convertBtn.Disable()
	s.tabTitle = ""
	s.state = tabIdle
	s.status.SetText("Choose a video file or folder to convert.")
	s.app.refreshTabChrome(s)
}

func (s *convertSession) setConvertStatus(text string) {
	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.status.SetText(text)
	})
}

func (s *convertSession) setConversionActive(active bool) {
	s.mu.Lock()
	s.active = active
	s.mu.Unlock()
}

func (s *convertSession) isConversionActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func (s *convertSession) convertVideoQuality() string {
	if s.videoSel == nil || s.videoSel.Selected == "" {
		return "720p"
	}
	return s.videoSel.Selected
}

func (s *convertSession) convertAudioQuality() string {
	if s.audioSel == nil || s.audioSel.Selected == "" {
		return "128k"
	}
	return s.audioSel.Selected
}

func (s *convertSession) appendLog(chunk string) {
	if !s.stillOpen() {
		return
	}
	s.logRaw += chunk
	s.logEntry.SetText(renderLog(s.logRaw))
}
