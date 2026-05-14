# A2A Compatibility Map

This document maps rallish internals to the Google A2A Protocol v1.0 concepts.

## Endpoints

| A2A Concept | rallish Endpoint |
|-------------|------------------|
| Agent Card | `GET /.well-known/agent.json` |
| JSON-RPC 2.0 | `POST /a2a` |

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

### `tasks/send`

- Creates a new task or appends a message to an existing one.
- Blocks until the task reaches a terminal state.
- Returns the full `Task` object.

### `tasks/sendSubscribe`

- Same as `tasks/send` but streams `TaskStatusUpdateEvent` and `TaskArtifactUpdateEvent` via SSE.
- Each turn completion triggers a status update.

### `tasks/get`

- Returns the current `Task` snapshot without sending a new message.

### `tasks/cancel`

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
- Authentication is not implemented in v0; use a local-only binding or reverse proxy.
