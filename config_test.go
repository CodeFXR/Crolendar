package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestResolveConfigPathXDGAndValidation(t *testing.T) {
	dir := t.TempDir()

	// 1. Valid absolute XDG_CONFIG_HOME is honored.
	t.Setenv("XDG_CONFIG_HOME", dir)
	got, err := ResolveConfigPath("")
	if err != nil {
		t.Fatalf("ResolveConfigPath with valid XDG failed: %v", err)
	}
	want := filepath.Join(dir, "crolendar", "config.json")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	// 2. Relative XDG_CONFIG_HOME is ignored per XDG Base Directory spec.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join("relative", "xdg"))
	gotRel, err := ResolveConfigPath("")
	if err != nil {
		t.Fatalf("ResolveConfigPath with relative XDG fallback failed: %v", err)
	}
	if !filepath.IsAbs(gotRel) || strings.Contains(gotRel, filepath.Join("relative", "xdg")) {
		t.Fatalf("expected relative XDG_CONFIG_HOME to be ignored, got %q", gotRel)
	}

	// 3. Control-character XDG_CONFIG_HOME is ignored safely.
	t.Setenv("XDG_CONFIG_HOME", "/tmp/bad\npath")
	gotCtrl, err := ResolveConfigPath("")
	if err != nil {
		t.Fatalf("ResolveConfigPath with control-char XDG fallback failed: %v", err)
	}
	if strings.Contains(gotCtrl, "bad") {
		t.Fatalf("expected control-char XDG_CONFIG_HOME to be ignored, got %q", gotCtrl)
	}

	// 4. Explicit override path validation rejects control chars/NUL/root.
	for _, bad := range []string{"/tmp/bad\x00.json", "/tmp/bad\n.json", "/tmp/bad\x1b.json", string(filepath.Separator)} {
		if _, err := validateConfigPath(bad); err == nil {
			t.Fatalf("expected validateConfigPath(%q) to fail", bad)
		}
	}
}

func TestConfigSaveLoadRoundTripAndSanitization(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "crolendar", "config.json")

	// Missing file returns empty map without error.
	empty, warns, err := LoadConfig(cfgPath)
	if err != nil || len(empty) != 0 || len(warns) != 0 {
		t.Fatalf("expected clean empty load on missing file, got map=%v warns=%v err=%v", empty, warns, err)
	}

	// Round-trip normal alias, explicit blank-alias clear, and color-only override.
	in := map[JobKey]JobOverride{
		{Source: "cron", ID: "cron-bbb"}:        {Alias: "Nightly DB", HasAlias: true, Color: "#7D56F4"},
		{Source: "cron", ID: "cron-aaa"}:        {Alias: "", HasAlias: true, Color: "#0052D4"},
		{Source: "systemd", ID: "backup.timer"}: {Alias: "", HasAlias: false, Color: "#ff8800"},
	}
	if err := SaveConfig(cfgPath, in); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	bytes1, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	// Repeat save to confirm deterministic sorted JSON output.
	if err := SaveConfig(cfgPath, in); err != nil {
		t.Fatalf("second SaveConfig failed: %v", err)
	}
	bytes2, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("second ReadFile failed: %v", err)
	}
	if string(bytes1) != string(bytes2) {
		t.Fatalf("expected deterministic JSON output across saves")
	}

	out, warns, err := LoadConfig(cfgPath)
	if err != nil || len(warns) != 0 {
		t.Fatalf("LoadConfig round-trip failed: warns=%v err=%v", warns, err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 overrides, got %d (%+v)", len(out), out)
	}
	if got := out[JobKey{Source: "cron", ID: "cron-aaa"}]; !got.HasAlias || got.Alias != "" || got.Color != "#0052D4" {
		t.Fatalf("explicit alias-clear round-trip mismatch: %+v", got)
	}
	if got := out[JobKey{Source: "systemd", ID: "backup.timer"}]; got.HasAlias || got.Color != "#FF8800" {
		t.Fatalf("color-only override round-trip mismatch: %+v", got)
	}

	// Hand-edited JSON: omitted has_alias with non-empty alias, ANSI/bidi/overlong alias, bad hex, missing id.
	longAlias := strings.Repeat("a", 80)
	handEdited := `{
  "version": 1,
  "overrides": [
    {"source": "cron", "id": "j1", "alias": "\u001b[31m\u202e` + longAlias + `\u001b[0m", "color": "not-a-color"},
    {"source": "cron", "id": "", "alias": "Bad"},
    {"source": "cron", "id": "j2", "alias": "Hand Edited", "color": "#abc"}
  ]
}`
	handPath := filepath.Join(dir, "hand.json")
	if err := os.WriteFile(handPath, []byte(handEdited), 0o600); err != nil {
		t.Fatalf("write hand.json failed: %v", err)
	}
	parsed, pWarns, err := LoadConfig(handPath)
	if err != nil {
		t.Fatalf("LoadConfig hand.json failed: %v", err)
	}
	if len(pWarns) != 2 {
		t.Fatalf("expected 2 warnings (bad hex + empty id), got %d: %v", len(pWarns), pWarns)
	}
	j1 := parsed[JobKey{Source: "cron", ID: "j1"}]
	if !j1.HasAlias || len([]rune(j1.Alias)) != maxAliasRunes || j1.Color != "" {
		t.Fatalf("expected sanitized 48-rune alias and dropped invalid hex, got %+v", j1)
	}
	j2 := parsed[JobKey{Source: "cron", ID: "j2"}]
	if !j2.HasAlias || j2.Alias != "Hand Edited" || j2.Color != "#AABBCC" {
		t.Fatalf("expected hand-edited alias with normalized #AABBCC, got %+v", j2)
	}

	// Reject malformed JSON, root null, root array, missing/zero/unsupported version, trailing garbage, and >1 MiB file.
	for name, content := range map[string]string{
		"corrupt.json":     `{"version": 1, "overrides": [`,
		"null.json":        `null`,
		"array.json":       `[]`,
		"missing_ver.json": `{"overrides": []}`,
		"zero_ver.json":    `{"version": 0, "overrides": []}`,
		"future_ver.json":  `{"version": 99, "overrides": []}`,
		"neg_ver.json":     `{"version": -1, "overrides": []}`,
		"trailing.json":    `{"version": 1, "overrides": []} trailing`,
		"oversized.json":   strings.Repeat(" ", maxConfigBytes+10),
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s failed: %v", name, err)
		}
		if _, _, err := LoadConfig(p); err == nil {
			t.Fatalf("expected LoadConfig(%s) to fail", name)
		}
	}

	// Accept UTF-8 BOM and arbitrary key order (overrides before version).
	bomContent := "\xef\xbb\xbf" + `{"overrides":[{"source":"cron","id":"key  with spaces","alias":"BOM Job","color":"#123456"}],"version":1}`
	bomPath := filepath.Join(dir, "bom.json")
	if err := os.WriteFile(bomPath, []byte(bomContent), 0o600); err != nil {
		t.Fatalf("write bom.json failed: %v", err)
	}
	bomParsed, bomWarns, err := LoadConfig(bomPath)
	if err != nil || len(bomWarns) != 0 {
		t.Fatalf("expected BOM + reversed-key JSON to load cleanly, got warns=%v err=%v", bomWarns, err)
	}
	if got := bomParsed[JobKey{Source: "cron", ID: "key  with spaces"}]; got.Alias != "BOM Job" || got.Color != "#123456" {
		t.Fatalf("expected raw key with double spaces preserved byte-for-byte, got %+v", bomParsed)
	}

	// SaveConfig refuses > maxConfigOverrides without silent truncation.
	tooManyExact := make(map[JobKey]JobOverride, maxConfigOverrides+1)
	for i := 0; i <= maxConfigOverrides; i++ {
		tooManyExact[JobKey{Source: "cron", ID: filepath.Join("id", strings.Repeat("a", i%3+1), string(rune(0x100+i)))}] = JobOverride{
			Alias: "A", HasAlias: true, Color: "#111111",
		}
	}
	if err := SaveConfig(filepath.Join(dir, "toomany.json"), tooManyExact); err == nil {
		t.Fatalf("expected SaveConfig to reject > %d overrides", maxConfigOverrides)
	}

	// Streaming cap handles large empty-object arrays in bounded memory and still validates version.
	var emptyObjs strings.Builder
	emptyObjs.WriteString(`{"overrides":[`)
	for i := 0; i < maxConfigOverrides+500; i++ {
		if i > 0 {
			emptyObjs.WriteByte(',')
		}
		emptyObjs.WriteString(`{}`)
	}
	emptyObjs.WriteString(`],"version":1}`)
	ampPath := filepath.Join(dir, "amp.json")
	if err := os.WriteFile(ampPath, []byte(emptyObjs.String()), 0o600); err != nil {
		t.Fatalf("write amp.json failed: %v", err)
	}
	_, ampWarns, err := LoadConfig(ampPath)
	if err != nil {
		t.Fatalf("LoadConfig streaming-capped empty-object array failed: %v", err)
	}
	if len(ampWarns) != maxConfigWarnings+1 {
		t.Fatalf("expected capped %d warnings on oversized empty-object array, got %d", maxConfigWarnings+1, len(ampWarns))
	}

	// Warning cap on 100 invalid entries.
	entries := make([]configOverrideEntry, 100)
	for i := range entries {
		entries[i] = configOverrideEntry{Source: "cron", ID: ""}
	}
	rawCap, _ := json.Marshal(configFileData{Version: 1, Overrides: entries})
	capPath := filepath.Join(dir, "many_warn.json")
	if err := os.WriteFile(capPath, rawCap, 0o600); err != nil {
		t.Fatalf("write many_warn.json failed: %v", err)
	}
	_, capWarns, err := LoadConfig(capPath)
	if err != nil {
		t.Fatalf("LoadConfig many_warn failed: %v", err)
	}
	if len(capWarns) != maxConfigWarnings+1 || capWarns[maxConfigWarnings] != formatSuppressedWarnings(100-maxConfigWarnings) {
		t.Fatalf("expected capped %d warnings with canonical summary, got %+v", maxConfigWarnings+1, capWarns)
	}
}

func TestBubbleTeaSettingsPersistenceCorruptGuardAndReload(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "crolendar", "config.json")
	store, err := NewConfigStore(cfgPath)
	if err != nil {
		t.Fatalf("NewConfigStore failed: %v", err)
	}

	prov := stubProvider{
		name: "cron",
		items: []JobItem{
			{ID: "cron-job1", Title: "/usr/local/bin/backup.sh", ScheduleLabel: "0 2 * * *", Color: "#0052D4"},
		},
	}

	// 1. Start model, load provider, edit Alias + Color in Settings, persist to disk.
	m := newModelWithProviders(nil, []JobProvider{prov})
	m.attachConfigStore(store)
	m.width, m.height, m.ready = 100, 30, true

	loadedMsg := loadProvidersCmd(context.Background(), m.providerLoader, m.loadGen)()
	next, _ := m.Update(loadedMsg)
	m = next.(model)

	m = m.switchView(viewSettings, false)
	m.focusGrid = true
	// Enter -> edit Alias
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m.input.SetValue("Nightly Backup")
	// Enter -> move to Color
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m.input.SetValue("#12ab34")
	// Enter -> save & dispatch async ConfigStore command
	next, saveCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if saveCmd == nil {
		t.Fatalf("expected saveConfigCmd to be returned when ConfigStore is attached")
	}
	savedMsg := saveCmd()
	next, _ = m.Update(savedMsg)
	m = next.(model)
	if m.settingsError != "" || m.settingsNotice != "Saved settings" || len(m.configWarnings) != 0 {
		t.Fatalf("expected clean Saved settings notice and cleared warnings, got err=%q notice=%q warns=%v", m.settingsError, m.settingsNotice, m.configWarnings)
	}

	// 1b. Quit-before-cmd-starts durability: save in Settings, DO NOT execute saveCmd
	// (simulating immediate quit before Bubble Tea starts the command goroutine), then
	// CloseAndFlush() must synchronously persist the enqueued edit to disk.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m.input.SetValue("Immediate Quit Save")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	m.input.SetValue("#654321")
	next, unrunCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if unrunCmd == nil {
		t.Fatalf("expected save cmd")
	}
	// Intentionally skip running unrunCmd; simulate process shutdown calling CloseAndFlush().
	if err := store.CloseAndFlush(); err != nil {
		t.Fatalf("CloseAndFlush failed: %v", err)
	}
	flushedData, _, err := store.Load()
	if err != nil {
		t.Fatalf("Load after CloseAndFlush failed: %v", err)
	}
	if got := flushedData[JobKey{Source: "cron", ID: "cron-job1"}]; got.Alias != "Immediate Quit Save" || got.Color != "#654321" {
		t.Fatalf("expected CloseAndFlush to persist unrun enqueued edit, got %+v", got)
	}

	// 2. Restart a brand-new model pointing at the same ConfigStore and reload provider:
	// persisted Alias and Color must apply automatically after provider load.
	store2, err := NewConfigStore(cfgPath)
	if err != nil {
		t.Fatalf("second NewConfigStore failed: %v", err)
	}
	m2 := newModelWithProviders(nil, []JobProvider{prov})
	m2.attachConfigStore(store2)
	loadedMsg2 := loadProvidersCmd(context.Background(), m2.providerLoader, m2.loadGen)()
	next2, _ := m2.Update(loadedMsg2)
	m2 = next2.(model)

	eff := m2.effectiveJob(m2.jobs[0])
	if eff.DisplayName() != "Immediate Quit Save" || eff.DisplayColor() != "#654321" {
		t.Fatalf("expected persisted Alias/Color after restart+reload, got name=%q color=%q", eff.DisplayName(), eff.DisplayColor())
	}

	// 3. Corrupt config on startup must NOT be overwritten when user saves in Settings.
	corruptPath := filepath.Join(dir, "corrupt.json")
	corruptContent := `{"version": 1, "overrides": [broken`
	if err := os.WriteFile(corruptPath, []byte(corruptContent), 0o600); err != nil {
		t.Fatalf("write corrupt.json failed: %v", err)
	}
	corruptStore, err := NewConfigStore(corruptPath)
	if err != nil {
		t.Fatalf("NewConfigStore corrupt failed: %v", err)
	}
	mc := newModelWithProviders(nil, []JobProvider{prov})
	mc.attachConfigStore(corruptStore)
	if !mc.configLoadFailed || mc.configErr == "" {
		t.Fatalf("expected configLoadFailed=true on corrupt JSON")
	}
	mc.width, mc.height, mc.ready = 100, 30, true
	loadedCorrupt := loadProvidersCmd(context.Background(), mc.providerLoader, mc.loadGen)()
	nextC, _ := mc.Update(loadedCorrupt)
	mc = nextC.(model)
	mc = mc.switchView(viewSettings, false)
	mc.focusGrid = true
	// Footer and Settings view both surface the config error without panic.
	if view := renderSettingsView(mc, 79, 28); !strings.Contains(view, "config error:") {
		t.Fatalf("expected Settings banner to show config error, got:\n%s", view)
	}
	if footer := mc.renderFooter(); !strings.Contains(footer, "config error:") {
		t.Fatalf("expected footer to show config error, got:\n%s", footer)
	}
	// Editing updates in memory ONLY and preserves corrupt.json intact.
	nextC, _ = mc.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mc = nextC.(model)
	mc.input.SetValue("Temp Edit")
	nextC, _ = mc.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mc = nextC.(model)
	mc.input.SetValue("#abcdef")
	nextC, cmdC := mc.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mc = nextC.(model)
	if cmdC != nil {
		t.Fatalf("expected nil saveCmd when configLoadFailed is true")
	}
	afterCorrupt, _ := os.ReadFile(corruptPath)
	if string(afterCorrupt) != corruptContent {
		t.Fatalf("corrupt config file was overwritten: got %q", string(afterCorrupt))
	}

	// 4. SaveSerialized drops stale generations and stays race-free under concurrent calls.
	racePath := filepath.Join(dir, "race.json")
	raceStore, err := NewConfigStore(racePath)
	if err != nil {
		t.Fatalf("NewConfigStore race failed: %v", err)
	}
	if err := raceStore.SaveSerialized(5, map[JobKey]JobOverride{
		{Source: "cron", ID: "k"}: {Alias: "Gen5", HasAlias: true, Color: "#555555"},
	}); err != nil {
		t.Fatalf("SaveSerialized gen 5 failed: %v", err)
	}
	if err := raceStore.SaveSerialized(3, map[JobKey]JobOverride{
		{Source: "cron", ID: "k"}: {Alias: "StaleGen3", HasAlias: true, Color: "#333333"},
	}); err != nil {
		t.Fatalf("SaveSerialized stale gen 3 failed: %v", err)
	}
	loadedRace, _, err := raceStore.Load()
	if err != nil || loadedRace[JobKey{Source: "cron", ID: "k"}].Alias != "Gen5" {
		t.Fatalf("expected stale gen 3 to be ignored and Gen5 preserved, got %+v err=%v", loadedRace, err)
	}

	var wg sync.WaitGroup
	for g := uint64(6); g <= 25; g++ {
		wg.Add(1)
		gen := g
		go func() {
			defer wg.Done()
			_ = raceStore.SaveSerialized(gen, map[JobKey]JobOverride{
				{Source: "cron", ID: "k"}: {Alias: "Concurrent", HasAlias: true, Color: "#112233"},
			})
		}()
	}
	wg.Wait()
	if err := raceStore.CloseAndFlush(); err != nil {
		t.Fatalf("CloseAndFlush after concurrent saves failed: %v", err)
	}
	if _, _, err := raceStore.Load(); err != nil {
		t.Fatalf("concurrent saves left unreadable config: %v", err)
	}
}
