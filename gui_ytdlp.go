//go:build gui

package main

import (
	"context"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func (g *guiApp) ytdlpRow() fyne.CanvasObject {
	g.ytdlpLabel = widget.NewLabel("Checking yt-dlp…")
	g.ytdlpLabel.Wrapping = fyne.TextWrapWord
	g.ytdlpCheck = widget.NewButton("Check", func() {
		go g.checkYTDLP()
	})
	g.ytdlpUpdate = widget.NewButton("Update", func() {
		dialog.ShowConfirm("Update yt-dlp?", g.ytdlpUpdatePrompt(), func(ok bool) {
			if ok {
				go g.applyYTDLPUpdate()
			}
		}, g.win)
	})
	g.ytdlpUpdate.Disable()
	return container.NewBorder(nil, nil, nil, container.NewHBox(g.ytdlpCheck, g.ytdlpUpdate), g.ytdlpLabel)
}

func (g *guiApp) ytdlpUpdatePrompt() string {
	g.ytdlpMu.Lock()
	latest := g.ytdlpLatest
	g.ytdlpMu.Unlock()
	if latest == "" {
		return "Update yt-dlp now?"
	}
	return "Update yt-dlp to " + latest + "?"
}

func (g *guiApp) checkYTDLP() {
	if !g.beginYTDLPWork("Checking yt-dlp…") {
		return
	}
	defer g.endYTDLPWork()

	current, currentErr := installedYTDLPVersion()
	if currentErr != nil {
		g.setYTDLPStatus("yt-dlp is not installed.", false, "")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	latest, latestErr := fetchLatestYTDLPVersion(ctx)
	text, canUpdate := ytdlpStatusText(current, latest, latestErr)
	if latestErr != nil {
		latest = ""
	}
	g.setYTDLPStatus(text, canUpdate, latest)
}

func (g *guiApp) applyYTDLPUpdate() {
	if !g.beginYTDLPWork("Updating yt-dlp…") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	_, err := upgradeYTDLP(ctx)
	g.endYTDLPWork()
	if err != nil {
		g.setYTDLPStatus(err.Error(), true, "")
		return
	}
	g.checkYTDLP()
}

func (g *guiApp) beginYTDLPWork(status string) bool {
	g.ytdlpMu.Lock()
	if g.ytdlpBusy {
		g.ytdlpMu.Unlock()
		return false
	}
	g.ytdlpBusy = true
	g.ytdlpMu.Unlock()
	fyne.Do(func() {
		g.ytdlpLabel.SetText(status)
		g.ytdlpCheck.Disable()
		g.ytdlpUpdate.Disable()
	})
	return true
}

func (g *guiApp) endYTDLPWork() {
	g.ytdlpMu.Lock()
	g.ytdlpBusy = false
	g.ytdlpMu.Unlock()
	fyne.Do(func() {
		g.ytdlpCheck.Enable()
	})
}

func (g *guiApp) setYTDLPStatus(text string, canUpdate bool, latest string) {
	g.ytdlpMu.Lock()
	g.ytdlpLatest = latest
	g.ytdlpMu.Unlock()
	fyne.Do(func() {
		g.ytdlpLabel.SetText(text)
		if canUpdate {
			g.ytdlpUpdate.Enable()
			return
		}
		g.ytdlpUpdate.Disable()
	})
}
