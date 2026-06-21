# PRD: Rally MCP Server Surface

## §1 Problem Definition

`rallish rally` is usable from the CLI and from raw HTTP/SSE, but there is no standard protocol surface that lets external coding agents (Kimi, Claude, Codex, etc.) participate as rally participants through their native tool-calling channels. The recent hardening work (PR #22) made the baton lifecycle safe for concurrent SSE clients; the next step is to expose the same state machine through MCP so agents can ping-pong without relying on shelling out to `rallish rally join/done`.

## §2 Decision & Rationale

**Selected:** Add an MCP 2025-03-26 server surface to the existing broker that exposes rally operations as MCP tools. The existing HTTP/SSE rally endpoints and CLI commands remain untouched and fully functional.

**Why:** MCP is becoming the de-facto standard for agent-to-server tool invocation. Adding it as a thin layer on top of the already-separated `rallyStore` lets external agents join rallies with minimal new code, while keeping the human CLI path intact. Reusing the store guarantees that MCP participants, HTTP participants, and CLI participants can coexist in the same rally session.

## §3 Alternatives (Rejected)

| Alt | Pros | Cons | Verdict |
|-----|------|------|---------|
| A. A2A-only extension | Reuses existing A2A routes | A2A task model does not map cleanly to named participant baton turns; would require stretching the spec | Rejected |
| B. Replace HTTP rally with MCP | Single protocol to maintain | Breaks existing CLI, scripts, and skills that depend on `rallish rally` | Rejected |
| C. Custom gRPC/JSON surface | Full control | Yet another protocol; no ecosystem benefit | Rejected |

## §4 Implementation Spec

### 4.1 New files

```
pkg/contract/mcp.go              // MCP wire types + rally tool request/response structs
internal/broker/mcp.go           // SSE transport manager, JSON-RPC dispatch, rally tool handlers
internal/broker/mcp_test.go      // table-driven tests for tools, transport, and coexistence
docs/mcp-compatibility.md        // mapping spec and quickstart
```

### 4.2 Modified files

```
internal/broker/broker.go        // add s.registerMCPRoutes()
docs/a2a-compatibility.md        // note the separate MCP rally surface
CHANGELOG.md / .ko.md / .jp.md   // record feature
AGENTS.md                        // extend A2A/MCP protocol section
```

### 4.3 Protocol surface

MCP 2025-03-26 JSON-RPC over SSE:

```
GET  /mcp/sse                    → SSE transport; server emits JSON-RPC responses/notifications
POST /mcp/message?session_id=..  → client-to-server JSON-RPC requests
```

The SSE endpoint assigns a transport `session_id` (a random 16-char hex string, distinct from rally session IDs). The client must include this `session_id` when POSTing messages. The transport manager maps `session_id` → a send channel used to push JSON-RPC responses/notifications back to the SSE stream.

### 4.4 MCP tools

All tool names are prefixed with `rally_`.

```go
// rally_create
{
    "participants": ["kimi", "claude"],
    "repo": "/path/to/repo",
    "task": "[pattern:cycle] refactor auth",
    "first_holder": "kimi" // optional
}
// → contract.RallySession

// rally_join
{
    "session_id": "rly_1234567890123_abcd",
    "as": "kimi",
    "timeout_ms": 30000 // optional, default 30000, max 300000
}
// → contract.BatonEvent on baton arrival
// → {"timeout": true} if no baton before deadline

// rally_done
{
    "session_id": "rly_1234567890123_abcd",
    "as": "kimi",
    "note": "implemented login",
    "handoff_to": "claude" // optional
}
// → contract.RallySession

// rally_status
{
    "session_id": "rly_1234567890123_abcd"
}
// → contract.RallySession

// rally_interrupt
{
    "session_id": "rly_1234567890123_abcd"
}
// → contract.RallySession (interrupted)
```

### 4.5 State-machine reuse

All tool handlers delegate to the existing `rallyStore` in `internal/broker/rally.go`:

- `rally_create` calls the same creation logic as `handleRallyCreate`.
- `rally_done` calls the same handoff logic as `handleRallyDone`.
- `rally_status` calls the same snapshot logic as `handleRallyStatus`.
- `rally_interrupt` marks the session `interrupted`, sends `BatonEvent{Closed:true}` to all connected streams, and closes their channels (mirrors `CloseAllRallies` but for one session).
- `rally_join` registers a participant channel in `rallySession.streams[name]`, blocks until a baton event is delivered or the timeout fires, then removes the channel registration. If the session is already interrupted, returns an immediate close sentinel.

The same mutex (`rallies.mu`) protects all state; no new locks are introduced.

### 4.6 Error mapping

Typed errors from `pkg/contract/errors.go` map to JSON-RPC errors:

| Error | JSON-RPC code | HTTP status (for transport errors only) |
|-------|---------------|----------------------------------------|
| `ErrSessionNotFound` | -32602 (Invalid params) | 404 |
| `ErrNotHolder` | -32602 | 409 |
| `ErrSessionInterrupted` | -32602 | 409 |
| `ErrAlreadyConnected` | -32602 | 409 |
| `ErrInvalidName` / `ErrInvalidSessionID` / etc. | -32602 | 400 |
| Internal errors | -32603 | 500 |

### 4.7 Coexistence semantics

- A session created via MCP can be joined by HTTP/SSE participants and vice versa.
- A baton handoff from an MCP participant is delivered to an HTTP participant through the existing SSE channel, and vice versa.
- `rally_join` and the HTTP baton SSE both use the same `streams[name]` slot; therefore a participant cannot be connected through both transports simultaneously (`ErrAlreadyConnected`).
- Interrupt via MCP closes both MCP and HTTP streams for that session.

### 4.8 Heartbeat / timeout

- `rally_join` uses a server-side timer with the requested `timeout_ms`.
- SSE transport itself sends an SSE comment heartbeat every 15 s (same as HTTP rally SSE) to keep the TCP connection alive; this is independent of tool timeouts.
- If a client disconnects, the SSE handler cleans up the transport session; any in-flight `rally_join` for that transport is unblocked by the request context and removes its participant channel.

## §5 Guardrails

- No new adapter interface or in-process adapter changes.
- No persistence redesign.
- No auth/key-management beyond what A2A already has.
- Existing CLI commands and HTTP routes are read-only in this change.
- MCP tool names are namespaced with `rally_` to avoid future collisions.
- Transport `session_id` and rally `session_id` are distinct concepts and use different formats.

## §6 Test Plan

### 6.1 Unit tests (`internal/broker/mcp_test.go`)

- `TestMCPInitialize` — `initialize` request returns protocol version and capabilities.
- `TestMCPToolsList` — `tools/list` returns the five rally tools.
- `TestMCPCreateRally` — `rally_create` returns a valid `RallySession`.
- `TestMCPJoinAndDonePingPong` — two MCP participants pass the baton back and forth.
- `TestMCPJoinTimeout` — `rally_join` with short timeout returns `{"timeout":true}` and cleans up.
- `TestMCPJoinInterruptedSession` — joining an interrupted session returns close sentinel.
- `TestMCPDoneNotHolder` — non-holder `rally_done` returns `ErrNotHolder`.
- `TestMCPInterruptSession` — `rally_interrupt` closes active join calls.
- `TestMCPCoexistWithHTTPRally` — HTTP SSE participant and MCP participant exchange batons.
- `TestMCPTransportSessionRequired` — POST without `session_id` returns error.

### 6.2 Integration dogfood

1. `rallish daemon` in a temp HOME.
2. Create rally via `rallish rally new`.
3. Join as `alice` via `rallish rally join` (HTTP).
4. Join as `bob` via a small MCP client script.
5. `alice` calls `rally done`; verify `bob` receives baton through MCP.
6. `bob` calls `rally_done` via MCP; verify `alice` receives baton through HTTP SSE.

### 6.3 Gate

- `make check-all` passes.
- Existing rally tests in `internal/broker/rally*_test.go` and `internal/cli/rally*_test.go` still pass.

## §7 Acceptance Criteria

1. `GET /mcp/sse` establishes an SSE transport and assigns a session ID.
2. `POST /mcp/message?session_id=..` accepts JSON-RPC tool calls and returns responses over SSE.
3. All five `rally_*` tools work end-to-end against the daemon.
4. HTTP/SSE rally endpoints and CLI commands continue to pass existing tests.
5. New MCP tests cover happy path, timeout, interrupt, and cross-transport coexistence.
6. `make check-all` passes.
7. `docs/mcp-compatibility.md` is present and accurate.

## §8 Open Questions / Future Work

- Authentication: deferred to a follow-up; MCP shares the same unauthenticated local daemon surface as HTTP and A2A today.
- Resource templates: could expose rally session state as an MCP resource in the future, but tools are sufficient for ping-pong.
- Sampling/completion: not needed for baton passing; agents use their own LLM outside the rally broker.
