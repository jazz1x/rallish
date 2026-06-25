# Runbook: Cross-Check Ping-Pong

End-to-end verification for F22 — intent-aware handoff, dry-round breaker,
stuck detection, and verifiable claims.

## Prerequisites

- `rallish` built: `make build`
- A running daemon or let `squash` auto-spawn one.

## 1. Intent-aware handoff

Create a session with the `pair-review` preset and observe that a turn can
carry an explicit handoff intent.

```bash
# In one terminal, start the daemon (or skip — squash auto-spawns).
rallish daemon

# In another terminal, run a headless pair-review session.
rallish squash --preset pair-review --task "Refactor auth.go to use interfaces"
```

The executor can emit:

```json
{
  "done": false,
  "handoff_to": "reviewer",
  "handoff_intent": "cross_check",
  "summary": "extracted AuthService interface",
  "artifacts": ["auth.go"],
  "self_eval": "confident"
}
```

The next turn routed to `reviewer` will receive `LastTurn.Intent: "cross_check"`,
and its adapter prompt will include the cross-check framing.

Verify with a broker test:

```bash
go test -run 'TestBroker_IntentForwarding|TestBuildPrompt_CrossCheckFraming' ./internal/broker/ ./internal/adapter/
```

## 2. Dry-round breaker

The shipped `pair-review` preset sets `budget.dry_rounds_threshold: 3` and
`exit_when: [dry_rounds]`. If three consecutive turns produce no new artifact,
do not set `done`, and do not request a `handoff_to`, the session terminates
with reason `dry rounds`.

Verify:

```bash
go test -run TestBroker_DryRoundsExit ./internal/broker/
```

## 3. Stuck detection

A 6-turn alternating-role dry ping-pong terminates with a `ping-pong` reason
when `exit_when: [stuck]` is present.

Verify:

```bash
go test -run TestBroker_StuckPingPongExit ./internal/broker/
```

## 4. Verifiable claims

Inside an autonomous cycle, a claim with a reproducible check is verified by
`ClaimGate`.

```bash
# Create a cycle with a claim in ViolationsFound.
go test -run 'TestClaimGate_Verified|TestClaimGate_Falsified' ./internal/cycle/gates/
```

A passing claim emits `claim_verified`; a failing claim emits `claim_falsified`
and halts the cycle.

## 5. Full gate

Run the project’s CI gate locally:

```bash
make check-all
```

All tests, lint, and coverage floors must pass.
