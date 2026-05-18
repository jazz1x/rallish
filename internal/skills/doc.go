// Package skills bundles the rallish skill files into the rallish
// binary and exposes an Install function that writes them to a target directory.
//
// # Embedding strategy: Strategy A
//
// The canonical SKILL.md and SKILL.ko.md files live at
// internal/skills/rallish/ so that go:embed can reach them directly
// (go:embed cannot reference files outside the embedding package's directory
// tree). The repo-root skills/rallish/ directory is replaced with
// relative symlinks pointing back to internal/skills/rallish/, which
// keeps vendor-neutral scanners (anything that globs skills/*/SKILL.md)
// working without duplicating file content.
package skills
