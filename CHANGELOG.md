# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- SSE long-poll on `/next` gated by `?as=<role>` to eliminate race conditions between concurrent runners.
- `fake.NewPingPong(maxTurns, delay)` for realistic turn-taking simulation in demo presets.
- Release workflow (`.github/workflows/release.yml`) with cosign keyless signing and syft SBOM generation.
- Homebrew tap automation via GoReleaser.
- `RELEASING.md` with versioning policy and release checklist.

### Changed

- `fake` adapter in the CLI registry now simulates 1-second work per turn so the hocket flow is visible during demos.

### Fixed

- Restored real Claude/Kimi adapters in the registry.
