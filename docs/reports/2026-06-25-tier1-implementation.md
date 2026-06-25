# rallish — Tier 1 implementation log (2026-06-25)

Autonomous implementation against `docs/handoff-implementation.md`, branch
`fix/tier0-usability`. Each item followed the per-item loop: build → test →
decorrelated Sonnet review → docs/maturity flip → commit.

## Tier 1 — first-run UX: COMPLETE (4/4)

| Item | Feature | Commit | Verification |
|------|---------|--------|--------------|
| 1 | Adapter auth preflight + `doctor --probe` | `ffc4d33` | shared `DiagnoseOutput` classifier; stub-binary Run tests; reviewer-driven marker tightening |
| 2 | Bundled `fake-demo` preset | `968bebd` | ran `squash --preset fake-demo` end-to-end in an isolated HOME, no agent CLI — completed + ledger |
| 3 | `rally new` daemon auto-spawn (`ensureBrokerClient`) | `ce9cba2` | cold `rally new` spawned daemon + created session; typo `--as` → broker 403, no hang |
| 4 | Honest install lead (curl/go over npx) | `0ba6fd2` | confirmed v0.3.0 assets match install.sh; skills.sh resolves but serves stale `rallish-operator` |

Maturity flips landed in the same commits: AC-F4.2/G-F4, AC-F1.3/G-F1,
G-F2.1/G-F2.2, F18 → ✅ (feature-spec en+ko); user-manual + README (en/ko/jp)
kept true.

## Golden findings (discovered during implementation)

1. **Daemon cold-start double-spawn race** (pre-existing, shared with `squash`,
   NOT introduced by F2). Two concurrent cold `rally new`/`squash` within the
   ~5 s readiness window can both pass `dialExistingDaemon`'s single-instance
   check (`internal/cli/daemon.go:82`) before either binds, then both run: the
   TCP listener uses `127.0.0.1:0` (ephemeral, no conflict) and the unix path
   does `us.Remove()` then `us.Listen()`, so the second clobbers the first and
   the port file is overwritten by whoever writes last. Correct fix belongs in
   the daemon's single-instance guard (file lock or fixed-port bind), NOT a
   defensive lock in `ensureBrokerClient`. Backlog, low real-world likelihood.

2. **skills.sh serves a stale skill name.** `npx skills add jazz1x/rallish`
   resolves, but the indexed skill is `rallish-operator` (the pre-rename name),
   which the repo can no longer refresh. Either re-publish/claim on skills.sh or
   keep leading with the repo-controlled curl/go paths (done).

## Tier 2 — make the harness claims true: 3/4 DONE

| Item | Feature | Commit | Verification |
|------|---------|--------|--------------|
| 5 | G6 PreToolUse hook enforcement | `0e86dca` | hook script dogfooded: `rm -rf /`→deny, `git reset --hard`→ask, `ls`→proceed; shellcheck-clean |
| 6 | `cycle verify` wires RFC 9162 Merkle | `be3d989` | unit tests over a real chain: intact+proofs pass, tamper→exit 15 |
| 7 | `logx` log-time secret redaction | `6f14144` | slog middleware; positive + error-attr + false-positive-guard tests |
| 8 | A2A SSE named events + `sessionId` (F16) | — | **NOT STARTED** |

## Next — resume here

**Immediate: F16 (A2A SSE conformance)** — `internal/broker/a2a.go` has 3 `data:`-only
SSE emit sites (lines ~299, 329, 341). Add named `event:` lines (`TaskStatusUpdateEvent`,
or `TaskArtifactUpdateEvent` when `Artifact != nil`) mirroring the MCP path's
`writeSSEMessage` (`internal/broker/mcp.go:149`, `event: message\ndata: …`). Also
populate `A2ATask.sessionId` in `mapSessionToA2ATask` (`a2a.go:396` — set
`SessionID` = the session id). Flip F16 ◑→✅ + AC-F16.3 in feature-spec en+ko.

**Then Tier 3 + feature work** (handoff §work-plan items 9–14): real-adapter
integration test + gate/autogoal coverage (test-plan §6); CI coverage-floor; Homebrew
tap; `lastHash` O(n) perf fix + scaling benchmarks (performance-spec §4.2/§7);
cross-check ping-pong F22 (PRD); scratchpad wiring F20 + `strict_round_robin`/
`last_writer_wins` routing F6.

## How to resume in a new session

1. Branch `fix/tier0-usability`, working tree clean; 8 commits land Tier 1 (F4/F1/F2/
   F-install) + Tier 2 (F13/F12/F21) on top of `0e2a720`.
2. Build env: Go is Homebrew-only — `export PATH="/opt/homebrew/bin:$PATH:$(go env GOPATH)/bin"`
   before any `go`/`golangci-lint`. Gate: `bash scripts/check-all.sh`.
3. Per-item loop (handoff §): read feature-spec entry → build (Sonnet) → test →
   decorrelated review → flip maturity tag (en+ko) + manual in the SAME change → commit → next.
4. Spec SSOT = `docs/feature-spec.md` (maturity tags reflect reality as of this session).
   Locale rule: every spec/manual/README change mirrors into `.ko` (and README `.jp`).
