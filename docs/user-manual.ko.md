# rallish — 사용설명서

> 과업 중심 가이드: 설치 → 첫 세션 → squash/rally → 자율 사이클 → 설정 → 트러블슈팅.
> **버전:** `VERSION`(0.3.0) 추종 · **최종 수정:** 2026-06-24 · [English](./user-manual.md)

이 매뉴얼은 자기 머신에서 rallish를 돌리는 **운영자·통합자**를 위한 것이다. *왜* rallish가 이런 모습인지는 `docs/north-star.md`, *무엇이 연결됐고 안 됐는지*는 `docs/feature-spec.ko.md` 참조.

---

## 1. rallish란 (1분 요약)

rallish는 둘 이상의 에이전트 런타임(오늘은 `claude`·`kimi` CLI 출시) 사이에 앉아 그들 간 **턴 교대**를 돌리는 작은 **로컬 브로커**다. 브로커가 대화 상태를 소유하고 누구 차례인지 결정한다. 모든 것이 당신 머신에서 — 클라우드 조율자 없음.

세 가지 방식으로 쓴다:

- **`squash`** — *헤드리스* 프리셋 세션을 던져두면 끝; 브로커가 어댑터를 기동하고 ping-pong을 완료까지 돌림.
- **`rally`** — 라이브 코딩 CLI 세션 간 *대화형* 바톤 패싱.
- **`cycle`** — *자율*·게이트·재개 가능한 저장소 작업; cron/CI 정규 진입점은 `cycle run --once`.

---

## 2. 요구사항

- 지원 OS(Linux/macOS; Windows는 Unix 소켓 대신 TCP 루프백 폴백 사용).
- PATH에 에이전트 CLI 최소 하나: **`claude`** CLI(Claude Code) 및/또는 **`kimi`** CLI. 이들은 **인증되어 있어야** 한다 — rallish는 서브프로세스로 실행하지만 자격증명을 관리하지 않음.
- 소스 빌드 시: **Go 1.25+**.

---

## 3. 설치

동작하는 경로 아무거나 고르면 된다. 이후 `rallish version`으로 확인.

```bash
# 소스에서 (Go 1.25+)
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
sudo install dist/rallish /usr/local/bin/rallish

# go install
go install github.com/jazz1x/rallish/cmd/rallish@latest

# Curl 설치기 (최신 GitHub Release 타르볼 받음; Go 툴체인 불필요)
curl -fsSL https://raw.githubusercontent.com/jazz1x/rallish/main/install.sh | sh
```

> **설치 경로 주의.** GitHub Releases(cosign 서명, SBOM 포함)가 신뢰할 만한 아티팩트 출처다. Homebrew tap은 계획됐으나 아직 미구성. 다른 곳에 skills 레지스트리 원라이너가 참조되더라도, 그 레지스트리가 라이브임이 확인되기 전엔 위 경로를 선호하라.

### Bootstrap (권장 첫 실행)

```bash
rallish bootstrap
```

멱등적. 번들 스킬을 `~/.claude/skills/rallish/`에 설치하고, 몇 개 config 값(기본 프리셋, 코딩 CLI 벤더, wait 모드)을 수집해 `~/.rallish/config.yaml`에 쓰고, 데몬 도달성을 점검한다. 플래그: `--yes`(기본값 수용), `--skip-skill`, `--skip-config`.

---

## 4. 5분 만의 첫 세션

### 4.1 환경 점검

```bash
rallish doctor            # 존재 점검 (빠름, 턴 소비 없음)
rallish doctor --probe    # 각 어댑터 로그인 여부까지 검증 (어댑터당 턴 1회 소비)
```

`doctor`는 데몬 도달성, PATH의 어댑터, config/skill 경로를 보고한다.

기본 `doctor`는 어댑터 바이너리가 *존재·실행 가능*한지만 확인하고 로그인은 하지 않는다. `--probe`를 붙이면 인증을 검증한다: PATH의 각 어댑터가 최소 라이브 턴 1회에 응답하므로, 미인증/레이트리밋 CLI가 나중에 난해한 세션 에러로 터지는 대신 실패한 `adapter:*:auth` 점검으로 드러난다. (프로브는 시간 제한이 있어 멈춘 로그인 프롬프트가 명령을 막지 않는다.)

> `--probe` 없이도, 미인증 CLI로 세션이 실패하면 이제 옛 `parsing response: no JSON TurnResponse found in output` 대신 실행 가능한 메시지("…runtime is not authenticated — run `claude` once interactively to log in…")를 보고한다.

### 4.2 헤드리스 세션 실행(squash)

```bash
rallish squash --preset solo-ralph --task "--version 플래그 추가"
```

`--task`는 새 세션 시작 시 **필수**다. 브로커를 자동 기동(squash가 자동 기동하는 유일한 명령)하고, `solo-ralph` 프리셋 — 30턴/600k토큰/90분 예산의 단일 `claude`/sonnet 에이전트 — 을 돌려 종료 조건 발화 시 종료한다.

세 역할 planner→executor→reviewer 흐름을 쓰려면:

```bash
rallish squash --preset pair-review --task "--version 플래그 추가"   # PATH에 `claude`와 `kimi` 둘 다 필요
```

> **무자격증명 점검.** 자격증명을 소모하지 않고 설치를 검증하려면 프리셋이 `runtime: fake`(인프로세스 테스트 어댑터)를 가리키게 할 수 있다. 아직 번들 `fake` 프리셋이 없으므로, 오늘은 `~/.rallish/presets/`에 작은 YAML을 직접 써야 한다. §6 참조.

---

## 5. 대화형 바톤 패싱(rally)

둘 이상의 라이브 세션이 각자의 코딩 CLI로 구동되며 작업을 주고받게 하고 싶을 때 `rally`를 쓴다.

> **선행조건:** `squash`와 달리 `rally` 명령은 브로커를 자동 기동하지 **않는다**. 먼저 시작하라:
> ```bash
> rallish daemon &     # 또는 별도 터미널에서 실행
> ```

**1. 세션 생성:**

```bash
rallish rally new --participants alice,bob --repo /path/to/repo --task "파서 리팩터"
# 세션 ID 출력
```

`--first alice`는 바톤을 선배정; 아니면 첫 참가자가 `join`해야 시작.

**2. 각 참가자가 합류해 바톤 대기:**

```bash
rallish rally join --session-id <ID> --as alice
```

라이브 SSE 스트림을 열고 바톤이 올 때까지 블록한 뒤, 턴 번호와 지시를 출력한다. 유용한 플래그:
- `--once` — 첫 바톤 후 종료(한 턴 스크립팅에 좋음).
- `--timeout 5m` — 해당 시간 후 포기하고 **코드 2로 종료**(바톤 미수신). `--timeout` 없으면 무한 블록 — 잘못된 이름으로 매달릴 수 있으니 항상 설정하라.

**3. 내 차례가 끝나면 바톤 넘기기:**

```bash
rallish rally done --session-id <ID> --as alice --note "파서를 lexer+parser로 분리" --handoff-to bob
```

내 차례가 아닐 때 `done`을 부르면 명확한 "당신 차례 아님" / "세션 중단" 메시지(HTTP 409, 종료 1)를 받는다 — 아무것도 손상되지 않음.

**4. 언제든 상태 확인:**

```bash
rallish rally status --session-id <ID>
```

상태, 현재 보유자, 턴 번호, 과업, repo, 참가자(stale 표시 포함), 최근 몇 턴을 보여준다.

**MCP 변형.** `rallish rally mcp-agent --mode create|join|done|status|interrupt …`는 raw JSON을 출력하는 원샷 MCP 클라이언트 — MCP 인지 에이전트에 rally를 배선할 때 유용. 여기 `join`은 기본 30초 타임아웃.

---

## 6. 프리셋과 설정

### 6.1 Config

`~/.rallish/config.yaml`에 저장. 관리:

```bash
rallish config list                 # 모든 키, 현재 값, 출처
rallish config get default_preset
rallish config set default_preset pair-review
rallish config path                 # 파일 경로 출력
rallish config edit                 # $EDITOR로 열기(없으면 생성)
```

| 키 | 기본값 | 의미 |
|----|--------|------|
| `default_preset` | `solo-ralph` | `--preset` 생략 시 `squash`가 쓰는 프리셋 |
| `default_adapter` | `claude` | 기본 어댑터 |
| `coding_cli` | `claude` | 벤더: `claude`, `kimi`, `codex` |
| `wait_mode` | `yield` | `yield`(폴) 또는 `block`(블로킹 join) |
| `editor` | (빈값) | `config edit`에서 `$VISUAL`/`$EDITOR` 대체 |
| `telemetry` | `off` | `on` / `off` |

### 6.2 프리셋 해부

프리셋은 YAML 템플릿이다. 빌트인은 바이너리에 컴파일됨; `squash`는 프리셋을 이름으로 먼저 빌트인에서, 그다음 `~/.rallish/presets/<name>.yaml`에서 해석한다. (주의: `squash`는 사용자 프리셋을 `~/.rallish/presets/`에서**만** 찾는다 — `rallish add`가 거기 쓸 수 있더라도 프로젝트 로컬 `./.rallish/presets/`는 읽지 **않는다**.)

```yaml
name: solo-ralph
description: Single runtime running the ralph loop with budget and exit guards.
roles:
  - id: ralph
    runtime: claude        # claude | kimi | fake
    model: sonnet
routing: round_robin       # round_robin | handoff_then_round_robin
                           # (strict_round_robin / last_writer_wins 은 아직 미구현)
budget:
  max_turns: 30            # 필수, > 0
  max_tokens: 600000       # 필수, > 0
  deadline_minutes: 90
exit_when:
  - tests_pass
  - turns_exhausted
  - deadline_passed
scratch:
  max_kb: 64
  summarize_with: claude-haiku
```

**라우팅.** 오늘은 `round_robin`과 `handoff_then_round_robin`만 동작; 나머지 두 이름은 파싱되나 런타임에 실패. 턴의 명시적 `handoff_to`는 항상 우선; `blocked` self-eval은 `reviewer` 역할이 있으면 그리로 에스컬레이션.

**종료 조건.** `turns_exhausted`·`tokens_exhausted`·`deadline_passed`·`reviewer_approved`는 squash/브로커 경로에서 동작. **`tests_pass` / `all_artifacts_compile`은 브로커 구동 squash를 종료시키지 않음** — 브로커가 셸 술어를 의도적으로 건너뜀(세션 repo가 아닌 데몬 디렉터리에서 실행될 것이므로). 그 셸 검사는 자율 사이클 게이트 파이프라인 안에서는 *실행됨*. `exit_when`을 그에 맞게 계획하라: `squash`에선 `reviewer_approved`/`turns_exhausted`/`deadline_passed`에 의존.

**최소 무자격증명 프리셋** (`~/.rallish/presets/fake-demo.yaml`에 투입):

```yaml
name: fake-demo
description: 인프로세스 fake 어댑터를 쓰는 무자격증명 스모크 테스트.
roles:
  - id: ralph
    runtime: fake
    model: none
routing: round_robin
budget: { max_turns: 3, max_tokens: 1000, deadline_minutes: 5 }
exit_when: [turns_exhausted]
scratch: { max_kb: 8, summarize_with: none }
```

```bash
rallish squash --preset fake-demo --task "스모크 테스트"     # API 자격증명 없이 완료
```

### 6.3 컴포넌트 추가

```bash
rallish add --list                          # 빌트인 어댑터/프리셋/스킬 보기
rallish add preset pair-review              # 빌트인 프리셋을 로컬 설치
rallish add preset my-preset --from <URL>   # 다운로드
rallish add adapter <name> --global         # ./.rallish 대신 ~/.rallish에 설치
```

---

## 7. 자율 사이클

**사이클**은 경계가 있고 게이트되며 재개 가능한 저장소 작업이다. 에이전트가 가드레일 아래 실제 커밋을 만들게 하고 싶을 때 쓴다.

### 7.1 생성과 실행

```bash
# 사이클 생성 (실행 안 함)
rallish cycle new --goal "HTTP 클라이언트에 재시도 추가" --branch feat/retry

# 원샷: 생성, 선택적 에이전트 오케스트레이션, 정지까지 감시
rallish cycle start --goal "HTTP 클라이언트에 재시도 추가" --agents claude

# cron/CI용 참조 드라이버: 정확히 한 경계 패스 실행 후 종료
rallish cycle run --once --cycle-id <ID> --agents claude,kimi
```

유용한 `cycle new`/`start` 플래그(명령마다 기본값이 다름에 주의): `--max-cycles`(`cycle new` 기본 **10**; `cycle start` 기본 **0**=무제한), `--max-duration` 분(`cycle new` 기본 **0**=무제한; `cycle start` 기본 **240**=4시간), `--auto-goal` 각 사이클 후 다음 목표 발견(`cycle new` 기본 **false**; `cycle start` 기본 **true**), `--local-gate`(반복 가능한 추가 명령). **`cycle new` 전용**(`cycle start`엔 등록 안 됨): `--audit-cmd`(audit 게이트 오버라이드, 기본 `make check-all`; 예 `npm test`), `--polish-test-cmd`(polish 테스트 오버라이드, 기본 `go test -race ./...`).

### 7.2 사이클 패스가 하는 일

각 패스는 **게이트 파이프라인**을 돈다: `Preflight → Audit → [로컬 게이트] → Philosophy → Polish → Commit`. 게이트 실패 시 커밋 대신 **정지**한다. 요점:
- **Preflight**는 `main`/`master`에서 실행 거부, 클린 트리와 비어있지 않은 목표 요구.
- **Commit**은 `--amend`나 `--no-verify`를 결코 안 씀.
- **stuck 브레이커**가 턴 반복·게이트 실패 반복·ping-pong·무진전 루프를 정지.
- 정지는 **끈끈함** — cron이 부활시킨 스피닝 런이 스스로 정지하고 되살아나지 않음.

### 7.3 종료코드(cron/CI용)

`cycle run --once`는 정지 사유를 종료코드에 인코딩: `0` 정상 패스, `10` stuck, `11` 예산 초과, `12` preflight 실패, `13` 게이트 실패, `14` 파싱 불가 턴, `15` 사용자 요청, `16` 셀프 감사 위반, `17` SSH 인증 실패, `18` 최대 사이클 도달, `19` 미지, `1` 운영 오류. 스케줄러를 이들에 따라 분기하라.

### 7.4 검사와 제어

```bash
rallish cycle status  --cycle-id <ID>     # 단계, 카운트, 브랜치, 목표, 정지?, 위반
rallish cycle ledger  --cycle-id <ID>     # append-only 해시체인 감사 추적(JSON)
rallish cycle watch   --cycle-id <ID>     # 라이브 SSE 이벤트 스트림(정지 시 종료)
rallish cycle next    --cycle-id <ID>     # 한 스텝 전진(수동 디버그)
rallish cycle halt    --cycle-id <ID> --reason "중단"
```

상태와 레저는 `~/.rallish/cycles/cycle-<ID>.json`, `cycle-<ID>-ledger.jsonl`에 있다.

---

## 8. 액션 게이트(선택적 안전 훅)

rallish는 보류 명령이 위험한지 **결정**할 수 있으나, 스스로 실행·차단하지 **않는다** — 런타임 PreToolUse 훅이 종료코드로 결정을 강제한다.

```bash
rallish gate tooluse --command "rm -rf /"      # 13 종료 (deny)
rallish gate tooluse --command "ls -la"        # 0 종료  (allow)
```

종료코드: `0` allow, `13` deny, `14` 사람 개입 필요(needs-hitl). 에이전트의 PreToolUse 훅에 배선해 훅이 거부(13)하거나 에스컬레이션(14)하게 하라. 그 배선이 없으면 게이트는 *보고만* 하며 명령을 멈출 수 없다. 차단 결정은 `--cycle-id`로 사이클 레저에 기록 가능.

거부목록은 루트/홈의 `rm -rf`, 포크밤, 디스크 덮어쓰기(`dd`/`mkfs` to 디바이스), 보호 브랜치로의 force-push를 포함; `git reset --hard origin/…`과 `DROP/TRUNCATE TABLE`은 사람 검토로 에스컬레이션.

---

## 9. 데몬 실행

```bash
rallish daemon          # 포그라운드; 127.0.0.1:<동적> 바인딩, Unix 소켓 생성
```

데몬은 포트를 `~/.rallish/port`에, 소켓 포인터를 `~/.rallish/socket`에 쓴다(소켓 모드 `0600`). 살아있는 데몬이 이미 소켓을 소유하면 기동을 거부하고, 기동 시 비정상 종료의 stale 소켓을 정리한다. SIGINT/SIGTERM을 우아하게 처리(활성 rally 세션 종료 후 셧다운). CLI는 Unix 소켓을 선호하고 자동으로 TCP 루프백 폴백.

---

## 10. 트러블슈팅

| 증상 | 유력 원인 | 해결 |
|------|-----------|------|
| `parsing response: no JSON TurnResponse found in output` | 어댑터 CLI 미인증/레이트리밋 | CLI 손으로 실행(`claude -p hi`); 로그인; 재시도 |
| `rally new`가 데몬 없음 에러 | `rally`/`cycle`은 자동 기동 안 함 | `rallish daemon &` 먼저 시작 |
| `rally join`이 영구 블록 | 잘못된 참가자 이름 + 타임아웃 없음 | 항상 `--timeout` 전달; `rally status`로 유효 이름 확인 |
| `rally done` → "당신 차례 아님"(409) | 바톤 미보유 | `rally status` 확인; 차례 대기 |
| squash 종료 조건이 `tests_pass`로 발화 안 함 | 브로커가 설계상 셸 술어 건너뜀 | squash엔 `reviewer_approved`/`turns_exhausted`/`deadline_passed` 사용; 셸 검사는 사이클 게이트에서 실행 |
| 사이클이 시작 안 됨 | `main`/`master`, dirty 트리, 빈 목표 | 피처 브랜치로 전환, 커밋/스태시, `--goal` 설정 |
| 사이클이 즉시 12로 종료 | preflight 실패 | 위 참조; `cycle status` 확인 |
| `doctor`에서 어댑터 "not found on PATH" | CLI 미설치/PATH에 없음 | `claude` 또는 `kimi` CLI 설치 |
| 소켓으로 데몬 도달 불가 | stale 소켓 포인터 | CLI가 자동 정리하고 TCP로 폴백; 또는 `~/.rallish/socket` 제거 후 데몬 재시작 |

**파일 위치:** config `~/.rallish/config.yaml`; 데몬 포트/소켓 `~/.rallish/port`, `~/.rallish/socket`, `~/.rallish/rallish.sock`; 세션 `~/.rallish/sessions/`; 사이클 `~/.rallish/cycles/`; 스킬 `~/.claude/skills/rallish/`.

**더 자세히:** 헬스 스냅샷은 `rallish doctor`; 자율 런의 전체 감사 추적은 `rallish cycle ledger --cycle-id <ID>`; 권위 있는 플래그 목록은 `rallish <command> --help`.

---

## 11. 명령 빠른 참조

```
rallish bootstrap                 원스텝 셋업 (스킬 + config + 데몬 점검)
rallish doctor                    어댑터·데몬·config 진단
rallish version                   빌드 버전 / 커밋 / 날짜

rallish squash --preset <name> --task <설명>   헤드리스 프리셋 세션 (--task 필수; 데몬 자동 기동)

rallish daemon                    브로커 실행 (rally/cycle에 필요)
rallish rally new|join|done|status|mcp-agent     대화형 바톤 패싱

rallish cycle new|start|run --once|status|ledger|watch|next|halt   자율 작업
rallish gate tooluse --command <cmd>             사전 실행 정책 결정 (훅이 강제)
rallish trigger <phrase>                         자연어 문구로 스킬 실행 (예: autonomous-cycle)

rallish config list|get|set|path|edit           설정
rallish add [adapter|preset|skill] <name>        컴포넌트 설치
rallish skill install --name <skill>             번들 스킬 설치
```

권위 있는 전체 플래그 목록은 어느 명령이든 `--help`로 실행하라.
