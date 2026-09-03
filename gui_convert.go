//go:build gui

package main

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var convertVideoQualities = []string{"1080p", "720p", "480p", "360p"}
var convertAudioQualities = []string{"320k", "256k", "192k", "128k", "96k"}

func (g *guiApp) buildConvert(cfg appConfig) {
	g.convertSourceEntry = widget.NewMultiLineEntry()
	g.convertSourceEntry.Disable()
	g.convertSourceEntry.SetMinRowsVisible(4)
	g.convertSourceEntry.SetPlaceHolder("Drop a video file or folder here, or use one of the buttons below.")

	// The saved quality is applied before the handlers are attached: SetSelected
	// fires OnChanged straight away, and these handlers touch widgets that are
	// still nil this early in the build.
	g.convertVideoSel = widget.NewSelect(convertVideoQualities, nil)
	g.convertVideoSel.SetSelected(pickPreferred(convertVideoQualities, cfg.ConvertVideoQuality))

	g.convertAudioSel = widget.NewSelect(convertAudioQualities, nil)
	g.convertAudioSel.SetSelected(pickPreferred(convertAudioQualities, cfg.ConvertAudioQuality))

	g.convertStatus = widget.NewLabel("Choose a video file or folder to convert.")
	g.convertStatus.Wrapping = fyne.TextWrapWord

	g.convertLogEntry = widget.NewMultiLineEntry()
	g.convertLogEntry.Disable()
	g.convertLogEntry.Wrapping = fyne.TextWrapWord
	g.convertLogEntry.SetPlaceHolder("FFmpeg conversion output appears here…")

	g.convertProgress = widget.NewProgressBar()
	g.convertProgress.Hide()

	g.convertBtn = widget.NewButtonWithIcon("Convert", theme.MediaVideoIcon(), func() {
		jobs, err := g.currentConversionJobs()
		if err != nil {
			g.convertStatus.SetText(err.Error())
			return
		}
		videoQuality := g.convertVideoQuality()
		audioQuality := g.convertAudioQuality()
		go g.runConversion(jobs, videoQuality, audioQuality)
	})
	g.convertBtn.Importance = widget.HighImportance
	g.convertBtn.Disable()

	g.convertStopBtn = widget.NewButtonWithIcon("Stop", theme.CancelIcon(), func() {
		requestCancel()
		g.convertStopBtn.Disable()
		g.convertStatus.SetText("Stopping conversion…")
	})
	g.convertStopBtn.Importance = widget.DangerImportance
	g.convertStopBtn.Disable()

	g.convertClearBtn = widget.NewButtonWithIcon("Clear", theme.ContentClearIcon(), func() {
		g.clearConversion()
	})

	g.convertVideoSel.OnChanged = func(_ string) {
		g.persistSettings()
		// The folder output path carries the quality tag, so it has to be redrawn.
		g.refreshConvertSource()
	}
	g.convertAudioSel.OnChanged = func(_ string) {
		g.persistSettings()
	}
}

func (g *guiApp) convertTabContent() fyne.CanvasObject {
	selectFile := widget.NewButtonWithIcon("Choose file", theme.FileIcon(), func() {
		picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				g.convertStatus.SetText(fmt.Sprintf("Could not open file picker: %s", err))
				return
			}
			if reader == nil {
				return
			}
			path := reader.URI().Path()
			_ = reader.Close()
			g.setConversionFiles([]string{path})
		}, g.win)
		picker.Show()
	})

	selectFolder := widget.NewButtonWithIcon("Choose folder", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				g.convertStatus.SetText(fmt.Sprintf("Could not open folder picker: %s", err))
				return
			}
			if uri == nil {
				return
			}
			g.setConversionFolder(uri.Path())
		}, g.win)
	})

	dropHelp := widget.NewCard(
		"Input",
		"Drop one or more video files, or a folder, anywhere on this tab.",
		container.NewVBox(
			g.convertSourceEntry,
			container.NewHBox(selectFile, selectFolder, g.convertClearBtn),
		),
	)

	options := widget.NewForm(
		widget.NewFormItem("Video quality", g.convertVideoSel),
		widget.NewFormItem("Audio quality", g.convertAudioSel),
	)

	logFloor := canvas.NewRectangle(color.Transparent)
	logFloor.SetMinSize(fyne.NewSize(0, 300))
	logArea := container.NewStack(
		logFloor,
		container.NewBorder(
			widget.NewLabelWithStyle("FFmpeg output", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			nil, nil, nil,
			g.convertLogEntry,
		),
	)

	bottom := container.NewVBox(
		g.convertProgress,
		container.NewBorder(
			nil, nil,
			g.convertStatus,
			container.NewHBox(g.convertStopBtn, g.convertBtn),
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

func (g *guiApp) handleDroppedURIs(uris []fyne.URI) {
	if len(uris) == 0 || g.isConversionActive() {
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
		g.convertStatus.SetText(fmt.Sprintf("Could not read dropped item: %s", err))
		return
	}
	if info.IsDir() {
		g.setConversionFolder(paths[0])
		return
	}
	g.setConversionFiles(paths)
}

func (g *guiApp) setConversionFiles(paths []string) {
	supported := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() && isSupportedVideo(path) {
			supported = append(supported, filepath.Clean(path))
		}
	}

	g.convertFolder = ""
	g.convertPaths = supported
	g.refreshConvertSource()
	if len(supported) == 0 {
		g.convertStatus.SetText("No supported video files were selected.")
		return
	}
	g.convertStatus.SetText(fmt.Sprintf("%d file(s) ready to convert.", len(supported)))
}

func (g *guiApp) setConversionFolder(path string) {
	g.convertFolder = filepath.Clean(path)
	g.convertPaths = nil
	g.refreshConvertSource()

	jobs, err := g.currentConversionJobs()
	if err != nil {
		g.convertStatus.SetText(err.Error())
		return
	}
	g.convertStatus.SetText(fmt.Sprintf("%d video file(s) found in the folder.", len(jobs)))
}

func (g *guiApp) refreshConvertSource() {
	switch {
	case g.convertFolder != "":
		output := filepath.Join(g.convertFolder, "converted_"+g.convertVideoQuality())
		g.convertSourceEntry.SetText(fmt.Sprintf("Folder: %s\nOutput: %s", g.convertFolder, output))
	case len(g.convertPaths) > 0:
		g.convertSourceEntry.SetText(strings.Join(g.convertPaths, "\n"))
	default:
		g.convertSourceEntry.SetText("")
	}

	if g.convertFolder != "" || len(g.convertPaths) > 0 {
		g.convertBtn.Enable()
	} else {
		g.convertBtn.Disable()
	}
}

func (g *guiApp) currentConversionJobs() ([]conversionJob, error) {
	videoQuality := g.convertVideoQuality()
	if g.convertFolder != "" {
		jobs, err := conversionJobsForFolder(g.convertFolder, videoQuality)
		if err != nil {
			return nil, err
		}
		if len(jobs) == 0 {
			return nil, fmt.Errorf("no supported video files found in %s", g.convertFolder)
		}
		return jobs, nil
	}

	jobs, err := conversionJobsForFiles(g.convertPaths, videoQuality)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("choose at least one supported video file")
	}
	return jobs, nil
}

func (g *guiApp) runConversion(jobs []conversionJob, videoQuality, audioQuality string) {
	g.setConvertBusy(true)
	defer g.setConvertBusy(false)

	beginCancellable()
	defer endCancellable()

	g.setConversionActive(true)
	defer g.setConversionActive(false)

	fyne.Do(func() {
		g.convertLogRaw = ""
		g.convertLogEntry.SetText("")
		g.convertProgress.SetValue(0)
		g.convertProgress.Show()
		g.convertStopBtn.Enable()
	})

	completed := 0
	failed := 0
	for index, job := range jobs {
		if isCancelled() {
			break
		}

		g.setConvertStatus(fmt.Sprintf(
			"Converting %s (%d of %d)…",
			filepath.Base(job.Input),
			index+1,
			len(jobs),
		))
		fmt.Printf("\nConverting: %s\nOutput: %s\n", job.Input, job.Output)

		err := convertVideo(job, videoQuality, audioQuality)
		if err != nil {
			if isCancelled() {
				break
			}
			failed++
			fmt.Printf("Failed: %s\n", err)
		} else {
			completed++
			fmt.Printf("Done: %s\n", job.Output)
		}

		progress := float64(index+1) / float64(len(jobs))
		fyne.Do(func() {
			g.convertProgress.SetValue(progress)
		})
	}

	stopped := isCancelled()
	message := fmt.Sprintf("Converted %d of %d file(s).", completed, len(jobs))
	if failed > 0 {
		message = fmt.Sprintf("Converted %d of %d file(s); %d failed.", completed, len(jobs), failed)
	}
	if stopped {
		message = fmt.Sprintf("Stopped after converting %d of %d file(s).", completed, len(jobs))
	}

	fmt.Println(message)
	g.setConvertStatus(message)
	fyne.Do(func() {
		g.convertStopBtn.Disable()
		title := "Conversion complete"
		if stopped {
			title = "Conversion stopped"
		}
		dialog.ShowInformation(title, message, g.win)
	})
}

func (g *guiApp) setConvertBusy(busy bool) {
	fyne.Do(func() {
		if busy {
			g.convertBtn.Disable()
			g.convertClearBtn.Disable()
			g.lookupBtn.Disable()
			g.dlBtn.Disable()
			g.resetBtn.Disable()
			return
		}

		g.convertClearBtn.Enable()
		g.resetBtn.Enable()
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

func (g *guiApp) clearConversion() {
	if g.isConversionActive() {
		return
	}
	g.convertFolder = ""
	g.convertPaths = nil
	g.convertSourceEntry.SetText("")
	g.convertLogRaw = ""
	g.convertLogEntry.SetText("")
	g.convertProgress.SetValue(0)
	g.convertProgress.Hide()
	g.convertBtn.Disable()
	g.convertStatus.SetText("Choose a video file or folder to convert.")
}

func (g *guiApp) setConvertStatus(text string) {
	fyne.Do(func() {
		g.convertStatus.SetText(text)
	})
}

func (g *guiApp) setConversionActive(active bool) {
	g.convertMu.Lock()
	g.convertActive = active
	g.convertMu.Unlock()
}

func (g *guiApp) isConversionActive() bool {
	g.convertMu.Lock()
	defer g.convertMu.Unlock()
	return g.convertActive
}

func (g *guiApp) convertVideoQuality() string {
	if g.convertVideoSel == nil || g.convertVideoSel.Selected == "" {
		return "720p"
	}
	return g.convertVideoSel.Selected
}

func (g *guiApp) convertAudioQuality() string {
	if g.convertAudioSel == nil || g.convertAudioSel.Selected == "" {
		return "128k"
	}
	return g.convertAudioSel.Selected
}
