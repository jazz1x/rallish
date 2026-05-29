# A2A Compatibility Map

This document maps rallish internals to the Linux-Foundation A2A Protocol v1.0
concepts. Conformance is **partial**: the v1.0 discovery path, `protocolVersion`,
PascalCase RPC method names, strict typed intake, the `A2APart` `kind`
discriminator field, and signed agent card (F16 — `SignAgentCard`/`VerifyAgentCard`)
are implemented; **mutual auth is the one remaining deferred gap** (see Limitations).

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
- **Signed agent card (F16):** `SignAgentCard`/`VerifyAgentCard` are implemented
  (`pkg/contract/agent_card_sign.go`, ed25519, stdlib only); the broker serves an
  unsigned card by default (no signing-key infra wired yet). Key management
  (rotation, CA, PKI) is deferred.
- **Mutual auth is NOT implemented and is deferred.** The A2A v1.0 spec specifies
  client-certificate / mutual-TLS authentication (F16 follow-on). rallish does not
  perform any mutual-auth handshake; it has no identity/PKI infrastructure. This is
  the **sole remaining gap to full v1.0 conformance**. Until mutual auth lands,
  secure deployment requires a local-only binding or a terminating reverse proxy
  that enforces mTLS externally.
- **`A2APart.Kind` (v1.0) vs legacy `type` (draft):** the discriminator field is
  now `kind` per the v1.0 spec. A client sending the legacy `type` field will
  receive a parse error under `DisallowUnknownFields` — intentional (strict, not
  liberal, at the interface).
