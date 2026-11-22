package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// CronJob represents a single cron entry
type CronJob struct {
	Schedule cron.Schedule
	Command  string
	RawLine  string
}

// LoadCronTab loads jobs from a file (or 'crontab -l' if filename is empty)
func LoadCronTab(filename string) ([]CronJob, error) {
	var scanner *bufio.Scanner
	if filename == "" {
		cmd := exec.Command("crontab", "-l")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		scanner = bufio.NewScanner(stdout)
		// We'll let the command finish in the background or wait for it?
		// Ideally we read until EOF.
		defer cmd.Wait() 
	} else {
		f, err := os.Open(filename)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		scanner = bufio.NewScanner(f)
	}

	var jobs []CronJob
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 6 {
			// Check for special strings like @daily
			if len(parts) >= 2 && strings.HasPrefix(parts[0], "@") {
				// Proceed
			} else {
				continue 
			}
		}

		var spec, command string
		var schedule cron.Schedule
		var err error

		if strings.HasPrefix(parts[0], "@") {
			spec = parts[0]
			command = strings.Join(parts[1:], " ")
			schedule, err = parser.Parse(spec)
		} else {
			// Try standard 5 fields
			if len(parts) >= 6 {
				spec = strings.Join(parts[:5], " ")
				command = strings.Join(parts[5:], " ")
				schedule, err = parser.Parse(spec)
			} else {
				err = fmt.Errorf("invalid format")
			}
		}

		if err == nil {
			jobs = append(jobs, CronJob{
				Schedule: schedule,
				Command:  command,
				RawLine:  line,
			})
		} else {
			// Silent fail or log? For TUI, better silent or debug log.
			// fmt.Printf("Error parsing line '%s': %v\n", line, err)
		}
	}

	return jobs, scanner.Err()
}

// GetJobsForHour returns a list of jobs that run within the given hour
func GetJobsForHour(jobs []CronJob, t time.Time) []CronJob {
	var activeJobs []CronJob
	
	// Define the hour window
	start := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
	end := start.Add(time.Hour)

	for _, job := range jobs {
		// Check if the job runs between start (inclusive) and end (exclusive)
		// We subtract 1 nanosecond from start to make the search inclusive of the start time itself
		nextRun := job.Schedule.Next(start.Add(-1 * time.Nanosecond))
		if nextRun.Before(end) {
			activeJobs = append(activeJobs, job)
		}
	}
		return activeJobs
	}
	
	// GetJobsForDay returns a list of jobs that run within the given day
	func GetJobsForDay(jobs []CronJob, t time.Time) []CronJob {
		var activeJobs []CronJob
		
		// Define the day window
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		end := start.AddDate(0, 0, 1)
	
		for _, job := range jobs {
			// Check if the job runs between start (inclusive) and end (exclusive)
			nextRun := job.Schedule.Next(start.Add(-1 * time.Nanosecond))
			if nextRun.Before(end) {
				activeJobs = append(activeJobs, job)
			}
		}
		return activeJobs
	}
	