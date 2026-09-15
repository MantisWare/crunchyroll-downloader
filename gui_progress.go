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

func (s *downloadSession) beginDownload(total int) {
	s.progressMu.Lock()
	s.dlActive = true
	s.dlTotal = total
	s.dlDone = 0
	s.dlPhase = 0
	s.dlPhasePct = 0
	s.progressMu.Unlock()

	fyne.Do(func() {
		if !s.stillOpen() {
			return
		}
		s.progress.Show()
		s.applyProgress()
	})
}

func (s *downloadSession) startEpisodeProgress() {
	s.progressMu.Lock()
	s.dlPhase = 0
	s.dlPhasePct = 0
	s.progressMu.Unlock()

	fyne.Do(s.applyProgress)
}

func (s *downloadSession) completeEpisodeProgress() {
	s.progressMu.Lock()
	if s.dlDone < s.dlTotal {
		s.dlDone++
	}
	s.dlPhase = 0
	s.dlPhasePct = 0
	s.progressMu.Unlock()

	fyne.Do(s.applyProgress)
}

func (s *downloadSession) endDownload() {
	s.progressMu.Lock()
	s.dlActive = false
	s.progressMu.Unlock()

	fyne.Do(func() {
		s.applyProgress()
	})
}

func (s *downloadSession) hideProgress() {
	s.progressMu.Lock()
	s.dlActive = false
	s.dlTotal = 0
	s.dlDone = 0
	s.dlPhase = 0
	s.dlPhasePct = 0
	s.progressMu.Unlock()

	s.progress.SetValue(0)
	s.progress.Hide()
}

func (s *downloadSession) noteLogProgress(chunk string) {
	s.progressMu.Lock()
	active := s.dlActive
	s.progressMu.Unlock()
	if !active {
		return
	}

	changed := false

	if matches := segmentProgressPattern.FindAllStringSubmatch(chunk, -1); len(matches) > 0 {
		last := matches[len(matches)-1]
		done, doneErr := strconv.ParseFloat(last[1], 64)
		total, totalErr := strconv.ParseFloat(last[2], 64)
		if doneErr == nil && totalErr == nil && total > 0 {
			s.progressMu.Lock()
			s.dlPhasePct = done / total
			s.progressMu.Unlock()
			changed = true
		}
	}

	if finished := strings.Count(chunk, "Finished downloading!"); finished > 0 {
		s.progressMu.Lock()
		s.dlPhase += finished
		if s.dlPhase > phasesPerEpisode {
			s.dlPhase = phasesPerEpisode
		}
		s.dlPhasePct = 0
		s.progressMu.Unlock()
		changed = true
	}

	if changed {
		s.applyProgress()
	}
}

func (s *downloadSession) applyProgress() {
	if !s.stillOpen() {
		return
	}
	s.progress.SetValue(s.progressFraction())
	s.progress.Refresh()
}

func (s *downloadSession) progressFraction() float64 {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()

	if s.dlTotal <= 0 {
		return 0
	}
	if s.dlDone >= s.dlTotal {
		return 1
	}

	phase := float64(s.dlPhase) + s.dlPhasePct
	episodeFraction := phase / float64(phasesPerEpisode)
	if episodeFraction > 0.99 {
		episodeFraction = 0.99
	}
	if episodeFraction < 0 {
		episodeFraction = 0
	}

	return (float64(s.dlDone) + episodeFraction) / float64(s.dlTotal)
}

func (s *downloadSession) progressText() string {
	fraction := s.progressFraction()

	s.progressMu.Lock()
	total := s.dlTotal
	done := s.dlDone
	s.progressMu.Unlock()

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
