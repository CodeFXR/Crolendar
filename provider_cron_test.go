package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestParseCronExpressionsAndVixieSemantics(t *testing.T) {
	// Standard 5-field expressions and macros.
	sched, err := parseCronExpression("5/20 9 * * MON-FRI")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if (sched.minute&(1<<5)) == 0 || (sched.minute&(1<<25)) == 0 || (sched.minute&(1<<45)) == 0 || (sched.minute&(1<<0)) != 0 {
		t.Fatalf("unexpected 5/20 minute mask: %064b", sched.minute)
	}

	// DOW Sunday as 7 and FRI-SUN wrap plus 7/2 step.
	friSun, err := parseCronExpression("0 0 * * FRI-SUN")
	if err != nil {
		t.Fatalf("expected FRI-SUN valid, got %v", err)
	}
	if (friSun.dow&(1<<5)) == 0 || (friSun.dow&(1<<6)) == 0 || (friSun.dow&(1<<0)) == 0 || (friSun.dow&(1<<1)) != 0 {
		t.Fatalf("unexpected FRI-SUN dow mask: %08b", friSun.dow)
	}
	stepSun, err := parseCronExpression("0 0 * * 7/2")
	if err != nil {
		t.Fatalf("expected 7/2 valid, got %v", err)
	}
	if (stepSun.dow&(1<<0)) == 0 || (stepSun.dow&(1<<2)) == 0 || (stepSun.dow&(1<<4)) == 0 || (stepSun.dow&(1<<6)) == 0 {
		t.Fatalf("unexpected 7/2 dow mask: %08b", stepSun.dow)
	}

	// Vixie OR semantics: explicit DOM=15 AND explicit DOW=1 (Mon) -> matches either 15th OR Mondays.
	orSched, err := parseCronExpression("0 9 15 * 1")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	monJan12 := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC) // Monday
	tueJan13 := time.Date(2026, 1, 13, 0, 0, 0, 0, time.UTC) // Tuesday
	thuJan15 := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC) // Thursday 15th
	if !orSched.matchesDate(monJan12) || orSched.matchesDate(tueJan13) || !orSched.matchesDate(thuJan15) {
		t.Fatalf("Vixie DOM+DOW OR mismatch: mon12=%v tue13=%v thu15=%v",
			orSched.matchesDate(monJan12), orSched.matchesDate(tueJan13), orSched.matchesDate(thuJan15))
	}

	// Stepped star AND semantics: */2 DOM with DOW=1 -> odd-day Mondays only.
	andSched, err := parseCronExpression("0 9 */2 * 1")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	monJan5 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // odd Monday
	if !andSched.matchesDate(monJan5) || andSched.matchesDate(monJan12) || andSched.matchesDate(thuJan15) {
		t.Fatalf("stepped DOM+DOW AND mismatch: mon5=%v mon12=%v thu15=%v",
			andSched.matchesDate(monJan5), andSched.matchesDate(monJan12), andSched.matchesDate(thuJan15))
	}

	// Invalid expressions must fail cleanly.
	bad := []string{
		"60 * * * *",
		"* 24 * * *",
		"* * 0 * *",
		"* * 32 * *",
		"* * * 13 *",
		"* * * * 8",
		"*/0 * * * *",
		"5-2 * * * *",
		"1,,2 * * * *",
		"1/2/3 * * * *",
		"* * * *",
		"bad * * * *",
	}
	for _, expr := range bad {
		if _, err := parseCronExpression(expr); err == nil {
			t.Fatalf("expected parseCronExpression(%q) to fail", expr)
		}
	}
}

func TestParseCrontabMacrosEnvTZAndSanitization(t *testing.T) {
	raw := strings.Join([]string{
		"# Header comment",
		"SHELL=/bin/bash",
		`MAILTO=""`,
		"CRON_TZ=UTC",
		"0 0 * * * /usr/local/bin/backup --full\r",
		"0 0 * * * /usr/local/bin/backup --full",
		`CRON_TZ="Invalid/Zone"`,
		"@hourly \x1b[31m\u202Eecho hello\x1b[0m",
		"@reboot /usr/bin/init-cache",
		"@bogus /bin/true",
		"invalid cron line here",
		"0 9 * * *",
	}, "\n")

	jobs := ParseCrontab(raw)
	if len(jobs) != 4 {
		t.Fatalf("expected 4 valid jobs, got %d (%+v)", len(jobs), jobs)
	}

	// Duplicate lines must receive distinct stable IDs.
	if jobs[0].ID == "" || jobs[1].ID == "" || jobs[0].ID == jobs[1].ID {
		t.Fatalf("expected distinct non-empty IDs for duplicate lines, got %q and %q", jobs[0].ID, jobs[1].ID)
	}
	if jobs[0].TimeZone != "UTC" {
		t.Fatalf("expected first job TimeZone=UTC, got %q", jobs[0].TimeZone)
	}

	// UTC 00:00 job must land at 17:00 MST when viewed in America/Denver.
	denver, err := time.LoadLocation("America/Denver")
	if err == nil {
		grid := indexDayHourJobs([]JobItem{jobs[0]}, time.Date(2026, 1, 15, 0, 0, 0, 0, denver))
		if len(grid[17]) != 1 || len(grid[0]) != 0 {
			t.Fatalf("expected CRON_TZ=UTC 00:00 job at 17:00 Denver, got h17=%d h0=%d", len(grid[17]), len(grid[0]))
		}
	}

	// Invalid CRON_TZ must reset back to local (empty TimeZone), not inherit UTC.
	if jobs[2].TimeZone != "" {
		t.Fatalf("expected invalid CRON_TZ to reset TimeZone to empty, got %q", jobs[2].TimeZone)
	}
	if jobs[2].DisplayName() != "echo hello" {
		t.Fatalf("expected ANSI/BiDi scrubbed from cron command, got %q", jobs[2].DisplayName())
	}

	// @reboot job must surface with ScheduleLabel="@reboot" and nil OccurrencesInRange.
	if jobs[3].ScheduleLabel != "@reboot" || jobs[3].OccurrencesInRange != nil {
		t.Fatalf("expected @reboot job unscheduled on calendar, got label=%q fn=%v", jobs[3].ScheduleLabel, jobs[3].OccurrencesInRange != nil)
	}
}

func TestCronOccurrencesDSTSpringForwardFallBackAndBounds(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err == nil {
		// Spring-forward 2026-03-08: 02:30 does not exist in Denver.
		sched, err := parseCronExpression("30 2 * * *")
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		start := time.Date(2026, 3, 8, 0, 0, 0, 0, denver)
		end := time.Date(2026, 3, 10, 0, 0, 0, 0, denver)
		occs := sched.occurrences(start, end, denver)
		if len(occs) != 1 || occs[0].Day() != 9 || occs[0].Hour() != 2 || occs[0].Minute() != 30 {
			t.Fatalf("expected spring-forward 02:30 skipped on Mar 8 and fired Mar 9, got %v", occs)
		}

		// Fall-back 2026-11-01: 01:30 fires once cleanly in ascending order.
		fallSched, _ := parseCronExpression("30 1 * * *")
		fStart := time.Date(2026, 11, 1, 0, 0, 0, 0, denver)
		fEnd := time.Date(2026, 11, 2, 0, 0, 0, 0, denver)
		fOccs := fallSched.occurrences(fStart, fEnd, denver)
		if len(fOccs) != 1 || fOccs[0].Hour() != 1 || fOccs[0].Minute() != 30 {
			t.Fatalf("expected single 01:30 fall-back occurrence, got %v", fOccs)
		}
	}

	// Lord Howe 30m spring-forward (2026-10-04 02:00 -> 02:30): 02:15 skipped, 02:45 fires.
	lordHowe, err := time.LoadLocation("Australia/Lord_Howe")
	if err == nil {
		skipSched, _ := parseCronExpression("15 2 * * *")
		keepSched, _ := parseCronExpression("45 2 * * *")
		lhStart := time.Date(2026, 10, 4, 0, 0, 0, 0, lordHowe)
		lhEnd := time.Date(2026, 10, 5, 0, 0, 0, 0, lordHowe)
		if got := skipSched.occurrences(lhStart, lhEnd, lordHowe); len(got) != 0 {
			t.Fatalf("expected Lord Howe nonexistent 02:15 skipped, got %v", got)
		}
		if got := keepSched.occurrences(lhStart, lhEnd, lordHowe); len(got) != 1 || got[0].Minute() != 45 {
			t.Fatalf("expected Lord Howe 02:45 to fire, got %v", got)
		}
	}

	// Bounded cap at provider boundary + strict chronological order.
	everyMin, _ := parseCronExpression("* * * * *")
	s := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	occs := everyMin.occurrences(s, s.AddDate(0, 0, 100), time.UTC)
	if len(occs) != maxOccurrencesPerQuery {
		t.Fatalf("expected provider-capped %d occurrences, got %d", maxOccurrencesPerQuery, len(occs))
	}
	for i := 1; i < len(occs); i++ {
		if !occs[i].After(occs[i-1]) {
			t.Fatalf("occurrences not strictly chronological at %d: %v <= %v", i, occs[i], occs[i-1])
		}
	}
}

func TestCronProviderFileSecurityAndErrors(t *testing.T) {
	dir := t.TempDir()

	// Directory rejected.
	if _, err := NewCronProvider(dir).Load(context.Background()); err == nil {
		t.Fatalf("expected directory path to fail")
	}

	// Control chars in path rejected.
	if _, err := NewCronProvider(dir + "/bad\nfile").Load(context.Background()); err == nil {
		t.Fatalf("expected newline path to fail")
	}

	// Valid file loads cleanly.
	goodPath := filepath.Join(dir, "sample.cron")
	if err := os.WriteFile(goodPath, []byte("15 4 * * * /usr/bin/vacuum\n"), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	jobs, err := NewCronProvider(goodPath).Load(context.Background())
	if err != nil || len(jobs) != 1 || jobs[0].ScheduleLabel != "15 4 * * *" {
		t.Fatalf("unexpected file load result: jobs=%+v err=%v", jobs, err)
	}

	// Oversized (>1 MiB) file rejected.
	bigPath := filepath.Join(dir, "huge.cron")
	big := strings.Repeat("# padding\n", (maxCrontabBytes/10)+20)
	if err := os.WriteFile(bigPath, []byte(big), 0600); err != nil {
		t.Fatalf("write big failed: %v", err)
	}
	if _, err := NewCronProvider(bigPath).Load(context.Background()); err == nil {
		t.Fatalf("expected >1 MiB crontab file to be rejected")
	}
}

type stubProvider struct {
	name  string
	items []JobItem
	err   error
	panic bool
}

func (s stubProvider) Name() string { return s.name }
func (s stubProvider) Load(ctx context.Context) ([]JobItem, error) {
	if s.panic {
		panic("boom")
	}
	return s.items, s.err
}

func TestLoadProvidersDedupeIsolationAndBubbleTeaRefresh(t *testing.T) {
	providers := []JobProvider{
		stubProvider{
			name: "cron",
			items: []JobItem{
				{ID: "", Title: "DropEmptyID"},
				{ID: "a#2", Title: "ExistingHash2"},
				{ID: "a", Title: "FirstA"},
				{ID: "a", Title: "SecondA"},
			},
		},
		stubProvider{
			name: "k8s",
			err:  errors.New("cluster unreachable"),
		},
		stubProvider{
			name:  "broken",
			panic: true,
		},
	}

	res := LoadProviders(context.Background(), providers)
	if len(res.Jobs) != 3 {
		t.Fatalf("expected 3 valid deduplicated jobs, got %d (%+v)", len(res.Jobs), res.Jobs)
	}
	seen := make(map[JobKey]bool)
	for _, j := range res.Jobs {
		if j.Source != "cron" {
			t.Fatalf("expected Source defaulted to cron, got %q", j.Source)
		}
		if seen[j.Key()] {
			t.Fatalf("duplicate key leaked from LoadProviders: %+v", j.Key())
		}
		seen[j.Key()] = true
	}
	if len(res.Statuses) != 3 || res.Statuses[1].Err == nil || res.Statuses[2].Err == nil {
		t.Fatalf("expected isolated error/panic statuses, got %+v", res.Statuses)
	}

	// Bubble Tea async load, stale gen drop, override persistence across refresh, and bounded error rendering.
	m := newModelWithProviders(nil, providers)
	if !m.loading || m.Init() == nil {
		t.Fatalf("expected loading=true and non-nil Init batch with providers")
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 44, Height: 14})
	m = next.(model)

	// Apply load result gen 1.
	next, _ = m.Update(providersLoadedMsg{gen: 1, result: res})
	m = next.(model)
	if m.loading || len(m.jobs) != 3 {
		t.Fatalf("expected 3 loaded jobs after gen 1, loading=%v len=%d", m.loading, len(m.jobs))
	}

	// Save an override on FirstA, then trigger reload 'r' (gen 2) and ensure stale gen 1 is ignored.
	firstKey := m.jobs[1].Key()
	m.overrides[firstKey] = JobOverride{Alias: "Pinned", HasAlias: true, Color: "#112233"}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = next.(model)
	if !m.loading || cmd == nil || m.loadGen != 2 {
		t.Fatalf("expected 'r' to start reload gen 2, loading=%v gen=%d cmd=%v", m.loading, m.loadGen, cmd != nil)
	}
	// Stale gen 1 message must be ignored.
	next, _ = m.Update(providersLoadedMsg{gen: 1, result: ProviderLoadResult{}})
	m = next.(model)
	if !m.loading || len(m.jobs) != 3 {
		t.Fatalf("expected stale gen 1 ignored during gen 2 load")
	}

	// Complete gen 2 -> override survives and footer/Settings error stays bounded at 44x14.
	next, _ = m.Update(providersLoadedMsg{gen: 2, result: res})
	m = next.(model)
	if eff := m.effectiveJob(m.jobs[1]); eff.DisplayName() != "Pinned" || eff.Color != "#112233" {
		t.Fatalf("expected override preserved across provider reload, got %+v", eff)
	}
	out := m.View()
	if lipgloss.Width(out) > 44 || lipgloss.Height(out) > 14 {
		t.Fatalf("provider error footer overflowed 44x14: got %dx%d", lipgloss.Width(out), lipgloss.Height(out))
	}
	if !strings.Contains(ansi.Strip(out), "k8s") {
		t.Fatalf("expected provider error indicator in footer, got:\n%s", ansi.Strip(out))
	}

	// Empty-job error state in Settings must render cleanly at 44x14.
	next, _ = m.Update(providersLoadedMsg{gen: 2, result: ProviderLoadResult{Statuses: res.Statuses[1:2]}})
	m = next.(model)
	m = m.switchView(viewSettings, false)
	outSettings := m.View()
	if lipgloss.Width(outSettings) > 44 || lipgloss.Height(outSettings) > 14 {
		t.Fatalf("empty error Settings overflowed 44x14: got %dx%d", lipgloss.Width(outSettings), lipgloss.Height(outSettings))
	}
}
