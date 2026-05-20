package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func withTempHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("RALLISH_HOME", filepath.Join(tmp, ".rallish"))
	return tmp
}

func TestDefaults_NonEmpty(t *testing.T) {
	d := Defaults()
	require.Equal(t, "solo-ralph", d.DefaultPreset)
	require.Equal(t, "claude", d.DefaultAdapter)
	require.Equal(t, "claude", d.CodingCLI)
	require.Equal(t, "yield", d.WaitMode)
}

func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	withTempHome(t)
	cfg, res, err := Load()
	require.NoError(t, err)
	require.False(t, res.FileExists)
	require.Equal(t, Defaults(), cfg)
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withTempHome(t)
	cfg := Defaults()
	require.NoError(t, Set(&cfg, "default_preset", "pair-review"))
	require.NoError(t, Set(&cfg, "wait_mode", "block"))
	require.NoError(t, Save(cfg))

	loaded, res, err := Load()
	require.NoError(t, err)
	require.True(t, res.FileExists)
	require.Equal(t, "pair-review", loaded.DefaultPreset)
	require.Equal(t, "block", loaded.WaitMode)
	require.True(t, res.IsFromFile("default_preset"))
	require.True(t, res.IsFromFile("wait_mode"))
	require.False(t, res.IsFromFile("editor"))
}

func TestSet_ValidatesEnums(t *testing.T) {
	cfg := Defaults()
	require.Error(t, Set(&cfg, "wait_mode", "nope"))
	require.Error(t, Set(&cfg, "telemetry", "maybe"))
	require.Error(t, Set(&cfg, "coding_cli", "gpt"))
	require.Error(t, Set(&cfg, "unknown_key", "x"))
	require.NoError(t, Set(&cfg, "wait_mode", "block"))
	require.Equal(t, "block", cfg.WaitMode)
}

func TestGet_UnknownKeyErrs(t *testing.T) {
	cfg := Defaults()
	_, err := Get(cfg, "nope")
	require.Error(t, err)
}

func TestResolve_EditorEnvFallback(t *testing.T) {
	withTempHome(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "nano")
	cfg, res, err := Load()
	require.NoError(t, err)
	entries := Resolve(cfg, res)
	var got *Entry
	for i := range entries {
		if entries[i].Key == "editor" {
			got = &entries[i]
		}
	}
	require.NotNil(t, got)
	require.Equal(t, "nano", got.Value)
	require.Equal(t, SourceEnv, got.Source)
}

func TestResolve_AllKeysListed(t *testing.T) {
	withTempHome(t)
	cfg, res, err := Load()
	require.NoError(t, err)
	entries := Resolve(cfg, res)
	require.Equal(t, len(Keys()), len(entries))
	for _, e := range entries {
		require.NotEmpty(t, e.Description, "key %q missing description", e.Key)
	}
}

func TestPath_HonorsRallishHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("RALLISH_HOME", tmp)
	p, err := Path()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tmp, "config.yaml"), p)
}

func TestLoad_ForwardCompat_IgnoresUnknownKeys(t *testing.T) {
	// A user upgraded from a future rallish that wrote a key we don't
	// know about must still load cleanly with the known fields intact.
	tmp := withTempHome(t)
	dir := filepath.Join(tmp, ".rallish")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	yamlBody := "default_preset: pair-review\n" +
		"future_field: some-value\n" +
		"another_future: 42\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yamlBody), 0o600))

	cfg, res, err := Load()
	require.NoError(t, err)
	require.True(t, res.FileExists)
	require.Equal(t, "pair-review", cfg.DefaultPreset)
}

func TestLoad_DowngradeCompat_MissingKeysUseDefaults(t *testing.T) {
	// A user wrote only one key; every other key must fall back to
	// Defaults() rather than empty strings.
	tmp := withTempHome(t)
	dir := filepath.Join(tmp, ".rallish")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("wait_mode: block\n"), 0o600))

	cfg, res, err := Load()
	require.NoError(t, err)
	require.True(t, res.IsFromFile("wait_mode"))
	require.False(t, res.IsFromFile("default_preset"))
	// Defaults fill in the unset fields.
	require.Equal(t, "solo-ralph", cfg.DefaultPreset)
	require.Equal(t, "claude", cfg.CodingCLI)
}

func TestLoad_MalformedYAML_ReturnsError(t *testing.T) {
	tmp := withTempHome(t)
	dir := filepath.Join(tmp, ".rallish")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte(":\n  : not valid yaml ::\n"), 0o600))

	_, _, err := Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse config")
}

func TestSave_FileMode_IsRestrictive(t *testing.T) {
	// config.yaml may hold sensitive future fields (api keys, tokens);
	// on-disk mode must be 0o600 — owner read/write only.
	withTempHome(t)
	require.NoError(t, Save(Defaults()))
	path, err := Path()
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	// On Windows the os.WriteFile mode bits are ignored; gate the
	// assertion to Unix to keep cross-platform CI green.
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config.yaml mode = %o, want 0o600", info.Mode().Perm())
	}
}

func TestSave_AtomicTempIsCleaned(t *testing.T) {
	withTempHome(t)
	cfg := Defaults()
	require.NoError(t, Save(cfg))
	dir, err := Dir()
	require.NoError(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".tmp")
	}
}
