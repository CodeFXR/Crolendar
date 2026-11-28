package main

import (
	"bytes"
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
	"unicode/utf8"
)

const (
	currentConfigVersion = 1
	maxConfigBytes       = 1 << 20 // 1 MiB
	maxConfigOverrides   = 5000
	maxConfigWarnings    = 20
	maxAliasRunes        = 48
	maxJobKeyBytes       = 256
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

// ConfigStore coordinates atomic, serialized reads and writes to Crolendar's
// XDG config file. Enqueue is non-blocking for Bubble Tea Update(), while
// Flush/CloseAndFlush guarantee durability even if the user quits immediately
// after saving or Bubble Tea exits with an error.
type ConfigStore struct {
	path string

	ioMu sync.Mutex // serializes disk I/O
	mu   sync.Mutex // guards pending/generation state below

	hasPending   bool
	pendingGen   uint64
	pendingData  map[JobKey]JobOverride
	lastSavedGen uint64
	lastErrGen   uint64
	lastErr      error
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
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	return LoadConfig(s.path)
}

// Save atomically writes overrides to disk synchronously.
func (s *ConfigStore) Save(overrides map[JobKey]JobOverride) error {
	if s == nil {
		return nil
	}
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	return SaveConfig(s.path, overrides)
}

// Enqueue records a newer override snapshot synchronously before an async
// Bubble Tea command is dispatched. It never blocks on disk I/O.
func (s *ConfigStore) Enqueue(gen uint64, overrides map[JobKey]JobOverride) {
	if s == nil {
		return
	}
	snap := cloneOverrides(overrides)
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen <= s.lastSavedGen || (s.hasPending && gen < s.pendingGen) {
		return
	}
	s.pendingGen = gen
	s.pendingData = snap
	s.hasPending = true
}

// Flush persists all enqueued overrides in order and returns the outcome for gen
// (or the latest flushed error when gen == 0).
func (s *ConfigStore) Flush(gen uint64) error {
	if s == nil {
		return nil
	}
	s.ioMu.Lock()
	defer s.ioMu.Unlock()

	var latestErr error
	for {
		s.mu.Lock()
		if !s.hasPending {
			var err error
			if gen != 0 && s.lastErrGen == gen {
				err = s.lastErr
			} else if gen == 0 {
				err = latestErr
			}
			s.mu.Unlock()
			return err
		}
		curGen := s.pendingGen
		curData := s.pendingData
		s.hasPending = false
		s.pendingData = nil
		s.mu.Unlock()

		err := SaveConfig(s.path, curData)

		s.mu.Lock()
		if err != nil {
			s.lastErrGen = curGen
			s.lastErr = err
			latestErr = err
		} else {
			if curGen > s.lastSavedGen {
				s.lastSavedGen = curGen
			}
			if s.lastErrGen <= curGen {
				s.lastErr = nil
				s.lastErrGen = 0
			}
			latestErr = nil
		}
		s.mu.Unlock()
	}
}

// SaveSerialized enqueues and flushes overrides for gen. Stale generations are ignored.
func (s *ConfigStore) SaveSerialized(gen uint64, overrides map[JobKey]JobOverride) error {
	if s == nil {
		return nil
	}
	s.Enqueue(gen, overrides)
	return s.Flush(gen)
}

// CloseAndFlush blocks until any in-flight save finishes and synchronously writes
// any still-pending enqueued edit before process exit.
func (s *ConfigStore) CloseAndFlush() error {
	return s.Flush(0)
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
	if !utf8.ValidString(p) {
		return true
	}
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

// validateJobKeyComponent validates persisted Source/ID strings byte-for-byte
// without mutating interior whitespace or punctuation that may distinguish raw
// provider keys.
func validateJobKeyComponent(s string) bool {
	if s == "" || len(s) > maxJobKeyBytes || strings.TrimSpace(s) == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == 0 || r == '\r' || r == '\n' || r == '\t' || isBlockedControlRune(r) {
			return false
		}
	}
	return true
}

// LoadConfig reads, validates, and sanitizes overrides from a config JSON file.
// Missing files return an empty map and nil error. Symlinks, special files,
// group/world-writable or foreign-owned files/dirs, oversized files (>1 MiB),
// and missing/unsupported versions fail safely.
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
	// Strip optional UTF-8 BOM before checking/decoding JSON.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if len(bytes.TrimSpace(data)) == 0 {
		return make(map[JobKey]JobOverride), nil, nil
	}

	return decodeConfigJSON(data)
}

func decodeConfigJSON(data []byte) (map[JobKey]JobOverride, []string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid config JSON: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, nil, errors.New("invalid config JSON: root must be a JSON object")
	}

	hasVersion := false
	versionVal := 0
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

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("invalid config JSON: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, nil, errors.New("invalid config JSON: expected string object key")
		}

		switch key {
		case "version":
			if err := dec.Decode(&versionVal); err != nil {
				return nil, nil, fmt.Errorf("invalid config version field: %w", err)
			}
			hasVersion = true

		case "overrides":
			arrTok, err := dec.Token()
			if err != nil {
				return nil, nil, fmt.Errorf("invalid config overrides field: %w", err)
			}
			if arrTok == nil {
				continue // explicit null overrides treated as empty
			}
			arrDelim, ok := arrTok.(json.Delim)
			if !ok || arrDelim != '[' {
				return nil, nil, errors.New("invalid config JSON: overrides must be an array")
			}

			processed := 0
			capWarned := false
			for dec.More() {
				if processed >= maxConfigOverrides {
					if !capWarned {
						addWarn(fmt.Sprintf("config override cap (%d) reached; remaining entries ignored", maxConfigOverrides))
						capWarned = true
					}
					if err := skipJSONValue(dec); err != nil {
						return nil, nil, fmt.Errorf("invalid config JSON: %w", err)
					}
					continue
				}
				var entry configOverrideEntry
				if err := dec.Decode(&entry); err != nil {
					return nil, nil, fmt.Errorf("invalid config override entry %d: %w", processed+1, err)
				}
				processed++

				if !validateJobKeyComponent(entry.Source) || !validateJobKeyComponent(entry.ID) {
					addWarn(fmt.Sprintf("config entry %d: invalid or missing source/id; skipped", processed))
					continue
				}

				alias := trimToRunes(sanitizeText(entry.Alias), maxAliasRunes)
				hasAlias := entry.HasAlias || alias != ""

				color := ""
				if strings.TrimSpace(entry.Color) != "" {
					if norm, ok := normalizeHexColor(entry.Color); ok {
						color = norm
					} else {
						addWarn(fmt.Sprintf("config entry %d (%s/%s): invalid hex color ignored", processed, sanitizeText(entry.Source), sanitizeText(entry.ID)))
					}
				}

				if !hasAlias && color == "" {
					continue
				}

				overrides[JobKey{Source: entry.Source, ID: entry.ID}] = JobOverride{
					Alias:    alias,
					HasAlias: hasAlias,
					Color:    color,
				}
			}

			// Consume closing ']'.
			if _, err := dec.Token(); err != nil {
				return nil, nil, fmt.Errorf("invalid config JSON: %w", err)
			}

		default:
			if err := skipJSONValue(dec); err != nil {
				return nil, nil, fmt.Errorf("invalid config JSON: %w", err)
			}
		}
	}

	// Consume closing '}'.
	if _, err := dec.Token(); err != nil {
		return nil, nil, fmt.Errorf("invalid config JSON: %w", err)
	}
	// Reject trailing tokens after root JSON object.
	if _, err := dec.Token(); err != io.EOF {
		return nil, nil, errors.New("invalid config JSON: unexpected trailing data")
	}

	if !hasVersion || versionVal != currentConfigVersion {
		return nil, nil, fmt.Errorf("missing or unsupported config version %d (supported: %d)", versionVal, currentConfigVersion)
	}

	if suppressed > 0 {
		warnings = append(warnings, formatSuppressedWarnings(suppressed))
	}
	return overrides, warnings, nil
}

// skipJSONValue consumes a single JSON value from dec in O(depth) memory without
// allocating objects/slices for ignored entries.
func skipJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' && delim != '[' {
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
	depth := 1
	for depth > 0 {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

// SaveConfig atomically serializes and writes validated job overrides to path.
func SaveConfig(path string, overrides map[JobKey]JobOverride) error {
	cleanPath, err := validateConfigPath(path)
	if err != nil {
		return err
	}
	if len(overrides) > maxConfigOverrides {
		return fmt.Errorf("override count %d exceeds maximum %d", len(overrides), maxConfigOverrides)
	}

	entries := make([]configOverrideEntry, 0, len(overrides))
	for key, ov := range overrides {
		if !validateJobKeyComponent(key.Source) || !validateJobKeyComponent(key.ID) {
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
			Source:   key.Source,
			ID:       key.ID,
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
