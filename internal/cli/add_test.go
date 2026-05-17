package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdd_List(t *testing.T) {
	var out strings.Builder
	err := listComponents(&out)
	require.NoError(t, err)

	s := out.String()
	require.Contains(t, s, "solo-ralph")
	require.Contains(t, s, "pair-review")
	require.Contains(t, s, "claude")
	require.Contains(t, s, "kimi")
	require.Contains(t, s, "fake")
	require.Contains(t, s, "TYPE")
	require.Contains(t, s, "NAME")
	require.Contains(t, s, "DESCRIPTION")
}

func TestAdd_Preset_Local(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	var out strings.Builder
	opts := AddOptions{
		Yes:    true,
		stdout: &out,
	}

	err := runAdd(context.Background(), opts, []string{"preset", "solo-ralph"})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(tmp, ".rallish", "presets", "solo-ralph.yaml")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(data), "name: solo-ralph")
	require.Contains(t, out.String(), "Installed preset solo-ralph")
	require.Contains(t, out.String(), "rallish squash --preset solo-ralph")
}

func TestAdd_Preset_Global(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	var out strings.Builder
	opts := AddOptions{
		Global: true,
		Yes:    true,
		stdout: &out,
	}

	err := runAdd(context.Background(), opts, []string{"preset", "pair-review"})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(tmp, ".rallish", "presets", "pair-review.yaml")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(data), "name: pair-review")
}

func TestAdd_Adapter_Local(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	var out strings.Builder
	opts := AddOptions{
		Yes:    true,
		stdout: &out,
	}

	err := runAdd(context.Background(), opts, []string{"adapter", "claude"})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(tmp, ".rallish", "adapters", "claude.yaml")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(data), "name: claude")
	require.Contains(t, out.String(), "Use runtime \"claude\" in your presets")
}

func TestAdd_UnknownBuiltIn(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	opts := AddOptions{
		Yes:    true,
		stdout: io.Discard,
	}

	err := runAdd(context.Background(), opts, []string{"preset", "nonexistent"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no built-in preset named \"nonexistent\"")
}

func TestAdd_Skill_NoBuiltIn(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	opts := AddOptions{
		Yes:    true,
		stdout: io.Discard,
	}

	err := runAdd(context.Background(), opts, []string{"skill", "anything"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no built-in skill named \"anything\"")
}

func TestAdd_FromURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "name: custom-preset\n")
	}))
	defer ts.Close()

	tmp := t.TempDir()
	t.Chdir(tmp)

	opts := AddOptions{
		Yes:    true,
		From:   ts.URL + "/custom-preset.yaml",
		stdout: io.Discard,
	}

	err := runAdd(context.Background(), opts, []string{"preset", "custom-preset"})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(tmp, ".rallish", "presets", "custom-preset.yaml")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(data), "name: custom-preset")
}

func TestAdd_ConfirmNo(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	var out strings.Builder
	in := strings.NewReader("n\n")
	opts := AddOptions{
		stdout: &out,
		stdin:  in,
	}

	err := runAdd(context.Background(), opts, []string{"preset", "solo-ralph"})
	require.NoError(t, err)
	require.Contains(t, out.String(), "Cancelled.")

	_, err = os.Stat(filepath.Join(tmp, ".rallish", "presets", "solo-ralph.yaml"))
	require.True(t, os.IsNotExist(err))
}

func TestAdd_ConfirmYes(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	var out strings.Builder
	in := strings.NewReader("y\n")
	opts := AddOptions{
		stdout: &out,
		stdin:  in,
	}

	err := runAdd(context.Background(), opts, []string{"preset", "solo-ralph"})
	require.NoError(t, err)
	require.Contains(t, out.String(), "Installed preset solo-ralph")

	_, err = os.Stat(filepath.Join(tmp, ".rallish", "presets", "solo-ralph.yaml"))
	require.NoError(t, err)
}

func TestAdd_Overwrite(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	targetDir := filepath.Join(tmp, ".rallish", "presets")
	require.NoError(t, os.MkdirAll(targetDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "solo-ralph.yaml"), []byte("old"), 0o644)) //nolint:gosec

	var out strings.Builder
	in := strings.NewReader("y\n")
	opts := AddOptions{
		stdout: &out,
		stdin:  in,
	}

	err := runAdd(context.Background(), opts, []string{"preset", "solo-ralph"})
	require.NoError(t, err)
	require.Contains(t, out.String(), "Overwrite")

	data, err := os.ReadFile(filepath.Join(targetDir, "solo-ralph.yaml")) //nolint:gosec
	require.NoError(t, err)
	require.Contains(t, string(data), "name: solo-ralph")
}

func TestAdd_ValidateType(t *testing.T) {
	require.NoError(t, validateType("adapter"))
	require.NoError(t, validateType("preset"))
	require.NoError(t, validateType("skill"))
	require.Error(t, validateType("foo"))
}

func TestAdd_ValidateName(t *testing.T) {
	require.NoError(t, validateName("foo"))
	require.Error(t, validateName(""))
	require.Error(t, validateName("foo/bar"))
	require.Error(t, validateName("foo\\bar"))
	require.Error(t, validateName("."))
	require.Error(t, validateName(".."))
}
