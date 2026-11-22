package main

import "github.com/charmbracelet/lipgloss"

var (
	purple = lipgloss.Color("#7D56F4")
	blue   = lipgloss.Color("#0052D4")
	white  = lipgloss.Color("#FFFFFF")
	gray   = lipgloss.Color("#3C3C3C")

	navItemStyle         = lipgloss.NewStyle().Foreground(white)
	selectedNavItemStyle = navItemStyle.Copy().Foreground(purple).Bold(true)

	sidebarStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder(), false, true, false, false).
	BorderForeground(gray).
	Padding(0)

	mainPanelStyle = lipgloss.NewStyle().Padding(0)
)
