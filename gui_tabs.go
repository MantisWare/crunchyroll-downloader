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
	download := g.newDownloadTab()
	convert := g.newConvertTab()
	tabs := container.NewDocTabs(download, convert)
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

	downloadBtn := widget.NewButtonWithIcon("Download", theme.DownloadIcon(), func() {
		g.becomeDownloadTab(item)
	})
	downloadBtn.Importance = widget.HighImportance
	convertBtn := widget.NewButtonWithIcon("Convert", theme.MediaVideoIcon(), func() {
		g.becomeConvertTab(item)
	})

	return container.NewCenter(container.NewVBox(
		hint,
		container.NewGridWithColumns(2, downloadBtn, convertBtn),
	))
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
	g.tabs.Append(g.newDownloadTab())
}

func (g *guiApp) selectedConvert() (*convertSession, bool) {
	if g.tabs == nil {
		return nil, false
	}
	sess, ok := g.sessionFor(g.tabs.Selected()).(*convertSession)
	return sess, ok
}
