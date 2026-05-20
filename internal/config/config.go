// Package config is the SSOT for user-facing rallish settings.
//
// Settings live at ~/.rallish/config.yaml in a small flat schema. The
// file is created on demand by [Save]; missing files are treated as
// "all defaults". Each field exposes its origin via [LoadResult] so
// `rallish config list` can show whether a value came from the file,
// an environment variable, or the built-in default.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Config is the typed view of ~/.rallish/config.yaml.
//
// Add new keys here, then teach [Keys], [Get], and [Set] about them.
// Booleans use *bool so the YAML round-trip can preserve "unset".
type Config struct {
	DefaultPreset  string `yaml:"default_preset,omitempty"`
	DefaultAdapter string `yaml:"default_adapter,omitempty"`
	CodingCLI      string `yaml:"coding_cli,omitempty"`
	Editor         string `yaml:"editor,omitempty"`
	WaitMode       string `yaml:"wait_mode,omitempty"`
	Telemetry      string `yaml:"telemetry,omitempty"`
}

// Source describes where a resolved value came from.
type Source string

const (
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
	SourceDefault Source = "default"
)

// Entry is a single resolved key.
type Entry struct {
	Key         string
	Value       string
	Source      Source
	Description string
}

// Defaults returns the hard-coded fallback config. Editing this is the
// only place to change a built-in default.
func Defaults() Config {
	return Config{
		DefaultPreset:  "solo-ralph",
		DefaultAdapter: "claude",
		CodingCLI:      "claude",
		Editor:         "",
		WaitMode:       "yield",
		Telemetry:      "off",
	}
}

// Keys is the enumerated set of valid config keys. Sorted; safe to
// expose to users.
func Keys() []string {
	return []string{
		"coding_cli",
		"default_adapter",
		"default_preset",
		"editor",
		"telemetry",
		"wait_mode",
	}
}

// KnownKey reports whether k is a valid config key.
func KnownKey(k string) bool {
	for _, x := range Keys() {
		if x == k {
			return true
		}
	}
	return false
}

// Describe returns a one-line description for k. Returns "" for
// unknown keys.
func Describe(k string) string {
	switch k {
	case "default_preset":
		return "Preset used when `rallish squash` is run without --preset"
	case "default_adapter":
		return "Adapter (runtime) presets default to (e.g. claude, kimi)"
	case "coding_cli":
		return "Coding-CLI vendor for skill install path (claude / kimi / codex)"
	case "editor":
		return "Editor opened by `rallish config edit` (else $EDITOR, $VISUAL, vi)"
	case "wait_mode":
		return "Default rally WAIT_MODE (yield = poll on user msg; block = blocking join)"
	case "telemetry":
		return "Reserved for future opt-in usage telemetry (on / off)"
	default:
		return ""
	}
}

// Path returns the absolute config file path, honoring RALLISH_HOME if
// set, else $HOME/.rallish/config.yaml.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Dir returns the rallish home directory (~/.rallish by default).
func Dir() (string, error) {
	if v := os.Getenv("RALLISH_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".rallish"), nil
}

// Load reads the config file. A missing file yields [Defaults] with
// LoadResult.FileExists=false. Invalid YAML returns an error.
func Load() (Config, LoadResult, error) {
	defs := Defaults()
	path, err := Path()
	if err != nil {
		return defs, LoadResult{}, err
	}
	res := LoadResult{Path: path}

	data, err := os.ReadFile(path) //nolint:gosec // user-owned file
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defs, res, nil
		}
		return defs, res, fmt.Errorf("read config: %w", err)
	}
	res.FileExists = true

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return defs, res, fmt.Errorf("parse config: %w", err)
	}
	// Layer file values over defaults.
	merged := defs
	if cfg.DefaultPreset != "" {
		merged.DefaultPreset = cfg.DefaultPreset
		res.fromFile("default_preset")
	}
	if cfg.DefaultAdapter != "" {
		merged.DefaultAdapter = cfg.DefaultAdapter
		res.fromFile("default_adapter")
	}
	if cfg.CodingCLI != "" {
		merged.CodingCLI = cfg.CodingCLI
		res.fromFile("coding_cli")
	}
	if cfg.Editor != "" {
		merged.Editor = cfg.Editor
		res.fromFile("editor")
	}
	if cfg.WaitMode != "" {
		merged.WaitMode = cfg.WaitMode
		res.fromFile("wait_mode")
	}
	if cfg.Telemetry != "" {
		merged.Telemetry = cfg.Telemetry
		res.fromFile("telemetry")
	}
	return merged, res, nil
}

// LoadResult is metadata about a [Load] call.
type LoadResult struct {
	Path       string
	FileExists bool
	fileKeys   map[string]bool
}

func (r *LoadResult) fromFile(k string) {
	if r.fileKeys == nil {
		r.fileKeys = map[string]bool{}
	}
	r.fileKeys[k] = true
}

// IsFromFile reports whether key k was set in the loaded file.
func (r LoadResult) IsFromFile(k string) bool {
	return r.fileKeys[k]
}

// Save writes cfg atomically to the config file path. Creates the
// parent directory if missing.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

// Get returns the current value for key. Empty string for keys that
// fall back to defaults but have no built-in default.
func Get(cfg Config, key string) (string, error) {
	switch key {
	case "default_preset":
		return cfg.DefaultPreset, nil
	case "default_adapter":
		return cfg.DefaultAdapter, nil
	case "coding_cli":
		return cfg.CodingCLI, nil
	case "editor":
		return cfg.Editor, nil
	case "wait_mode":
		return cfg.WaitMode, nil
	case "telemetry":
		return cfg.Telemetry, nil
	default:
		return "", fmt.Errorf("unknown key %q (run `rallish config list` for valid keys)", key)
	}
}

// Set writes value onto cfg in-place after validating value where the
// schema allows enumerated choices.
func Set(cfg *Config, key, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "default_preset":
		cfg.DefaultPreset = value
	case "default_adapter":
		cfg.DefaultAdapter = value
	case "coding_cli":
		if value != "" && !validCodingCLI(value) {
			return fmt.Errorf("coding_cli must be one of: claude, kimi, codex")
		}
		cfg.CodingCLI = value
	case "editor":
		cfg.Editor = value
	case "wait_mode":
		if value != "" && value != "yield" && value != "block" {
			return fmt.Errorf("wait_mode must be 'yield' or 'block'")
		}
		cfg.WaitMode = value
	case "telemetry":
		if value != "" && value != "on" && value != "off" {
			return fmt.Errorf("telemetry must be 'on' or 'off'")
		}
		cfg.Telemetry = value
	default:
		return fmt.Errorf("unknown key %q (run `rallish config list` for valid keys)", key)
	}
	return nil
}

// Resolve returns the full set of entries with values + sources for
// `rallish config list`. Env-var overrides win over file values for
// keys that opt into them (currently: editor → $EDITOR/$VISUAL).
func Resolve(cfg Config, res LoadResult) []Entry {
	entries := make([]Entry, 0, len(Keys()))
	for _, k := range Keys() {
		val, _ := Get(cfg, k)
		src := SourceDefault
		if res.IsFromFile(k) {
			src = SourceFile
		}
		if k == "editor" && val == "" {
			if v := os.Getenv("VISUAL"); v != "" {
				val = v
				src = SourceEnv
			} else if v := os.Getenv("EDITOR"); v != "" {
				val = v
				src = SourceEnv
			} else {
				val = "vi"
				src = SourceDefault
			}
		}
		entries = append(entries, Entry{
			Key:         k,
			Value:       val,
			Source:      src,
			Description: Describe(k),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries
}

func validCodingCLI(v string) bool {
	switch v {
	case "claude", "kimi", "codex":
		return true
	}
	return false
}
