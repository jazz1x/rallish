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
      - "일반 HTTP/SSE rally (MCP 키워드 없음) — rallish 스킬 사용"
      - "자율 사이클, squash, rally 외 작업 — 해당 스킬 사용"
      - "daemon 생애주기 또는 CLI 플래그 수정 — internal/cli/daemon.go, internal/cli/rally_mcp_agent.go 참조"
      - "스킬 감사 / SSL 검사 — skill-audit 사용"
  structural:
    scenes: [DaemonCheck, Create, Join, Work, Done, Status]
    resumable: false
    branches:
      - "바이너리 해석: `command -v rallish` 시도, 다음 `$PWD/dist/rallish`, 다음 `make build`"
      - "DaemonCheck: `~/.rallish/port`가 없으면 백그라운드에서 `rallish daemon` 실행 후 port 파일 대기"
      - "Create: `--participants`에 2명 이상 필요; 선택적 `--first`, `--task`, `--repo`"
      - "Create: RallySession JSON stdout에서 `id` 추출"
      - "Join: baton 도착 또는 `--timeout`까지 블록; exit 2 = timeout; exit 1 = 세션 없음 / 멤버 아님 / 중복 연결"
      - "Done: `--as`가 현재 holder여야 함; 선택적 `--handoff-to`; 에러 메시지에 실제 holder 포함"
      - "Status: `--session-id` 필요; RallySession JSON 출력"
      - "Interrupt: `rally mcp-agent --mode interrupt --session-id <SID>`로 rally 중단"
  logical:
    tools: [Bash]
    side_effects:
      reads: ["~/.rallish/socket", "~/.rallish/port"]
      writes:
        - "~/.rallish/port — daemon 시작 시 기록"
        - "~/.rallish/rallish.sock — daemon이 생성 (mode 0600)"
        - "~/.rallish/sessions/<id>/log.jsonl — daemon 세션 저장소가 기록"
        - "~/.rallish/daemon.log — daemon 출력을 리다이렉트할 경우"
      network: ["rallish 바이너리를 통한 로컬 HTTP/SSE loopback"]
    idempotent: false
    rollback: "`rallish rally mcp-agent --mode interrupt --session-id <SID>`로 rally 중단"
---

# rallish-mcp — MCP를 통한 Rally

이 스킬은 daemon의 MCP 2025-03-26 표면을 통해 rallish rally에 참여합니다.
에이전트는 `rallish rally mcp-agent`만 호출하면 되며, SSE 전송, JSON-RPC,
tool dispatch는 서브커맨드가 날부 처리합니다.

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

`rallish doctor`는 daemon이 없어도 상태 테이블을 출력하고 exit 0을
반환하므로, `doctor || daemon` 패턴에 의존하지 마세요. 대신 daemon port
파일을 직접 확인합니다:

```sh
if [ ! -f ~/.rallish/port ]; then
    $RALLISH daemon > ~/.rallish/daemon.log 2>&1 &
    while [ ! -f ~/.rallish/port ]; do sleep 0.5; done
fi
```

`~/.rallish/port`가 생길 때까지 대기한 후 `mcp-agent` 명령을 호출합니다.

### 2. rally 세션 생성

```sh
SID=$($RALLISH rally mcp-agent --mode create \
  --participants alice,bob \
  --first alice \
  --task "[pattern:cycle] refactor auth")
```

`--participants`는 쉼표로 구분된 2명 이상의 이름이 필요합니다. `--first`,
`--task`, `--repo`는 선택적입니다. 명령은 `RallySession` JSON 객체를 출력합니다.
여기서 `id`를 추출해 `SID`에 저장합니다.

### 3. 내 차례가 오면 baton 대기

```sh
$RALLISH rally mcp-agent --mode join --session-id "$SID" --as "$ROLE"
```

baton이 도착할 때까지 블록. 출력은 `BatonEvent` JSON 객체입니다:

```json
{"turn_n": 2, "from": "alice", "note": "implemented login"}
```

명령이 exit code 2로 끝나면 timeout이 발생한 것이므로, 사용자에게 계속
대기할지 중단할지 묻습니다. exit 1은 세션이 없거나 participant가 멤버가
아니거나 이미 연결되어 있음을 의미합니다.

### 4. 작업 수행

baton note나 session task에 설명된 작업을 수행합니다.

### 5. baton 넘기기

```sh
$RALLISH rally mcp-agent --mode done \
  --session-id "$SID" \
  --as "$ROLE" \
  --note "내가 한 일 요약" \
  --handoff-to bob
```

출력은 업데이트된 `RallySession` JSON입니다. holder가 아니면 실제 holder를
포함한 "not the current baton holder" 메시지와 함께 실패합니다.
특정 participant에게 baton을 넘기려면 `--handoff-to`를 사용하고, 그렇지
않으면 rally 순서에 따라 다음 holder가 결정됩니다.

### 6. status로 폴링 (선택)

`join`으로 블록하지 않을 때 세션 스냅숏을 확인:

```sh
$RALLISH rally mcp-agent --mode status --session-id "$SID"
```

### 7. 멈춘 rally 중단

세션이 멈춰서 중단해야 할 때:

```sh
$RALLISH rally mcp-agent --mode interrupt --session-id "$SID"
```

## HTTP/SSE rally 스킬과의 공존

`rally mcp-agent --mode create`로 만든 세션은 `rallish rally join`(HTTP/SSE)을
통해 참여할 수 있고, 그 반대도 가능합니다. MCP 표면과 HTTP 표면은 동일한
메모리 내 rally 상태를 공유합니다.
