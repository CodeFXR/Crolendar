package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderLogo() string {
	text := "/// CROLENDAR ///"
	colors := []string{
		"#7D56F4", "#745AF2", "#6B5EF0", "#6262EE", "#5966EC",
		"#506AEA", "#476EE8", "#3E72E6", "#3576E4", "#2C7AE2",
	}
	var grad strings.Builder
	for i, c := range text {
		idx := i % len(colors)
		grad.WriteString(
			lipgloss.NewStyle().
				Foreground(lipgloss.Color(colors[idx])).
				Bold(true).
				Render(string(c)),
		)
	}
	return lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(3).
		Align(lipgloss.Center, lipgloss.Center).
		Render(grad.String())
}

func renderSidebar(m model, bodyHeight int) string {
	logo := renderLogo()

	viewsHeader := lipgloss.NewStyle().
		Width(sidebarWidth).
		Align(lipgloss.Center).
		Foreground(mutedGray).
		Bold(true).
		Render("VIEWS")
	separator := lipgloss.NewStyle().
		Width(sidebarWidth).
		Align(lipgloss.Center).
		Foreground(borderGray).
		Render(strings.Repeat("─", 14))

	type navItem struct {
		name string
		icon string
		key  string
	}
	items := []navItem{
		{name: "Agenda", icon: "≡", key: "1"},
		{name: "Month", icon: "◈", key: "2"},
		{name: "Week", icon: "▤", key: "3"},
		{name: "Day", icon: "○", key: "4"},
		{name: "Settings", icon: "◇", key: "5"},
	}

	var lines []string
	for i, item := range items {
		style := navItemStyle
		prefix := "  "
		if int(m.state) == i {
			prefix = "• "
			if !m.focusGrid {
				style = selectedNavFocusStyle
			} else {
				style = selectedNavBlurStyle
			}
		}
		label := fmt.Sprintf("%s%s %-9s %s", prefix, item.icon, item.name, mutedTextStyle.Render(item.key))
		lines = append(lines, style.Render(label))
	}

	navBlock := lipgloss.JoinVertical(lipgloss.Left, lines...)
	centeredNav := lipgloss.NewStyle().
		Width(sidebarWidth).
		PaddingLeft(2).
		Render(navBlock)

	topSection := lipgloss.JoinVertical(lipgloss.Left, logo, viewsHeader, separator, "", centeredNav)

	borderColor := borderGray
	if !m.focusGrid {
		borderColor = purple
	}

	return sidebarStyle.
		BorderForeground(borderColor).
		Width(sidebarWidth).
		Height(bodyHeight).
		Align(lipgloss.Left, lipgloss.Top).
		Render(topSection)
}
