# PRD: Unix Domain Socket IPC for rallish

## §1 Problem Definition

`internal/ipc/` contains only `doc.go`. The CLI (`start`, `doctor`, etc.) and daemon communicate via HTTP on `127.0.0.1` with a port file. There is no lighter-weight, file-system-based IPC path. A Unix domain socket would allow local-only, zero-port, permission-gated communication between CLI and daemon.

## §2 Decision & Rationale

**Selected:** Add Unix domain socket transport alongside existing HTTP. HTTP remains the default for A2A compatibility; Unix socket is used for CLI↔Daemon internal control plane.

**Why:** Zero learning cost for existing A2A clients. Unix socket adds no network surface and supports file-permission-based access control. Revisit if Windows named-pipe support is later required.

## §3 Alternatives (Rejected)

| Alt | Pros | Cons | Verdict |
|-----|------|------|---------|
| A. Replace HTTP with Unix socket | Simpler, no port file | Breaks A2A HTTP clients, remote adapters | Rejected |
| C. gRPC over Unix socket | Strong typing | New dependency, overkill for local CLI | Rejected |

## §4 Implementation Spec

### 4.1 Package `internal/ipc`

```
internal/ipc/
  doc.go          // exists
  socket.go       // Socket type, listener, dialer
  socket_test.go  // tests
```

### 4.2 Types

```go
// Socket manages a Unix domain socket path.
type Socket struct {
    Path string
}

// Listen returns a net.Listener bound to the Unix socket.
func (s *Socket) Listen() (net.Listener, error)

// Dial returns a net.Conn connected to the Unix socket.
func (s *Socket) Dial(ctx context.Context) (net.Conn, error)

// Remove cleans up the socket file if it exists.
func (s *Socket) Remove() error
```

### 4.3 Path Convention

- Socket path: `~/.rallish/rallish.sock`
- Port file remains: `~/.rallish/port` (backward compat)

### 4.4 Daemon Integration

- `cli.RunDaemon` attempts to listen on Unix socket first.
- Falls back to TCP (`127.0.0.1:0`) if Unix socket fails.
- On daemon shutdown, remove socket file.

### 4.5 CLI Integration

- `cli.RunStart` tries Unix socket dial before HTTP port-file lookup.
- If Unix socket connects, derive `brokerURL` as `http+unix://` or use custom `http.Transport` with `net.DialContext` to Unix socket.

### 4.6 Transport Helper

```go
func HTTPClientOverSocket(socketPath string) *http.Client
```
Returns an `http.Client` whose transport dials the given Unix socket for `http://rallish.local` requests.

## §5 Test Criteria

- `TestSocketListenDial`: create socket, listen, dial, echo round-trip.
- `TestSocketRemove`: verify idempotent cleanup.
- `TestHTTPClientOverSocket`: HTTP request over Unix socket succeeds.
- Race-free: `go test -race ./internal/ipc/...`

## §6 Guardrails

- No breaking changes to existing HTTP-only flow.
- No new external dependencies (stdlib only).
- Socket file must be removed on daemon exit (defer/cleanup).
- Path traversal guard: socket path must be under `~/.rallish/`.

## §7 Acceptance Criteria

1. `make check` passes (go vet + golangci-lint + go test -race).
2. `lefthook run pre-commit` passes.
3. `internal/ipc` has ≥70% test coverage.
4. Daemon starts with Unix socket; `rallish doctor` still works.
5. Socket file is removed when daemon exits.
