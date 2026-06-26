# rallish — Implementation Handoff

> Read this first before implementing in Claude Code. It is the entry point that ties the specs together and states *how* to execute — not *what* to build (that lives in the linked specs).
> **Version:** tracks `VERSION` (0.3.0) · **Last updated:** 2026-06-24 · [한국어](./implementation-handoff.ko.md)

## 1. What you've been handed

A complete spec set. The intent is that implementation is now **execution, not design** — every decision a builder needs is already written down. Read in this order:

| Read | Document | Answers |
|------|----------|---------|
| 1 | `feature-spec.md` | What is **wired** vs **declared-only** vs **planned** — the truth table. Start here so you never "implement" something that already exists. |
| 2 | `docs/reports/2026-06-23-production-readiness-gaps.md` | The tiered gap list (Tier 0–3) — the **work queue**, already prioritized. |
| 3 | `test-plan.md` | The Definition of Done and the exact tests each feature must have. |
| 4 | `performance-spec.md` | Performance targets + the benchmark suite + the known `lastHash` O(n) risk. |
| 5 | `user-manual.md` | The behavior contract from the user's side — your acceptance lens. |
| 6 | `prd-cross-check-ping-pong.md` | The one **planned** feature (F22), fully specced. |
| — | `north-star.md` | The *why* and the moat. Consult when a decision affects vendor-neutrality, audit, or gates. |

**Sequencing** is already decided — do not re-plan it. Follow `feature-spec.md` §5 ("Open items the implementer inherits"), which mirrors the audit tiers: Tier 1 first-run UX → Tier 2 make-the-harness-claims-true → Tier 3 trust → feature work.

## 2. Execution guidance — use Sonnet 적재적소 (the right tool in the right place)

This work parallelizes well. Spend the cheaper, faster model (Sonnet) aggressively on the wide, well-scoped parts, and reserve deeper reasoning for the few places a wrong move ripples outward.

**Deploy Sonnet aggressively — in parallel — for:**

- **Search & exploration fan-out** — mapping call sites, finding every place a symbol is used, confirming a claim against code. Spin up several read-only Sonnet explorers at once.
- **Writing the tests in `test-plan.md` §5** — they are already specified bullet-by-bullet; table-driven test generation is exactly Sonnet's lane.
- **Mechanical / bulk edits** — renames, doc/code-comment sync, flag-help wording, the benchmark scaffolding in `performance-spec.md` §4.2.
- **Adversarial cross-check review** — run *decorrelated* Sonnet reviewers that read the code fresh and try to **falsify** a change, not confirm it (this is exactly how this spec set was hardened). This is the cross-check ping-pong (F22) applied to your own work.
- **Translation / mirroring** — keeping the `.md` and `.ko.md` pairs in sync.

**Reserve deeper, single-threaded reasoning (a careful pass, or a stronger model) for:**

- **Contract changes in `pkg/contract`** — it is the SSOT; a wrong shape ripples through every consumer. Change it once, deliberately, with a `schema_version` bump.
- **The gate pipeline and anti-spin invariants** (`internal/cycle/gates`, `stuck.go`) — the differentiator; subtle correctness, easy to silently weaken.
- **The G6 action-gate enforcement boundary** — rallish *declares + records*, the hook *enforces*. Crossing that line by accident makes rallish the executor (a north-star violation).
- **A2A v1.0 / MCP wire conformance** — strict parsing and named SSE events; conformance is binary.
- Anything touching the **moat**: vendor-neutrality, tamper-evident audit, un-gameable gates.

**Always:** let the cheapest oracle decide correctness — run `make check-all` (the same gate the autonomous cycle runs), not a re-read. Trust the green gate, not a self-summary.

## 3. The efficiency principle — never do the same work twice (or three times)

> **최고의 효율은 같은 일을 두 번, 세 번 하지 않는 것.** The highest efficiency is not repeating work.

This is not a slogan; it is how rallish itself is designed, and it must govern the implementation. Concretely:

1. **Single source of truth — change a fact in exactly one place.** The contract lives in `pkg/contract`; the gate order lives once in `StandardPipeline`; a config default lives once in `internal/config`. Never fork a fact across files — if you find yourself editing the "same" thing twice, you've found a missing SSOT.
2. **Reuse existing surfaces; do not invent parallel ones.** This is the cross-check PRD's own guardrail (`Summary` / `Artifacts` / `TurnRecord` / the ledger are reused instead of a new workflow graph). Before adding a field, a file, or a type, check whether an existing surface already carries it.
3. **Cache, don't re-scan — the `lastHash` lesson.** `performance-spec.md` §7 flags that the ledger append re-reads the whole file every turn (O(n) → O(n²)). The fix and the mindset are the same: remember the tail instead of recomputing it. Apply this everywhere — recomputing what you already know is doing the work twice.
4. **Review once, decorrelated — then trust the gate.** Run the adversarial cross-check a single time with reviewers who haven't seen your narrative, fix what they falsify, and move on. Re-reviewing confirmed-green work is repeating the work.
5. **Parallelize independent work; serialize only real dependencies.** Independent tasks (e.g. the Tier-1 items) run concurrently; only chain steps that truly depend on each other's output.
6. **Honest, capability-gated naming so nothing gets re-investigated.** The moment a feature flips ○/◑ → ✅, update its maturity tag in `feature-spec.md` in the *same* change. A stale tag forces the next person to re-audit what you already finished — the most expensive kind of repeated work.

## 4. Autonomous delivery loop — commit → PR → CI-green merge

Drive each unit of work all the way to a merged, green PR **autonomously** — no human gate, only a green one. The repo already encodes every rule below; follow it, don't reinvent it.

**1. Branch.** Work on a feature branch, never on `main`/`master` (the cycle Preflight gate and `CONTRIBUTING.md` both enforce this; a commit on `main` is rejected).

**2. Semantic-unit commits (의미 단위 커밋).** One logical change per commit — not one giant commit, not a commit per file. Each commit should be independently reviewable and leave the tree green.
- **Conventional-commits prefix is mandatory** and enforced by the `commit-msg` lefthook hook: `feat: fix: refactor: docs: test: chore: sec: ci: build: perf: style:`. A subject without one is rejected.
- **Never `--amend`, never `--no-verify`** — same rule as the cycle's Commit gate. The hooks are the point; do not bypass them.
- A semantic commit carries its own docs: update `CHANGELOG.md` (and the `.ko`/`.jp` parity files per `CONTRIBUTING.md`) and flip the relevant `feature-spec` maturity tag **in the same commit**, so the change is self-contained and never re-audited.

**3. Local gate before push — the single oracle.** Run `make check-all` (gofmt + `go vet` + `-race` tests + `golangci-lint` + no-raw-ANSI sweep). It **mirrors CI exactly**, so a green `make check-all` is your prediction of a green CI. Don't hand-verify what it already checks — that is doing the work twice (§3).

**4. Open the PR against `main`.** Fill the `.github/PULL_REQUEST_TEMPLATE.md` checklist (`go mod tidy` clean, `go vet`, `go test ./...`, `golangci-lint`). State type-of-change and how you tested.

**5. Drive CI to green, then merge — autonomously.** The `CI` workflow (`.github/workflows/ci.yml`) runs `tidy` (`go mod tidy` + `git diff --exit-code`), `vet`, `lint`, and the test/race jobs on every push and PR.
- **Never merge red.** If a job fails, read its log, **fix-forward** with another semantic commit, and let CI re-run.
- A common trip-wire is the `tidy` job: run `go mod tidy` and commit `go.mod`/`go.sum` if they changed, or `git diff --exit-code` fails.
- Once **all** CI jobs are green and the Definition of Done (§5) is met, merge. No human approval step is required for green — but a red or unmet-DoD PR must **never** be merged. This is the same discipline as the harness: the verifier-produced green signal is the only thing you trust, and the worker cannot fake it.

This delivery loop is the outer wrapper around `cycle run --once`: the cycle's Commit gate already produces atomic, hook-respecting commits; this section adds push → PR → green-merge around it so a unit of work lands without supervision.

## 5. Definition of done

A change is done when it meets `test-plan.md` §8: tests exist (with `-race` for concurrency), touched-package coverage holds the §4 floor, `make check-all` passes, contract changes carry round-trip tests and a `schema_version` bump, any feature that moved to ✅ has its `feature-spec.md` maturity tag updated **and** its corresponding gap test flipped from pending to green, and — per §4 — the change is landed as semantic-unit commits on a green, merged PR.
