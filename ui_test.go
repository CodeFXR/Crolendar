package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestSanitizeTextStripsEscapesControlsAndUnicodeSpoofing(t *testing.T) {
	dirty := "\x1b[31mEvil\x1b[0m\x1b]0;TitleHijack\x07\x1bPignored\x1b\\\u009B31mc1\u202Espoof\u200B\u2066\uFEFF\n\tNext\rLine\x00"
	got := sanitizeText(dirty)
	for _, r := range []rune{'\x1b', '\x9B', '\n', '\r', '\x00', '\u202E', '\u200B', '\u2066', '\uFEFF'} {
		if strings.ContainsRune(got, r) {
			t.Fatalf("sanitizeText leaked rune %U in %q", r, got)
		}
	}
	if got != "Evilc1spoof Next Line" {
		t.Fatalf("unexpected sanitized output: %q", got)
	}

	family := sanitizeText("👨‍👩‍👧‍👦 family")
	if !strings.ContainsRune(family, '\u200D') {
		t.Fatalf("expected sanitizeText to preserve ZWJ in %q", family)
	}
	if w := ansi.StringWidth("👨‍👩‍👧‍👦"); w != 2 {
		t.Fatalf("expected family emoji width 2, got %d", w)
	}
}

func TestTruncateWidthMatchesLipglossAcrossGraphemes(t *testing.T) {
	cases := []string{
		"Database — Backup 日本語",
		"🇺🇸 flag",
		"👍🏽 thumb",
		"e\u0301\u0301 combining",
		"👨‍👩‍👧‍👦 family",
	}
	for _, in := range cases {
		for _, max := range []int{4, 6, 8, 12} {
			got := truncateWidth(in, max)
			if lipgloss.Width(got) > max {
				t.Fatalf("lipgloss.Width(%q)=%d exceeds max=%d (input=%q)", got, lipgloss.Width(got), max, in)
			}
			if ansi.StringWidth(got) > max {
				t.Fatalf("ansi.StringWidth(%q)=%d exceeds max=%d", got, ansi.StringWidth(got), max)
			}
		}
	}
	if truncateWidth("abc", 0) != "" {
		t.Fatalf("expected empty string for maxWidth 0")
	}
	if truncateWidth("abc", 1) != "a" {
		t.Fatalf("expected single char for maxWidth 1, got %q", truncateWidth("abc", 1))
	}
}

func TestNormalizeHexColor(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"#abc", "#AABBCC", true},
		{" #7d56f4 ", "#7D56F4", true},
		{"#GGGGGG", "", false},
		{"0052D4", "", false},
		{"#12345", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeHexColor(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("normalizeHexColor(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRenderAllViewsAcrossSizesBoundedAndMonthComplete(t *testing.T) {
	sizes := [][2]int{
		{20, 8},   // smaller than min
		{44, 14},  // exact minimum
		{60, 24},  // narrow terminal (compact month)
		{63, 14},  // threshold width but short height (must compact month)
		{65, 14},  // wide enough for colWidth=6, short height (must compact month)
		{80, 18},  // standard width, short height
		{80, 24},  // standard terminal
		{140, 42}, // wide terminal
	}
	views := []sessionState{viewAgenda, viewMonth, viewWeek, viewDay, viewSettings}
	cursors := []time.Time{
		time.Date(2026, 1, 15, 10, 0, 0, 0, time.Local), // 5-week month
		time.Date(2026, 5, 15, 10, 0, 0, 0, time.Local), // 6-week month
	}

	for _, jobs := range [][]JobItem{nil, demoJobs()} {
		for _, sz := range sizes {
			for _, cur := range cursors {
				for _, v := range views {
					m := newModel(jobs)
					m.cursor = cur
					next, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
					m = next.(model)
					m = m.switchView(v, true)

					out := m.View()
					if out == "" {
						t.Fatalf("empty output for view=%v size=%v", v, sz)
					}
					if sz[0] >= minTermWidth && sz[1] >= minTermHeight {
						if h := lipgloss.Height(out); h > sz[1] {
							t.Fatalf("view %v overflowed height at %dx%d: got %d lines", v, sz[0], sz[1], h)
						}
						if w := lipgloss.Width(out); w > sz[0] {
							t.Fatalf("view %v overflowed width at %dx%d: got %d cols", v, sz[0], sz[1], w)
						}
						if v == viewMonth {
							plainLines := strings.Split(ansi.Strip(out), "\n")
							gridOnly := ""
							if len(plainLines) > 2 {
								gridOnly = strings.Join(plainLines[2:], "\n")
							}
							for day := 1; day <= 31; day++ {
								re := regexp.MustCompile(fmt.Sprintf(`\b%d\b`, day))
								if !re.MatchString(gridOnly) {
									t.Fatalf("month view (%s) at %dx%d missing day %d in grid", cur.Format("2006-01"), sz[0], sz[1], day)
								}
							}
						}
					}
				}
			}
		}
	}

	// Agenda with maxEventLines==1 and multiple jobs must still indicate overflow inline.
	mAgenda := newModel([]JobItem{
		{ID: "a1", Title: "Alpha", ScheduleLabel: "Daily", MatchesDay: func(time.Time) bool { return true }},
		{ID: "a2", Title: "Beta", ScheduleLabel: "Daily", MatchesDay: func(time.Time) bool { return true }},
		{ID: "a3", Title: "Gamma", ScheduleLabel: "Daily", MatchesDay: func(time.Time) bool { return true }},
	})
	mAgenda.cursor = time.Date(2026, 1, 15, 10, 0, 0, 0, time.Local)
	next, _ := mAgenda.Update(tea.WindowSizeMsg{Width: 44, Height: 14})
	mAgenda = next.(model)
	outAgenda := ansi.Strip(mAgenda.View())
	if !strings.Contains(outAgenda, "(+2") {
		t.Fatalf("expected inline overflow marker on 44x14 Agenda, got:\n%s", outAgenda)
	}
}

func TestWeekAndDayGridUniformRowHeightsAndNegativeGuard(t *testing.T) {
	// Must never panic even if formatHourSlotLines receives negative width.
	l1, l2 := formatHourSlotLines(demoJobs(), -1, false)
	if l1 != "" || l2 != "" {
		t.Fatalf("expected empty lines for negative width, got %q %q", l1, l2)
	}

	m := newModel(demoJobs())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(model)

	_, availW := m.layoutDimensions()
	wh, wb := renderWeekView(m, availW)
	if lipgloss.Height(wh) != 3 || lipgloss.Height(wb) != 73 {
		t.Fatalf("unexpected week dimensions: header=%d body=%d", lipgloss.Height(wh), lipgloss.Height(wb))
	}

	dh, db := renderDayView(m, availW)
	if lipgloss.Height(dh) != 3 || lipgloss.Height(db) != 73 {
		t.Fatalf("unexpected day dimensions: header=%d body=%d", lipgloss.Height(dh), lipgloss.Height(db))
	}
}

func TestMonthClampAndDSTWallHour(t *testing.T) {
	jan31 := time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)
	feb := addMonths(jan31, 1)
	if feb.Month() != time.February || feb.Day() != 28 {
		t.Fatalf("expected Feb 28 from Jan 31 + 1 month, got %v", feb)
	}
	mar31 := time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	febBack := addMonths(mar31, -1)
	if febBack.Month() != time.February || febBack.Day() != 28 {
		t.Fatalf("expected Feb 28 from Mar 31 - 1 month, got %v", febBack)
	}

	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("America/Denver tzdata unavailable: %v", err)
	}
	springForwardDay := time.Date(2026, 3, 8, 0, 0, 0, 0, denver)
	if _, ok := wallHourTime(springForwardDay, 2); ok {
		t.Fatalf("expected nonexistent 02:00 spring-forward hour to return ok=false")
	}
	if slot, ok := wallHourTime(springForwardDay, 3); !ok || slot.Hour() != 3 {
		t.Fatalf("expected 03:00 spring-forward hour to be valid, got %v ok=%v", slot, ok)
	}
}

func TestSettingsAtomicCancelAndKeyedOverrides(t *testing.T) {
	// Empty jobs must never crash or underflow settingsCursor.
	mEmpty := newModel(nil)
	next, _ := mEmpty.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mEmpty = next.(model)
	mEmpty.state = viewSettings
	mEmpty.focusGrid = true
	next, _ = mEmpty.Update(tea.KeyMsg{Type: tea.KeyUp})
	mEmpty = next.(model)
	if mEmpty.settingsCursor != 0 {
		t.Fatalf("expected settingsCursor 0 on empty jobs, got %d", mEmpty.settingsCursor)
	}

	m := newModel(demoJobs())
	next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(model)
	m = m.switchView(viewSettings, false)
	m.focusGrid = true

	// Start editing job 0 alias -> change to "Temp", advance to color, then press Esc.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m.input.SetValue("Temp Alias")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.settingsState != 2 {
		t.Fatalf("expected settingsState 2, got %d", m.settingsState)
	}
	// Esc must discard the pending alias draft.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.settingsState != 0 || m.effectiveJob(m.jobs[0]).DisplayName() != "DB Backup" {
		t.Fatalf("expected Esc to rollback draft alias, got state=%d name=%q", m.settingsState, m.effectiveJob(m.jobs[0]).DisplayName())
	}

	// Now complete a full edit and confirm override survives job reordering.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m.input.SetValue("Renamed Backup")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m.input.SetValue("#3af")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.settingsState != 0 {
		t.Fatalf("expected settingsState 0 after valid save, got %d", m.settingsState)
	}

	// Reorder jobs slice (simulate provider refresh): override follows Source+ID, not index.
	m.jobs[0], m.jobs[1] = m.jobs[1], m.jobs[0]
	eff := m.effectiveJob(m.jobs[1])
	if eff.DisplayName() != "Renamed Backup" || eff.Color != "#33AAFF" {
		t.Fatalf("expected override keyed by (Source, ID) to follow reordered job, got %+v", eff)
	}
}

func TestJobItemDefaultsAndHourOnlyOccurrence(t *testing.T) {
	j := JobItem{Title: "\x1b[31mTest\x1b[0m", Color: "bad"}
	if j.DisplayName() != "Test" {
		t.Fatalf("expected sanitized DisplayName Test, got %q", j.DisplayName())
	}
	if j.DisplayColor() != defaultJobColor {
		t.Fatalf("expected fallback color %s, got %s", defaultJobColor, j.DisplayColor())
	}
	if j.OccursOnDay(time.Now()) || j.OccursInHour(time.Now()) {
		t.Fatalf("expected false when matchers are nil")
	}

	// Hour-only job must also surface in day queries (Month/Agenda).
	hourOnly := JobItem{
		ID:          "h1",
		Source:      "cron",
		Title:       "Hourly Task",
		MatchesHour: func(h time.Time) bool { return h.Hour() == 15 },
	}
	if !hourOnly.OccursOnDay(time.Now()) {
		t.Fatalf("expected hour-only job to report OccursOnDay=true")
	}

	// Day-only job must also surface in Week/Day (00:00 slot).
	dayOnly := JobItem{
		ID:         "d1",
		Source:     "cron",
		Title:      "Day Only",
		MatchesDay: func(d time.Time) bool { return d.Weekday() == time.Wednesday },
	}
	wed := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)
	if !dayOnly.OccursInHour(wed) {
		t.Fatalf("expected day-only job to match 00:00 hour slot")
	}
	weekStart := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC) // Sunday
	wGrid := indexWeekJobs([]JobItem{dayOnly}, weekStart)
	if len(wGrid[3][0]) != 1 {
		t.Fatalf("expected day-only job on Wednesday 00:00 in indexWeekJobs, got %d", len(wGrid[3][0]))
	}
}

func TestDSTWeekIndexingAndFallBackFoldConsistency(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("America/Denver tzdata unavailable: %v", err)
	}

	// Spring-forward week (2026-03-08): 23-hour Sunday must not shift Mon/Tue 00:00 backward.
	springStart := time.Date(2026, 3, 8, 0, 0, 0, 0, denver)
	springMonMidnight := time.Date(2026, 3, 9, 0, 0, 0, 0, denver)
	springTueMidnight := time.Date(2026, 3, 10, 0, 0, 0, 0, denver)
	springJob := JobItem{
		ID:     "spring",
		Source: "cron",
		OccurrencesInRange: func(start, end time.Time) []time.Time {
			return []time.Time{springMonMidnight, springTueMidnight}
		},
	}
	sGrid := indexWeekJobs([]JobItem{springJob}, springStart)
	if len(sGrid[0][0]) != 0 || len(sGrid[1][0]) != 1 || len(sGrid[2][0]) != 1 {
		t.Fatalf("spring-forward column shift: sun=%d mon=%d tue=%d", len(sGrid[0][0]), len(sGrid[1][0]), len(sGrid[2][0]))
	}

	// Fall-back week (2026-11-01): 25-hour Sunday must not shift 23:30 into Monday,
	// and second fold (01:30 MST -07:00) must match both Week and OccursInHour.
	fallStart := time.Date(2026, 11, 1, 0, 0, 0, 0, denver)
	fallSunLate := time.Date(2026, 11, 1, 23, 30, 0, 0, denver)
	fallSecondFold := time.Date(2026, 11, 1, 1, 30, 0, 0, denver).Add(time.Hour) // 01:30 MST (-07:00)
	fallJob := JobItem{
		ID:     "fall",
		Source: "cron",
		OccurrencesInRange: func(start, end time.Time) []time.Time {
			var out []time.Time
			for _, occ := range []time.Time{fallSecondFold, fallSunLate} {
				if !occ.Before(start) && occ.Before(end) {
					out = append(out, occ)
				}
			}
			return out
		},
	}
	fGrid := indexWeekJobs([]JobItem{fallJob}, fallStart)
	if len(fGrid[0][23]) != 1 || len(fGrid[1][23]) != 0 {
		t.Fatalf("fall-back column shift for 23:30: sun=%d mon=%d", len(fGrid[0][23]), len(fGrid[1][23]))
	}
	if len(fGrid[0][1]) != 1 {
		t.Fatalf("expected fall-back second fold in indexWeekJobs[0][1], got %d", len(fGrid[0][1]))
	}
	if !fallJob.OccursInHour(time.Date(2026, 11, 1, 1, 0, 0, 0, denver)) {
		t.Fatalf("expected OccursInHour to detect second fall-back fold 01:30 MST")
	}
}

func TestWeekAndDayCurrentHourPreservesMultipleJobs(t *testing.T) {
	now := time.Now()
	jobs := []JobItem{
		{
			ID: "j1", Title: "AlphaJob", Color: "#FF0000",
			MatchesDay:  func(d time.Time) bool { return sameDay(d, now) },
			MatchesHour: func(h time.Time) bool { return sameDay(h, now) && h.Hour() == now.Hour() },
		},
		{
			ID: "j2", Title: "BetaJob", Color: "#00FF00",
			MatchesDay:  func(d time.Time) bool { return sameDay(d, now) },
			MatchesHour: func(h time.Time) bool { return sameDay(h, now) && h.Hour() == now.Hour() },
		},
	}
	m := newModel(jobs)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(model)

	_, dayBody := renderDayView(m, 79)
	plainDay := ansi.Strip(dayBody)
	if !strings.Contains(plainDay, "AlphaJob") || !strings.Contains(plainDay, "BetaJob") {
		t.Fatalf("expected both jobs to remain visible during current hour in Day view")
	}

	_, weekBody := renderWeekView(m, 79)
	plainWeek := ansi.Strip(weekBody)
	if !strings.Contains(plainWeek, "AlphaJob") && !strings.Contains(plainWeek, "Alpha") {
		t.Fatalf("expected first job visible in Week view")
	}
	if !strings.Contains(plainWeek, "Beta") && !strings.Contains(plainWeek, "+1") {
		t.Fatalf("expected second job or +N more preserved during current hour in Week view")
	}
}

func TestSettingsKeyCaptureBlankAliasNoIDAndResizeDuringEdit(t *testing.T) {
	// Non-ID job must be rejected safely in Settings.
	mNoID := newModel([]JobItem{{Title: "NoIDJob", Source: "cron"}})
	next, _ := mNoID.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mNoID = next.(model)
	mNoID = mNoID.switchView(viewSettings, false)
	mNoID.focusGrid = true
	next, _ = mNoID.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mNoID = next.(model)
	if mNoID.settingsState != 0 || mNoID.settingsError == "" {
		t.Fatalf("expected job without ID to block edit cleanly, got state=%d err=%q", mNoID.settingsState, mNoID.settingsError)
	}

	jobs := []JobItem{
		{ID: "1", Source: "cron", Title: "Raw Title One", Alias: "Provider Alias One"},
		{ID: "2", Source: "systemd", Title: "Raw Title Two"},
	}
	m := newModel(jobs)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(model)
	m = m.switchView(viewSettings, false)
	m.focusGrid = true

	// Start editing job 0 -> enter edit -> clear alias -> BiDi rune filtered -> reorder jobs mid-edit -> save.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\u202E'}})
	m = next.(model)
	if strings.ContainsRune(m.input.Value(), '\u202E') {
		t.Fatalf("expected BiDi rune to be filtered from live textinput")
	}
	m.input.SetValue("")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)

	// Reorder slice AND shrink terminal to minimum 44x14 mid-edit.
	m.jobs[0], m.jobs[1] = m.jobs[1], m.jobs[0]
	next, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 14})
	m = next.(model)
	outMin := m.View()
	if lipgloss.Width(outMin) > 44 || lipgloss.Height(outMin) > 14 {
		t.Fatalf("resize during Settings edit overflowed 44x14: got %dx%d", lipgloss.Width(outMin), lipgloss.Height(outMin))
	}

	m.input.SetValue("#123456")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)

	// Override must have updated key ("cron", "1") now at index 1, clearing provider alias back to Title.
	eff1 := m.effectiveJob(m.jobs[1])
	if eff1.DisplayName() != "Raw Title One" || eff1.Color != "#123456" {
		t.Fatalf("expected blank alias to clear provider alias on captured key, got %+v", eff1)
	}
	eff0 := m.effectiveJob(m.jobs[0])
	if eff0.DisplayName() != "Raw Title Two" || eff0.Color != "" {
		t.Fatalf("expected unrelated job untouched after mid-edit reorder, got %+v", eff0)
	}
}

func TestTimeZoneDurationCapZWNJAndMonthAnchor(t *testing.T) {
	// Preserve ZWNJ (U+200C) in non-Latin titles while blocking BiDi overrides.
	persian := sanitizeText("نیم\u200Cفاصله\u202E")
	if !strings.ContainsRune(persian, '\u200C') || strings.ContainsRune(persian, '\u202E') {
		t.Fatalf("expected ZWNJ preserved and RLO stripped, got %q", persian)
	}

	// Job with TimeZone="UTC" running at 00:00 UTC must appear at 17:00 MST in Denver.
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("America/Denver tzdata unavailable: %v", err)
	}
	utcJob := JobItem{
		ID:          "utc-job",
		Source:      "k8s",
		Title:       "UTC Midnight",
		TimeZone:    "UTC",
		MatchesDay:  func(d time.Time) bool { return true },
		MatchesHour: func(h time.Time) bool { return h.Hour() == 0 },
	}
	denverDay := time.Date(2026, 1, 15, 0, 0, 0, 0, denver) // MST = UTC-7
	dayHours := indexDayHourJobs([]JobItem{utcJob}, denverDay)
	if len(dayHours[17]) != 1 || len(dayHours[0]) != 0 {
		t.Fatalf("expected UTC 00:00 job at 17:00 Denver, got h17=%d h0=%d", len(dayHours[17]), len(dayHours[0]))
	}

	// Job starting at 23:00 with Duration=3h must overlap 23:00 on day 0 plus 00:00 and 01:00 on day 1.
	spanStart := time.Date(2026, 1, 15, 23, 0, 0, 0, time.UTC)
	durJob := JobItem{
		ID:       "long-job",
		Source:   "systemd",
		Title:    "Long Compaction",
		Duration: 3 * time.Hour,
		OccurrencesInRange: func(start, end time.Time) []time.Time {
			if spanStart.Before(end) && spanStart.Add(3*time.Hour).After(start) {
				return []time.Time{spanStart}
			}
			return nil
		},
	}
	nextDayGrid := indexDayHourJobs([]JobItem{durJob}, time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC))
	if len(nextDayGrid[0]) != 1 || len(nextDayGrid[1]) != 1 || len(nextDayGrid[2]) != 0 {
		t.Fatalf("expected 3h job from 23:00 to cover 00:00 & 01:00 next day, got h0=%d h1=%d h2=%d",
			len(nextDayGrid[0]), len(nextDayGrid[1]), len(nextDayGrid[2]))
	}
	if !durJob.OccursOnDay(time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected 3h job from Jan 15 23:00 to report OccursOnDay=true for Jan 16")
	}

	// Bounded occurrences cap must stop runaway generators without panicking.
	floodJob := JobItem{
		ID:     "flood",
		Source: "test",
		OccurrencesInRange: func(start, end time.Time) []time.Time {
			out := make([]time.Time, maxOccurrencesPerQuery+500)
			for i := range out {
				out[i] = start
			}
			return out
		},
	}
	if got := len(floodJob.boundedOccurrences(spanStart, spanStart.Add(24*time.Hour))); got != maxOccurrencesPerQuery {
		t.Fatalf("expected boundedOccurrences cap %d, got %d", maxOccurrencesPerQuery, got)
	}

	// Month anchor preservation: Jan 31 -> PgDown (Feb 28) -> PgDown (Mar 31).
	m := newModel(nil)
	m.cursor = time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(model)
	m = m.switchView(viewMonth, false)
	m.focusGrid = true
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(model)
	if m.cursor.Month() != time.February || m.cursor.Day() != 28 {
		t.Fatalf("expected Feb 28 after Jan 31 PgDown, got %v", m.cursor)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(model)
	if m.cursor.Month() != time.March || m.cursor.Day() != 31 {
		t.Fatalf("expected Mar 31 restored from anchor after Feb 28 PgDown, got %v", m.cursor)
	}
}

func TestRound3EdgeCasesDenseC1DSTCombosClipboardAndDuplicates(t *testing.T) {
	// 1. 8-bit C1 DCS/SOS/PM/APC sequences must be stripped with their payloads.
	c1Dirty := "\u0090secret-dcs\u009C\u0098secret-sos\u009C\u009Epm\u009C\u009Fapc\u009Cok"
	if got := sanitizeText(c1Dirty); got != "ok" {
		t.Fatalf("expected 8-bit C1 string sequences stripped, got %q", got)
	}

	// 2. Dense 15-min schedule across 42-day grid (4032 > 2048 occurrences) must not drop day 41.
	start42 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	denseJob := JobItem{
		ID:     "dense-15m",
		Source: "cron",
		OccurrencesInRange: func(start, end time.Time) []time.Time {
			var out []time.Time
			for ts := start; ts.Before(end); ts = ts.Add(15 * time.Minute) {
				out = append(out, ts)
			}
			return out
		},
	}
	dayGrid42 := indexDayJobs([]JobItem{denseJob}, start42, 42)
	if len(dayGrid42[41]) != 1 {
		t.Fatalf("expected dense 15m job still present on day 42, got %d", len(dayGrid42[41]))
	}

	// 3. Multi-week Duration (10 days) must still show on day 10.
	longStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tenDayJob := JobItem{
		ID:       "ten-day",
		Source:   "ops",
		Duration: 10 * 24 * time.Hour,
		OccurrencesInRange: func(start, end time.Time) []time.Time {
			if longStart.Before(end) && longStart.Add(10*24*time.Hour).After(start) {
				return []time.Time{longStart}
			}
			return nil
		},
	}
	if !tenDayJob.OccursOnDay(time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected 10-day Duration job starting Jan 1 to occur on Jan 10")
	}

	// 4. Combined MatchesDay + MatchesHour must filter Week/Day by MatchesDay too.
	comboJob := JobItem{
		ID:          "combo",
		Source:      "cron",
		MatchesDay:  func(d time.Time) bool { return d.Weekday() == time.Monday },
		MatchesHour: func(h time.Time) bool { return h.Hour() == 9 },
	}
	weekSun := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC) // Sunday
	wCombo := indexWeekJobs([]JobItem{comboJob}, weekSun)
	if len(wCombo[0][9]) != 0 || len(wCombo[1][9]) != 1 {
		t.Fatalf("combined MatchesDay+MatchesHour mismatch in week grid: sun=%d mon=%d", len(wCombo[0][9]), len(wCombo[1][9]))
	}
	if comboJob.OccursInHour(time.Date(2026, 1, 11, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected combined job to return false on Sunday 09:00")
	}

	// 5. Denver fall-back 01:00 second-fold 2h Duration propagation on MatchesHour.
	denver, err := time.LoadLocation("America/Denver")
	if err == nil {
		secondFoldOnly := JobItem{
			ID:          "fold2-2h",
			Source:      "cron",
			Duration:    2 * time.Hour,
			MatchesHour: func(h time.Time) bool { name, _ := h.Zone(); return h.Hour() == 1 && name == "MST" },
		}
		fallDay := time.Date(2026, 11, 1, 0, 0, 0, 0, denver)
		fHours := indexDayHourJobs([]JobItem{secondFoldOnly}, fallDay)
		if len(fHours[1]) != 1 || len(fHours[2]) != 1 || len(fHours[3]) != 0 {
			t.Fatalf("expected second-fold 2h MatchesHour job at 01:00 and 02:00 only, got h1=%d h2=%d h3=%d", len(fHours[1]), len(fHours[2]), len(fHours[3]))
		}
		// Invalid nonblank TimeZone must fall back to caller fallback, not time.Local.
		badTZJob := JobItem{TimeZone: "Invalid/Zone"}
		if badTZJob.effectiveLocation(denver) != denver {
			t.Fatalf("expected invalid TimeZone to fall back to caller fallback %v, got %v", denver, badTZJob.effectiveLocation(denver))
		}
	}

	// 6. Half-hour DST (Australia/Lord_Howe): fall-back hour 1 must stop before 02:00.
	lordHowe, err := time.LoadLocation("Australia/Lord_Howe")
	if err == nil {
		lhFall := time.Date(2026, 4, 5, 12, 0, 0, 0, lordHowe)
		s1, e1, ok1 := wallHourSpan(lhFall, 1)
		s2, _, ok2 := wallHourSpan(lhFall, 2)
		if !ok1 || !ok2 || !e1.Equal(s2) || e1.Sub(s1) != 90*time.Minute {
			t.Fatalf("unexpected Lord_Howe fall-back hour 1 span: [%v..%v) vs h2=%v", s1, e1, s2)
		}
	}

	// 7. Canonical cross-TZ day-only placement: map each job-local midnight 00:00 to one viewer day.
	if denver != nil {
		if _, kerr := time.LoadLocation("Pacific/Kiritimati"); kerr == nil {
			kiritiJob := JobItem{
				ID:         "kiriti-jan15",
				TimeZone:   "Pacific/Kiritimati",
				MatchesDay: func(d time.Time) bool { return d.Day() == 15 },
			}
			// Kiritimati Jan 15 00:00 (+14) is Denver Jan 14 03:00 MST -> Denver Jan 14 only.
			if !kiritiJob.OccursOnDay(time.Date(2026, 1, 14, 0, 0, 0, 0, denver)) {
				t.Fatalf("expected Pacific/Kiritimati Jan 15 midnight to map to Denver Jan 14")
			}
			if kiritiJob.OccursOnDay(time.Date(2026, 1, 15, 0, 0, 0, 0, denver)) {
				t.Fatalf("expected Pacific/Kiritimati Jan 15 not to double-place onto Denver Jan 15")
			}
		}
		if _, nerr := time.LoadLocation("America/New_York"); nerr == nil {
			nyMon := JobItem{
				ID:         "ny-mon",
				TimeZone:   "America/New_York",
				MatchesDay: func(d time.Time) bool { return d.Weekday() == time.Monday },
			}
			// NY Mon 2026-01-12 00:00 EST is Denver Sun 2026-01-11 22:00 MST -> Sunday only.
			if !nyMon.OccursOnDay(time.Date(2026, 1, 11, 0, 0, 0, 0, denver)) {
				t.Fatalf("expected NY Monday midnight to map to Denver Sunday")
			}
			if nyMon.OccursOnDay(time.Date(2026, 1, 12, 0, 0, 0, 0, denver)) {
				t.Fatalf("expected NY Monday not to double-place onto Denver Monday")
			}
		}
	}

	// 8. Duplicate (Source, ID) must be rejected before Settings edit, and sanitizeLiveInput preserves cursor prefix.
	mDup := newModel([]JobItem{
		{ID: "same", Source: "cron", Title: "First"},
		{ID: "same", Source: "cron", Title: "Second"},
		{ID: "unique", Source: "cron", Title: "Third", Alias: "OrigAlias"},
	})
	next, _ := mDup.Update(tea.WindowSizeMsg{Width: 44, Height: 14})
	mDup = next.(model)
	mDup = mDup.switchView(viewSettings, false)
	mDup.focusGrid = true
	next, _ = mDup.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mDup = next.(model)
	if mDup.settingsState != 0 || mDup.settingsError == "" {
		t.Fatalf("expected duplicate key edit blocked, got state=%d err=%q", mDup.settingsState, mDup.settingsError)
	}
	mDup.settingsCursor = 2
	next, _ = mDup.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mDup = next.(model)
	// Put unsafe runes both before and after cursor and verify exact sanitized cursor index.
	mDup.input.SetValue("a\u202Eb_after\u200B")
	mDup.input.SetCursor(4) // "a\u202Eb_"
	mDup.sanitizeLiveInput()
	if mDup.input.Value() != "ab_after" || mDup.input.Position() != 3 {
		t.Fatalf("expected value=ab_after pos=3, got %q pos=%d", mDup.input.Value(), mDup.input.Position())
	}
	// Wide CJK input at minimum 44x14 terminal must never overflow width.
	mDup.input.SetValue("日本語タイトルテスト文字列")
	mDup.input.CursorEnd()
	outWide := mDup.View()
	if lipgloss.Width(outWide) > 44 || lipgloss.Height(outWide) > 14 {
		t.Fatalf("wide CJK Settings edit overflowed 44x14: got %dx%d", lipgloss.Width(outWide), lipgloss.Height(outWide))
	}
	// Clear provider alias, save, then do a color-only edit -> cleared alias must stay cleared (not pinned to Title).
	mDup.input.SetValue("")
	next, _ = mDup.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mDup = next.(model)
	mDup.input.SetValue("#112233")
	next, _ = mDup.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mDup = next.(model)
	// Second edit: leave alias unchanged (showing "Third"), update color only.
	next, _ = mDup.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mDup = next.(model)
	next, _ = mDup.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mDup = next.(model)
	mDup.input.SetValue("#445566")
	next, _ = mDup.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mDup = next.(model)
	mDup.jobs[2].Title = "Updated Upstream Title"
	eff := mDup.effectiveJob(mDup.jobs[2])
	if eff.DisplayName() != "Updated Upstream Title" || eff.Color != "#445566" {
		t.Fatalf("expected cleared alias to stay cleared after color-only edit so upstream title flows through, got %+v", eff)
	}
}
