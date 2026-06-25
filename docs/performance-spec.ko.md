# rallish — 성능 및 벤치마크 명세

> 브로커·게이트 파이프라인·레저·어댑터에 대한 성능 목표, 벤치마크 시나리오, 측정 방법론.
> **버전:** `VERSION`(0.3.0) 추종 · **최종 수정:** 2026-06-24 · [English](./performance-spec.md)

## 1. 목적과 범위

rallish는 **로컬 단일 호스트 브로커**이지 분산 서비스가 아니다. 성능 예산은 rallish가 **통제하지 못하는** 한 가지가 지배한다: 기저 에이전트 CLI 서브프로세스의 지연(한 번의 `claude`/`kimi` 턴은 수 초~수 분). 브로커 자신의 작업 — 턴 라우팅, 레저 한 줄 추가, 게이트 실행 — 은 그에 비해 **무시할 만큼** 작아야 한다.

따라서 성능 계약은 비대칭이다:

- **브로커 오버헤드**(어댑터 호출 사이에 rallish가 하는 모든 것)는 **서브밀리초~한 자릿수 밀리초** 범위를 유지하고 세션 길이에 따라 증가하지 않아야 한다.
- **어댑터 지연**은 측정·노출(`Usage.Ms`)되지만 rallish 통제 밖이며 rallish 자체 목표에서 **제외**한다.

이 문서는 (a) 컴포넌트별 성능 목표, (b) 그것을 증명하는 벤치마크 스위트, (c) 숫자가 재현 가능하도록 하는 방법론과 환경을 정의한다. 오늘은 **마이크로 벤치마크 5개**만 존재하며, 이 명세는 프로젝트가 키워가야 할 목표 스위트를 정의한다.

## 2. 성능 원칙

1. **O(history)가 아닌 O(window).** 모든 턴별 연산(라우팅, stuck 감지, 예산 점검)은 전체 세션/레저 길이가 아닌 고정 윈도우에 한정돼야 한다. stuck 감지기는 명시적으로 레저에 대한 O(window)이며, 이 성질을 보존하고 레저 크기를 키워가며 벤치마크해 증명해야 한다.
2. **다시 쓰지 말고 추가하라.** 레저는 append-only JSONL. 턴 N을 쓸 때 턴 1..N−1을 재직렬화하면 안 된다.
3. **핫패스 할당 규율.** 턴별 함수는 할당을 보고(`b.ReportAllocs()`); allocs/op 퇴행은 성능 퇴행으로 취급.
4. **브로커는 병목이 아니다.** 브로커 오버헤드가 한 턴의 측정 가능한 비율을 차지하면 그것은 버그다.

## 3. 성능 목표

목표는 참조 환경(§6)에서의 **자릿수 예산**이지 엄격한 SLA가 아니다. 퇴행이 명백히 드러나도록 존재한다. "턴당"은 어댑터 서브프로세스를 제외한, 한 ping-pong 스텝의 브로커측 작업을 뜻한다.

### 3.1 핫패스(턴당 브로커 작업)

| 연산 | 목표(p50) | 목표(p99) | 할당 목표 | 근거 |
|------|-----------|-----------|-----------|------|
| `Router.Next`(라우팅 결정) | < 5 µs | < 50 µs | 정상상태 0 alloc | 역할에 대한 순수 map/slice |
| `Budgeter.Remaining` | < 1 µs | < 10 µs | 0 alloc | 산술뿐 |
| `BuildPrompt` | < 100 µs | < 500 µs | JSON 마샬 + ≤3 alloc | 마샬 1회 + 문자열 빌드 |
| `ParseLastJSONBlock`(전형 CLI 출력) | < 500 µs | < 2 ms | 출력 크기에 한정 | regex + unmarshal 1회 |
| `Stuck` 감지기(레저 윈도우) | < 200 µs | < 1 ms | O(window) | 레저 크기에 따라 증가하면 **안 됨** |
| `LedgerFileSync.Append`(해시 포함) | < 2 ms | < 10 ms | 한정 | `lastHash` 읽기 1회 + SHA-256 + 추가 |
| `ChainHash`(단일 항목) | < 20 µs | < 100 µs | 1–2 alloc | canonical 마샬 1회 + SHA-256 |
| `VerifyChain`(항목당) | < 20 µs | < 100 µs | 분할상환 | 선형 순회 |

**집계 예산:** 턴당 총 브로커 오버헤드(라우팅 + 프롬프트 빌드 + 파싱 + 레저 추가 + 예산/stuck 점검)는 **p50 < 5 ms, p99 < 15 ms** — 즉 수 초짜리 어댑터 턴 옆에서 보이지 않을 정도.

### 3.2 스케일링(평탄 또는 선형 유지)

| 양 | 요구 |
|----|------|
| 세션 길이 대비 턴당 오버헤드 | **평탄** — 1000번째 턴 오버헤드 ≈ 1번째(O(window) 증명) |
| 레저 크기 대비 `Stuck` 비용 | 윈도우 내 **평탄**(한정 스캔), 전체 레저 O(n) 아님 |
| 레저 크기 대비 `VerifyChain` / `ReadAll` | 항목 수에 **선형**; 감사 시점(턴별 아님) 연산이므로 허용 |
| 레저 크기 대비 `LedgerFileSync.Append`의 `lastHash` 비용 | 현재 마지막 해시를 찾으려 읽음 — **O(1) 또는 O(tail)**이어야지 O(n) 아님. 전체 파일 재스캔이면 표시(§7 리스크) |
| 활성 세션당 메모리 | 한정; 스크래치패드는 프리셋 `max_kb`에 캡; 레저는 스트리밍, 통째로 보유 안 함 |

### 3.3 동시성 / 라이브니스

| 성질 | 목표 |
|------|------|
| 브로커 레이스 청결성 | CI에서 `go test ./... -race -count=1` 그린(일회성 감사 패스에서 브로커를 추가로 `-race -count=5` ×5 깨끗이 돌림) |
| 배타적 보유자 강제(rally) | 동시 `done`/`join` 하에서 두 참가자가 동시에 바톤 보유 안 함 |
| 소켓 다이얼 타임아웃(CLI→데몬 생존 점검) | 300 ms(이미 연결됨) |
| 데몬 기동~최초 accept | 참조 환경에서 < 1 s |
| SSE 전달 지연(바톤 넘김 → 열린 스트림 수신) | 로컬 < 50 ms |

### 3.4 기동 / 풋프린트

| 지표 | 목표 |
|------|------|
| `rallish version` / `doctor` 콜드 스타트 | < 50 ms(단일 정적 Go 바이너리) |
| 유휴 데몬 RSS | < 30 MB |
| 바이너리 크기 | 릴리스마다 추적; 하드캡 없음, 예기치 않은 증가 감시 |

## 4. 벤치마크 스위트

### 4.1 기존 벤치마크(베이스라인)

오늘 6개 마이크로 벤치마크 존재:

| 벤치마크 | 파일 | 커버 |
|----------|------|------|
| `BenchmarkBuildPrompt` | `internal/adapter/prompt_test.go` | 프롬프트 구성 |
| `BenchmarkManagerAppend` | `internal/scratch/scratch_test.go` | 스크래치패드 추가 + 압축 |
| `BenchmarkBudgeter_Remaining` | `internal/budget/budget_test.go` | 예산 산술 |
| `BenchmarkStoreAppend` | `internal/session/session_test.go` | 세션 레저 추가 |
| `BenchmarkTurnResponse_Compact` | `pkg/contract/types_test.go` | 턴 응답 직렬화 |
| `BenchmarkLedgerAppend/size=*` | `internal/cycle/ledger_test.go` | 레저 추가 + tail-hash 스케일링 |

이는 **바닥**이다. 직렬화·추가는 커버하나 브로커·게이트·라우터·stuck 감지기·엔드투엔드 턴 루프는 미측정.

### 4.2 추가할 목표 벤치마크

보호 대상별로 묶음. 각각 `b.ReportAllocs()`를 쓰고, 해당 시 `b.Run`으로 여러 입력 크기에서 서브벤치마크.

**핫패스(턴당):**
- `BenchmarkRouterNext` — 1/3/8역할 프리셋 라우팅 결정.
- `BenchmarkParseLastJSONBlock` — 전형 출력, 후행 노이즈 출력, 균형중괄호 폴백 경로.
- `BenchmarkChainHash` / `BenchmarkVerifyChain` — 단일 항목 및 항목당 분할상환.
- `BenchmarkStuck/ledger=10,100,1000,10000` — **핵심 스케일링 가드**: 레저가 커져도 비용이 평탄 유지(윈도우 한정 증명).

**게이트 파이프라인:**
- `BenchmarkStandardPipeline_NoShell` — 셸 게이트를 스텁한 파이프라인 오버헤드(`go test`/`make check-all`에서 rallish 자체 비용 분리).
- `BenchmarkPhilosophyGate` — 대표 `git diff`에 대한 regex 스캔, 소/중/대 diff에서.

**감사 / Merkle(연결 시, F12):**
- `BenchmarkMerkleRoot/n=...`, `BenchmarkInclusionProof`, `BenchmarkVerifyConsistency` — O(n) 빌드, O(log n) 증명.

**액션게이트(G6):**
- `BenchmarkDecideToolUse` — 짧은/긴 명령 분류기(동기 PreToolUse 훅에 충분히 싸야 함, O(len)).

**엔드투엔드(`fake` 어댑터):**
- `BenchmarkSquashLoop_Fake` — `fake`(서브프로세스 0)로 전체 N턴 ping-pong, 턴당 브로커 오버헤드 보고. 가장 유용한 단일 집계 숫자: rallish 자체 턴당 비용 분리.

### 4.3 매크로 / 시나리오 벤치마크(측정·보고, 게이트 안 함)

수동 또는 야간 잡으로 실행; 어댑터 서브프로세스를 포함하므로 목표로 강제하지 않고 **관측성 위해 보고**(어댑터 지연이 지배하고 범위 밖):

| 시나리오 | 측정 대상 |
|----------|-----------|
| `solo-ralph`, 10턴, 실제 `claude` | 턴당 벽시계, `Usage.Ms` 분포, 턴당 토큰 |
| `pair-review`, 20턴, 실제 `claude`+`kimi` | 핸드오프 오버헤드, 런타임 교차 턴 비용 |
| `cycle run --once` 콜드 | 에이전트 턴 제외 게이트 파이프라인 벽시계(audit/polish는 `go test`/`make`가 지배) |

브로커 귀속 오버헤드를 어댑터 + 셸게이트 시간과 분리 보고해 비대칭을 가시화.

## 5. 지표와 계측

- **턴당 사용량**은 이미 계약에 있음: `TurnResponse`의 `Usage{tokens_in, tokens_out, ms}`. 세션별로 집계해 턴당 토큰·턴 지연 보고.
- **프로파일링 소스로서의 레저.** `agent_turn`/`gate_passed`/`gate_failed`의 이벤트 타임스탬프(`at`, Unix ms)로 추가 계측 없이 감사 추적에서 턴·게이트 소요를 재구성.
- **권장 추가**(미존재): 게이트 파이프라인의 `pprof` CPU/힙 프로파일을 내는 `cycle run --once`의 `--profile` 플래그; 노이즈 방지 위해 config로 게이트되는 구조화 턴별 타이밍 로그.

## 6. 참조 환경 및 방법론

숫자가 실행·기여자 간 비교 가능하려면 벤치마크 보고마다 환경을 기록하라.

**방법론.**
- Go 테스팅 벤치마크 사용: 패키지별 `go test -bench=. -benchmem -benchtime=...`; 비교 시 `-cpu` 고정.
- 조용한 머신에서 실행; 가능하면 터보/스로틀 변동 비활성화; ≥5회 실행의 중앙값 보고.
- 스케일링 벤치마크는 입력 크기 대비 ops를 그려 기대 형태(평탄/선형/로그) 검증.
- `benchstat`으로 전후 비교, 단일 실행 노이즈가 아닌 통계적으로 유의한 퇴행에 게이트.
- `fake` 어댑터로 벤치마크해 브로커 목표에서 어댑터 서브프로세스 시간 제외.

**보고마다 기록:** Go 버전(현재 `go 1.25.0`), OS/arch, CPU 모델, 커밋 SHA, `rallish version`, 정확한 `go test -bench` 호출.

**권장 퇴행 게이트(CI, 처음엔 권고):** `benchstat`이 핫패스 벤치마크에서 > 20% 퇴행(시간 또는 할당)을 보이거나, `BenchmarkStuck/*`·`BenchmarkLedgerAppend/*` 스케일링 벤치마크에서 비평탄 결과를 보이면 PR 실패.

## 7. 알려진 성능 리스크(코드 리뷰에서)

- ✅ **`LedgerFileSync.lastHash` 재독이 수정됨.** Append가 이전에는 꼬리 해시를 찾기 위해 전체 파일을 순회해 쓰기 비용이 턴당 O(n), 세션당 O(n²)였다. 이제 절대 경로별 프로세스 내 `ledgerLock`(`internal/cycle/ledger.go`)에 tail 해시를 캐시하며, `BenchmarkLedgerAppend/size=*`는 레저 크기에 따른 평탄 비용을 보여준다.
- **`forEachLedgerLine`이 무경계 `bufio.Reader` 사용.** 올바름(대형 게이트 리포트에 64 KiB `Scanner` 벽돌화 회피)이나 단일 병적 항목이 메모리를 튀게 할 수 있음. 항목 크기 한정 또는 가정 문서화.
- **`git diff`에 대한 Philosophy 게이트 regex.** 비용이 diff 크기에 비례; 단일 사이클의 대형 diff가 느릴 수 있음. 대형 diff에서 벤치마크; diff 크기 캡 검토.
- **`ParseLastJSONBlock` 폴백**이 출력 전체에서 균형 객체를 스캔. 병적 어댑터 출력(거대 비-JSON 텍스트)은 이를 O(output)으로 만듦. 스캔 윈도우 한정.
- **프로세스 간 레저 경합.** 인프로세스 뮤텍스만 추가를 보호; 한 사이클 파일에 두 드라이버는 OS 수준에서 공정성 보장 없이 경합. 단일 드라이버 사용엔 범위 밖이나 명기.

## 8. "성능이 명세됨"의 수용 기준

- [ ] §4.2의 핫패스 벤치마크가 존재하고 CI에서 그린.
- [ ] `BenchmarkStuck/*`·`BenchmarkLedgerAppend/*`이 레저가 커져도 **평탄**한 턴당 비용을 입증(또는 퇴행 수정).
- [ ] `BenchmarkSquashLoop_Fake`이 §3.1 집계 예산 내 턴당 브로커 오버헤드 보고.
- [ ] 참조 벤치마크 보고(§6 필드 기입)가 `docs/reports/` 아래 커밋되고 릴리스마다 갱신.
- [x] `lastHash` O(n) 리스크(§7)가 해결 확인 또는 허용 가능으로 문서화.
