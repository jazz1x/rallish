# A2A Compatibility Map

This document maps rallish internals to the Linux-Foundation A2A Protocol v1.0
concepts. Conformance is **partial**: the v1.0 discovery path, `protocolVersion`,
PascalCase RPC method names, and strict typed intake are implemented; **signed
agent cards (F16) and mutual auth are deferred** (see Limitations).

## Endpoints

| A2A Concept | rallish Endpoint |
|-------------|------------------|
| Agent Card (v1.0) | `GET /.well-known/agent-card.json` |
| Agent Card (legacy alias) | `GET /.well-known/agent.json` |
| JSON-RPC 2.0 | `POST /a2a` |

The card carries `protocolVersion` (currently `"1.0"`) so a client can negotiate
the wire shape (versioned public surface).

## Data Model Mapping

| A2A Type | rallish Internal | Notes |
|----------|-----------------|-------|
| `Task` | `Session` | Session ID = Task ID |
| `TaskState` | `Session.Status` | See state table below |
| `Message` | `TurnRequest` / `TurnResponse` | One turn = one message |
| `Artifact` | `TurnResponse.Files` | File outputs attached to turn |
| `Part` | `TurnRequest.Parts` | Text, file, or tool-use parts |

## State Mapping

| A2A TaskState | rallish Session.Status |
|---------------|------------------------|
| `submitted` | New session, no turns yet |
| `working` | `active` |
| `input_required` | `blocked` |
| `completed` | `done` |
| `failed` | `error` |
| `canceled` | `stopped` |
| `rejected` | `blocked` (irrecoverable) |

## Methods

The v1.0 PascalCase method name is primary; the legacy lowercase name is kept as
a back-compat alias.

JSON-RPC params are decoded into typed structs with `DisallowUnknownFields`
(parse-don't-validate; RFC 9413 / Postel critique) — an unknown or extra field is
a JSON-RPC error (`-32602` for params, `-32700` for the envelope), never silently
dropped.

### `SendMessage` (legacy alias: `tasks/send`)

- Creates a new task or appends a message to an existing one.
- Blocks until the task reaches a terminal state.
- Returns the full `Task` object.

### `SubscribeToTask` (legacy alias: `tasks/sendSubscribe`)

- Same as `SendMessage` but streams `TaskStatusUpdateEvent` and `TaskArtifactUpdateEvent` via SSE.
- Each turn completion triggers a status update.

### `GetTask` (legacy alias: `tasks/get`)

- Returns the current `Task` snapshot without sending a new message.

### `CancelTask` (legacy alias: `tasks/cancel`)

- Sets the task state to `canceled`.
- Stops any pending SSE streams.

## SSE Events

| Event | When |
|-------|------|
| `TaskStatusUpdateEvent` | After each turn completes |
| `TaskArtifactUpdateEvent` | When a turn carries file outputs |

## Limitations

- No `tasks/resubscribe` (not needed; sessions are short-lived).
- No `tasks/pushNotifications` (no outbound push in v0).
- **Signed agent cards (F16) are deferred** — the card is served unsigned, so a
  client cannot yet verify card authenticity. This is why conformance is
  **partial**, not full.
- **Mutual auth is deferred**; use a local-only binding or reverse proxy.
