package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func renderMonthView(m model, availWidth, bodyHeight int) string {
	now := time.Now()
	loc := m.cursor.Location()
	if loc == nil {
		loc = time.Local
	}
	first := time.Date(m.cursor.Year(), m.cursor.Month(), 1, 0, 0, 0, 0, loc)
	startWeekday := int(first.Weekday())
	lastDay := first.AddDate(0, 1, -1).Day()
	numWeeks := (startWeekday + lastDay + 6) / 7
	if numWeeks < 5 {
		numWeeks = 5
	}
	dayGrid := indexDayJobs(m.jobs, first, lastDay)

	titlePadTop := 1
	titlePadBottom := 1
	if bodyHeight < 25 {
		titlePadTop = 0
		titlePadBottom = 0
	}
	title := viewTitleStyle.
		Width(availWidth).
		PaddingTop(titlePadTop).
		PaddingBottom(titlePadBottom).
		Render(truncateWidth(m.cursor.Format("January 2006"), availWidth))
	titleLines := lipgloss.Height(title)

	colWidth := availWidth / 7
	// Bordered 7-col cells require colWidth >= 6 and at least 3 lines per week row
	// (plus title and weekday header). Fall back to a compact borderless grid whenever
	// width or height is too tight so every day 1..lastDay remains visible.
	if colWidth < 6 || bodyHeight-titleLines-1 < numWeeks*3 {
		return renderCompactMonthView(m, now, first, loc, startWeekday, lastDay, numWeeks, dayGrid, title, titleLines, availWidth, bodyHeight)
	}

	cellWidth := colWidth - 2 // subtract left & right border columns
	gridAvailHeight := bodyHeight - titleLines - 1
	if gridAvailHeight < numWeeks*3 {
		gridAvailHeight = numWeeks * 3
	}
	rowTotalHeight := gridAvailHeight / numWeeks
	cellHeight := rowTotalHeight - 2
	if cellHeight < 1 {
		cellHeight = 1
	}

	days := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	headerRow := make([]string, 0, 7)
	for _, d := range days {
		headerRow = append(
			headerRow,
			lipgloss.NewStyle().
				Width(colWidth).
				Align(lipgloss.Center).
				Foreground(purple).
				Bold(true).
				Render(truncateWidth(d, colWidth)),
		)
	}

	baseCellStyle := lipgloss.NewStyle().
		Width(cellWidth).
		Height(cellHeight).
		MaxHeight(cellHeight+2).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderGray).
		Align(lipgloss.Right, lipgloss.Top).
		Padding(0, 1)

	emptyCellStyle := baseCellStyle.Foreground(mutedGray)

	var rows []string
	var currentRow []string
	for i := 0; i < startWeekday; i++ {
		currentRow = append(currentRow, emptyCellStyle.Render(""))
	}

	innerTextWidth := cellWidth - 2
	if innerTextWidth < 1 {
		innerTextWidth = 1
	}

	for day := 1; day <= lastDay; day++ {
		currDate := time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, loc)
		style := baseCellStyle
		dayNumStyle := lipgloss.NewStyle().Foreground(softWhite)

		isToday := sameDay(currDate, now)
		isSelected := sameDay(currDate, m.cursor)

		if isToday {
			style = style.BorderForeground(purple)
			dayNumStyle = dayNumStyle.Foreground(purple).Bold(true)
		}
		if isSelected {
			style = style.BorderForeground(blue)
			if m.focusGrid {
				dayNumStyle = dayNumStyle.Background(blue).Foreground(white).Bold(true)
			} else {
				dayNumStyle = dayNumStyle.Foreground(blue).Bold(true)
			}
		}

		dayJobs := dayGrid[day-1]
		dayLabel := truncateWidth(fmt.Sprintf("%d", day), innerTextWidth)

		var cell string
		if cellHeight == 1 {
			badgeText := fmt.Sprintf("[%d]", len(dayJobs))
			if len(dayJobs) > 0 && len(badgeText)+1+lipgloss.Width(dayLabel) <= innerTextWidth {
				badgeJob := m.effectiveJob(dayJobs[0])
				badge := lipgloss.NewStyle().Foreground(badgeJob.LipglossColor()).Render(badgeText)
				cell = style.Render(badge + " " + dayNumStyle.Render(dayLabel))
			} else {
				cell = style.Render(dayNumStyle.Render(dayLabel))
			}
		} else {
			lines := []string{dayNumStyle.Render(dayLabel)}
			maxJobLines := cellHeight - 1
			if len(dayJobs) > 0 && maxJobLines > 0 {
				showCount := len(dayJobs)
				overflow := 0
				inlineMore := ""
				if showCount > maxJobLines {
					if maxJobLines > 1 {
						showCount = maxJobLines - 1
						overflow = len(dayJobs) - showCount
					} else {
						showCount = 1
						inlineMore = fmt.Sprintf("+%d", len(dayJobs)-1)
					}
				}
				for idx := 0; idx < showCount; idx++ {
					j := m.effectiveJob(dayJobs[idx])
					var txt string
					if inlineMore != "" && innerTextWidth > len(inlineMore)+2 {
						namePart := truncateWidth(j.DisplayName(), innerTextWidth-len(inlineMore)-1)
						txt = namePart + " " + inlineMore
					} else {
						txt = truncateWidth(j.DisplayName(), innerTextWidth)
					}
					jobLine := lipgloss.NewStyle().Foreground(j.LipglossColor()).Render(txt)
					lines = append(lines, jobLine)
				}
				if overflow > 0 && len(lines) < cellHeight {
					lines = append(lines, mutedTextStyle.Render(truncateWidth(fmt.Sprintf("+%d more", overflow), innerTextWidth)))
				}
			}
			inner := lipgloss.JoinVertical(lipgloss.Right, lines...)
			cell = style.Render(inner)
		}

		currentRow = append(currentRow, cell)
		if len(currentRow) == 7 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
			currentRow = nil
		}
	}

	if len(currentRow) > 0 {
		for len(currentRow) < 7 {
			currentRow = append(currentRow, emptyCellStyle.Render(""))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
	}

	view := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		lipgloss.JoinHorizontal(lipgloss.Top, headerRow...),
		lipgloss.JoinVertical(lipgloss.Left, rows...),
	)
	return lipgloss.NewStyle().
		Width(availWidth).
		Height(bodyHeight).
		MaxHeight(bodyHeight).
		Render(view)
}

func renderCompactMonthView(
	m model,
	now, first time.Time,
	loc *time.Location,
	startWeekday, lastDay, numWeeks int,
	dayGrid [][]JobItem,
	title string,
	titleLines, availWidth, bodyHeight int,
) string {
	colWidth := availWidth / 7
	if colWidth < 2 {
		colWidth = 2
	}

	days := []string{"S", "M", "T", "W", "T", "F", "S"}
	headerRow := make([]string, 0, 7)
	for _, d := range days {
		headerRow = append(headerRow, lipgloss.NewStyle().
			Width(colWidth).
			Align(lipgloss.Center).
			Foreground(purple).
			Bold(true).
			Render(d))
	}

	showDots := bodyHeight-titleLines-1 >= numWeeks*2
	var rows []string
	var currentRow []string
	emptyCell := lipgloss.NewStyle().Width(colWidth).Render("")
	if showDots {
		emptyCell = lipgloss.JoinVertical(lipgloss.Center, emptyCell, emptyCell)
	}
	for i := 0; i < startWeekday; i++ {
		currentRow = append(currentRow, emptyCell)
	}

	for day := 1; day <= lastDay; day++ {
		currDate := time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, loc)
		style := lipgloss.NewStyle().Width(colWidth).Align(lipgloss.Center).Foreground(softWhite)
		if sameDay(currDate, now) {
			style = style.Foreground(purple).Bold(true)
		}
		if sameDay(currDate, m.cursor) {
			if m.focusGrid {
				style = style.Background(blue).Foreground(white).Bold(true)
			} else {
				style = style.Foreground(blue).Bold(true)
			}
		}

		dayJobs := dayGrid[day-1]
		dayStr := fmt.Sprintf("%2d", day)
		if !showDots && len(dayJobs) > 0 && colWidth >= 3 {
			dayStr = fmt.Sprintf("%2d*", day)
		}
		cell := style.Render(truncateWidth(dayStr, colWidth))

		if showDots {
			dotStr := " "
			dotColor := mutedGray
			if len(dayJobs) > 0 {
				dotJob := m.effectiveJob(dayJobs[0])
				dotColor = dotJob.LipglossColor()
				dots := len(dayJobs)
				if dots > 2 {
					dots = 2
				}
				dotStr = strings.Repeat("•", dots)
			}
			dot := lipgloss.NewStyle().Width(colWidth).Align(lipgloss.Center).Foreground(dotColor).Render(dotStr)
			cell = lipgloss.JoinVertical(lipgloss.Center, cell, dot)
		}

		currentRow = append(currentRow, cell)
		if len(currentRow) == 7 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
			currentRow = nil
		}
	}
	if len(currentRow) > 0 {
		for len(currentRow) < 7 {
			currentRow = append(currentRow, emptyCell)
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
	}

	view := lipgloss.JoinVertical(
		lipgloss.Center,
		title,
		lipgloss.JoinHorizontal(lipgloss.Top, headerRow...),
		lipgloss.JoinVertical(lipgloss.Left, rows...),
	)
	return lipgloss.NewStyle().
		Width(availWidth).
		Height(bodyHeight).
		MaxHeight(bodyHeight).
		Align(lipgloss.Center, lipgloss.Top).
		Render(view)
}
