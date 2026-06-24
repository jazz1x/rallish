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

## Next (Tier 2 — make the harness claims true)

Per handoff §work-plan, in order: G6 action-gate enforcement (F13, Opus for the
threat-model + hook contract), Merkle wiring (F12), `logx` redaction (F21, Opus
for the boundary), A2A SSE named events + `sessionId` (F16).
