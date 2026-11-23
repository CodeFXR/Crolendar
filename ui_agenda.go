package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func renderAgendaView(m model, availWidth, bodyHeight int) string {
	now := time.Now()
	loc := m.cursor.Location()
	if loc == nil {
		loc = time.Local
	}
	first := time.Date(m.cursor.Year(), m.cursor.Month(), 1, 0, 0, 0, 0, loc)
	startDate := first.AddDate(0, 0, -int(first.Weekday()))
	dayGrid := indexDayJobs(m.jobs, startDate, 42)

	cellWidth := availWidth / 7
	if cellWidth < 3 {
		cellWidth = 3
	}

	compactGrid := bodyHeight < 24
	showDotsRow := bodyHeight >= 20

	titlePaddingTop := 1
	titlePaddingBottom := 1
	if compactGrid {
		titlePaddingTop = 0
		titlePaddingBottom = 0
	}

	title := viewTitleStyle.
		Width(availWidth).
		PaddingTop(titlePaddingTop).
		PaddingBottom(titlePaddingBottom).
		Render(m.cursor.Format("January 2006"))

	headerStyle := lipgloss.NewStyle().
		Width(cellWidth).
		Align(lipgloss.Center).
		Foreground(purple).
		Bold(true)
	days := []string{"S", "M", "T", "W", "T", "F", "S"}
	headerRow := make([]string, 0, 7)
	for _, d := range days {
		headerRow = append(headerRow, headerStyle.Render(d))
	}
	gridHeaderPad := 1
	if compactGrid {
		gridHeaderPad = 0
	}
	gridHeader := lipgloss.NewStyle().
		PaddingBottom(gridHeaderPad).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, headerRow...))

	var rows []string
	var currentRow []string
	var selectedJobs []JobItem
	foundSelected := false
	for i := 0; i < 42; i++ {
		curr := startDate.AddDate(0, 0, i)
		style := lipgloss.NewStyle().Width(cellWidth).Align(lipgloss.Center)

		if curr.Month() != m.cursor.Month() {
			style = style.Foreground(mutedGray)
		} else {
			style = style.Foreground(softWhite)
		}
		if sameDay(curr, now) {
			style = style.Foreground(purple).Bold(true)
		}
		if sameDay(curr, m.cursor) {
			style = style.Background(blue).Foreground(white).Bold(true)
		}

		dayJobs := dayGrid[i]
		if sameDay(curr, m.cursor) {
			selectedJobs = dayJobs
			foundSelected = true
		}
		dayStr := fmt.Sprintf("%2d", curr.Day())
		if !showDotsRow && len(dayJobs) > 0 {
			dayStr = fmt.Sprintf("%2d*", curr.Day())
		}
		cell := style.Render(dayStr)

		if showDotsRow {
			dotLine := " "
			dotColor := mutedGray
			if len(dayJobs) > 0 {
				firstJob := m.effectiveJob(dayJobs[0])
				dotColor = firstJob.LipglossColor()
				dots := len(dayJobs)
				if dots > 3 {
					dots = 3
				}
				dotLine = strings.Repeat("•", dots)
			}
			dot := lipgloss.NewStyle().
				Width(cellWidth).
				Align(lipgloss.Center).
				Foreground(dotColor).
				Render(dotLine)
			cell = lipgloss.JoinVertical(lipgloss.Center, cell, dot)
		}

		currentRow = append(currentRow, cell)
		if len(currentRow) == 7 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
			currentRow = nil
		}
	}

	topContent := lipgloss.JoinVertical(
		lipgloss.Center,
		title,
		gridHeader,
		lipgloss.JoinVertical(lipgloss.Left, rows...),
	)
	topLines := lipgloss.Height(topContent)

	divWidth := availWidth - 4
	if divWidth < 1 {
		divWidth = 1
	}
	divider := lipgloss.NewStyle().
		Width(availWidth).
		Align(lipgloss.Center).
		Foreground(borderGray).
		Render(strings.Repeat("─", divWidth))

	bottomHeight := bodyHeight - topLines - 1
	if bottomHeight < 1 {
		bottomHeight = 1
	}

	if !foundSelected {
		selectedJobs = JobsForDay(m.jobs, m.cursor)
	}
	dayHeading := fmt.Sprintf("%s  (%s)", m.cursor.Format("Monday, Jan 02"), formatDayCountLabel(len(selectedJobs)))
	dayHeader := lipgloss.NewStyle().
		Width(availWidth).
		Align(lipgloss.Center).
		Foreground(blue).
		Bold(true).
		Render(truncateWidth(dayHeading, availWidth-2))

	maxEventLines := bottomHeight - 2
	if maxEventLines < 1 {
		maxEventLines = 1
	}

	var jobBlock string
	if len(selectedJobs) == 0 {
		jobBlock = mutedTextStyle.Render("No scheduled events")
	} else {
		var lines []string
		limit := len(selectedJobs)
		overflow := 0
		inlineSuffix := ""
		if limit > maxEventLines {
			if maxEventLines > 1 {
				limit = maxEventLines - 1
				overflow = len(selectedJobs) - limit
			} else {
				limit = 1
				inlineSuffix = fmt.Sprintf(" (+%d more)", len(selectedJobs)-1)
			}
		}
		maxLineWidth := availWidth - 4
		if maxLineWidth < 8 {
			maxLineWidth = 8
		}
		for i := 0; i < limit; i++ {
			j := m.effectiveJob(selectedJobs[i])
			swatch := lipgloss.NewStyle().Foreground(j.LipglossColor()).Render("■")
			meta := j.DisplaySchedule()
			if src := j.DisplaySource(); src != "" {
				meta = fmt.Sprintf("[%s] %s", src, meta)
			}
			availText := maxLineWidth - 2
			if inlineSuffix != "" {
				suffix := inlineSuffix
				if len(suffix) >= availText-2 {
					suffix = fmt.Sprintf(" (+%d)", len(selectedJobs)-1)
				}
				if availText > len(suffix)+2 {
					text := truncateWidth(fmt.Sprintf("%s  %s", j.DisplayName(), meta), availText-len(suffix)) + suffix
					lines = append(lines, swatch+" "+lipgloss.NewStyle().Foreground(softWhite).Render(text))
				} else {
					lines = append(lines, swatch+" "+lipgloss.NewStyle().Foreground(softWhite).Render(truncateWidth(strings.TrimSpace(suffix), availText)))
				}
			} else {
				text := truncateWidth(fmt.Sprintf("%s  %s", j.DisplayName(), meta), availText)
				lines = append(lines, swatch+" "+lipgloss.NewStyle().Foreground(softWhite).Render(text))
			}
		}
		if overflow > 0 {
			lines = append(lines, mutedTextStyle.Render(truncateWidth(fmt.Sprintf("+%d more", overflow), maxLineWidth)))
		}
		jobBlock = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	bottomContent := lipgloss.JoinVertical(lipgloss.Center, dayHeader, "", jobBlock)
	bottomPanel := lipgloss.NewStyle().
		Width(availWidth).
		Height(bottomHeight).
		MaxHeight(bottomHeight).
		Align(lipgloss.Center, lipgloss.Top).
		Render(bottomContent)

	return lipgloss.NewStyle().
		Width(availWidth).
		Height(bodyHeight).
		MaxHeight(bodyHeight).
		Render(lipgloss.JoinVertical(lipgloss.Left, topContent, divider, bottomPanel))
}
