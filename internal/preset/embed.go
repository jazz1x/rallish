package preset

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jazz1x/hocketty/pkg/contract"
)

//go:embed all:presets
var presetsFS embed.FS

// LoadEmbedded reads all shipped YAML presets from the embedded FS.
func LoadEmbedded() (map[string]contract.Preset, error) {
	subFS, err := fs.Sub(presetsFS, "presets")
	if err != nil {
		return nil, fmt.Errorf("sub presets fs: %w", err)
	}
	return loadFromFS(subFS)
}

func loadFromFS(fsys fs.FS) (map[string]contract.Preset, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read presets dir: %w", err)
	}

	presets := make(map[string]contract.Preset)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}

		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read preset %q: %w", name, err)
		}

		p, err := Load(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("load preset %q: %w", name, err)
		}

		presets[p.Name] = p
	}

	return presets, nil
}
