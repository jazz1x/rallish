package safepath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		input   string
		wantErr bool
		wantSub string // substring the result must contain (if not error)
	}{
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "absolute path no traversal",
			input:   "/tmp/foo/bar",
			wantErr: false,
			wantSub: "foo/bar",
		},
		{
			name:    "relative path resolved to absolute",
			input:   "some/relative/path",
			wantErr: false,
			wantSub: cwd,
		},
		{
			name:    "dot segments cleaned",
			input:   "/tmp/foo/./bar",
			wantErr: false,
			wantSub: "/tmp/foo/bar",
		},
		{
			name:    "absolute path with double-dot before Abs normalization",
			input:   "/tmp/foo/../bar",
			wantErr: false,
			wantSub: "/tmp/bar",
		},
		{
			name:    "simple absolute",
			input:   "/usr/local/bin",
			wantErr: false,
			wantSub: "/usr/local/bin",
		},
		{
			name:    "root path",
			input:   "/",
			wantErr: false,
			wantSub: "/",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Clean(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Clean(%q) = %q, nil; want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Clean(%q) error: %v", tc.input, err)
			}
			if tc.wantSub != "" && !strings.Contains(got, tc.wantSub) {
				t.Fatalf("Clean(%q) = %q; want contains %q", tc.input, got, tc.wantSub)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("Clean(%q) = %q; want absolute path", tc.input, got)
			}
		})
	}
}

func TestUnderRoot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cases := []struct {
		name    string
		p       string
		root    string
		wantErr bool
	}{
		{
			name:    "path directly under root",
			p:       filepath.Join(tmpDir, "sub"),
			root:    tmpDir,
			wantErr: false,
		},
		{
			name:    "path deeply nested under root",
			p:       filepath.Join(tmpDir, "a", "b", "c"),
			root:    tmpDir,
			wantErr: false,
		},
		{
			name:    "path equal to root",
			p:       tmpDir,
			root:    tmpDir,
			wantErr: false,
		},
		{
			name:    "path escapes root sibling dir",
			p:       filepath.Dir(tmpDir) + "/other",
			root:    tmpDir,
			wantErr: true,
		},
		{
			name:    "path is parent of root",
			p:       filepath.Dir(tmpDir),
			root:    tmpDir,
			wantErr: true,
		},
		{
			name:    "empty p",
			p:       "",
			root:    tmpDir,
			wantErr: true,
		},
		{
			name:    "empty root",
			p:       filepath.Join(tmpDir, "x"),
			root:    "",
			wantErr: true,
		},
		{
			name:    "path with dot-dot that resolves inside root",
			p:       filepath.Join(tmpDir, "a", "..", "b"),
			root:    tmpDir,
			wantErr: false,
		},
		{
			name:    "path with dot-dot that resolves outside root",
			p:       filepath.Join(tmpDir, "..", "outside"),
			root:    tmpDir,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := UnderRoot(tc.p, tc.root)
			if tc.wantErr && err == nil {
				t.Fatalf("UnderRoot(%q, %q) = nil; want error", tc.p, tc.root)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("UnderRoot(%q, %q) = %v; want nil", tc.p, tc.root, err)
			}
		})
	}
}
