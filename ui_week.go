package main

import (
	"fmt"
	"strings"
	"time"
	"github.com/charmbracelet/lipgloss"
)

func renderWeekView(m model) (string, string) {
	timeColWidth := 8
	gridWidth := m.width - 22
	
	reservedWidth := timeColWidth + 1 + 7 
	remainingWidth := gridWidth - reservedWidth
	if remainingWidth < 0 { remainingWidth = 0 }
	
baseDayWidth := remainingWidth / 7
	extraPixels := remainingWidth % 7

	getDayWidth := func(i int) int {
		w := baseDayWidth
		if i < extraPixels {
			w++
		}
		return w
	}

	startOfWeek := m.cursor.AddDate(0, 0, -int(m.cursor.Weekday()))
	now := time.Now()
	mins := now.Minute()

	// 1. Header Row
	headerCorner := lipgloss.NewStyle().
		Width(timeColWidth).
		Height(4). 
		Align(lipgloss.Right).
		PaddingRight(1).
		Foreground(gray).
		Border(lipgloss.NormalBorder(), false, true, false, false). 
		BorderForeground(gray).
		Render("")

	var headerCells []string
	headerCells = append(headerCells, headerCorner)

	for i := 0; i < 7; i++ {
		date := startOfWeek.AddDate(0, 0, i)
		w := getDayWidth(i)
		
		mixedBorder := lipgloss.Border{
			Top:    " ",
			Bottom: " ", 
			Left:   " ",
			Right:  " ", 
			TopLeft: " ", TopRight: " ", BottomLeft: " ", BottomRight: " ",
		}

		style := lipgloss.NewStyle().
			Width(w).
			Align(lipgloss.Center).
			PaddingTop(2).    
			PaddingBottom(1). 
			Border(mixedBorder, false, true, false, false).
			BorderForeground(gray)

		if date.Format("2006-01-02") == now.Format("2006-01-02") {
			style = style.Foreground(purple).Bold(true)
		} else {
			style = style.Foreground(white)
		}
		headerCells = append(headerCells, style.Render(date.Format("Mon 02")))
	}
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top, headerCells...)

	// 2. Grid Body
	var timeColRows []string
	var dayCols [][]string = make([][]string, 7)

	for h := 0; h < 24; h++ {
		label := fmt.Sprintf("%02d:00", h)
		
		timeStyle := lipgloss.NewStyle().
			Width(timeColWidth).
			Height(3).
			Align(lipgloss.Right, lipgloss.Top).
			PaddingRight(1).
			Foreground(gray).
			Border(lipgloss.NormalBorder(), false, true, false, false). 
			BorderForeground(gray)

		timeCell := timeStyle.Render(label)
		timeColRows = append(timeColRows, timeCell)

		for d := 0; d < 7; d++ {
			date := startOfWeek.AddDate(0, 0, d)
			isToday := date.Format("2006-01-02") == now.Format("2006-01-02")
			isCurrentHour := isToday && h == now.Hour()
			w := getDayWidth(d)

			cellStyle := lipgloss.NewStyle().
				Width(w).
				BorderForeground(gray)

			if h == 23 {
				cellStyle = cellStyle.Height(1).Border(lipgloss.NormalBorder(), true, true, true, false)
			} else {
				cellStyle = cellStyle.Height(2).Border(lipgloss.NormalBorder(), true, true, false, false)
			}

			var cellContent string
			
			// Check for jobs in this hour
			checkTime := time.Date(date.Year(), date.Month(), date.Day(), h, 0, 0, 0, time.Local)
			jobs := GetJobsForHour(m.jobs, checkTime)

			if isCurrentHour {
				// Exact width filling: Dot (1) + Line (w-2) to avoid touching border
				lineLength := w - 2
				if lineLength < 0 { lineLength = 0 }
				
				// U+25CF (●) + U+2500 (─)
				visualLine := "●" + strings.Repeat("─", lineLength)
				
				lineStyle := lipgloss.NewStyle().Width(w).Foreground(purple)

				if mins < 15 {
					cellStyle = cellStyle.BorderTopForeground(purple)
					if len(jobs) > 0 {
						txt := truncate(jobs[0].Command, w)
						cellContent = lipgloss.NewStyle().Width(w).Foreground(white).Background(blue).Render(txt)
						cellContent = lipgloss.JoinVertical(lipgloss.Left, " ", cellContent)
					} else {
						cellContent = " \n "
					}
				} else if mins < 45 {
					// Line in middle
					if len(jobs) > 0 {
						txt := truncate(jobs[0].Command, w)
						jobLabel := lipgloss.NewStyle().Width(w).Foreground(white).Background(blue).Render(txt)
						cellContent = jobLabel + "\n" + lineStyle.Render(visualLine)
					} else {
						cellContent = lineStyle.Render(visualLine) + "\n "
					}
				} else {
					// Line at bottom
					if len(jobs) > 0 {
						txt := truncate(jobs[0].Command, w)
						jobLabel := lipgloss.NewStyle().Width(w).Foreground(white).Background(blue).Render(txt)
						cellContent = jobLabel + "\n" + lineStyle.Render(visualLine)
					} else {
						cellContent = " \n" + lineStyle.Render(visualLine)
					}
				}
			} else {
				if len(jobs) > 0 {
					txt := truncate(jobs[0].Command, w)
					jobLabel := lipgloss.NewStyle().Width(w).Foreground(white).Background(blue).Render(txt)
					// Fill remaining space
					cellContent = lipgloss.JoinVertical(lipgloss.Left, jobLabel, " ")
				} else {
					cellContent = strings.Repeat(" ", w)
					cellContent = lipgloss.JoinVertical(lipgloss.Left, cellContent, " ")
				}
			}
			dayCols[d] = append(dayCols[d], cellStyle.Render(cellContent))
		}
	}

	timeColumn := lipgloss.JoinVertical(lipgloss.Top, timeColRows...)
	var dayColsRendered []string
	for _, rows := range dayCols {
		dayColsRendered = append(dayColsRendered, lipgloss.JoinVertical(lipgloss.Top, rows...))
	}
	gridBody := lipgloss.JoinHorizontal(lipgloss.Top, append([]string{timeColumn}, dayColsRendered...)...)

	return headerRow, gridBody
}
