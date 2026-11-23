package main

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	sidebarWidth     = 20
	sidebarFullWidth = sidebarWidth + 1 // sidebar + 1-col right border
	minTermWidth     = 44
	minTermHeight    = 14
	defaultJobColor  = "#0052D4"
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

// OccursOnDay reports whether the job has at least one occurrence on day.
func (j JobItem) OccursOnDay(day time.Time) bool {
	if j.OccurrencesInRange != nil {
		loc := day.Location()
		if loc == nil {
			loc = time.Local
		}
		start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
		end := start.AddDate(0, 0, 1)
		return len(j.OccurrencesInRange(start, end)) > 0
	}
	if j.MatchesDay != nil {
		return j.MatchesDay(day)
	}
	if j.MatchesHour != nil {
		for h := 0; h < 24; h++ {
			if slot, ok := wallHourTime(day, h); ok && j.MatchesHour(slot) {
				return true
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
	if j.OccurrencesInRange != nil {
		return len(j.OccurrencesInRange(start, end)) > 0
	}
	if j.MatchesHour != nil {
		for slot := start; slot.Before(end); slot = slot.Add(time.Hour) {
			if j.MatchesHour(slot) {
				return true
			}
		}
		return false
	}
	if j.MatchesDay != nil && hour.Hour() == 0 {
		return j.MatchesDay(hour)
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
	for d := 0; d < numDays; d++ {
		dayDates[d] = start.AddDate(0, 0, d)
	}

	for _, job := range jobs {
		if job.OccurrencesInRange != nil {
			seen := make([]bool, numDays)
			for _, occ := range job.OccurrencesInRange(start, end) {
				localOcc := occ.In(loc)
				if localOcc.Before(start) || !localOcc.Before(end) {
					continue
				}
				for d, dayDate := range dayDates {
					if !seen[d] && sameDay(localOcc, dayDate) {
						seen[d] = true
						grid[d] = append(grid[d], job)
						break
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

// indexWeekJobs pre-computes a 7x24 hour grid for the week starting at weekStart
// so Week rendering performs one pass over jobs rather than 168 repeated scans.
func indexWeekJobs(jobs []JobItem, weekStart time.Time) [7][24][]JobItem {
	var grid [7][24][]JobItem
	if len(jobs) == 0 {
		return grid
	}
	loc := weekStart.Location()
	if loc == nil {
		loc = time.Local
	}
	start := time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 7)
	var dayDates [7]time.Time
	for d := 0; d < 7; d++ {
		dayDates[d] = start.AddDate(0, 0, d)
	}

	for _, job := range jobs {
		if job.OccurrencesInRange != nil {
			var seen [7][24]bool
			for _, occ := range job.OccurrencesInRange(start, end) {
				localOcc := occ.In(loc)
				if localOcc.Before(start) || !localOcc.Before(end) {
					continue
				}
				dIdx := -1
				for d := 0; d < 7; d++ {
					if sameDay(localOcc, dayDates[d]) {
						dIdx = d
						break
					}
				}
				hIdx := localOcc.Hour()
				if dIdx >= 0 && dIdx < 7 && hIdx >= 0 && hIdx < 24 && !seen[dIdx][hIdx] {
					seen[dIdx][hIdx] = true
					grid[dIdx][hIdx] = append(grid[dIdx][hIdx], job)
				}
			}
			continue
		}
		if job.MatchesHour == nil {
			if job.MatchesDay != nil {
				for d := 0; d < 7; d++ {
					if job.MatchesDay(dayDates[d]) {
						grid[d][0] = append(grid[d][0], job)
					}
				}
			}
			continue
		}
		for d := 0; d < 7; d++ {
			dayDate := dayDates[d]
			for h := 0; h < 24; h++ {
				slotStart, slotEnd, ok := wallHourSpan(dayDate, h)
				if !ok {
					continue
				}
				for slot := slotStart; slot.Before(slotEnd); slot = slot.Add(time.Hour) {
					if job.MatchesHour(slot) {
						grid[d][h] = append(grid[d][h], job)
						break
					}
				}
			}
		}
	}
	return grid
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
