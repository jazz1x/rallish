# rallish — Production-readiness gap audit (2026-06-23)

What it takes to bring rallish to an *actually-usable* level. Findings from a
5-agent parallel audit (happy-path, adapters, install/distribution, test
coverage, claims-vs-reality), with the highest-impact claims spot-verified by
hand against the code.

## TL;DR — there are two different "usable" bars

| Bar | What it means | Distance | Gating tier |
|-----|---------------|----------|-------------|
| **A. Install + run a real session** | A stranger installs rallish and runs `squash`/`rally` with the `claude` CLI | **Close** — core broker + claude adapter work | Tier 0–1 (~3–4 days) |
| **B. Trustworthy autonomous harness (G1–G6)** | The "safe/resumable/verifiable/auditable" harness the README + north-star advertise actually holds | **Far** — much is partial / unwired / declared-only | Tier 2–3 (~2–3 weeks) |

The core mechanics are real and race-clean (broker `-race -count=5` passed ×5,
all 25 test packages green). The gaps are **not** "the foundation is broken" —
they are "the advertised surface is wider than the wired surface," plus a few
concrete first-run blockers.

## Spot-verification of high-impact claims (CLAUDE.md discipline)

| Claim (source agent) | Verdict | Evidence |
|---|---|---|
| `install.sh` is a dangling symlink | **CONFIRMED** | `install.sh → internal/skills/rallish-operator/...`; dir is `internal/skills/rallish/` (renamed in `94433856`). Real installer: `internal/skills/rallish/scripts/install-binary.sh`. **Re-pointing the symlink is NOT enough**: `raw.githubusercontent.com` does not resolve symlinks — it returns the link's *target path as text* (verified via WebFetch), so `curl … \| sh` pipes a path string to `sh`. install.sh must be a real file. |
| `go install` blocked by unreleased Go 1.25 | **FALSE (agent error)** | `go version` → `go1.25.0` present; Go 1.25 released ~2025-08. `go install` works for anyone on Go ≥1.25. Downgraded BLOCKER → MINOR (normal version floor) |
| `--allow-shell-exit` is a dead no-op | **CONFIRMED — but mis-framed by the audit** | `AllowShellExit` set at `cmd/rallish/main.go:273`, stored at `internal/cli/squash.go:31`, **never read again**; broker hardcodes `exit.NewEvaluator(false)` at `internal/broker/broker.go:375`. NOT a "silent budget burn": the broker **intentionally** skips shell predicates and logs `exit_predicate_shell_skipped` (`broker.go:413`), because `runShellPredicate` sets no `cmd.Dir` (would run `go test` in the daemon CWD, not the session repo) and running shell from a global daemon is a posture choice. So the flag can never take effect via squash → it is a misleading dead flag, resolved by **removal** (not by wiring shell into the daemon). |
| `internal/logx` is a stub | **CONFIRMED** | only `doc.go` (2 lines); no redaction implementation |
| RFC 9162 Merkle proofs are unwired | **CONFIRMED** | `MerkleRoot`/`InclusionProof`/`VerifyConsistency` referenced only in `pkg/contract/merkle.go` + `merkle_test.go`; **zero production call sites** |
| Scratchpad is unwired (4th over-claim, found in verification) | **CONFIRMED** | `internal/scratch` is imported by **zero** production code (`rg 'jazz1x/rallish/internal/scratch'` over non-test `.go` → empty); `scratch.NewManager` is only self-referenced. Preset `scratch:` config parses into `contract.ScratchConfig` and `TurnRequest.ScratchPath` exists, but nothing populates or consumes them. The Manager + compaction are dead code. |
| A2A SSE emits only `data:` (no named events) | **CONFIRMED (A2A path)** | `internal/broker/a2a.go:299,329` + `rally.go:486` emit `data:` only; named `event:` lines exist only on the **MCP** path (`mcp.go:107,149`) |

## Tier 0 — Ship-blockers for "install + run" (LANDED in `fix/tier0-usability`)

After hand-verification, two of the four items were re-scoped (the audit
mis-framed #2 and #3). What landed in this PR:

1. **✅ `install.sh` is now a real file** (was a dangling symlink to the
   pre-rename `rallish-operator/` path). Re-pointing the symlink would *not*
   have worked — GitHub raw doesn't resolve symlinks (verified). install.sh is
   now a self-contained installer, an intentional mirror of the go:embed'd
   bundle copy (`go:embed` can't reach outside `internal/skills`, so the two
   cannot share one source — noted in both files). Unblocks `curl … | sh`.
2. **✅ Removed the dead `--allow-shell-exit` flag** (cmd + `SquashOptions`).
   Re-scoped from the audit's "wire it / burns budget": the broker
   **intentionally** skips shell predicates and logs `exit_predicate_shell_skipped`
   (`broker.go:413`); the flag could never take effect via squash. Wiring shell
   into the global daemon is a security-posture change (and needs a `cmd.Dir`
   fix) — deliberately **not** done here; left as a documented future decision.
3. **✅ Scratchpad over-claim corrected in docs** (not built). The audit's
   "deliver scratchpad to agents" is not a one-line fix: `internal/scratch` is
   imported by zero production code — the whole feature is unwired. Wiring it
   (manager per session + `ScratchPath` + compaction + adapter consumption) is
   Tier 1–2. For now the README Features row is marked *(planned)*.
4. **✅ Adapter over-claim trimmed** in README.md / .ko / .jp. `claude` and
   `kimi` adapters are real (the author's machine has a `kimi` binary, so kimi
   is *not* fake — the audit's "no public binary" was wrong). Only **Cursor**
   and **Codex** have zero adapter code; the intro now frames them as
   addable-via-the-2-method-port, not shipped. (handbook.md mentions deferred —
   they conflate skill-awareness with adapter support; separate follow-up.)

## Tier 1 — First-run UX (so a stranger succeeds, not just the author)

5. **Adapter auth preflight + better error.** An unauthenticated/rate-limited
   `claude` CLI yields the cryptic `parsing response: no JSON TurnResponse found
   in output` (`internal/adapter/claude/claude.go:79`). `doctor` reports ✓ on
   PATH presence only, not auth. Add a health/auth check and an actionable
   message; have `doctor` verify auth.
6. **Discoverable zero-credential smoke test.** A `fake` adapter exists and is
   registered, but no bundled preset uses `runtime: fake`, so a stranger must
   hand-write a YAML to verify the install. Ship a `fake-demo` preset (or
   `rallish squash --fake`).
7. **`rally` daemon auto-spawn + default join timeout.** `rally new` errors if
   no daemon is running (only `squash` auto-spawns), and `doctor`'s "will
   auto-spawn on `rally new`" message is misleading. `rally join` with a typo'd
   participant **hangs forever** (no default `--timeout`).
8. **Honest primary install path.** `npx skills add jazz1x/rallish` (README's
   lead install command) has **no registry backing** — no `package.json` /
   `skills.json` / manifest in the repo. Either register the skills.sh manifest
   or lead the README with a path that works today (the now-real `install.sh`
   curl path / `go install` / from-source). GitHub Releases themselves are healthy
   (v0.1.1–v0.3.0, cross-platform, cosign-signed, SBOM).

## Tier 2 — Make the autonomous-harness claims *true* (the differentiator)

9. **G6 action-gate enforcement.** Today it only *declares* policy (deny-list
   data in `pkg/contract/action_gate.go`); nothing enforces it. Without a
   user-wired PreToolUse hook, `rm -rf /` runs unblocked. Ship the hook wiring +
   a runbook, or downgrade the README claim to "policy declaration; enforcement
   requires hook X." **The README presents this as a live safety feature.**
10. **Wire the Merkle proofs** into a real path (e.g. a `rallish gate verify` /
    ledger-audit command) or stop tagging RFC 9162 inclusion/consistency proofs
    as ✅ built. The library is complete and tested but dead.
11. **Implement log-time secret redaction** (`internal/logx` is a 2-line stub)
    or remove the "secret redaction" log claim. (Redaction currently exists only
    in the pre-exec command classifier, not on log output.)
12. **A2A v1.0 SSE conformance.** Emit named `event:` type lines
    (`event: TaskStatusUpdateEvent`, …) on the A2A SSE path (`internal/broker/a2a.go`)
    and populate `A2ATask.sessionId`. A stock v1.0 client doing event-type
    discrimination fails today. (MCP path already emits named events — mirror it.)

## Tier 3 — Test the *real* path + close distribution (trust)

13. **Real-adapter integration test.** The real `claude`/`kimi` `Run()`,
    `adapter.BuildPrompt`, the gate `StandardPipeline` (`PreflightGate`,
    `CommitGate`, `PhilosophyGate`), and `autogoal.Run` are **all 0% covered** —
    the autonomous-cycle path is exercised end-to-end only through the `fake`
    stub that never touches a subprocess. Add at least one gated/optional
    real-subprocess integration test, plus coverage for the gate pipeline and
    `autogoal.Run` entry points. (claude adapter 40%, kimi 28.9%, gates 45.3%,
    doctor 24.6%.)
14. **Provision the Homebrew tap** (`jazz1x/homebrew-rallish` repo +
    `TAP_GITHUB_TOKEN`) — currently disabled in `.goreleaser.yaml:73–97`; it's
    the most common macOS install path.

## Out of scope for "usable" (noted, not blocking)

- **`docs/prd-cross-check-ping-pong.md`** (untracked working-tree draft) is a
  *quality* feature for `pair-review` (intent-aware handoff, dry-round breaker,
  claim oracle) — orthogonal to usability. Not on the critical path; pursue
  after Tier 0–1.
- The deferred broker refactors from the 2026-06-21 audit-followup (items 1/2/4,
  `joinRallySession` extraction) are internal hygiene, not user-facing.

## Effort

| Tier | Scope | Rough effort |
|------|-------|--------------|
| 0 | symlink, allow-shell-exit, scratchpath, docs trim | ~1 day |
| 1 | auth preflight, fake demo, rally auto-spawn/timeout, install path | ~2–3 days |
| 2 | action-gate enforcement, Merkle wiring, logx, A2A conformance | ~1–2 weeks |
| 3 | real-adapter + gate + autogoal tests, brew tap | ~3–5 days |

**Bottom line:** as an interactive multi-agent rally/squash tool driven by the
`claude` CLI, rallish is **~3–4 days (Tier 0–1)** from "a stranger can install
and run it." As the trustworthy autonomous *audit harness* it advertises, it is
**~2–3 more weeks (Tier 2–3)** — and the README should be trimmed to match the
wired surface in the meantime.
