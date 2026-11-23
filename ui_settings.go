package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

func renderSettingsView(m model, availWidth, bodyHeight int) string {
	header := viewTitleStyle.
		Width(availWidth).
		PaddingTop(1).
		PaddingBottom(1).
		Render("Settings: Job Display Configuration")
	headerLines := lipgloss.Height(header)

	if len(m.jobs) == 0 {
		emptyBox := lipgloss.NewStyle().
			Width(availWidth).
			Padding(1, 3).
			Render(lipgloss.JoinVertical(
				lipgloss.Left,
				lipgloss.NewStyle().Foreground(softWhite).Bold(true).Render("No jobs loaded."),
				"",
				mutedTextStyle.Render(truncateWidth("Job providers will populate here once connected.", availWidth-6)),
				mutedTextStyle.Render(truncateWidth("Tip: run `crolendar --demo` to preview sample jobs.", availWidth-6)),
			))
		return lipgloss.NewStyle().
			Width(availWidth).
			Height(bodyHeight).
			MaxHeight(bodyHeight).
			Render(lipgloss.JoinVertical(lipgloss.Top, header, emptyBox))
	}

	cursor := clampInt(m.settingsCursor, 0, len(m.jobs)-1)

	blocks := make([][]string, len(m.jobs))
	maxLineWidth := availWidth - 4
	if maxLineWidth < 12 {
		maxLineWidth = 12
	}

	for i, rawJob := range m.jobs {
		job := m.effectiveJob(rawJob)
		isSelected := cursor == i
		alias := job.DisplayName()
		colorHex := job.DisplayColor()
		sched := job.DisplaySchedule()

		style := lipgloss.NewStyle().PaddingLeft(2).Foreground(softWhite)
		prefix := "  "
		if isSelected {
			style = style.Bold(true).Foreground(purple)
			prefix = "> "
		}

		sourceTag := ""
		if src := job.DisplaySource(); src != "" {
			sourceTag = fmt.Sprintf("[%s] ", src)
		}
		headerLine := truncateWidth(fmt.Sprintf("%s%s (%s)", sourceTag, alias, sched), maxLineWidth-2)
		var itemLines []string
		itemLines = append(itemLines, style.Render(prefix+headerLine))

		if isSelected {
			detailStyle := lipgloss.NewStyle().PaddingLeft(6).Foreground(mutedGray)

			aliasVal := truncateWidth(alias, maxLineWidth-13)
			if m.settingsState == 1 {
				aliasVal = m.input.View()
			} else if m.settingsState == 2 && m.settingsDraftAlias != "" {
				aliasVal = truncateWidth(m.settingsDraftAlias, maxLineWidth-13)
			}
			itemLines = append(itemLines, detailStyle.Render(fmt.Sprintf("Alias: %s", aliasVal)))

			swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(colorHex)).Render("■")
			colorRow := fmt.Sprintf("Color: %s %s", colorHex, swatch)
			if m.settingsState == 2 {
				colorRow = fmt.Sprintf("Color: %s", m.input.View())
			}
			itemLines = append(itemLines, detailStyle.Render(colorRow))

			if m.settingsError != "" {
				itemLines = append(itemLines, errorTextStyle.PaddingLeft(6).Render(truncateWidth(m.settingsError, maxLineWidth-6)))
			} else if m.settingsNotice != "" {
				itemLines = append(itemLines, okTextStyle.PaddingLeft(6).Render(truncateWidth(m.settingsNotice, maxLineWidth-6)))
			}

			hint := "[Enter] Edit  [j/k] Navigate"
			if m.settingsState != 0 {
				hint = "[Enter] Next/Save  [Esc] Cancel"
			}
			itemLines = append(itemLines, lipgloss.NewStyle().PaddingLeft(6).Foreground(mutedGray).Render(truncateWidth(hint, maxLineWidth-6)))
			itemLines = append(itemLines, "")
		}
		blocks[i] = itemLines
	}

	availLines := bodyHeight - headerLines
	if availLines < 1 {
		availLines = 1
	}

	startIdx := cursor
	endIdx := cursor + 1
	used := len(blocks[cursor])
	markerLines := func(s, e int) int {
		n := 0
		if s > 0 {
			n++
		}
		if e < len(blocks) {
			n++
		}
		return n
	}
	for startIdx > 0 {
		nextUsed := used + len(blocks[startIdx-1])
		if nextUsed+markerLines(startIdx-1, endIdx) > availLines {
			break
		}
		startIdx--
		used = nextUsed
	}
	for endIdx < len(blocks) {
		nextUsed := used + len(blocks[endIdx])
		if nextUsed+markerLines(startIdx, endIdx+1) > availLines {
			break
		}
		used = nextUsed
		endIdx++
	}

	var visible []string
	if startIdx > 0 {
		visible = append(visible, mutedTextStyle.PaddingLeft(2).Render("▲ more jobs above"))
	}
	for i := startIdx; i < endIdx; i++ {
		visible = append(visible, blocks[i]...)
	}
	if endIdx < len(blocks) {
		visible = append(visible, mutedTextStyle.PaddingLeft(2).Render("▼ more jobs below"))
	}

	if len(visible) > availLines {
		visible = visible[:availLines]
	}

	content := lipgloss.JoinVertical(lipgloss.Left, visible...)
	return lipgloss.NewStyle().
		Width(availWidth).
		Height(bodyHeight).
		MaxHeight(bodyHeight).
		Render(lipgloss.JoinVertical(lipgloss.Top, header, content))
}
