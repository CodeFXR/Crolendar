package main

import (
	"fmt"
	"strings"
	"time"
	"github.com/charmbracelet/lipgloss"
)

func renderAgendaView(m model) string {
	availWidth := m.width - 22
	availHeight := m.height - 1
	halfHeight := availHeight / 2

	first := time.Date(m.cursor.Year(), m.cursor.Month(), 1, 0, 0, 0, 0, time.Local)
	startSpace := int(first.Weekday())
	startDate := first.AddDate(0, 0, -startSpace)

	cellWidth := availWidth / 7
	headerStyle := lipgloss.NewStyle().Width(cellWidth).Align(lipgloss.Center).Foreground(purple).Bold(true)
	days := []string{"S", "M", "T", "W", "T", "F", "S"}
	var headerRow []string
	for _, d := range days { headerRow = append(headerRow, headerStyle.Render(d)) }
	
	// Add padding to the header to separate it from the numbers
	gridHeader := lipgloss.NewStyle().
		PaddingBottom(1).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, headerRow...))

	var rows []string
	var currentRow []string
	today := time.Now().Format("2006-01-02")
	for i := 0; i < 42; i++ {
		curr := startDate.AddDate(0, 0, i)
		style := lipgloss.NewStyle().Width(cellWidth).Align(lipgloss.Center)
		
		// Month coloring
		if curr.Month() != m.cursor.Month() { 
			style = style.Foreground(gray) 
		} else { 
			style = style.Foreground(white) 
		}

		// Current Day Highlight (Bold Purple)
		if curr.Format("2006-01-02") == today {
			style = style.Foreground(purple).Bold(true)
		}

		// Selection Highlight (Blue Background)
		if curr.Format("2006-01-02") == m.cursor.Format("2006-01-02") { 
			style = style.Background(blue).Foreground(white).Bold(true) 
		}

		dayStr := fmt.Sprintf("%d", curr.Day())
		content := style.Render(dayStr)

		// Check for jobs
		dayJobs := GetJobsForDay(m.jobs, curr)
		if len(dayJobs) > 0 {
			// Get color of first job for the dot
			_, colorHex := m.config.GetJobDisplay(dayJobs[0].Command)
			
			// Use the same width and alignment as the day number to ensure perfect centering
			dotStyle := lipgloss.NewStyle().
				Width(cellWidth).
				Align(lipgloss.Center).
				Foreground(lipgloss.Color(colorHex))
				
			dot := dotStyle.Render("•")
			
			content = lipgloss.JoinVertical(lipgloss.Center, style.Render(dayStr), dot)
		} else {
			// Add a blank line of same height/style to maintain grid consistency
			blank := lipgloss.NewStyle().Width(cellWidth).Align(lipgloss.Center).Render(" ")
			content = lipgloss.JoinVertical(lipgloss.Center, style.Render(dayStr), blank)
		}

		currentRow = append(currentRow, content)
				if len(currentRow) == 7 {
					rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
					currentRow = []string{}
				}	}

	title := lipgloss.NewStyle().
		Width(availWidth).
		Align(lipgloss.Center).
		Foreground(blue).
		Bold(true).
		PaddingTop(2).
		PaddingBottom(1).
		Render(m.cursor.Format("January 2006"))

	topContent := lipgloss.JoinVertical(lipgloss.Center,
		title,
		gridHeader, 
		lipgloss.JoinVertical(lipgloss.Left, rows...))

	topHalf := lipgloss.NewStyle().
		Width(availWidth).
		Height(halfHeight).
		Align(lipgloss.Center, lipgloss.Top). 
		Render(topContent)

	line := lipgloss.NewStyle().Foreground(gray).Render(strings.Repeat("─", availWidth-2))
	
	// Bottom Half: Show Jobs for Selected Day
	selectedJobs := GetJobsForDay(m.jobs, m.cursor)
	var jobList string
	if len(selectedJobs) > 0 {
		var lines []string
		for _, j := range selectedJobs {
			label, colorHex := m.config.GetJobDisplay(j.Command)
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorHex))
			lines = append(lines, style.Render(fmt.Sprintf("• %s", label)))
		}
		jobList = lipgloss.JoinVertical(lipgloss.Left, lines...)
	} else {
		jobList = lipgloss.NewStyle().Foreground(gray).Render("No Scheduled Events")
	}

	bottomContent := lipgloss.JoinVertical(lipgloss.Center, 
		"\n", 
		lipgloss.NewStyle().Foreground(blue).Bold(true).Render(m.cursor.Format("Monday 02")), 
		"\n\n", 
		jobList,
	)

	bottomHalf := lipgloss.NewStyle().Width(availWidth).Height(halfHeight).Align(lipgloss.Center, lipgloss.Top).
	Render(bottomContent)

	return lipgloss.JoinVertical(lipgloss.Left, topHalf, line, bottomHalf)
}
