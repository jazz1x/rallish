# rallish — Feature Specification

> Consolidated functional spec. Maps every advertised feature to its **actual wired state** in the code, with behavior contracts and acceptance criteria.
> **Version:** tracks `VERSION` (0.3.0) · **Last updated:** 2026-06-24 · [한국어](./feature-spec.ko.md)

## How to read this document

This spec is the single place that answers *"what does rallish actually do today, and what is only declared?"* It exists because the advertised surface (README, north-star) is intentionally wider than the wired surface, and an implementer needs to know the difference before building on top of it.

**Maturity legend** (same as `north-star.md`):

| Tag | Meaning |
|-----|---------|
| ✅ **Wired** | Has production call sites; runs at runtime; covered by tests |
| ◑ **Partial** | Wired but incomplete, asymmetric, or warning-only |
| ○ **Declared-only** | Code/policy exists but has zero production call sites, or is parsed but never consumed |
| ▷ **Planned** | Specced (PRD exists) but not yet built |

Every feature row cites the authoritative source file. Where a claim was spot-verified against code, the file:line is given.

---

## 1. Feature Map (at a glance)

| # | Feature | Maturity | Source of truth |
|---|---------|----------|-----------------|
| F1 | Squash (headless preset sessions) | ✅ | `internal/cli/squash.go`, `internal/preset` |
| F2 | Rally (interactive baton-passing) | ✅ | `internal/cli/rally.go`, `internal/broker/rally.go` |
| F3 | Autonomous cycle + reference driver (`cycle run --once`) | ✅ | `internal/cli/cycle_run.go`, `internal/cycle` |
| F4 | Adapter port (claude / kimi / fake) | ✅ | `internal/adapter` |
| F5 | Preset system (YAML templates) | ✅ | `internal/preset` |
| F6 | Turn routing | ◑ | `internal/router/router.go` |
| F7 | Token / turn / wall-clock budgets | ✅ | `internal/budget`, `internal/exit` |
| F8 | Exit conditions | ◑ | `internal/exit/exit.go` |
| F9 | Gate pipeline (preflight→audit→philosophy→polish→commit) | ✅ | `internal/cycle/gates/pipeline.go` |
| F10 | Anti-spin stuck breaker + lifetime ceiling | ✅ | `internal/cycle/stuck.go`, `internal/budget` |
| F11 | Hash-chained append-only ledger (G4 audit) | ✅ | `pkg/contract/harness_ledger.go`, `internal/cycle/ledger.go` |
| F12 | Merkle (RFC 9162) inclusion/consistency proofs | ○ | `pkg/contract/merkle.go` |
| F13 | Action-gate (G6) decision + recording | ◑ | `pkg/contract/action_gate.go`, `internal/cli/gate.go` |
| F14 | Secret containment classifier | ◑ | `pkg/contract/tooluse_gate.go` |
| F15 | Unix-socket IPC + TCP loopback fallback | ✅ | `internal/ipc/socket.go`, `internal/cli/broker_client.go` |
| F16 | A2A v1.0 wire shape (agent-card, JSON-RPC, SSE) | ◑ | `internal/broker/a2a.go` |
| F17 | MCP server (rally tools over SSE) | ✅ | `internal/broker/mcp.go` |
| F18 | Auto-daemon spawn | ✅ | `internal/cli/broker_client.go` (`ensureBrokerClient`: squash + `rally new`) |
| F19 | `doctor` diagnostics | ◑ | `internal/doctor/doctor.go` |
| F20 | Shared scratchpad with compaction | ○ | `internal/scratch` |
| F21 | Log-time secret redaction | ○ | `internal/logx` (stub) |
| F22 | Cross-check ping-pong (intent-aware handoff, dry-round breaker, claim oracle) | ▷ | `docs/prd-cross-check-ping-pong.md` |

The rest of the document specifies each in detail.

---

## 2. Core Concepts

**Broker / daemon.** A single local HTTP server (`rallish daemon`) bound to `127.0.0.1:0` (dynamic port). It owns conversation/session state and decides whose turn it is. It is reached over a Unix domain socket (`~/.rallish/rallish.sock`, mode `0600`) with a TCP-loopback fallback. The broker is a **carrier of turns and state**, never a judge of quality.

**Adapter.** A thin wrapper around an agent runtime CLI. The port is two methods: `Name() string` and `Run(ctx, TurnRequest) (TurnResponse, error)` (`internal/adapter/adapter.go`). Concrete adapters: `claude`, `kimi`, `fake`.

**Turn contract.** The broker sends a `TurnRequest` (turn number, role, budget, last-turn summary, task) and the adapter returns a `TurnResponse` (`done`, `handoff_to`, `summary`, `artifacts`, `self_eval`, optional `usage`). Defined in `pkg/contract/types.go`.

**Preset.** A YAML template defining roles (with runtime + model), a routing strategy, a budget, exit conditions, and scratchpad limits. Shipped presets are compiled into the binary via `go:embed`.

**Cycle.** An autonomous unit of bounded repository work driven through the gate pipeline, with an append-only hash-chained ledger. The orchestrator that drives cycles is a *reference driver*, not the product — the product is the harness layer (gates/state/audit/breaker).

---

## 3. Feature Specifications

### F1 — Squash (headless preset sessions) ✅

**What it is.** `rallish squash` runs a headless preset session end-to-end; the broker spawns the configured adapters automatically and runs the ping-pong to completion.

**Behavior.**
- Resolves a preset by name: built-in first, then `~/.rallish/presets/<name>.yaml`. `squash` does **not** read a project-local `./.rallish/presets/` (squash.go:174 uses the user home dir only). Default preset from config `default_preset` (ships as `solo-ralph`).
- Auto-spawns the broker daemon if none is running (via the shared `ensureBrokerClient`; `rally new` does the same — see F18).
- Drives turns until an exit condition fires.

**Acceptance criteria.**
- AC-F1.1: `rallish squash --preset solo-ralph` with the `claude` adapter on PATH completes a session and exits 0.
- AC-F1.2: With no daemon running, `squash` starts one and reaches it over the Unix socket.
- AC-F1.3 ✅: A zero-credential smoke path ships as the bundled `fake-demo` preset (`internal/preset/presets/fake-demo.yaml`): one role on the in-process `fake` runtime, `turns_exhausted` after 5 turns. `rallish squash --preset fake-demo` auto-spawns the daemon, runs to completion, and writes a ledger — verified end-to-end with no agent CLI or API key present.

**Known gaps.**
- ✅ G-F1 (resolved): the bundled `fake-demo` preset gives a credential-free install check; no hand-written YAML needed.

---

### F2 — Rally (interactive baton-passing) ✅

**What it is.** `rallish rally` provides live baton-passing between two or more coding-CLI sessions. Exclusive-holder enforcement is delivered over SSE; agents self-loop the ping-pong without a per-turn user trigger.

**Command surface.**

| Command | Purpose | Required flags |
|---------|---------|----------------|
| `rally new` | Create a session | `--participants` (≥2, comma-sep) |
| `rally join` | Join and wait for the baton (SSE) | `--session-id`, `--as` |
| `rally done` | Pass the baton on | `--session-id`, `--as` |
| `rally status` | Show session state | `--session-id` |
| `rally mcp-agent` | One-shot MCP client (create/join/done/status/interrupt) | `--mode` |

**Behavior contracts.**
- `rally new` validates ≥2 participant names and an optional repo path (must exist). `--first` pre-assigns the baton; if omitted, the first-listed participant must `join` to start.
- `rally join` opens an SSE stream to `/rally/sessions/{id}/baton?as={participant}`, prints turn number + instructions. `--once` exits after the first baton; `--timeout` returns **exit 2** if no baton arrives in the window. `--timeout 0` blocks indefinitely.
- `rally done` returns **HTTP 409** as a non-fatal "not your turn" / "session interrupted" condition (exit 1).

**Acceptance criteria.**
- AC-F2.1: A baton handed by participant A is delivered to participant B's open `join` stream.
- AC-F2.2: A non-holder calling `rally done` is rejected with 409 and a clear message.
- AC-F2.3: `rally join --timeout 5s` against a session with no incoming baton exits with code 2.

**Known gaps.**
- ✅ G-F2.1 (resolved): `rally new` now auto-spawns the daemon via the shared `ensureBrokerClient` helper (`internal/cli/broker_client.go`), mirroring `squash`. A stranger no longer has to run `rallish daemon` first. (`join`/`done`/`status`/`cycle` still use `resolveBrokerClient` directly — they act on an existing session, so a missing daemon stays a clear up-front error rather than a surprise mid-flow spawn.)
- ✅ G-F2.2 (resolved at the root): a typo'd `--as` does **not** block — the broker validates participant membership on the baton stream and returns **403 "participant … is not in this session"** immediately (`internal/broker/rally.go`), which the CLI surfaces as a clear error. `--timeout` remains opt-in for the legitimate case (waiting on a peer who has not yet passed the baton); a blunt default timeout is deliberately *not* added, as it would cut short valid long waits.

---

### F3 — Autonomous cycle + reference driver ✅

**What it is.** A bounded, resumable unit of autonomous repo work. The canonical entry point for cron/CI is `rallish cycle run --once`: it runs **a single bounded pass then exits**, with the exit code carrying the halt reason. It is a reference driver, **not** a watch loop.

**Command surface.** `cycle new`, `cycle start` (one-shot create+watch), `cycle run --once` (reference driver), `cycle status`, `cycle ledger`, `cycle next`, `cycle halt`, `cycle watch`.

**Exit-code contract** (`internal/cli/cycle_run.go`, `exitCodeForHalt`):

| Code | Halt reason |
|------|-------------|
| 0 | success / clean pass (advanced) |
| 10 | stuck |
| 11 | budget-exceeded |
| 12 | preflight-failed |
| 13 | gate-failure |
| 14 | unparseable-turn |
| 15 | user-requested |
| 16 | self-audit-violation |
| 17 | ssh-auth-failed |
| 18 | max-cycles-reached |
| 19 | unknown-reason (forward-compat) |
| 1 | operational error (unreadable state, adapter not found) |

**Two modes of `cycle run --once`.**
- **Pure gate-pipeline** (no `--agents`): loads state from disk, runs the reviver anti-spin guard, the stuck breaker, and the lifetime budget ceiling, then one `cycle.Driver.Step()` through the standard gate pipeline.
- **Agent-orchestrated** (`--agents claude,kimi`): same preamble, then delegates to the multi-agent orchestrator which runs one agent turn plus one gated step.

**Resumability.** State files (`~/.rallish/cycles/cycle-<id>.json`) are written atomically with `.bak` recovery (G1). A halt is **sticky**: it is sealed into the ledger so a cron-revived spinning run self-halts and is not resurrected (anti-spin reviver guard).

**Acceptance criteria.**
- AC-F3.1: `cycle run --once` on a clean, advanceable cycle exits 0 and increments the completed-cycle count.
- AC-F3.2: A stuck cycle exits 10 and seals `cycle_halted` to the ledger.
- AC-F3.3: Re-invoking `cycle run --once` on a sealed-halt cycle does not resurrect it.

---

### F4 — Adapter port ✅

**What it is.** The minimal two-method port that lets any agent runtime CLI plug in.

```go
type Adapter interface {
    Name() string
    Run(ctx context.Context, req contract.TurnRequest) (contract.TurnResponse, error)
}
```

**Shipped adapters.**

| Adapter | Invocation | Env allowlist |
|---------|-----------|---------------|
| `claude` | `claude -p <prompt> --max-turns=1` | `PATH, HOME, LANG, TERM, USER, LOGNAME, SHELL, TMPDIR, XDG_CONFIG_HOME, ANTHROPIC_` |
| `kimi` | `kimi -p <prompt>` | …same base… `+ KIMI_` |

> Allowlist entries are matched by **exact key or prefix** (`internal/adapter/env.go`, `strings.HasPrefix`). `ANTHROPIC_` / `KIMI_` are prefixes — any env var whose name begins with them passes through. They are literal prefixes, not globs (no `*`).
| `fake` | in-process canned responses (test/demo) | n/a |

**Prompt + parse contract** (`internal/adapter/prompt.go`):
- `BuildPrompt` embeds a slimmed `TurnRequest` as fenced JSON plus a preamble describing the `TurnResponse` schema.
- `ParseLastJSONBlock` extracts the **last** fenced JSON block; falls back to a balanced-brace scan. If neither is found, returns `no JSON TurnResponse found in output`.
- The subprocess `cmd.Dir` is set to `req.Task.RepoRoot` when present. The environment is restricted to the allowlist (no broad token leakage).

**Acceptance criteria.**
- AC-F4.1: A well-formed fenced-JSON `TurnResponse` round-trips through `Run` unchanged.
- AC-F4.2 ✅: An unauthenticated/rate-limited runtime CLI surfaces an actionable error. A shared classifier (`internal/adapter/diagnose.go`, `DiagnoseOutput`) inspects subprocess stdout/stderr for auth and rate-limit signatures; both `claude` and `kimi` `Run` map a match to a clear message ("…runtime is not authenticated — run `claude` once interactively to log in…") instead of `no JSON TurnResponse found in output`.

**Known gaps.**
- ✅ G-F4 (resolved): auth/rate-limit failures now classify into actionable messages at the adapter boundary, and `doctor --probe` runs one minimal live turn per adapter to verify auth (not just PATH presence). The probe is opt-in because it spends a turn; it is bounded (`probeTimeout`) so a hung login prompt cannot stall the diagnostic.
- Note: Only `claude` and `kimi` ship adapter code. Cursor/Codex are *addable via the 2-method port*, not shipped.

---

### F5 — Preset system ✅

**Schema** (`internal/preset/preset.go`, validated with `DisallowUnknownField`):

```yaml
name: <string, required>
description: <string>
roles:                      # ≥1 required
  - id: <string>
    runtime: <claude|kimi|fake>
    model: <string hint>
routing: <round_robin | handoff_then_round_robin | strict_round_robin | last_writer_wins>
budget:
  max_turns: <int > 0, required>
  max_tokens: <int64 > 0, required>
  deadline_minutes: <int>
exit_when: [<exit condition>, ...]
scratch:
  max_kb: <int64>
  summarize_with: <string>
```

**Shipped presets.**

| Preset | Roles | Routing | Budget | Exit when |
|--------|-------|---------|--------|-----------|
| `solo-ralph` | ralph (claude/sonnet) | round_robin | 30 turns · 600k tokens · 90 min | tests_pass, turns_exhausted, deadline_passed |
| `pair-review` | planner (claude/opus), executor (kimi/k2), reviewer (claude/sonnet) | handoff_then_round_robin | 20 turns · 400k tokens · 60 min | tests_pass, reviewer_approved, turns_exhausted |

Both shipped presets also declare `scratch: { max_kb: 64, summarize_with: claude-haiku }`. Note this is parsed but **not yet consumed** at runtime — see F20.

**Validation rules.** `name` required; ≥1 role; `max_turns > 0`; `max_tokens > 0`; routing must be one of the four names; unknown YAML keys are rejected (strict parse).

**Acceptance criteria.**
- AC-F5.1: A preset with an unknown top-level key is rejected at load.
- AC-F5.2: A preset with `max_turns: 0` is rejected.

---

### F6 — Turn routing ◑

**Decision priority** (`internal/router/router.go`, `Next`):
1. **Explicit handoff** — if the previous `TurnResponse.HandoffTo` names a valid role, route there.
2. **Blocked escalation** — if `prev.SelfEval == "blocked"`, escalate to a `reviewer` role if one exists; else error.
3. **Routing rule** — apply the preset strategy.

**Why ◑.** Only `round_robin` and `handoff_then_round_robin` are implemented. `strict_round_robin` and `last_writer_wins` are *accepted by the schema validator* but return `routing rule %q not supported in phase 1` at runtime. An implementer must either implement them or the validator should reject them until they exist.

**Acceptance criteria.**
- AC-F6.1: With `round_robin`, role assignment cycles `(turn-1) mod len(roles)`.
- AC-F6.2: A `handoff_to` naming a valid role overrides round-robin.
- AC-F6.3 (gap): selecting `strict_round_robin` should not validate-then-fail-at-runtime.

---

### F7 — Budgets ✅ / F8 — Exit conditions ◑

**Budget tracking** (`internal/budget/budget.go`). Per turn: `tokens_left -= tokens_in + tokens_out`; `turns_left -= 1`. Wall-clock deadline is stored and compared against elapsed time; it does not decrement. `IsExhausted` = `turns_left ≤ 0 || tokens_left ≤ 0`.

**Lifetime ceiling.** `LifetimeTurns` counts `agent_turn` ledger events across the whole append-only log (survives revivals); `ExceedsLifetimeCeiling` is the **hard cost ceiling distinct from the stuck breaker** — it stops a *productive* runaway that a stuck detector would never catch.

**Exit conditions** (`internal/exit/exit.go`):

| Condition | Evaluation |
|-----------|-----------|
| `turns_exhausted` | `turns_left ≤ 0` |
| `tokens_exhausted` | `tokens_left ≤ 0` |
| `deadline_passed` | now > start + deadline |
| `reviewer_approved` | last response `self_eval == confident` **and** `done` |
| `tests_pass` | runs `go test ./...` (shell predicate) |
| `all_artifacts_compile` | runs `go vet ./...` (shell predicate) |

**Why F8 is ◑.** The shell predicates (`tests_pass`, `all_artifacts_compile`) require `allowShell=true`, but the broker constructs the evaluator with `allowShell=false` and intentionally logs `exit_predicate_shell_skipped` — running shell from a global daemon would execute in the daemon CWD, not the session repo, and is a deliberate security-posture choice. **Consequence:** preset `exit_when: [tests_pass]` does not fire on the squash/broker path today; convergence relies on `reviewer_approved`, `turns_exhausted`, or `deadline_passed`. The shell predicates *do* run inside the cycle gate pipeline (audit/polish gates), which sets the correct `cmd.Dir`.

**Acceptance criteria.**
- AC-F7.1: A turn consuming N tokens reduces `tokens_left` by exactly N.
- AC-F8.1: A session reaching `turns_exhausted` terminates with that reason.
- AC-F8.2 (documented behavior): `tests_pass` in a *preset* does not terminate a broker-driven squash; document this clearly to avoid surprise.

---

### F9 — Gate pipeline ✅

**Single source of truth:** `StandardPipeline(auditCmd, polishTestCmd, localGates)` in `internal/cycle/gates/pipeline.go`. Both the broker and the CLI one-shot delegate here, so the order cannot drift.

**Order:** `Preflight → Audit → [repo-local command gates] → Philosophy → Polish → Commit`.

| Gate | Checks | Failure semantics |
|------|--------|-------------------|
| **Preflight** | branch ∉ {main, master}; working tree clean; baseline SHA captured; `next_cycle_goal` non-empty | halt (exit 12) on any of these four; **SSH auth is a separate best-effort check that only warns and continues — it never halts** |
| **Audit** | runs `--audit-cmd` (default `make check-all`); whitespace-only override fails loudly | halt (exit 13) |
| **Local gates** | each `--local-gate` command, in order | halt (exit 13) |
| **Philosophy** | scans `git diff <baseline>` for ROP / SSOT / SRP / hardcoded-version violations | always **warns** on the first cycle that finds violations; fails **only** when violations were already recorded from a prior cycle **and** the new count strictly exceeds that prior count. A failure here halts with reason `self-audit-violation` → **exit 16** (not 13) |
| **Polish** | runs `--polish-test-cmd` (default `go test -race ./...`) | halt (exit 13) |
| **Commit** | `git add -A` then `git commit -m <goal>`; **never `--amend`, never `--no-verify`** | "nothing to commit" is acceptable |

**Guarantees.** The main-branch ban and the `--no-verify` ban are enforced structurally (the flags are never added in code). Gate execution short-circuits on the first failure.

**Acceptance criteria.**
- AC-F9.1: Running on `main` halts at Preflight with exit 12.
- AC-F9.2: A failing audit command halts at exit 13 before Commit is reached.
- AC-F9.3: The commit gate never passes `--no-verify`.

---

### F10 — Anti-spin (stuck breaker + lifetime ceiling) ✅

**Stuck predicates** (`internal/cycle/stuck.go`, pure O(window) over the ledger):

| Signal | Threshold |
|--------|-----------|
| Repeated turn (same summary+files fingerprint) | ≥ 4 |
| Repeated gate failure (same gate+summary) | ≥ 3 |
| Ping-pong (A,B,A,B alternation, no new artifacts) | ≥ 6 |
| No progress (last K turns add no new files **and** no `validation_green`) | window = 5 |

Called from `orchestrator.RunOnce`; on match it records `cycle_halted` and persists state. The lifetime budget ceiling (F7) is checked alongside.

**Design principle.** *Detect "stuck", don't define "progress."* Frontier-growth-vs-cycle is harder to game than self-report, but it is **not** un-gameable (an agent can churn new nodes). The only un-gameable signal is the verifier-produced green gate (F9), which the worker cannot write.

**Acceptance criteria.**
- AC-F10.1: 4 identical turns trip the repeated-turn breaker → halt 10.
- AC-F10.2: A 6-turn ping-pong with no new artifacts trips → halt 10.

---

### F11 — Hash-chained ledger (G4 audit) ✅

**Format.** Append-only JSONL, one `HarnessLedgerEntry` per line (`pkg/contract/harness_ledger.go`). Every entry carries `schema_version` (currently `"1"`), `prev_hash`, and `hash` = SHA-256 over the canonical entry (with `hash` zeroed) concatenated with `prev_hash`. Genesis hash is 64 hex zeros.

**Event types.** `cycle_created`, `agent_turn`, `gate_passed`, `gate_failed`, `handoff_created`, `cycle_halted`, `cycle_completed`, `validation_green`, `action_denied`, `secret_flagged`, `gates_pinned`, `gate_tampered`, `tooluse_decision`.

**Integrity.** `VerifyChain` (pure reader) walks entries, checks each `prev_hash` link and recomputes each `hash` for content-tamper detection, returning the first broken index or −1.

**Writer.** `LedgerFileSync.Append` (`internal/cycle/ledger.go`) uses a per-path in-process mutex; files are created `0600`; lines are read with an unbounded `bufio.Reader` (not the 64 KiB `Scanner`, which would brick on oversized gate reports).

**Known gap.** Cross-process write coordination is **not** provided (no `flock`); the design assumes a single active writer per cycle. Document this for any setup that might run two drivers against one cycle file.

**Acceptance criteria.**
- AC-F11.1: Tampering with any entry's content makes `VerifyChain` return that entry's index.
- AC-F11.2: Every appended entry's `prev_hash` equals the previous entry's `hash`.

---

### F12 — Merkle proofs (RFC 9162) ○

**Status: declared-only.** `MerkleRoot`, `InclusionProof`, `VerifyInclusion`, `ConsistencyProof`, `VerifyConsistency` are implemented and unit-tested in `pkg/contract/merkle.go` (RFC 6962/9162-conformant, with leaf/node domain separation), but there are **zero production call sites**. The library is complete and dead.

**To make it true (audit Tier 2, item 10):** wire it into a real path — e.g. a `rallish gate verify` / ledger-audit command that produces inclusion/consistency proofs — or stop tagging RFC 9162 proofs as ✅ in user-facing docs until then.

---

### F13 — Action-gate (G6) ◑ / F14 — Secret containment ◑

**What it is.** A pre-execution policy classifier for a runtime PreToolUse hook. `rallish gate tooluse --command "<cmd>"` decides and records; **the hook enforces** via the exit code.

**Decision model** (`DecideToolUse`, most-severe of action + secret): `deny` > `needs-hitl` > `allow`. Exit codes: `0` allow, `13` deny, `14` needs-hitl.

**Action deny-list** (`pkg/contract/action_gate.go`, pure O(len) matcher over a normalized command):

| Rule | Verdict |
|------|---------|
| `rm -rf` targeting `/`, `/*`, `~`, `$HOME` | deny |
| fork bomb `:(){ :\|:& };:` | deny |
| `dd of=/dev/…`, `mkfs /dev/…`, redirect to `/dev/sd*` or `/dev/nvme*` | deny |
| `git push --force` / `--force-with-lease` to main/master/release | deny |
| `git reset --hard origin/…` | needs-hitl |
| `DROP TABLE` / `DROP DATABASE` / `TRUNCATE TABLE` | needs-hitl |

**Recording.** A blocking decision (deny/needs-hitl) with `--cycle-id` is appended to that cycle's ledger as `tooluse_decision`; **safe (allow) decisions are never recorded** (false-positive guard).

**Why ◑.** The policy classifier and recording are wired and tested, but rallish **declares + records only** — it does not execute, intercept, or block anything. Without a user-wired PreToolUse hook calling `gate tooluse` and honoring the exit code, `rm -rf /` runs unblocked. This is a deliberate boundary (rallish never becomes the executor), but the README presents G6 as a live safety feature.

**To make it true (audit Tier 2, item 9):** ship the hook wiring + a runbook, or downgrade the claim to "policy declaration; enforcement requires hook X."

**Acceptance criteria.**
- AC-F13.1: `gate tooluse --command "rm -rf /"` prints a deny decision and exits 13.
- AC-F13.2: A safe command exits 0 and writes nothing to the ledger.
- AC-F13.3: With a wired hook, a denied command is actually refused (requires hook — see gap).

---

### F15 — IPC ✅

Unix domain socket at `~/.rallish/rallish.sock` (mode `0600`), preferred. The CLI resolves the broker via the `~/.rallish/socket` pointer (validated to live under `~/.rallish`), probes liveness with a 300 ms dial, removes a stale pointer, and falls back to TCP on `127.0.0.1:<port>` from `~/.rallish/port`. Loopback only — never `0.0.0.0`.

**Acceptance criteria.**
- AC-F15.1: With a live socket, the CLI uses it (not TCP).
- AC-F15.2: A stale socket pointer is cleaned up and TCP fallback succeeds.

---

### F16 — A2A v1.0 wire shape ◑ / F17 — MCP server ✅

**A2A.** Serves `GET /.well-known/agent-card.json` (plus legacy `agent.json`) returning an `AgentCard` with `protocolVersion`, capabilities (`streaming: true`), and skills. JSON-RPC intake is strict (`DisallowUnknownFields`). Methods: send-message, subscribe-to-task, get-task, cancel-task (plus legacy `tasks/*` aliases).

**Why ◑.** The A2A SSE path emits **only `data:` lines** (`internal/broker/a2a.go`), with no named `event:` type lines, and `A2ATask.sessionId` is not populated. A stock A2A v1.0 client that discriminates on event type fails today. The **MCP** path (`internal/broker/mcp.go`) already emits named `event:` lines (`endpoint`, `message`) — mirror that on the A2A path to reach conformance (audit Tier 2, item 12). Signed cards and mutual auth are deferred.

**MCP (F17, ✅).** `GET /mcp/sse` + `POST /mcp/message?session_id=…`, MCP 2025-03-26 handshake, rally tools (`rally_create/join/done/status/interrupt`). `rally mcp-agent` is a bundled one-shot client.

**Acceptance criteria.**
- AC-F16.1: `GET /.well-known/agent-card.json` returns a card with a real `protocolVersion`.
- AC-F16.2: JSON-RPC with unknown fields is rejected.
- AC-F16.3 (gap): A2A SSE emits named `event:` lines and populates `sessionId`.

---

### F18 — Auto-daemon ✅ / F19 — doctor ◑

- **F18:** `squash` **and `rally new`** auto-spawn the broker (via the shared `ensureBrokerClient`); `rally join`/`done`/`status` and `cycle` require an already-running daemon (they operate on an existing session). The `doctor`/`bootstrap` "daemon not running — will auto-spawn on `rally new`" message is now **accurate** (it was false when only `squash` auto-spawned).
- **F19:** `doctor` reports daemon reachability, adapter presence on PATH, and config/skill paths. With `--probe` it also verifies adapter **auth** via one bounded live turn per adapter (G-F4 resolved). Adapters absent from PATH are reported as info, not failure.

---

### F20 — Scratchpad ○ / F21 — Log redaction ○

- **F20 (declared-only):** `internal/scratch` (`Manager`, `Append`, compaction at 80% of `max_kb`) is imported by **zero** production code. Preset `scratch:` parses into `ScratchConfig` and `TurnRequest.ScratchPath` exists, but nothing populates or consumes them. Wiring it (manager per session + path injection + adapter consumption) is the work to make the feature real.
- **F21 (declared-only):** `internal/logx` is a 2-line stub; there is no log-time secret redaction. Redaction exists only in the pre-exec command classifier (F14), not on log output.

---

### F22 — Cross-check ping-pong ▷ (planned)

**Status: specced, not built.** Full spec in `docs/prd-cross-check-ping-pong.md`. It adds, to the `squash`/`pair-review` path:
- **P0′ intent-aware carryover** — `HandoffIntent` (`continue` / `cross_check`) on `TurnResponse`; the broker forwards it (carrier, not judge) and the adapter prompt builder selects framing so a reviewer inspects artifacts adversarially instead of echoing the executor's summary.
- **P1′ loop-until-dry + stuck-breaker** — preset `dry_rounds_threshold` + `exit_when: [dry_rounds]`; a pure `SessionStuck` helper over `TurnRecord`s.
- **P2′ verifiable discovery** — optional `Claims []Violation` with a reproducible `Check`; broker appends `claim_registered` ledger events (does not verify).
- **P3′ external oracle anchor** — a `ClaimGate` runs `Check.Command`, compares to `Check.Expected`, and emits `claim_verified` / `claim_falsified`.

**Acceptance criteria** (from the PRD): executor→reviewer handoff produces `cross_check`; the reviewer prompt does not trust the executor summary; 3 dry rounds exit `dry_rounds`; a 6-turn ping-pong exits `stuck`; a passing claim emits `claim_verified`, a failing one `claim_falsified` and halts; `make check-all` passes.

**Guardrails to preserve:** no broker judgment, no LangGraph creep, preset policy not code policy, claims optional + ledger-bound, `continue` is the default. This feature is orthogonal to usability (audit places it after Tier 0–1).

---

## 4. Cross-cutting requirements

These apply to *every* feature and double as design-review gates:

- **Parse-don't-validate** at every boundary; unparseable input is an error (e.g. strict JSON-RPC intake).
- **Strict, not liberal**, at interfaces (RFC 9413 / Postel critique).
- **Version the public surface** (`schema_version` / `protocolVersion`).
- **Honest, capability-gated naming** — not "audit" until hash-chained, not "conformant" until strict-parsed. This spec's maturity tags exist to keep that honesty.
- **Trust structural facts, not self-report** (Goodhart) — the un-gameable signal is the verifier gate.
- **No silent fallback / ROP throughout.**

## 5. Open items the implementer inherits

Ordered by the audit's tiering (`docs/reports/2026-06-23-production-readiness-gaps.md`):

1. **Tier 1 (first-run UX):** ✅ adapter auth preflight (G-F4 — done); ✅ `fake-demo` preset (G-F1 — done); ✅ `rally new` auto-spawn + typo-join fail-fast (G-F2 — done); ✅ honest install lead (README now leads with the verified curl / `go install` paths; `npx skills add` demoted to a caveated skills.sh alternative — done). **Tier 1 complete.**
2. **Tier 2 (make harness claims true):** G6 hook wiring (F13); wire Merkle (F12); implement `logx` redaction (F21); A2A SSE named events + `sessionId` (F16).
3. **Tier 3 (trust):** real-adapter integration tests + gate/autogoal coverage (see `test-plan.md`); Homebrew tap.
4. **Feature work:** cross-check ping-pong (F22); scratchpad wiring (F20); `strict_round_robin` / `last_writer_wins` routing (F6).
