---
name: rallish-operator
description: >
  에이전트 주도 테니스 스타일 랠리 플레이북. 에이전트가 rallish CLI 명령을
  직접 실행하며, 사용자는 세 가지 자연어 트리거만 입력하면 됩니다. 서버
  준비, 리시버 준비, 첫 번째 서브, 이후 리턴, 종료까지 다룹니다. squash
  모드(헤드리스 프리셋 자동 실행)도 포함합니다.
  바톤 위에 세 가지 행동 패턴을 지원합니다: cycle (계획/실행/검토), discuss (다관점 논의), help (막힐 때 짧은 조언).
  v0.3은 자동 루프를 제공합니다: 각 에이전트가 한 번의 설정 트리거 후 `rally join --once`로 자가 폴링하며 사용자 개입 없이 핑퐁합니다.
  Triggers: "랠리보낼 준비해", "let's serve", "serve prep", "rally prep — serving", "서브 준비", "랠리받을 준비해", "let's return", "returner prep", "rally prep — returning", "리턴 준비", "시작", "serve!", "go", "start rally", "내 차례", "내 차례 됐어?", "is it my turn", "ready", "ready to return", "끝", "match over", "stop rally", "랠리 끝", "cycle", "plan-execute-review", "사이클로 가자", "discuss", "discussion rally", "논의 랠리", "여러 시선으로", "stuck rally", "help me out", "막혔어 도와줘", "한 번만 봐줘"
version: 0.3.0
ssl:
  scheduling:
    anti_triggers:
      - "어댑터 wiring / 신규 런타임 통합 — internal/adapter/ 와 DESIGN.md §11 참조"
      - "브로커 HTTP 표면 수정 — docs/prd-rally-mode.md 와 internal/broker/ 참조"
      - "일반 Go 코딩 컨벤션 — AGENTS.md 참조"
      - "스킬 감사 / SSL 점검 — galmuri:audit 사용"
      - "짧은 트리거 '시작' / 'go' / '끝' / '내 차례'는 STATE-GATED. 직전의 'serve prep' 또는 'returner prep' 트리거로 ROLE과 SID가 이미 설정된 경우에만 매칭. 무관한 맥락의 단독 'go' / '시작' / '끝'은 무시 — 랠리 시그널이 아닌 일반 언어로 취급."
  structural:
    scenes: [ServerPrep, PatternSelect, ReceiverPrep, Serve, Return, Continue, AutoLoop, MatchOver]
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
      - "pattern cue absent in ServerPrep message → ask user 'cycle / discuss / help / freeform'; default freeform after timeout"
      - "pattern cycle → planner side emits [plan] notes; executor side emits [result] notes; planner reviews with [review] on alternate turns"
      - "pattern discuss → both sides peer; convergence detected by mutual [agree] within 2 turns OR user '끝'"
      - "pattern help → owner stays driving; helper provides at most ~3 [hint] turns; owner's [resolved] ends session"
      - "mid-rally switch → [switch-pattern:<name>] proposed, acked next turn by [switch-ack:<name>]"
      - "서버 준비는 `rally new --first server`를 사용하므로 세션 생성 즉시 바톤이 할당됨 (SSE 팬텀 조인 불필요)"
      - "턴 사이에 각 쪽은 `rally join --once --timeout 5m --as <ROLE>`을 실행하여 다음 바톤 도착 또는 타임아웃(exit 2)까지 블로킹"
      - "join exit 2 (타임아웃) → 사용자에게 '5분간 baton 없음. 계속 대기할까 혹은 끝낼까?' 체크포인트; 기본 동작: `끝` 입력 없으면 루프 재개"
      - "패턴별 종료 신호 감지 → 에이전트가 최종 요약을 사용자에게 전달하고 루프 종료"
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
- PATTERN: cycle | discuss | help | freeform (기본값; 트리거 A 또는 랠리 중 전환으로 설정)
- LAST_HOLDER: 에이전트가 마지막으로 확인한 holder (바톤 도착 감지에 사용)
- EXIT_REASON: 루프 종료 시 채워짐 ('mutual-agree', 'review-approved', 'resolved', 'user-끝', 'timeout-abandoned')

## 트리거 A — "랠리보낼 준비해" (또는 영어 동의어)
이 쪽 에이전트가 서버가 됩니다.

1. `rallish doctor` 실행해 브로커 접속 확인. 데몬 미실행 시
   `rallish daemon &` 백그라운드 실행.
2. 새 --first server 플래그로 rally new 실행:

   ```sh
   SID=$(rallish rally new --participants server,returner --first server \
         --task "[pattern:$PATTERN] $TASK_TEXT")
   ```

   --first 덕분에 세션이 즉시 server_turn / holder=server / turnN=1 상태가 됨 —
   바톤 확보를 위한 SSE 조인 불필요.

3. 상태 저장: SID, ROLE=server, PHASE=prep.
3a. 패턴 선택. 사용자의 "랠리보낼 준비해" 메시지에서 패턴 단서를 스캔합니다 (아래 "랠리 패턴" 섹션 참조). 단서가 있으면 PATTERN을 설정합니다. 없으면 한 번 질문합니다: "패턴 선택 — cycle (계획/실행/검토), discuss (다관점 논의), help (막힐 때 짧은 조언), freeform (자유)?" — 1턴 타임아웃 후 기본값 freeform. 선택된 패턴을 `rally new --task`의 `[pattern:<name>]` 접두사로 인코딩합니다. 예: `--task "[pattern:cycle] OAuth2 PKCE 도입"`. 리시버 쪽은 `rally status`에서 이 접두사를 파싱합니다.
4. 사용자에게 안내:
   > Server 준비 완료. Session ID: <SID>.
   > 다른 터미널에서 "랠리받을 준비해 <SID>" 라고 말해줘.

5a. **첫 번째 턴을 직접 서브합니다.** 선택된 PATTERN에 맞게 첫 노트를 작성:
    - cycle: `[plan] step 1: <한 줄 지시>`
    - discuss: `[opinion] <입장 + 근거>`
    - help: `[stuck] 증상: …, 시도: …`

    실행: `rallish rally done --session-id $SID --as server --note "<위 내용>"`.
    상태가 이제 returner_turn이 됩니다.

5b. **자동 루프 진입** (아래 "자동 루프" 섹션 참조).

## 트리거 B — "랠리받을 준비해 <SID>" (또는 영어 동의어)
이 쪽 에이전트가 리시버가 됩니다.

1. 사용자 메시지에서 rly_... 패턴으로 SID 추출.
2. `rallish rally status --session-id $SID` 실행해 세션 존재 확인.
   404 시 "그 ID로 세션이 없어. 서버 쪽에서 다시 만들어달라고 해줘." 안내 후 중단.
3. 상태 저장: SID, ROLE=returner, PHASE=prep.
3a. 패턴 감지. `rally status` 출력의 `task` 필드에서 선행 `[pattern:<name>]` 토큰을 파싱합니다. 로컬 PATTERN = 해당 이름 (없으면 `freeform`). 아래 §랠리 패턴의 역할 구성을 미러링합니다.
4. 사용자에게 안내:
   > Returner 준비 완료. 서버가 서브할 때까지 대기 중.

4a. **자동 루프 진입** (아래 "자동 루프" 섹션 참조). 리시버는
    `내 차례` 사용자 트리거를 기다리지 않습니다 —
    `rally join --once` 호출이 바톤 도착까지 블로킹합니다.

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

**참고 (v0.3+):** 자동 루프가 턴 사이의 `내 차례` 필요성을 대체합니다.
이 트리거는 수동 오버라이드 또는 루프가 일시정지된 경우에도 계속 지원됩니다.

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

## 자동 루프 (Auto-Loop, 양쪽 모두)

트리거 A의 첫 번째 턴(서버) 또는 트리거 B의 준비(리시버) 이후,
양쪽이 동일한 루프를 실행합니다. 사용자는 턴 사이에 어떤 트리거도
입력할 필요가 없습니다.

```
while true:
    cue = bash("rallish rally join --session-id $SID --as $ROLE --once --timeout 5m")
    exit_code = $?

    if exit_code == 2:
        # 타임아웃 — 5분간 브로커 조용
        사용자에게 안내: "🎾 5분간 baton 없음. 계속 대기할까(엔터)? 끝낼까(끝)?"
        사용자가 '끝' 입력: EXIT_REASON=timeout-abandoned 로 루프 종료
        그 외: continue   # 루프 재개

    # 바톤 도착 — cue 라인에서 turn + from + note 파싱
    parse_cue(cue) → TURN, FROM, NOTE

    # 패턴별 종료 신호 감지
    if PATTERN == discuss and last_two_history_notes_are("[agree]", "[agree]"):
        EXIT_REASON = mutual-agree; 사용자에게 안내 "🎾 양쪽 [agree]. 랠리 종료."; break
    if PATTERN == cycle and NOTE.startswith("[review] approved") and no_pending_plan_in_history():
        EXIT_REASON = review-approved; 사용자에게 안내 "🎾 사이클 완료."; break
    if PATTERN == help and NOTE.startswith("[resolved]") and ROLE == helper:
        EXIT_REASON = resolved; 사용자에게 안내 "🎾 owner가 [resolved]. 도움 랠리 종료."; break

    # PATTERN + 이전 NOTE에 맞게 응답 작성
    new_note = compose_response(PATTERN, NOTE, history)
    bash("rallish rally done --session-id $SID --as $ROLE --note '$new_note'")

    # 사용자에게 체크포인트 (한 줄)
    사용자에게 안내: "🎾 보냈어: <new_note 첫 60자>. 다음 차례 대기 중."

    # 루프 전 사용자 입력 확인: '끝' 입력 시 루프 종료
    if user_message_pending and contains_끝:
        EXIT_REASON = user-끝; 사용자에게 안내 "랠리 종료."; break
```

**루프 내부 휴리스틱:**
- `compose_response(discuss, NOTE, history)` — NOTE가 `[opinion]`이면
  에이전트 자신의 관점에 따라 `[counter]` 또는 `[agree]` 발행;
  NOTE가 `[question]`이면 답변하는 `[opinion]` 발행.
- `compose_response(cycle, NOTE, history)` — ROLE==executor이고
  NOTE가 `[plan]`이면 작업 수행 후 `[result]` 발행; ROLE==planner이고
  NOTE가 `[result]`이면 `[review] approved` (또는 `change request: …`)
  뒤에 다음 `[plan]` 슬라이스 발행.
- `compose_response(help, NOTE, history)` — helper는 `[hint]` 발행;
  owner는 각 힌트 후 `[try]`, 문제 해결 시 `[resolved]` 발행.
  helper는 `[try]` 없이 `[hint]`를 3턴 연속 발행하지 않습니다.

**사용자 인터럽트 규칙:** `rally done` 완료와 다음 `rally join --once`
사이에 에이전트가 사용자에게 양보합니다. 이 짧은 체크포인트 창에서
사용자 메시지가 오면 처리합니다 (예: `끝`, "잠깐, 방향 바꿔줘").
침묵 = 계속.

**하드 상한:** 루프가 종료 신호 없이 20회 이상 반복되면
에이전트가 `"🎾 20턴 넘었어. 정리할까?"` 로 사용자에게
체크포인트하고 일시 정지합니다.

## 랠리 패턴

이 스킬은 랠리 프리미티브 위에 세 가지 행동 패턴을 지원합니다. 패턴은
서버 준비(트리거 A)에서 선택되고 리시버가 `rally status`로 미러링합니다.
세 패턴 모두 동일한 `rally new/join/done/status` 명령을 사용하며,
**역할 구성**과 **노트 접두사**만 다릅니다.

### 패턴: cycle (계획 / 실행 / 검토)

두 코딩 CLI 세션 사이에서 번갈아 슬라이스를 위임할 때 사용합니다.

- 역할 구성: server = `planner`, returner = `executor`.
- 노트 규칙:
  - planner → `[plan] step N: <한 줄 지시>`
  - executor → `[result] diff: <요약>, tests: <pass|fail>`
  - planner (매 두 번째 턴) → `[review] approved` 또는 `[review] change request: <피드백>`
- 완료 신호: planner가 슬라이스 목록을 소진하면 `[review] approved` 발행,
  또는 사용자가 `끝`이라고 말하면 종료.

예시 교환:
1. planner — `[plan] step 1: OAuth2 PKCE 클라이언트 설정 추가`
2. executor — `[result] diff: cmd/auth/oauth.go +42 / tests: pass`
3. planner — `[review] approved. [plan] step 2: CLI에 플래그 연결`
4. executor — `[result] diff: internal/cli/auth.go +28 / tests: pass`
5. planner — `[review] approved. 모든 슬라이스 완료.`

### 패턴: discuss (다관점 논의)

두 관점이 수렴해야 하는 설계 결정이나 기술 토론에 사용합니다.

- 역할 구성: 양쪽 모두 `peer` (계층 없음).
- 노트 규칙:
  - `[opinion] <입장 + 근거 하나>`
  - `[question] <상대방에게 방어를 요청할 내용>`
  - `[counter] <반론 + 대안>`
  - `[agree] <양보 + 합의된 요점 재진술>`
- 완료 신호: 마지막 두 턴 안에 양쪽 모두 `[agree]` 발행, 또는 사용자가 `끝`.

예시 교환:
1. peer1 — `[opinion] Postgres로 마이그레이션; SQLite는 쓰기 경쟁 시 잠김`
2. peer2 — `[counter] WAL 모드가 처리 가능; 전환 비용 2주`
3. peer1 — `[question] WAL이 버티는 목표 wQPS가 있어?`
4. peer2 — `[opinion] 피크 ~50 wQPS; WAL은 1000+까지 문제없음`
5. peer1 — `[agree] SQLite + WAL 유지; wQPS > 500이면 재검토`
6. peer2 — `[agree] 동의`

### 패턴: help (막힐 때 짧은 조언)

오너가 막혔을 때 한두 라운드 입력을 받는 짧고 비대칭적인 교환에 사용합니다.

- 역할 구성: server = `owner` (계속 작업 주도), returner = `helper`.
- 노트 규칙:
  - owner → `[stuck] 증상: <오류 또는 동작>, 시도: <X, Y>`
  - helper → `[hint] <Z> 시도해봐, 또는 <W> 확인해봐` (helper는 작업을 직접 하지 않음)
  - owner → `[try] <Z> 적용, 결과: <새로운 상태>`
  - owner → `[resolved] <근본 원인 + 수정>` (랠리 종료)
- 예상 길이: 2–6턴. helper는 owner의 `[try]` 없이 `[hint]`를 3번 연속 이상
  발행하지 않습니다 — 이 경우 `[suggest:share-context]`를 발행해 관련
  코드/로그를 붙여넣어달라고 요청합니다.
- 완료 신호: owner가 `[resolved]` 발행, 또는 사용자가 `끝`.

예시 교환:
1. owner — `[stuck] SSE 쓰기가 3턴 후 블록됨; WriteTimeout 설정 시도해봤음`
2. helper — `[hint] http.ResponseController가 부모 컨텍스트 취소에 의해 가려졌는지 확인`
3. owner — `[try] 컨텍스트 체크 추가; 여전히 재현됨`
4. helper — `[hint] 각 쓰기 전에 flush; goroutine이 GC될 수 있음`
5. owner — `[resolved] 맞음 — 각 이벤트 후 rc.Flush() 추가; 원인은 버퍼된 writer`

### 랠리 중 패턴 전환

어느 쪽이든 전환을 제안할 수 있습니다:

- 제안자 → `[switch-pattern:<name>] reason: <이유>`
- 다음 턴 수신자 → `[switch-ack:<name>]`

양쪽 모두 이후 턴을 새 패턴에 따라 구성합니다. 스킬은 전환을 강제하지
않으며, 조율 규칙입니다.

### Freeform (기본값)

ServerPrep에서 패턴 단서가 감지되지 않으면 PATTERN = `freeform`.
에이전트는 접두사 없이 일반 노트를 사용합니다. v0.1.x 랠리 동작을
그대로 보존합니다.

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
v0.3에서는 에이전트가 `rally join --once --timeout <dur>`을 사용해
짧은 블로킹 SSE 대기와 명시적 데드라인을 결합합니다 — 두 방법의 장점을 모두 취합니다.

## 참조
- PRD: docs/prd-rally-mode.md
- 런북: docs/runbook-rally-mode.md
- 코드: internal/broker/rally.go, internal/cli/rally.go
- 프로젝트 컨벤션: AGENTS.md
