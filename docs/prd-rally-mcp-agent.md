# PRD: `rallish rally mcp-agent` — MCP Client Subcommand

## §1 Problem Definition

The broker now exposes rally baton-passing as MCP 2025-03-26 tools, but there is no packaged client for agents. An agent (Kimi, Claude, etc.) that wants to participate in a rally via MCP must hand-roll SSE transport setup, JSON-RPC request/response matching, endpoint discovery, and tool dispatch. This is error-prone and prevents the `rallish-mcp` skill from being concise and reliable.

## §2 Decision & Rationale

**Selected:** Add a `rallish rally mcp-agent` CLI subcommand that acts as a one-shot MCP client. It opens an SSE transport, sends the requested `tools/call`, waits for the matching response, prints the raw JSON result, and exits. Bundle a `rallish-mcp` skill that instructs agents to use this subcommand instead of speaking raw MCP.

**Why:** Reusing the existing `rallish` CLI for daemon discovery and MCP plumbing keeps the skill thin and language-neutral. The agent only needs to parse JSON printed by a familiar subcommand. The subcommand can be tested with `httptest` and reused by humans for debugging.

## §3 Alternatives (Rejected)

| Alt | Pros | Cons | Verdict |
|-----|------|------|---------|
| A. Markdown-only skill with raw MCP instructions | No new CLI code | Agents frequently get SSE/JSON-RPC details wrong; hard to regression-test | Rejected |
| B. Separate helper binary/script | Could be Python/bash | Extra install path, version skew with the daemon, harder to discover | Rejected |
| C. Extend `rally join/done/status` with `--mcp` flag | Fewer commands | Confuses transport semantics; `join` already has SSE behavior | Rejected |

## §4 Implementation Spec

### 4.1 New files

```
internal/cli/rally_mcp_agent.go       // Cobra subcommand + MCP client logic
internal/cli/rally_mcp_agent_test.go // httptest-based table-driven tests
internal/skills/rallish-mcp/SKILL.md // agent skill (English)
internal/skills/rallish-mcp/SKILL.ko.md // agent skill (Korean)
docs/prd-rally-mcp-agent.md          // this document
```

### 4.2 Modified files

```
internal/cli/rally.go                 // add mcp-agent subcommand
internal/skills/skills.go             // add all:rallish-mcp to go:embed
internal/cli/bootstrap.go             // offer to install rallish-mcp skill
internal/cli/add.go                   // list skill automatically via ListEmbeddedSkills
docs/mcp-compatibility.md             // document the client subcommand
CHANGELOG.md / .ko.md / .jp.md        // record feature
AGENTS.md                             // update skill conventions if needed
```

### 4.3 CLI surface

```bash
rallish rally mcp-agent --mode create --participants alice,bob [--repo <path>] [--task <text>] [--first <name>]
rallish rally mcp-agent --mode join  --session-id <id> --as <name> [--timeout <duration>]
rallish rally mcp-agent --mode done  --session-id <id> --as <name> [--note <text>] [--handoff-to <name>]
rallish rally mcp-agent --mode status --session-id <id>
```

Flags:

| Flag | Mode | Default | Description |
|------|------|---------|-------------|
| `--mode` | all | (required) | `create`, `join`, `done`, `status` |
| `--participants` | create | (required) | Comma-separated participant names |
| `--repo` | create | "" | Repository path |
| `--task` | create | "" | Task description |
| `--first` | create | "" | Pre-assign baton holder |
| `--session-id` | join/done/status | (required) | Rally session ID |
| `--as` | join/done | (required) | Participant name |
| `--timeout` | join | 30s | Max wait for baton; 0 means block forever |
| `--note` | done | "" | Baton note |
| `--handoff-to` | done | "" | Explicit next holder |

Output: raw JSON printed to stdout, exactly as returned by the MCP tool result content. `join` timeout prints `{"timeout":true}`.
Exit codes: 0 on success, 1 on MCP/daemon error, 2 on join timeout (mirrors `rally join`).

### 4.4 MCP client behavior

1. Resolve daemon base URL via existing `resolveBrokerClient(homeDir, 30s)`.
2. `GET <base>/mcp/sse` with `Accept: text/event-stream`.
3. Read the `endpoint` event to obtain the POST URL including `session_id`.
4. Send `initialize` request and wait for the matching response.
5. Send the requested `tools/call` and wait for the matching response.
6. Extract the first `text` content item; print it as raw JSON.
7. Close SSE response body and exit.

Request IDs start at 1 and increment per message on a single transport. The client matches responses by ID so that concurrent tool calls on the same transport cannot be confused; for this one-shot subcommand there is only one in-flight call, but matching prevents accidental consumption of stale server events.

### 4.5 Error handling

- Daemon not running: propagate `resolveBrokerClient` error.
- SSE connection fails or no `endpoint` event: exit 1.
- JSON-RPC error response: exit 1, print error to stderr.
- Tool result with `isError: true`: exit 1, print tool error to stderr.
- Join timeout: exit 2, print `{"timeout":true}` to stdout.

### 4.6 Skill content

`internal/skills/rallish-mcp/SKILL.md` frontmatter:

```yaml
---
name: rallish-mcp
version: 0.1.0
description: >
  Participate in a rallish rally via the MCP 2025-03-26 server surface.
  Uses `rallish rally mcp-agent` under the hood.
ssl:
  logical:
    writes: ["~/.rallish/cycles/*"]
    idempotent: false
    rollback: "Interrupt the rally with `rally mcp-agent --mode interrupt`"
Triggers:
  - rally mcp
  - mcp rally
  - MCP로 랠리
  - 랠리 MCP
---
```

Body instructs the agent to:
1. Ensure daemon is running (`rallish doctor`; start with `rallish daemon &` if needed).
2. Create: `rallish rally mcp-agent --mode create --participants server,returner --first server --task "[pattern:cycle] ..."`.
3. Extract `id` from JSON output.
4. When it is the agent's turn: `rallish rally mcp-agent --mode join --session-id <id> --as <name>`.
5. Do work.
6. Hand off: `rallish rally mcp-agent --mode done --session-id <id> --as <name> --note "..."`.
7. Use `--mode status` to poll if needed.

## §5 Guardrails

- No broker changes; reuse existing `rally_*` MCP tools.
- No auth work.
- Each invocation opens a fresh SSE transport; no persistent session state in the CLI.
- The skill must not instruct the agent to parse raw SSE events or construct JSON-RPC envelopes.

## §6 Test Plan

### 6.1 Unit tests (`internal/cli/rally_mcp_agent_test.go`)

Use `httptest.Server` wrapping the broker `Server`.

- `TestMCPAgentCreate` — create returns valid `RallySession` JSON.
- `TestMCPAgentJoin` — join receives a `BatonEvent` after another participant calls done.
- `TestMCPAgentDone` — done advances holder and returns updated `RallySession`.
- `TestMCPAgentStatus` — status returns current session snapshot.
- `TestMCPAgentJoinTimeout` — short timeout prints `{"timeout":true}`.
- `TestMCPAgentCreateInvalidParticipants` — create with <2 participants exits non-zero.

### 6.2 Integration dogfood

1. `rallish daemon` in a temp HOME.
2. Create session: `rallish rally mcp-agent --mode create --participants alice,bob --first alice`.
3. In background: `rallish rally mcp-agent --mode join --session-id <id> --as bob --timeout 30s`.
4. Alice hands off: `rallish rally mcp-agent --mode done --session-id <id> --as alice --note "from mcp-agent"`.
5. Verify bob's join output contains `{"turn_n":2,"from":"alice","note":"from mcp-agent"}`.

### 6.3 Gate

- `make check-all` passes.

## §7 Acceptance Criteria

1. `rallish rally mcp-agent --mode create/join/done/status` works end-to-end against the daemon.
2. `rallish skill install --name rallish-mcp` installs the skill.
3. `make check-all` passes.
4. New tests cover all four modes and timeout.
5. Dogfood confirms a handoff between two `mcp-agent` invocations.
