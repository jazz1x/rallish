---
name: rallish-operator
description: >
  rallish 랠리 운용 플레이북 — 사람이 직접 띄운 두 코딩 CLI 세션(claude, kimi 등)
  사이에서 로컬 rallish 브로커가 baton을 중계하는 라이브 협업 모드. 세션 셋업,
  참가자 브리핑, baton 수신, hand-off, 종료까지 다룬다. squash 모드(헤드리스
  프리셋 자동 실행)도 포함. 사용자가 이 리포에서 멀티 에이전트 랠리 세션을
  시작·참여·조율하려 할 때 읽어야 한다.
  Triggers: "rally start", "let's rally", "start a rally", "two agents", "두 에이전트", "두 에이전트 같이", "baton pass", "baton hand-off", "multi-agent session", "pair coding session", "rallish 시작", "squash session", "headless squash"
version: 0.0.1
ssl:
  scheduling:
    anti_triggers:
      - "어댑터 wiring / 신규 런타임 통합 — internal/adapter/ 와 DESIGN.md §11 참조"
      - "브로커 HTTP 표면 수정 — docs/prd-rally-mode.md 와 internal/broker/ 참조"
      - "일반 Go 코딩 컨벤션 — AGENTS.md 참조"
      - "스킬 감사 / SSL 점검 — galmuri:audit 사용"
  structural:
    scenes: [Setup, Brief, Listen, HandOff, Status, Shutdown]
    resumable: true
    branches:
      - "fresh repo → `make build` 후 `rallish doctor`"
      - "데몬 미실행 → `rallish start` 가 자동 스폰; 또는 명시적으로 `rallish daemon &`"
      - "헤드리스 프리셋(solo-ralph, pair-review) → `rallish squash --preset <name>`"
      - "인터랙티브 2-CLI → `rallish rally new/join/done/status`"
      - "세션 중단(SSE drop) → 같은 --as 이름으로 `rallish rally join` 재실행; 브로커가 마지막 baton 재전송"
      - "비-홀더가 done 호출 → 409 → exit 1 + stderr 메시지"
  logical:
    tools: [Bash, Read]
    side_effects:
      reads: ["~/.rallish/socket", "~/.rallish/port", "~/.rallish/sessions/<id>/log.jsonl"]
      writes:
        - "~/.rallish/rallish.sock (mode 0600, 데몬 소유)"
        - "~/.rallish/sessions/<id>/log.jsonl (턴별 req/resp)"
        - "~/.rallish/presets/*.yaml (사용자 커스텀 프리셋 추가 시)"
      deletes:
        - "~/.rallish/{rallish.sock, socket, port} (데몬 SIGTERM 시)"
      network: []
    idempotent: true
    rollback: null
---

# rallish-operator — 라이브 랠리 플레이북

이 스킬은 이 리포의 rallish 브로커를 사용해 **rally** 세션 — 두 라이브 코딩
CLI 인스턴스 사이의 baton 전달 — 을 운용하는 에이전트(또는 사람)를 위한
브리핑이다.

## rallish란

여러 에이전트가 한 작업을 두고 turn-taking 할 때 "지금 누구 차례인지"를
관리하는 로컬 브로커 프로세스. 두 가지 모드:

- **squash** — 헤드리스. `rallish` 가 어댑터 서브프로세스(`claude -p`,
  `kimi -p`)를 스폰하고 프리셋(`solo-ralph`, `pair-review`)을 사람 개입
  없이 끝까지 돌림.
- **rally** — 인터랙티브. 두 사람(또는 사람+에이전트)이 각자의 CLI 세션을
  띄움; rallish는 SSE로 baton만 양쪽에 전달.

squash는 "자동운전". rally는 "두 라이브 선수의 테니스, rallish는 심판".

## Pre-flight

```bash
make build                   # ./dist/rallish 생성
./dist/rallish doctor        # 어댑터 바이너리 + 데몬 상태 점검
```

`doctor`가 `daemon not running` (다음 명령이 자동 스폰) 또는
`daemon reachable via unix socket path=~/.rallish/rallish.sock perm=-rw-------`
보고.

## Squash 모드 (헤드리스)

```bash
rallish squash --preset solo-ralph  --task "foo/bar 의 flaky 테스트 수정"
rallish squash --preset pair-review --task "session store 리팩터"
```

브로커가 설정된 어댑터를 스폰해 예산 소진 또는 `exit_when` 일치까지 구동.
턴별 페이로드는 `~/.rallish/sessions/<id>/log.jsonl` 에 기록. 추가 입력
불필요.

## Rally 모드 (인터랙티브)

### 1. 세션 생성

```bash
SID=$(rallish rally new --participants alice,bob --task "OAuth2 PKCE")
echo $SID                    # rly_1747382400000_a3f9
```

이름은 `^[a-zA-Z0-9_-]{1,16}$` 매치 필수. 참가자 2명 이상.

### 2. 각 참가자가 자기 터미널에서 join

```bash
# 터미널 A
rallish rally join --session-id $SID --as alice

# 터미널 B
rallish rally join --session-id $SID --as bob
```

첫 join 한 참가자가 자동으로 첫 baton을 받음. join은 blocking — 프로세스가
SSE를 잡고 있다가 자기 차례 되면 cue를 출력:

```
🏓 your turn (turn 1, from (start)): (no note)
   → work in your CLI (e.g. claude). When done, in any terminal:
   →   rallish rally done --session-id rly_... --as alice --note "<summary>"
```

### 3. 라이브 에이전트 브리핑

라이브 코딩 CLI(claude/kimi/cursor)는 rally 시그널을 자동으로 보지 못함.
다음과 같이 알려줄 것:

> 당신은 rally 세션 `<SID>` 의 참가자 `<name>` 입니다. join 터미널에
> `🏓 your turn` 이 뜨면 이번 턴 작업을 수행하세요. 끝나면 멈추고
> 다음 한 줄을 출력하세요: `RALLY:DONE — <요약 한 줄>`. 그 줄 이후로는
> 계속 진행하지 말고 다음 턴을 기다리세요.

각 에이전트에게 줄 system prompt 블록 예:

```
당신은 rally <SID> 의 PLANNER, 상대는 REVIEWER 입니다.
- 파일 경로와 diff 가 포함된 구체적 계획을 출력하세요.
- 완료 시 "RALLY:DONE — <요약>" 출력 후 정지.
- 컨벤션: 작은 diff, conventional commits, AGENTS.md 참조.
```

### 4. baton 전달

라이브 에이전트가 자기 턴 작업을 끝내면 운영자가:

```bash
rallish rally done --session-id $SID --as alice --note "plan v1: endpoint 3개"
```

선택적 `--handoff-to bob` 으로 기본 round-robin 순서 오버라이드 가능.
출력: `ok — baton passed to bob (turn 2)`. bob의 join 터미널이 즉시 alice의
note를 컨텍스트로 cue 출력.

### 5. 상태 확인

```bash
rallish rally status --session-id $SID
```

현재 holder, 턴 카운트, 참가자별 last-seen 하트비트, hand-off 이력 표시.

### 6. 종료

```bash
kill -TERM $(pgrep -f "rallish daemon")
```

데몬이 활성 SSE 스트림에 `data: {"closed":true}` 브로드캐스트, 세션을
`interrupted` 로 전이, `~/.rallish/{rallish.sock, socket, port}` 를 1초 내
정리.

## 에이전트에게 주입할 컨벤션

- **무한 루프 금지.** `RALLY:DONE` 출력하고 멈춤. 운영자(또는 셸 권한 있다면
  본인)가 `rally done` 호출.
- **이전 note 를 먼저 읽을 것.** 직전 참가자의 작업 요약.
- **첫 턴 note 는 `(no note)`** — `rallish rally status` 의 task 설명에서 시작.
- **409 발생 시** ("not your turn"): `rally status` 로 실제 holder 확인.
- **연결 끊김 시**(SSE drop): `rallish rally join --as <name>` 재실행.
  자기 차례면 브로커가 현재 baton 재전송.

## Anti-pattern

| 하지 말 것 | 이유 |
|---|---|
| 비-홀더가 `rally done` | 409; 브로커가 거부 + 로그 |
| 비-trivial 턴에 `--note` 생략 | 다음 홀더에 컨텍스트 없음 |
| `RALLY:DONE` 이후에도 계속 작업 | 턴 경계 깨짐; 브로커가 도울 수 없음 |
| `~/.rallish/sessions/<id>/log.jsonl` 직접 편집 | append-only 감사 로그; resume 깨짐 |

## 트러블슈팅

| 증상 | 원인 | 해결 |
|---|---|---|
| `rally new` 후 `daemon not running` | 첫 실행; 다음 명령이 자동 스폰 | 재시도; `rallish doctor` 로 확인 |
| `🏓 your turn` 안 옴 | 상대 참가자가 아직 join 안 했거나 본인이 holder 아님 | `rally status` 로 holder 확인 |
| stderr `Error: not your turn (holder: bob)` | 다른 참가자 이름으로 done 호출 | 실제 holder 이름으로 재시도 |
| 크래시 후 소켓 파일 잔존 | 데몬을 -9 로 죽임 (TERM 아님) | `rm -f ~/.rallish/{rallish.sock,socket,port}` 후 재기동 |

## 참조

- PRD: `docs/prd-rally-mode.md`
- 런북 (검증 워크스루): `docs/runbook-rally-mode.md`
- 코드: `internal/broker/rally.go`, `internal/cli/rally.go`
- 프로젝트 컨벤션: `AGENTS.md`
- 아키텍처: `DESIGN.md`
