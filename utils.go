package main

import (
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// ansiEscapeRe matches 7-bit ESC and 8-bit C1 CSI/OSC sequences so untrusted
// job labels cannot inject terminal control sequences.
var ansiEscapeRe = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[PX^_][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])|\x{009B}[0-9;?]*[ -/]*[@-~]|\x{009D}[^\x07\x1b\x{009C}]*(?:\x07|\x1b\\|\x{009C})`)

// isBlockedControlRune reports whether r is a control rune or unsafe format rune
// (BiDi overrides/isolates, zero-width spaces, BOM), while preserving ZWJ (\u200D)
// so joined emoji/grapheme clusters render cleanly.
func isBlockedControlRune(r rune) bool {
	if unicode.IsControl(r) || r == unicode.ReplacementChar {
		return true
	}
	return unicode.Is(unicode.Cf, r) && r != '\u200D'
}

// sanitizeText strips terminal escape sequences, control characters (Cc), and
// unsafe Unicode format characters (Cf: bidi overrides, zero-width spaces),
// collapsing whitespace runs into single spaces while preserving ZWJ graphemes.
func sanitizeText(s string) string {
	if s == "" {
		return ""
	}
	cleaned := ansiEscapeRe.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(cleaned))
	prevSpace := false
	for _, r := range cleaned {
		if r == '\r' || r == '\n' || r == '\t' {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		if isBlockedControlRune(r) {
			continue
		}
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// truncateWidth truncates s to fit within maxWidth terminal cells using
// grapheme-cluster width calculation matching Lipgloss, adding an ellipsis when cut.
func truncateWidth(s string, maxWidth int) string {
	s = sanitizeText(s)
	if maxWidth <= 0 || s == "" {
		return ""
	}
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return ansi.Truncate(s, 1, "")
	}
	return ansi.Truncate(s, maxWidth, "…")
}

// normalizeHexColor validates #RGB or #RRGGBB hex strings and returns an
// uppercase #RRGGBB color string.
func normalizeHexColor(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "#") {
		return "", false
	}
	hex := s[1:]
	if len(hex) != 3 && len(hex) != 6 {
		return "", false
	}
	for i := 0; i < len(hex); i++ {
		c := hex[i]
		isDigit := c >= '0' && c <= '9'
		isLower := c >= 'a' && c <= 'f'
		isUpper := c >= 'A' && c <= 'F'
		if !isDigit && !isLower && !isUpper {
			return "", false
		}
	}
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	return "#" + strings.ToUpper(hex), true
}

func clampInt(v, minVal, maxVal int) int {
	if maxVal < minVal {
		return minVal
	}
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

// addMonths shifts t by delta months, clamping the day of month to the target
// month's last valid day (e.g. Jan 31 + 1 month -> Feb 28/29 instead of Mar 3).
func addMonths(t time.Time, delta int) time.Time {
	y, m, d := t.Date()
	firstOfTarget := time.Date(y, m+time.Month(delta), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	lastDay := firstOfTarget.AddDate(0, 1, -1).Day()
	if d > lastDay {
		d = lastDay
	}
	return time.Date(firstOfTarget.Year(), firstOfTarget.Month(), d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// wallHourTime constructs the timestamp for a wall-clock hour on day's calendar date.
// It returns ok=false when that wall-clock hour does not exist due to a spring-forward DST gap.
func wallHourTime(day time.Time, hour int) (time.Time, bool) {
	loc := day.Location()
	if loc == nil {
		loc = time.Local
	}
	t := time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, loc)
	if t.Day() != day.Day() || t.Hour() != hour {
		return time.Time{}, false
	}
	return t, true
}

// wallHourSpan returns the half-open UTC interval [start, end) covering all instants
// whose local wall-clock hour on day equals hour (1 hour normally, 2 hours on DST fall-back,
// and ok=false on a skipped spring-forward DST hour).
func wallHourSpan(day time.Time, hour int) (start, end time.Time, ok bool) {
	start, ok = wallHourTime(day, hour)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	for {
		prev := start.Add(-time.Hour)
		if !sameDay(prev, start) || prev.Hour() != hour {
			break
		}
		start = prev
	}
	end = start.Add(time.Hour)
	for sameDay(end, start) && end.Hour() == hour {
		end = end.Add(time.Hour)
	}
	return start, end, true
}
