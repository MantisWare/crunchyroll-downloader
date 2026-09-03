//go:build gui

package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type crunchyTheme struct{}

func (t crunchyTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary, theme.ColorNameFocus:
		return color.NRGBA{R: 244, G: 117, B: 33, A: 255}
	case theme.ColorNameBackground:
		return color.NRGBA{R: 14, G: 14, B: 16, A: 255}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 245, G: 245, B: 247, A: 255}
	case theme.ColorNameButton:
		return color.NRGBA{R: 36, G: 36, B: 42, A: 255}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 28, G: 28, B: 32, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 120, G: 120, B: 128, A: 255}
	case theme.ColorNameHover:
		return color.NRGBA{R: 56, G: 42, B: 30, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 26, G: 26, B: 30, A: 255}
	case theme.ColorNameInputBorder:
		return color.NRGBA{R: 58, G: 58, B: 64, A: 255}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 140, G: 140, B: 148, A: 255}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 244, G: 117, B: 33, A: 70}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 48, G: 48, B: 54, A: 255}
	case theme.ColorNameShadow:
		return color.NRGBA{A: 90}
	case theme.ColorNameOverlayBackground, theme.ColorNameMenuBackground:
		return color.NRGBA{R: 22, G: 22, B: 26, A: 255}
	}
	return theme.DefaultTheme().Color(name, theme.VariantDark)
}

func (t crunchyTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t crunchyTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t crunchyTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 22
	}
	return theme.DefaultTheme().Size(name)
}
