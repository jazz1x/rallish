# rallish

[![version](https://img.shields.io/badge/version-0.0.1-blue)](CHANGELOG.ko.md)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![go](https://img.shields.io/badge/go-1.25+-blue)](go.mod)

> *다중 에이전트 순차 실행을 위한 로컬 브로커, A2A 호환.*

[English](README.md) · [日本語](README.jp.md)

## rallish란?

rallish는 여러 에이전트 런타임 사이에 위치하는 작은 로컬 브로커 프로세스입니다. 각 런타임은 `claude`, `kimi`, `codex` 등 상용 코딩 CLI입니다. 브로커는 대화 상태를 관리하고, 누구의 차례인지 결정하며, 에이전트 간에 간결한 턴 페이로드를 중계합니다.

와이어 포맷은 합리적인 범위 내에서 **A2A(Agent2Agent) 프로토콜**을 따륯므로, 어댑터를 통해 A2A 호환 에이전트를 연결할 수 있습니다.

## 기능

| 기능 | 설명 |
|------|------|
| **순차 실행** | 공유 브로커를 통해 에이전트가 턴을 번갈아가며 실행 |
| **A2A 프로토콜** | `/.well-known/agent.json`, JSON-RPC 2.0 태스크, SSE 스트리밍 |
| **토큰 예산** | 세션당 토큰, 턴 수, 시간의 상한선을 강제 |
| **스크래치패드** | 자동 압축(compaction)이 적용된 롤링 공유 스크래치 |
| **프리셋** | 역할, 라우팅, 종료 조건을 정의한 YAML 템플릿 |
| **보안** | 경로 탐색 방어, 비밀 정보 마스킹, 최소한의 환경 변수 허용 목록 |

## 빠른 시작

### 사전 요구사항

- Go 1.25+
- `claude` CLI 및/또는 `kimi` CLI 설치 및 인증 완료

### 빌드

```bash
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
```

### 실행

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

## 프리셋

내장 프리셋은 `internal/preset/presets/`에 있습니다:

| 프리셋 | 역할 | 설명 |
|--------|------|------|
| `solo-ralph` | 1 × claude | 예산 제한이 있는 단일 에이전트 실행 |
| `pair-review` | planner, executor, reviewer | 구조화된 리뷰 루프 |

사용자 정의 프리셋은 `~/.rallish/presets/<name>.yaml`에 배치할 수 있습니다.

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
   │   claude   │       │    kimi    │
   └────────────┘       └────────────┘
```

## 보안

전체 위협 모델은 [DESIGN.md](DESIGN.md) §14 및 [docs/handbook.md](docs/handbook.md)를 참조하세요.

## 기여

1. `make check`가 통과해야 합니다 (`go vet`, `golangci-lint`, `go test -race`)
2. Conventional Commits를 따르세요 (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`)
3. `internal/session`, `internal/router`, `internal/exit`, `internal/preset`, `pkg/contract`에서 테스트 커버리지 70% 이상을 유지하세요

## 라이선스

MIT © jazz1x
