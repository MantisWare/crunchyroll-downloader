//go:build gui

package main

import (
	"os"
	"strings"

	"fyne.io/fyne/v2"
)

func (g *guiApp) startLogCapture() {
	reader, writer, err := os.Pipe()
	if err != nil {
		return
	}
	os.Stdout = writer

	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				fyne.Do(func() {
					g.appendLog(chunk)
				})
			}
			if readErr != nil {
				return
			}
		}
	}()
}

func (g *guiApp) appendLog(chunk string) {
	g.logRaw += chunk
	text := normalizeCarriageReturns(g.logRaw)
	g.logEntry.SetText(text)
	g.logEntry.CursorRow = strings.Count(text, "\n")
	g.logEntry.Refresh()
}

func normalizeCarriageReturns(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if idx := strings.LastIndex(line, "\r"); idx >= 0 {
			lines[i] = line[idx+1:]
		}
	}
	return strings.Join(lines, "\n")
}
