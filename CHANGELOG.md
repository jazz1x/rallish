# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- A2A Protocol layer: `GET /.well-known/agent.json`, `POST /a2a` (JSON-RPC 2.0)
  - `tasks/send`, `tasks/get`, `tasks/cancel`, `tasks/sendSubscribe` (SSE)
- `pkg/contract/a2a.go` with AgentCard, TaskState, JSON-RPC envelopes
- Token budget hard enforcement in broker (`handleNextTurn`)
- `internal/scratch/scratch.go` with automatic compaction when `max_kb` exceeded
- Model hint injection into adapter prompts

### Changed

- Removed all "hocket" terminology; replaced with "turn-taking" / "relay"
- Updated `lefthook.yml` to scan staged Go files only
- Upgraded `.golangci.yml` to v2 format with `.toolchain` exclusion
- Fixed `internal/cli/start.go` forbidigo and errcheck violations

### Fixed

- Restored real Claude/Kimi adapters in the registry.
