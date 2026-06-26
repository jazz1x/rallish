# 런북: G6 액션 게이트 — 파괴적 명령을 실제로 차단하기

> **선행조건:** `PATH`에 `rallish`, `jq` 설치, PreToolUse 훅을 지원하는 코딩 CLI
> (이 런북은 **Claude Code** 기준). [English](./runbook-action-gate.md)

---

## 1. 무엇을 제공하나

rallish는 **실행 전 정책**(G6)을 제공한다: 파괴적 명령 거부 목록 + 시크릿 격리
(`pkg/contract` → `DecideToolUse`). rallish 자체는 판결을 **선언·기록**만 하고
명령을 실행하거나 차단하지 않는다. 강제 경계는 런타임에 있다: **PreToolUse 훅**이
게이트를 호출하고 판결을 따른다.

이 런북은 번들된 훅을 연결해, 예컨대 `rm -rf /`가 실행 전에 **거부**되고
`git reset --hard origin/main`은 **먼저 확인**을 받게 만든다.

| rallish 판결 | 게이트 종료 코드 | 훅 → Claude Code 결정 |
|---|---|---|
| `allow` | 0 | 진행(프롬프트 없음) |
| `needs-hitl` | 14 | `ask` — Claude Code가 확인을 요청 |
| `deny` | 13 | `deny` — 도구 호출 거부, 사유를 모델에 표시 |

래퍼는 **안전 우선**으로 동작한다: `jq`나 `rallish`가 없거나 게이트가 에러를 내면,
조용히 허용하지 않고 `ask`(사람 확인)로 에스컬레이션한다.

## 2. 번들된 훅

`rallish bootstrap`(또는 `rallish skill install`)이 래퍼를 다음 경로에 설치한다:

```
~/.claude/skills/rallish/scripts/gate-pretooluse.sh
```

훅은 stdin의 PreToolUse JSON을 읽어 Bash 명령을 추출하고,
`rallish gate tooluse --command "<cmd>"`를 실행해 종료 코드를 Claude Code의
`permissionDecision` 계약으로 매핑한다. Bash가 아닌 도구는 그대로 통과시킨다.

## 3. 연결(한 번만)

설정에 PreToolUse 훅을 추가한다. **모든** 프로젝트를 보호하려면
`~/.claude/settings.json`을, 한 저장소로 한정하려면 프로젝트 `.claude/settings.json`을 쓴다:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.claude/skills/rallish/scripts/gate-pretooluse.sh"
          }
        ]
      }
    ]
  }
}
```

`"matcher": "Bash"`는 훅을 셸 명령으로 한정한다 — G6 거부 목록이 다루는 유일한 대상.
설정을 다시 읽도록 Claude Code를 재시작(또는 새 세션 시작)한다.

## 4. 동작 확인

셸에서 샘플 PreToolUse 페이로드를 훅에 넣어 결정을 확인한다:

```bash
HOOK="$HOME/.claude/skills/rallish/scripts/gate-pretooluse.sh"

# deny — 치명적
printf '{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}' | "$HOOK"
# → permissionDecision: "deny"   (사유: rm-rf-root)

# ask — 위험하나 정당할 수 있음
printf '{"tool_name":"Bash","tool_input":{"command":"git reset --hard origin/main"}}' | "$HOOK"
# → permissionDecision: "ask"    (사유: git-hard-reset-remote)

# allow — 평범한 명령은 아무것도 출력하지 않고 진행
printf '{"tool_name":"Bash","tool_input":{"command":"ls -la"}}' | "$HOOK"
```

Claude Code 안에서 임시 디렉터리에 `rm -rf /`를 실행하라고 요청하면, 그 호출은
게이트 사유와 함께 거부되어야 하고 절대 실행되지 않아야 한다.

## 5. 사이클 원장에 기록(선택)

자율 사이클 중에는 `--cycle-id`를 넘겨 모든 **차단** 결정이 해당 사이클의 추가-전용
원장에 `tooluse_decision` 감사 레코드로 기록되게 한다(안전한 `allow` 결정은 기록되지
않음 — 오탐 방지). 훅에서 하려면 세션에 `RALLISH_CYCLE_ID`를 설정하고 래퍼의
`gate tooluse` 호출에 `--cycle-id "$RALLISH_CYCLE_ID" --state-dir ~/.rallish/cycles`를 더한다.

## 6. 경계와 한계

- **rallish는 결정 계층이지 실행자가 아니다.** 이 훅(또는 런타임의 동등한 훅)이 없으면
  정책은 비활성 — 아무것도 차단하지 못한다.
- 거부 목록은 오탐을 피하려 의도적으로 좁고 고정밀(치명적 패턴만)이다; 샌드박스가
  아니다. 신뢰할 수 없는 입력에는 OS 수준 통제와 함께 쓰라.
- 훅은 **Bash** 도구를 다룬다. 파일 편집 도구는 설계상 범위 밖(Claude Code 자체 권한
  모델이 담당).
