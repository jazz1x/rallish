# 변경 이력

이 프로젝트의 모든 주요 변경 사항이 이 파일에 기록됩니다.

형식은 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)를 기반으로 하며,
이 프로젝트는 [Semantic Versioning](https://semver.org/spec/v2.0.0.html)을 준수합니다.

## [미발표]

### 추가됨

- **`internal/ui` — CLI 출력 SSOT.** 컬러 토큰
  (info / success / warn / error / dim / prompt / accent / heading),
  PyClack 글리프 (`◇ ✓ ■ ⚠ ◆ └ • → │`), 프롬프트 / confirm / 숫자
  선택 헬퍼, 그리고 컬럼 정렬 Table 렌더러를 추가했습니다. `NO_COLOR`,
  `TERM=dumb` 또는 stdout 이 TTY 가 아닌 경우 자동으로 컬러를
  비활성화합니다 (회귀 테스트 포함).
- **`rallish config` 명령 그룹.** `list` (모든 키의 값 + source 테이블),
  `get <key>`, `set <key> <value>` (`wait_mode` / `telemetry` /
  `coding_cli` 열거형 검증), `path`, `edit` (`~/.rallish/config.yaml`
  기본값 시드 후 `$EDITOR` 실행). 스키마는 `internal/config` 에 있습니다.
- **간결한 부트스트랩 위저드.** `rallish bootstrap` 이 4단계 1화면
  인터랙티브 플로우 (skill install → config 위저드 → 요약 → daemon
  체크) 로 재작성되었습니다. 자주 묻는 3가지 설정 (기본 preset,
  coding-CLI 벤더, wait mode) 만 묻습니다. `--yes` 는 CI 용,
  `--skip-skill` / `--skip-config` 은 개별 단계 건너뛰기.
- **`rallish add` 인터랙티브 picker.** 인자 없는 `rallish add` 는 npx
  스타일 type → name → scope 위저드를 실행합니다.
- **그룹화된 루트 help.** `rallish --help` 가 Setup / Rally / Manage
  / System 헤더로 명령을 분류합니다; 인자 없는 `rallish` 는 4줄 힌트
  배너를 출력합니다. cobra `--version` 추가.
- **`rallish doctor` 뷰.** 구조화된 `doctor.Inspect()` API 가 typed
  `Check` 레코드를 반환하며 CLI 는 글리프로 렌더링합니다.
- **`scripts/check-no-raw-ANSI.sh` 가드레일** + lefthook 훅 —
  `internal/ui` 외부에 `\x1b[` 이스케이프가 들어가면 커밋을 실패시킵니다.

## [0.2.1] - 2026-05-18

### 변경됨

- **스킬 이름 변경: `rallish-operator` → `rallish`.** 스킬 번들의
  식별자, 설치 디렉토리, 프론트매터 `name:` 필드가 `rallish-operator`에서
  간결한 `rallish`로 통합되었습니다 — 프로젝트의 벤더 중립적 정체성이
  확립된 이후 `-operator` 접미사가 더 이상 의미를 더하지 않았습니다.
  `~/.claude/skills/rallish-operator/`에 기존 설치가 있는 경우 자동
  마이그레이션되지 않습니다; 업그레이드 후 `rm -rf
  ~/.claude/skills/rallish-operator/ && rallish bootstrap` 을 실행하세요.
  트리거 문구(`랠리보낼 준비해`, `let's serve`, …)는 변경되지 않았습니다;
  프론트매터 `name:` 또는 트리거 문자열로 스킬을 해석하는 에이전트는
  재부트스트랩 후 즉시 계속 작동합니다. `go:embed` 경로,
  `defaultSkillTarget()`, 리포 루트의 `skills/rallish` 심볼릭 링크가
  모두 정렬되었습니다.

## [0.2.0] - 2026-05-18

### 추가됨

- **랠리 자동 루프** — 스킬이 각 쪽에서 한 번의 설정 트리거 후 양쪽
  랠리를 자율적으로 구동합니다. 서버 쪽은 `rally new --first server`로
  바톤을 미리 지정합니다 (SSE phantom-join 불필요). 리시버 쪽은 첫
  `rally status` 폴링에서 바톤을 수신합니다. 기본 `WAIT_MODE=yield`:
  에이전트가 `rally done` 후 사용자에게 제어권을 반환하고, 다음 사용자
  메시지에서 status를 확인해 자기 차례면 계속 진행합니다. 옵트인
  `WAIT_MODE=block`은 `rally join --once --timeout <dur>`로 사용 가능
  (양쪽이 모두 준비된 세션용). 패턴별 종료 신호로 루프가 자동 종료됩니다.
- **크로스 벤더 호환성 검증**: rallish-operator 스킬이 브랜드 그룹 경로
  `~/.claude/skills/`를 통해 Claude Code, Kimi, Codex, Cursor 등 스킬 인식
  CLI에서 자동 발견됩니다. 벤더별 설정 불필요. 라이브 검증: Claude Code와
  Kimi 간 discuss 패턴 랠리가 4턴 만에 상호 `[agree]`에 도달. 스킬 본문과
  핸드북에 크로스 벤더 callout 추가.
- **외부 리포 사용**: rallish 스킬, 데몬, 바이너리 모두 전역 위치
  (`~/.claude/skills/`, `~/.rallish/`, `/usr/local/bin`)에 있습니다. 최초
  설치 후 소스 트리 의존성이 없습니다. 새 핸드북 섹션
  [어디서든 rallish 사용](docs/handbook.md#using-rallish-from-any-project)과
  README callout으로 프로젝트 독립 워크플로우를 문서화. `rallish squash`의
  `--repo` 플래그는 세션 메타데이터 전용으로, 스킬이나 데몬 위치와 무관.

### 변경됨

- **단일 인스턴스 데몬 보호**: `rallish daemon`이 이미 `~/.rallish/rallish.sock`
  에 바인딩된 인스턴스가 있을 때 시작을 거부하고 다음 메시지 출력 후 비정상
  종료합니다:
  `rallish daemon already running at <path> — not starting a second instance`
  기존에는 두 번째 호출이 라이브 데몬의 소켓 파일을 조용히 unlink하여 첫
  번째 데몬을 고아로 만들었습니다. 복구: `kill -TERM $(pgrep -f "rallish
  daemon")` 후 재기동.

- 자동 루프를 가능하게 하는 새 CLI 옵션 두 가지:
  - `rally new --first <name>` — 세션 생성 시 바톤을 미리 지정;
    SSE phantom-join 트릭 불필요.
  - `rally join --once [--timeout <dur>]` — 첫 번째 바톤 이벤트 후
    깔끔하게 종료, 타임아웃 시 exit code 2. 플래그 없을 때 기본 동작
    (무한 블로킹, 다중 이벤트 수신) 유지.
  하위 호환 유지: 기존 세션 및 CLI 호출 변경 없음.

## [0.1.2] - 2026-05-17

### 추가됨

- **랠리 패턴** — 랠리 프리미티브 위에 레이어된 세 가지 동작 패턴:
  **cycle** (플래너 ↔ 익스큐터, `[plan]`/`[result]`/`[review]` 노트 컨벤션),
  **discuss** (피어 ↔ 피어, 상호 `[agree]`로 수렴하는 설계 토론), **help**
  (오너 ↔ 헬퍼, `[stuck]`/`[hint]`/`[try]`/`[resolved]` 짧은 비대칭 교환).
  패턴은 서버 준비 시 자연어 큐로 선택 (`"사이클로 가자"`, `"논의 랠리"`,
  `"막혔어 도와줘"`). 브로커 / CLI / 컨트랙트 변경 없음; rallish-operator
  스킬 본문에 컨벤션으로 인코딩 (v0.1.0 → v0.2.0). 참고:
  [docs/prd-rally-patterns.md](docs/prd-rally-patterns.md) 및
  [docs/runbook-rally-mode.md#rally-patterns](docs/runbook-rally-mode.md#rally-patterns).

## [0.1.1] - 2026-05-17

### 변경됨

- `.goreleaser.yaml`의 `brews:` 블록을 임시 비활성. Homebrew tap 리포
  (`jazz1x/homebrew-rallish`)와 `TAP_GITHUB_TOKEN` 시크릿이 아직 미설정
  상태로, v0.1.0 릴리즈 파이프라인이 brew publish 단계에서 실패함. tap
  셋업 전까지는 curl 원라이너, `npx skills add`, 또는 소스 빌드로 설치.
  Homebrew 는 후속 릴리즈에서 복구.

## [0.1.0] - 2026-05-17

두 라이브 코딩 CLI 세션 사이의 라이브 바톤 전달(랠리 모드) 추가, 운용
플레이북의 벤더 중립 스킬 번들 패키지화, IPC + 태그 파이프라인 강화.
태그 발급 시 v0.1.0 으로 릴리즈 예정.

### 추가됨

- **랠리 모드** — 라이브 바톤 전달 프리미티브 (`rally new/join/done/status`).
  - 에이전트 주도 UX: 자연어 트리거 세 개(`랠리보낼 준비해` /
    `랠리받을 준비해 <SID>` / `시작` / `내 차례` / `끝`)로 전체 세션 구동;
    에이전트가 모든 rallish CLI 호출 대신 실행.
  - 테니스 테마(🎾): `server` / `returner` 역할, 동시 단일 바톤.
  - 세션 ID 포맷 `rly_<unixmillis>_<rand4hex>`; SSE 하트비트(15초);
    비활성 참가자 감지; 독점 홀더 강제(409).
  - `broker.CloseAllRallies()` — SIGTERM 시 `{"closed":true}` 브로드캐스트로
    SSE 클라이언트가 5초 셧다운 데드라인 내에 정상 종료.
- **`rallish-operator` 스킬 번들** — `skills/rallish-operator/` 벤더 중립
  스킬 (캐노니컬은 `internal/skills/rallish-operator/`, 심볼릭 링크).
  한 줄 설치: `npx skills add jazz1x/rallish`.
  - `scripts/install-binary.sh` 번들 — 첫 트리거 때 `rallish` PATH 미존재
    감지 시 에이전트가 번들 인스톨러 실행(uname → GitHub Release tarball →
    `/usr/local/bin` 또는 `~/.local/bin`).
  - `//go:embed all:rallish-operator`로 바이너리에 임베드;
    `rallish skill install` / `rallish bootstrap` 으로 풀어냄.
- **Squash 우산** — `rallish squash`가 `rallish start`를 대체, 헤드리스
  프리셋 오케스트레이터(`solo-ralph`, `pair-review`) 커버. 하위 호환
  별칭 없음(AGENTS.md 컨벤션).
- **Unix 도메인 소켓 IPC** — `~/.rallish/rallish.sock` (mode `0600`)을
  주 CLI↔Daemon 전송으로; A2A 클라이언트와 Windows 폴백(빌드 태그 스텁,
  `ErrUnsupported` 반환)용 TCP 루프백 유지.
- **`rallish doctor`** — 소켓 경유 데몬 도달성 보고, 소켓 권한 점검
  (0600 보다 헐겁다면 경고).
- **A2A 프로토콜 레이어** — `GET /.well-known/agent.json`, `POST /a2a`
  (JSON-RPC 2.0: `tasks/send`, `tasks/get`, `tasks/cancel`,
  `tasks/sendSubscribe` SSE), `pkg/contract/a2a.go`.
- **토큰 예산 강제 적용** — 브로커 (`handleNextTurn`).
- **`internal/scratch/scratch.go`** — `max_kb` 초과 시 자동 압축;
  어댑터 프롬프트에 모델 힌트 주입.
- **`internal/safepath/`** — 사용자 입력 경로 traversal 가드; 랠리
  `--repo` 플래그에서 사용.
- **릴리즈 헬퍼** — `make release-{patch,minor,major,dry-run}`. 
  `scripts/release.sh`가 VERSION 범프, README 배지 동기, 커밋, 태그, 푸시;
  더티 트리 / 비-main 브랜치 / 미푸시 커밋 / 비-monotonic 버전 / 기존 태그
  (로컬+원격) 거부.
- **Lefthook 훅** — `commit-msg`(conventional prefix 강제), `pre-commit`
  (fmt/vet/test/lint), `pre-push`(build/vet/test).
- **LICENSE** (MIT) — README 배지 + goreleaser 아카이브 `files:` 글롭 백킹.
- **PRD + 런북** — `docs/prd-rally-mode.md`, `docs/runbook-rally-mode.md`.

### 변경됨

- `rallish start` 제거; 기존 스크립트는 `rallish squash`로 마이그레이션 필수.
- Runner HTTP 클라이언트가 소켓 인식 transport 사용. 이전엔 모든
  `next`/`turn` 요청이 일반 `http.Client`로 가서 `http://rallish.local`
  DNS lookup이 silent 실패.
- 데몬 정리 강화: TCP serve 오류 시에도 Unix listener 닫고 socket-pointer +
  port 파일 제거.
- AGENTS.md: conventional commits가 `commit-msg` 훅으로 머신 강제. 허용
  prefix: `feat fix refactor docs test chore sec ci build perf style`.
  Feature Documentation Workflow + 신규 패키지 layout 행 추가.
- README / CHANGELOG / DESIGN.md를 EN / KO / JP 3개국어로 lockstep 동기.
- 3개국어 README 재정렬: `npx skills add jazz1x/rallish` 단일 헤드라인;
  파워 유저용 설치 경로는 `<details>` 블록으로 강등.

### 수정됨

- Runner HTTP 클라이언트가 소켓 인식 아님 — 세션 생성 후 모든 polling 실패
  (브로커 랠리 `data:` 줄 완료가 도달 못함). `runner.NewLoopWithClient` 로 픽스.
- `handleRallyBaton` 후기-join 분기가 history에서 직전 note를 읽지 않아,
  핸드오프 후 join한 참가자가 `note=""` 보던 결함 수정.
- 데몬 SIGTERM 시 graceful shutdown 전에 TCP serve 오류 발생하면
  `rallish.sock`, `socket`, `port` 가 디스크에 남던 leak 수정.
- Unix 도메인 소켓이 기본 `0755` 권한으로 생성됨; 이제 `Listen` 후
  명시적 `chmod 0600`.
- `daemon`과 `doctor` cobra 명령의 `Short` 설명이 `--help`에서 비어있던
  결함 수정.
- Cobra 오류가 `SilenceErrors: true` 로 출력 안 됨 — 잘못된 참가자 이름 등
  검증 실패가 stderr 없이 exit 1. 이제 `Error: ...` 를 stderr 로 출력 후 종료.
- `make check`가 `.toolchain/bin/` 핀된 `golangci-lint` 대신 `$PATH` 의 것을
  찾던 결함 수정. Makefile이 toolchain 바이너리 자동 발견 + Go 런타임
  prefix.

### 보안

- Unix 소켓 권한 `0600` 강화 (브로커 측).
- Socket-pointer 파일(`~/.rallish/socket`)을 `cli.RunStart`에서 사용 전
  rallish home 루트 안에 있는지 검증해 변조된 포인터를 통한 traversal 차단.
- `--repo` 경로가 `internal/safepath.Clean` 과 명시적 `os.Stat` 디렉터리
  검사를 거쳐 브로커에 전달.
- `forbidigo` lint 규칙으로 라이브러리 코드에서 `os.Environ()` 과
  `exec.Command("sh"…)` 금지 (DESIGN.md §14).
- `govulncheck`를 모든 push/PR 마다 실행 (`.github/workflows/ci.yml`).
- 모든 릴리즈 아티팩트에 `cosign` keyless 서명 + `syft` SBOM
  (`.goreleaser.yaml`).

### CI / 파이프라인

- `release.yml`이 push된 태그를 `^v[0-9]+\.[0-9]+\.[0-9]+$` 정규식 검증 후
  goreleaser 호출; 권한 스코프를 최소(`contents:write` + `id-token:write`)
  로 트림.
- `ci.yml` build job이 `CGO_ENABLED=0` (goreleaser 일치) 명시; build 후
  `dist/rallish version` 스모크 실행; 매트릭스는 macOS + Linux 커버.
- Dependabot이 `gomod` 와 `github-actions` 두 에코시스템 추적.

### 알려진 follow-up (다음 릴리즈로 연기)

- 서드파티 GitHub Actions를 SHA로 고정 (현재 mutable 태그).
- CodeQL 워크플로우 추가.
- CI 빌드 매트릭스에 Windows 추가.
- 70% 커버리지 floor를 CI 에 강제 (현재 AGENTS.md 문서뿐).
- `SECURITY.md` / `CODE_OF_CONDUCT.md`.
