package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func renderSettingsView(m model, availWidth, bodyHeight int) string {
	header := viewTitleStyle.
		Width(availWidth).
		PaddingTop(1).
		PaddingBottom(1).
		Render("Settings: Job Display Configuration")
	headerLines := lipgloss.Height(header)

	if len(m.jobs) == 0 {
		emptyLines := []string{}
		if m.loading {
			emptyLines = append(emptyLines,
				lipgloss.NewStyle().Foreground(softWhite).Bold(true).Render("Loading scheduled jobs..."),
			)
		} else {
			emptyLines = append(emptyLines,
				lipgloss.NewStyle().Foreground(softWhite).Bold(true).Render("No jobs loaded."),
				"",
			)
			hasErrOrWarn := false
			for _, st := range m.providerStatuses {
				if st.Err != nil {
					hasErrOrWarn = true
					msg := fmt.Sprintf("%s: %s", st.Name, sanitizeText(st.Err.Error()))
					emptyLines = append(emptyLines, errorTextStyle.Render(truncateWidth(msg, availWidth-6)))
				} else if len(st.Warnings) > 0 {
					hasErrOrWarn = true
					msg := fmt.Sprintf("%s: %d warning(s) (%s)", st.Name, len(st.Warnings), sanitizeText(st.Warnings[0]))
					emptyLines = append(emptyLines, warnTextStyle.Render(truncateWidth(msg, availWidth-6)))
				}
			}
			if !hasErrOrWarn {
				emptyLines = append(emptyLines,
					mutedTextStyle.Render(truncateWidth("No scheduled entries were returned by active providers.", availWidth-6)),
				)
			}
			emptyLines = append(emptyLines,
				mutedTextStyle.Render(truncateWidth("Tip: press r to reload, pass --crontab <file>, or use --demo.", availWidth-6)),
			)
		}
		emptyBox := lipgloss.NewStyle().
			Width(availWidth).
			Padding(1, 3).
			Render(lipgloss.JoinVertical(lipgloss.Left, emptyLines...))
		return lipgloss.NewStyle().
			Width(availWidth).
			Height(bodyHeight).
			MaxHeight(bodyHeight).
			Render(lipgloss.JoinVertical(lipgloss.Top, header, emptyBox))
	}

	cursor := clampInt(m.settingsCursor, 0, len(m.jobs)-1)

	maxLineWidth := availWidth - 4
	if maxLineWidth < 12 {
		maxLineWidth = 12
	}

	var statusBanner string
	for _, st := range m.providerStatuses {
		if st.Err != nil {
			msg := fmt.Sprintf("%s error: %s", st.Name, sanitizeText(st.Err.Error()))
			statusBanner = errorTextStyle.PaddingLeft(2).Render(truncateWidth(msg, maxLineWidth))
			break
		}
	}
	if statusBanner == "" {
		for _, st := range m.providerStatuses {
			if len(st.Warnings) > 0 {
				msg := fmt.Sprintf("%s: %d warning(s) (%s)", st.Name, len(st.Warnings), sanitizeText(st.Warnings[0]))
				statusBanner = warnTextStyle.PaddingLeft(2).Render(truncateWidth(msg, maxLineWidth))
				break
			}
		}
	}

	renderJobRow := func(i int, isSelected bool) []string {
		job := m.effectiveJob(m.jobs[i])
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
		itemLines := []string{style.Render(prefix + headerLine)}

		if isSelected {
			detailStyle := lipgloss.NewStyle().PaddingLeft(6).Foreground(mutedGray)

			aliasVal := truncateWidth(alias, maxLineWidth-13)
			if m.settingsState == 1 {
				aliasVal = m.input.View()
			} else if m.settingsState == 2 && m.settingsDraftAlias != "" {
				aliasVal = truncateWidth(m.settingsDraftAlias, maxLineWidth-13)
			}
			itemLines = append(itemLines, detailStyle.Render(ansi.Truncate(fmt.Sprintf("Alias: %s", aliasVal), maxLineWidth-6, "")))

			swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(colorHex)).Render("■")
			colorRow := fmt.Sprintf("Color: %s %s", colorHex, swatch)
			if m.settingsState == 2 {
				colorRow = fmt.Sprintf("Color: %s", m.input.View())
			}
			itemLines = append(itemLines, detailStyle.Render(ansi.Truncate(colorRow, maxLineWidth-6, "")))

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
		return itemLines
	}

	availLines := bodyHeight - headerLines
	if statusBanner != "" && availLines > 1 {
		availLines--
	}
	if availLines < 1 {
		availLines = 1
	}

	selectedLines := renderJobRow(cursor, true)
	startIdx := cursor
	endIdx := cursor + 1
	used := len(selectedLines)
	markerLines := func(s, e int) int {
		n := 0
		if s > 0 {
			n++
		}
		if e < len(m.jobs) {
			n++
		}
		return n
	}
	for startIdx > 0 {
		nextUsed := used + 1
		if nextUsed+markerLines(startIdx-1, endIdx) > availLines {
			break
		}
		startIdx--
		used = nextUsed
	}
	for endIdx < len(m.jobs) {
		nextUsed := used + 1
		if nextUsed+markerLines(startIdx, endIdx+1) > availLines {
			break
		}
		used = nextUsed
		endIdx++
	}

	var visible []string
	if statusBanner != "" {
		visible = append(visible, statusBanner)
	}
	if startIdx > 0 {
		visible = append(visible, mutedTextStyle.PaddingLeft(2).Render("▲ more jobs above"))
	}
	for i := startIdx; i < endIdx; i++ {
		if i == cursor {
			visible = append(visible, selectedLines...)
		} else {
			visible = append(visible, renderJobRow(i, false)...)
		}
	}
	if endIdx < len(m.jobs) {
		visible = append(visible, mutedTextStyle.PaddingLeft(2).Render("▼ more jobs below"))
	}

	maxTotal := bodyHeight - headerLines
	if maxTotal < 1 {
		maxTotal = 1
	}
	if len(visible) > maxTotal {
		visible = visible[:maxTotal]
	}

	content := lipgloss.JoinVertical(lipgloss.Left, visible...)
	return lipgloss.NewStyle().
		Width(availWidth).
		Height(bodyHeight).
		MaxHeight(bodyHeight).
		Render(lipgloss.JoinVertical(lipgloss.Top, header, content))
}
