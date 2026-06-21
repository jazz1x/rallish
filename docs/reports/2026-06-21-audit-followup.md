# Structural-debt audit — follow-up verdict & backlog (2026-06-21)

Follow-up to the structural-debt audit. Each item was **verified against the
actual code** before any action (the audit table was treated as a hypothesis,
not a work order). This records what landed, what was rejected, and what is
deliberately deferred with a recommended approach.

## Landed this session

| PR | Item | Change | Risk |
|----|------|--------|------|
| [#26](https://github.com/jazz1x/rallish/pull/26) | 6 (DRY) | `internal/skills`: extract `writeFileIfChanged` — `InstallNamed`/`InstallCompanionFiles` shared the write-if-changed block | low |
| [#27](https://github.com/jazz1x/rallish/pull/27) | 5 (DRY) | `internal/cycle/gates`: extract `runShellGate` — `CommandGate`/`AuditGate`/`LocalAuditGate` shared the exec→fold→GateResult tail | low |
| [#28](https://github.com/jazz1x/rallish/pull/28) | 7 (ROP) | `internal/cycle/autogoal`: surface toolchain failures instead of false `HaltSuccess` (see below — this was a real bug) | low, +tests |
| [#29](https://github.com/jazz1x/rallish/pull/29) | 4 + 8 | `internal/broker`: extract `writeSSEHeaders` + `streamBatonEvents` from `handleRallyBaton` (175→106 lines); name `sseHeartbeatInterval` / `sseWriteDeadline` | low (post-lock only) |

All four merged green (20-check CI: build matrix, tidy, vet, lint, test,
arch-guard, gitleaks, trivy, govulncheck). Each was verified locally with
`build`/`vet`/`lint`/`go test -race` before commit; the broker SSE change was
run `-race -count=3` because the loop is concurrency-sensitive.

## #28 was a real defect, not just style

`discoverNextGoalWithRunner` discarded every exec error with `out, _ :=`. When
`go vet` / `golangci-lint` / `git` **all fail to run** (missing toolchain,
timeout) it returned `("", nil)` — and `driver.go` reads an empty goal as
`HaltSuccess` ("no work left, stop"). A broken environment was reported as a
clean, completed codebase and the autonomous loop halted "successfully".

Fix: distinguish `*exec.ExitError` (the tool ran and exited non-zero — the
expected "issues found" signal → parse output) from a cancelled/expired context
or missing binary (the tool could not run → surface it). Discovery now errors
only when **every** strategy fails to run, so genuine "no work" still halts
success while a broken toolchain surfaces the error the driver already
propagates.

## Rejected — item 3 is invalid

> 🔴 `internal/cycle` depends on `internal/adapter`, `internal/budget` → Clean Architecture

**This contradicts the documented, test-enforced architecture.** Acting on it
would break the build.

- `internal/arch/import_guard_test.go` package doc: *"internal/cycle is
  deliberately excluded because it legitimately imports internal/adapter (the
  reference driver coupling); that coupling is documented as a reference driver
  concern, not a core contract concern."*
- `internal/arch/layer_guard_test.go` encodes the layering
  `cli → broker → {cycle, adapter, session} → contract` and asserts it on every
  CI run. `cycle → adapter` is an **inward** (allowed) edge; only the SSOT leaf
  `pkg/contract` is forbidden from importing any `internal/` package.

The `cycle → adapter`/`budget` dependency is an intentional reference-driver
coupling, not debt. No change. If the coupling is ever to be inverted it must go
through `docs/north-star.md` and the arch guards first — not a refactor PR.

## Item 6 / 8 — scoped to what actually repeats

The repeated SSE timing literals (`15s` heartbeat ×2 across rally+mcp, `2s`
write-deadline ×3) became named constants in #29. The other ~25 `*time.X`
literals in `internal/broker`/`internal/cli` are **local, single-use, and
self-documenting** — flag defaults (`"step-timeout", 10*time.Minute`), one-off
dial/connect timeouts, config-field defaults. Hoisting them to shared constants
would *reduce* readability (a distant `connectTimeout` is worse than an inline
literal at its sole use site). Centralising all of them was explicitly **not**
done; that would be over-engineering, not cleanup.

## Deferred — items 1, 2, and the rest of 4 (recommended follow-ups)

These are real but share one risky surface: the **locked rally-session
registration** in `internal/broker`, recently hardened against adversarial races
([#21](https://github.com/jazz1x/rallish/pull/21)–[#23](https://github.com/jazz1x/rallish/pull/23)).
They were left untouched on purpose — rushing them autonomously risks
reintroducing the exact races those PRs fixed. Recommended as deliberate,
separately-reviewed work.

### Item 2 — extract a transport-agnostic `joinRallySession` (DRY, highest value)

The "reject duplicate join → register `chan BatonEvent` → compute the immediate
baton event under `rallies.mu`" logic is duplicated:

- `rally.go` `handleRallyBaton` (the locked section, ~lines 294–376)
- `mcp.go` `mcpToolRallyJoin` (~lines 484–510)

**Approach:** a `joinRallySession(id, as) (ch chan contract.BatonEvent, immediate *contract.BatonEvent, err error)` that owns the full lock/unlock window and the duplicate-join + idle-baton logic. HTTP then streams via `streamBatonEvents`; MCP blocks for one event. **Must** preserve the exact lock boundary and be validated with the existing adversarial-race tests under `-race -count` ≥ 5.

### Item 1 — inject `rallies` store into `Server` (SRP / testability)

`var rallies = newRallyStore()` is a process-global (`rally.go`). The header
comment notes it is kept separate from `Server.mu` *to avoid nesting locks* — so
this is a considered decision, not an accident. Injecting it touches ~20 free
functions (`createRallySession`, `advanceRallyBaton`, `getRallySession`, …) that
would become methods or take the store as a parameter.

**Approach:** make the store a `Server` field and convert the free functions to
methods in one mechanical pass; keep `rallies.mu` semantics identical. Best done
**after** item 2 (fewer call sites to thread once the join logic is unified).
Pure structure — gate on `-race` and the full broker suite.

### Item 4 (remainder) — the locked registration section

#29 extracted only the post-lock SSE I/O. The locked
register-stream-and-compute-immediate-event block is the same code item 2 needs
to share; fold it in there rather than as a separate extraction.

## Other audit 🟡 items already addressed

`InstallNamed`/`InstallCompanionFiles` dedup (#26), gate command boilerplate
(#27), and `autogoal.go` command-failure handling (#28) are done. The
`handleRallyBaton` God-function (#4) is materially reduced (#29) with the
remainder folded into item 2 above.
