//go:build gui

package main

import (
	"context"
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (g *guiApp) startOrCancelBrowserLogin() {
	g.browserLoginMu.Lock()
	if g.browserLoginCancel != nil {
		g.browserLoginCancel()
		g.browserLoginMu.Unlock()
		g.setAccountStatus("Cancelling browser sign-in…")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), browserLoginTimeout)
	g.browserLoginCancel = cancel
	g.browserLoginMu.Unlock()

	g.setAccountStatus("Complete the sign-in in the browser window. The app will continue automatically.")
	fyne.Do(func() {
		g.signInBtn.SetText("Cancel sign-in")
		g.signInBtn.SetIcon(theme.CancelIcon())
		g.signInBtn.Importance = widget.DangerImportance
		g.signInBtn.Refresh()
	})

	go g.runBrowserLogin(ctx, cancel)
}

func (g *guiApp) runBrowserLogin(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()

	cookie, err := loginWithBrowser(ctx)
	if err == nil {
		err = loginWithCookie(cookie)
	}

	g.browserLoginMu.Lock()
	g.browserLoginCancel = nil
	g.browserLoginMu.Unlock()

	fyne.Do(func() {
		g.signInBtn.SetText("Sign in with browser")
		g.signInBtn.SetIcon(theme.LoginIcon())
		g.signInBtn.Importance = widget.MediumImportance
		g.signInBtn.Refresh()

		if err == nil {
			g.etpEntry.SetText(cookie)
			g.persistSettings()
			g.applyAccountStatus("Signed in successfully. Paste a Crunchyroll URL, then click Lookup.")
			return
		}

		switch {
		case errors.Is(err, context.Canceled):
			g.applyAccountStatus("Browser sign-in cancelled.")
		case errors.Is(err, context.DeadlineExceeded):
			g.applyAccountStatus("Browser sign-in timed out after 10 minutes. Please try again.")
		case errors.Is(err, errBrowserClosed):
			g.applyAccountStatus("Browser closed before sign-in completed. Please try again.")
		default:
			g.applyAccountStatus(fmt.Sprintf("Browser sign-in failed: %s", err))
		}
	})
}

func (g *guiApp) cancelBrowserLogin() {
	g.browserLoginMu.Lock()
	cancel := g.browserLoginCancel
	g.browserLoginMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
