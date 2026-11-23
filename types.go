package main

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	sidebarWidth              = 20
	sidebarFullWidth          = sidebarWidth + 1 // sidebar + 1-col right border
	minTermWidth              = 44
	minTermHeight             = 14
	defaultJobColor           = "#0052D4"
	maxOccurrencesPerQuery    = 100000
	maxDurationLookback       = 366 * 24 * time.Hour
	maxLegacyDurationLookback = 31 * 24 * time.Hour
)

type sessionState int

const (
	viewAgenda sessionState = iota
	viewMonth
	viewWeek
	viewDay
	viewSettings
)

func (s sessionState) String() string {
	switch s {
	case viewAgenda:
		return "Agenda"
	case viewMonth:
		return "Month"
	case viewWeek:
		return "Week"
	case viewDay:
		return "Day"
	case viewSettings:
		return "Settings"
	default:
		return "Unknown"
	}
}

// JobKey uniquely identifies a job across provider refreshes using its source and ID.
type JobKey struct {
	Source string
	ID     string
}

// JobOverride holds user-customized display metadata keyed by JobKey.
type JobOverride struct {
	Alias    string
	HasAlias bool
	Color    string
}

// JobProvider defines the contract for future read-only job source adapters
// (cron, systemd timers, Kubernetes CronJobs, etc.).
type JobProvider interface {
	Name() string
	Load(ctx context.Context) ([]JobItem, error)
}

// JobItem is a provider-agnostic representation of a scheduled task for UI rendering.
type JobItem struct {
	ID            string
	Title         string
	Alias         string
	Source        string
	TimeZone      string
	Duration      time.Duration
	ScheduleLabel string
	Color         string

	// Optional range occurrence provider (preferred for future adapters).
	OccurrencesInRange func(start, end time.Time) []time.Time
	// Legacy/simple predicate matchers when OccurrencesInRange is nil.
	MatchesDay  func(day time.Time) bool
	MatchesHour func(hour time.Time) bool
}

// Key returns the stable source-namespaced identifier for settings mapping.
func (j JobItem) Key() JobKey {
	return JobKey{
		Source: j.Source,
		ID:     j.ID,
	}
}

// HasStableKey reports whether the job carries a non-empty ID safe for settings overrides.
func (j JobItem) HasStableKey() bool {
	return j.ID != ""
}

// Location resolves the job's IANA timezone, falling back to time.Local.
func (j JobItem) Location() *time.Location {
	if tz := sanitizeText(j.TimeZone); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}

// DisplayName returns the sanitized alias when set, otherwise the sanitized title.
func (j JobItem) DisplayName() string {
	if name := sanitizeText(j.Alias); name != "" {
		return name
	}
	if title := sanitizeText(j.Title); title != "" {
		return title
	}
	return "Untitled Job"
}

// DisplaySchedule returns a sanitized schedule label for UI rendering.
func (j JobItem) DisplaySchedule() string {
	if sched := sanitizeText(j.ScheduleLabel); sched != "" {
		return sched
	}
	return "Unscheduled"
}

// DisplaySource returns a sanitized source tag (empty if none).
func (j JobItem) DisplaySource() string {
	return sanitizeText(j.Source)
}

// DisplayColor returns a validated #RRGGBB hex color or the default accent color.
func (j JobItem) DisplayColor() string {
	if hex, ok := normalizeHexColor(j.Color); ok {
		return hex
	}
	return defaultJobColor
}

// LipglossColor returns the validated color as a lipgloss.Color.
func (j JobItem) LipglossColor() lipgloss.Color {
	return lipgloss.Color(j.DisplayColor())
}

func (j JobItem) clampedDuration() time.Duration {
	if j.Duration <= 0 {
		return 0
	}
	if j.Duration > maxDurationLookback {
		return maxDurationLookback
	}
	return j.Duration
}

func (j JobItem) effectiveLocation(fallback *time.Location) *time.Location {
	if tz := sanitizeText(j.TimeZone); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	if fallback != nil {
		return fallback
	}
	return time.Local
}

// matchesViewerDay checks whether j.MatchesDay matches any calendar date in jobLoc
// overlapping the viewer interval [dayStart, dayEnd).
func (j JobItem) matchesViewerDay(dayStart, dayEnd time.Time, jobLoc *time.Location) bool {
	if j.MatchesDay == nil {
		return true
	}
	d0 := dayStart.In(jobLoc)
	if j.MatchesDay(d0) {
		return true
	}
	d1 := dayEnd.Add(-time.Nanosecond).In(jobLoc)
	if !sameDay(d0, d1) && j.MatchesDay(d1) {
		return true
	}
	return false
}

func (j JobItem) boundedOccurrences(start, end time.Time) []time.Time {
	if j.OccurrencesInRange == nil {
		return nil
	}
	queryStart := start.Add(-j.clampedDuration())
	occs := j.OccurrencesInRange(queryStart, end)
	if len(occs) > maxOccurrencesPerQuery {
		return occs[:maxOccurrencesPerQuery]
	}
	return occs
}

func occurrenceOverlaps(occ time.Time, dur time.Duration, winStart, winEnd time.Time) bool {
	if dur <= 0 {
		return !occ.Before(winStart) && occ.Before(winEnd)
	}
	return occ.Before(winEnd) && occ.Add(dur).After(winStart)
}

// OccursOnDay reports whether the job has at least one occurrence on day.
func (j JobItem) OccursOnDay(day time.Time) bool {
	loc := day.Location()
	if loc == nil {
		loc = time.Local
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	dur := j.clampedDuration()

	if j.OccurrencesInRange != nil {
		for _, occ := range j.boundedOccurrences(start, end) {
			if occurrenceOverlaps(occ, dur, start, end) {
				return true
			}
		}
		return false
	}
	jobLoc := j.effectiveLocation(loc)
	if j.MatchesDay != nil && j.MatchesHour == nil {
		return j.matchesViewerDay(start, end, jobLoc)
	}
	if j.MatchesHour != nil {
		if dur > 0 || j.MatchesDay != nil {
			dayGrid := indexHourGrid([]JobItem{j}, day, 1)[0]
			for h := 0; h < 24; h++ {
				if len(dayGrid[h]) > 0 {
					return true
				}
			}
			return false
		}
		for h := 0; h < 24; h++ {
			if slotStart, slotEnd, ok := wallHourSpan(day, h); ok {
				for slot := slotStart; slot.Before(slotEnd); slot = slot.Add(time.Hour) {
					if j.MatchesHour(slot.In(jobLoc)) {
						return true
					}
				}
			}
		}
	}
	return false
}

// OccursInHour reports whether the job has at least one occurrence during the hour slot.
func (j JobItem) OccursInHour(hour time.Time) bool {
	start, end, ok := wallHourSpan(hour, hour.Hour())
	if !ok {
		return false
	}
	dur := j.clampedDuration()
	if j.OccurrencesInRange != nil {
		for _, occ := range j.boundedOccurrences(start, end) {
			if occurrenceOverlaps(occ, dur, start, end) {
				return true
			}
		}
		return false
	}
	loc := hour.Location()
	if loc == nil {
		loc = time.Local
	}
	jobLoc := j.effectiveLocation(loc)
	if j.MatchesHour != nil {
		if dur > time.Hour {
			dayGrid := indexHourGrid([]JobItem{j}, hour, 1)[0]
			return len(dayGrid[hour.Hour()]) > 0
		}
		for slot := start; slot.Before(end); slot = slot.Add(time.Hour) {
			jobSlot := slot.In(jobLoc)
			if j.MatchesHour(jobSlot) && (j.MatchesDay == nil || j.MatchesDay(jobSlot)) {
				return true
			}
		}
		return false
	}
	if j.MatchesDay != nil && hour.Hour() == 0 {
		dayStart := time.Date(hour.Year(), hour.Month(), hour.Day(), 0, 0, 0, 0, loc)
		return j.matchesViewerDay(dayStart, dayStart.AddDate(0, 0, 1), jobLoc)
	}
	return false
}

// JobsForDay returns all jobs occurring on the given calendar day.
func JobsForDay(jobs []JobItem, day time.Time) []JobItem {
	var active []JobItem
	for _, job := range jobs {
		if job.OccursOnDay(day) {
			active = append(active, job)
		}
	}
	return active
}

// JobsForHour returns all jobs occurring within the given hour slot.
func JobsForHour(jobs []JobItem, hour time.Time) []JobItem {
	var active []JobItem
	for _, job := range jobs {
		if job.OccursInHour(hour) {
			active = append(active, job)
		}
	}
	return active
}

// indexDayJobs pre-computes per-day active jobs over [startDay, startDay+numDays)
// so Month and Agenda views evaluate OccurrencesInRange once per job per render.
func indexDayJobs(jobs []JobItem, startDay time.Time, numDays int) [][]JobItem {
	if numDays <= 0 {
		return nil
	}
	grid := make([][]JobItem, numDays)
	if len(jobs) == 0 {
		return grid
	}
	loc := startDay.Location()
	if loc == nil {
		loc = time.Local
	}
	start := time.Date(startDay.Year(), startDay.Month(), startDay.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, numDays)
	dayDates := make([]time.Time, numDays)
	dayEnds := make([]time.Time, numDays)
	for d := 0; d < numDays; d++ {
		dayDates[d] = start.AddDate(0, 0, d)
		dayEnds[d] = start.AddDate(0, 0, d+1)
	}

	for _, job := range jobs {
		if job.OccurrencesInRange != nil {
			dur := job.clampedDuration()
			seen := make([]bool, numDays)
			for _, occ := range job.boundedOccurrences(start, end) {
				if !occurrenceOverlaps(occ, dur, start, end) {
					continue
				}
				if dur <= 0 {
					localOcc := occ.In(loc)
					for d, dayDate := range dayDates {
						if !seen[d] && sameDay(localOcc, dayDate) {
							seen[d] = true
							grid[d] = append(grid[d], job)
							break
						}
					}
				} else {
					for d := 0; d < numDays; d++ {
						if !seen[d] && occurrenceOverlaps(occ, dur, dayDates[d], dayEnds[d]) {
							seen[d] = true
							grid[d] = append(grid[d], job)
						}
					}
				}
			}
			continue
		}
		for d, dayDate := range dayDates {
			if job.OccursOnDay(dayDate) {
				grid[d] = append(grid[d], job)
			}
		}
	}
	return grid
}

type hourSpan struct {
	start time.Time
	end   time.Time
	ok    bool
}

// indexHourGrid pre-computes a [numDays][24] hour grid over [startDay, startDay+numDays)
// handling DST gaps/folds, TimeZone conversion, Duration overlap, and capped range results.
func indexHourGrid(jobs []JobItem, startDay time.Time, numDays int) [][24][]JobItem {
	if numDays <= 0 {
		return nil
	}
	grid := make([][24][]JobItem, numDays)
	if len(jobs) == 0 {
		return grid
	}
	loc := startDay.Location()
	if loc == nil {
		loc = time.Local
	}
	start := time.Date(startDay.Year(), startDay.Month(), startDay.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, numDays)
	dayDates := make([]time.Time, numDays)
	spans := make([][24]hourSpan, numDays)
	for d := 0; d < numDays; d++ {
		dayDates[d] = start.AddDate(0, 0, d)
		for h := 0; h < 24; h++ {
			s, e, ok := wallHourSpan(dayDates[d], h)
			spans[d][h] = hourSpan{start: s, end: e, ok: ok}
		}
	}

	for _, job := range jobs {
		seen := make([][24]bool, numDays)
		dur := job.clampedDuration()
		if job.OccurrencesInRange != nil {
			for _, occ := range job.boundedOccurrences(start, end) {
				if !occurrenceOverlaps(occ, dur, start, end) {
					continue
				}
				if dur <= 0 {
					localOcc := occ.In(loc)
					hIdx := localOcc.Hour()
					for d := 0; d < numDays; d++ {
						if sameDay(localOcc, dayDates[d]) && hIdx >= 0 && hIdx < 24 && !seen[d][hIdx] {
							seen[d][hIdx] = true
							grid[d][hIdx] = append(grid[d][hIdx], job)
							break
						}
					}
					continue
				}
				for d := 0; d < numDays; d++ {
					for h := 0; h < 24; h++ {
						sp := spans[d][h]
						if sp.ok && !seen[d][h] && occurrenceOverlaps(occ, dur, sp.start, sp.end) {
							seen[d][h] = true
							grid[d][h] = append(grid[d][h], job)
						}
					}
				}
			}
			continue
		}

		jobLoc := job.effectiveLocation(loc)
		if job.MatchesHour == nil {
			if job.MatchesDay != nil {
				for d := 0; d < numDays; d++ {
					dayEnd := start.AddDate(0, 0, d+1)
					if job.matchesViewerDay(dayDates[d], dayEnd, jobLoc) {
						grid[d][0] = append(grid[d][0], job)
					}
				}
			}
			continue
		}

		legacyDur := dur
		if legacyDur > maxLegacyDurationLookback {
			legacyDur = maxLegacyDurationLookback
		}
		lookbackDays := 0
		if legacyDur > time.Hour {
			lookbackDays = int((legacyDur + 23*time.Hour) / (24 * time.Hour))
		}
		for d := -lookbackDays; d < numDays; d++ {
			dayDate := start.AddDate(0, 0, d)
			for h := 0; h < 24; h++ {
				var sp hourSpan
				if d >= 0 {
					sp = spans[d][h]
				} else {
					s, e, ok := wallHourSpan(dayDate, h)
					sp = hourSpan{start: s, end: e, ok: ok}
				}
				if !sp.ok {
					continue
				}
				for slot := sp.start; slot.Before(sp.end); slot = slot.Add(time.Hour) {
					jobSlot := slot.In(jobLoc)
					if !job.MatchesHour(jobSlot) || (job.MatchesDay != nil && !job.MatchesDay(jobSlot)) {
						continue
					}
					if legacyDur <= time.Hour {
						if d >= 0 && !seen[d][h] {
							seen[d][h] = true
							grid[d][h] = append(grid[d][h], job)
						}
						break
					}
					for td := maxInt(0, d); td < numDays; td++ {
						for th := 0; th < 24; th++ {
							tsp := spans[td][th]
							if tsp.ok && !seen[td][th] && occurrenceOverlaps(slot, legacyDur, tsp.start, tsp.end) {
								seen[td][th] = true
								grid[td][th] = append(grid[td][th], job)
							}
						}
					}
				}
			}
		}
	}
	return grid
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// indexWeekJobs pre-computes a 7x24 hour grid for the week starting at weekStart
// so Week rendering performs one pass over jobs rather than 168 repeated scans.
func indexWeekJobs(jobs []JobItem, weekStart time.Time) [7][24][]JobItem {
	var grid [7][24][]JobItem
	rows := indexHourGrid(jobs, weekStart, 7)
	for d := 0; d < 7 && d < len(rows); d++ {
		grid[d] = rows[d]
	}
	return grid
}

// indexDayHourJobs pre-computes a 24-hour grid for a single calendar day.
func indexDayHourJobs(jobs []JobItem, day time.Time) [24][]JobItem {
	var dayGrid [24][]JobItem
	rows := indexHourGrid(jobs, day, 1)
	if len(rows) > 0 {
		dayGrid = rows[0]
	}
	return dayGrid
}

// demoJobs returns generic in-memory sample jobs for UI preview (--demo).
func demoJobs() []JobItem {
	return []JobItem{
		{
			ID:            "demo-backup",
			Title:         "Database Snapshot",
			Alias:         "DB Backup",
			Source:        "task",
			ScheduleLabel: "Daily 02:00",
			Color:         "#0052D4",
			MatchesDay:    func(d time.Time) bool { return true },
			MatchesHour:   func(h time.Time) bool { return h.Hour() == 2 },
		},
		{
			ID:            "demo-cert-check",
			Title:         "TLS Certificate Renewal Check",
			Source:        "service",
			ScheduleLabel: "Mon, Thu 09:00",
			Color:         "#10B981",
			MatchesDay: func(d time.Time) bool {
				return d.Weekday() == time.Monday || d.Weekday() == time.Thursday
			},
			MatchesHour: func(h time.Time) bool {
				return (h.Weekday() == time.Monday || h.Weekday() == time.Thursday) && h.Hour() == 9
			},
		},
		{
			ID:            "demo-maint",
			Title:         "System Maintenance & Prune",
			Alias:         "System Maint",
			Source:        "system",
			ScheduleLabel: "Fri 10:00",
			Color:         "#F59E0B",
			MatchesDay:    func(d time.Time) bool { return d.Weekday() == time.Friday },
			MatchesHour:   func(h time.Time) bool { return h.Weekday() == time.Friday && h.Hour() == 10 },
		},
		{
			ID:            "demo-sync",
			Title:         "Artifact Mirror Sync",
			Source:        "worker",
			ScheduleLabel: "Weekdays 14:00",
			Color:         "#EC4899",
			MatchesDay: func(d time.Time) bool {
				return d.Weekday() >= time.Monday && d.Weekday() <= time.Friday
			},
			MatchesHour: func(h time.Time) bool {
				return h.Weekday() >= time.Monday && h.Weekday() <= time.Friday && h.Hour() == 14
			},
		},
		{
			ID:            "demo-report",
			Title:         "Weekly Digest",
			Source:        "pipeline",
			ScheduleLabel: "Fri 10:00",
			Color:         "#7D56F4",
			MatchesDay:    func(d time.Time) bool { return d.Weekday() == time.Friday },
			MatchesHour:   func(h time.Time) bool { return h.Weekday() == time.Friday && h.Hour() == 10 },
		},
	}
}

func formatDayCountLabel(n int) string {
	if n == 1 {
		return "1 job"
	}
	return fmt.Sprintf("%d jobs", n)
}
