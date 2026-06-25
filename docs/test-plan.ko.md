# rallish — 테스트 및 품질 계획

> 테스트 전략, 커버리지 목표, 그리고 메워야 할 구체적 갭 — 프로덕션 준비도 감사(`docs/reports/2026-06-23-production-readiness-gaps.md`)에 정박.
> **버전:** `VERSION`(0.3.0) 추종 · **최종 수정:** 2026-06-24 · [English](./test-plan.md)

## 1. 품질 목표

rallish는 강한 주장을 한다 — *안전·재개가능·검증가능·감사가능*. 테스트 스위트의 임무는 그 주장을 열망이 아니라 **참이고 입증 가능하게** 만드는 것이다. 우선순위 순 세 목표:

1. **연결된 표면이 정확하고 레이스 청결하다.** `feature-spec.ko.md`에서 ✅로 태그된 모든 것은 동시성을 포함해 테스트로 커버돼야 한다.
2. **광고된 표면이 정직하게 테스트된다.** 기능이 ◑/○인 곳에선 테스트가 *실제 동작하는 것*을 반영하고, 갭이 가시화돼야 한다(항상 통과하는 스텁 뒤에 숨지 않음).
3. **스텁이 아닌 실제 경로가 행사된다.** 자율 사이클 경로는 현재 서브프로세스를 결코 건드리지 않는 `fake` 어댑터로만 엔드투엔드 테스트됨. 적어도 하나의 실제 서브프로세스 통합 테스트가 존재해야 한다(게이트/선택적).

## 2. 현재 상태(베이스라인)

- **규모:** 비-테스트 Go 파일 ~100개, 테스트 파일 ~73개, 테스트 패키지 26개.
- **레이스:** CI는 `go test ./... -race -count=1`을 실행(`scripts/check-all.sh`); 26개 테스트 패키지 모두 그린. 2026-06-23 감사의 일회성 강화 패스에서 브로커를 추가로 `-race -count=5`로 5회 깨끗이 돌렸음 — 이는 감사 검증이지 상시 CI 스텝이 아님.
- **벤치마크:** 마이크로 벤치마크 5개(`performance-spec.ko.md` §4.1).
- **알려진 저/제로 커버리지**(감사에서): 실제 `claude`(~40%)·`kimi`(~28.9%) 어댑터 `Run()` 경로, 실제 경로의 `adapter.BuildPrompt`, 게이트 `StandardPipeline`(`PreflightGate`/`CommitGate`/`PhilosophyGate`, ~45.3%), 자동 목표 발견 로직(`cycle` 패키지의 `discoverNextGoal`와 헬퍼 — 외부에서 부를 수 있는 `autogoal.Run` 심볼은 없음; 비공식 약칭), `doctor`(~24.6%)가 저커버 또는 0%.
- **커버리지 바닥:** ≥70% 바닥이 `AGENTS.md`/`README`에 문서화됐으나 **아직 CI에서 강제되지 않음**.

## 3. 테스트 분류

| 레벨 | 범위 | 도구 | 위치 |
|------|------|------|------|
| **단위** | 순수 함수, 계약, 술어 | `go test`, `testify` | 패키지별 `*_test.go` |
| **통합(인프로세스)** | `fake`로 배선된 브로커 + 라우터 + 어댑터 | `go test` | `internal/integration_test.go`, `internal/broker/*_test.go` |
| **통합(실제 서브프로세스)** | 실제 `claude`/`kimi` `Run()` 엔드투엔드 | 빌드태그/환경게이트 | 신규: `internal/adapter/*/integration_test.go` |
| **적대적** | 배타적 보유자, 레이스, 변형 입력 | `-race`, 테이블 주도 | 예: `internal/cli/rally_adversarial_test.go` |
| **준수** | A2A v1.0 와이어 형태, MCP 핸드셰이크 | golden + 클라이언트 프로브 | `internal/broker/a2a_test.go`, mcp 테스트 |
| **속성 / 퍼즈** | 파서(`ParseLastJSONBlock`, JSON-RPC 인테이크) | `go test -fuzz` | 신규 |
| **벤치마크** | 핫패스 + 스케일링 | `go test -bench -benchmem`, `benchstat` | `performance-spec.ko.md` 참조 |

## 4. 커버리지 목표

패키지별 바닥(문서화된 ≥70%를 CI 강제로 격상):

| 패키지 | 바닥 | 이유 |
|--------|------|------|
| `pkg/contract` | 85% | 계약이 SSOT; 레저·게이트·액션게이트·merkle이 여기 |
| `internal/router` | 80% | 라우팅 정확성이 모든 세션을 게이트 |
| `internal/exit`, `internal/budget` | 80% | 종료 + 안티폭주 로직 |
| `internal/cycle` + `internal/cycle/gates` | 80% | 차별점; 게이트 파이프라인 + stuck 브레이커 |
| `internal/session`, `internal/preset`, `internal/ipc` | 75% | 핵심 메커니즘 |
| `internal/broker`(incl. `rally.go`) | 75% | 라이브 표면 |
| `internal/adapter`(claude/kimi `Run`) | 60% + 게이트된 실제 테스트 ≥1 | 서브프로세스 경로는 완전 단위테스트가 어려움 |
| `internal/doctor` | 70% | 첫 실행 UX가 의존 |

**강제:** 패키지별 `go test -coverprofile`을 돌려 바닥 미만 시 실패하는 CI 스텝 추가. 그 전까지 바닥은 권고이고 이 표가 목표다.

## 5. 기능별 테스트 요구

`feature-spec.ko.md` ID에 매핑. 각 불릿은 존재해야 할 테스트다.

### F2/F17 Rally + MCP (✅)
- A가 넘긴 바톤이 B의 열린 SSE 스트림에 전달.
- 비보유자 `done` → 409; 보유자 `done`은 턴 전진.
- 동시 `done`/`join` 하 배타적 보유자(레이스 테스트) — 이중 보유 없음.
- `rally join --timeout`은 바톤 없을 때 코드 2 반환; `--once`는 하나 후 종료.
- MCP 2025-03-26 핸드셰이크 + 각 도구(`create/join/done/status/interrupt`) 왕복.

### F3/F9/F10 Cycle + 게이트 + 안티스핀 (✅, 차별점)
- 파이프라인 순서가 정확히 `Preflight→Audit→[local]→Philosophy→Polish→Commit`; 첫 실패 단락.
- Preflight가 `main`/`master`, dirty 트리, 빈 목표에서 정지(종료 12).
- Commit 게이트가 결코 `--amend`/`--no-verify` 방출 안 함(구성된 git 인자 검사로 단언).
- Audit/Polish가 `--audit-cmd`/`--polish-test-cmd` 존중; 공백뿐 오버라이드는 큰소리로 실패.
- stuck 브레이커가 4신호 각각(반복턴 ≥4, 반복게이트실패 ≥3, ping-pong ≥6, 무진전 윈도우 5)에 발동 → 정지 10.
- 수명 한도가 stuck 브레이커가 놓칠 *생산적* 폭주를 정지(예산 초과, 11).
- **부활 가드:** 봉인 정지 사이클을 `cycle run --once`로 재호출 시 부활 안 함.
- **재개성:** 사이클 중간에 죽이고, `.bak`에서 복구, `state.ID`에서 재개.
- 모든 정지 사유가 올바른 종료코드에 매핑(테이블 주도 `exitCodeForHalt`).

### F11/F12 레저 + Merkle (✅ / ○)
- `VerifyChain`이 변조된 항목의 인덱스 반환; 온전한 체인엔 통과.
- 추가된 각 항목 `prev_hash` == 직전 `hash`; 제네시스는 64개 0.
- 모든 항목에 `schema_version` 스탬프; 형태 변경 탐지 가능.
- 과대 게이트 리포트 항목이 올바르게 읽힘(무경계 리더, 64 KiB Scanner 아님).
- Merkle: 포함/일관성 증명이 RFC 6962/9162 벡터에 검증(이미 테스트됨). **F12 연결 후 신규 요구:** 레저 감사 명령이 실제 포함 증명을 생성·검증하는 엔드투엔드 테스트.

### F13/F14 액션게이트 (◑)
- 각 deny 규칙 → 종료 13; 각 HITL 규칙 → 종료 14; 안전 → 종료 0.
- `--cycle-id`와 함께한 차단 결정은 `tooluse_decision`으로 기록; 안전 결정은 아무것도 기록 안 함.
- **신규 요구(Tier 2):** PreToolUse 훅 배선 엔드투엔드 테스트 — 거부된 명령이 훅에 의해 실제로 거부됨(단순 보고 아님). 훅 출시 전까진 이 테스트가 갭을 문서화.

### F4 어댑터 (✅ 포트, 저커버 실제 경로)
- `BuildPrompt`이 슬림화 `TurnRequest`와 `TurnResponse` 스키마를 임베드; `continue` 대 (향후) `cross_check` 프레이밍 차이.
- `ParseLastJSONBlock`: 마지막 펜스 블록 우선; 균형중괄호 폴백; 부재 시 명확한 에러 → **퍼즈 타깃**.
- 환경 허용목록: 허용된 변수만 서브프로세스 도달(광범위 토큰 유출 없음).
- **Tier 3 실제 서브프로세스 테스트**(빌드태그 `//go:build integration`, `RALLISH_IT=1`과 인증된 CLI 없으면 스킵): 실제 `claude -p` 턴이 `TurnResponse` 왕복. 이 테스트 하나가 최대 신뢰 갭을 메움.

### F6/F8 라우팅 + 종료 (◑)
- 라운드로빈 인덱스 산술; 핸드오프 우선; blocked→reviewer 에스컬레이션.
- **갭 테스트:** `strict_round_robin`/`last_writer_wins` 선택이 검증통과-후-런타임실패하면 안 됨(현재 그럼) — 의도 동작 단언, pending 표시.
- 문서-as-테스트: 프리셋의 `tests_pass`는 브로커 squash를 종료시키지 **않음**(`exit_predicate_shell_skipped` 로깅 단언), 사이클 게이트 파이프라인 안에선 발화 *함*.

### F16 A2A 준수 (◑)
- 에이전트 카드가 실제 `protocolVersion` 보유; 미지 JSON-RPC 필드 거부.
- **갭 테스트(Tier 2):** A2A SSE가 명명된 `event:` 줄을 방출하고 `sessionId`를 채움 — 지금 테스트 작성(예상실패/pending)해 연결 시 레드가 그린으로 뒤집히게.

### F22 Cross-check ping-pong (▷ 계획됨)
PRD §5 테스트 계획이 이 기능의 수용 스위트: intent+claims 계약 왕복; 브로커가 intent를 다음 요청에 전달; 3 dry 라운드 → `dry_rounds`; 6턴 ping-pong → `stuck`; `continue` 대 `cross_check` 프롬프트 차이; `ClaimGate`가 검증/반증하고 레저 이벤트 방출; 프리셋이 `dry_rounds_threshold` 파싱.

## 6. 최고가치 추가 3종(감사 Tier 3)

1. **실제 어댑터 통합 테스트 하나.** 게이트·선택적이나 실제: `claude`/`kimi` `Run()`을 단일 턴 구동하고 파싱된 `TurnResponse` 단언. 가장 중요한 커버리지 갭 — 자율 경로가 현재 `fake` 스텁으로만 행사됨.
2. **게이트 파이프라인 커버리지 ~80%로.** `PreflightGate`·`CommitGate`·`PhilosophyGate`·자동 목표 발견 로직(`cycle` 패키지의 `discoverNextGoal`)은 차별점이며 저커버. 임시 git repo에 대해 각 게이트의 통과/경고/실패 분기 테이블 주도 테스트 추가.
3. **CI 커버리지 바닥 강제.** 문서화된 ≥70% 바닥을 §4대로 실패 CI 게이트로 전환.

## 7. CI & 도구

- **pre-commit / pre-push:** `lefthook.yml`이 로컬 훅 실행; `scripts/check-all.sh`와 `make check-all`이 audit 게이트 명령(사이클 `--audit-cmd` 기본값이기도).
- **린트:** `.golangci.yml`; `scripts/check-no-raw-ansi.sh` 가드.
- **필수 CI 매트릭스:** `go test ./...`, `go test -race ./...`(최소 브로커와 동시성 민감 패키지), `go vet ./...`, `golangci-lint run`, 그리고(처음엔 권고) 커버리지 바닥·벤치마크 퇴행 게이트.
- **결정성:** 모든 테스트는 결정적이어야 함(fake 클럭 사용 — `budget`/`exit`/`session`에 `Clock` 인터페이스 존재; 타임아웃 명시 테스트 외 벽시계 sleep 없음).
- **보안 스캔:** repo에 이미 `.gitleaks.toml`·`.trivyignore` 존재; 시크릿·취약점 스캔을 CI에 유지.

## 8. 변경별 완료 정의(DoD)

변경이 완료되는 조건:
- 신규/변경 동작에 단위 테스트; 동시성 민감 변경엔 `-race` 테스트.
- 손댄 패키지 커버리지가 §4 바닥 아래로 떨어지지 않음.
- `make check-all` 통과(자율 사이클이 돌리는 동일 게이트).
- 계약 변경 시: 왕복 JSON 테스트와 형태 변경 시 `schema_version` 범프.
- 기능을 ○/◑에서 ✅로 뒤집는 것: §5의 해당 "갭 테스트"가 pending에서 그린으로 뒤집히고, 같은 변경에서 `feature-spec.ko.md`의 성숙도 태그 갱신(정직한 명명).

## 9. "품질이 명세됨"의 수용 기준

- [ ] `feature-spec.ko.md`의 모든 ✅ 기능이 §5에 나열된 테스트를 가짐.
- [x] 게이트된 실제 어댑터 통합 테스트가 최소 하나 존재하고 문서화됨(§6.1).
- [x] 게이트 파이프라인 패키지가 §4 바닥 도달(§6.2).
- [x] CI가 패키지별 커버리지 바닥 강제(§6.3).
- [ ] `ParseLastJSONBlock`과 JSON-RPC 인테이크에 파서 퍼즈 타깃 존재.
- [ ] 명명된 갭(A2A SSE 이벤트, G6 훅 강제, 라우팅 검증통과-후-실패)에 pending/예상실패 테스트 존재 — 각 갭을 메우면 레드가 그린으로.
