//go:build gui

package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
)

// segmentProgressPattern matches the per-segment counter the downloader writes
// to stdout, e.g. "Downloaded 120 of 480 segments (25%)".
var segmentProgressPattern = regexp.MustCompile(`Downloaded (\d+) of (\d+) segments`)

// phasesPerEpisode is how many download passes each episode makes (video, then
// audio). Used to spread a single episode across its slice of the bar.
const phasesPerEpisode = 2

// beginDownload arms the progress bar for a run of total episodes.
func (g *guiApp) beginDownload(total int) {
	g.progressMu.Lock()
	g.dlActive = true
	g.dlTotal = total
	g.dlDone = 0
	g.dlPhase = 0
	g.dlPhasePct = 0
	g.progressMu.Unlock()

	fyne.Do(func() {
		g.progress.Show()
		g.applyProgress()
	})
}

// startEpisodeProgress resets the per-episode phase tracking.
func (g *guiApp) startEpisodeProgress() {
	g.progressMu.Lock()
	g.dlPhase = 0
	g.dlPhasePct = 0
	g.progressMu.Unlock()

	fyne.Do(g.applyProgress)
}

// completeEpisodeProgress advances the bar by one whole episode, whether it
// downloaded or was skipped, so the bar always reaches the end of the run.
func (g *guiApp) completeEpisodeProgress() {
	g.progressMu.Lock()
	if g.dlDone < g.dlTotal {
		g.dlDone++
	}
	g.dlPhase = 0
	g.dlPhasePct = 0
	g.progressMu.Unlock()

	fyne.Do(g.applyProgress)
}

func (g *guiApp) endDownload() {
	g.progressMu.Lock()
	g.dlActive = false
	g.progressMu.Unlock()

	fyne.Do(func() {
		g.applyProgress()
	})
}

func (g *guiApp) hideProgress() {
	g.progressMu.Lock()
	g.dlActive = false
	g.dlTotal = 0
	g.dlDone = 0
	g.dlPhase = 0
	g.dlPhasePct = 0
	g.progressMu.Unlock()

	g.progress.SetValue(0)
	g.progress.Hide()
}

// noteLogProgress reads download progress out of the captured stdout stream.
// Runs on the main goroutine, called from the log pump.
func (g *guiApp) noteLogProgress(chunk string) {
	g.progressMu.Lock()
	active := g.dlActive
	g.progressMu.Unlock()
	if !active {
		return
	}

	changed := false

	if matches := segmentProgressPattern.FindAllStringSubmatch(chunk, -1); len(matches) > 0 {
		last := matches[len(matches)-1]
		done, doneErr := strconv.ParseFloat(last[1], 64)
		total, totalErr := strconv.ParseFloat(last[2], 64)
		if doneErr == nil && totalErr == nil && total > 0 {
			g.progressMu.Lock()
			g.dlPhasePct = done / total
			g.progressMu.Unlock()
			changed = true
		}
	}

	// Each "Finished downloading!" ends one pass (video, then audio).
	if finished := strings.Count(chunk, "Finished downloading!"); finished > 0 {
		g.progressMu.Lock()
		g.dlPhase += finished
		if g.dlPhase > phasesPerEpisode {
			g.dlPhase = phasesPerEpisode
		}
		g.dlPhasePct = 0
		g.progressMu.Unlock()
		changed = true
	}

	if changed {
		g.applyProgress()
	}
}

// applyProgress pushes the tracked counters onto the bar. Main goroutine only.
func (g *guiApp) applyProgress() {
	g.progress.SetValue(g.progressFraction())
	g.progress.Refresh()
}

func (g *guiApp) progressFraction() float64 {
	g.progressMu.Lock()
	defer g.progressMu.Unlock()

	if g.dlTotal <= 0 {
		return 0
	}
	if g.dlDone >= g.dlTotal {
		return 1
	}

	phase := float64(g.dlPhase) + g.dlPhasePct
	episodeFraction := phase / float64(phasesPerEpisode)
	if episodeFraction > 0.99 {
		// Hold just short of the next episode until it actually finishes.
		episodeFraction = 0.99
	}
	if episodeFraction < 0 {
		episodeFraction = 0
	}

	return (float64(g.dlDone) + episodeFraction) / float64(g.dlTotal)
}

// progressText labels the bar with the episode count behind the percentage.
func (g *guiApp) progressText() string {
	fraction := g.progressFraction()

	g.progressMu.Lock()
	total := g.dlTotal
	done := g.dlDone
	g.progressMu.Unlock()

	if total <= 0 {
		return ""
	}

	current := done + 1
	if current > total {
		current = total
	}
	if done >= total {
		return fmt.Sprintf("%d of %d episodes — done", total, total)
	}
	return fmt.Sprintf("Episode %d of %d — %.0f%%", current, total, fraction*100)
}
