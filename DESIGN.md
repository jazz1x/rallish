# hocketty — Design

> *"Two agents, one melody."*
> A local broker that lets multiple coding-agent CLIs (Claude Code, Kimi Code, …) hocket — alternate turns — on a single task.

---

## 1. What this is, in one paragraph

`hocketty` is a small **local broker process** (Bun + Hono) that sits between N "agent runtimes" — each of which is an off-the-shelf coding CLI like `claude`, `kimi`, `codex`, etc. The broker owns the conversation state, decides whose turn it is, and shuttles compact turn payloads between them. Each agent runtime keeps using its own sub-agent / tool ecosystem internally; hocketty only routes turns at the outer boundary.

The wire format follows the **A2A (Agent2Agent) protocol** where reasonable, so any A2A-compliant agent can be plugged in via an adapter.

---

## 2. Non-goals

- Not a new agent framework. We do not call LLM APIs directly.
- Not a tmux multiplexer. claude-squad / Crystal already do that; we are about **turn-level dialogue**, not parallel sessions.
- Not a UI. CLI + log files + (later) a tiny status TUI. Web dashboard is a stretch goal.
- Not a sandbox. Agents run with whatever permissions the user gives the CLI they wrap.

---

## 3. Architecture

```
┌──────────────────────────────────────────────────┐
│  hocketty broker (Bun, single process, 127.0.0.1)│
│                                                  │
│  POST  /sessions               start session     │
│  GET   /sessions/:id/next?as=X SSE next-turn     │
│  POST  /sessions/:id/turn      submit response   │
│  GET   /sessions/:id           snapshot          │
│  POST  /sessions/:id/stop      end + reason      │
│                                                  │
│  internals:                                      │
│    SessionStore  → jsonl append-only log         │
│    Router        → decides next role/runtime     │
│    Budgeter      → tokens / turns / wallclock    │
│    ExitEvaluator → fires when exit_when matches  │
└────────▲────────────────────────────▲────────────┘
         │                            │
   ┌─────┴──────┐               ┌─────┴──────┐
   │ adapter:   │               │ adapter:   │
   │   claude   │               │   kimi     │
   │ (claude -p │               │ (kimi -p   │
   │  --output  │               │  …)        │
   │   stream-  │               │            │
   │   json)    │               │            │
   └────────────┘               └────────────┘
   each adapter is a thin process that loops:
     1. GET /next  (long-poll via SSE)
     2. exec the agent CLI with prompt = turn payload
     3. parse stdout → TurnResponse
     4. POST /turn
```

---

## 4. Contract (the only thing other projects need to agree on)

### TurnRequest (broker → adapter)

```ts
type TurnRequest = {
  session: string;
  turn: number;                        // monotonically increasing
  role: "planner" | "executor" | "reviewer" | string;
  runtime_hint: "claude" | "kimi" | string;
  model_hint?: "opus" | "sonnet" | "haiku" | "kimi-k2" | string;
  budget: {
    tokens_left: number;
    turns_left: number;
    deadline_ms?: number;
  };
  shared_scratch_path: string;         // absolute path the agent may read/write
  last_turn?: {                        // null on first turn
    from: string;                      // role name
    runtime: string;
    summary: string;                   // ≤ ~400 tokens, agent-authored
    artifacts: string[];               // file paths touched
    self_eval: "confident" | "uncertain" | "blocked";
  };
  task: {
    title: string;
    body: string;                      // free-form, original task spec
    repo_root: string;
  };
  exit_when: ExitCondition[];          // see §6
};
```

### TurnResponse (adapter → broker)

```ts
type TurnResponse = {
  done: boolean;                       // true ⇒ session may end (subject to ExitEvaluator)
  handoff_to?: string;                 // role name; null ⇒ Router decides
  summary: string;                     // ≤ ~400 tokens; this is what the next turn sees
  artifacts: string[];                 // file paths created/modified
  self_eval: "confident" | "uncertain" | "blocked";
  notes_for_human?: string;            // surfaced in CLI status, not passed to next agent
};
```

**Why the summary field is mandatory:** the next turn does NOT receive the previous agent's full conversation. It receives only `summary` + the shared scratch file. This is the single biggest token-optimization lever.

### Mapping to A2A

- A2A `Message` ≈ TurnRequest / TurnResponse
- A2A `Task` ≈ Session
- A2A `Artifact` ≈ artifacts[] (file paths, not blobs — we are local)

We start with our own JSON shape, expose A2A endpoint as a compatibility layer in Phase 5.

---

## 5. Session state on disk

```
<repo_root>/.hocketty/
  config.yaml                          # which preset to use in this repo
  sessions/
    <session-id>/
      log.jsonl                        # append-only: every TurnRequest + TurnResponse
      scratch.md                       # rolling shared scratchpad, agent-authored
      meta.json                        # status, start time, exit reason
      artifacts/                       # optional: diffs/snapshots per turn
```

`log.jsonl` is the source of truth. Broker death → on restart, re-derive in-memory state from the log.

---

## 6. Router & ExitEvaluator

### Router

Picks the next role for the next turn. Order of precedence:

1. If previous TurnResponse has `handoff_to` → use it.
2. Else apply preset's `routing` rule (default: round-robin across `roles[]`).
3. If a role is `blocked`, escalate to `reviewer` if defined, else pause session.

### ExitConditions

```yaml
exit_when:
  - all_artifacts_compile        # run `pnpm typecheck` or repo-defined cmd
  - tests_pass                   # run `pnpm test`
  - reviewer_approved            # last reviewer turn had self_eval=confident && done=true
  - turns_exhausted              # budget.turns_left == 0
  - tokens_exhausted
  - human_signal                 # `hocketty stop <id>`
```

Each condition is a small predicate over session state + (optionally) a shell command.

---

## 7. Preset format

```yaml
# .hocketty/config.yaml or ~/.hocketty/presets/<name>.yaml
name: pair-review
description: Claude plans, Kimi executes, Claude reviews.

roles:
  - id: planner
    runtime: claude
    model: opus
  - id: executor
    runtime: kimi
    model: kimi-k2
  - id: reviewer
    runtime: claude
    model: sonnet

routing: handoff_then_round_robin   # | strict_round_robin | last_writer_wins

budget:
  max_turns: 20
  max_tokens: 400000                # soft cap; broker tracks via adapter-reported usage
  deadline_minutes: 60

exit_when:
  - tests_pass
  - reviewer_approved
  - turns_exhausted

scratch:
  max_kb: 64                        # rolling window
  summarize_with: claude-haiku      # who compacts scratch when it overflows
```

Shipped presets (target):

- `pair-review` — planner / executor / reviewer
- `solo-ralph` — one runtime, broker just runs the ralph loop with budget/exit
- `triad` — three peers, group-chat style routing
- `dueling-executors` — same task to 2 runtimes in parallel, reviewer picks winner

---

## 8. Token-optimization rules (the non-negotiables)

1. **Next turn never receives the previous full transcript.** Only `summary` + scratch diff.
2. **Scratch.md is rolling**: when it exceeds `scratch.max_kb`, broker invokes `scratch.summarize_with` agent to compact the oldest half into a 1-paragraph summary block.
3. **Haiku-tier compaction interludes**: presets may insert a `compactor` role every K turns whose only job is to rewrite scratch tighter. Costs are cheap; saves expensive turns.
4. **Adapters MUST report token usage** in `TurnResponse.meta.tokens_in / tokens_out` (extend the type when implementing). Broker shows running totals.
5. **No "be helpful" prompt bloat from broker.** TurnRequest is data, not prose.

---

## 9. Adapters

Each adapter is its own small program. Interface:

```ts
interface Adapter {
  name: string;                        // "claude" | "kimi" | ...
  run(turn: TurnRequest): Promise<TurnResponse>;
}
```

### claude adapter (initial)

- Invokes: `claude -p <prompt> --output-format=stream-json --max-turns=1`
- Prompt = a deterministic template that embeds TurnRequest as a fenced JSON block + a brief instruction to reply with a fenced JSON TurnResponse.
- Parses last fenced JSON from stdout.

### kimi adapter (initial)

- TODO: confirm `kimi` (or `kimicode`) headless CLI flags. Adapter follows the same pattern: prompt-in, JSON-out.

### Pluggable

Adapters live in `src/adapters/<name>.ts` and are loaded by name from preset. Adding a new runtime = one file.

---

## 10. CLI surface

```
hocketty start [--preset NAME] [--task FILE|"inline task"] [--session-id ID]
hocketty attach <session-id>           # follow live SSE stream in terminal
hocketty status [<session-id>]         # show all running / recent sessions
hocketty stop <session-id> [--reason]
hocketty log <session-id> [--turn N]   # pretty-print log.jsonl
hocketty presets                       # list shipped + user presets
hocketty doctor                        # check adapters: claude in PATH? kimi?
```

Daemon mode (Phase 4): `hocketty daemon` runs broker in background; CLI commands talk to it over Unix socket at `~/.hocketty/sock`.

---

## 11. Phased delivery

Each phase is a runnable cut. Don't bundle.

### Phase 0 — Draft (this doc + skeleton)
- [x] DESIGN.md (this file)
- [ ] Bun + TS skeleton: `package.json`, `tsconfig.json`, `src/`, `bin/hocketty`
- [ ] Type definitions: `src/contract.ts` (TurnRequest, TurnResponse, Session, Preset)
- [ ] Zod validators for Preset YAML

### Phase 1 — Broker MVP
- [ ] In-memory SessionStore with jsonl append (`src/store.ts`)
- [ ] Hono routes: `POST /sessions`, `GET /sessions/:id/next` (SSE), `POST /sessions/:id/turn`, `GET /sessions/:id`
- [ ] Round-robin Router
- [ ] Tests: fake adapter ping-pongs 3 turns, log.jsonl correct.

### Phase 2 — Adapters
- [ ] `src/adapters/claude.ts` — wraps `claude -p` headless
- [ ] `src/adapters/kimi.ts` — wraps `kimi` headless (confirm flags first)
- [ ] `src/adapters/fake.ts` — for tests
- [ ] `hocketty doctor` checks each adapter binary exists

### Phase 3 — Loop runner & presets
- [ ] `hocketty start` boots broker (if not running) + spawns adapter processes per role
- [ ] Preset loader (`src/preset.ts`)
- [ ] ExitEvaluator with at least: `turns_exhausted`, `tests_pass`, `reviewer_approved`
- [ ] Ship presets: `pair-review`, `solo-ralph`

### Phase 4 — Packaging & reuse
- [ ] `bun build --compile` single binary → `dist/hocketty`
- [ ] Install script → `~/.hocketty/bin`, `~/.hocketty/presets/`
- [ ] `hocketty init` drops `.hocketty/config.yaml` in any repo
- [ ] Dogfood: run hocketty in a *different* repo successfully

### Phase 5 — Autonomy & polish
- [ ] Scratch rolling-window compaction (haiku role)
- [ ] Token meter with budget enforcement
- [ ] Watchdog: blocked/no-response → escalate role
- [ ] A2A-compatible endpoint (`POST /a2a/...`) as adapter on top of internal contract
- [ ] Optional TUI status (`hocketty status --watch`)

---

## 12. Open questions for Phase 0

1. **Kimi headless flags** — does `kimi` ship a `--print` / `-p` non-interactive mode and a stable stdout format? If not, adapter writes to a tempfile and reads result file.
2. **Single-binary distribution on macOS** — `bun build --compile` produces ~80MB binaries. Acceptable for v0; revisit later.
3. **Where the broker binds** — default `127.0.0.1:7777` (configurable). Unix socket later.
4. **How adapters auth to broker** — for v0, none (loopback only). For v1, a session token written to `~/.hocketty/sock.token`.

---

## 13. Definitely-not-doing (v0)

- Web UI
- Multi-host distribution
- Encryption / multi-tenant auth
- Persistent SQLite (jsonl is enough)
- Built-in LLM calls
- Anything that requires touching the user's git history without explicit instruction

---

## 14. Naming reference (for adapters / presets)

- The product: **hocketty**
- The binary: `hocketty` (alias: `htty`)
- The protocol vibe: "a hocket" = one alternating exchange between two voices
- A "session" is one task, however many turns
- A "turn" is one (request, response) pair

Anything new follows this vocabulary. No `agent.ts` / `manager.ts` / `executor.ts` boilerplate names — use the domain terms.
