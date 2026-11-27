package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	maxCrontabBytes    = 1 << 20 // 1 MiB read limit
	maxCrontabJobs     = 2000    // per-file entry cap to protect UI responsiveness
	maxCronSearchDays  = 3660    // 10-year safety bound for occurrence search
	cronProviderSource = "cron"
)

var cronPalette = []string{
	"#0052D4",
	"#10B981",
	"#7D56F4",
	"#F59E0B",
	"#06B6D4",
	"#EC4899",
}

var cronMonthNames = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
	"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

var cronDOWNames = map[string]int{
	"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6,
}

var cronMacros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// CronProvider reads local user cron jobs (via `crontab -l` or an explicit file path)
// in read-only mode and converts them into provider-agnostic JobItems.
type CronProvider struct {
	FilePath string
}

// NewCronProvider creates a read-only CronProvider. When filePath is empty, it reads
// the current user's crontab via `crontab -l`.
func NewCronProvider(filePath string) CronProvider {
	return CronProvider{FilePath: filePath}
}

func (p CronProvider) Name() string {
	return cronProviderSource
}

func (p CronProvider) Load(ctx context.Context) ([]JobItem, error) {
	items, _, err := p.LoadWithWarnings(ctx)
	return items, err
}

func (p CronProvider) LoadWithWarnings(ctx context.Context) ([]JobItem, []string, error) {
	var raw string
	var err error
	if p.FilePath != "" {
		raw, err = readCrontabFile(ctx, p.FilePath)
	} else {
		raw, err = readSystemCrontab(ctx)
	}
	if err != nil {
		return nil, nil, err
	}
	items, warnings := ParseCrontabWithWarnings(raw)
	return items, warnings, nil
}

func readCrontabFile(ctx context.Context, path string) (string, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("crontab read cancelled: %w", err)
		}
	}
	if strings.ContainsAny(path, "\x00\r\n") {
		return "", errors.New("invalid crontab file path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read crontab file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("crontab path must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open crontab file: %w", err)
	}
	defer f.Close()

	fdInfo, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat crontab file: %w", err)
	}
	if !fdInfo.Mode().IsRegular() {
		return "", errors.New("crontab path must be a regular file")
	}

	data, err := io.ReadAll(io.LimitReader(f, maxCrontabBytes+1))
	if err != nil {
		return "", fmt.Errorf("read crontab file: %w", err)
	}
	if len(data) > maxCrontabBytes {
		return "", errors.New("crontab file exceeds 1 MiB limit")
	}
	return string(data), nil
}

func readSystemCrontab(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "crontab", "-l")
	var stdout, stderr limitedBuffer
	stdout.limit = maxCrontabBytes + 1
	stderr.limit = 64 * 1024
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("crontab timed out: %w", ctx.Err())
		}
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("crontab command not found")
		}
		errMsg := strings.ToLower(strings.TrimSpace(stderr.String()))
		if strings.Contains(errMsg, "no crontab for") {
			return "", nil
		}
		if errMsg != "" {
			return "", fmt.Errorf("crontab -l failed: %s", sanitizeText(errMsg))
		}
		return "", fmt.Errorf("crontab -l failed: %w", err)
	}
	if stdout.Len() > maxCrontabBytes {
		return "", errors.New("crontab output exceeds 1 MiB limit")
	}
	return stdout.String(), nil
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len() < b.limit {
		rem := b.limit - b.buf.Len()
		if len(p) > rem {
			b.buf.Write(p[:rem])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) Len() int       { return b.buf.Len() }
func (b *limitedBuffer) String() string { return b.buf.String() }

type cronSchedule struct {
	minute  uint64 // bits 0..59
	hour    uint32 // bits 0..23
	dom     uint32 // bits 1..31
	month   uint16 // bits 1..12
	dow     uint8  // bits 0..6 (0=Sun)
	domStar bool
	dowStar bool
}

func (s cronSchedule) matchesDate(t time.Time) bool {
	if (s.month & (1 << uint(t.Month()))) == 0 {
		return false
	}
	domMatch := (s.dom & (1 << uint(t.Day()))) != 0
	dowMatch := (s.dow & (1 << uint(t.Weekday()))) != 0
	if s.domStar || s.dowStar {
		return domMatch && dowMatch
	}
	// Standard Vixie cron semantics: when both DOM and DOW are explicitly
	// restricted (neither is * or */n), either match satisfies the date.
	return domMatch || dowMatch
}

func (s cronSchedule) occurrences(start, end time.Time, loc *time.Location) []time.Time {
	if !start.Before(end) {
		return nil
	}
	if loc == nil {
		loc = time.Local
	}
	sLoc := start.In(loc)
	eLoc := end.In(loc)
	curDay := time.Date(sLoc.Year(), sLoc.Month(), sLoc.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(eLoc.Year(), eLoc.Month(), eLoc.Day(), 0, 0, 0, 0, loc)

	var out []time.Time
	for days := 0; !curDay.After(endDay) && days < maxCronSearchDays; days++ {
		if s.matchesDate(curDay) {
			y, m, d := curDay.Date()
			for h := 0; h < 24; h++ {
				if (s.hour & (1 << uint(h))) == 0 {
					continue
				}
				for min := 0; min < 60; min++ {
					if (s.minute & (1 << uint(min))) == 0 {
						continue
					}
					ts := time.Date(y, m, d, h, min, 0, 0, loc)
					// Skip nonexistent spring-forward DST wall minutes.
					if ts.Year() != y || ts.Month() != m || ts.Day() != d || ts.Hour() != h || ts.Minute() != min {
						continue
					}
					if ts.Before(start) || !ts.Before(end) {
						continue
					}
					out = append(out, ts)
					if len(out) >= maxOccurrencesPerQuery {
						return out
					}
				}
			}
		}
		curDay = curDay.AddDate(0, 0, 1)
	}
	return out
}

// ParseCrontab parses POSIX/Vixie user-crontab text into provider-agnostic JobItems.
// Malformed lines are skipped safely without aborting valid entries.
func ParseCrontab(content string) []JobItem {
	items, _ := ParseCrontabWithWarnings(content)
	return items
}

// ParseCrontabWithWarnings parses POSIX/Vixie user-crontab text into JobItems and
// returns non-fatal warnings for skipped malformed lines, invalid CRON_TZ directives,
// or entry-cap truncation.
func ParseCrontabWithWarnings(content string) ([]JobItem, []string) {
	var jobs []JobItem
	var warnings []string
	var currentTZ string
	currentLoc := time.Local
	seenIDs := make(map[string]int)

	lines := strings.Split(content, "\n")
	for idx, rawLine := range lines {
		lineNo := idx + 1
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if key, val, ok := parseCronEnvLine(line); ok {
			// Only CRON_TZ changes cron schedule interpretation; TZ sets the
			// child command runtime environment and does not shift the schedule.
			if key == "CRON_TZ" {
				tz := sanitizeText(val)
				if tz == "" {
					currentTZ = ""
					currentLoc = time.Local
				} else if loc, err := time.LoadLocation(tz); err == nil {
					currentTZ = tz
					currentLoc = loc
				} else {
					currentTZ = ""
					currentLoc = time.Local
					warnings = append(warnings, fmt.Sprintf("line %d: invalid CRON_TZ %q (using local)", lineNo, tz))
				}
			}
			continue
		}

		schedLabel, sched, isReboot, command, ok := parseCronJobLine(line)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("line %d: skipped malformed cron entry", lineNo))
			continue
		}

		cleanCmd := sanitizeText(splitCronCommand(command))
		if cleanCmd == "" {
			warnings = append(warnings, fmt.Sprintf("line %d: skipped empty command", lineNo))
			continue
		}

		if len(jobs) >= maxCrontabJobs {
			warnings = append(warnings, fmt.Sprintf("truncated at %d cron jobs", maxCrontabJobs))
			break
		}

		baseID, colorHex := cronJobIdentity(schedLabel, cleanCmd, currentTZ)
		id := baseID
		if count, exists := seenIDs[baseID]; exists {
			seenIDs[baseID] = count + 1
			id = fmt.Sprintf("%s#%d", baseID, count+1)
		} else {
			seenIDs[baseID] = 1
		}

		item := JobItem{
			ID:            id,
			Title:         cleanCmd,
			Source:        cronProviderSource,
			TimeZone:      currentTZ,
			ScheduleLabel: schedLabel,
			Color:         colorHex,
		}
		if !isReboot {
			capturedSched := sched
			capturedLoc := currentLoc
			item.OccurrencesInRange = func(start, end time.Time) []time.Time {
				return capturedSched.occurrences(start, end, capturedLoc)
			}
		}
		jobs = append(jobs, item)
	}
	return jobs, warnings
}

// splitCronCommand returns the executed command portion before the first unescaped
// Vixie cron '%' stdin separator, unescaping '\%' sequences to literal '%'.
func splitCronCommand(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) && raw[i+1] == '%' {
			b.WriteByte('%')
			i++
			continue
		}
		if raw[i] == '%' {
			break
		}
		b.WriteByte(raw[i])
	}
	return strings.TrimSpace(b.String())
}

func parseCronEnvLine(line string) (key, val string, ok bool) {
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return "", "", false
	}
	k := strings.TrimSpace(line[:eq])
	if k == "" || !isValidEnvName(k) {
		return "", "", false
	}
	v := strings.TrimSpace(line[eq+1:])
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
	}
	return k, v, true
}

func isValidEnvName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isLetter {
			return false
		}
		if !isLetter && !isDigit {
			return false
		}
	}
	return true
}

func parseCronJobLine(line string) (schedLabel string, sched cronSchedule, isReboot bool, command string, ok bool) {
	if strings.HasPrefix(line, "@") {
		macro, rest, found := splitFirstField(line)
		if !found {
			return "", cronSchedule{}, false, "", false
		}
		macroLower := strings.ToLower(macro)
		if macroLower == "@reboot" {
			return "@reboot", cronSchedule{}, true, rest, true
		}
		expr, validMacro := cronMacros[macroLower]
		if !validMacro {
			return "", cronSchedule{}, false, "", false
		}
		parsed, err := parseCronExpression(expr)
		if err != nil {
			return "", cronSchedule{}, false, "", false
		}
		return macroLower, parsed, false, rest, true
	}

	fields, rest, found := splitCronFields(line, 5)
	if !found {
		return "", cronSchedule{}, false, "", false
	}
	expr := strings.Join(fields, " ")
	parsed, err := parseCronExpression(expr)
	if err != nil {
		return "", cronSchedule{}, false, "", false
	}
	return expr, parsed, false, rest, true
}

func splitFirstField(line string) (field, rest string, ok bool) {
	fields, rem, found := splitCronFields(line, 1)
	if !found {
		return "", "", false
	}
	return fields[0], rem, true
}

func splitCronFields(line string, n int) (fields []string, rest string, ok bool) {
	fields = make([]string, 0, n)
	i := 0
	for len(fields) < n {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= len(line) {
			return nil, "", false
		}
		start := i
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		fields = append(fields, line[start:i])
	}
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) {
		return nil, "", false
	}
	return fields, line[i:], true
}

func parseCronExpression(expr string) (cronSchedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return cronSchedule{}, fmt.Errorf("expected 5 fields, got %d", len(parts))
	}

	minMask, err := parseCronField(parts[0], 0, 59, nil, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("minute: %w", err)
	}
	hourMask, err := parseCronField(parts[1], 0, 23, nil, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("hour: %w", err)
	}
	domMask, err := parseCronField(parts[2], 1, 31, nil, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("dom: %w", err)
	}
	monMask, err := parseCronField(parts[3], 1, 12, cronMonthNames, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("month: %w", err)
	}
	dowMask, err := parseCronField(parts[4], 0, 7, cronDOWNames, true)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("dow: %w", err)
	}
	// Normalize Sunday (bit 7 -> bit 0).
	if (dowMask & (1 << 7)) != 0 {
		dowMask |= 1 << 0
		dowMask &^= 1 << 7
	}

	return cronSchedule{
		minute:  minMask,
		hour:    uint32(hourMask),
		dom:     uint32(domMask),
		month:   uint16(monMask),
		dow:     uint8(dowMask),
		domStar: parts[2] == "*" || strings.HasPrefix(parts[2], "*/"),
		dowStar: parts[4] == "*" || strings.HasPrefix(parts[4], "*/"),
	}, nil
}

func parseCronField(field string, minVal, maxVal int, names map[string]int, isDOW bool) (uint64, error) {
	if field == "" {
		return 0, errors.New("empty field")
	}
	var mask uint64
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return 0, errors.New("empty list item")
		}
		step := 1
		rangePart := part
		if strings.Contains(part, "/") {
			slashParts := strings.Split(part, "/")
			if len(slashParts) != 2 || slashParts[0] == "" || slashParts[1] == "" {
				return 0, fmt.Errorf("invalid step %q", part)
			}
			s, err := strconv.Atoi(slashParts[1])
			if err != nil || s <= 0 {
				return 0, fmt.Errorf("invalid step %q", part)
			}
			step = s
			rangePart = slashParts[0]
		}

		var startVal, endVal int
		hasStep := strings.Contains(part, "/")
		if rangePart == "*" {
			startVal = minVal
			endVal = maxVal
			if isDOW {
				endVal = 6
			}
		} else if strings.Contains(rangePart, "-") {
			bounds := strings.Split(rangePart, "-")
			if len(bounds) != 2 || bounds[0] == "" || bounds[1] == "" {
				return 0, fmt.Errorf("invalid range %q", rangePart)
			}
			a, err := parseCronAtom(bounds[0], minVal, maxVal, names)
			if err != nil {
				return 0, err
			}
			b, err := parseCronAtom(bounds[1], minVal, maxVal, names)
			if err != nil {
				return 0, err
			}
			if isDOW && b == 0 && a > 0 {
				b = 7
			}
			if a > b {
				return 0, fmt.Errorf("inverted range %q", rangePart)
			}
			startVal = a
			endVal = b
		} else {
			v, err := parseCronAtom(rangePart, minVal, maxVal, names)
			if err != nil {
				return 0, err
			}
			startVal = v
			if hasStep {
				if isDOW && startVal == 7 {
					startVal = 0
				}
				endVal = maxVal
				if isDOW {
					endVal = 6
				}
			} else {
				endVal = v
			}
		}

		for v := startVal; v <= endVal; v += step {
			mask |= 1 << uint(v)
		}
	}
	if mask == 0 {
		return 0, errors.New("empty bitmask")
	}
	return mask, nil
}

func parseCronAtom(raw string, minVal, maxVal int, names map[string]int) (int, error) {
	if names != nil {
		if v, ok := names[strings.ToUpper(raw)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < minVal || v > maxVal {
		return 0, fmt.Errorf("value %q out of bounds [%d..%d]", raw, minVal, maxVal)
	}
	return v, nil
}

func cronJobIdentity(schedule, command, tz string) (id string, colorHex string) {
	sum := sha256.Sum256([]byte(schedule + "\x00" + command + "\x00" + tz))
	id = "cron-" + hex.EncodeToString(sum[:8])
	colorHex = cronPalette[int(sum[8])%len(cronPalette)]
	return id, colorHex
}
