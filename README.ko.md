# rallish

> 다중 에이전트 순차 실행을 위한 로컬 브로커, A2A 호환.

![version](https://img.shields.io/badge/version-0.2.0-blue)
![license](https://img.shields.io/badge/license-MIT-green)
![go](https://img.shields.io/badge/go-1.25+-blue)

**rallish**는 여러 에이전트 런타임 사이에 위치하는 작은 로컬 브로커 프로세스입니다. 각 런타임은 어댑터만 있으면 어떤 코딩 CLI(Claude, Kimi, Cursor, Codex 등, 또는 동일 종류라도 서로 다른 컨텍스트에서 실행되는 경우)도 사용할 수 있습니다. 브로커는 대화 상태를 관리하고, 누구의 차례인지 결정하며, 에이전트 간에 간결한 턴 페이로드를 중계합니다.

모든 것은 로컬에서 실행됩니다. 클라우드 브로커나 외부 조정 서비스가 없습니다. 와이어 포맷은 합리적인 범위 내에서 **A2A(Agent2Agent) 프로토콜**을 따륯므로, 어댑터를 통해 A2A 호환 에이전트를 연결할 수 있습니다.

[English](./README.md) · [日本語](./README.jp.md)

## Features

| 기능 | 설명 |
|------|------|
| **Squash (헤드리스)** | `rallish squash`로 헤드리스 프리셋 세션 실행(`solo-ralph`, `pair-review`); 브로커가 어댑터를 자동으로 스폰 |
| **Rally (인터랙티브)** | `rallish rally`로 두 코딩 CLI 세션 간 라이브 바톤 전달; 에이전트가 핑퐁을 자율 루프 (턴마다 사용자 트리거 불필요); SSE를 통한 독점 홀더 강제 |
| **A2A 프로토콜** | `/.well-known/agent.json`, JSON-RPC 2.0 태스크, SSE 스트리밍 |
| **토큰 예산** | 세션당 토큰, 턴 수, 시간의 상한선을 강제 |
| **스크래치패드** | 자동 압축(compaction)이 적용된 롤링 공유 스크래치 |
| **프리셋** | 역할, 라우팅, 종료 조건을 정의한 YAML 템플릿 |
| **Unix 소켓 IPC** | CLI↔Daemon이 `~/.rallish/rallish.sock`(`0600`) 경유. A2A 외부 클라이언트와 Windows 폴백용으로 TCP 루프백 유지 |
| **자동 데몬** | `rallish squash`가 브로커 미실행 시 자동 스폰. `rallish doctor`가 소켓 도달성 보고 |
| **보안** | 경로 탐색 방어, 비밀 정보 마스킹, 최소한의 환경 변수 허용 목록 |

## 아키텍처

```
┌──────────────────────────────────────────┐
│  rallish 브로커 (Go)                     │
│  POST /sessions                          │
│  GET  /sessions/:id/next?as=<role> (SSE) │
│  POST /sessions/:id/turn                 │
│  GET  /.well-known/agent.json            │
│  POST /a2a                               │
└──┬───────────────┬───────────────────┬───┘
   │ unix socket   │ unix socket       │ tcp 루프백
   │ ~/.rallish/   │ ~/.rallish/       │ 127.0.0.1:<port>
   │ rallish.sock  │ rallish.sock      │ (A2A + 폴백)
┌──┴─────────┐   ┌─┴────────┐      ┌──┴───────────┐
│ 에이전트 A │   │에이전트 B│      │ 외부 A2A     │
│  (CLI)     │   │  (CLI)   │      │ 클라이언트    │
└────────────┘   └──────────┘      └──────────────┘
```

같은 브로커가 두 전송 채널을 동시에 서비스합니다. CLI(`rallish squash`, `rallish rally`, `rallish doctor`)는 Unix 소켓을 우선 사용하고, 외부 A2A 클라이언트는 TCP 루프백을 사용합니다.

## 사전 요구사항

- **Go 1.25+** (소스 빌드용)
- 호환되는 에이전트 CLI 하나 이상 설치 및 인증 완료 (지원 어댑터 참조)

확인 방법:

```bash
go version        # 1.25+ 이어야 함
which claude      # $PATH에 있는 지원 어댑터 바이너리
```

## 설치

명령 하나:

```bash
npx skills add jazz1x/rallish
```

스킬 번들(SKILL.md + 바이너리 인스톨러)을 `~/.claude/skills/rallish/`
에 깔아둡니다. [skills.sh](https://www.skills.sh) 경유로 해석.

어떤 프로젝트든 Claude Code (또는 다른 스킬 인식 코딩 CLI) 열고
`랠리보낼 준비해` / `let's serve`. 첫 사용 시 스킬이 번들된 플랫폼 감지
스크립트(`scripts/install-binary.sh`)로 `rallish` 바이너리를 자동 설치
(최신 GitHub Release → `/usr/local/bin` 또는 `~/.local/bin`).

<details>
<summary><b>파워 유저용 (번들 우회)</b></summary>

| 방법 | 명령 |
|---|---|
| **Homebrew tap** (macOS) | _준비 중_ — `jazz1x/homebrew-rallish` tap 리포와 `TAP_GITHUB_TOKEN` 시크릿 설정 후 `brew install jazz1x/rallish/rallish` 작동 예정 |
| **curl** (Unix 전반) | `curl -fsSL https://raw.githubusercontent.com/jazz1x/rallish/main/install.sh \| sh` |
| **소스 빌드** | `git clone https://github.com/jazz1x/rallish && cd rallish && make build` |
| **`go install`** | `go install github.com/jazz1x/rallish/cmd/rallish@latest` |

바이너리가 `$PATH`에 있으면 `rallish bootstrap` (멱등)이 스킬 번들 설치 +
데몬 검증을 수행.
</details>

> ✓ rallish는 프로젝트별이 아닌 사용자별로 한 번만 실행됩니다. 최초 설치 후
> rallish 소스 트리 안에 있을 필요 없이 어디서든 rally를 사용할 수 있습니다.
> 데몬은 `~/.rallish/`에 전역으로 위치합니다. 프로젝트 독립 워크플로우는
> [docs/handbook.md#using-rallish-from-any-project](docs/handbook.md#using-rallish-from-any-project)
> 를 참조하세요.

## 빠른 시작

```bash
# 환경 점검 (어댑터 존재 + 데몬 도달성 보고)
./dist/rallish doctor

# 내장 어댑터/프리셋 목록
./dist/rallish add --list

# 헤드리스 프리셋 세션 시작 (데몬 자동 스폰)
./dist/rallish squash \
  --preset pair-review \
  --task "OAuth2 지원 추가" \
  --repo ./my-project

# 실제 어댑터 없이 스모크 테스트 (fake/결정론적, 3턴)
cat > ~/.rallish/presets/fake-demo.yaml <<'EOF'
name: fake-demo
roles:
  - {id: ralph, runtime: fake, model: fake-1}
routing: round_robin
budget: {max_turns: 3, max_tokens: 10000, deadline_minutes: 5}
exit_when: [turns_exhausted]
scratch: {max_kb: 16}
EOF
./dist/rallish squash --preset fake-demo --task "smoke test" --repo /tmp

# 두 터미널 테니스 랠리 (라이브 바톤 전달)
# skills/rallish 기반 자연어 UX를 권장합니다 —
# 에이전트(Claude Code, Cursor 등)가 모든 rally 명령을 대신 실행합니다.
# 터미널 A 의 코딩 CLI 세션:                   "랠리보낼 준비해 — 사이클로 가자"
# 에이전트: rally new --first server + role=server, SID 출력, 첫 턴 서브, yield.
# 터미널 B 의 코딩 CLI 세션:                   "랠리받을 준비해 <SID>"
# 에이전트: 패턴 파싱, role=returner, 즉시 바톤 수신, yield.
# 각 쪽이 턴을 마치면 에이전트가 사용자에게 제어권을 넘기고,
# 다음 사용자 메시지에 status를 확인해 자기 차례면 계속 진행합니다.
# 턴마다 "내 차례" 트리거 불필요.
# 어느 쪽이든, 언제든지 끝낼 때:               "끝"
#
# Raw CLI (스킬이 내부적으로 호출 / 스크립트에서 사용):
SESSION=$(./dist/rallish rally new --participants server,returner --task "warm-up rally")
./dist/rallish rally status --session-id $SESSION
./dist/rallish rally done   --session-id $SESSION --as server --note "draft v1"

# A2A discovery (외부 클라이언트는 TCP 루프백 사용)
curl http://127.0.0.1:$(cat ~/.rallish/port)/.well-known/agent.json

# A2A 태스크 전송
curl -X POST http://127.0.0.1:$(cat ~/.rallish/port)/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"Hello"}]}}}'
```

턴별 요청/응답은 `~/.rallish/sessions/<id>/log.jsonl`에 기록됩니다.

## 사용법

### 1. 헤드리스 세션 시작

```bash
rallish squash --preset <name> --task "<설명>" --repo <경로>
```

프리셋은 `internal/preset/presets/` (내장) 또는 `~/.rallish/presets/` (사용자 정의)에 있습니다. 프리셋 작성법은 [docs/handbook.md](docs/handbook.md)를 참조하세요.

### 1b. 인터랙티브 랠리 세션 시작

**에이전트 주도 (권장).** 스킬을 자동 발견하는 코딩 CLI(Claude Code, Cursor
등)에서 이 리포를 열면 [`skills/rallish`](skills/rallish/SKILL.md)
스킬이 다음 자연어 트리거로 로드됩니다:

| 발화 | 에이전트 동작 |
|---|---|
| `랠리보낼 준비해` / `let's serve` | `rally new` 실행, ROLE=`server`, SID 출력 |
| `랠리받을 준비해 <SID>` / `let's return` | `rally status` 실행, ROLE=`returner`, 대기 |
| `시작` / `go` (서버 측) | 첫 턴 서브 후 요약 note 와 함께 `rally done` |
| `내 차례` / `is it my turn` (리시버 측) | `rally status`; 자기 차례면 직전 note 읽고 작업 후 `rally done` |
| `끝` / `match over` | 깔끔한 종료 |

`시작` / `go` / `끝` / `내 차례` 같은 짧은 트리거는 직전 prep 트리거로
ROLE+SID 가 이미 설정된 경우에만 활성화 — 무관한 일반 단어는 무시됩니다.

**Raw CLI (스크립트나 스킬 미지원 클라이언트용):**

```bash
rallish rally new    --participants <a>,<b> [--task "<설명>"]
rallish rally join   --session-id <id> --as <이름>          # SSE 블록
rallish rally done   --session-id <id> --as <이름> [--note "<요약>"] [--handoff-to <이름>]
rallish rally status --session-id <id>
```

전체 두 터미널 연습은 [docs/runbook-rally-mode.md](docs/runbook-rally-mode.md)를
참조하세요.

### 2. A2A 연동

A2A 호환 클라이언트는 태스크를 발견하고 전송할 수 있습니다:

| 메서드 | 경로 | 설명 |
|--------|------|------|
| `GET` | `/.well-known/agent.json` | Agent Card |
| `POST` | `/a2a` | JSON-RPC 2.0 (tasks/send, tasks/get, tasks/cancel, tasks/sendSubscribe) |

전체 매핑은 [docs/a2a-compatibility.md](docs/a2a-compatibility.md)를 참조하세요.

### 3. 동일 타입 페어링

Claude 두 개, Kimi 두 개, 또는 어떤 조합도 가능합니다. 브로커는 턴 순서만 관리하며, 벤더는 신경 쓰지 않습니다.

### 4. 예산 상태 확인

예산(토큰, 턴 수, 데드라인)은 세션별로 강제됩니다. 예산이 고갈되면 브로커는 `410 Gone`을 반환하고, 이어서 작업할 수 있도록 스크래치패드를 보존합니다.

### 5. 사용자 정의 프리셋

`~/.rallish/presets/<name>.yaml`에 YAML 파일을 배치하세요:

```yaml
name: my-preset
description: 한 줄 요약(선택).
roles:
  - id: planner
    runtime: claude
    model: opus
  - id: executor
    runtime: kimi
    model: kimi-k2
routing: handoff_then_round_robin    # 또는 round_robin
budget:
  max_turns: 20
  max_tokens: 400000
  deadline_minutes: 60
exit_when: [tests_pass, reviewer_approved, turns_exhausted]
scratch:
  max_kb: 64
  summarize_with: claude-haiku
```

### 6. 데몬 라이프사이클

```bash
rallish daemon &                            # 명시적 기동 (선택 — squash가 자동 스폰)
ls ~/.rallish/                              # rallish.sock (0600), socket, port, sessions/
kill -TERM $(pgrep -f "rallish daemon")     # graceful 종료 시 세 파일 모두 정리
```

`rallish doctor`로 도달성 확인:

```
daemon reachable via unix socket path=/Users/<you>/.rallish/rallish.sock perm=-rw-------
```

Windows에서는 브로커가 TCP 루프백만 사용합니다 (Unix 소켓 스텁이 `ErrUnsupported` 반환).

## 보안

- 브로커는 `127.0.0.1`에만 바인딩됩니다.
- v0에는 인증 레이어가 없습니다. 공유 머신에서는 리버스 프록시 또는 로컬 방화벽을 사용하세요.
- 프리셋 파일은 로드 전 경로 탐색 공격 여부를 검증합니다.
- 환경 변수의 비밀 정보는 로그에서 마스킹됩니다.

전체 위협 모델은 [DESIGN.md](DESIGN.md) §14를 참조하세요.

## 개발

클론 후 한 번만 프리커밋 훅을 활성화하세요:

```bash
make setup-hooks
```

전체 검증 스위트 실행:

```bash
make check   # go vet + golangci-lint + go test -race
```

### 테스트

```bash
make test    # go test ./...
make bench   # go test -bench=. -benchmem ./...
```

커버리지 하한: `internal/session`, `internal/router`, `internal/exit`, `internal/preset`, `pkg/contract`에서 70% 이상.

## 컨벤션

코딩 가이드라인, 프로젝트 레이아웃, 커밋 규칙은 [AGENTS.md](AGENTS.md)를 참조하세요.

## 라이선스

MIT — [LICENSE](./LICENSE) 참조.

> *"랠리처럼, 누구도 공을 독점하지 않을 때 가장 좋은 턴이 만들어진다."*
