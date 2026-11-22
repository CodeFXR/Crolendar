package main

import (
	"fmt"
	"strings"
	"time"
	"github.com/charmbracelet/lipgloss"
)

func renderDayView(m model) (string, string) {
	timeColWidth := 8
	gridWidth := m.width - 22
	
	barWidth := gridWidth - timeColWidth - 1 
	if barWidth < 0 { barWidth = 0 }

	now := time.Now()
	isToday := m.cursor.Format("2006-01-02") == now.Format("2006-01-02")
	mins := now.Minute()
	
timeHeader := lipgloss.NewStyle().
		Width(timeColWidth).
		Height(4). 
		Align(lipgloss.Right).
		PaddingRight(1).
		Border(lipgloss.NormalBorder(), false, true, false, false). 
		BorderForeground(gray).
		Render("") 

	gridHeader := lipgloss.NewStyle().
		Width(barWidth).
		Align(lipgloss.Center).
		Foreground(blue).
		Bold(true).
		PaddingTop(2).
		PaddingBottom(1).
		Render(m.cursor.Format("Monday, January 02"))

	header := lipgloss.JoinHorizontal(lipgloss.Top, timeHeader, gridHeader)

	var hours []string
	var bars []string

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

		barStyle := lipgloss.NewStyle().
			Width(barWidth).
			BorderForeground(gray)

		if h == 23 {
			barStyle = barStyle.Height(1).Border(lipgloss.NormalBorder(), true, false, true, false)
		} else {
			barStyle = barStyle.Height(2).Border(lipgloss.NormalBorder(), true, false, false, false)
		}

		// Check for jobs
		checkTime := time.Date(m.cursor.Year(), m.cursor.Month(), m.cursor.Day(), h, 0, 0, 0, time.Local)
		jobs := GetJobsForHour(m.jobs, checkTime)

		content := ""
		if isToday && h == now.Hour() {
			lineLength := barWidth - 2
			if lineLength < 0 { lineLength = 0 }
			
			visualLine := "●" + strings.Repeat("─", lineLength)
			lineStyle := lipgloss.NewStyle().Width(barWidth).Foreground(purple)

			if mins < 15 {
				barStyle = barStyle.BorderTopForeground(purple)
				if len(jobs) > 0 {
					content = lipgloss.NewStyle().Width(barWidth).Foreground(white).Background(blue).PaddingLeft(1).Render(jobs[0].Command)
					content = lipgloss.JoinVertical(lipgloss.Left, " ", content)
				} else {
					content = " \n "
				}
			} else if mins < 45 {
				if len(jobs) > 0 {
					jobLabel := lipgloss.NewStyle().Width(barWidth).Foreground(white).Background(blue).PaddingLeft(1).Render(jobs[0].Command)
					content = jobLabel + "\n" + lineStyle.Render(visualLine)
				} else {
					content = lineStyle.Render(visualLine) + "\n "
				}
			} else {
				if len(jobs) > 0 {
					jobLabel := lipgloss.NewStyle().Width(barWidth).Foreground(white).Background(blue).PaddingLeft(1).Render(jobs[0].Command)
					content = jobLabel + "\n" + lineStyle.Render(visualLine)
				} else {
					content = " \n" + lineStyle.Render(visualLine)
				}
			}
		} else {
			if len(jobs) > 0 {
				jobLabel := lipgloss.NewStyle().Width(barWidth).Foreground(white).Background(blue).PaddingLeft(1).Render(jobs[0].Command)
				content = lipgloss.JoinVertical(lipgloss.Left, jobLabel, " ")
			} else {
				content = strings.Repeat(" ", barWidth)
				content = lipgloss.JoinVertical(lipgloss.Left, content, " ")
			}
		}

		hours = append(hours, timeStyle.Render(label))
		bars = append(bars, barStyle.Render(content))
	}

	timeColumn := lipgloss.JoinVertical(lipgloss.Top, hours...)
	dayColumn := lipgloss.JoinVertical(lipgloss.Top, bars...)
	
	body := lipgloss.JoinHorizontal(lipgloss.Top, timeColumn, dayColumn)
	return header, body
}