package main

import (
	"strings"
	"github.com/charmbracelet/lipgloss"
)

func renderLogo() string {
	text := "/// CROLENDAR ///"
	colors := []string{"#7D56F4", "#745AF2", "#6B5EF0", "#6262EE", "#5966EC", "#506AEA", "#476EE8", "#3E72E6", "#3576E4", "#2C7AE2"}
	var grad string
	for i, c := range text {
		idx := i % len(colors)
		grad += lipgloss.NewStyle().Foreground(lipgloss.Color(colors[idx])).Bold(true).Render(string(c))
	}
	return lipgloss.NewStyle().Width(20).Height(4).Align(lipgloss.Center, lipgloss.Center).Render(grad)
}

func renderSidebar(m model) string {
	logo := renderLogo()
	sidebarWidth := 20

	viewsHeader := lipgloss.NewStyle().Width(sidebarWidth).Align(lipgloss.Center).Foreground(gray).Bold(true).Render("VIEWS")
	separator := lipgloss.NewStyle().Width(sidebarWidth).Align(lipgloss.Center).Foreground(gray).Render(strings.Repeat("─", 14))

	type navItem struct{ name, icon string }
	items := []navItem{{"Agenda", "≣"}, {"Month", "◈"}, {"Week", "▤"}, {"Day", "☉"}, {"Settings", "⚙"}}

	fixedPadding := (sidebarWidth - 12) / 2
	var lines []string
	for i, item := range items {
		style := navItemStyle
		prefix := "  "
		if int(m.state) == i {
			style = selectedNavItemStyle
			prefix = "• "
			if m.focusGrid { style = style.Copy().Foreground(gray) }
		}
		lines = append(lines, style.Render(prefix+item.icon+" "+item.name))
	}

	navBlock := lipgloss.JoinVertical(lipgloss.Left, lines...)
	centeredNav := lipgloss.NewStyle().Width(sidebarWidth).PaddingLeft(fixedPadding).Render(navBlock)

	return sidebarStyle.
		Width(20).
		Height(m.height).
		Align(lipgloss.Left, lipgloss.Top). // Explicit Top Alignment
		Render(
		lipgloss.JoinVertical(lipgloss.Left, logo, viewsHeader, separator, "\n", centeredNav),
	)
}