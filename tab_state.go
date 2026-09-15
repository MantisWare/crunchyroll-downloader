package main

import "fmt"

type tabKind int

const (
	tabKindChooser tabKind = iota
	tabKindDownload
	tabKindConvert
)

type tabRunState int

const (
	tabIdle tabRunState = iota
	tabRunning
	tabComplete
	tabStopped
)

func tabRunIconName(state tabRunState, kind tabKind) string {
	switch state {
	case tabRunning:
		return "viewRefresh"
	case tabComplete:
		return "confirm"
	case tabStopped:
		return "cancel"
	default:
		if kind == tabKindConvert {
			return "mediaVideo"
		}
		return "download"
	}
}

func numberedSessionTitle(kind tabKind, seq int) string {
	base := "Download"
	if kind == tabKindConvert {
		base = "Convert"
	}
	if seq <= 1 {
		return base
	}
	return fmt.Sprintf("%s %d", base, seq)
}

func uniqueTitle(desired string, existing []string) string {
	used := make(map[string]bool, len(existing))
	for _, title := range existing {
		used[title] = true
	}
	if !used[desired] {
		return desired
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)", desired, n)
		if !used[candidate] {
			return candidate
		}
	}
}

func truncateTitle(s string, max int) string {
	runes := []rune(s)
	if max <= 0 || len(runes) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}
