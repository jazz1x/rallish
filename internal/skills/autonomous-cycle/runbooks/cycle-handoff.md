# Cycle Handoff Runbook

## Quick Start

```bash
# Start an overnight cycle
rallish cycle start \
  --goal "feat: continue autonomous-cycle hardening" \
  --agents claude,kimi \
  --local-gate "make check-all" \
  --log-file tmp/nightly.log

# Check status anytime
rallish cycle status --cycle-id <id>

# Watch live events
rallish cycle watch --cycle-id <id> --log-file tmp/watch.log
```

## Morning Review Checklist

1. **Check halt reason**
   ```bash
   cat ~/.rallish/cycles/cycle-*.json | jq '.halted, .halt_reason, .completed_cycles'
   ```

2. **Review commits**
   ```bash
   git log --oneline -10
   ```

3. **Check violations**
   ```bash
   cat ~/.rallish/cycles/cycle-*.json | jq '.violations_found'
   ```

4. **Resume or restart**
   - Resume: `rallish cycle next --cycle-id <id> --goal "feat: resume"`
   - Fresh:  `rallish trigger "자율 사이클"`

## State Schema

`~/.rallish/cycles/cycle-<id>.json` fields:
- `id`: unique cycle identifier
- `phase`: current phase (`preflight`, `audit`, `philosophy`, `polish`, `commit`, `handoff`, `halted`)
- `completed_cycles`: finished cycle count
- `max_cycles`: safety limit (0 = unlimited)
- `max_duration_minutes`: time limit
- `branch`: git branch being worked on
- `baseline_sha`: commit SHA to diff against
- `pending_files`: files still needing work
- `local_gates`: repository-specific validation commands run after the built-in audit gate
- `halted`: true if stopped
- `halt_reason`: why it stopped
- `last_failed_gate`: gate that caused failure
- `violations_found`: accumulated issues
- `auto_goal`: whether automatic goal discovery is enabled
- `history`: ordered list of gate reports from completed cycles
- `started_at`: Unix ms timestamp when cycle began

## Agent Rotation

Every 3 completed cycles (`completed_cycles % 3 == 0`), the skill signals a **fresh-agent reset**.
Handoff data (keep under 2 KB):
- `cycle_id`, `completed_cycles`, `next_cycle_goal`
- `violations_found` (if any)
- Last 3 commit SHAs from `history`

## Halt Reasons

| Reason | Meaning | Action |
|--------|---------|--------|
| `success` | No more issues found | Review commits, merge PR |
| `self-audit-violation` | Philosophy / quality gate failed | Check `violations_found`, fix, resume |
| `gate-failure` | A gate failed | Check `last_failed_gate`, fix, resume |
| `max-cycles-reached` | Cycle limit hit | Review, restart with higher limit if needed |
| `max-duration-reached` | Time limit hit | Review, restart if more work needed |
| `user-requested` | Human halted | Review current state |
| `ssh-auth-failed` | Git push/auth failed | Fix SSH key / auth, resume |
| `preflight-failed` | Preflight checks failed | Fix branch / dirty state, restart |

## Recovery

- **Corrupt JSON**: Delete `~/.rallish/cycles/cycle-<id>.json`, restart with `rallish trigger "자율 사이클"`
- **Daemon not running**: `rallish daemon` (or script auto-starts it)
- **Lost cycle ID**: `ls ~/.rallish/cycles/cycle-*.json` or `git log --grep="autonomous-cycle"`
