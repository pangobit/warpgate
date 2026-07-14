package ssh

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

const (
	colorSuccess      = "#04B575"
	colorDanger       = "#FF4672"
	colorMuted        = "#606060"
	colorWarningLight = "#c78b31"
	colorWarningDark  = "#e6bc72"
)

// uiStyles holds lipgloss styles for the operator TUI.
type uiStyles struct {
	// Title styles the main heading.
	Title lipgloss.Style
	// Header styles section headings.
	Header lipgloss.Style
	// Dim styles secondary text.
	Dim lipgloss.Style
	// Err styles error text.
	Err lipgloss.Style
	// OK styles success text.
	OK lipgloss.Style
	// Notice styles warning and in-progress text.
	Notice lipgloss.Style
	// Confirm styles confirmation prompts.
	Confirm lipgloss.Style
}

func newUIStyles(isDark bool) uiStyles {
	warning := warningColor(isDark)
	return uiStyles{
		Title:   lipgloss.NewStyle().Bold(true),
		Header:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSuccess)),
		Dim:     lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)),
		Err:     lipgloss.NewStyle().Foreground(lipgloss.Color(colorDanger)),
		OK:      lipgloss.NewStyle().Foreground(lipgloss.Color(colorSuccess)),
		Notice:  lipgloss.NewStyle().Foreground(warning),
		Confirm: lipgloss.NewStyle().Bold(true).Foreground(warning),
	}
}

func warningColor(isDark bool) color.Color {
	lightDark := lipgloss.LightDark(isDark)
	return lightDark(lipgloss.Color(colorWarningLight), lipgloss.Color(colorWarningDark))
}
