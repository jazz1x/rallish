---
name: rallish-mcp
description: >
  rallish의 MCP 2025-03-26 서버 표면을 통해 rally에 참여합니다.
  이 스킬은 날것의 SSE 전송이나 JSON-RPC envelope을 직접 다루지 않고
  `rallish rally mcp-agent` 서브커맨드를 사용합니다. 기존 `rallish` 스킬
  (HTTP/SSE rally)과 동일한 daemon을 공유합니다.
  트리거: "rally mcp", "mcp rally", "MCP로 랠리", "랠리 MCP"
version: 0.1.0
ssl:
  scheduling:
    anti_triggers:
      - "브로커 MCP 표면 수정 — docs/prd-rally-mcp.md, internal/broker/mcp.go 참조"
      - "일반 Go 코딩 컨벤션 — AGENTS.md 참조"
  structural:
    scenes: [DaemonCheck, Create, Join, Work, Done, Status]
    resumable: true
    branches:
      - "DaemonCheck: daemon 미실행 시 `rallish daemon &`로 백그라운드 실행"
      - "Create: `rally mcp-agent --mode create`의 JSON stdout에서 session id 추출"
      - "Join: `rally mcp-agent --mode join`은 baton 도착 또는 timeout까지 블록; exit 2는 timeout"
      - "Done: holder 불일치 시 tool 에러; 실제 holder를 보고하고 양보"
      - "Status: 블록하지 않을 때 `--mode status`로 폴링"
  logical:
    tools: [Bash, Read]
    side_effects:
      reads: ["~/.rallish/socket", "~/.rallish/port"]
      writes:
        - "~/.rallish/rallish.sock (mode 0600, daemon 소유)"
      network: []
    idempotent: false
    rollback: "`rallish rally mcp-agent --mode interrupt --session-id <SID>`로 rally 중단"
---

# rallish-mcp — MCP를 통한 Rally

이 스킬은 daemon의 MCP 2025-03-26 표면을 통해 rallish rally에 참여합니다.
에이전트는 `rallish rally mcp-agent`만 호출하면 되며, SSE 전송, JSON-RPC,
tool dispatch는 서브커맨드가 날部 처리합니다.

## `rallish` 바이너리 찾기

모든 명령 전에 실행 가능한 경로를 선택합니다:

1. `command -v rallish`를 시도. 있으면 bare `rallish` 사용.
2. 없으면 `$PWD/dist/rallish` 확인. 있으면 절대 경로 사용.
3. 없으면 repo root에서 `make build` 후 `$PWD/dist/rallish` 사용.

아래에서는 선택한 경로를 `$RALLISH`로 표기합니다.

## 대화 상태

- `SID`: rally session id
- `ROLE`: 이 에이전트의 participant 이름 (예: `alice`)
- `PARTNERS`: 모든 participant의 쉼표 구분 목록

## 트리거 — "rally mcp" / "MCP로 랠리"

### 1. daemon 확인

```sh
$RALLISH doctor || ($RALLISH daemon &)
```

daemon이 `~/.rallish/port`를 작성할 때까지 잠시 대기.

### 2. rally 세션 생성

```sh
SID=$($RALLISH rally mcp-agent --mode create \
  --participants alice,bob \
  --first alice \
  --task "[pattern:cycle] refactor auth")
```

명령은 `RallySession` JSON 객체를 출력합니다. 여기서 `id`를 추출해 `SID`에
저장합니다.

### 3. 내 차례가 오면 baton 대기

```sh
$RALLISH rally mcp-agent --mode join --session-id "$SID" --as "$ROLE"
```

baton이 도착할 때까지 블록. 출력은 `BatonEvent` JSON 객체입니다:

```json
{"turn_n": 2, "from": "alice", "note": "implemented login"}
```

명령이 exit code 2로 끝나면 timeout이 발생한 것이므로, 사용자에게 계속
대기할지 중단할지 묻습니다.

### 4. 작업 수행

baton note나 session task에 설명된 작업을 수행합니다.

### 5. baton 넘기기

```sh
$RALLISH rally mcp-agent --mode done \
  --session-id "$SID" \
  --as "$ROLE" \
  --note "내가 한 일 요약"
```

출력은 업데이트된 `RallySession` JSON입니다. holder가 아니면 "not the current
baton holder" 메시지와 함께 실패합니다.

### 6. status로 폴링 (선택)

`join`으로 블록하지 않을 때 세션 스냅숏을 확인:

```sh
$RALLISH rally mcp-agent --mode status --session-id "$SID"
```

## HTTP/SSE rally 스킬과의 공존

`rally mcp-agent --mode create`로 만든 세션은 `rallish rally join`(HTTP/SSE)을
사용하는 participant도 참여할 수 있고, 그 반대도 가능합니다. MCP와 HTTP
표면은 동일한 in-memory rally 상태를 공유합니다.
