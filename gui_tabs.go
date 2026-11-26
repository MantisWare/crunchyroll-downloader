//go:build gui

package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func tabRunIcon(state tabRunState, kind tabKind) fyne.Resource {
	switch tabRunIconName(state, kind) {
	case "viewRefresh":
		return theme.ViewRefreshIcon()
	case "confirm":
		return theme.ConfirmIcon()
	case "cancel":
		return theme.CancelIcon()
	case "mediaVideo":
		return theme.MediaVideoIcon()
	case "mediaPlay":
		return theme.MediaPlayIcon()
	default:
		return theme.DownloadIcon()
	}
}

type chooserSession struct {
	app *guiApp
	tab *container.TabItem
}

func (c *chooserSession) kind() tabKind               { return tabKindChooser }
func (c *chooserSession) tabItem() *container.TabItem { return c.tab }
func (c *chooserSession) isBusy() bool                { return false }
func (c *chooserSession) cancel()                     {}
func (c *chooserSession) runState() tabRunState       { return tabIdle }
func (c *chooserSession) title() string               { return "New" }

func (g *guiApp) buildTabs() *container.DocTabs {
	tabs := container.NewDocTabs(g.newChooserTab())
	tabs.CreateTab = func() *container.TabItem {
		return g.newChooserTab()
	}
	tabs.CloseIntercept = g.closeTab
	g.tabs = tabs
	return tabs
}

func (g *guiApp) bind(item *container.TabItem, sess tabSession) {
	g.sessions[item] = sess
}

func (g *guiApp) unbind(item *container.TabItem) {
	delete(g.sessions, item)
}

func (g *guiApp) sessionFor(item *container.TabItem) tabSession {
	if item == nil {
		return nil
	}
	return g.sessions[item]
}

func (g *guiApp) otherTitles(self *container.TabItem) []string {
	titles := make([]string, 0, len(g.sessions))
	for item, sess := range g.sessions {
		if item == self || sess == nil {
			continue
		}
		titles = append(titles, sess.title())
	}
	return titles
}

func (g *guiApp) refreshTabChrome(sess tabSession) {
	if sess == nil {
		return
	}
	item := sess.tabItem()
	if item == nil {
		return
	}
	item.Text = sess.title()
	item.Icon = tabRunIcon(sess.runState(), sess.kind())
	if g.tabs != nil {
		g.tabs.Refresh()
	}
}

func (g *guiApp) newDownloadTab() *container.TabItem {
	s := g.newDownloadSession()
	item := container.NewTabItemWithIcon(s.title(), tabRunIcon(tabIdle, tabKindDownload), s.content())
	s.tab = item
	g.bind(item, s)
	return item
}

func (g *guiApp) newConvertTab() *container.TabItem {
	s := g.newConvertSession()
	item := container.NewTabItemWithIcon(s.title(), tabRunIcon(tabIdle, tabKindConvert), s.content())
	s.tab = item
	g.bind(item, s)
	return item
}

func (g *guiApp) newChooserTab() *container.TabItem {
	item := container.NewTabItemWithIcon("New", theme.ContentAddIcon(), widget.NewLabel(""))
	c := &chooserSession{app: g, tab: item}
	item.Content = g.chooserContent(item)
	g.bind(item, c)
	return item
}

func (g *guiApp) chooserContent(item *container.TabItem) fyne.CanvasObject {
	hint := widget.NewLabel("What should this tab do?")
	hint.Alignment = fyne.TextAlignCenter

	download := chooserChoice(
		"Download",
		"Crunchyroll episodes and seasons.",
		theme.DownloadIcon(),
		widget.HighImportance,
		func() { g.becomeDownloadTab(item) },
	)
	video := chooserChoice(
		"Video download",
		"YouTube videos and playlists, and other video pages.",
		theme.MediaPlayIcon(),
		widget.MediumImportance,
		func() { g.becomeYouTubeTab(item) },
	)
	convert := chooserChoice(
		"Convert",
		"Local video files, or every video in a folder.",
		theme.MediaVideoIcon(),
		widget.MediumImportance,
		func() { g.becomeConvertTab(item) },
	)

	return container.NewCenter(container.NewVBox(
		hint,
		container.NewGridWithColumns(3, download, video, convert),
	))
}

func chooserChoice(label, description string, icon fyne.Resource, importance widget.Importance, tapped func()) fyne.CanvasObject {
	button := widget.NewButtonWithIcon(label, icon, tapped)
	button.Importance = importance
	text := widget.NewLabel(description)
	text.Wrapping = fyne.TextWrapWord
	text.Alignment = fyne.TextAlignCenter
	return container.NewVBox(button, text)
}

func (g *guiApp) becomeDownloadTab(item *container.TabItem) {
	g.unbind(item)
	s := g.newDownloadSession()
	s.tab = item
	item.Text = s.title()
	item.Icon = tabRunIcon(tabIdle, tabKindDownload)
	item.Content = s.content()
	g.bind(item, s)
	if g.tabs != nil {
		g.tabs.Refresh()
		g.tabs.Select(item)
	}
}

func (g *guiApp) becomeYouTubeTab(item *container.TabItem) {
	g.unbind(item)
	s := g.newYouTubeSession()
	s.tab = item
	item.Text = s.title()
	item.Icon = tabRunIcon(tabIdle, tabKindYouTube)
	item.Content = s.content()
	g.bind(item, s)
	if g.tabs != nil {
		g.tabs.Refresh()
		g.tabs.Select(item)
	}
}

func (g *guiApp) becomeConvertTab(item *container.TabItem) {
	g.unbind(item)
	s := g.newConvertSession()
	s.tab = item
	item.Text = s.title()
	item.Icon = tabRunIcon(tabIdle, tabKindConvert)
	item.Content = s.content()
	g.bind(item, s)
	if g.tabs != nil {
		g.tabs.Refresh()
		g.tabs.Select(item)
	}
}

func (g *guiApp) closeTab(item *container.TabItem) {
	sess := g.sessionFor(item)
	finish := func() {
		if sess != nil {
			sess.cancel()
		}
		g.unbind(item)
		g.tabs.Remove(item)
		g.ensureTabExists()
	}

	if sess != nil && sess.isBusy() {
		dialog.ShowConfirm(
			"Stop and close?",
			"This tab still has a job running. Stop it and close the tab?",
			func(ok bool) {
				if ok {
					finish()
				}
			},
			g.win,
		)
		return
	}
	finish()
}

func (g *guiApp) ensureTabExists() {
	if g.tabs == nil {
		return
	}
	if len(g.tabs.Items) > 0 {
		return
	}
	g.tabs.Append(g.newChooserTab())
}

func (g *guiApp) selectedConvert() (*convertSession, bool) {
	if g.tabs == nil {
		return nil, false
	}
	sess, ok := g.sessionFor(g.tabs.Selected()).(*convertSession)
	return sess, ok
}
