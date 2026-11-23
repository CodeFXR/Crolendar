package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func renderWeekView(m model, availWidth int) (string, string) {
	timeColWidth := 8 // " 00:00 " + right border

	// Reserve 1 border column after time column and 1 after each of the 7 day columns.
	reservedWidth := timeColWidth + 8
	remainingWidth := availWidth - reservedWidth
	if remainingWidth < 7 {
		remainingWidth = 7
	}
	baseDayWidth := remainingWidth / 7
	extraCols := remainingWidth % 7

	getDayWidth := func(i int) int {
		w := baseDayWidth
		if i < extraCols {
			w++
		}
		return w
	}

	startOfWeek := m.cursor.AddDate(0, 0, -int(m.cursor.Weekday()))
	now := time.Now()
	mins := now.Minute()
	weekGrid := indexWeekJobs(m.jobs, startOfWeek)

	// 1. Sticky 3-line header row
	headerCorner := lipgloss.NewStyle().
		Width(timeColWidth-1).
		Height(3).
		Align(lipgloss.Right).
		PaddingRight(1).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(borderGray).
		Render("")

	headerCells := []string{headerCorner}
	for i := 0; i < 7; i++ {
		date := startOfWeek.AddDate(0, 0, i)
		w := getDayWidth(i)

		style := lipgloss.NewStyle().
			Width(w + 1).
			PaddingTop(1).
			PaddingBottom(1).
			Align(lipgloss.Center)

		isToday := sameDay(date, now)
		isSelected := sameDay(date, m.cursor)

		switch {
		case isSelected && m.focusGrid:
			style = style.Background(blue).Foreground(white).Bold(true)
		case isSelected:
			style = style.Foreground(blue).Bold(true)
		case isToday:
			style = style.Foreground(purple).Bold(true)
		default:
			style = style.Foreground(softWhite)
		}

		label := date.Format("Mon 02")
		if w < 6 {
			label = date.Format("02")
		}
		headerCells = append(headerCells, style.Render(truncateWidth(label, w)))
	}
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top, headerCells...)

	// 2. Uniform 3-lines-per-hour grid body + 1 closing bottom border line
	var timeColRows []string
	dayCols := make([][]string, 7)

	for h := 0; h < 24; h++ {
		timeLabel := fmt.Sprintf("%02d:00", h)
		timeCell := lipgloss.NewStyle().
			Width(timeColWidth-1).
			Height(3).
			Align(lipgloss.Right, lipgloss.Top).
			PaddingRight(1).
			Foreground(mutedGray).
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(borderGray).
			Render(timeLabel)
		timeColRows = append(timeColRows, timeCell)

		for d := 0; d < 7; d++ {
			date := startOfWeek.AddDate(0, 0, d)
			isToday := sameDay(date, now)
			isCurrentHour := isToday && h == now.Hour()
			w := getDayWidth(d)

			cellBorder := lipgloss.NormalBorder()
			if h == 0 {
				if d == 6 {
					cellBorder.TopRight = "┐"
				} else {
					cellBorder.TopRight = "┬"
				}
			} else {
				if d == 6 {
					cellBorder.TopRight = "┤"
				} else {
					cellBorder.TopRight = "┼"
				}
			}

			cellStyle := lipgloss.NewStyle().
				Width(w).
				Height(2).
				MaxHeight(3).
				Border(cellBorder, true, true, false, false).
				BorderForeground(borderGray)

			_, validWallHour := wallHourTime(date, h)
			var jobs []JobItem
			if validWallHour {
				for _, j := range weekGrid[d][h] {
					jobs = append(jobs, m.effectiveJob(j))
				}
			}

			line1, line2 := formatHourSlotLines(jobs, w, false)

			if isCurrentHour {
				nowLine := renderNowIndicatorLine(w)
				if mins < 15 || len(jobs) >= 2 {
					cellStyle = cellStyle.BorderTopForeground(purple)
				} else if mins < 45 {
					if len(jobs) == 0 {
						line1 = nowLine
					} else {
						line2 = nowLine
					}
				} else {
					line2 = nowLine
				}
			}

			cellContent := line1 + "\n" + line2
			dayCols[d] = append(dayCols[d], cellStyle.Render(cellContent))
		}
	}

	timeBottom := lipgloss.NewStyle().
		Width(timeColWidth).
		Foreground(borderGray).
		Render(strings.Repeat(" ", timeColWidth-1) + "└")
	var bottomSegments []string
	bottomSegments = append(bottomSegments, timeBottom)
	for d := 0; d < 7; d++ {
		w := getDayWidth(d)
		corner := "┴"
		if d == 6 {
			corner = "┘"
		}
		bottomSegments = append(bottomSegments, lipgloss.NewStyle().Foreground(borderGray).Render(strings.Repeat("─", w)+corner))
	}
	bottomCap := lipgloss.JoinHorizontal(lipgloss.Top, bottomSegments...)

	timeColumn := lipgloss.JoinVertical(lipgloss.Top, timeColRows...)
	dayColsRendered := make([]string, 0, 7)
	for _, rows := range dayCols {
		dayColsRendered = append(dayColsRendered, lipgloss.JoinVertical(lipgloss.Top, rows...))
	}
	gridRows := lipgloss.JoinHorizontal(lipgloss.Top, append([]string{timeColumn}, dayColsRendered...)...)
	gridBody := lipgloss.JoinVertical(lipgloss.Left, gridRows, bottomCap)

	return headerRow, gridBody
}

func renderNowIndicatorLine(w int) string {
	if w <= 0 {
		return ""
	}
	if w == 1 {
		return lipgloss.NewStyle().Width(1).Foreground(purple).Render("●")
	}
	return lipgloss.NewStyle().Width(w).Foreground(purple).Render("●" + strings.Repeat("─", w-1))
}

func formatHourSlotLines(jobs []JobItem, width int, includeSchedule bool) (string, string) {
	if width <= 0 {
		return "", ""
	}
	blank := strings.Repeat(" ", width)
	if len(jobs) == 0 {
		return blank, blank
	}

	renderJobLine := func(j JobItem) string {
		text := j.DisplayName()
		if includeSchedule {
			if src := j.DisplaySource(); src != "" {
				text = fmt.Sprintf("[%s] %s · %s", src, text, j.DisplaySchedule())
			} else {
				text = fmt.Sprintf("%s · %s", text, j.DisplaySchedule())
			}
		}
		innerW := width
		padLeft := 0
		if width >= 4 && includeSchedule {
			padLeft = 1
			innerW = width - 1
		}
		clipped := truncateWidth(text, innerW)
		return lipgloss.NewStyle().
			Width(width).
			PaddingLeft(padLeft).
			Foreground(white).
			Background(j.LipglossColor()).
			Render(clipped)
	}

	line1 := renderJobLine(jobs[0])
	line2 := blank
	if len(jobs) == 2 {
		line2 = renderJobLine(jobs[1])
	} else if len(jobs) > 2 {
		moreTxt := truncateWidth(fmt.Sprintf("+%d more", len(jobs)-1), width)
		line2 = lipgloss.NewStyle().Width(width).Foreground(mutedGray).Render(moreTxt)
	}
	return line1, line2
}
