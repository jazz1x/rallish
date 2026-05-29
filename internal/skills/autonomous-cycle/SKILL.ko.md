---
name: autonomous-cycle
description: >
  rallish 기반 벤더-중립 자율 리팩터 루프.
  Preflight → Audit → Philosophy → Polish → Commit 게이트 파이프라인으로 N사이클을 주행하며,
  3사이클 주기로 에이전트를 교체하고 위반 발생 시 graceful halt 한다.
  4–5시간 장기 실행 모드를 지원하며, 20분 단위 감시 라운드로 가드레일 보완과 철학 준수를 확보한다.
  트리거: "autonomous cycle", "자율 사이클", "cycle start", "nightly run", "오토 리팩터",
          "long run", "20분 감시", "가드레일 보완", "철학 보완"
version: 0.2.0
ssl:
  scheduling:
    when_to_use:
      - "목표가 명확하고 범위가 좁은 장기 기계적 리팩터"
      - "게이트 파이프라인으로 검증 가능한 목표 (사이클당 인간 판단 불필요)"
      - "기준 SHA와 pending_files를 사전에 열거할 수 있음"
      - "20분 단위 인간 체크인이 포함된 4–5시간 야간 배치"
      - "가드레일 보완: 자율점검 → 수정 → 재검증 → 철학 스위프"
    anti_triggers:
      - "모호한 범위나 진화하는 목표 — rallish rally 대화형 사용"
      - "사이클당 인간 결정이 필요한 작업 — harnish:forki 사용"
      - "기준 패턴이 없는 첫 시도 — 수동으로 패턴을 먼저 작성"
      - "명시적 오버라이드 없이 MAX_CYCLES > 10 — 수익递减"
  structural:
    scenes: [Preflight, CycleLoop, GateCheck, GuardrailHarden, PhilosophySweep, HandoffOrCommit, Reset, WatchRound]
    resumable: true
    branches:
      - "상태 파일 없음 → Preflight가 초기화"
      - "completed_cycles % 3 == 0 (and > 0) → 에이전트 리셋 신호"
      - "self-audit violations > 0 → halt=true, 사용자에게 전달"
      - "SSH 인증 실패 → 경고, 다음 사이클 재시도"
      - "completed_cycles >= MAX_CYCLES → graceful exit"
      - "마지막 감시 후 20분 경과 → 인간 체크인 라운드"
  logical:
    tools: [Bash, Read, Write]
    side_effects:
      reads: ["tmp/cycle-*.json", "git HEAD", "ssh git@github.com"]
      writes:
        - "tmp/cycle-*.json (매 사이클 갱신)"
        - "git commits (사이클당 1개, conventional message)"
      deletes: []
---

# autonomous-cycle

단일 벤더 CLI에 얽매이지 않고 rallish 낭에서 실행되는 밤새 돌려도 안전한 자율 리팩터 루프.
멀티 에이전트 핑퐁 지원: rallish가 3사이클 마다 어댑터를 교체한다.

## 동반 파일
- 상태 스키마: `tmp/cycle-<id>.json`
- 브로커 이벤트: `rallish cycle watch --cycle-id <id>`
- 로그 스트림: `rallish daemon` 로그 (SSE via `cycle watch`)

## 사이클 워크플로우

```
┌─ 사이클 시작 ──────────────────────────────────────┐
│  1. tmp/cycle-<id>.json 읽기 (재개점)              │
│  2. Preflight 게이트 (브랜치, 클린, 목표, SSH)     │
│  3. Audit 게이트 (make check-all)                  │
│  4. Local 게이트 (--local-gate, 설정 시)           │
│  5. Philosophy 게이트 (ROP / SSOT / SRP 스위프)    │
│  6. Polish 게이트 (테스트, 린트, no-raw-ansi)      │
│  7. Commit 게이트 (conventional message, amend 금지)│
│  8. tmp/cycle-<id>.json 업데이트                   │
│  9. 3사이클 마다 에이전트 리셋                     │
└────────────────────────────────────────────────────┘
       ↓ rate-limit / token exhaust
   handoff 노트 작성 → graceful exit
```

## 트리거 흐름

### A — 원샷 시작 (권장)
```
사용자: "autonomous cycle" / "자율 사이클 시작"
에이전트:
  1. rallish daemon 실행 확인:  rallish doctor
  2. 원샷 시작 (이벤트를 실시간 스트리밍하며 블록):
       rallish cycle start --goal "feat: refactor adapter package" --agents claude,kimi --local-gate "make check-all"
     - 사이클 생성, 오케스트레이션 시작, SSE 감시를 한 번에 처리.
     - Ctrl+C로 detach 가능; 백그라운드에서 계속 실행.
  3. 나중에 감시 재개:  rallish cycle watch --cycle-id <id>
  4. 상태 확인:          rallish cycle status --cycle-id <id>
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

## 장기 실행 모드 (4–5시간)

야간 또는 장기 자율 실행 시:

1. **`--max-cycles`를 8–10으로 설정** (한 세션의 안전한 기본값).
2. **`--agents claude,kimi`로 멀티 에이전트 핑퐁 사용**.
3. **`--local-gate "<command>"`로 내장 Go 게이트 외 프로젝트별 검증 명령을 추가**.
4. **터미널 멀티플렉서** (`tmux`, `screen`)에서 시작해 SSH 연결 끊김에도 데몬이 살아남도록 한다.
5. **로그를 파일로 리다이렉트**:
   ```bash
   rallish daemon > tmp/autonomous-$(date +%Y%m%d-%H%M).log 2>&1 &
   ```
6. **우아한 성능 저하**: 게이트 실패 시 사이클이 중단되고 `halt_reason`을 기록하지만, 데몬은 다른 요청을 계속 처리한다.

## 20분 감시 라운드

장기 실행 시 20분마다 인간 체크인을 수행한다:

```bash
# 빠른 건강 확인
rallish cycle status --cycle-id <id> | jq '.completed_cycles, .halted, .last_failed_gate'

# 최신 이벤트 tail
rallish cycle watch --cycle-id <id> --since 20m
```

확인할 사항:
- `completed_cycles`가 꾸준히 증가하는가
- `last_failed_gate`가 비어 있는가 (게이트 실패 없음)
- `violations_found`가 늘지 않는가
- `halted`가 false인가

이 중 하나라도 이상하면 `rallish cycle halt`로 중단하고 상태 파일을 검토한 후 재개한다.

## 가드레일 보완 워크플로우

사용자가 "가드레일 보완"이나 "guardrail hardening"을 요청할 때:

```
┌─ 가드레일 보완 ────────────────────────────────────┐
│  1. /self-audit  → 현재 위반 목록화                │
│  2. 위반 수정    → 코드 또는 설정 변경              │
│  3. /polish      → 게이트 로컬 재실행              │
│  4. /ralphi      → 토큰 예산 + 철학 스위프        │
│  5. Commit       → 보완 라운드당 1커밋            │
│  6. Verify       → make check-all green           │
└────────────────────────────────────────────────────┘
```

rallish 용어로:
- **Self-audit** → `AuditGate` (make check-all) + `violations_found` 수동 검토
- **Fix** → `violations_found: []`를 반환하는 어댑터 턴
- **Polish** → `PolishGate` (테스트, 린트, no-raw-ansi)
- **Ralphi** → `PhilosophyGate` (ROP, SSOT, SRP, 버전 하드코딩 스위프)
- **Commit** → `CommitGate` (conventional message, amend 금지)

## 멀티 에이전트 오케스트레이션

`cycle new`에 `--agents claude,kimi`를 전달하면 브로커가 3사이클 마다 어댑터를 교체:

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
- ❌ 명시적 오버라이드 없이 한 밤에 MAX_CYCLES > 10 (검토 부담 증가).
- ❌ 처리량을 위해 Philosophy 게이트 스킵 (안전 장치 무력화).
- ❌ 사이클 간 `sleep` < 30초 (rate limit 위험).
- ❌ 4–5시간 실행 시 20분 감시 라운드 생략 (위반 누적 미발견).
- ❌ SSE 이벤트의 `last_failed_gate` 무시 (조기 경고 신호 놓침).
