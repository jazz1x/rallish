---
name: rallish-operator
description: >
  에이전트 주도 테니스 스타일 랠리 플레이북. 에이전트가 rallish CLI 명령을
  직접 실행하며, 사용자는 세 가지 자연어 트리거만 입력하면 됩니다. 서버
  준비, 리시버 준비, 첫 번째 서브, 이후 리턴, 종료까지 다룹니다. squash
  모드(헤드리스 프리셋 자동 실행)도 포함합니다.
  Triggers: "랠리보낼 준비해", "let's serve", "serve prep", "rally prep — serving", "서브 준비", "랠리받을 준비해", "let's return", "returner prep", "rally prep — returning", "리턴 준비", "시작", "serve!", "go", "start rally", "내 차례", "내 차례 됐어?", "is it my turn", "ready", "ready to return", "끝", "match over", "stop rally", "랠리 끝"
version: 0.1.0
ssl:
  scheduling:
    anti_triggers:
      - "어댑터 wiring / 신규 런타임 통합 — internal/adapter/ 와 DESIGN.md §11 참조"
      - "브로커 HTTP 표면 수정 — docs/prd-rally-mode.md 와 internal/broker/ 참조"
      - "일반 Go 코딩 컨벤션 — AGENTS.md 참조"
      - "스킬 감사 / SSL 점검 — galmuri:audit 사용"
      - "짧은 트리거 '시작' / 'go' / '끝' / '내 차례'는 STATE-GATED. 직전의 'serve prep' 또는 'returner prep' 트리거로 ROLE과 SID가 이미 설정된 경우에만 매칭. 무관한 맥락의 단독 'go' / '시작' / '끝'은 무시 — 랠리 시그널이 아닌 일반 언어로 취급."
  structural:
    scenes: [ServerPrep, ReceiverPrep, Serve, Return, Continue, MatchOver]
    resumable: true
    branches:
      - "ServerPrep: 데몬 미실행 → `rally new` 전 `rallish daemon &` 백그라운드 실행"
      - "ServerPrep: `rally new` stdout에서 rly_... 세션 ID 파싱"
      - "ReceiverPrep: 사용자 메시지에서 rly_... 패턴으로 SID 추출"
      - "ReceiverPrep: status 확인 시 404 → 세션 없음 안내 후 중단"
      - "Serve(첫 턴): 시작 메시지에 작업 설명이 있으면 사용; 없으면 질문"
      - "Return/Continue: holder 불일치 → 실제 holder 보고, 진행 중단"
      - "409(rally done): 드문 레이스; rally status 재실행 후 실제 holder 보고"
      - "SSE 미사용 — 에이전트는 사용자 트리거 시 rally status 폴링 (메시지별 생명주기)"
      - "크래시 복구 (데몬 -9 종료로 소켓 잔존) → 수동 `rm -f ~/.rallish/{rallish.sock,socket,port}` 후 `rallish daemon &`"
      - "--handoff-to: 지원; 사용자가 대상 참가자를 지정하면 rally done에 전달"
      - "headless squash 폴백: 두 번째 터미널 없을 때 `rallish squash --preset solo-ralph --task \"...\"`"
  logical:
    tools: [Bash, Read]
    side_effects:
      reads: ["~/.rallish/socket", "~/.rallish/port", "~/.rallish/sessions/<id>/log.jsonl"]
      writes:
        - "~/.rallish/rallish.sock (mode 0600, 데몬 소유)"
        - "~/.rallish/sessions/<id>/log.jsonl (턴별 req/resp)"
        - "~/.rallish/presets/*.yaml (사용자 커스텀 프리셋 추가 시)"
      deletes:
        - "~/.rallish/{rallish.sock, socket, port} — 데몬 SIGTERM 시 자동; 데몬이 -9 로 종료되어 파일이 남았을 때 크래시 복구용으로 수동 `rm -f`"
      network: []
    idempotent: true
    rollback: null
---

# rallish-operator — 테니스 랠리 플레이북

이 스킬은 rallish 브로커를 통해 두 코딩 CLI 세션 사이의 라이브 테니스
스타일 랠리를 에이전트가 직접 구동합니다. 사용자는 세 가지만 입력하면
되고, 나머지는 에이전트가 처리합니다.

## Bootstrap (rallish 바이너리가 없을 때)

스킬에 플랫폼 감지 설치 스크립트가 번들되어 있습니다. `command -v rallish`
가 실패하면 번들 스크립트를 실행하세요:

```sh
sh ~/.claude/skills/rallish-operator/scripts/install-binary.sh
```

현재 OS/아키텍처에 맞는 최신 GitHub 릴리즈 바이너리를 받아
`/usr/local/bin` (쓰기 불가 시 `~/.local/bin`) 에 설치합니다.
설치 완료 후 트리거를 다시 실행하세요.

## `rallish` 바이너리 경로 먼저 해결
어떤 rally 명령보다 먼저 실행 가능 경로를 결정:

1. `command -v rallish` 시도. 있으면 그대로 사용.
2. 없으면 `$PWD/dist/rallish` 확인 (`make build` 직후 생성). 있으면 이후 모든 CLI 호출에 절대경로 사용.
3. 그것도 없으면 빌드: 리포 루트에서 `make build` 실행 후 `$PWD/dist/rallish` 사용.

선택된 경로를 이 스킬 전체에서 `$RALLISH` 로 부른다.

## 유지할 대화 상태
- SID: 랠리 세션 ID (`rally new` 출력 또는 사용자 메시지에서 추출)
- ROLE: "server" 또는 "returner"
- PHASE: prep / serving / returning / done
- RALLISH: 해결된 바이너리 경로 (위 참조)

## 트리거 A — "랠리보낼 준비해" (또는 영어 동의어)
이 쪽 에이전트가 서버가 됩니다.

1. `rallish doctor` 실행해 브로커 접속 확인. 데몬 미실행 시
   `rallish daemon &` 백그라운드 실행.
2. `SID=$(rallish rally new --participants server,returner --task "TBD")` 실행.
   stdout에서 SID 파싱.
3. 상태 저장: SID, ROLE=server, PHASE=prep.
4. 사용자에게 안내:
   > Server 준비 완료. Session ID: <SID>.
   > 다른 터미널에서 "랠리받을 준비해 <SID>" 라고 말해줘.
   > 받는 쪽 준비되면 여기에 "시작" 이라고 해.

## 트리거 B — "랠리받을 준비해 <SID>" (또는 영어 동의어)
이 쪽 에이전트가 리시버가 됩니다.

1. 사용자 메시지에서 rly_... 패턴으로 SID 추출.
2. `rallish rally status --session-id $SID` 실행해 세션 존재 확인.
   404 시 "그 ID로 세션이 없어. 서버 쪽에서 다시 만들어달라고 해줘." 안내 후 중단.
3. 상태 저장: SID, ROLE=returner, PHASE=prep.
4. 사용자에게 안내:
   > Returner 준비 완료. 서버가 서브할 때까지 대기 중.
   > 서버가 넘겼다고 알려주면 그냥 "내 차례" 라고 말해.

## 트리거 C — "시작" (서버 쪽, prep 이후)
에이전트가 첫 번째 턴을 서브합니다.

1. ROLE == server 이고 PHASE == prep 인지 확인 (아니면 잘못된 쪽 메시지 안내).
2. `rallish rally status --session-id $SID` 실행. holder == "server" 확인.
3. "서브할 작업 뭐야?" 질문 — 단, 사용자의 "시작" 메시지에 이미 작업 설명이
   포함되어 있으면 그것을 사용.
4. 작업 수행 — 파일 읽기, 코드 작성, 명령 실행 등 작업에 필요한 모든 것.
5. `rallish rally done --session-id $SID --as server --note "<방금 한 작업 한 줄 요약>"` 실행.
6. 사용자에게 안내:
   > 🎾 서브 완료. Returner한테 넘겼어.
   > 상대 터미널에서 "내 차례" 라고 말하면 받을 거야.

## 트리거 D — "내 차례" (prep 이후, 리시버 쪽)
에이전트가 자기 차례이면 바톤을 받습니다.

1. `rallish rally status --session-id $SID` 실행.
2. holder != 내 ROLE: "아직 내 차례 아니야. 현재 홀더: <holder>." 안내 후 중단.
3. holder == 내 ROLE: 히스토리 마지막 항목의 note 읽기.
4. 사용자에게 "🎾 상대가 넘긴 메모: \"<note>\". 이대로 진행할까?" 확인 (방향 수정 기회 제공).
5. 사용자 OK 또는 수정 지시 대기. 그런 다음 작업 수행.
6. `rallish rally done --session-id $SID --as <내 ROLE> --note "<요약>"` 실행.
7. 사용자에게 안내:
   > 🎾 리턴 완료. 다시 상대 차례.
   > 상대가 넘기면 또 "내 차례" 라고 말해.

## 트리거 E — "끝" / "match over"
깔끔한 종료.

1. 사용자에게 안내: "랠리 종료. 데몬은 살아있어 — 다음 세션도 같은 데몬 씀.
   완전히 끄려면 `kill -TERM $(pgrep -f 'rallish daemon')` 직접 실행."
2. 상태 초기화.

## 에러 처리 (가능하면 조용히 처리)
- 409 "not your turn": 드물지만 발생 시 rally status 실행 후
  실제 holder를 사용자에게 보고.
- 데몬 연결 거부: `rallish doctor` 실행; 데몬 미실행 시 `rallish daemon &`
  실행 후 재시도.
- 이 흐름에서 SSE 미사용 — 에이전트가 `rally status` 폴링으로 대체
  (장기 백그라운드 프로세스 불필요).

## 폴링, SSE 아닌 이유?
에이전트의 생명주기는 메시지 단위이며 지속적이지 않습니다. 백그라운드
SSE는 이 스킬의 범위를 벗어나는 프로세스 감시가 필요합니다. `rally status`
는 HTTP GET 한 번이면 충분하고, 사용자 주도 턴 흐름이 이미 명시적이므로
"내 차례" 시 폴링이 올바른 자동화 수준입니다.

## 참조
- PRD: docs/prd-rally-mode.md
- 런북: docs/runbook-rally-mode.md
- 코드: internal/broker/rally.go, internal/cli/rally.go
- 프로젝트 컨벤션: AGENTS.md
