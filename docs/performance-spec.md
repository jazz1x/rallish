# rallish — Performance & Benchmark Specification

> Performance targets, benchmark scenarios, and measurement methodology for the broker, gate pipeline, ledger, and adapters.
> **Version:** tracks `VERSION` (0.3.0) · **Last updated:** 2026-06-24 · [한국어](./performance-spec.ko.md)

## 1. Purpose and scope

rallish is a **local, single-host broker**, not a distributed service. Its performance budget is dominated by one thing it does **not** control: the latency of the underlying agent CLI subprocess (a `claude` or `kimi` turn takes seconds to minutes). The broker's own work — routing a turn, appending a ledger line, running a gate — must be **negligible** next to that.

So the performance contract is asymmetric:

- **Broker overhead** (everything rallish does between adapter calls) must stay in the **sub-millisecond to low-millisecond** range and must not grow with session length.
- **Adapter latency** is measured and surfaced (via `Usage.Ms`) but is **out of rallish's control** and is excluded from rallish's own targets.

This document defines (a) the performance targets per component, (b) the benchmark suite that proves them, and (c) the methodology and environment so numbers are reproducible. Today only **5 micro-benchmarks** exist; this spec defines the target suite the project should grow into.

## 2. Performance principles

1. **O(window), never O(history).** Every per-turn operation (routing, stuck detection, budget check) must be bounded by a fixed window, not by the full session/ledger length. The stuck detector is explicitly O(window) over the ledger; this property must be preserved and benchmarked at growing ledger sizes to prove it.
2. **Append, don't rewrite.** The ledger is append-only JSONL. Writing turn N must not re-serialize turns 1..N−1.
3. **Allocation discipline on the hot path.** Per-turn functions report allocations (`b.ReportAllocs()`); regressions in allocs/op are treated as performance regressions.
4. **The broker is not the bottleneck.** If broker overhead is ever a measurable fraction of a turn, that is a bug.

## 3. Performance targets

Targets are **order-of-magnitude budgets** on the reference environment (§6), not hard SLAs. They exist so a regression is obvious. "Per turn" means the broker-side work for one ping-pong step, excluding the adapter subprocess.

### 3.1 Hot-path (per-turn broker work)

| Operation | Target (p50) | Target (p99) | Allocation target | Rationale |
|-----------|--------------|--------------|-------------------|-----------|
| `Router.Next` (routing decision) | < 5 µs | < 50 µs | 0 allocs steady-state | pure map/slice over roles |
| `Budgeter.Remaining` | < 1 µs | < 10 µs | 0 allocs | arithmetic only |
| `BuildPrompt` | < 100 µs | < 500 µs | ≤ 3 allocs + JSON marshal | one JSON marshal + string build |
| `ParseLastJSONBlock` (typical CLI output) | < 500 µs | < 2 ms | bounded by output size | regex + one unmarshal |
| `Stuck` detector (window over ledger) | < 200 µs | < 1 ms | O(window) | must NOT grow with ledger size |
| `LedgerFileSync.Append` (incl. hash) | < 2 ms | < 10 ms | bounded | one `lastHash` read + SHA-256 + append |
| `ChainHash` (single entry) | < 20 µs | < 100 µs | 1–2 allocs | one canonical marshal + SHA-256 |
| `VerifyChain` (per entry) | < 20 µs | < 100 µs | amortized | linear walk |

**Aggregate budget:** total broker overhead per turn (route + prompt build + parse + ledger append + budget/stuck checks) should be **< 5 ms p50, < 15 ms p99** — i.e. invisible next to a multi-second adapter turn.

### 3.2 Scaling (must stay flat or linear)

| Quantity | Requirement |
|----------|-------------|
| Per-turn overhead vs. session length | **Flat** — turn 1000's overhead ≈ turn 1's (proves O(window)) |
| `Stuck` cost vs. ledger size | **Flat** within the window (bounded scan), not O(n) over full ledger |
| `VerifyChain` / `ReadAll` vs. ledger size | **Linear** in entries; acceptable because it is an audit-time (not per-turn) operation |
| `LedgerFileSync.Append` `lastHash` cost vs. ledger size | Currently reads to find the last hash — **must be O(1) or O(tail)**, not O(n). Flag if it re-scans the whole file (see §7 risk). |
| Memory per active session | Bounded; scratchpad capped at preset `max_kb`; ledger streamed, not held wholesale |

### 3.3 Concurrency / liveness

| Property | Target |
|----------|--------|
| Broker race-cleanliness | `go test ./... -race -count=1` green in CI (a one-time audit pass also ran the broker at `-race -count=5` ×5 clean) |
| Exclusive-holder enforcement (rally) | No two participants hold the baton simultaneously under concurrent `done`/`join` |
| Socket dial timeout (CLI→daemon liveness probe) | 300 ms (already wired) |
| Daemon startup to first-accept | < 1 s on the reference environment |
| SSE delivery latency (baton handed → received on open stream) | < 50 ms local |

### 3.4 Startup / footprint

| Metric | Target |
|--------|--------|
| `rallish version` / `doctor` cold start | < 50 ms (single static Go binary) |
| Idle daemon RSS | < 30 MB |
| Binary size | tracked per release; no hard cap, watch for unexpected growth |

## 4. Benchmark suite

### 4.1 Existing benchmarks (baseline)

Six micro-benchmarks exist today:

| Benchmark | File | Covers |
|-----------|------|--------|
| `BenchmarkBuildPrompt` | `internal/adapter/prompt_test.go` | prompt construction |
| `BenchmarkManagerAppend` | `internal/scratch/scratch_test.go` | scratchpad append + compaction |
| `BenchmarkBudgeter_Remaining` | `internal/budget/budget_test.go` | budget arithmetic |
| `BenchmarkStoreAppend` | `internal/session/session_test.go` | session ledger append |
| `BenchmarkTurnResponse_Compact` | `pkg/contract/types_test.go` | turn response serialization |
| `BenchmarkLedgerAppend/size=*` | `internal/cycle/ledger_test.go` | ledger append + tail-hash scaling |

These are the **floor**. They cover serialization and append but leave the broker, gates, router, stuck detector, and end-to-end turn loop unmeasured.

### 4.2 Target benchmarks to add

Grouped by what they protect. Each should use `b.ReportAllocs()` and, where relevant, run at several input sizes via sub-benchmarks (`b.Run`).

**Hot path (per-turn):**
- `BenchmarkRouterNext` — routing decision for 1/3/8-role presets.
- `BenchmarkParseLastJSONBlock` — typical output, output with trailing noise, fallback balanced-brace path.
- `BenchmarkChainHash` / `BenchmarkVerifyChain` — single entry and per-entry amortized.
- `BenchmarkStuck/ledger=10,100,1000,10000` — **the key scaling guard**: cost must stay flat as the ledger grows (proves the window bound).

**Gate pipeline:**
- `BenchmarkStandardPipeline_NoShell` — pipeline overhead with stubbed shell gates (isolates rallish's own cost from `go test` / `make check-all`).
- `BenchmarkPhilosophyGate` — regex scan over a representative `git diff`, at small/medium/large diffs.

**Audit / Merkle (when wired, F12):**
- `BenchmarkMerkleRoot/n=...`, `BenchmarkInclusionProof`, `BenchmarkVerifyConsistency` — O(n) build, O(log n) proofs.

**Action-gate (G6):**
- `BenchmarkDecideToolUse` — classifier over short/long commands (must be O(len), cheap enough for a synchronous PreToolUse hook).

**End-to-end (with `fake` adapter):**
- `BenchmarkSquashLoop_Fake` — full N-turn ping-pong using `fake` (zero subprocess), reporting broker overhead per turn. This is the single most useful aggregate number: it isolates rallish's own per-turn cost.

### 4.3 Macro / scenario benchmarks (measured, reported, not gated)

Run manually or in a nightly job; these include the adapter subprocess and are **reported for observability**, not enforced as targets (adapter latency dominates and is out of scope):

| Scenario | What it measures |
|----------|------------------|
| `solo-ralph`, 10 turns, real `claude` | wall-clock per turn, `Usage.Ms` distribution, tokens/turn |
| `pair-review`, 20 turns, real `claude`+`kimi` | handoff overhead, cross-runtime turn cost |
| `cycle run --once` cold | gate-pipeline wall-clock excluding agent turn (audit/polish dominated by `go test`/`make`) |

Report broker-attributable overhead separately from adapter + shell-gate time so the asymmetry stays visible.

## 5. Metrics and instrumentation

- **Per-turn usage** is already in the contract: `Usage{tokens_in, tokens_out, ms}` on `TurnResponse`. Aggregate these per session to report tokens/turn and turn latency.
- **Ledger as a profiling source.** Event timestamps (`at`, Unix ms) on `agent_turn` / `gate_passed` / `gate_failed` let you reconstruct turn and gate durations from the audit trail without extra instrumentation.
- **Recommended additions** (not yet present): a `--profile` flag on `cycle run --once` to emit `pprof` CPU/heap profiles for the gate pipeline; a structured per-turn timing log line (gated behind config to avoid noise).

## 6. Reference environment & methodology

For numbers to be comparable across runs and contributors, record the environment with every benchmark report.

**Methodology.**
- Use Go's testing benchmarks: `go test -bench=. -benchmem -benchtime=...` per package; pin `-cpu` if comparing.
- Run on a quiet machine; disable turbo/throttle variance where possible; report median of ≥ 5 runs.
- For scaling benchmarks, plot ops vs. input size and assert the expected shape (flat / linear / log).
- Use `benchstat` to compare before/after and gate on statistically significant regressions, not single-run noise.
- Exclude adapter subprocess time from broker targets by benchmarking with the `fake` adapter.

**Record with each report:** Go version (currently `go 1.25.0`), OS/arch, CPU model, commit SHA, `rallish version`, and the exact `go test -bench` invocation.

**Suggested regression gate (CI, advisory first):** fail a PR if `benchstat` shows a > 20 % regression (time or allocs) on any hot-path benchmark, or any non-flat result on `BenchmarkStuck/*` and `BenchmarkLedgerAppend/*` scaling benchmarks.

## 7. Known performance risks (from code review)

- ✅ **`LedgerFileSync.lastHash` re-read is fixed.** Append previously walked the whole file to find the tail hash, making write cost O(n) per turn and O(n²) per session. It now caches the tail hash in a process-wide `ledgerLock` (`internal/cycle/ledger.go`) keyed by absolute ledger path; `BenchmarkLedgerAppend/size=*` demonstrates flat cost across ledger sizes.
- **`forEachLedgerLine` uses an unbounded `bufio.Reader`.** Correct (avoids the 64 KiB `Scanner` brick on large gate reports) but means a single pathological entry can spike memory. Bound entry size or document the assumption.
- **Philosophy gate regex over `git diff`.** Cost scales with diff size; large diffs in a single cycle could be slow. Benchmark at large diffs; consider a diff-size cap.
- **`ParseLastJSONBlock` fallback** scans for a balanced object across the whole output. Pathological adapter output (huge non-JSON text) makes this O(output). Bound the scanned window.
- **Cross-process ledger contention.** Only an in-process mutex guards appends; two drivers on one cycle file would contend at the OS level with no fairness guarantee. Out of scope for single-driver use, but note it.

## 8. Acceptance criteria for "performance is specified"

- [ ] The hot-path benchmarks in §4.2 exist and run green in CI.
- [ ] `BenchmarkStuck/*` and `BenchmarkLedgerAppend/*` demonstrate **flat** per-turn cost as the ledger grows (or the regressions are fixed).
- [ ] `BenchmarkSquashLoop_Fake` reports broker overhead per turn within the §3.1 aggregate budget.
- [ ] A reference benchmark report (§6 fields filled in) is committed under `docs/reports/` and refreshed per release.
- [x] The `lastHash` O(n) risk (§7) is confirmed resolved or documented as acceptable.
