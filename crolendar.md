# Crolendar Architecture, Controls & Roadmap

Crolendar is a terminal calendar and scheduled-job visualizer built with Go, Bubble Tea, and Lipgloss. The codebase is currently isolated to a clean, provider-agnostic TUI baseline so multiple job sources (cron, systemd timers, Kubernetes CronJobs, and custom schedules) can be integrated cleanly on top of a hardened UI layer.

For security defects found and patched in this codebase, see `security.md`.

---

## 1. Working Tree

```text
crolendar/
├── main.go          # Bubble Tea MVU entrypoint, key routing, layout bounds, status footer, --demo flag
├── types.go         # Provider-agnostic JobItem model, view state enum, day/hour filters, demo fixtures
├── styles.go        # Centralized Lipgloss color palette and shared component styles
├── utils.go         # Terminal escape sanitization, UTF-8 width truncation, hex color validation
├── ui_sidebar.go    # Left navigation sidebar with logo, view list, and focus indicator
├── ui_agenda.go     # Split month-calendar + selected-day job detail panel
├── ui_month.go      # Dynamic full-month grid with multi-job truncation and overflow badges
├── ui_week.go       # 7-day Clean Strip hourly grid with sticky header and current-time line
├── ui_day.go        # Single-day hourly timeline with schedule/source badges and current-time line
├── ui_settings.go   # Windowed job display editor (alias and validated hex color)
├── ui_test.go       # Unit and render-invariant test suite
├── go.mod           # Go module definition (Bubble Tea, Bubbles, Lipgloss, x/ansi)
├── go.sum           # Cryptographic checksums of Go dependencies
├── crolendar.png    # Project icon asset
├── LICENSE          # MIT license
├── README.md        # Public repository landing README
├── crolendar.md     # Current architecture, tree, controls, and roadmap (this file)
└── security.md      # Patched bugs, security vulnerabilities, and regression checklist
```

---

## 2. Current Architecture

### 2.1 Model-View-Update (MVU) Loop (`main.go`)
- `newModel(jobs []JobItem)` initializes the Bubble Tea `model` with `viewAgenda`, `time.Now()` cursor, and an inline `textinput.Model` for Settings.
- `layoutDimensions()` reserves 2 lines for the bottom status/keybinding footer and `sidebarFullWidth` (`21` columns: 20 content + 1 border) for the left sidebar.
- Terminals smaller than `44x14` (`minTermWidth x minTermHeight`) render a centered resize notice instead of clipping or panicking.
- `main()` accepts `--demo` (`false` by default). Running `./crolendar` starts with an empty job slice; `./crolendar --demo` loads in-memory `demoJobs()` fixtures for UI preview without touching system crontabs or disk files.

### 2.2 Provider-Agnostic View Model (`types.go`)
All UI views consume `JobItem` and `JobProvider` rather than cron-specific structs:
- **Provider Contract:** `JobProvider` (`Name() string`, `Load(ctx context.Context) ([]JobItem, error)`) defines the adapter boundary for Phase 2 sources.
- **Identity & Overrides:** `JobKey{Source, ID}` indexes user `JobOverride{Alias, Color}` settings cleanly across refreshes/reorders via `effectiveJob()`.
- **Schedule & Timezone:** `TimeZone`, `Duration`, and `OccurrencesInRange(start, end time.Time)` (with fallback to `MatchesDay`/`MatchesHour`), plus single-pass `indexWeekJobs` / `indexDayJobs` and `wallHourSpan` for DST gap and fall-back fold safety.
- **Safe Accessors:** `DisplayName()`, `DisplaySchedule()`, `DisplaySource()`, `DisplayColor()`, and `LipglossColor()` enforce sanitization and valid `#RRGGBB` fallback (`#0052D4`).

### 2.3 Views (`ui_*.go`)
- **Sidebar (`ui_sidebar.go`):** Renders the gradient `/// CROLENDAR ///` header, 5 numbered views (`Agenda`, `Month`, `Week`, `Day`, `Settings`), and highlights its right border in `purple` (`#7D56F4`) when sidebar-focused.
- **Agenda (`ui_agenda.go`):** 6-week compact calendar above a horizontal divider and selected-day job list (up to 3 color-coded dots per day; `+N more` / inline `(+N more)` overflow).
- **Month (`ui_month.go`):** Responsive 5- or 6-week bordered grid (`colWidth >= 6` and sufficient vertical height) with automatic compact borderless fallback on narrow (`44–62` cols) or short terminals (`63x14`, `65x14`, etc.) so all days `1..31` remain visible, distinguishing `today` (`#7D56F4`) and `cursor` (`#0052D4`).
- **Week & Day (`ui_week.go`, `ui_day.go`):** Sticky 3-line headers paired with a `viewport.Model` rendering uniform 3-line hourly rows (`00:00`–`23:00`) plus a 1-line closing border (`73` lines total), initial scroll snapped to `(now.Hour() - 1) * 3`, and a `purple` current-time indicator (`●─────`).
- **Settings (`ui_settings.go`):** Windowed scrollable list keeping `settingsCursor` visible, atomic Alias + `#RGB`/`#RRGGBB` Color editing (`Esc` rolls back cleanly), keyed by `(Source, ID)`, inline validation errors, and an empty-state guide when no jobs are loaded.

---

## 3. Controls

| Scope | Key | Action |
|---|---|---|
| Global | `Tab` | Toggle focus between Sidebar and Grid |
| Global | `1` – `5` | Jump directly to Agenda, Month, Week, Day, or Settings |
| Global | `t` | Jump cursor to Today (calendar views) |
| Global | `q` / `Ctrl+c` | Quit (`q` types normally during Settings text input) |
| Sidebar | `Up`/`k`, `Down`/`j` | Cycle active view |
| Sidebar | `Enter`, `Right`/`l` | Focus main Grid panel |
| Agenda / Month | `Left`/`h`, `Right`/`l` | Move `-1` / `+1` day |
| Agenda / Month | `Up`/`k`, `Down`/`j` | Move `-7` / `+7` days |
| Agenda / Month | `PgUp`, `PgDown` | Move `-1` / `+1` month |
| Week / Day | `Left`/`h`, `Right`/`l` | Move `-1` / `+1` day (`[` / `]` or `H` / `L` moves `-7` / `+7` days) |
| Week / Day | `Up`/`k`, `Down`/`j`, `PgUp`, `PgDown` | Scroll hourly viewport |
| Settings | `Up`/`k`, `Down`/`j` | Select job |
| Settings | `Enter` | Edit Alias, then Color; `Esc` cancels |

---

## 4. Build, Check & Test Commands

```bash
gofmt -w .
go vet ./...
go test -race -v ./...
go build -o crolendar .
./crolendar --demo
```

---

## 5. Development Phases & Roadmap

### Phase 1: UI/UX Hardening & Decoupled Baseline (Complete)
- Strip premature cron-specific parsing and disk config I/O from the UI layer.
- Standardize `JobItem` view model, shared `sanitizeText` / `truncateWidth` / `normalizeHexColor` helpers, bounds guards, and responsive rendering across all 5 views.
- Establish `ui_test.go` regression coverage under `go test -race`.

### Phase 2: Multi-Source Job Providers (Next)
- Define a read-only `JobProvider` interface returning `[]JobItem` with explicit error/status reporting.
- Implement adapters for:
  1. **Local Cron:** Read-only `crontab -l` / explicit `--crontab <path>` file inspection.
  2. **systemd Timers:** Parse `systemctl list-timers --all --output=json` (user and system scopes).
  3. **Kubernetes CronJobs:** Read-only cluster/context CronJob schedule visualization (`kubectl` or `client-go`) with namespace filtering.
- Add source-filter toggles and per-provider status indicators in the UI.

### Phase 3: Persistence, Job Management & Inspection
- Add XDG-compliant config persistence (`$XDG_CONFIG_HOME/crolendar/config.json`) using atomic temp-file writes and strict symlink/permission checks for job aliases, colors, and enabled providers.
- Add safe CRUD workflows where supported by the source provider, execution log inspection, manual trigger with explicit confirmation, and same-hour overlap/conflict highlighting.

### Phase 4: Packaging & Release Readiness
- Set up CI gates (`gofmt`, `go vet`, `go test -race`, `govulncheck`).
- Update `README.md` with verified installation instructions, screenshots, and release artifacts.
