package main

import (
	"fmt"
	"time"
	"github.com/charmbracelet/lipgloss"
)

func renderMonthView(m model) string {
	first := time.Date(m.cursor.Year(), m.cursor.Month(), 1, 0, 0, 0, 0, time.Local)
	availWidth := m.width - 22
	
	title := lipgloss.NewStyle().
		Width(availWidth).
		Align(lipgloss.Center).
		Foreground(blue).
		Bold(true).
		PaddingTop(2).
		PaddingBottom(1).
		Render(m.cursor.Format("January 2006"))

	cellWidth := (availWidth / 7) - 2
	cellHeight := (m.height / 8)
	if cellHeight < 3 { cellHeight = 3 }

	cellStyle := lipgloss.NewStyle().Width(cellWidth).Height(cellHeight).Border(lipgloss.NormalBorder()).BorderForeground(gray).Align(lipgloss.Right, lipgloss.Top).Padding(0, 1)

	days := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	var headerRow []string
	for _, d := range days { headerRow = append(headerRow, lipgloss.NewStyle().Width(cellWidth+2).Align(lipgloss.Center).Foreground(purple).Bold(true).Render(d)) }

	var rows []string
	var currentRow []string
	for i := 0; i < int(first.Weekday()); i++ { currentRow = append(currentRow, cellStyle.Render("")) }
	lastDay := first.AddDate(0, 1, -1).Day()
	for day := 1; day <= lastDay; day++ {
		currDate := time.Date(m.cursor.Year(), m.cursor.Month(), day, 0, 0, 0, 0, time.Local)
		style := cellStyle.Copy()
		if currDate.Format("2006-01-02") == time.Now().Format("2006-01-02") { style = style.BorderForeground(purple).Foreground(purple) }
		if m.focusGrid && currDate.Format("2006-01-02") == m.cursor.Format("2006-01-02") { style = style.BorderForeground(blue).Bold(true) }
		
		dayLabel := fmt.Sprintf("%d", day)
		content := style.Render(dayLabel)
		
		// Check for jobs
		dayJobs := GetJobsForDay(m.jobs, currDate)
		if len(dayJobs) > 0 {
			var jobLines []string
			jobLines = append(jobLines, style.Render(dayLabel))
			
			// We can fit maybe 1 or 2 lines depending on height?
			// Cell height is m.height/8. Usually ~3-5 lines.
			// Let's add up to 2 jobs.
			count := 0
			for _, j := range dayJobs {
				if count >= 2 { break }
				label, colorHex := m.config.GetJobDisplay(j.Command)
				// Truncate to cell width - padding
				txt := truncate(label, cellWidth-2)
				jobStyle := lipgloss.NewStyle().Width(cellWidth).Align(lipgloss.Right).Foreground(lipgloss.Color(colorHex))
				jobLines = append(jobLines, jobStyle.Render(txt))
				count++
			}
			content = lipgloss.JoinVertical(lipgloss.Right, jobLines...)
			// We need to re-apply border/size to the container, or just render content inside?
			// The original loop renders `style.Render(dayLabel)`. 
			// `style` has the size and border.
			// So we should construct the inner string first, then Render?
			// No, `style` has Height(cellHeight). If we render a multi-line string, it might overflow or center differently.
			// `style` aligns Right, Top.
			
			// Correct approach:
			// 1. Create inner content (Number \n Job \n Job)
			// 2. Render that with style.
			inner := lipgloss.JoinVertical(lipgloss.Right, dayLabel)
			for _, j := range dayJobs {
				if count >= 3 { break } // Limit jobs displayed
				label, colorHex := m.config.GetJobDisplay(j.Command)
				txt := truncate(label, cellWidth-2)
				jobLine := lipgloss.NewStyle().Foreground(lipgloss.Color(colorHex)).Render(txt)
				inner = lipgloss.JoinVertical(lipgloss.Right, inner, jobLine)
			}
			content = style.Render(inner)
		} else {
			content = style.Render(dayLabel)
		}

		currentRow = append(currentRow, content)
		if len(currentRow) == 7 { rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...)); currentRow = []string{} }
	}
	if len(currentRow) > 0 {
		for len(currentRow) < 7 { currentRow = append(currentRow, cellStyle.Render("")) }
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, lipgloss.JoinHorizontal(lipgloss.Top, headerRow...), lipgloss.JoinVertical(lipgloss.Left, rows...))
}
