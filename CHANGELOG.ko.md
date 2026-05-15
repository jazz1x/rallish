# 변경 이력

이 프로젝트의 모든 주요 변경 사항이 이 파일에 기록됩니다.

형식은 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)를 기반으로 하며,
이 프로젝트는 [Semantic Versioning](https://semver.org/spec/v2.0.0.html)을 준수합니다.

## [미발표]

### 추가됨

- CLI ↔ 데몬 기본 전송 채널로 Unix 도메인 소켓(`~/.rallish/rallish.sock`) 추가.
  TCP 루프백은 폴백 및 A2A 클라이언트용으로 유지. 데몬은 소켓 권한을 `0600`으로
  강제하고, CLI는 소켓 포인터 변조를 차단. Windows 빌드는 빌드태그 스텁을 통해
  TCP로 폴백.
- `rallish doctor`가 소켓 경유 데몬 도달 가능 여부를 보고.
- A2A 프로토콜 레이어: `GET /.well-known/agent.json`, `POST /a2a` (JSON-RPC 2.0)
  - `tasks/send`, `tasks/get`, `tasks/cancel`, `tasks/sendSubscribe` (SSE)
- AgentCard, TaskState, JSON-RPC 봉투를 포함한 `pkg/contract/a2a.go`
- 브로커의 토큰 예산 강제 적용 (`handleNextTurn`)
- `max_kb` 초과 시 자동 압축(compaction)이 있는 `internal/scratch/scratch.go`
- 어댑터 프롬프트에 모델 힌트 주입

### 변경됨

- 모든 "hocket" 용어 제거; "turn-taking" / "relay"로 대체
- `lefthook.yml`을 스테이징된 Go 파일만 스캔하도록 업데이트
- `.golangci.yml`을 v2 형식으로 업그레이드하고 `.toolchain` 제외
- `internal/cli/start.go`의 forbidigo 및 errcheck 위반 수정

### 수정됨

- 레지스트리에서 실제 Claude/Kimi 어댑터 복원
