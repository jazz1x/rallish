# rallish

> 다중 에이전트 순차 실행을 위한 로컬 브로커, A2A 호환.

![version](https://img.shields.io/badge/version-0.0.1-blue)
![license](https://img.shields.io/badge/license-MIT-green)
![go](https://img.shields.io/badge/go-1.25+-blue)

**rallish**는 여러 에이전트 런타임 사이에 위치하는 작은 로컬 브로커 프로세스입니다. 각 런타임은 어댑터만 있으면 어떤 코딩 CLI(Claude, Kimi, Cursor, Codex 등, 또는 동일 종류라도 서로 다른 컨텍스트에서 실행되는 경우)도 사용할 수 있습니다. 브로커는 대화 상태를 관리하고, 누구의 차례인지 결정하며, 에이전트 간에 간결한 턴 페이로드를 중계합니다.

모든 것은 로컬에서 실행됩니다. 클라우드 브로커나 외부 조정 서비스가 없습니다. 와이어 포맷은 합리적인 범위 내에서 **A2A(Agent2Agent) 프로토콜**을 따륯므로, 어댑터를 통해 A2A 호환 에이전트를 연결할 수 있습니다.

[English](./README.md) · [日本語](./README.jp.md)

## Features

| 기능 | 설명 |
|------|------|
| **순차 실행** | 공유 브로커를 통해 에이전트가 턴을 번갈아가며 실행 |
| **A2A 프로토콜** | `/.well-known/agent.json`, JSON-RPC 2.0 태스크, SSE 스트리밍 |
| **토큰 예산** | 세션당 토큰, 턴 수, 시간의 상한선을 강제 |
| **스크래치패드** | 자동 압축(compaction)이 적용된 롤링 공유 스크래치 |
| **프리셋** | 역할, 라우팅, 종료 조건을 정의한 YAML 템플릿 |
| **보안** | 경로 탐색 방어, 비밀 정보 마스킹, 최소한의 환경 변수 허용 목록 |

## 아키텍처

```
┌──────────────────────────────────────────┐
│  rallish 브로커 (Go, 127.0.0.1)         │
│  POST /sessions                          │
│  GET  /sessions/:id/next?as=<role> (SSE) │
│  POST /sessions/:id/turn                 │
│  GET  /.well-known/agent.json            │
│  POST /a2a                               │
└────────▲─────────────────────▲───────────┘
         │                     │
   ┌─────┴──────┐       ┌─────┴──────┐
   │  에이전트 A │       │  에이전트 B │
   └────────────┘       └────────────┘
```

## 사전 요구사항

- **Go 1.25+** (소스 빌드용)
- 호환되는 에이전트 CLI 하나 이상 설치 및 인증 완료 (지원 어댑터 참조)

확인 방법:

```bash
go version        # 1.25+ 이어야 함
which claude      # $PATH에 있는 지원 어댑터 바이너리
```

## 설치

### 옵션 1 — 소스에서 빌드 (개발용 권장)

```bash
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
```

바이너리는 `./dist/rallish`에 생성됩니다.

### 옵션 2 — Homebrew (첫 릴리즈 이후)

```bash
brew tap jazz1x/rallish
brew install rallish
```

### 옵션 3 — go install

```bash
go install github.com/jazz1x/rallish@latest
```

## 빠른 시작

```bash
# 환경 점검
./dist/rallish doctor

# 순차 실행 세션 시작
./dist/rallish start \
  --preset pair-review \
  --task "OAuth2 지원 추가" \
  --repo ./my-project

# A2A discovery
curl http://127.0.0.1:$(cat ~/.rallish/port)/.well-known/agent.json

# A2A 태스크 전송
curl -X POST http://127.0.0.1:$(cat ~/.rallish/port)/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"Hello"}]}}}'
```

## 사용법

### 1. 세션 시작

```bash
rallish start --preset <name> --task "<설명>" --repo <경로>
```

프리셋은 `internal/preset/presets/` (내장) 또는 `~/.rallish/presets/` (사용자 정의)에 있습니다. 프리셋 작성법은 [docs/handbook.md](docs/handbook.md)를 참조하세요.

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

### 4. 사용자 정의 프리셋

`~/.rallish/presets/<name>.yaml`에 YAML 파일을 배치하세요:

```yaml
name: my-preset
roles:
  planner:
    adapter: claude
    model: claude-3-5-sonnet-20241022
routing:
  - planner
exit:
  maxTurns: 10
```

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
