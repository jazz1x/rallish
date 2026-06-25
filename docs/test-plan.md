# rallish — Test & Quality Plan

> Test strategy, coverage targets, and the concrete gaps to close — anchored to the production-readiness audit (`docs/reports/2026-06-23-production-readiness-gaps.md`).
> **Version:** tracks `VERSION` (0.3.0) · **Last updated:** 2026-06-24 · [한국어](./test-plan.ko.md)

## 1. Quality goals

rallish makes strong claims — *safe, resumable, verifiable, auditable*. The test suite's job is to make those claims **true and demonstrable**, not aspirational. Three quality goals, in priority order:

1. **The wired surface is correct and race-clean.** Everything tagged ✅ in `feature-spec.md` must be covered by tests, including concurrency.
2. **The advertised surface is honestly tested.** Where a feature is ◑/○, the tests must reflect *what actually runs*, and the gaps must be visible (not hidden behind a stub that always passes).
3. **The real path is exercised, not just the stub.** The autonomous-cycle path is currently end-to-end-tested only through the `fake` adapter, which never touches a subprocess. At least one real-subprocess integration test must exist (gated/optional).

## 2. Current state (baseline)

- **Scale:** ~100 non-test Go files, ~73 test files, 26 test packages.
- **Race:** CI runs `go test ./... -race -count=1` (`scripts/check-all.sh`); all 26 test packages green. A one-time hardening pass in the 2026-06-23 audit additionally ran the broker at `-race -count=5` five times clean — that is an audit verification, not a standing CI step.
- **Benchmarks:** 5 micro-benchmarks (see `performance-spec.md` §4.1).
- **Known low/zero coverage** (from the audit): the real `claude` (~40 %) and `kimi` (~28.9 %) adapter `Run()` paths, `adapter.BuildPrompt` on the real path, the gate `StandardPipeline` (`PreflightGate`/`CommitGate`/`PhilosophyGate`, ~45.3 %), the auto-goal discovery logic (`discoverNextGoal` and helpers in package `cycle` — there is no exported `autogoal.Run` symbol; the name is informal shorthand), and `doctor` (~24.6 %) are under-covered or 0 %.
- **Coverage floor:** a ≥70 % floor is documented in `AGENTS.md`/`README` but **not yet enforced in CI**.

## 3. Test taxonomy

| Level | Scope | Tooling | Where |
|-------|-------|---------|-------|
| **Unit** | pure functions, contracts, predicates | `go test`, `testify` | per-package `*_test.go` |
| **Integration (in-process)** | broker + router + adapters wired via `fake` | `go test` | `internal/integration_test.go`, `internal/broker/*_test.go` |
| **Integration (real subprocess)** | real `claude`/`kimi` `Run()` end-to-end | build-tagged / env-gated | new: `internal/adapter/*/integration_test.go` |
| **Adversarial** | exclusive-holder, race, malformed input | `-race`, table-driven | e.g. `internal/cli/rally_adversarial_test.go` |
| **Conformance** | A2A v1.0 wire shape, MCP handshake | golden + client probes | `internal/broker/a2a_test.go`, `mcp` tests |
| **Property / fuzz** | parsers (`ParseLastJSONBlock`, JSON-RPC intake) | `go test -fuzz` | new |
| **Benchmark** | hot-path + scaling | `go test -bench -benchmem`, `benchstat` | see `performance-spec.md` |

## 4. Coverage targets

Per-package floors (raise the documented ≥70 % into CI enforcement):

| Package | Floor | Why |
|---------|-------|-----|
| `pkg/contract` | 85 % | the contract is the SSOT; ledger, gates, action-gate, merkle live here |
| `internal/router` | 80 % | routing correctness gates every session |
| `internal/exit`, `internal/budget` | 80 % | termination + anti-runaway logic |
| `internal/cycle` + `internal/cycle/gates` | 80 % | the differentiator; gate pipeline + stuck breaker |
| `internal/session`, `internal/preset`, `internal/ipc` | 75 % | core mechanics |
| `internal/broker` (incl. `rally.go`) | 75 % | the live surface |
| `internal/adapter` (claude/kimi `Run`) | 60 % + ≥1 gated real test | subprocess paths are hard to unit-test fully |
| `internal/doctor` | 70 % | first-run UX depends on it |

**Enforcement:** add a CI step running `go test -coverprofile` per package and failing under the floor. Until then the floor is advisory and this table is the target.

## 5. Test requirements by feature

Mapped to `feature-spec.md` IDs. Each bullet is a test that must exist.

### F2/F17 Rally + MCP (✅)
- Baton handed by A is delivered to B's open SSE stream.
- Non-holder `done` → 409; holder `done` advances the turn.
- Exclusive-holder under concurrent `done`/`join` (race test) — no double-hold.
- `rally join --timeout` returns exit 2 on no-baton; `--once` exits after one.
- MCP 2025-03-26 handshake + each tool (`create/join/done/status/interrupt`) round-trips.

### F3/F9/F10 Cycle + gates + anti-spin (✅, the differentiator)
- Pipeline order is exactly `Preflight→Audit→[local]→Philosophy→Polish→Commit`; first failure short-circuits.
- Preflight halts (exit 12) on `main`/`master`, dirty tree, empty goal.
- Commit gate never emits `--amend`/`--no-verify` (assert by inspecting the constructed git args).
- Audit/Polish honor `--audit-cmd`/`--polish-test-cmd`; whitespace-only override fails loudly.
- Stuck breaker trips on each of the 4 signals (repeat-turn ≥4, repeat-gate-fail ≥3, ping-pong ≥6, no-progress window 5) → halt 10.
- Lifetime ceiling halts a *productive* runaway (budget-exceeded, 11) that the stuck breaker would miss.
- **Reviver guard:** a sealed-halt cycle re-invoked via `cycle run --once` is not resurrected.
- **Resumability:** kill mid-cycle, recover from `.bak`, resume from `state.ID`.
- Every halt reason maps to the correct exit code (table-driven over `exitCodeForHalt`).

### F11/F12 Ledger + Merkle (✅ / ○)
- `VerifyChain` returns the index of any tampered entry; passes on an intact chain.
- Each appended entry's `prev_hash` == previous `hash`; genesis is 64 zeros.
- `schema_version` is stamped on every entry; a shape change is detectable.
- Oversized gate-report entries are read correctly (unbounded reader, not the 64 KiB Scanner).
- Merkle: inclusion/consistency proofs verify against RFC 6962/9162 vectors (already tested). **New requirement once F12 is wired:** an end-to-end test that the ledger-audit command produces and verifies a real inclusion proof.

### F13/F14 Action-gate (◑)
- Each deny rule → exit 13; each HITL rule → exit 14; safe → exit 0.
- A blocking decision with `--cycle-id` is recorded as `tooluse_decision`; a safe decision records nothing.
- **New requirement (Tier 2):** an end-to-end test of the PreToolUse hook wiring — a denied command is actually refused by the hook (not merely reported). Until the hook ships, this test documents the gap.

### F4 Adapters (✅ port, under-covered real path)
- `BuildPrompt` embeds the slimmed `TurnRequest` and the `TurnResponse` schema; `continue` vs (future) `cross_check` framing differs.
- `ParseLastJSONBlock`: last fenced block wins; balanced-brace fallback; clear error when absent → **fuzz target**.
- Env allowlist: only allowlisted vars reach the subprocess (no broad token leak).
- **Tier 3 real-subprocess test** (build-tagged `//go:build integration`, skipped unless `RALLISH_IT=1` and the CLI is authed): a real `claude -p` turn round-trips a `TurnResponse`. One such test closes the biggest trust gap.

### F6/F8 Routing + exit (◑)
- Round-robin index math; handoff override; blocked→reviewer escalation.
- **Gap test:** selecting `strict_round_robin`/`last_writer_wins` should not validate-then-fail-at-runtime (currently it does) — assert the intended behavior and mark it pending.
- Document-as-test: `tests_pass` in a preset does **not** terminate a broker squash (assert `exit_predicate_shell_skipped` is logged), while it *does* fire inside the cycle gate pipeline.

### F16 A2A conformance (◑)
- Agent card has a real `protocolVersion`; unknown JSON-RPC fields are rejected.
- **Gap test (Tier 2):** A2A SSE emits named `event:` lines and populates `sessionId` — write the test now (expected-fail/pending) so wiring it flips a red test green.

### F22 Cross-check ping-pong (▷ planned)
The PRD's §5 test plan is the acceptance suite for this feature: contract round-trip for intent+claims; broker forwards intent into the next request; 3 dry rounds → `dry_rounds`; 6-turn ping-pong → `stuck`; prompt differs for `continue` vs `cross_check`; `ClaimGate` verifies/falsifies and emits ledger events; preset parses `dry_rounds_threshold`.

## 6. The three highest-value additions (audit Tier 3)

1. **One real-adapter integration test.** Gated, optional, but real: drives `claude`/`kimi` `Run()` through a single turn and asserts a parsed `TurnResponse`. This is the single most important coverage gap — the autonomous path is currently only exercised via the `fake` stub.
2. **Gate-pipeline coverage to ~80 %.** `PreflightGate`, `CommitGate`, `PhilosophyGate`, and the auto-goal discovery logic (`discoverNextGoal` in package `cycle`) are the differentiator and are under-covered. Add table-driven tests for each gate's pass/warn/fail branches against a temp git repo.
3. **CI coverage-floor enforcement.** Turn the documented ≥70 % floor into a failing CI gate per §4.

## 7. CI & tooling

- **Pre-commit / pre-push:** `lefthook.yml` runs the local hooks; `scripts/check-all.sh` and `make check-all` are the audit-gate command (also the default for `cycle --audit-cmd`).
- **Lint:** `.golangci.yml`; a `scripts/check-no-raw-ansi.sh` guard.
- **Required CI matrix:** `go test ./...`, `go test -race ./...` (at least the broker and concurrency-sensitive packages), `go vet ./...`, `golangci-lint run`, and (advisory first) the coverage-floor and benchmark-regression gates.
- **Determinism:** all tests must be deterministic (use fake clocks — `Clock` interfaces exist in `budget`/`exit`/`session`; no wall-clock sleeps except where explicitly testing timeouts).
- **Security scans:** the repo already carries `.gitleaks.toml` and `.trivyignore`; keep secret-scan and vuln-scan in CI.

## 8. Definition of done (per change)

A change is done when:
- New/changed behavior has unit tests; concurrency-sensitive changes have a `-race` test.
- Coverage for touched packages does not drop below the §4 floor.
- `make check-all` passes (the same gate the autonomous cycle runs).
- For contract changes: round-trip JSON tests and a `schema_version` bump if the shape changed.
- For anything that flips a feature from ○/◑ to ✅: the corresponding "gap test" in §5 flips from pending to green, and `feature-spec.md`'s maturity tag is updated in the same change (honest naming).

## 9. Acceptance criteria for "quality is specified"

- [ ] Every ✅ feature in `feature-spec.md` has the tests listed in §5.
- [ ] At least one gated real-adapter integration test exists and is documented (§6.1).
- [x] Gate-pipeline packages reach their §4 floor (§6.2).
- [x] CI enforces the per-package coverage floor (§6.3).
- [ ] Parser fuzz targets exist for `ParseLastJSONBlock` and JSON-RPC intake.
- [ ] Pending/expected-fail tests exist for the named gaps (A2A SSE events, G6 hook enforcement, routing validate-then-fail) so closing each gap turns a red test green.
