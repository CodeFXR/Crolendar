package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// version is overridden at release build time via -ldflags "-X main.version=...".
var version = "dev"

type providersLoadedMsg struct {
	gen    uint64
	result ProviderLoadResult
}

type model struct {
	state                sessionState
	width, height        int
	cursor               time.Time
	cursorAnchorDay      int
	focusGrid            bool
	viewport             viewport.Model
	ready                bool
	stickyHeader         string
	jobs                 []JobItem
	providers            []JobProvider
	providerStatuses     []ProviderStatus
	loading              bool
	loadGen              uint64
	overrides            map[JobKey]JobOverride
	input                textinput.Model
	settingsState        int // 0: List, 1: EditAlias, 2: EditColor
	settingsCursor       int
	settingsEditKey      JobKey
	settingsInitialAlias string
	settingsDraftAlias   string
	settingsError        string
	settingsNotice       string
}

func newModel(jobs []JobItem) model {
	return newModelWithProviders(jobs, nil)
}

func newModelWithProviders(jobs []JobItem, providers []JobProvider) model {
	ti := textinput.New()
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(purple)
	ti.CharLimit = 48
	ti.Width = 24

	m := model{
		state:     viewAgenda,
		cursor:    time.Now(),
		jobs:      jobs,
		providers: providers,
		overrides: make(map[JobKey]JobOverride),
		input:     ti,
	}
	if len(providers) > 0 {
		m.loading = true
		m.loadGen = 1
	}
	return m
}

func loadProvidersCmd(providers []JobProvider, gen uint64) tea.Cmd {
	list := append([]JobProvider(nil), providers...)
	return func() tea.Msg {
		return providersLoadedMsg{
			gen:    gen,
			result: LoadProviders(context.Background(), list),
		}
	}
}

func (m model) effectiveJob(j JobItem) JobItem {
	if !j.HasStableKey() {
		return j
	}
	if ov, ok := m.overrides[j.Key()]; ok {
		if ov.HasAlias {
			j.Alias = ov.Alias
		}
		if ov.Color != "" {
			j.Color = ov.Color
		}
	}
	return j
}

func (m model) findJobByKey(key JobKey) (JobItem, bool) {
	for _, j := range m.jobs {
		if j.HasStableKey() && j.Key() == key {
			return j, true
		}
	}
	return JobItem{}, false
}

func (m model) hasDuplicateKey(key JobKey) bool {
	count := 0
	for _, j := range m.jobs {
		if j.HasStableKey() && j.Key() == key {
			count++
			if count > 1 {
				return true
			}
		}
	}
	return false
}

func stripUnsafeInput(s string) string {
	if s == "" {
		return ""
	}
	noEsc := ansiEscapeRe.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(noEsc))
	for _, r := range noEsc {
		if isBlockedControlRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (m *model) sanitizeLiveInput() {
	raw := m.input.Value()
	if raw == "" {
		return
	}
	clean := stripUnsafeInput(raw)
	if clean == raw {
		return
	}
	rawRunes := []rune(raw)
	pos := clampInt(m.input.Position(), 0, len(rawRunes))
	cleanPrefix := stripUnsafeInput(string(rawRunes[:pos]))
	m.input.Reset()
	m.input.SetValue(clean)
	m.input.SetCursor(len([]rune(cleanPrefix)))
}

func (m model) Init() tea.Cmd {
	tick := tea.Tick(time.Minute, func(t time.Time) tea.Msg { return t })
	if len(m.providers) > 0 {
		return tea.Batch(tick, loadProvidersCmd(m.providers, m.loadGen))
	}
	return tick
}

func (m model) layoutDimensions() (bodyHeight, availWidth int) {
	bodyHeight = m.height - 2 // reserve 2 lines for footer (border + status)
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	availWidth = m.width - sidebarFullWidth
	if availWidth < 1 {
		availWidth = 1
	}
	return bodyHeight, availWidth
}

func (m model) viewportHeight() int {
	bodyHeight, _ := m.layoutDimensions()
	vpHeight := bodyHeight - 3 // sticky Week/Day header is 3 lines
	if vpHeight < 1 {
		vpHeight = 1
	}
	return vpHeight
}

func (m *model) syncInputWidth() {
	_, availWidth := m.layoutDimensions()
	w := availWidth - 17
	if w < 5 {
		w = 5
	}
	if w > 48 {
		w = 48
	}
	if m.input.Width != w {
		m.input.Width = w
		if m.settingsState != 0 {
			pos := m.input.Position()
			val := m.input.Value()
			m.input.Reset()
			m.input.SetValue(val)
			m.input.SetCursor(pos)
		}
	}
}

func updateViewContent(m model) model {
	if !m.ready || m.width < minTermWidth || m.height < minTermHeight {
		return m
	}
	_, availWidth := m.layoutDimensions()
	switch m.state {
	case viewWeek:
		h, b := renderWeekView(m, availWidth)
		m.stickyHeader = h
		m.viewport.SetContent(b)
	case viewDay:
		h, b := renderDayView(m, availWidth)
		m.stickyHeader = h
		m.viewport.SetContent(b)
	default:
		m.stickyHeader = ""
	}
	return m
}

func (m model) switchView(next sessionState, scrollToNow bool) model {
	m.state = next
	m.settingsState = 0
	m.settingsEditKey = JobKey{}
	m.settingsInitialAlias = ""
	m.settingsDraftAlias = ""
	m.settingsError = ""
	m.settingsNotice = ""
	m.input.Blur()
	if m.ready {
		_, availWidth := m.layoutDimensions()
		m.viewport.Width = availWidth
		m.viewport.Height = m.viewportHeight()
		m.syncInputWidth()
		m = updateViewContent(m)
		if scrollToNow && (m.state == viewWeek || m.state == viewDay) {
			m.scrollViewportToCurrentHour()
		}
	}
	return m
}

func (m *model) scrollViewportToCurrentHour() {
	hour := time.Now().Hour() - 1
	if hour < 0 {
		hour = 0
	}
	m.viewport.SetYOffset(hour * 3)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case providersLoadedMsg:
		if msg.gen < m.loadGen {
			return m, nil
		}
		m.loading = false
		m.jobs = msg.result.Jobs
		m.providerStatuses = msg.result.Statuses
		if len(m.jobs) == 0 {
			m.settingsCursor = 0
		} else {
			m.settingsCursor = clampInt(m.settingsCursor, 0, len(m.jobs)-1)
		}
		if m.settingsState != 0 {
			if _, ok := m.findJobByKey(m.settingsEditKey); !ok {
				m.settingsState = 0
				m.settingsEditKey = JobKey{}
				m.settingsInitialAlias = ""
				m.settingsDraftAlias = ""
				m.settingsError = "Job removed during refresh"
				m.input.Blur()
			}
		}
		m = updateViewContent(m)
		return m, nil

	case time.Time:
		m = updateViewContent(m)
		return m, tea.Tick(time.Minute, func(t time.Time) tea.Msg { return t })

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		_, availWidth := m.layoutDimensions()
		vpHeight := m.viewportHeight()

		if !m.ready {
			m.viewport = viewport.New(availWidth, vpHeight)
			m.ready = true
			m.syncInputWidth()
			m = updateViewContent(m)
			if m.state == viewWeek || m.state == viewDay {
				m.scrollViewportToCurrentHour()
			}
		} else {
			m.viewport.Width = availWidth
			m.viewport.Height = vpHeight
			m.syncInputWidth()
			m = updateViewContent(m)
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// Handle Settings inline text editing first so keys like 'q' or numbers type normally.
		if m.state == viewSettings && m.settingsState != 0 {
			return m.handleSettingsInput(msg)
		}

		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "1":
			return m.switchView(viewAgenda, false), nil
		case "2":
			return m.switchView(viewMonth, false), nil
		case "3":
			return m.switchView(viewWeek, true), nil
		case "4":
			return m.switchView(viewDay, true), nil
		case "5":
			return m.switchView(viewSettings, false), nil
		case "r":
			if len(m.providers) > 0 && !m.loading {
				m.loadGen++
				m.loading = true
				return m, loadProvidersCmd(m.providers, m.loadGen)
			}
			return m, nil
		case "t":
			if m.state != viewSettings {
				m.cursor = time.Now()
				m.cursorAnchorDay = 0
				m = updateViewContent(m)
				if m.state == viewWeek || m.state == viewDay {
					m.scrollViewportToCurrentHour()
				}
			}
			return m, nil
		case "tab":
			m.focusGrid = !m.focusGrid
			m.settingsError = ""
			m.settingsNotice = ""
			m = updateViewContent(m)
			return m, nil
		}

		if !m.focusGrid {
			return m.handleSidebarKey(msg)
		}
		return m.handleGridKey(msg)

	default:
		// Forward non-key messages (such as cursor.BlinkMsg or clipboard pasteMsg)
		// to textinput when editing, scrubbing any unsafe escape/format runes from paste.
		if m.state == viewSettings && m.settingsState != 0 {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.sanitizeLiveInput()
			return m, cmd
		}
	}
	return m, nil
}

func (m model) handleSettingsInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.settingsState = 0
		m.settingsEditKey = JobKey{}
		m.settingsInitialAlias = ""
		m.settingsDraftAlias = ""
		m.settingsError = ""
		m.input.Blur()
		return m, nil
	}

	if msg.Type == tea.KeyEnter {
		rawJob, ok := m.findJobByKey(m.settingsEditKey)
		if !ok {
			m.settingsState = 0
			m.settingsEditKey = JobKey{}
			m.settingsInitialAlias = ""
			m.settingsDraftAlias = ""
			m.settingsError = "Job no longer available"
			m.input.Blur()
			return m, nil
		}
		job := m.effectiveJob(rawJob)
		if m.settingsState == 1 {
			m.settingsDraftAlias = sanitizeText(m.input.Value())
			m.settingsError = ""
			m.settingsState = 2
			m.input.SetValue(job.DisplayColor())
			m.input.CursorEnd()
			return m, nil
		}
		if m.settingsState == 2 {
			hex, ok := normalizeHexColor(m.input.Value())
			if !ok {
				m.settingsError = "Invalid color: use #RGB or #RRGGBB"
				return m, nil
			}
			if m.overrides == nil {
				m.overrides = make(map[JobKey]JobOverride)
			}
			prevOv, hadPrev := m.overrides[m.settingsEditKey]
			hasAlias := true
			aliasVal := m.settingsDraftAlias
			if m.settingsDraftAlias == sanitizeText(m.settingsInitialAlias) {
				if hadPrev {
					hasAlias = prevOv.HasAlias
					aliasVal = prevOv.Alias
				} else {
					hasAlias = false
					aliasVal = ""
				}
			} else if m.settingsDraftAlias == "" {
				// Empty alias clears any provider-set alias so Title shows.
				hasAlias = sanitizeText(rawJob.Alias) != ""
				aliasVal = ""
			}
			m.overrides[m.settingsEditKey] = JobOverride{
				Alias:    aliasVal,
				HasAlias: hasAlias,
				Color:    hex,
			}
			m.settingsEditKey = JobKey{}
			m.settingsInitialAlias = ""
			m.settingsDraftAlias = ""
			m.settingsError = ""
			m.settingsNotice = "Updated in-memory settings"
			m.settingsState = 0
			m.input.Blur()
			return m, nil
		}
	}

	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		filtered := make([]rune, 0, len(msg.Runes))
		for _, r := range msg.Runes {
			if !isBlockedControlRune(r) {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			return m, nil
		}
		msg.Runes = filtered
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.sanitizeLiveInput()
	m.settingsError = ""
	return m, cmd
}

func (m model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		next := sessionState((int(m.state) - 1 + 5) % 5)
		return m.switchView(next, true), nil
	case "down", "j":
		next := sessionState((int(m.state) + 1) % 5)
		return m.switchView(next, true), nil
	case "enter", "right", "l":
		m.focusGrid = true
		m = updateViewContent(m)
		return m, nil
	}
	return m, nil
}

func (m model) handleGridKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.state == viewSettings {
		switch msg.String() {
		case "up", "k":
			if len(m.jobs) > 0 {
				m.settingsCursor = (m.settingsCursor - 1 + len(m.jobs)) % len(m.jobs)
				m.settingsError = ""
				m.settingsNotice = ""
			}
		case "down", "j":
			if len(m.jobs) > 0 {
				m.settingsCursor = (m.settingsCursor + 1) % len(m.jobs)
				m.settingsError = ""
				m.settingsNotice = ""
			}
		case "enter":
			if len(m.jobs) > 0 {
				idx := clampInt(m.settingsCursor, 0, len(m.jobs)-1)
				m.settingsCursor = idx
				rawJob := m.jobs[idx]
				if !rawJob.HasStableKey() {
					m.settingsError = "Cannot edit job without ID"
					m.settingsNotice = ""
					return m, nil
				}
				if m.hasDuplicateKey(rawJob.Key()) {
					m.settingsError = "Duplicate job key; cannot edit"
					m.settingsNotice = ""
					return m, nil
				}
				job := m.effectiveJob(rawJob)
				prefill := ansi.Truncate(job.DisplayName(), m.input.CharLimit, "")
				m.settingsState = 1
				m.settingsEditKey = rawJob.Key()
				m.settingsInitialAlias = prefill
				m.settingsDraftAlias = job.DisplayName()
				m.settingsError = ""
				m.settingsNotice = ""
				m.syncInputWidth()
				m.input.SetValue(prefill)
				m.input.CursorEnd()
				return m, m.input.Focus()
			}
		}
		return m, nil
	}

	if m.state == viewWeek || m.state == viewDay {
		switch msg.String() {
		case "up", "down", "k", "j", "pgup", "pgdown":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case "H", "[":
			m.cursor = m.cursor.AddDate(0, 0, -7)
			m.cursorAnchorDay = 0
			m = updateViewContent(m)
			return m, nil
		case "L", "]":
			m.cursor = m.cursor.AddDate(0, 0, 7)
			m.cursorAnchorDay = 0
			m = updateViewContent(m)
			return m, nil
		}
	}

	switch msg.String() {
	case "left", "h":
		m.cursor = m.cursor.AddDate(0, 0, -1)
		m.cursorAnchorDay = 0
		m = updateViewContent(m)
	case "right", "l":
		m.cursor = m.cursor.AddDate(0, 0, 1)
		m.cursorAnchorDay = 0
		m = updateViewContent(m)
	case "up", "k":
		m.cursor = m.cursor.AddDate(0, 0, -7)
		m.cursorAnchorDay = 0
		m = updateViewContent(m)
	case "down", "j":
		m.cursor = m.cursor.AddDate(0, 0, 7)
		m.cursorAnchorDay = 0
		m = updateViewContent(m)
	case "pgup":
		if m.cursorAnchorDay == 0 {
			m.cursorAnchorDay = m.cursor.Day()
		}
		m.cursor = addMonthsWithAnchor(m.cursor, -1, m.cursorAnchorDay)
		m = updateViewContent(m)
	case "pgdown":
		if m.cursorAnchorDay == 0 {
			m.cursorAnchorDay = m.cursor.Day()
		}
		m.cursor = addMonthsWithAnchor(m.cursor, 1, m.cursorAnchorDay)
		m = updateViewContent(m)
	}
	return m, nil
}

func (m model) renderFooter() string {
	focusLabel := "SIDEBAR"
	focusColor := purple
	if m.focusGrid {
		focusLabel = "GRID"
		focusColor = blue
	}
	badge := lipgloss.NewStyle().
		Foreground(white).
		Background(focusColor).
		Bold(true).
		Padding(0, 1).
		Render(focusLabel)

	hints := "Tab: focus  1-5: view  t: today  Arrows/hjkl: move  q: quit"
	if m.state == viewSettings {
		if m.settingsState != 0 {
			hints = "Enter: next/save  Esc: cancel"
		} else if len(m.providers) > 0 {
			hints = "Tab: focus  1-5: view  j/k: select  Enter: edit  r: reload  q: quit"
		} else {
			hints = "Tab: focus  1-5: view  j/k: select  Enter: edit  q: quit"
		}
	} else if m.state == viewWeek || m.state == viewDay {
		hints = "Tab: focus  1-5: view  t: today  h/l: day  j/k: scroll  q: quit"
	} else if len(m.providers) > 0 {
		hints = "Tab: focus  1-5: view  t: today  r: reload  hjkl: move  q: quit"
	}

	statusNote := ""
	statusIsErr := false
	if m.loading {
		statusNote = "Loading..."
	} else {
		for _, st := range m.providerStatuses {
			if st.Err != nil {
				statusNote = fmt.Sprintf("%s error: %s", st.Name, sanitizeText(st.Err.Error()))
				statusIsErr = true
				break
			}
		}
	}

	dateInfo := lipgloss.NewStyle().Foreground(softWhite).Render(m.cursor.Format("Mon 02 Jan 2006"))
	prefix := fmt.Sprintf(" %s  %s  ", badge, dateInfo)
	contentWidth := m.width - 2 // Padding(0, 1)
	if contentWidth < 1 {
		contentWidth = 1
	}
	remWidth := contentWidth - lipgloss.Width(prefix)
	var line string
	if remWidth <= 0 {
		line = prefix
	} else if statusNote != "" {
		renderedNote := truncateWidth(statusNote, remWidth)
		if statusIsErr {
			line = prefix + errorTextStyle.Render(renderedNote)
		} else {
			line = prefix + okTextStyle.Render(renderedNote)
		}
	} else {
		line = prefix + truncateWidth(hints, remWidth)
	}
	return footerStyle.
		Width(m.width).
		MaxHeight(2).
		Render(line)
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}
	if m.width < minTermWidth || m.height < minTermHeight {
		msg := fmt.Sprintf("Terminal too small (%dx%d). Minimum: %dx%d.", m.width, m.height, minTermWidth, minTermHeight)
		return lipgloss.NewStyle().
			Width(m.width).
			Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(mutedGray).
			Render(msg)
	}

	bodyHeight, availWidth := m.layoutDimensions()
	sidebar := renderSidebar(m, bodyHeight)

	var content string
	switch m.state {
	case viewAgenda:
		content = renderAgendaView(m, availWidth, bodyHeight)
	case viewMonth:
		content = renderMonthView(m, availWidth, bodyHeight)
	case viewWeek, viewDay:
		content = lipgloss.JoinVertical(lipgloss.Left, m.stickyHeader, m.viewport.View())
	case viewSettings:
		content = renderSettingsView(m, availWidth, bodyHeight)
	}

	mainPanel := mainPanelStyle.
		Width(availWidth).
		Height(bodyHeight).
		MaxHeight(bodyHeight).
		Align(lipgloss.Left, lipgloss.Top).
		Render(content)

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, mainPanel)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.renderFooter())
}

func main() {
	demo := flag.Bool("demo", false, "Populate UI with sample scheduled jobs")
	crontabPath := flag.String("crontab", "", "Read cron jobs from a crontab file path (read-only)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("crolendar %s\n", version)
		return
	}

	var m model
	if *demo {
		m = newModel(demoJobs())
	} else {
		providers := []JobProvider{NewCronProvider(*crontabPath)}
		m = newModelWithProviders(nil, providers)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "crolendar error: %v\n", err)
		os.Exit(1)
	}
}
