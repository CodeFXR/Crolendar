# Crolendar Security & Bug Remediation Log

This document records all security vulnerabilities and functional/layout bugs identified and patched in Crolendar, along with their regression tests and preventive rules for future phases.

---

## 1. Security Rules for All Changes

1. **Untrusted Job Metadata:** Treat all job titles, aliases, descriptions, unit names, namespaces, schedule labels, and colors as untrusted input. Pass every display string through `sanitizeText` (or `truncateWidth`) before rendering.
2. **Terminal Escape & Unicode Spoofing Isolation:** Never emit raw `\x1b`, `\r`, `\n`, `\t`, C0/C1 control characters, or Unicode format/bidi characters (`Cf`) from data fields into Bubble Tea views.
3. **Color & Input Validation:** Validate hex colors through `normalizeHexColor` before storing or passing to `lipgloss.Color()`.
4. **No Unsanitized Shell Execution:** Future job providers must execute external binaries with explicit argument arrays (`exec.Command("bin", arg1, arg2)`), never via `sh -c` or string interpolation.
5. **Safe File Persistence:** Future config/file writes must resolve under XDG user directories, validate paths against traversal/newlines, and use atomic temporary-file replacement (`0600`/`0644`, no symlink following).

---

## 2. Patched Bugs & Vulnerabilities

### SEC-01: Terminal Escape, ANSI Control & Unicode Bidi/Zero-Width Injection
- **Severity:** High
- **Affected Files:** `utils.go`, `types.go`, `ui_agenda.go`, `ui_month.go`, `ui_week.go`, `ui_day.go`, `ui_settings.go`, `main.go`
- **Root Cause:** Job strings were originally rendered without stripping escape sequences, and the first `sanitizeText` pass stripped `Cc` controls but left Unicode `Cf` format controls (bidi overrides `U+202A–U+202E`, isolates `U+2066–U+2069`, zero-width chars `U+200B–U+200D`, `U+FEFF`, `U+2060`) and DCS/SOS/PM/APC sequences.
- **Patch:** Extended `ansiEscapeRe` in `utils.go` to match CSI, OSC, DCS/SOS/PM/APC, and single-char ESC sequences, and updated `sanitizeText` to strip `unicode.IsControl(r)`, `unicode.Is(unicode.Cf, r)`, and `U+FFFD`.
- **Regression Tests:** `TestSanitizeTextStripsEscapesControlsAndUnicodeSpoofing`, `TestJobItemDefaultsAndHourOnlyOccurrence` (`ui_test.go`).

### SEC-02: UTF-8 Byte-Slicing & Grapheme Width Mismatch
- **Severity:** Medium
- **Affected Files:** `utils.go`, `ui_month.go`, `ui_week.go`, `ui_day.go`, `ui_agenda.go`, `ui_settings.go`
- **Root Cause:** Byte slicing (`s[:max]`) corrupted multi-byte UTF-8, and `go-runewidth` disagreed with Lipgloss (`uniseg`) on multi-codepoint graphemes (such as two-codepoint regional flag clusters), overflowing cells by 1+ columns. `ui_day.go` also omitted truncation.
- **Patch:** Replaced `go-runewidth` in `truncateWidth` with `github.com/charmbracelet/x/ansi` (`ansi.StringWidth` and `ansi.Truncate`), matching Lipgloss grapheme measurement across all views and the footer.
- **Regression Tests:** `TestTruncateWidthMatchesLipglossAcrossGraphemes` (`ui_test.go`).

### SEC-03: Negative-Count `strings.Repeat` Panic, Viewport Overflow & Narrow Month Corruption
- **Severity:** High
- **Affected Files:** `main.go`, `ui_agenda.go`, `ui_month.go`, `ui_week.go`, `ui_day.go`, `ui_settings.go`
- **Root Cause:**
  1. `ui_agenda.go` panicked on narrow widths (`availWidth - 2 < 0`) and overflowed `halfHeight` on 24-row terminals.
  2. `ui_month.go` forced `colWidth >= 4` even when `availWidth < 42` (terminals 44–62 wide), overflowing 7 bordered columns and dropping days 12–31.
  3. `ui_week.go` had header width drift (`w` vs `w + 1`), wrapped `timeBottom`, misaligned `h == 23`, and computed `strings.Repeat(" ", width)` before checking `width <= 0` in `formatHourSlotLines`.
- **Patch:**
  - Enforced `minTermWidth` (`44`) and `minTermHeight` (`14`) guard in `model.View()`, clamped `layoutDimensions()`, and moved `width <= 0` guard above `strings.Repeat` in `formatHourSlotLines`.
  - Added `renderCompactMonthView` fallback when `availWidth / 7 < 6` so all days 1–31 render cleanly at 44–62 column widths.
  - Standardized Week/Day rows to 3 lines per hour + closing bottom cap (`73` lines total) and aligned Week headers to `w + 1`.
- **Regression Tests:** `TestRenderAllViewsAcrossSizesBoundedAndMonthComplete`, `TestWeekAndDayGridUniformRowHeightsAndNegativeGuard` (`ui_test.go`).

### SEC-04: Unvalidated Hex Color Input in Settings
- **Severity:** Low / Input Validation
- **Affected Files:** `utils.go`, `main.go`, `ui_settings.go`
- **Root Cause:** Settings accepted arbitrary strings into color state and `lipgloss.Color()` without `#RGB` / `#RRGGBB` validation.
- **Patch:** Added `normalizeHexColor` in `utils.go`; `handleSettingsInput` rejects invalid colors with inline `settingsError`, and `JobItem.DisplayColor()` falls back to `#0052D4`.
- **Regression Tests:** `TestNormalizeHexColor`, `TestSettingsAtomicCancelAndKeyedOverrides` (`ui_test.go`).

### SEC-05: Settings Partial-Save on Cancel, Index Keying, Input Overflow & Cursor Blink Loss
- **Severity:** High (Data Integrity / UX)
- **Affected Files:** `main.go`, `types.go`, `ui_settings.go`
- **Root Cause:**
  1. Pressing `Enter` on Alias immediately mutated `m.jobs[idx].Alias` before Color was confirmed, so `Esc` failed to roll back the Alias edit.
  2. Overrides were stored directly on the transient `[]JobItem` slice by index rather than stable `(Source, ID)`.
  3. `Update` dropped non-key messages (stopping `cursor.BlinkMsg`), `m.input.Width` wasn't synced to `availWidth`, and `settingsCursor` underflowed to `-1` on empty job lists.
- **Patch:** Staged Alias edits in `settingsDraftAlias` until final Color validation succeeds; moved user overrides into `m.overrides map[JobKey]JobOverride` keyed by `(Source, ID)` via `effectiveJob()`; forwarded non-key messages to `m.input.Update(msg)` while editing; synced `m.input.Width` on resize/edit; guarded empty lists with `clampInt`.
- **Regression Tests:** `TestSettingsAtomicCancelAndKeyedOverrides` (`ui_test.go`).

### SEC-06: Month-End Date Drift, DST Normalization, Hour-Only Matchers & Week Scan Cost
- **Severity:** Medium
- **Affected Files:** `utils.go`, `types.go`, `ui_week.go`, `ui_day.go`
- **Root Cause:**
  1. `AddDate(0, ±1, 0)` skipped February from Jan 31 (`Jan 31 + 1 month -> Mar 3`).
  2. `time.Date(..., time.Local)` silently shifted nonexistent spring-forward DST hours (e.g. `02:00` -> `01:00`) and queried `01:00` jobs twice.
  3. `OccursOnDay` ignored hour-only jobs (`MatchesHour != nil`, `MatchesDay == nil`), and Week rendering ran 168 repeated `O(N)` job scans.
- **Patch:** Added `addMonths` (clamped to month end), `wallHourTime` (skips nonexistent spring-forward slots), `OccurrencesInRange` + `TimeZone` + `Duration` + `JobProvider` interface in `types.go`, hour-only fallback in `OccursOnDay`, and single-pass `indexWeekJobs` in `ui_week.go`.
- **Regression Tests:** `TestMonthClampAndDSTWallHour`, `TestJobItemDefaultsAndHourOnlyOccurrence` (`ui_test.go`).

### SEC-07: Unhardened Subprocess & CWD File Coupling (Removed for Clean Baseline)
- **Severity:** Informational / Architectural Risk
- **Affected Files:** Removed `cron.go`, `config.go`, `crontab.txt`, `crolendar.json`
- **Root Cause:** Pre-refactor code executed `crontab -l` and read/wrote `crontab.txt` and `crolendar.json` in the CWD without atomic replacement or path confinement.
- **Patch:** Removed `cron.go`, `config.go`, and mock CWD files to establish a pure, zero-side-effect UI baseline (`--demo` uses in-memory fixtures only). Future providers and persistence in Phases 2–3 must follow Section 1.

### SEC-08: DST Week Column Shift, Ambiguous Fall-Back Hours & Day-Only Week/Day Drop (Round 2)
- **Severity:** High
- **Affected Files:** `types.go`, `utils.go`, `ui_agenda.go`, `ui_month.go`
- **Root Cause:**
  1. `indexWeekJobs` derived `dIdx` via `int(localOcc.Sub(start) / (24 * time.Hour))`, which shifted Monday 00:00 back into Sunday on 23-hour spring-forward days and shifted Sunday 23:30 forward into Monday on 25-hour fall-back days whenever `dIdx` stayed in `0..6`.
  2. `OccursInHour` queried `[start, start+1h)` from one ambiguous `time.Date`, missing occurrences in the second fold of a fall-back hour (e.g., `01:30 MST`).
  3. `MatchesDay`-only jobs vanished from Week/Day (`indexWeekJobs` / `OccursInHour`), and Agenda/Month called `JobsForDay` up to 43 times per render.
- **Patch:** Derived week/day column indices strictly by calendar date (`sameDay` against `start.AddDate(0, 0, i)`), added `wallHourSpan` to span all wall-clock instants of an hour across both fall-back folds, surfaced day-only `MatchesDay` jobs in `00:00` Week/Day slots, and added single-pass `indexDayJobs` for Month and Agenda.
- **Regression Tests:** `TestDSTWeekIndexingAndFallBackFoldConsistency`, `TestJobItemDefaultsAndHourOnlyOccurrence` (`ui_test.go`).

### SEC-09: Short-Height Month Clipping, Agenda/Current-Hour Overwrite & Scroll Marker Accounting (Round 2)
- **Severity:** High
- **Affected Files:** `ui_month.go`, `ui_agenda.go`, `ui_week.go`, `ui_day.go`, `ui_settings.go`, `main.go`
- **Root Cause:**
  1. `renderMonthView` switched to compact layout only on width (`colWidth < 6`), clipping bottom week rows (days 18–31) on short terminals like `63x14`, `65x14`, and `80x18`.
  2. Agenda silently dropped overflow jobs when `maxEventLines == 1`, and Week/Day overwrote the 2nd job or `+N more` line with the current-time bar when `len(jobs) >= 2 && mins >= 15`.
  3. `renderSettingsView` added `more jobs above/below` markers outside `availLines` budgeting, and pressing `t` in Week/Day did not scroll to the current hour.
- **Patch:** Triggered `renderCompactMonthView` whenever `bodyHeight-titleLines-1 < numWeeks*3`, added inline `(+N more)` indicators in tight Agenda/Month slots, switched current-hour indicator with 2+ jobs to top-border highlight, budgeted scroll marker lines in `ui_settings.go`, and scrolled on `t` in Week/Day.
- **Regression Tests:** `TestRenderAllViewsAcrossSizesBoundedAndMonthComplete`, `TestWeekAndDayCurrentHourPreservesMultipleJobs` (`ui_test.go`).

### SEC-10: Settings Identity Collisions, Empty IDs, Blank-Alias Clearing, Resize Offset Drift & BiDi Paste (Round 2)
- **Severity:** High / Medium
- **Affected Files:** `types.go`, `main.go`
- **Root Cause:**
  1. `JobItem.Key()` sanitized identity strings and permitted empty `ID` values, causing distinct or un-ID'd jobs to collide; `handleSettingsInput` re-indexed `m.jobs[idx]` on save instead of capturing `settingsEditKey` at edit start.
  2. `effectiveJob` ignored empty `ov.Alias`, preventing users from clearing provider-supplied aliases; unchanged titles were also pinned as overrides.
  3. Shrinking terminal width mid-edit updated `m.input.Width` without recalculating `bubbles/textinput` scroll offsets, causing `Alias:` lines to wrap/clip; initial `truncateWidth` also baked `…` into long edit drafts, and pasted `Cf` bidi runes entered live input.
- **Patch:** Removed lossy sanitization from `JobKey`, added `HasStableKey()` enforcement before edit, captured `settingsEditKey`, added `HasAlias` on `JobOverride` so blank input clears provider aliases, reset/repositioned `m.input` on width change, removed synthetic ellipsis from edit prefill, and filtered `isBlockedControlRune` on `tea.KeyRunes`.
- **Regression Tests:** `TestSettingsKeyCaptureBlankAliasNoIDAndResizeDuringEdit` (`ui_test.go`).

### SEC-11: C1 Escape Sequences & Emoji ZWJ Grapheme Preservation (Round 2)
- **Severity:** Medium
- **Affected Files:** `utils.go`
- **Root Cause:** Stripping every `unicode.Cf` rune removed `U+200D` (ZWJ), breaking valid joined emoji/ligatures into separate glyphs before truncation, and 8-bit C1 CSI/OSC sequences left printable payload text behind when `\x9B`/`\x9D` was stripped.
- **Patch:** Updated `ansiEscapeRe` to strip 8-bit C1 CSI/OSC sequences and updated `isBlockedControlRune` to strip unsafe `Cf` runes (BiDi overrides/isolates, ZWSP, BOM) while preserving `U+200D` (ZWJ).
- **Regression Tests:** `TestSanitizeTextStripsEscapesControlsAndUnicodeSpoofing`, `TestTruncateWidthMatchesLipglossAcrossGraphemes` (`ui_test.go`).

### SEC-12: Job TimeZone/Duration Enforcement, Occurrence Caps, Day Indexing & ZWNJ Preservation (Round 3)
- **Severity:** Medium
- **Affected Files:** `types.go`, `ui_day.go`, `utils.go`, `main.go`
- **Root Cause:**
  1. `JobItem.TimeZone` (`Location()`) and `JobItem.Duration` were defined on the model but ignored by day/hour evaluation (`OccursOnDay`, `OccursInHour`, `indexWeekJobs`), causing non-local legacy matchers and multi-hour/cross-midnight jobs to misalign.
  2. `OccurrencesInRange` outputs had no upper bound, and `ui_day.go` still performed 24 linear `JobsForHour` scans per render.
  3. `isBlockedControlRune` stripped ZWNJ (`U+200C`), breaking Persian/Arabic/Indic ligatures, and paging `Jan 31 -> Feb 28 -> Mar` lost the 31st day anchor.
- **Patch:** Unified hour-grid indexing (`indexHourGrid`, `indexDayHourJobs`, `indexWeekJobs`), applied `effectiveLocation` and `clampedDuration` overlap intervals, bounded runaway range results (`maxOccurrencesPerQuery`), switched `ui_day.go` to `indexDayHourJobs`, preserved `U+200C` alongside `U+200D`, and added `cursorAnchorDay` via `addMonthsWithAnchor`.
- **Regression Tests:** `TestTimeZoneDurationCapZWNJAndMonthAnchor` (`ui_test.go`).

### SEC-13: Dense Range Truncation, 30m DST Spans, Combined Matchers, Clipboard Paste & Duplicate Keys (Round 3 Audit Fixes)
- **Severity:** High / Medium
- **Affected Files:** `types.go`, `utils.go`, `main.go`
- **Root Cause:**
  1. Capping `boundedOccurrences` at `2048` silently truncated dense 15-minute schedules over a 42-day grid; a 7-day duration cap truncated longer events.
  2. `wallHourSpan` stepped by 1 hour (breaking 30m DST transitions like `Australia/Lord_Howe`); `indexHourGrid` broke after the first fold of a multi-hour `MatchesHour` job, and ignored `MatchesDay` when combined with `MatchesHour`.
  3. Invalid non-blank `TimeZone` values ignored caller fallback in `effectiveLocation`; single-noon conversion missed wide-offset day overlaps (e.g., Denver vs Kiritimati).
  4. `bubbles/textinput` clipboard `pasteMsg` bypassed `tea.KeyRunes` filtering, `ansiEscapeRe` omitted 8-bit C1 DCS/SOS/PM/APC string sequences, and duplicate `(Source, ID)` jobs were editable in Settings.
- **Patch:** Raised `maxOccurrencesPerQuery` (`100000`) and `maxDurationLookback` (`366d`), switched `wallHourSpan` to 15-minute steps, evaluated both `MatchesDay` and all fall-back folds consistently, added multi-TZ interval overlap via `matchesViewerDay`, preserved fallback timezone on load error, added `sanitizeLiveInput()`, extended `ansiEscapeRe` to C1 DCS/SOS/PM/APC, and blocked editing duplicate `(Source, ID)` keys.
- **Regression Tests:** `TestRound3EdgeCasesDenseC1DSTCombosClipboardAndDuplicates` (`ui_test.go`).

### SEC-14: Canonical Cross-TZ Day Tie-Break, Aligned Lookback Clamp, Span Indexing & Settings Polish (Round 4)
- **Severity:** Medium
- **Affected Files:** `types.go`, `main.go`, `ui_settings.go`
- **Root Cause:**
  1. `matchesViewerDay` matched any overlapping endpoint, double-placing cross-timezone day-only jobs (such as `America/New_York` Monday) onto both Sunday and Monday in Denver.
  2. Duration lookback clamps differed (`366d` for range vs `31d` for legacy `MatchesHour`), and `indexHourGrid` scanned all `numDays*24` spans per occurrence on duration > 0.
  3. Color-only Settings edits after clearing a provider alias re-pinned the title; `sanitizeLiveInput` adjusted cursor using total removed count instead of sanitized prefix length; wide CJK cursors at `44x14` could exceed line width.
- **Patch:** Anchored `matchesViewerDay` strictly to the viewer day containing job-local midnight `00:00`, aligned `maxDurationLookback` (`31d`) across range and legacy paths, split `indexHourGrid` into modular helpers using targeted `markSpanOverlap`, preserved unchanged alias override state via `settingsInitialAlias`, switched `sanitizeLiveInput` cursor tracking to sanitized prefix length, and cell-truncated Settings input rows.
- **Regression Tests:** `TestRound3EdgeCasesDenseC1DSTCombosClipboardAndDuplicates` (`ui_test.go`).

### SEC-15: Read-Only Cron Provider Subprocess, File, Memory & Key Hardening (Phase 2)
- **Severity:** Preventive Controls
- **Affected Files:** `providers.go`, `provider_cron.go`, `main.go`, `ui_settings.go`
- **Controls Applied:**
  1. Executed `crontab -l` strictly via `exec.CommandContext(ctx, "crontab", "-l")` with no shell invocation, a 5-second per-provider timeout, and bounded stdout/stderr buffers (`1 MiB` / `64 KiB`).
  2. Validated `--crontab <path>` against NUL/CR/LF characters and directories, enforcing a `1 MiB` read limit via `io.LimitReader`.
  3. Isolated panicking or failing providers in `LoadProviders`, enforced `Source` namespacing, dropped empty IDs, and deduplicated `(Source, ID)` collisions deterministically.
  4. Bounded cron occurrence generation at the provider boundary (`maxOccurrencesPerQuery` and `maxCronSearchDays = 3660`) and skipped nonexistent spring-forward DST wall minutes.
- **Regression Tests:** `TestParseCronExpressionsAndVixieSemantics`, `TestParseCrontabMacrosEnvTZAndSanitization`, `TestCronOccurrencesDSTSpringForwardFallBackAndBounds`, `TestCronProviderFileSecurityAndErrors`, `TestLoadProvidersDedupeIsolationAndBubbleTeaRefresh` (`provider_cron_test.go`).

### SEC-16: Cron Consensus Hardening — FIFOs, Parse Warnings, `CRON_TZ`, `%` Splitting & Provider Isolation (Phase 2 Audit)
- **Severity:** Medium
- **Affected Files:** `providers.go`, `provider_cron.go`, `main.go`, `ui_settings.go`, `styles.go`
- **Root Cause:**
  1. `readCrontabFile` only rejected directories, allowing FIFOs/devices to block `os.Open` indefinitely while ignoring `ctx`; `loadProvidersCmd` used `context.Background()` instead of a cancellable app context.
  2. Malformed crontab lines and invalid `CRON_TZ` directives vanished silently; plain `TZ=...` shifted schedules instead of `CRON_TZ` only; unescaped cron `%` stdin payloads stayed in displayed command titles; massive crontabs had no job count cap and Settings rendered all rows on each redraw.
  3. `LoadProviders` called `p.Name()` outside panic recovery, trusted non-empty `item.Source` (allowing namespace spoofing), loaded providers serially, and failed reloads overwrote previously loaded jobs in `main.go`.
- **Patch:** Enforced regular-file checks before and on the opened fd (`Mode().IsRegular()`) plus `ctx` cancellation; added `ParseCrontabWithWarnings` + `WarningProvider` surfaced in footer/Settings; restricted schedule timezone shifts to `CRON_TZ` only; split cron `%` stdin / unescaped `\%`; added `maxCrontabJobs` (`2000`) and `O(visible)` Settings row rendering; moved `p.Name()` into `loadSingleProviderSafe`, enforced `item.Source = name` unconditionally, loaded providers concurrently via `sync.WaitGroup`, and preserved last-known jobs on full reload error.
- **Regression Tests:** `TestParseCrontabMacrosEnvTZAndSanitization`, `TestCronProviderFileSecurityAndErrors`, `TestCronProviderRejectsFIFOWithoutBlocking`, `TestLoadProvidersDedupeIsolationAndBubbleTeaRefresh` (`provider_cron_test.go`, `provider_cron_unix_test.go`).

### SEC-17: Zero-Alloc Cron Bucketing, Hard Provider Deadlines, Non-Blocking Open, Step Overflow & Partial Reload Merge (Phase 2 Hardening)
- **Severity:** High / Medium
- **Affected Files:** `types.go`, `providers.go`, `provider_cron.go`, `provider_cron_unix.go`, `provider_cron_other.go`, `main.go`
- **Root Cause:**
  1. Rendering 2,000 `* * * * *` jobs on a 42-day Agenda materialized `60,480` timestamps per job (`OccurrencesInRange`), causing severe render-time memory/CPU DoS.
  2. `LoadProviders` waited on `wg.Wait()` even if a provider ignored `ctx`; `readCrontabFile` had an `os.Stat` -> `os.Open` FIFO-swap TOCTOU window; `readSystemCrontab` panicked on `nil` `ctx`.
  3. `parseCronField` allowed `int64` step overflow on huge values (e.g., `2/9223372036854775807`), malformed 1 MiB crontabs could allocate ~500k warning strings, and partial multi-source reload errors dropped last-known jobs from failed sources.
- **Patch:** Added zero-allocation `HasOccurrenceInRange` fast path backed by bitmask `cronSchedule.hasOccurrenceIn` (`math/bits`), added `select` timeout isolation per provider in `LoadProviders`, opened files via `O_RDONLY|O_CLOEXEC|O_NONBLOCK` + fd `f.Stat` on Unix (`provider_cron_unix.go`), normalized `nil` `ctx`, bounded cron step values and loop increments against overflow, capped stored warnings at `maxCrontabWarnings` (`20`) with a suppression summary, and added per-source `mergeReloadJobs`.
- **Regression Tests:** `TestHardeningTimeoutWarningCapNilCtxAndDense2000Render`, `TestCronProviderFIFOSwapTOCTOUNeverBlocks` (`provider_cron_test.go`, `provider_cron_unix_test.go`).

### SEC-18: ProviderLoader Anti-Accumulation, Non-Blocking Name Deadline, Global Warning Cap & Quit Cancel (2026-11-27)
- **Risk:** (1) Calling `safeProviderName(prov)` synchronously inside the `LoadProviders` timeout branch allowed a blocking `Name()` implementation to hang `wg.Wait()` past the 5-second deadline; (2) repeated manual reloads (`r`) while an uncooperative provider remained hung could spawn additional detached goroutines; (3) non-cron `WarningProvider` adapters had no loader-level warning cap or ANSI sanitization; (4) duplicate provider `Name()` values during partial reload could emit duplicate `JobKey` entries in `mergeReloadJobs`; (5) quitting via `q`/`ctrl+c` did not cancel `appCtx` until `p.Run()` returned.
- **Fix:**
  - Added stateful `ProviderLoader` (`providers.go`) with per-slot `busy atomic.Bool` and cached `lastName atomic.Value`: if a timed-out provider is still running, subsequent reloads immediately report `"provider timed out: previous load still running"` without spawning another goroutine while healthy sibling providers continue reloading normally.
  - Resolved `p.Name()` strictly inside the bounded worker goroutine and delivered it via a 1-buffered channel (`nameCh`), so the `<-pCtx.Done()` branch never calls `p.Name()` synchronously and falls back immediately to `"provider-<N>"`.
  - Enforced `sanitizeAndCapWarnings` (`maxProviderWarnings = 20` + suppressed summary) for every `WarningProvider` inside `ProviderLoader.Load`.
  - Deduplicated retained jobs by `JobKey` in `mergeReloadJobs` (`main.go`) and invoked `m.appCancel()` on `q`/`ctrl+c` before returning `tea.Quit`.
  - Documented the explicit concurrency, fast-unique-name, and `ctx`-cancellation contract on `JobProvider` (`types.go`).
- **Regression Tests:** `TestProviderLoaderHardeningBlockingNameNoAccumulationAndWarningCap` (`provider_cron_test.go`).

---

## 3. Verification Gate

Run after any modification to Crolendar before committing:

```bash
gofmt -w .
go vet ./...
go test -race -v ./...
```
