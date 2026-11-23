package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func renderDayView(m model, availWidth int) (string, string) {
	timeColWidth := 8
	barWidth := availWidth - timeColWidth
	if barWidth < 1 {
		barWidth = 1
	}

	now := time.Now()
	isToday := sameDay(m.cursor, now)
	mins := now.Minute()

	timeHeader := lipgloss.NewStyle().
		Width(timeColWidth-1).
		Height(3).
		Align(lipgloss.Right).
		PaddingRight(1).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(borderGray).
		Render("")

	headerTitle := m.cursor.Format("Monday, January 02")
	if isToday {
		headerTitle += "  (Today)"
	}
	gridHeader := lipgloss.NewStyle().
		Width(barWidth).
		Align(lipgloss.Center).
		Foreground(blue).
		Bold(true).
		PaddingTop(1).
		PaddingBottom(1).
		Render(truncateWidth(headerTitle, barWidth))

	header := lipgloss.JoinHorizontal(lipgloss.Top, timeHeader, gridHeader)

	var hours []string
	var bars []string

	for h := 0; h < 24; h++ {
		label := fmt.Sprintf("%02d:00", h)

		timeStyle := lipgloss.NewStyle().
			Width(timeColWidth-1).
			Height(3).
			Align(lipgloss.Right, lipgloss.Top).
			PaddingRight(1).
			Foreground(mutedGray).
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(borderGray)

		barStyle := lipgloss.NewStyle().
			Width(barWidth).
			Height(2).
			MaxHeight(3).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(borderGray)

		var jobs []JobItem
		if checkTime, ok := wallHourTime(m.cursor, h); ok {
			for _, j := range JobsForHour(m.jobs, checkTime) {
				jobs = append(jobs, m.effectiveJob(j))
			}
		}

		line1, line2 := formatHourSlotLines(jobs, barWidth, true)

		if isToday && h == now.Hour() {
			nowLine := renderNowIndicatorLine(barWidth)
			if mins < 15 || len(jobs) >= 2 {
				barStyle = barStyle.BorderTopForeground(purple)
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

		hours = append(hours, timeStyle.Render(label))
		bars = append(bars, barStyle.Render(line1+"\n"+line2))
	}

	timeBottom := lipgloss.NewStyle().
		Width(timeColWidth).
		Foreground(borderGray).
		Render(strings.Repeat(" ", timeColWidth-1) + "└")
	barBottom := lipgloss.NewStyle().
		Foreground(borderGray).
		Render(strings.Repeat("─", barWidth))
	bottomCap := lipgloss.JoinHorizontal(lipgloss.Top, timeBottom, barBottom)

	timeColumn := lipgloss.JoinVertical(lipgloss.Top, hours...)
	dayColumn := lipgloss.JoinVertical(lipgloss.Top, bars...)
	gridRows := lipgloss.JoinHorizontal(lipgloss.Top, timeColumn, dayColumn)
	body := lipgloss.JoinVertical(lipgloss.Left, gridRows, bottomCap)

	return header, body
}
