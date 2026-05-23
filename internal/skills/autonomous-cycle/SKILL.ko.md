---
name: autonomous-cycle
description: >
  rallish 기반 벤더-중립 자율 리팩터 루프.
  Preflight → Audit → Philosophy → Polish → Commit 게이트 파이프라인으로 N사이클을 주행하며,
  3사이클 주기로 에이전트를 교체하고 위반 발생 시 graceful halt 한다.
  트리거: "autonomous cycle", "자율 사이클", "cycle start", "nightly run", "오토 리팩터"
version: 0.1.0
ssl:
  scheduling:
    when_to_use:
      - "목표가 명확하고 범위가 좁은 장기 기계적 리팩터"
      - "게이트 파이프라인으로 검증 가능한 목표 (사이클마다 인간 판단 불필요)"
      - "기준 SHA와 pending_files를 사전에 열거할 수 있음"
    anti_triggers:
      - "모호한 범위나 진화하는 목표 — rallish rally 대화형 사용"
      - "사이클마다 인간 결정이 필요한 작업 — harnish:forki 사용"
      - "기준 패턴이 없는 첫 시도 — 수동으로 패턴을 먼저 작성"
  structural:
    scenes: [Preflight, CycleLoop, GateCheck, HandoffOrCommit, Reset]
    resumable: true
    branches:
      - "상태 파일 없음 → Preflight가 초기화"
      - "completed_cycles % 3 == 0 (and > 0) → 에이전트 리셋 신호"
      - "self-audit violations > 0 → halt=true, 사용자에게 전달"
      - "SSH 인증 실패 → 경고, 하드 halt 아님"
      - "completed_cycles >= MAX_CYCLES → graceful exit"
  logical:
    tools: [Bash, Read, Write]
    side_effects:
      reads: ["tmp/cycle-*.json", "git HEAD", "ssh git@github.com"]
      writes:
        - "tmp/cycle-*.json (매 사이클 갱신)"
        - "git commits (사이클마다 1개, conventional message)"
      deletes: []
---

# autonomous-cycle

단일 벤더 CLI에 얽매이지 않고 rallish 내부에서 실행되는 밤새 돌려도 안전한 자율 리팩터 루프.
멀티 에이전트 핑퐁 지원: rallish가 3사이클마다 어댑터를 교체한다.

## 동반 파일
- 상태 스키마: `tmp/cycle-<id>.json`
- 브로커 이벤트: `rallish cycle watch --cycle-id <id>`

## 사이클 워크플로우

```
┌─ 사이클 시작 ──────────────────────────────────────┐
│  1. tmp/cycle-<id>.json 읽기 (재개점)              │
│  2. Preflight 게이트 (브랜치, 클린, 목표, SSH)     │
│  3. Audit 게이트 (make check-all)                  │
│  4. Philosophy 게이트 (ROP / SSOT / SRP 스위프)    │
│  5. Polish 게이트 (테스트, 린트, no-raw-ansi)      │
│  6. Commit 게이트 (conventional message, amend 금지)│
│  7. tmp/cycle-<id>.json 업데이트                   │
│  8. 3사이클마다 에이전트 리셋                      │
└────────────────────────────────────────────────────┘
       ↓ rate-limit / token exhaust
   handoff 노트 작성 → graceful exit
```

## 트리거 흐름

### A — 새 사이클 시작 (서버)
```
사용자: "autonomous cycle" / "자율 사이클 시작"
에이전트:
  1. rallish daemon 실행 확인:  rallish doctor
  2. 사이클 생성:  rallish cycle new --goal "feat: refactor adapter package" --max-cycles 5
  3. --agents 설정 시 오케스트레이션 시작:  curl -X POST /cycles/<id>/orchestrate
  4. 사용자에게 cycle-id와 초기 상태 보고.
```

### B — 상태 확인 (언제든)
```
사용자: "cycle status" / "사이클 상태"
에이전트:
  1. rallish cycle status --cycle-id <id>
  2. phase, completed_cycles, violations, halt_reason 요약.
```

### C — 수동 스텝 (디버그 / HITL)
```
사용자: "cycle next" / "다음 사이클"
에이전트:
  1. rallish cycle next --cycle-id <id> --goal "feat: extract helper function"
  2. 새 상태 보고.
```

### D — 중단
```
사용자: "cycle halt" / "사이클 중단"
에이전트:
  1. rallish cycle halt --cycle-id <id> --reason user-requested
  2. 상태 파일에 halt_reason 기록 확인.
```

### E — 이벤트 감시
```
사용자: "cycle watch" / "사이클 감시"
에이전트:
  1. rallish cycle watch --cycle-id <id>
  2. 중단되거나 사용자가 인터럽트할 때까지 SSE 이벤트 스트리밍.
```

## 멀티 에이전트 오케스트레이션

`cycle new`에 `--agents claude,kimi`를 전달하면 브로커가 3사이클마다 어댑터를 교체:

```
사이클 1-3 → claude
사이클 4-6 → kimi
사이클 7-9 → claude
...
```

각 에이전트는 전체 `CycleState`를 `TurnRequest` 페이로드로 받고, 다음을 담은 `TurnResponse`를 반환:
- `next_goal` (문자열)
- `violations_found` ([]Violation)
- `halt_requested` (불리언)

`OrchestratorConfig.RepoURL`로 크로스-리포 오케스트레이션 지원.

## 크로스-벤더 호환성

이 스킬은 벤더별 API가 아닌 `rallish` CLI를 사용한다. 다음과 함께 작동:
- Claude Code
- Kimi Code CLI
- Codex CLI
- Cursor (터미널 경유)

모든 에이전트가 동일한 브로커 엔드포인트를 호출하므로 상태는 벤더-중립적이다.

## 중단 조건 (graceful exit)

- `self-audit` 위반 수 > 0
- SSH 인증 실패 (preflight 경고, 지속 시 에스컬레이션)
- `completed_cycles >= MAX_CYCLES`
- 게이트 비정상 종료
- 사용자 중단 요청

중단 시 항상 `halted=true` + `halt_reason`을 `tmp/cycle-<id>.json`에 기록.

## 안티 패턴

- ❌ `main`에서 실행 (Preflight 게이트 거부).
- ❌ 루프 중 `git commit --amend` (Commit 게이트는 amend하지 않음).
- ❌ 실패한 hook을 `--no-verify`로 무시 (Polish 게이트가 포착).
- ❌ `next_cycle_goal` 없이 실행 (Preflight 게이트 거부).
- ❌ 한 밤에 MAX_CYCLES > 10 (수익递减).
- ❌ 처리량을 위해 Philosophy 게이트 스킵 (안전 장치 무력화).
- ❌ 사이클 간 `sleep` < 30초 (rate limit 위험).
