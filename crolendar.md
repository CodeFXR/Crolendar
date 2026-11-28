# Crolendar Architecture, Controls & Roadmap

Crolendar is a terminal calendar and scheduled-job visualizer built with Go, Bubble Tea, and Lipgloss. The codebase is currently isolated to a clean, provider-agnostic TUI baseline so multiple job sources (cron, systemd timers, Kubernetes CronJobs, and custom schedules) can be integrated cleanly on top of a hardened UI layer.

For security defects found and patched in this codebase, see `security.md`.

---

## 1. Working Tree

```text
crolendar/
├── main.go               # Bubble Tea MVU entrypoint, async provider reload & config save, flags (--demo, --crontab, --config, --version)
├── types.go              # Provider-agnostic JobItem model, JobProvider contract, view state, day/hour indexers
├── config.go             # XDG config path resolution, JSON schema v1 load/save, ConfigStore serialization
├── config_unix.go        # Unix O_NOFOLLOW/O_NONBLOCK config open, 0700/0600 atomic temp+fsync+rename write
├── config_other.go       # Non-Unix config open and atomic write helpers for cross-platform builds
├── config_test.go        # XDG resolution, round-trip, sanitization, corrupt-guard, and Bubble Tea save tests
├── config_unix_test.go   # Unix 0700/0600 permission, symlink, FIFO, and world-writable rejection tests
├── providers.go          # Provider aggregator (timeouts, panic recovery, Source namespacing, key deduplication)
├── provider_cron.go           # Read-only POSIX/Vixie cron parser (crontab -l or --crontab <path>, CRON_TZ, macros)
├── provider_cron_unix.go      # Unix O_NONBLOCK regular-file open helper preventing FIFO TOCTOU hangs
├── provider_cron_other.go     # Non-Unix regular-file open helper for cross-platform builds
├── provider_cron_test.go      # Cron parser, Vixie DOM/DOW, DST, warning-cap, 2000-job perf, and reload tests
├── provider_cron_unix_test.go # Unix FIFO and concurrent FIFO-swap TOCTOU regression tests
├── styles.go             # Centralized Lipgloss color palette and shared component styles
├── utils.go              # Terminal escape sanitization, UTF-8 width truncation, hex color validation, DST spans
├── ui_sidebar.go         # Left navigation sidebar with logo, view list, and focus indicator
├── ui_agenda.go          # Split month-calendar + selected-day job detail panel
├── ui_month.go           # Dynamic full-month grid with compact fallback and overflow badges
├── ui_week.go            # 7-day Clean Strip hourly grid with sticky header and current-time line
├── ui_day.go             # Single-day hourly timeline with schedule/source badges and current-time line
├── ui_settings.go        # Windowed job display editor (alias and validated hex color) and provider error state
├── ui_test.go            # Unit and render-invariant test suite
├── go.mod                # Go module definition (Bubble Tea, Bubbles, Lipgloss, x/ansi)
├── go.sum                # Cryptographic checksums of Go dependencies
├── crolendar.png         # Project icon asset
├── LICENSE               # MIT license
├── README.md             # Public repository landing README
├── crolendar.md          # Current architecture, tree, controls, and roadmap (this file)
└── security.md           # Patched bugs, security vulnerabilities, and regression checklist
```

---

## 2. Current Architecture

### 2.1 Model-View-Update (MVU) Loop (`main.go`)
- `newModelWithProviders(jobs []JobItem, providers []JobProvider)` initializes the Bubble Tea `model` with `viewAgenda`, `time.Now()` cursor, inline `textinput.Model`, and async `LoadProviders` commands tracked by a generation counter (`loadGen`) to discard stale refreshes.
- `layoutDimensions()` reserves 2 lines for the bottom status/keybinding footer and `sidebarFullWidth` (`21` columns: 20 content + 1 border) for the left sidebar.
- Terminals smaller than `44x14` (`minTermWidth x minTermHeight`) render a centered resize notice instead of clipping or panicking.
- `main()` supports `--crontab <path>` (read-only crontab file), `--config <path>` (override XDG config JSON path), `--demo` (in-memory sample fixtures; skips disk config unless `--config` is explicitly provided), and `--version` (`crolendar <version>`). By default `./crolendar` loads the current user's crontab via `CronProvider` (`crontab -l`) in read-only mode and persists job display overrides (Alias, Color) under `$XDG_CONFIG_HOME/crolendar/config.json` (fallback: `os.UserConfigDir()/crolendar/config.json`).

### 2.2 Provider-Agnostic View Model (`types.go`)
All UI views consume `JobItem` and `JobProvider` rather than cron-specific structs:
- **Provider Contract:** `JobProvider` (`Name() string`, `Load(ctx context.Context) ([]JobItem, error)`) defines the adapter boundary for Phase 2 sources (adapters respect context cancellation, assign `Source`, emit unique non-empty `ID`s, and return chronological occurrences in half-open `[start, end)` intervals capped safely at `100000`).
- **Identity & Overrides:** `JobKey{Source, ID}` indexes user `JobOverride{Alias, HasAlias, Color}` settings cleanly across refreshes/reorders via `effectiveJob()`, rejecting empty IDs and duplicate keys.
- **Schedule & Timezone:** `TimeZone`, `Duration` (overlap expansion up to `31d` via `maxDurationLookback`), and `OccurrencesInRange(start, end time.Time)` (with fallback to `MatchesDay`/`MatchesHour` evaluated in `job.Location()` and canonical start-of-day TZ tie-breaking), plus single-pass `indexWeekJobs` / `indexDayHourJobs` / `indexDayJobs` and 15-minute `wallHourSpan` for sub-hour DST gaps and fall-back folds.
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
| Global | `r` | Reload active job providers asynchronously |
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

### Phase 2: Multi-Source Job Providers (In Progress)
- Wired concurrent read-only `JobProvider` loader (`providers.go`) with per-provider 5s timeouts, `Name()`/`Load()` panic isolation, unconditional `Source` namespacing, `(Source, ID)` deduplication, cancellable app context, async `tea.Cmd` reload (`r`), last-known-jobs retention on reload failure, and footer/Settings error & parse-warning surfacing.
- Implemented adapters:
  1. **Local Cron (Complete):** Pure-Go POSIX/Vixie user-crontab parser (`provider_cron.go`) supporting `crontab -l` and `--crontab <path>` (user 5-field format only, not `/etc/crontab` 6-field system format; regular files only with `1 MiB` / `2000` job caps; `CRON_TZ` schedule timezone while `TZ` remains child-env only; Vixie DOM/DOW OR/AND semantics; `%` stdin splitting and `\%` unescaping; malformed line warnings).
     - **Documented cron semantics:** Nonexistent spring-forward DST wall minutes are skipped (cronie may catch up late after the jump); fall-back repeated minutes emit once on the first fold; `@reboot` entries appear in Settings only (no calendar date); IDs are content-addressed (`cron-<sha256>` + `#N`), so editing a command/schedule generates a new ID.
  2. **systemd Timers (Next):** Parse `systemctl list-timers --all --output=json` (user and system scopes).
  3. **Kubernetes CronJobs (Planned):** Read-only cluster/context CronJob schedule visualization (`kubectl` or `client-go`) with namespace filtering.
- Remaining Phase 2 UI: source-filter toggles and per-provider status summary.

### Phase 3: Persistence, Job Management & Inspection (In Progress)
- Implemented XDG-compliant config persistence (`config.go`, `config_unix.go`, `config_other.go`) at `$XDG_CONFIG_HOME/crolendar/config.json` (ignoring relative/invalid `XDG_CONFIG_HOME` per XDG spec) using versioned JSON v1, `1 MiB` / `5000` entry caps, ANSI/bidi/hex sanitization, `O_NOFOLLOW|O_NONBLOCK` regular-file reads, `0700` dir / `0600` atomic `O_CREAT|O_EXCL` temp-file + `fsync` + `rename` writes, serialized `ConfigStore` generations, and corrupt-file overwrite protection.
- Remaining Phase 3: safe CRUD workflows where supported by the source provider, execution log inspection, manual trigger with explicit confirmation, and same-hour overlap/conflict highlighting.

### Phase 4: Packaging & Release Readiness
- Set up CI gates (`gofmt`, `go vet`, `go test -race`, `govulncheck`).
- Update `README.md` with verified installation instructions, screenshots, and release artifacts.
