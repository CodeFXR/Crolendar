package main

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
)

func renderSettingsView(m model) string {
	// Header
	header := lipgloss.NewStyle().
		Width(m.width - 22).
		Align(lipgloss.Center).
		Foreground(blue).
		Bold(true).
		PaddingTop(2).
		PaddingBottom(1).
		Render("Settings: Job Configuration")

	var rows []string

	if len(m.jobs) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(gray).Render("No cron jobs found."))
	}

	for i, job := range m.jobs {
		isSelected := m.settingsCursor == i
		
		alias, color := m.config.GetJobDisplay(job.Command)
		
		// Item Style
		style := lipgloss.NewStyle().PaddingLeft(2).Foreground(white)
		prefix := "  "
		if isSelected {
			style = style.Bold(true).Foreground(purple)
			prefix = "> "
		}

		// Render line
		line := fmt.Sprintf("%s%s (%s)", prefix, alias, job.Schedule)
		rows = append(rows, style.Render(line))

		// If selected, show details/edit mode
		if isSelected {
			detailStyle := lipgloss.NewStyle().PaddingLeft(6).Foreground(gray)
			
			// Alias Row
			aliasRow := fmt.Sprintf("Alias: %s", alias)
			if m.settingsState == 1 { // Editing Alias
				aliasRow = fmt.Sprintf("Alias: %s", m.input.View())
			}
			rows = append(rows, detailStyle.Render(aliasRow))

			// Color Row
			colorRow := fmt.Sprintf("Color: %s", color)
			// Show color preview
			preview := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("■")
			
			if m.settingsState == 2 { // Editing Color
				colorRow = fmt.Sprintf("Color: %s", m.input.View())
			} else {
				colorRow += " " + preview
			}
			rows = append(rows, detailStyle.Render(colorRow))
			
			// Instructions
			if m.settingsState == 0 {
				rows = append(rows, lipgloss.NewStyle().PaddingLeft(6).Foreground(gray).Faint(true).Render("[Enter] Edit  [j/k] Navigate"))
			} else {
				rows = append(rows, lipgloss.NewStyle().PaddingLeft(6).Foreground(gray).Faint(true).Render("[Enter] Save  [Esc] Cancel"))
			}
			
			rows = append(rows, "") // spacing
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)

	return lipgloss.JoinVertical(lipgloss.Top, header, content)
}
