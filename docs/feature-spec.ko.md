# rallish — 기능명세서

> 통합 기능명세. 광고된 모든 기능을 **실제 코드에 연결된 상태(wired state)**와 매핑하고, 동작 계약과 수용 기준을 함께 정의한다.
> **버전:** `VERSION`(0.3.0) 추종 · **최종 수정:** 2026-06-24 · [English](./feature-spec.md)

## 이 문서를 읽는 법

이 명세는 *"rallish가 오늘 실제로 무엇을 하는가, 그리고 무엇이 선언만 되어 있는가?"*에 답하는 단일 창구다. README와 north-star가 광고하는 표면은 의도적으로 실제 연결된 표면보다 넓다. 그 위에 무언가를 구현하려는 사람은 그 차이를 먼저 알아야 한다.

**성숙도 범례** (`north-star.md`와 동일):

| 태그 | 의미 |
|------|------|
| ✅ **연결됨(Wired)** | 프로덕션 호출 지점 있음, 런타임에 동작함, 테스트로 커버됨 |
| ◑ **부분(Partial)** | 연결되어 있으나 불완전·비대칭·경고만 함 |
| ○ **선언만(Declared-only)** | 코드/정책은 있으나 프로덕션 호출 지점이 0이거나, 파싱만 되고 소비되지 않음 |
| ▷ **계획됨(Planned)** | 명세(PRD)는 있으나 아직 미구현 |

각 기능 행은 근거 파일을 인용한다. 코드와 대조 검증한 항목은 file:line을 표기한다.

---

## 1. 기능 지도 (한눈에)

| # | 기능 | 성숙도 | 근거 |
|---|------|--------|------|
| F1 | Squash (헤드리스 프리셋 세션) | ✅ | `internal/cli/squash.go`, `internal/preset` |
| F2 | Rally (대화형 바톤 패싱) | ✅ | `internal/cli/rally.go`, `internal/broker/rally.go` |
| F3 | 자율 사이클 + 참조 드라이버(`cycle run --once`) | ✅ | `internal/cli/cycle_run.go`, `internal/cycle` |
| F4 | 어댑터 포트 (claude / kimi / fake) | ✅ | `internal/adapter` |
| F5 | 프리셋 시스템 (YAML 템플릿) | ✅ | `internal/preset` |
| F6 | 턴 라우팅 | ◑ | `internal/router/router.go` |
| F7 | 토큰 / 턴 / 벽시계 예산 | ✅ | `internal/budget`, `internal/exit` |
| F8 | 종료 조건 | ◑ | `internal/exit/exit.go` |
| F9 | 게이트 파이프라인 (preflight→audit→philosophy→polish→commit) | ✅ | `internal/cycle/gates/pipeline.go` |
| F10 | 안티스핀 stuck 브레이커 + 수명 한도 | ✅ | `internal/cycle/stuck.go`, `internal/budget` |
| F11 | 해시체인 append-only 레저 (G4 감사) | ✅ | `pkg/contract/harness_ledger.go`, `internal/cycle/ledger.go` |
| F12 | Merkle(RFC 9162) 포함/일관성 증명 | ○ | `pkg/contract/merkle.go` |
| F13 | 액션게이트(G6) 결정 + 기록 | ◑ | `pkg/contract/action_gate.go`, `internal/cli/gate.go` |
| F14 | 시크릿 격리 분류기 | ◑ | `pkg/contract/tooluse_gate.go` |
| F15 | Unix 소켓 IPC + TCP 루프백 폴백 | ✅ | `internal/ipc/socket.go`, `internal/cli/broker_client.go` |
| F16 | A2A v1.0 와이어 형태 (agent-card, JSON-RPC, SSE) | ◑ | `internal/broker/a2a.go` |
| F17 | MCP 서버 (rally 도구를 SSE로) | ✅ | `internal/broker/mcp.go` |
| F18 | 데몬 자동 기동 | ◑ | `internal/cli/squash.go` (squash 한정) |
| F19 | `doctor` 진단 | ◑ | `internal/doctor/doctor.go` |
| F20 | 공유 스크래치패드 + 압축 | ○ | `internal/scratch` |
| F21 | 로그 시점 시크릿 마스킹 | ○ | `internal/logx` (스텁) |
| F22 | Cross-check ping-pong (의도 인지 핸드오프, dry-round 브레이커, claim 오라클) | ▷ | `docs/prd-cross-check-ping-pong.md` |

이하 각 기능을 상세히 명세한다.

---

## 2. 핵심 개념

**브로커 / 데몬.** `127.0.0.1:0`(동적 포트)에 바인딩된 단일 로컬 HTTP 서버(`rallish daemon`). 대화/세션 상태를 소유하고 누구 차례인지 결정한다. Unix 도메인 소켓(`~/.rallish/rallish.sock`, 모드 `0600`)으로 접근하며 TCP 루프백 폴백이 있다. 브로커는 **턴과 상태의 운반자**이지 품질의 심판이 아니다.

**어댑터.** 에이전트 런타임 CLI를 감싸는 얇은 래퍼. 포트는 두 메서드: `Name() string`, `Run(ctx, TurnRequest) (TurnResponse, error)` (`internal/adapter/adapter.go`). 구현체: `claude`, `kimi`, `fake`.

**턴 계약.** 브로커가 `TurnRequest`(턴 번호, 역할, 예산, 직전 턴 요약, 과업)를 보내면 어댑터가 `TurnResponse`(`done`, `handoff_to`, `summary`, `artifacts`, `self_eval`, 선택적 `usage`)를 반환한다. `pkg/contract/types.go`.

**프리셋.** 역할(런타임+모델), 라우팅 전략, 예산, 종료 조건, 스크래치패드 한도를 정의하는 YAML 템플릿. 기본 프리셋은 `go:embed`로 바이너리에 컴파일된다.

**사이클.** 게이트 파이프라인을 통과하는, 경계가 있는 자율 저장소 작업 단위. append-only 해시체인 레저를 가진다. 사이클을 구동하는 오케스트레이터는 *참조 드라이버*이지 제품이 아니다 — 제품은 하네스 계층(게이트/상태/감사/브레이커)이다.

---

## 3. 기능 상세 명세

### F1 — Squash (헤드리스 프리셋 세션) ✅

**정의.** `rallish squash`는 헤드리스 프리셋 세션을 끝까지 실행한다. 브로커가 설정된 어댑터를 자동 기동하고 ping-pong을 완료까지 돌린다.

**동작.**
- 프리셋을 이름으로 해석한다: 빌트인 먼저, 그다음 `~/.rallish/presets/<name>.yaml`. `squash`는 프로젝트 로컬 `./.rallish/presets/`를 읽지 **않음**(squash.go:174는 사용자 홈 디렉터리만 사용). 기본 프리셋은 config `default_preset`(출시 기본값 `solo-ralph`).
- 데몬이 없으면 자동 기동한다(자동 기동하는 **유일한** 명령 — F18 참조).
- 종료 조건이 발화할 때까지 턴을 구동한다.

**수용 기준.**
- AC-F1.1: PATH에 `claude` 어댑터가 있을 때 `rallish squash --preset solo-ralph`가 세션을 완료하고 0으로 종료.
- AC-F1.2: 데몬이 없을 때 `squash`가 데몬을 기동하고 Unix 소켓으로 접속.
- AC-F1.3 ✅: 무자격증명 스모크 경로를 빌트인 `fake-demo` 프리셋(`internal/preset/presets/fake-demo.yaml`)으로 제공: 인프로세스 `fake` 런타임 단일 역할, 5턴 후 `turns_exhausted`. `rallish squash --preset fake-demo`는 데몬을 자동 기동하고 완료까지 실행하며 원장을 기록 — 어댑터 CLI/API 키 없이 엔드투엔드 검증됨.

**알려진 갭.**
- ✅ G-F1 (해결): 빌트인 `fake-demo` 프리셋이 무자격증명 설치 점검을 제공; YAML 직접 작성 불필요.

---

### F2 — Rally (대화형 바톤 패싱) ✅

**정의.** `rallish rally`는 둘 이상의 코딩 CLI 세션 사이에서 라이브 바톤 패싱을 제공한다. 배타적 보유자 강제는 SSE로 전달되고, 에이전트는 턴마다 사용자 트리거 없이 스스로 ping-pong을 돈다.

**명령 표면.**

| 명령 | 목적 | 필수 플래그 |
|------|------|-------------|
| `rally new` | 세션 생성 | `--participants` (≥2, 콤마 구분) |
| `rally join` | 합류 후 바톤 대기(SSE) | `--session-id`, `--as` |
| `rally done` | 바톤 넘기기 | `--session-id`, `--as` |
| `rally status` | 세션 상태 표시 | `--session-id` |
| `rally mcp-agent` | 원샷 MCP 클라이언트(create/join/done/status/interrupt) | `--mode` |

**동작 계약.**
- `rally new`는 참가자 이름 ≥2와 선택적 repo 경로(존재해야 함)를 검증. `--first`는 바톤을 선배정; 생략 시 첫 참가자가 `join`해야 시작.
- `rally join`은 `/rally/sessions/{id}/baton?as={participant}`로 SSE 스트림을 열고 턴 번호+지시를 출력. `--once`는 첫 바톤 후 종료; `--timeout`은 윈도우 내 바톤 미수신 시 **종료코드 2**. `--timeout 0`은 무한 블록.
- `rally done`은 **HTTP 409**를 비치명적 "당신 차례 아님" / "세션 중단" 조건으로 반환(종료코드 1).

**수용 기준.**
- AC-F2.1: A가 넘긴 바톤이 B의 열린 `join` 스트림에 전달.
- AC-F2.2: 비보유자의 `rally done`은 409와 명확한 메시지로 거부.
- AC-F2.3: 들어오는 바톤이 없는 세션에 대한 `rally join --timeout 5s`는 코드 2로 종료.

**알려진 갭.**
- G-F2.1: `rally new`는 데몬을 자동 기동하지 **않음**(squash만). 데몬이 없으면 에러. *권고:* `rally new`에서 자동 기동하거나 데몬 선행조건을 명확히 문서화(감사 Tier 1, 7번).
- G-F2.2: 참가자 이름 오타 시 `--timeout` 없이는 영구 블록. 합리적 기본 타임아웃 권고.

---

### F3 — 자율 사이클 + 참조 드라이버 ✅

**정의.** 경계가 있고 재개 가능한 자율 저장소 작업 단위. cron/CI의 정규 진입점은 `rallish cycle run --once`: **단일 경계 패스를 돌고 종료**하며, 종료코드가 정지 사유를 담는다. 감시 루프가 **아니다**.

**명령 표면.** `cycle new`, `cycle start`(원샷 생성+감시), `cycle run --once`(참조 드라이버), `cycle status`, `cycle ledger`, `cycle next`, `cycle halt`, `cycle watch`.

**종료코드 계약** (`internal/cli/cycle_run.go`, `exitCodeForHalt`):

| 코드 | 정지 사유 |
|------|-----------|
| 0 | 성공 / 정상 패스(전진함) |
| 10 | stuck |
| 11 | 예산 초과 |
| 12 | preflight 실패 |
| 13 | 게이트 실패 |
| 14 | 파싱 불가 턴 |
| 15 | 사용자 요청 |
| 16 | 셀프 감사 위반 |
| 17 | SSH 인증 실패 |
| 18 | 최대 사이클 도달 |
| 19 | 알 수 없는 사유(전방호환) |
| 1 | 운영 오류(상태 읽기 실패, 어댑터 없음) |

**`cycle run --once`의 두 모드.**
- **순수 게이트 파이프라인**(`--agents` 없음): 디스크에서 상태 로드, 부활(reviver) 안티스핀 가드·stuck 브레이커·수명 예산 한도 실행 후, 표준 게이트 파이프라인을 통한 `cycle.Driver.Step()` 1회.
- **에이전트 오케스트레이션**(`--agents claude,kimi`): 같은 전처리 후 멀티에이전트 오케스트레이터에 위임 — 에이전트 턴 1회 + 게이트 스텝 1회.

**재개성.** 상태 파일(`~/.rallish/cycles/cycle-<id>.json`)은 `.bak` 복구와 함께 원자적으로 기록(G1). 정지는 **끈끈하다(sticky)**: 레저에 봉인되어, cron이 부활시킨 스피닝 런이 스스로 정지하고 되살아나지 않는다(안티스핀 부활 가드).

**수용 기준.**
- AC-F3.1: 깨끗하고 전진 가능한 사이클에서 `cycle run --once`는 0 종료, 완료 사이클 수 증가.
- AC-F3.2: stuck 사이클은 10 종료, 레저에 `cycle_halted` 봉인.
- AC-F3.3: 봉인 정지 사이클에 `cycle run --once` 재호출 시 부활하지 않음.

---

### F4 — 어댑터 포트 ✅

**정의.** 임의의 에이전트 런타임 CLI를 꽂게 해주는 최소 2-메서드 포트.

```go
type Adapter interface {
    Name() string
    Run(ctx context.Context, req contract.TurnRequest) (contract.TurnResponse, error)
}
```

**출시 어댑터.**

| 어댑터 | 호출 | 환경변수 허용목록 |
|--------|------|-------------------|
| `claude` | `claude -p <prompt> --max-turns=1` | `PATH, HOME, LANG, TERM, USER, LOGNAME, SHELL, TMPDIR, XDG_CONFIG_HOME, ANTHROPIC_` |
| `kimi` | `kimi -p <prompt>` | …동일 기본… `+ KIMI_` |

> 허용목록 항목은 **정확한 키 또는 접두사**로 매칭됨(`internal/adapter/env.go`, `strings.HasPrefix`). `ANTHROPIC_` / `KIMI_`는 접두사 — 그 이름으로 시작하는 모든 환경변수가 통과. 글롭(`*`)이 아니라 리터럴 접두사다.
| `fake` | 인프로세스 캔드 응답(테스트/데모) | 해당 없음 |

**프롬프트 + 파싱 계약** (`internal/adapter/prompt.go`):
- `BuildPrompt`은 슬림화한 `TurnRequest`를 펜스 JSON으로 임베드하고 `TurnResponse` 스키마를 설명하는 머리말을 붙임.
- `ParseLastJSONBlock`은 **마지막** 펜스 JSON 블록을 추출; 실패 시 균형중괄호 스캔으로 폴백. 둘 다 없으면 `no JSON TurnResponse found in output` 반환.
- 서브프로세스 `cmd.Dir`은 `req.Task.RepoRoot`가 있으면 그것으로 설정. 환경은 허용목록으로 제한(광범위 토큰 유출 없음).

**수용 기준.**
- AC-F4.1: 형식이 올바른 펜스 JSON `TurnResponse`는 `Run`을 통해 변형 없이 왕복.
- AC-F4.2 ✅: 미인증/레이트리밋 런타임 CLI는 실행 가능한 에러를 표면화. 공유 분류기(`internal/adapter/diagnose.go`, `DiagnoseOutput`)가 서브프로세스 stdout/stderr에서 인증·레이트리밋 시그니처를 검사하고, `claude`·`kimi` `Run`은 일치 시 `no JSON TurnResponse found in output` 대신 명확한 메시지("…runtime is not authenticated — run `claude` once interactively to log in…")로 매핑.

**알려진 갭.**
- ✅ G-F4 (해결): 인증/레이트리밋 실패가 어댑터 경계에서 실행 가능한 메시지로 분류되고, `doctor --probe`가 어댑터당 최소 라이브 턴 1회로 인증을 검증(PATH 존재만이 아님). 프로브는 턴을 소비하므로 옵트인이며, 멈춘 로그인 프롬프트가 진단을 막지 못하도록 시간 제한(`probeTimeout`).
- 참고: 어댑터 코드는 `claude`·`kimi`만 출시. Cursor/Codex는 *2-메서드 포트로 추가 가능*하나 미출시.

---

### F5 — 프리셋 시스템 ✅

**스키마** (`internal/preset/preset.go`, `DisallowUnknownField`로 검증):

```yaml
name: <문자열, 필수>
description: <문자열>
roles:                      # ≥1 필수
  - id: <문자열>
    runtime: <claude|kimi|fake>
    model: <모델 힌트 문자열>
routing: <round_robin | handoff_then_round_robin | strict_round_robin | last_writer_wins>
budget:
  max_turns: <int > 0, 필수>
  max_tokens: <int64 > 0, 필수>
  deadline_minutes: <int>
exit_when: [<종료 조건>, ...]
scratch:
  max_kb: <int64>
  summarize_with: <문자열>
```

**출시 프리셋.**

| 프리셋 | 역할 | 라우팅 | 예산 | 종료 조건 |
|--------|------|--------|------|-----------|
| `solo-ralph` | ralph (claude/sonnet) | round_robin | 30턴 · 600k토큰 · 90분 | tests_pass, turns_exhausted, deadline_passed |
| `pair-review` | planner (claude/opus), executor (kimi/k2), reviewer (claude/sonnet) | handoff_then_round_robin | 20턴 · 400k토큰 · 60분 | tests_pass, reviewer_approved, turns_exhausted |

두 출시 프리셋 모두 `scratch: { max_kb: 64, summarize_with: claude-haiku }`도 선언한다. 단 이는 파싱되나 런타임에 **아직 소비되지 않음** — F20 참조.

**검증 규칙.** `name` 필수; 역할 ≥1; `max_turns > 0`; `max_tokens > 0`; routing은 네 이름 중 하나; 미지의 YAML 키 거부(엄격 파싱).

**수용 기준.**
- AC-F5.1: 미지의 최상위 키를 가진 프리셋은 로드 시 거부.
- AC-F5.2: `max_turns: 0` 프리셋은 거부.

---

### F6 — 턴 라우팅 ◑

**결정 우선순위** (`internal/router/router.go`, `Next`):
1. **명시적 핸드오프** — 직전 `TurnResponse.HandoffTo`가 유효 역할이면 그리로.
2. **블록 에스컬레이션** — `prev.SelfEval == "blocked"`이면 `reviewer` 역할이 있으면 그리로; 없으면 에러.
3. **라우팅 규칙** — 프리셋 전략 적용.

**왜 ◑인가.** `round_robin`과 `handoff_then_round_robin`만 구현됨. `strict_round_robin`·`last_writer_wins`는 스키마 검증기는 *통과*시키지만 런타임에 `routing rule %q not supported in phase 1`을 반환. 구현하든지, 존재할 때까지 검증기가 거부하든지 해야 함.

**수용 기준.**
- AC-F6.1: `round_robin`에서 역할 배정은 `(turn-1) mod len(roles)`로 순환.
- AC-F6.2: 유효 역할을 가리키는 `handoff_to`는 라운드로빈을 덮어씀.
- AC-F6.3(갭): `strict_round_robin` 선택이 검증통과-후-런타임실패해서는 안 됨.

---

### F7 — 예산 ✅ / F8 — 종료 조건 ◑

**예산 추적** (`internal/budget/budget.go`). 턴마다: `tokens_left -= tokens_in + tokens_out`; `turns_left -= 1`. 벽시계 데드라인은 저장 후 경과시간과 비교; 감소하지 않음. `IsExhausted` = `turns_left ≤ 0 || tokens_left ≤ 0`.

**수명 한도.** `LifetimeTurns`는 append-only 로그 전체의 `agent_turn` 이벤트를 카운트(부활 넘어 유지); `ExceedsLifetimeCeiling`은 **stuck 브레이커와 구별되는 하드 비용 한도** — stuck 감지기가 절대 못 잡는 *생산적인* 폭주를 멈춘다.

**종료 조건** (`internal/exit/exit.go`):

| 조건 | 평가 |
|------|------|
| `turns_exhausted` | `turns_left ≤ 0` |
| `tokens_exhausted` | `tokens_left ≤ 0` |
| `deadline_passed` | 현재 > 시작 + 데드라인 |
| `reviewer_approved` | 직전 응답 `self_eval == confident` **그리고** `done` |
| `tests_pass` | `go test ./...` 실행(셸 술어) |
| `all_artifacts_compile` | `go vet ./...` 실행(셸 술어) |

**왜 F8이 ◑인가.** 셸 술어(`tests_pass`, `all_artifacts_compile`)는 `allowShell=true`가 필요하나, 브로커는 `allowShell=false`로 평가기를 만들고 의도적으로 `exit_predicate_shell_skipped`를 로깅한다 — 전역 데몬에서 셸을 돌리면 세션 repo가 아닌 데몬 CWD에서 실행되기에, 의도된 보안 자세 선택이다. **결과:** 프리셋 `exit_when: [tests_pass]`는 오늘 squash/브로커 경로에서 발화하지 않음; 수렴은 `reviewer_approved`·`turns_exhausted`·`deadline_passed`에 의존. 셸 술어는 올바른 `cmd.Dir`을 설정하는 사이클 게이트 파이프라인(audit/polish 게이트) 안에서는 *실행됨*.

**수용 기준.**
- AC-F7.1: N토큰 소비 턴은 `tokens_left`를 정확히 N만큼 감소.
- AC-F8.1: `turns_exhausted` 도달 세션은 그 사유로 종료.
- AC-F8.2(문서화된 동작): 프리셋의 `tests_pass`는 브로커 구동 squash를 종료시키지 않음 — 놀라지 않도록 명확히 문서화.

---

### F9 — 게이트 파이프라인 ✅

**단일 진실 출처:** `internal/cycle/gates/pipeline.go`의 `StandardPipeline(auditCmd, polishTestCmd, localGates)`. 브로커와 CLI 원샷이 모두 여기에 위임하므로 순서가 어긋날 수 없다.

**순서:** `Preflight → Audit → [repo-로컬 명령 게이트] → Philosophy → Polish → Commit`.

| 게이트 | 검사 | 실패 의미 |
|--------|------|-----------|
| **Preflight** | 브랜치 ∉ {main, master}; 워킹트리 클린; 베이스라인 SHA 캡처; `next_cycle_goal` 비어있지 않음 | 이 네 검사 중 하나라도 실패 시 정지(종료 12); **SSH 인증은 별도 베스트에포트 검사로 경고만 하고 계속 — 결코 정지하지 않음** |
| **Audit** | `--audit-cmd` 실행(기본 `make check-all`); 공백뿐인 오버라이드는 큰소리로 실패 | 정지(종료 13) |
| **Local 게이트** | 각 `--local-gate` 명령, 순서대로 | 정지(종료 13) |
| **Philosophy** | `git diff <baseline>`에서 ROP / SSOT / SRP / 하드코딩 버전 위반 스캔 | 위반을 처음 발견한 사이클에선 항상 **경고**; 이전 사이클에서 위반이 이미 기록돼 있고 **그리고** 새 위반 수가 그 이전 수를 엄격히 초과할 때만 실패. 이때 정지 사유는 `self-audit-violation` → **종료 16**(13 아님) |
| **Polish** | `--polish-test-cmd` 실행(기본 `go test -race ./...`) | 정지(종료 13) |
| **Commit** | `git add -A` 후 `git commit -m <goal>`; **`--amend`·`--no-verify` 절대 안 씀** | "nothing to commit"은 허용 |

**보증.** main 브랜치 금지와 `--no-verify` 금지는 구조적으로 강제됨(플래그가 코드에 결코 추가되지 않음). 게이트 실행은 첫 실패에서 단락(short-circuit).

**수용 기준.**
- AC-F9.1: `main`에서 실행 시 Preflight에서 종료 12로 정지.
- AC-F9.2: audit 명령 실패 시 Commit 도달 전 종료 13으로 정지.
- AC-F9.3: commit 게이트는 결코 `--no-verify`를 넘기지 않음.

---

### F10 — 안티스핀(stuck 브레이커 + 수명 한도) ✅

**stuck 술어** (`internal/cycle/stuck.go`, 레저에 대한 순수 O(window)):

| 신호 | 임계값 |
|------|--------|
| 반복 턴(동일 summary+files 지문) | ≥ 4 |
| 반복 게이트 실패(동일 gate+summary) | ≥ 3 |
| ping-pong(A,B,A,B 교대, 새 산출물 없음) | ≥ 6 |
| 무진전(최근 K턴이 새 파일 없음 **그리고** `validation_green` 없음) | 윈도우 = 5 |

`orchestrator.RunOnce`에서 호출; 매치 시 `cycle_halted` 기록 후 상태 영속. 수명 예산 한도(F7)도 함께 점검.

**설계 원칙.** *"진전"을 정의하지 말고 "stuck"을 감지하라.* 프론티어-성장-대-사이클은 셀프리포트보다 게이밍이 어렵지만, **게이밍 불가는 아니다**(에이전트가 새 노드를 양산할 수 있음). 유일한 게이밍 불가 신호는 워커가 쓸 수 없는 검증자 산출 그린 게이트(F9)다.

**수용 기준.**
- AC-F10.1: 동일 턴 4회는 반복-턴 브레이커 발동 → 정지 10.
- AC-F10.2: 새 산출물 없는 6턴 ping-pong 발동 → 정지 10.

---

### F11 — 해시체인 레저(G4 감사) ✅

**형식.** append-only JSONL, 줄당 `HarnessLedgerEntry` 하나(`pkg/contract/harness_ledger.go`). 모든 항목은 `schema_version`(현재 `"1"`), `prev_hash`, `hash`를 가진다. `hash` = (canonical 항목에서 `hash`를 0으로 비운 것) ∥ `prev_hash` 에 대한 SHA-256. 제네시스 해시는 64개 0(hex).

**이벤트 타입.** `cycle_created`, `agent_turn`, `gate_passed`, `gate_failed`, `handoff_created`, `cycle_halted`, `cycle_completed`, `validation_green`, `action_denied`, `secret_flagged`, `gates_pinned`, `gate_tampered`, `tooluse_decision`.

**무결성.** `VerifyChain`(순수 리더)은 항목을 순회하며 각 `prev_hash` 링크를 검사하고 각 `hash`를 재계산해 내용 변조를 탐지, 첫 깨진 인덱스 또는 −1 반환.

**라이터.** `LedgerFileSync.Append`(`internal/cycle/ledger.go`)는 경로별 인프로세스 뮤텍스 사용; 파일은 `0600`으로 생성; 줄은 무경계 `bufio.Reader`로 읽음(과대 게이트 리포트에 벽돌이 되는 64 KiB `Scanner` 아님).

**알려진 갭.** 프로세스 간 쓰기 조율은 제공되지 **않음**(`flock` 없음); 사이클당 단일 활성 라이터를 가정. 한 사이클 파일에 두 드라이버를 돌릴 수 있는 구성이라면 이를 문서화하라.

**수용 기준.**
- AC-F11.1: 어떤 항목 내용을 변조하면 `VerifyChain`이 그 인덱스를 반환.
- AC-F11.2: 추가된 모든 항목의 `prev_hash`가 직전 항목 `hash`와 같음.

---

### F12 — Merkle 증명(RFC 9162) ○

**상태: 선언만.** `MerkleRoot`, `InclusionProof`, `VerifyInclusion`, `ConsistencyProof`, `VerifyConsistency`가 `pkg/contract/merkle.go`에 구현·단위테스트됨(RFC 6962/9162 준수, leaf/node 도메인 분리 포함). 그러나 **프로덕션 호출 지점 0**. 라이브러리는 완성됐으나 죽어 있다.

**참으로 만들려면(감사 Tier 2, 10번):** 실제 경로에 연결 — 예: 포함/일관성 증명을 생성하는 `rallish gate verify` / 레저 감사 명령 — 하거나, 그때까지 사용자 문서에서 RFC 9162 증명을 ✅로 표기하지 말 것.

---

### F13 — 액션게이트(G6) ◑ / F14 — 시크릿 격리 ◑

**정의.** 런타임 PreToolUse 훅을 위한 사전 실행 정책 분류기. `rallish gate tooluse --command "<cmd>"`가 결정하고 기록한다; **훅이 종료코드로 강제**한다.

**결정 모델** (`DecideToolUse`, 액션 + 시크릿 중 최고 심각도): `deny` > `needs-hitl` > `allow`. 종료코드: `0` allow, `13` deny, `14` needs-hitl.

**액션 거부목록** (`pkg/contract/action_gate.go`, 정규화 명령에 대한 순수 O(len) 매처):

| 규칙 | 판정 |
|------|------|
| `/`, `/*`, `~`, `$HOME` 대상 `rm -rf` | deny |
| 포크밤 `:(){ :\|:& };:` | deny |
| `dd of=/dev/…`, `mkfs /dev/…`, `/dev/sd*` 또는 `/dev/nvme*` 리디렉션 | deny |
| main/master/release로 `git push --force` / `--force-with-lease` | deny |
| `git reset --hard origin/…` | needs-hitl |
| `DROP TABLE` / `DROP DATABASE` / `TRUNCATE TABLE` | needs-hitl |

**기록.** `--cycle-id`와 함께한 차단 결정(deny/needs-hitl)은 그 사이클 레저에 `tooluse_decision`으로 추가; **안전(allow) 결정은 결코 기록 안 함**(오탐 가드).

**왜 ◑인가.** 정책 분류기와 기록은 연결·테스트됐으나, rallish는 **선언+기록만** 한다 — 실행·가로채기·차단을 하지 않는다. `gate tooluse`를 호출하고 종료코드를 존중하는 사용자 배선 PreToolUse 훅이 없으면 `rm -rf /`는 차단 없이 실행된다. 이는 의도된 경계(rallish는 결코 실행자가 안 됨)지만 README는 G6를 라이브 안전 기능처럼 제시한다.

**참으로 만들려면(감사 Tier 2, 9번):** 훅 배선 + 런북을 출시하거나, 주장을 "정책 선언; 강제는 훅 X 필요"로 낮출 것.

**수용 기준.**
- AC-F13.1: `gate tooluse --command "rm -rf /"`는 deny 결정을 출력하고 13 종료.
- AC-F13.2: 안전 명령은 0 종료, 레저에 아무것도 안 씀.
- AC-F13.3: 배선된 훅으로 거부된 명령이 실제로 거부됨(훅 필요 — 갭 참조).

---

### F15 — IPC ✅

`~/.rallish/rallish.sock`(모드 `0600`)의 Unix 도메인 소켓 우선. CLI는 `~/.rallish/socket` 포인터로 브로커를 해석(`~/.rallish` 하위인지 검증), 300ms 다이얼로 생존 확인, 죽은 포인터 제거, `~/.rallish/port`의 `127.0.0.1:<port>` TCP로 폴백. 루프백 전용 — `0.0.0.0` 절대 안 씀.

**수용 기준.**
- AC-F15.1: 살아있는 소켓이 있으면 CLI는 그것을 사용(TCP 아님).
- AC-F15.2: 죽은 소켓 포인터는 정리되고 TCP 폴백 성공.

---

### F16 — A2A v1.0 와이어 형태 ◑ / F17 — MCP 서버 ✅

**A2A.** `GET /.well-known/agent-card.json`(+ 레거시 `agent.json`) 제공, `protocolVersion`·역량(`streaming: true`)·스킬을 담은 `AgentCard` 반환. JSON-RPC 인테이크는 엄격(`DisallowUnknownFields`). 메서드: send-message, subscribe-to-task, get-task, cancel-task(+ 레거시 `tasks/*` 별칭).

**왜 ◑인가.** A2A SSE 경로는 **`data:` 줄만** 방출하고(`internal/broker/a2a.go`) 명명된 `event:` 타입 줄이 없으며, `A2ATask.sessionId`가 채워지지 않는다. 이벤트 타입으로 분기하는 표준 A2A v1.0 클라이언트는 오늘 실패한다. **MCP** 경로(`internal/broker/mcp.go`)는 이미 명명된 `event:` 줄(`endpoint`, `message`)을 방출하므로 — A2A 경로에 그것을 미러하면 준수에 도달(감사 Tier 2, 12번). 서명 카드와 상호 인증은 보류.

**MCP (F17, ✅).** `GET /mcp/sse` + `POST /mcp/message?session_id=…`, MCP 2025-03-26 핸드셰이크, rally 도구(`rally_create/join/done/status/interrupt`). `rally mcp-agent`는 번들된 원샷 클라이언트.

**수용 기준.**
- AC-F16.1: `GET /.well-known/agent-card.json`은 실제 `protocolVersion`을 가진 카드 반환.
- AC-F16.2: 미지 필드가 있는 JSON-RPC는 거부.
- AC-F16.3(갭): A2A SSE가 명명된 `event:` 줄을 방출하고 `sessionId`를 채움.

---

### F18 — 데몬 자동 기동 ◑ / F19 — doctor ◑

- **F18:** `squash`는 브로커를 자동 기동; `rally`/`cycle` 명령은 실행 중 데몬이 필요. 이를 정렬(`rally new`에서 자동 기동)하거나 선행조건을 문서화. **알려진 코드 버그:** `rallish bootstrap`이 "daemon not running — will auto-spawn on `rally new`"를 출력(`internal/cli/bootstrap.go`)하나 이는 거짓 — `squash`만 자동 기동함. 이 작업을 배선할 때 그 문자열을 수정하라.
- **F19:** `doctor`는 데몬 도달성, PATH의 어댑터 존재, config/skill 경로를 보고. `--probe` 사용 시 어댑터당 시간 제한된 라이브 턴 1회로 **인증**도 검증(G-F4 해결). PATH에 없는 어댑터는 실패가 아닌 정보로 보고.

---

### F20 — 스크래치패드 ○ / F21 — 로그 마스킹 ○

- **F20(선언만):** `internal/scratch`(`Manager`, `Append`, `max_kb`의 80%에서 압축)는 프로덕션 코드에서 **0회** 임포트됨. 프리셋 `scratch:`는 `ScratchConfig`로 파싱되고 `TurnRequest.ScratchPath`도 존재하나, 아무것도 채우거나 소비하지 않음. 연결(세션당 매니저 + 경로 주입 + 어댑터 소비)이 기능을 실제로 만드는 작업.
- **F21(선언만):** `internal/logx`는 2줄 스텁; 로그 시점 시크릿 마스킹이 없음. 마스킹은 사전 실행 명령 분류기(F14)에만 존재하고 로그 출력에는 없음.

---

### F22 — Cross-check ping-pong ▷ (계획됨)

**상태: 명세됨, 미구현.** 전체 명세는 `docs/prd-cross-check-ping-pong.md`. `squash`/`pair-review` 경로에 추가:
- **P0′ 의도 인지 carryover** — `TurnResponse`에 `HandoffIntent`(`continue` / `cross_check`); 브로커가 전달(운반자, 심판 아님), 어댑터 프롬프트 빌더가 프레이밍 선택 → 리뷰어가 실행자 요약을 메아리치지 않고 산출물을 적대적으로 검사.
- **P1′ loop-until-dry + stuck-breaker** — 프리셋 `dry_rounds_threshold` + `exit_when: [dry_rounds]`; `TurnRecord`에 대한 순수 `SessionStuck` 헬퍼.
- **P2′ 검증가능 발견** — 재현 가능한 `Check`를 가진 선택적 `Claims []Violation`; 브로커가 `claim_registered` 레저 이벤트 추가(검증 안 함).
- **P3′ 외부 오라클 앵커** — `ClaimGate`가 `Check.Command`를 실행, `Check.Expected`와 비교, `claim_verified` / `claim_falsified` 방출.

**수용 기준**(PRD에서): executor→reviewer 핸드오프가 `cross_check` 생성; 리뷰어 프롬프트가 실행자 요약을 신뢰하지 않음; 3 dry 라운드는 `dry_rounds`로 종료; 6턴 ping-pong은 `stuck`으로 종료; 통과 claim은 `claim_verified`, 실패 claim은 `claim_falsified` + 사이클 정지; `make check-all` 통과.

**보존할 가드레일:** 브로커 무판단, LangGraph 침범 금지, 코드 정책 아닌 프리셋 정책, claim은 선택적+레저 바운드, `continue`가 기본. 이 기능은 사용성과 직교(감사는 Tier 0–1 이후로 배치).

---

## 4. 횡단 요구사항

*모든* 기능에 적용되며 설계 리뷰 게이트 역할도 한다:

- 모든 경계에서 **parse-don't-validate**; 파싱 불가 입력은 오류(예: 엄격 JSON-RPC 인테이크).
- 인터페이스에서 **관대하지 말고 엄격하게**(RFC 9413 / Postel 비판).
- 공개 표면을 **버전화**(`schema_version` / `protocolVersion`).
- **정직한, 역량 게이트된 명명** — 해시체인 전엔 "audit" 아님, 엄격 파싱 전엔 "conformant" 아님. 이 명세의 성숙도 태그가 그 정직함을 지킨다.
- **셀프리포트가 아닌 구조적 사실을 신뢰**(Goodhart) — 게이밍 불가 신호는 검증자 게이트.
- **무음 폴백 없음 / 전반에 ROP.**

## 5. 구현자가 인계받는 미결 항목

감사 티어링 순서(`docs/reports/2026-06-23-production-readiness-gaps.md`):

1. **Tier 1(첫 실행 UX):** ✅ 어댑터 인증 사전점검(G-F4 — 완료); ✅ `fake-demo` 프리셋(G-F1 — 완료); `rally` 자동 기동 + 기본 타임아웃(G-F2); 오늘 동작하는 설치 경로.
2. **Tier 2(하네스 주장을 참으로):** G6 훅 배선(F13); Merkle 연결(F12); `logx` 마스킹 구현(F21); A2A SSE 명명 이벤트 + `sessionId`(F16).
3. **Tier 3(신뢰):** 실제 어댑터 통합 테스트 + 게이트/autogoal 커버리지(`test-plan.ko.md` 참조); Homebrew tap.
4. **기능 작업:** cross-check ping-pong(F22); 스크래치패드 연결(F20); `strict_round_robin` / `last_writer_wins` 라우팅(F6).
