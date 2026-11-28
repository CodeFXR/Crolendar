package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const (
	currentConfigVersion = 1
	maxConfigBytes       = 1 << 20 // 1 MiB
	maxConfigOverrides   = 5000
	maxConfigWarnings    = 20
	maxAliasRunes        = 48
)

type configFileData struct {
	Version   int                   `json:"version"`
	Overrides []configOverrideEntry `json:"overrides"`
}

type configOverrideEntry struct {
	Source   string `json:"source"`
	ID       string `json:"id"`
	Alias    string `json:"alias,omitempty"`
	HasAlias bool   `json:"has_alias,omitempty"`
	Color    string `json:"color,omitempty"`
}

// ConfigStore serializes atomic reads and writes to Crolendar's XDG config file.
type ConfigStore struct {
	path         string
	mu           sync.Mutex
	wg           sync.WaitGroup
	lastSavedGen uint64
}

// NewConfigStore returns a ConfigStore bound to a validated config file path.
func NewConfigStore(path string) (*ConfigStore, error) {
	clean, err := validateConfigPath(path)
	if err != nil {
		return nil, err
	}
	return &ConfigStore{path: clean}, nil
}

// Path returns the resolved absolute config file path.
func (s *ConfigStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads and validates overrides from disk. A missing file returns an empty map and nil error.
func (s *ConfigStore) Load() (map[JobKey]JobOverride, []string, error) {
	if s == nil {
		return make(map[JobKey]JobOverride), nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return LoadConfig(s.path)
}

// Save atomically writes overrides to disk.
func (s *ConfigStore) Save(overrides map[JobKey]JobOverride) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return SaveConfig(s.path, overrides)
}

// SaveSerialized writes overrides only if gen is newer than any previously saved
// generation, preventing out-of-order async saves from overwriting newer edits.
func (s *ConfigStore) SaveSerialized(gen uint64, overrides map[JobKey]JobOverride) error {
	if s == nil {
		return nil
	}
	s.wg.Add(1)
	defer s.wg.Done()

	s.mu.Lock()
	defer s.mu.Unlock()
	if gen < s.lastSavedGen {
		return nil
	}
	if err := SaveConfig(s.path, overrides); err != nil {
		return err
	}
	s.lastSavedGen = gen
	return nil
}

// Wait blocks until any in-flight SaveSerialized call finishes.
func (s *ConfigStore) Wait() {
	if s == nil {
		return
	}
	s.wg.Wait()
}

// ResolveConfigPath resolves the config file path using an explicit override or
// XDG Base Directory rules ($XDG_CONFIG_HOME/crolendar/config.json with fallback
// to os.UserConfigDir()/crolendar/config.json).
func ResolveConfigPath(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return validateConfigPath(override)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if !hasUnsafePathRune(xdg) && filepath.IsAbs(xdg) {
			return validateConfigPath(filepath.Join(filepath.Clean(xdg), "crolendar", "config.json"))
		}
		// Per the XDG Base Directory Specification, ignore relative or invalid
		// XDG_CONFIG_HOME values and fall back to the default user config directory.
		baseDir, err := defaultUserConfigDirIgnoringXDG()
		if err != nil {
			return "", fmt.Errorf("cannot resolve default config directory: %w", err)
		}
		return validateConfigPath(filepath.Join(baseDir, "crolendar", "config.json"))
	}
	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user config directory: %w", err)
	}
	return validateConfigPath(filepath.Join(baseDir, "crolendar", "config.json"))
}

func defaultUserConfigDirIgnoringXDG() (string, error) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("cannot resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}

func hasUnsafePathRune(p string) bool {
	for _, r := range p {
		if r == 0 || r == '\r' || r == '\n' || isBlockedControlRune(r) {
			return true
		}
	}
	return false
}

func validateConfigPath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("config path is empty")
	}
	if hasUnsafePathRune(raw) {
		return "", errors.New("config path contains invalid control characters")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("invalid config path: %w", err)
	}
	clean := filepath.Clean(abs)
	base := filepath.Base(clean)
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return "", errors.New("config path must refer to a file")
	}
	return clean, nil
}

// LoadConfig reads, validates, and sanitizes overrides from a config JSON file.
// Missing files return an empty map and nil error. Symlinks, special files,
// world-writable files, oversized files (>1 MiB), and unsupported versions fail safely.
func LoadConfig(path string) (map[JobKey]JobOverride, []string, error) {
	cleanPath, err := validateConfigPath(path)
	if err != nil {
		return nil, nil, err
	}
	f, err := openConfigFileReadOnly(cleanPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[JobKey]JobOverride), nil, nil
		}
		return nil, nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read config file: %w", err)
	}
	if len(data) > maxConfigBytes {
		return nil, nil, fmt.Errorf("config file exceeds %d byte limit", maxConfigBytes)
	}
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		return make(map[JobKey]JobOverride), nil, nil
	}
	if trimmed[0] != '{' {
		return nil, nil, errors.New("invalid config JSON: root must be a JSON object")
	}

	var parsed configFileData
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, nil, fmt.Errorf("invalid config JSON: %w", err)
	}
	if parsed.Version < 0 || parsed.Version > currentConfigVersion {
		return nil, nil, fmt.Errorf("unsupported config version %d (supported: %d)", parsed.Version, currentConfigVersion)
	}

	overrides := make(map[JobKey]JobOverride)
	var warnings []string
	suppressed := 0
	addWarn := func(msg string) {
		if len(warnings) < maxConfigWarnings {
			warnings = append(warnings, msg)
		} else {
			suppressed++
		}
	}

	for idx, entry := range parsed.Overrides {
		if len(overrides) >= maxConfigOverrides {
			addWarn(fmt.Sprintf("config override cap (%d) reached; remaining entries ignored", maxConfigOverrides))
			break
		}
		src := sanitizeText(entry.Source)
		id := sanitizeText(entry.ID)
		if src == "" || id == "" {
			addWarn(fmt.Sprintf("config entry %d: missing source or id; skipped", idx+1))
			continue
		}

		alias := trimToRunes(sanitizeText(entry.Alias), maxAliasRunes)
		hasAlias := entry.HasAlias || alias != ""

		color := ""
		if strings.TrimSpace(entry.Color) != "" {
			if norm, ok := normalizeHexColor(entry.Color); ok {
				color = norm
			} else {
				addWarn(fmt.Sprintf("config entry %d (%s/%s): invalid hex color ignored", idx+1, src, id))
			}
		}

		if !hasAlias && color == "" {
			continue
		}

		overrides[JobKey{Source: src, ID: id}] = JobOverride{
			Alias:    alias,
			HasAlias: hasAlias,
			Color:    color,
		}
	}

	if suppressed > 0 {
		warnings = append(warnings, formatSuppressedWarnings(suppressed))
	}
	return overrides, warnings, nil
}

// SaveConfig atomically serializes and writes sanitized job overrides to path.
func SaveConfig(path string, overrides map[JobKey]JobOverride) error {
	cleanPath, err := validateConfigPath(path)
	if err != nil {
		return err
	}

	entries := make([]configOverrideEntry, 0, len(overrides))
	for key, ov := range overrides {
		src := sanitizeText(key.Source)
		id := sanitizeText(key.ID)
		if src == "" || id == "" {
			continue
		}
		alias := trimToRunes(sanitizeText(ov.Alias), maxAliasRunes)
		hasAlias := ov.HasAlias || alias != ""
		color := ""
		if strings.TrimSpace(ov.Color) != "" {
			if norm, ok := normalizeHexColor(ov.Color); ok {
				color = norm
			}
		}
		if !hasAlias && color == "" {
			continue
		}
		entries = append(entries, configOverrideEntry{
			Source:   src,
			ID:       id,
			Alias:    alias,
			HasAlias: hasAlias,
			Color:    color,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Source != entries[j].Source {
			return entries[i].Source < entries[j].Source
		}
		return entries[i].ID < entries[j].ID
	})
	if len(entries) > maxConfigOverrides {
		entries = entries[:maxConfigOverrides]
	}

	payload := configFileData{
		Version:   currentConfigVersion,
		Overrides: entries,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode config JSON: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > maxConfigBytes {
		return fmt.Errorf("encoded config exceeds %d byte limit", maxConfigBytes)
	}

	return writeConfigFileAtomic(cleanPath, raw)
}

func cloneOverrides(src map[JobKey]JobOverride) map[JobKey]JobOverride {
	if len(src) == 0 {
		return make(map[JobKey]JobOverride)
	}
	dst := make(map[JobKey]JobOverride, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func trimToRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

func randomTempSuffix() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
