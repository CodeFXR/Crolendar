package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
		"*/61 * * * *",
		"1/9223372036854775807 * * * *",
		"2/9223372036854775807 * * * *",
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
		"TZ=UTC",
		"0 8 * * * /usr/bin/mail -s \"100\\% done\" admin%stdin line 1%line 2",
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

	jobs, warnings := ParseCrontabWithWarnings(raw)
	if len(jobs) != 5 {
		t.Fatalf("expected 5 valid jobs, got %d (%+v)", len(jobs), jobs)
	}
	if len(warnings) != 4 {
		t.Fatalf("expected 4 warnings (invalid CRON_TZ + 3 malformed lines), got %d (%v)", len(warnings), warnings)
	}

	// Plain TZ=UTC must NOT shift cron schedule timezone, and % stdin must be stripped while \% unescapes.
	if jobs[0].TimeZone != "" {
		t.Fatalf("expected plain TZ=UTC not to set schedule TimeZone, got %q", jobs[0].TimeZone)
	}
	if jobs[0].DisplayName() != `/usr/bin/mail -s "100% done" admin` {
		t.Fatalf("unexpected cron %% command split: %q", jobs[0].DisplayName())
	}

	// Duplicate lines must receive distinct stable IDs.
	if jobs[1].ID == "" || jobs[2].ID == "" || jobs[1].ID == jobs[2].ID {
		t.Fatalf("expected distinct non-empty IDs for duplicate lines, got %q and %q", jobs[1].ID, jobs[2].ID)
	}
	if jobs[1].TimeZone != "UTC" {
		t.Fatalf("expected CRON_TZ job TimeZone=UTC, got %q", jobs[1].TimeZone)
	}

	// UTC 00:00 job must land at 17:00 MST when viewed in America/Denver.
	denver, err := time.LoadLocation("America/Denver")
	if err == nil {
		grid := indexDayHourJobs([]JobItem{jobs[1]}, time.Date(2026, 1, 15, 0, 0, 0, 0, denver))
		if len(grid[17]) != 1 || len(grid[0]) != 0 {
			t.Fatalf("expected CRON_TZ=UTC 00:00 job at 17:00 Denver, got h17=%d h0=%d", len(grid[17]), len(grid[0]))
		}
	}

	// Invalid CRON_TZ must reset back to local (empty TimeZone), not inherit UTC.
	if jobs[3].TimeZone != "" {
		t.Fatalf("expected invalid CRON_TZ to reset TimeZone to empty, got %q", jobs[3].TimeZone)
	}
	if jobs[3].DisplayName() != "echo hello" {
		t.Fatalf("expected ANSI/BiDi scrubbed from cron command, got %q", jobs[3].DisplayName())
	}

	// @reboot job must surface with ScheduleLabel="@reboot" and nil OccurrencesInRange.
	if jobs[4].ScheduleLabel != "@reboot" || jobs[4].OccurrencesInRange != nil {
		t.Fatalf("expected @reboot job unscheduled on calendar, got label=%q fn=%v", jobs[4].ScheduleLabel, jobs[4].OccurrencesInRange != nil)
	}

	// Entry cap must bound massive crontabs cleanly with a truncation warning.
	var many strings.Builder
	for i := 0; i < maxCrontabJobs+25; i++ {
		many.WriteString("0 1 * * * /bin/true\n")
	}
	capped, capWarn := ParseCrontabWithWarnings(many.String())
	if len(capped) != maxCrontabJobs || len(capWarn) == 0 {
		t.Fatalf("expected cap at %d jobs with warning, got len=%d warnings=%v", maxCrontabJobs, len(capped), capWarn)
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

		// Fall-back 2026-11-01: 01:30 fires once on the earlier fold (MDT, -06:00 = 07:30 UTC),
		// never on the repeated later fold (MST, -07:00 = 08:30 UTC).
		fallSched, _ := parseCronExpression("30 1 * * *")
		fStart := time.Date(2026, 11, 1, 0, 0, 0, 0, denver)
		fEnd := time.Date(2026, 11, 2, 0, 0, 0, 0, denver)
		fOccs := fallSched.occurrences(fStart, fEnd, denver)
		if len(fOccs) != 1 || fOccs[0].Hour() != 1 || fOccs[0].Minute() != 30 {
			t.Fatalf("expected single 01:30 fall-back occurrence, got %v", fOccs)
		}
		if _, off := fOccs[0].Zone(); off != -6*3600 {
			t.Fatalf("expected earlier fall-back fold offset -21600 (MDT), got %d (%v)", off, fOccs[0])
		}
		firstFoldStart := time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC)  // 01:00 MDT
		secondFoldStart := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC) // 01:00 MST
		secondFoldEnd := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)   // 02:00 MST
		if !fallSched.hasOccurrenceIn(firstFoldStart, secondFoldStart, denver) {
			t.Fatalf("expected hasOccurrenceIn true for earlier fall-back fold [01:00 MDT, 01:00 MST)")
		}
		if fallSched.hasOccurrenceIn(secondFoldStart, secondFoldEnd, denver) {
			t.Fatalf("expected hasOccurrenceIn false for later fall-back fold [01:00 MST, 02:00 MST)")
		}
	}

	// Lord Howe 30m spring-forward (2026-10-04 02:00 -> 02:30): 02:15 skipped, 02:45 fires.
	// Also test 30m fall-back (2026-04-05 02:00 +11:00 -> 01:30 +10:30): 01:45 fires once on +11:00.
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

		lhFallSched, _ := parseCronExpression("45 1 * * *")
		lhFallStart := time.Date(2026, 4, 5, 0, 0, 0, 0, lordHowe)
		lhFallEnd := time.Date(2026, 4, 6, 0, 0, 0, 0, lordHowe)
		lhFallOccs := lhFallSched.occurrences(lhFallStart, lhFallEnd, lordHowe)
		if len(lhFallOccs) != 1 {
			t.Fatalf("expected single Lord Howe 01:45 fall-back occurrence, got %v", lhFallOccs)
		}
		if _, off := lhFallOccs[0].Zone(); off != 11*3600 {
			t.Fatalf("expected earlier Lord Howe fold offset +39600 (+11:00), got %d (%v)", off, lhFallOccs[0])
		}
		lhFirstStart := time.Date(2026, 4, 4, 14, 30, 0, 0, time.UTC) // 01:30 +11:00
		lhSecondStart := time.Date(2026, 4, 4, 15, 0, 0, 0, time.UTC) // 01:30 +10:30
		lhSecondEnd := time.Date(2026, 4, 4, 15, 30, 0, 0, time.UTC)  // 02:00 +10:30
		if !lhFallSched.hasOccurrenceIn(lhFirstStart, lhSecondStart, lordHowe) {
			t.Fatalf("expected hasOccurrenceIn true for Lord Howe earlier 30m fold")
		}
		if lhFallSched.hasOccurrenceIn(lhSecondStart, lhSecondEnd, lordHowe) {
			t.Fatalf("expected hasOccurrenceIn false for Lord Howe later 30m fold")
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

	// Cancelled context must abort file load cleanly.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewCronProvider(goodPath).Load(cancelledCtx); err == nil {
		t.Fatalf("expected cancelled context to fail file load")
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
	name      string
	items     []JobItem
	warnings  []string
	err       error
	panicLoad bool
	panicName bool
}

func (s stubProvider) Name() string {
	if s.panicName {
		panic("name boom")
	}
	return s.name
}
func (s stubProvider) Load(ctx context.Context) ([]JobItem, error) {
	if s.panicLoad {
		panic("boom")
	}
	return s.items, s.err
}
func (s stubProvider) LoadWithWarnings(ctx context.Context) ([]JobItem, []string, error) {
	items, err := s.Load(ctx)
	return items, s.warnings, err
}

func TestLoadProvidersDedupeIsolationAndBubbleTeaRefresh(t *testing.T) {
	providers := []JobProvider{
		stubProvider{
			name:     "cron",
			warnings: []string{"line 4: skipped malformed cron entry"},
			items: []JobItem{
				{ID: "", Title: "DropEmptyID"},
				{ID: "a#2", Source: "spoofed", Title: "ExistingHash2"},
				{ID: "a", Title: "FirstA"},
				{ID: "a", Title: "SecondA"},
			},
		},
		stubProvider{
			name: "k8s",
			err:  errors.New("cluster unreachable"),
		},
		stubProvider{
			name:      "broken",
			panicLoad: true,
		},
		stubProvider{
			panicName: true,
		},
	}

	res := LoadProviders(context.Background(), providers)
	if len(res.Jobs) != 3 {
		t.Fatalf("expected 3 valid deduplicated jobs, got %d (%+v)", len(res.Jobs), res.Jobs)
	}
	seen := make(map[JobKey]bool)
	for _, j := range res.Jobs {
		if j.Source != "cron" {
			t.Fatalf("expected Source unconditionally enforced to cron, got %q", j.Source)
		}
		if seen[j.Key()] {
			t.Fatalf("duplicate key leaked from LoadProviders: %+v", j.Key())
		}
		seen[j.Key()] = true
	}
	if len(res.Statuses) != 4 || len(res.Statuses[0].Warnings) != 1 || res.Statuses[1].Err == nil || res.Statuses[2].Err == nil || res.Statuses[3].Err == nil {
		t.Fatalf("expected isolated warning/error/panic statuses, got %+v", res.Statuses)
	}

	// Bubble Tea async load, stale gen drop, override persistence across refresh, and bounded error/warning rendering.
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

	// Partial reload: cron errors while systemd succeeds -> retain last-known cron jobs AND apply new systemd job.
	next, _ = m.Update(providersLoadedMsg{gen: 2, result: ProviderLoadResult{
		Jobs: []JobItem{{ID: "sys-1", Source: "systemd", Title: "Timer One"}},
		Statuses: []ProviderStatus{
			{Name: "cron", Err: errors.New("crontab unavailable")},
			{Name: "systemd", Count: 1},
		},
	}})
	m = next.(model)
	if len(m.jobs) != 4 {
		t.Fatalf("expected 1 new systemd job + 3 retained cron jobs on partial reload failure, got %d", len(m.jobs))
	}
	foundPinned := false
	for _, j := range m.jobs {
		if j.Key() == firstKey {
			eff := m.effectiveJob(j)
			if eff.DisplayName() == "Pinned" && eff.Color == "#112233" {
				foundPinned = true
			}
		}
	}
	if !foundPinned {
		t.Fatalf("expected override preserved on retained cron job across partial reload error")
	}
	out := m.View()
	if lipgloss.Width(out) > 44 || lipgloss.Height(out) > 14 {
		t.Fatalf("provider error footer overflowed 44x14: got %dx%d", lipgloss.Width(out), lipgloss.Height(out))
	}
	if !strings.Contains(ansi.Strip(out), "cron") {
		t.Fatalf("expected provider error indicator in footer, got:\n%s", ansi.Strip(out))
	}

	// Warning-only status must surface in footer & Settings cleanly at 44x14.
	next, _ = m.Update(providersLoadedMsg{gen: 2, result: ProviderLoadResult{
		Jobs:     res.Jobs,
		Statuses: res.Statuses[:1],
	}})
	m = next.(model)
	m = m.switchView(viewSettings, false)
	outWarn := m.View()
	if lipgloss.Width(outWarn) > 44 || lipgloss.Height(outWarn) > 14 {
		t.Fatalf("warning Settings view overflowed 44x14: got %dx%d", lipgloss.Width(outWarn), lipgloss.Height(outWarn))
	}
	if !strings.Contains(ansi.Strip(outWarn), "warning") {
		t.Fatalf("expected warning surfaced in Settings/footer, got:\n%s", ansi.Strip(outWarn))
	}

	// Initial empty-job error state in Settings must render cleanly at 44x14.
	mEmptyErr := newModelWithProviders(nil, providers)
	next, _ = mEmptyErr.Update(tea.WindowSizeMsg{Width: 44, Height: 14})
	mEmptyErr = next.(model)
	next, _ = mEmptyErr.Update(providersLoadedMsg{gen: 1, result: ProviderLoadResult{Statuses: res.Statuses[1:2]}})
	mEmptyErr = next.(model)
	mEmptyErr = mEmptyErr.switchView(viewSettings, false)
	outSettings := mEmptyErr.View()
	if lipgloss.Width(outSettings) > 44 || lipgloss.Height(outSettings) > 14 {
		t.Fatalf("empty error Settings overflowed 44x14: got %dx%d", lipgloss.Width(outSettings), lipgloss.Height(outSettings))
	}
}

type blockingProvider struct {
	unblock chan struct{}
}

func (b blockingProvider) Name() string { return "stuck" }
func (b blockingProvider) Load(ctx context.Context) ([]JobItem, error) {
	<-b.unblock
	return nil, nil
}

func TestHardeningTimeoutWarningCapNilCtxAndDense2000Render(t *testing.T) {
	// 1. Non-cooperative provider that ignores ctx must NOT block LoadProviders past ctx deadline.
	unblock := make(chan struct{})
	defer close(unblock)
	shortCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := LoadProviders(shortCtx, []JobProvider{blockingProvider{unblock: unblock}})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("LoadProviders blocked on non-cooperative provider: %v", elapsed)
	}
	if len(res.Statuses) != 1 || res.Statuses[0].Err == nil {
		t.Fatalf("expected timeout error status for stuck provider, got %+v", res.Statuses)
	}

	// 2. Warning cap: 500 malformed lines must be capped at maxCrontabWarnings+1 with suppressed summary.
	badMany := strings.Repeat("not a valid cron line\n", 500)
	_, warns := ParseCrontabWithWarnings(badMany)
	if len(warns) != maxCrontabWarnings+1 || !strings.Contains(warns[len(warns)-1], "suppressed") {
		t.Fatalf("expected capped %d warnings with suppressed notice, got %d (%v)", maxCrontabWarnings+1, len(warns), warns)
	}

	// 3. CronProvider.Load(nil) must never panic (file path and nil ctx normalization).
	dir := t.TempDir()
	p := filepath.Join(dir, "ok.cron")
	if err := os.WriteFile(p, []byte("0 5 * * * /bin/echo ok\n"), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	var nilCtx context.Context
	if jobs, err := NewCronProvider(p).Load(nilCtx); err != nil || len(jobs) != 1 {
		t.Fatalf("expected nil ctx file load to succeed cleanly, got jobs=%d err=%v", len(jobs), err)
	}
	// Also test readSystemCrontab(nil) does not panic even if crontab binary missing/empty.
	_, _ = readSystemCrontab(nilCtx)

	// 4. Dense 2,000 * * * * * jobs over 42-day Agenda and 7-day Week must use the
	// zero-allocation HasOccurrenceInRange fast path cleanly without wall-clock flakiness.
	var dense strings.Builder
	for i := 0; i < maxCrontabJobs; i++ {
		dense.WriteString("* * * * * /bin/true\n")
	}
	denseJobs := ParseCrontab(dense.String())
	if len(denseJobs) != maxCrontabJobs {
		t.Fatalf("expected %d dense jobs, got %d", maxCrontabJobs, len(denseJobs))
	}
	if denseJobs[0].HasOccurrenceInRange == nil {
		t.Fatalf("expected parsed cron job to wire HasOccurrenceInRange fast path")
	}
	dStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dGrid := indexDayJobs(denseJobs, dStart, 42)
	wGrid := indexWeekJobs(denseJobs, dStart)
	if len(dGrid[41]) != maxCrontabJobs || len(wGrid[6][23]) != maxCrontabJobs {
		t.Fatalf("expected all %d dense jobs indexed on day 42 and week day 6 h23", maxCrontabJobs)
	}
	everyMinSched, err := parseCronExpression("* * * * *")
	if err != nil {
		t.Fatalf("parse * * * * * failed: %v", err)
	}
	dEnd := dStart.Add(24 * time.Hour)
	if allocs := testing.AllocsPerRun(20, func() {
		if !everyMinSched.hasOccurrenceIn(dStart, dEnd, time.UTC) {
			t.Fatal("expected match")
		}
	}); allocs != 0 {
		t.Fatalf("expected hasOccurrenceIn fast path to perform 0 allocations, got %v", allocs)
	}
}

func BenchmarkDense2000AgendaAndWeek(b *testing.B) {
	var dense strings.Builder
	for i := 0; i < maxCrontabJobs; i++ {
		dense.WriteString("* * * * * /bin/true\n")
	}
	denseJobs := ParseCrontab(dense.String())
	dStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = indexDayJobs(denseJobs, dStart, 42)
		_ = indexWeekJobs(denseJobs, dStart)
	}
}

func TestCronHasOccurrenceInMatchesOccurrences(t *testing.T) {
	exprs := []string{
		"* * * * *",
		"0 0 * * *",
		"30 1 * * *",
		"30 2 * * *",
		"15 2 * * *",
		"45 1 * * *",
		"0 0 13 * 5",
		"0 0 */2 * 5",
		"5/20 9-17 * * MON-FRI",
		"0 0 29 2 *",
	}
	zoneNames := []string{
		"UTC",
		"America/Denver",
		"America/New_York",
		"Australia/Lord_Howe",
		"Pacific/Kiritimati",
	}
	anchors := [][3]int{
		{2026, 1, 15},
		{2026, 3, 8},  // US spring-forward
		{2026, 4, 5},  // Lord Howe fall-back (30m)
		{2026, 10, 4}, // Lord Howe spring-forward (30m)
		{2026, 11, 1}, // US fall-back (1h)
		{2026, 12, 31},
		{2028, 2, 29}, // leap day
	}
	offsets := []time.Duration{
		0,
		30 * time.Minute,
		1 * time.Hour,
		90 * time.Minute,
		2 * time.Hour,
		2*time.Hour + 15*time.Minute,
		9*time.Hour + 5*time.Minute,
		23*time.Hour + 30*time.Minute,
	}
	spans := []time.Duration{
		0,
		1 * time.Minute,
		15 * time.Minute,
		30 * time.Minute,
		1 * time.Hour,
		2 * time.Hour,
		24 * time.Hour,
		25 * time.Hour,
		42 * 24 * time.Hour,
	}

	var locs []*time.Location
	for _, z := range zoneNames {
		if loc, err := time.LoadLocation(z); err == nil {
			locs = append(locs, loc)
		}
	}

	for _, expr := range exprs {
		sched, err := parseCronExpression(expr)
		if err != nil {
			t.Fatalf("parse %q failed: %v", expr, err)
		}
		for _, loc := range locs {
			for _, a := range anchors {
				base := time.Date(a[0], time.Month(a[1]), a[2], 0, 0, 0, 0, loc)
				for _, off := range offsets {
					start := base.Add(off)
					for _, sp := range spans {
						if expr == "* * * * *" && sp > 25*time.Hour && off != 0 {
							continue
						}
						end := start.Add(sp)
						want := len(sched.occurrences(start, end, loc)) > 0
						got := sched.hasOccurrenceIn(start, end, loc)
						if got != want {
							t.Fatalf("mismatch expr=%q loc=%s range=[%v, %v): hasOccurrenceIn=%v want=%v",
								expr, loc, start, end, got, want)
						}
					}
				}
			}
		}
	}
}

type blockingNameProvider struct {
	unblock <-chan struct{}
}

func (b blockingNameProvider) Name() string {
	<-b.unblock
	return "blocked-name"
}

func (b blockingNameProvider) Load(ctx context.Context) ([]JobItem, error) {
	return nil, nil
}

type countingHungProvider struct {
	calls   *atomic.Int32
	unblock <-chan struct{}
}

func (c countingHungProvider) Name() string { return "hung-adapter" }
func (c countingHungProvider) Load(ctx context.Context) ([]JobItem, error) {
	c.calls.Add(1)
	<-c.unblock
	return nil, nil
}

type noisyWarningProvider struct {
	count int
}

func (n noisyWarningProvider) Name() string { return "noisy" }
func (n noisyWarningProvider) Load(ctx context.Context) ([]JobItem, error) {
	return nil, nil
}
func (n noisyWarningProvider) LoadWithWarnings(ctx context.Context) ([]JobItem, []string, error) {
	warns := make([]string, n.count)
	for i := range warns {
		warns[i] = "\x1b[31mwarning\x1b[0m line"
	}
	return []JobItem{{ID: "j1", Title: "ok"}}, warns, nil
}

func TestProviderLoaderHardeningBlockingNameNoAccumulationAndWarningCap(t *testing.T) {
	// 1. A provider whose Name() blocks must not defeat the deadline or hang wg.Wait().
	unblockName := make(chan struct{})
	defer close(unblockName)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel1()
	t0 := time.Now()
	res1 := LoadProviders(ctx1, []JobProvider{blockingNameProvider{unblock: unblockName}})
	if time.Since(t0) > time.Second {
		t.Fatalf("blocking Name() defeated deadline: took %v", time.Since(t0))
	}
	if len(res1.Statuses) != 1 || res1.Statuses[0].Err == nil || res1.Statuses[0].Name != "provider-1" {
		t.Fatalf("expected timed-out fallback provider-1 status, got %+v", res1.Statuses)
	}

	// 2. ProviderLoader must not spawn extra goroutines on retry while a non-cooperative
	// provider is still hung, while healthy sibling providers still reload normally.
	var hungCalls atomic.Int32
	unblockHung := make(chan struct{})
	defer close(unblockHung)
	loader := NewProviderLoader([]JobProvider{
		countingHungProvider{calls: &hungCalls, unblock: unblockHung},
		stubProvider{name: "healthy", items: []JobItem{{ID: "h1", Title: "ok"}}},
	})
	for attempt := 0; attempt < 3; attempt++ {
		c, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		out := loader.Load(c)
		cancel()
		if len(out.Jobs) != 1 || out.Jobs[0].Source != "healthy" {
			t.Fatalf("attempt %d: expected healthy sibling job, got %+v", attempt, out.Jobs)
		}
		if len(out.Statuses) != 2 || out.Statuses[0].Err == nil || out.Statuses[1].Err != nil {
			t.Fatalf("attempt %d: unexpected statuses %+v", attempt, out.Statuses)
		}
	}
	if got := hungCalls.Load(); got != 1 {
		t.Fatalf("expected hung provider Load to be invoked only once across 3 reloads, got %d", got)
	}

	// 3. Any WarningProvider's warnings are capped and ANSI-sanitized by ProviderLoader,
	// including the exact 21-raw-warning boundary and an ordinary 21st warning ending in "suppressed".
	resWarn21 := LoadProviders(context.Background(), []JobProvider{noisyWarningProvider{count: maxProviderWarnings + 1}})
	if got := resWarn21.Statuses[0].Warnings; len(got) != maxProviderWarnings+1 || !strings.Contains(got[maxProviderWarnings], "1 more warning(s) suppressed") {
		t.Fatalf("expected 21 raw warnings capped with 1 suppressed summary, got %+v", got)
	}
	ordinary21 := make([]string, maxProviderWarnings+1)
	for i := 0; i < maxProviderWarnings; i++ {
		ordinary21[i] = "ordinary warning"
	}
	ordinary21[maxProviderWarnings] = "line 21: directive was suppressed"
	resOrdinary21 := LoadProviders(context.Background(), []JobProvider{stubProvider{name: "custom", warnings: ordinary21}})
	if got := resOrdinary21.Statuses[0].Warnings; len(got) != maxProviderWarnings+1 || got[maxProviderWarnings] != formatSuppressedWarnings(1) {
		t.Fatalf("expected ordinary 21st warning ending in 'suppressed' to be capped, got %+v", got)
	}
	resWarn := LoadProviders(context.Background(), []JobProvider{noisyWarningProvider{count: 500}})
	if len(resWarn.Statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(resWarn.Statuses))
	}
	warns := resWarn.Statuses[0].Warnings
	if len(warns) != maxProviderWarnings+1 {
		t.Fatalf("expected %d capped warnings, got %d", maxProviderWarnings+1, len(warns))
	}
	if strings.Contains(warns[0], "\x1b") || warns[0] != "warning line" {
		t.Fatalf("expected sanitized warning, got %q", warns[0])
	}
	if !strings.Contains(warns[len(warns)-1], "480 more warning(s) suppressed") {
		t.Fatalf("expected suppressed warning summary, got %q", warns[len(warns)-1])
	}

	// 4. mergeReloadJobs deduplicates JobKeys even when two providers share a Source name.
	prev := []JobItem{
		{Source: "dup", ID: "k1", Title: "old-k1"},
		{Source: "dup", ID: "k2", Title: "old-k2"},
	}
	partial := ProviderLoadResult{
		Jobs: []JobItem{
			{Source: "dup", ID: "k1", Title: "new-k1"},
		},
		Statuses: []ProviderStatus{
			{Name: "dup", Count: 1},
			{Name: "dup", Err: errors.New("secondary failed")},
		},
	}
	merged := mergeReloadJobs(prev, partial)
	if len(merged) != 2 {
		t.Fatalf("expected 2 deduplicated merged jobs, got %d (%+v)", len(merged), merged)
	}
	if merged[0].Title != "new-k1" || merged[1].ID != "k2" {
		t.Fatalf("expected fresh k1 plus retained k2, got %+v", merged)
	}
}
