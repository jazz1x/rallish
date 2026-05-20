package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/config"
	"github.com/stretchr/testify/require"
)

func tempConfigHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("RALLISH_HOME", filepath.Join(tmp, ".rallish"))
	return tmp
}

func TestConfigList_DefaultsWhenNoFile(t *testing.T) {
	tempConfigHome(t)
	var out bytes.Buffer
	opts := &ConfigOptions{stdout: &out}
	require.NoError(t, RunConfigList(opts))
	s := out.String()
	require.Contains(t, s, "no config file yet")
	require.Contains(t, s, "default_preset")
	require.Contains(t, s, "solo-ralph")
}

func TestConfigSet_PersistsAndIsListed(t *testing.T) {
	tempConfigHome(t)
	var out bytes.Buffer
	opts := &ConfigOptions{stdout: &out}

	require.NoError(t, RunConfigSet(opts, "wait_mode", "block"))
	out.Reset()
	require.NoError(t, RunConfigList(opts))
	s := out.String()
	require.Contains(t, s, "wait_mode")
	require.Contains(t, s, "block")
	require.Contains(t, s, "file")
}

func TestConfigSet_RejectsInvalidEnum(t *testing.T) {
	tempConfigHome(t)
	opts := &ConfigOptions{stdout: &bytes.Buffer{}}
	err := RunConfigSet(opts, "wait_mode", "nope")
	require.Error(t, err)
}

func TestConfigGet_PrintsValue(t *testing.T) {
	tempConfigHome(t)
	opts := &ConfigOptions{stdout: &bytes.Buffer{}}
	require.NoError(t, RunConfigSet(opts, "default_preset", "pair-review"))
	var out bytes.Buffer
	opts2 := &ConfigOptions{stdout: &out}
	require.NoError(t, RunConfigGet(opts2, "default_preset"))
	require.Equal(t, "pair-review\n", out.String())
}

func TestConfigGet_UnknownKey(t *testing.T) {
	tempConfigHome(t)
	opts := &ConfigOptions{stdout: &bytes.Buffer{}}
	err := RunConfigGet(opts, "nope")
	require.Error(t, err)
}

func TestConfigEdit_CreatesFileAndCallsEditor(t *testing.T) {
	tempConfigHome(t)
	var out bytes.Buffer
	called := false
	var capturedEditor, capturedPath string
	opts := &ConfigOptions{
		stdout: &out,
		runEditor: func(editor, path string) error {
			called = true
			capturedEditor = editor
			capturedPath = path
			return nil
		},
	}
	t.Setenv("EDITOR", "myed")
	require.NoError(t, RunConfigEdit(opts))
	require.True(t, called, "runEditor should have been called")
	require.Equal(t, "myed", capturedEditor)

	expected, err := config.Path()
	require.NoError(t, err)
	require.Equal(t, expected, capturedPath)
	require.Contains(t, out.String(), "opening")
}

func TestChooseEditor_Precedence(t *testing.T) {
	t.Setenv("EDITOR", "ed-env")
	t.Setenv("VISUAL", "vis-env")
	require.Equal(t, "cfg-ed", chooseEditor("cfg-ed"))
	require.Equal(t, "vis-env", chooseEditor(""))
	t.Setenv("VISUAL", "")
	require.Equal(t, "ed-env", chooseEditor(""))
	t.Setenv("EDITOR", "")
	require.Equal(t, "vi", chooseEditor(""))
}

func TestConfigList_StripANSI_PlainOutput(t *testing.T) {
	tempConfigHome(t)
	var out bytes.Buffer
	opts := &ConfigOptions{stdout: &out}
	require.NoError(t, RunConfigList(opts))
	require.False(t, strings.Contains(out.String(), "\x1b["))
}
