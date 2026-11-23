package main

import "github.com/charmbracelet/lipgloss"

var (
	purple     = lipgloss.Color("#7D56F4")
	blue       = lipgloss.Color("#0052D4")
	white      = lipgloss.Color("#FFFFFF")
	softWhite  = lipgloss.Color("#E2E8F0")
	mutedGray  = lipgloss.Color("#7A828E")
	borderGray = lipgloss.Color("#3C3C3C")
	surfaceBg  = lipgloss.Color("#1A1E29")
	errorRed   = lipgloss.Color("#F87171")
	okGreen    = lipgloss.Color("#34D399")

	navItemStyle          = lipgloss.NewStyle().Foreground(softWhite)
	selectedNavFocusStyle = lipgloss.NewStyle().Foreground(purple).Bold(true)
	selectedNavBlurStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Bold(true)

	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(borderGray).
			Padding(0)

	mainPanelStyle = lipgloss.NewStyle().Padding(0)

	footerStyle = lipgloss.NewStyle().
			Foreground(mutedGray).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(borderGray).
			Padding(0, 1)

	viewTitleStyle = lipgloss.NewStyle().
			Align(lipgloss.Center).
			Foreground(blue).
			Bold(true)

	mutedTextStyle = lipgloss.NewStyle().Foreground(mutedGray)
	errorTextStyle = lipgloss.NewStyle().Foreground(errorRed)
	okTextStyle    = lipgloss.NewStyle().Foreground(okGreen)
)
