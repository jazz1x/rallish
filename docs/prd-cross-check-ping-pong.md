# PRD: Cross-Check Ping-Pong Efficiency

## §1 Problem Definition

`rallish squash --preset pair-review` (and similar multi-role presets) currently pass the same `Summary` carryover to every next turn, regardless of whether the next role should **continue** the work or **cross-check** it.

This hurts cross-check efficiency:

- The reviewer receives the executor’s self-summary, which carries the executor’s framing.
- Cross-check requires **decorrelation**: the reviewer should inspect artifacts and the original task with an adversarial stance, not the creator’s narrative.
- Without intent-aware handoff, the ping-pong collapses into an echo chamber.
- Without a dry-round / stuck-breaker policy, the loop either stops too early (false convergence) or spins forever (token leak).
- Without verifiable claims and oracle anchoring, review findings are self-reports that cannot be re-checked.

## §2 Decision & Rationale

**Selected:** Add intent-aware handoff and a guarded termination policy to the preset session path (`rallish squash`), while keeping the broker a **carrier**, not a judge.

| Pillar | What we add | What we refuse |
|---|---|---|
| P0′ intent-aware carryover | `HandoffIntent` (`continue` / `cross_check`) on `TurnResponse` → broker forwards → adapter selects prompt framing | Broker does not interpret summary content or decide “is this good enough” |
| P1′ loop-until-dry + stuck-breaker | Preset-declared `dry_rounds_threshold` and `exit_when: [dry_rounds]`; broker counts dry rounds and runs a pure `SessionStuck` helper over `TurnRecord`s | Broker does not encode workflow-specific ping-pong logic; predicates are pure functions over records |
| P2′ verifiable discovery | Optional `Claims []Violation` on `TurnResponse` with a reproducible `Check` field; broker appends claim events to the ledger | Broker does not verify claims; a gate/oracle does |
| P3′ external oracle anchor | `ClaimGate` runs `Check.Command` and compares output to `Check.Expected`; emits `claim_verified` / `claim_falsified` ledger events | Oracle is claim-scoped, not a generic “trust this agent” verdict |

**Why this design:**
- It respects the existing boundary: broker carries turns and state; adapters run models; presets declare policy; gates/oracles verify claims.
- It reuses existing surfaces (`Summary`, `Artifacts`, `TurnRecord`, ledger) instead of inventing a parallel workflow graph.
- It is bounded: a preset can opt in, but `solo-ralph` and simple rallies are unaffected.

## §3 Alternatives (Rejected)

| Alt | Pros | Cons | Verdict |
|---|---|---|---|
| A. Add `verified` field to baton schema | Simple boolean | Invites broker to become a referee; no real consumer branches on it today; YAGNI | Rejected |
| B. Hard-code cross-check framing in `pair-review.yaml` only | No broker changes | Cannot express continue-vs-cross-check transitions; every role gets the same prompt | Rejected |
| C. Full workflow graph engine in broker | Powerful | Becomes LangGraph; violates rallish identity | Rejected |
| D. Put dry/stuck logic entirely in adapter | Adapters decide when to stop | Adapters are runtime-specific; duplicated policy across claude/kimi/fake | Rejected |

## §4 Implementation Spec

### 4.1 New / modified files

```
pkg/contract/types.go              # HandoffIntent, TurnResponse/LastTurn fields
pkg/contract/cycle.go              # Violation.Check optional field
pkg/contract/harness_ledger.go     # claim_* ledger event types

internal/broker/broker.go          # forward intent, count dry rounds, evaluate ExitDryRounds
internal/broker/broker_test.go     # round-trip and dry-round tests

internal/exit/exit.go              # ExitDryRounds evaluator
internal/session/stuck.go          # new pure helpers DryRounds / SessionStuck

internal/adapter/prompt.go         # intent-aware prompt framing
internal/adapter/adapter.go        # no logic change; carry Claims if present

internal/cycle/gates/claim.go      # ClaimGate (oracle verifier)
internal/cycle/gates/pipeline.go   # include ClaimGate if cycle carries claims

internal/preset/preset.go          # parse dry_rounds_threshold
internal/preset/presets/pair-review.yaml  # opt-in config

docs/prd-cross-check-ping-pong.md  # this document
docs/runbook-cross-check-ping-pong.md     # end-to-end verification
docs/runbook-pair-review.md        # update with intent examples

CHANGELOG.md / .ko.md / .jp.md     # record feature
```

### 4.2 Contract changes

```go
// pkg/contract/types.go

type HandoffIntent string

const (
    HandoffIntentContinue   HandoffIntent = "continue"
    HandoffIntentCrossCheck HandoffIntent = "cross_check"
)

type TurnResponse struct {
    // ... existing fields ...
    HandoffIntent HandoffIntent `json:"handoff_intent,omitempty"`
    Claims        []Violation   `json:"claims,omitempty"`   // P2′
}

type LastTurn struct {
    // ... existing fields ...
    Intent HandoffIntent `json:"intent,omitempty"`
    Claims []Violation   `json:"claims,omitempty"`
}
```

```go
// pkg/contract/cycle.go

type ViolationCheck struct {
    Command  string `json:"command,omitempty"`  // reproducible shell check
    Expected string `json:"expected,omitempty"` // expected substring in combined output
}

type Violation struct {
    File    string          `json:"file,omitempty"`
    Line    int             `json:"line,omitempty"`
    Type    string          `json:"type"`
    Message string          `json:"message"`
    Check   *ViolationCheck `json:"check,omitempty"` // new
}
```

```go
// pkg/contract/harness_ledger.go

const (
    // ... existing types ...
    LedgerEventClaimRegistered LedgerEventType = "claim_registered"
    LedgerEventClaimVerified   LedgerEventType = "claim_verified"
    LedgerEventClaimFalsified  LedgerEventType = "claim_falsified"
)
```

### 4.3 P0′ — Intent-aware carryover

**Flow:**

1. Adapter emits `TurnResponse{HandoffIntent: "cross_check", Artifacts: [...]}`.
2. Broker stores `HandoffIntent` in `state.lastTurn.Intent` (neutral pass-through).
3. Broker builds next `TurnRequest` with `LastTurn{Intent: ..., Artifacts: ..., Summary: ...}`.
4. Adapter prompt builder reads `Intent` and appends framing:
   - `continue`: “Continue the previous turn. Use Summary as working state; Artifacts are context.”
   - `cross_check`: “Critically review the previous turn. Read Artifacts and the original Task fresh. Do not accept the Summary at face value; look for mistakes, gaps, and contradictions.”

**Who sets intent:**
- Default is `continue`.
- A role can declare it via `TurnResponse.HandoffIntent`.
- A preset routing rule can hint it (e.g., `executor → reviewer` defaults to `cross_check`).
- Optional CLI flag: `rallish squash --default-intent cross_check`.

**Broker scope:** broker forwards intent only. It does not read `Summary` or `Artifacts` to decide intent.

### 4.4 P1′ — Loop-until-dry + stuck-breaker

**Dry round definition for a session:**

A turn is dry when:
- It introduces no new artifacts (`len(newArtifacts) == 0`), **and**
- It does not set `Done=true`, **and**
- It does not change routing (`HandoffTo` empty).

The broker maintains `sessionState.dryRounds` and `sessionState.seenArtifacts`. After each `handlePostTurn`:

```go
if dry {
    state.dryRounds++
} else {
    state.dryRounds = 0
}
```

**Exit condition:**

Add `ExitDryRounds` to `pkg/contract/types.go` and evaluate it in `internal/exit/exit.go` using `state.DryRounds` and preset `Budget.DryRoundsThreshold`.

**Stuck helper:**

New pure functions in `internal/session/stuck.go`:

```go
func DryRounds(records []TurnRecord, threshold int) bool
func SessionStuck(records []TurnRecord) (reason string, ok bool)
```

`SessionStuck` detects:
- Ping-pong: alternating A,B,A,B with no new artifacts for ≥6 turns.
- No progress: last K turns introduce no new artifact and no green signal.
- Repeated fingerprint: same `(Summary, Artifacts)` ≥4 times.

It operates on `[]session.TurnRecord`, reusing the pattern of `internal/cycle/stuck.go` but without coupling to cycle ledger types.

**Preset opt-in:**

```yaml
# internal/preset/presets/pair-review.yaml
budget:
  max_turns: 20
  dry_rounds_threshold: 3
exit_when:
  - tests_pass
  - reviewer_approved
  - turns_exhausted
  - dry_rounds
```

### 4.5 P2′ — Verifiable discovery

**Claim registration:**

A reviewer (or any role) emits:

```go
TurnResponse{
    Claims: []Violation{
        {
            File: "internal/broker/rally.go",
            Line: 330,
            Type: "rop",
            Message: "SSE close-sentinel write error is silently ignored",
            Check: &ViolationCheck{
                Command:  "grep -n 'writeCloseSentinel' internal/broker/rally.go",
                Expected: "writeCloseSentinel",
            },
        },
    },
}
```

**Broker action:** broker appends `claim_registered` ledger entries (one per claim). It does not verify them.

**Cycle integration:** When the session is part of an autonomous cycle, `Claims` are also folded into `CycleState.ViolationsFound` so the next cycle agent sees them.

### 4.6 P3′ — External oracle anchor

**ClaimGate:**

```go
// internal/cycle/gates/claim.go
func NewClaimGate(claims []contract.Violation) Gate {
    return &claimGate{claims: claims}
}
```

For each claim with `Check != nil`:
- Run `Check.Command` with a timeout.
- Compare combined output against `Check.Expected` (substring match).
- Emit `claim_verified` or `claim_falsified` to the ledger.
- Return `GateFailure` if any claim is falsified.

**Pipeline placement:** After `audit` and before `polish`, so claimed defects are re-checked before the cycle declares green.

**Session path:** `exit_when: [tests_pass]` remains the coarse oracle. `ClaimGate` is used only inside the cycle harness where claims are tracked.

### 4.7 Error handling

- Unknown `HandoffIntent` values are treated as `continue` (forward-compatible).
- Claims with malformed `Check` are logged as warnings and skipped, not fail-fast.
- `ClaimGate` command failures are treated as `claim_falsified` (conservative: an unreproducible claim is not verified).
- Dry-round/stuck termination sets `terminalReason` so the caller can distinguish “done” from “stuck”.

### 4.8 Backward compatibility

- `HandoffIntent`, `Claims`, and `Violation.Check` are all `omitempty`.
- Existing presets without `dry_rounds_threshold` behave exactly as before.
- Existing adapters that do not emit intent default to `continue`.

## §5 Test Plan

- `pkg/contract`: round-trip JSON for `TurnResponse` with intent and claims.
- `internal/broker/broker_test.go`:
  - Intent from response appears in next `TurnRequest`.
  - Three consecutive dry turns trigger `terminal=true` with reason `dry_rounds`.
  - Six-turn ping-pong triggers `terminal=true` with reason `stuck`.
- `internal/session/stuck_test.go`: table-driven `DryRounds` and `SessionStuck` predicates.
- `internal/adapter/prompt_test.go`: prompt text differs for `continue` vs `cross_check`.
- `internal/cycle/gates/claim_test.go`: `ClaimGate` verifies/falsifies claims and emits ledger events.
- `internal/preset/preset_test.go`: parse `dry_rounds_threshold` from YAML.

## §6 Guardrails

1. **No broker judgment.** Broker forwards intent, counts dry rounds, and evaluates preset-declared exit conditions. It does not evaluate quality, truth, or verification state.
2. **No LangGraph creep.** We do not add a generic workflow graph, node/edge types, or conditional routing based on intent. Routing stays in `internal/router` and remains role-based.
3. **Preset policy, not code policy.** Dry/stuck thresholds live in preset YAML, not hard-coded in broker.
4. **Claims are optional and ledger-bound.** Claims do not bloat the baton schema beyond one optional slice; durable evidence lives in the ledger.
5. **Continue is the default.** Any adapter or preset that does not opt in continues to work unchanged.

## §7 Acceptance Criteria

- [ ] `rallish squash --preset pair-review` produces a `cross_check` intent on executor→reviewer handoff.
- [ ] Reviewer prompt does not include the executor’s `Summary` as trusted state; it references `Artifacts` and original `Task`.
- [ ] A 3-dry-round run exits with `terminalReason=dry_rounds`.
- [ ] A 6-turn ping-pong with no new artifacts exits with `terminalReason=stuck`.
- [ ] A reviewer claim with a passing `Check` emits `claim_verified`; a failing `Check` emits `claim_falsified` and halts the cycle.
- [ ] `make check-all` passes.

## §8 Documentation

- `docs/runbook-cross-check-ping-pong.md`: end-to-end walkthrough with `pair-review` preset.
- `docs/runbook-pair-review.md`: update with intent and claim examples.
- `CHANGELOG.md`, `CHANGELOG.ko.md`, `CHANGELOG.jp.md`: record under `[Unreleased]`.
