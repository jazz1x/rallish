# Runbook: Unix Domain Socket IPC — Manual Verification

> **Branch:** `feat/ipc-unix-socket`  
> **Commit:** `42e21ad`  
> **Prerequisites:** Go 1.25+, macOS/Linux, `~/.rallish/` directory writable

---

## 0. One-shot validation

Copy-paste the entire block. It builds, starts the daemon, runs all checks, sends SIGTERM, and asserts clean exit.

```bash
cd /path/to/rallish
make build
rm -rf ~/.rallish

# Start daemon in subshell so $! points to the real process
(
  cd "$(pwd)"
  ./dist/rallish daemon &
  DAEMON_PID=$!
  sleep 2

  # --- checks ---
  echo "=== 1. Unix socket exists ==="
  ls -la ~/.rallish/rallish.sock

  echo "=== 2. Socket path file exists ==="
  cat ~/.rallish/socket

  echo "=== 3. Port file exists ==="
  cat ~/.rallish/port

  echo "=== 4. A2A discovery over Unix socket ==="
  SOCKET_PATH=$(cat ~/.rallish/socket)
  curl --unix-socket "$SOCKET_PATH" -s http://rallish.local/.well-known/agent.json | head -c 200
  echo

  echo "=== 5. A2A discovery over TCP ==="
  PORT=$(cat ~/.rallish/port)
  curl -s "http://127.0.0.1:${PORT}/.well-known/agent.json" | head -c 200
  echo

  # --- graceful shutdown ---
  echo "=== 6. SIGTERM graceful shutdown ==="
  kill -TERM "$DAEMON_PID"
  wait "$DAEMON_PID"
  EXIT_CODE=$?
  echo "Exit code: $EXIT_CODE"

  echo "=== 7. Artifact cleanup ==="
  ls ~/.rallish/rallish.sock 2>/dev/null && echo "FAIL: socket still present" || echo "OK: socket removed"
  ls ~/.rallish/socket    2>/dev/null && echo "FAIL: socket file still present" || echo "OK: socket file removed"
  ls ~/.rallish/port      2>/dev/null && echo "FAIL: port file still present" || echo "OK: port file removed"

  if [ "$EXIT_CODE" -eq 0 ]; then
    echo "✅ ALL PASSED"
  else
    echo "❌ FAILED (exit code $EXIT_CODE)"
    exit 1
  fi
)
```

**Expected output:**
```
=== 1. Unix socket exists ===
srwxr-xr-x  ...  /Users/.../.rallish/rallish.sock
=== 2. Socket path file exists ===
/Users/.../.rallish/rallish.sock
=== 3. Port file exists ===
62345
=== 4. A2A discovery over Unix socket ===
{"name":"rallish", ... }
=== 5. A2A discovery over TCP ===
{"name":"rallish", ... }
=== 6. SIGTERM graceful shutdown ===
Exit code: 0
=== 7. Artifact cleanup ===
OK: socket removed
OK: socket file removed
OK: port file removed
✅ ALL PASSED
```

---

## 1. Build

```bash
cd /path/to/rallish
make build
```

**Verify:** `dist/rallish` exists and is recent.

```bash
ls -la dist/rallish
```

---

## 2. Start the daemon

```bash
rm -rf ~/.rallish
./dist/rallish daemon &
DAEMON_PID=$!
sleep 2
```

**Verify:** log lines show both listeners.

```
time=... level=INFO msg="daemon listening" addr=127.0.0.1:XXXXX
time=... level=INFO msg="daemon listening on unix socket" path=/Users/.../.rallish/rallish.sock
```

---

## 3. Verify Unix socket file

```bash
ls -la ~/.rallish/rallish.sock
```

**Expected:** `srwxr-xr-x` (socket file type `s`).

---

## 4. Verify socket path file

```bash
cat ~/.rallish/socket
```

**Expected:** absolute path to `rallish.sock`.

```
/Users/<you>/.rallish/rallish.sock
```

---

## 5. Verify port file (backward compat)

```bash
cat ~/.rallish/port
```

**Expected:** ephemeral port number (e.g., `62345`).

---

## 6. A2A discovery over Unix socket

```bash
SOCKET_PATH=$(cat ~/.rallish/socket)
curl --unix-socket "$SOCKET_PATH" -s http://rallish.local/.well-known/agent.json
```

**Expected:** JSON response with `name: "rallish"`.

---

## 7. A2A discovery over TCP

```bash
PORT=$(cat ~/.rallish/port)
curl -s "http://127.0.0.1:${PORT}/.well-known/agent.json"
```

**Expected:** identical JSON response as step 6.

---

## 8. SIGTERM graceful shutdown

> ⚠️ **Critical:** Do **not** use `cd /path && ./dist/rallish daemon &` — bash `$!` captures the subshell PID, not the daemon PID. Use the two-line form above or run inside a `(...)` subshell where `$!` is captured immediately after `./dist/rallish daemon &`.

```bash
kill -TERM "$DAEMON_PID"
wait "$DAEMON_PID"
echo "Exit code: $?"
```

**Expected:** `Exit code: 0`

---

## 9. Verify artifact cleanup

```bash
ls ~/.rallish/rallish.sock 2>/dev/null && echo "FAIL" || echo "OK: socket removed"
ls ~/.rallish/socket       2>/dev/null && echo "FAIL" || echo "OK: socket file removed"
ls ~/.rallish/port         2>/dev/null && echo "FAIL" || echo "OK: port file removed"
```

**Expected:** all three `OK` lines.

---

## 10. `rallish start` integration

With the daemon running (restart if needed):

```bash
./dist/rallish start --task "say hello"
```

**Expected:** CLI discovers the daemon via `~/.rallish/socket`, creates a session, and exits cleanly.

---

## 11. Regression: `make check`

```bash
make check
```

**Expected:**
- `go vet ./...` → 0 issues
- `golangci-lint run` → 0 issues
- `go test ./... -race` → all green

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `Exit: 143` + socket left behind | `$!` captured subshell PID, not daemon PID | Use `cd ...` then `./dist/rallish daemon &` on separate lines, or run inside `(...)` |
| `curl: (7) Couldn't connect` | Daemon not started or socket path wrong | Check `~/.rallish/socket` content and daemon logs |
| `golangci-lint` failures | v1 config schema | This repo uses v2; do not downgrade |
| Port file missing | TCP listener failed | Check daemon logs for `listen: ...` error |

---

## Reference

- PRD: [`docs/prd-ipc-unix-socket.md`](./prd-ipc-unix-socket.md)
- A2A types: [`pkg/contract/a2a.go`](../pkg/contract/a2a.go)
- IPC package: [`internal/ipc/`](../internal/ipc/)
- Daemon: [`internal/cli/daemon.go`](../internal/cli/daemon.go)
- CLI start: [`internal/cli/start.go`](../internal/cli/start.go)
