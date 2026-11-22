package main

import (
	"os"
	"time"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type sessionState int
const (
	viewAgenda sessionState = iota
	viewMonth
	viewWeek
	viewDay
	viewSettings
)

type model struct {
	state         sessionState
	width, height int
	cursor        time.Time
	focusGrid     bool
	selectedView  int
	viewport      viewport.Model
	ready         bool
	stickyHeader  string
	jobs          []CronJob
	config        Config
	
	input          textinput.Model
	settingsState  int // 0:List, 1:EditAlias, 2:EditColor
	settingsCursor int
}

func (m model) Init() tea.Cmd { return tea.Batch(tea.Tick(time.Minute, func(t time.Time) tea.Msg { return t }), textinput.Blink) }

func updateViewContent(m model) model {
	if m.state == viewWeek {
		h, b := renderWeekView(m)
		m.stickyHeader = h
		m.viewport.SetContent(b)
	} else if m.state == viewDay {
		h, b := renderDayView(m)
		m.stickyHeader = h
		m.viewport.SetContent(b)
	}
	return m
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case time.Time:
		return m, tea.Tick(time.Minute, func(t time.Time) tea.Msg { return t })
	
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		
		// Determine viewport height based on current view
		vpHeight := m.height - 2
		// Week/Day Headers are now Height 4 (PaddingTop 2 + Text 1 + PaddingBottom 1)
		if m.state == viewWeek || m.state == viewDay { vpHeight = m.height - 4 }
		
		if !m.ready {
			m.viewport = viewport.New(m.width-22, vpHeight)
			m.ready = true
		} else {
			m.viewport.Width, m.viewport.Height = m.width-22, vpHeight
		}
		
		m = updateViewContent(m)
		if m.state == viewWeek || m.state == viewDay {
			m.viewport.SetYOffset(time.Now().Hour() * 3)
		}

	case tea.KeyMsg:
		// Global Quit
		if msg.String() == "ctrl+c" { return m, tea.Quit }
		// Allow 'q' to quit ONLY if not editing settings
		if msg.String() == "q" && (m.state != viewSettings || m.settingsState == 0) { return m, tea.Quit }

		// Handle Settings Input (needs priority if editing)
		if m.state == viewSettings && m.settingsState != 0 {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			
			if msg.Type == tea.KeyEnter {
				// Save Logic
				if len(m.jobs) == 0 { return m, nil }
				job := m.jobs[m.settingsCursor]
				
				// Ensure map exists
				if m.config.JobSettings == nil {
					m.config.JobSettings = make(map[string]JobMetadata)
				}
				
				meta := m.config.JobSettings[job.Command] // Get existing or zero value
				
				if m.settingsState == 1 { // Saved Alias
					meta.Alias = m.input.Value()
					m.config.JobSettings[job.Command] = meta
					SaveConfig(m.config)
					
					// Move to Color
					m.settingsState = 2
					_, color := m.config.GetJobDisplay(job.Command)
					m.input.SetValue(color)
					return m, nil
				} else if m.settingsState == 2 { // Saved Color
					meta.Color = m.input.Value()
					m.config.JobSettings[job.Command] = meta
					SaveConfig(m.config)
					
					// Done
					m.settingsState = 0
					m.input.Blur()
					return m, nil
				}
			} else if msg.Type == tea.KeyEsc {
				m.settingsState = 0
				m.input.Blur()
				return m, nil
			}
			return m, cmd
		}

		if msg.String() == "tab" { 
			// If in settings edit mode, cancel edit
			if m.state == viewSettings && m.settingsState != 0 {
				m.settingsState = 0
				m.input.Blur()
			}
			m.focusGrid = !m.focusGrid
			return m, nil 
		}

		if m.focusGrid {
			if m.state == viewSettings {
				switch msg.String() {
				case "up", "k":
					m.settingsCursor--
					if m.settingsCursor < 0 { m.settingsCursor = len(m.jobs) - 1 }
				case "down", "j":
					m.settingsCursor++
					if m.settingsCursor >= len(m.jobs) { m.settingsCursor = 0 }
				case "enter":
					if len(m.jobs) > 0 {
						m.settingsState = 1 // Edit Alias
						alias, _ := m.config.GetJobDisplay(m.jobs[m.settingsCursor].Command)
						m.input.SetValue(alias)
						m.input.Focus()
						return m, textinput.Blink
					}
				}
				return m, nil
			}

			if m.state == viewWeek || m.state == viewDay {
				switch msg.String() {
				case "up", "down", "k", "j", "pgup", "pgdown":
					m.viewport, cmd = m.viewport.Update(msg)
					return m, cmd
				}
			}
			switch msg.String() {
			case "left", "h":
				m.cursor = m.cursor.AddDate(0, 0, -1)
				m = updateViewContent(m)
			case "right", "l":
				m.cursor = m.cursor.AddDate(0, 0, 1)
				m = updateViewContent(m)
			}
		} else {
			switch msg.String() {
			case "up", "k":
				m.selectedView = (m.selectedView - 1 + 5) % 5
				m.state = sessionState(m.selectedView)
				
				vpHeight := m.height - 2
				if m.state == viewWeek || m.state == viewDay { vpHeight = m.height - 4 }
				m.viewport.Height = vpHeight
				
				m = updateViewContent(m)
				if m.state == viewWeek || m.state == viewDay {
					m.viewport.SetYOffset(time.Now().Hour() * 3)
				}
			case "down", "j":
				m.selectedView = (m.selectedView + 1) % 5
				m.state = sessionState(m.selectedView)
				
				vpHeight := m.height - 2
				if m.state == viewWeek || m.state == viewDay { vpHeight = m.height - 4 }
				m.viewport.Height = vpHeight

				m = updateViewContent(m)
				if m.state == viewWeek || m.state == viewDay {
					m.viewport.SetYOffset(time.Now().Hour() * 3)
				}
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	if !m.ready { return "Initializing..." }
	sidebar := renderSidebar(m)
	var content string
	var stickyHeader string
	
	switch m.state {
	case viewAgenda: content = renderAgendaView(m)
	case viewMonth:  content = renderMonthView(m)
	case viewWeek, viewDay:
		content = m.viewport.View()
		stickyHeader = m.stickyHeader
	case viewSettings: content = renderSettingsView(m)
	}
	
	var fullContent string
	if stickyHeader != "" {
		fullContent = lipgloss.JoinVertical(lipgloss.Left, stickyHeader, content)
	} else {
		fullContent = content
	}
	
	mainPanel := mainPanelStyle.
		Width(m.width - 22).
		Height(m.height).
		Align(lipgloss.Left, lipgloss.Top).
		Render(fullContent)

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, mainPanel)
}

func main() {
	var jobs []CronJob
	if _, err := os.Stat("crontab.txt"); err == nil {
		jobs, _ = LoadCronTab("crontab.txt")
	} else {
		jobs, _ = LoadCronTab("")
	}

	config, _ := LoadConfig()
	
	ti := textinput.New()
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(purple)
	ti.CharLimit = 32

	p := tea.NewProgram(model{state: viewAgenda, cursor: time.Now(), focusGrid: false, jobs: jobs, config: config, input: ti}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil { os.Exit(1) }
}
