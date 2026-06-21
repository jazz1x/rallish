# MCP Compatibility Map

This document maps the rallish rally baton-passing state machine to the **Model
Context Protocol (MCP) 2025-03-26** server surface. The MCP surface is additive:
the existing HTTP/SSE rally endpoints and the `rallish rally` CLI continue to
work unchanged.

## Endpoints

| MCP Concept | rallish Endpoint |
|-------------|------------------|
| SSE transport | `GET /mcp/sse` |
| Client → server messages | `POST /mcp/message?session_id=<transport-session-id>` |

A client opens `GET /mcp/sse`, receives an `endpoint` event, then POSTs
JSON-RPC messages to the returned URL. Server responses arrive as `message`
events over the SSE stream.

## Protocol Version

The broker advertises protocol version `2025-03-26` in response to the MCP
`initialize` method.

## Capabilities

The server reports a minimal capability set:

```json
{
  "tools": {}
}
```

No resources, prompts, sampling, or logging capabilities are exposed in this
iteration.

## Tools

All rally operations are exposed as `rally_*` tools.

| Tool | Arguments | Result |
|------|-----------|--------|
| `rally_create` | `participants []string`, `repo?`, `task?`, `first_holder?` | `RallySession` |
| `rally_join` | `session_id string`, `as string`, `timeout_ms?` (default 30000) | `BatonEvent` or `{"timeout":true}` |
| `rally_done` | `session_id string`, `as string`, `note?`, `handoff_to?` | `RallySession` |
| `rally_status` | `session_id string` | `RallySession` |
| `rally_interrupt` | `session_id string` | `RallySession` (interrupted) |

Tool results use MCP `text` content containing a JSON payload. Tool execution
errors set `isError: true` and include the error message in the content text.

## Coexistence with HTTP Rally

- Sessions created via MCP are visible to HTTP/SSE clients and vice versa.
- A baton handoff from an MCP participant is delivered to an HTTP participant
  through the existing SSE channel, and vice versa.
- A participant may be connected through only one transport at a time; a second
  `rally_join` or HTTP baton request for the same name returns
  `ErrAlreadyConnected` until the first one cleans up.

## Limitations

- Authentication: deferred; MCP shares the same unauthenticated local daemon
  surface as HTTP and A2A today.
- No MCP resources or prompts; only tools are exposed.
- `rally_join` is a blocking long-poll tool. Clients that need push-style
  delivery can open a long-poll loop.
