// Package tui provides TUI components built on Charmbracelet v2, including
// a step-runner for bootstrap progress and a live cluster dashboard.
package tui

import "charm.land/lipgloss/v2"

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).MarginBottom(1)
	doneIcon     = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Render("✓")
	failIcon     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4672")).Render("✗")
	pendingIcon  = lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")).Render(" ")
	stepStyle    = lipgloss.NewStyle()
	runningStyle = lipgloss.NewStyle().Bold(true)
	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#606060"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4672")).MarginLeft(4)
	detailStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#606060"))
)
