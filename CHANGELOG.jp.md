# 変更履歴

このプロジェクトのすべての主要な変更はこのファイルに記録されます。

形式は [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) に基づいており、
このプロジェクトは [Semantic Versioning](https://semver.org/spec/v2.0.0.html) に準拠しています。

## [未発表]

### 追加

- **`rallish cycle` — 自律サイクルサブシステム。** ワンショット `cycle start`
  (作成 → オーケストレーション → 監視) with `--max-cycles`, `--max-duration`,
  `--auto-goal`, `--log-file`. `cycle status` は3サイクルごとにエージェント
  リセットヒントを表示. `cycle halt` / `cycle next` / `cycle watch` で
  インタラクティブ制御.
- **`rallish trigger` — 自然言語スキル呼び出し.** `rallish trigger "자율 사이클"`
  は埋め込みスキルトリガーをマッチングし、SKILL.md のデフォルト値で
  `cycle start` を自動実行. `--dry-run` で実行せずにコマンドをプレビュー.
- **autonomous-cycle コンパニオンファイル.** `skill install --name autonomous-cycle`
  でスキル + ドライバースクリプト (`~/.claude/scripts/autonomous-cycle.sh`) +
  ハンドオフランブック (`~/.claude/runbooks/cycle-handoff.md`) をインストール.
  Bootstrap ステップ1で自動インストール.
- **AutoGoal + 時間ベース終了.** `go vet`, `golangci-lint --fast-only`,
  TODO/FIXME スキャンで次の目標を自動発見. `MaxDurationMinutes` で実行時間を
  制限; コードベースがクリーンなら `HaltSuccess` で graceful 終了.
- **プロジェクト固有のサイクルゲート.** `cycle new` / `cycle start` は
  繰り返し指定できる `--local-gate "<command>"` チェックを受け取り、内蔵
  audit ゲート後に実行し、`CycleState.local_gates` に保存して `cycle status`
  とマルチエージェントのサイクル要約に含めます.
- **自律作業ハーネス契約.** `pkg/contract.WorkContract` が、特定の
  エージェントランタイムに依存しない作業目標、スコープ、ゲート、予算、
  オーケストレーション形状を定義し、アダプターが共通利用できるようにしました.
- **ハーネス台帳イベント契約.** `pkg/contract.HarnessLedgerEntry` が、将来の
  サイクル台帳向けの append-only 監査イベント形状を定義しました.
- **サイクル台帳ファイル同期.** `internal/cycle.LedgerFileSync` がハーネス
  台帳イベントを JSONL として append/read し、監査ログの保存点を確立しました.
- **ゲートから台帳への projection.** `contract.NewGateLedgerEntry` がゲート
  レポートを append-only の成功/失敗監査イベントへ変換します.
- **ハンドオフから台帳への projection.** `contract.NewHandoffLedgerEntry` が
  エージェントのハンドオフ応答を `handoff_to` を保持した append-only 監査
  イベントへ変換します.
- **オーケストレーターのハンドオフ台帳連携.** マルチエージェントサイクルは
  アダプターが `handoff_to` を返したとき `handoff_created` イベントを append
  します.
- **エージェントターン台帳イベント.** マルチエージェントサイクルは完了した
  アダプターターンを、任意のハンドオフイベントとは別の `agent_turn` イベント
  として append します.
- **台帳失敗ポリシー.** ハーネス PRD とランブックに、broker の台帳 append は
  best-effort、orchestrator turn append は fail-fast であることを文書化しました.
- **サイクル台帳の読み出し.** `GET /cycles/{id}/ledger` が append-only の
  ハーネス台帳イベントを返し、mutable state が削除済みの halt サイクルも
  参照できます.
- **サイクル台帳 CLI.** `rallish cycle ledger --cycle-id <id>` がハーネス
  台帳を、人と後続エージェントの両方が読みやすい pretty JSON で出力します.
- **サイクル状態の台帳サマリー.** `rallish cycle status` が台帳を読める場合に
  entry 数と最後のイベントを表示します.

- **アンチスピン活性性 (G5).** 無進捗/スピンする実行はトークンを浪費する前に自ら
  halt: `cycle.Stuck` が反復ターン・反復ゲート失敗・ピンポン・停滞した frontier を
  検出し、sticky-halt reviver ガードが halt 済みサイクルの再開を拒否、ハードな累積
  コスト上限が revival をまたぐ productive runaway を捕捉.
- **実行前 action-gate (G6).** `contract.DecideToolUse`(破壊的 deny-list +
  シークレット封じ込め)が保留中のコマンドを分類し、`rallish gate tooluse` が意味の
  ある exit code で verdict を返す — rallish はポリシーを宣言・記録し、ランタイム
  フックが強制.
- **改ざん検知・再現可能な監査 (G4).** 台帳エントリに `schema_version` + ハッシュ
  チェーン (`prev_hash`/`hash`); `contract.VerifyChain` が内容改ざんを検出、
  `contract.Replay` が制御グラフを再構築 (verify-before-reconstruct)、CT スタイルの
  Merkle 層 (RFC 9162) が inclusion + consistency 証明を追加.
- **ゲーム不可能な検証ゲート (G2).** エージェントのハンドシェイクを厳格にパース
  (パース不能なターンは halt、暗黙の prose フォールバックなし); gate self-eval が
  philosophy スキャナがシードされた違反を捕まえることを証明; ゲート定義をハッシュ
  ピン留めして in-cycle 編集を検出.
- **A2A v1.0 準拠 (G3).** ブローカーが `/.well-known/agent-card.json` に実際の
  `protocolVersion` を持つ署名付き Agent Card を提供、PascalCase RPC
  (`SendMessage`/`GetTask`/`CancelTask`/`SubscribeToTask`)、未知フィールドを拒否
  する厳格な型付き intake; レガシーパスは後方互換のため維持.
- **バウンド付き resume リファレンスドライバー (G1).** `rallish cycle run --once`
  が単一の非監視パスを実行し halt 理由から導出したコードで終了 — cron/スケジューラ
  が駆動する安全な呼び出し点; resume は既存の atomic 状態ファイルで検証.
- **不変条件としてのガードレール.** CI import-guard テストがコアの
  loop/scheduler/graph-DB パッケージの import を禁止; `go mod verify` を push
  ゲートに連結; AGENTS.md/CLAUDE.md を構造化された convention ソースとして取り込み.

## [0.3.0] - 2026-05-20

### 追加

- **`internal/ui` — CLI 出力の SSOT。** カラー トークン
  (info / success / warn / error / dim / prompt / accent / heading)、
  PyClack グリフ (`◇ ✓ ■ ⚠ ◆ └ • → │`)、プロンプト / confirm / 数値
  セレクト ヘルパー、整列テーブル レンダラー。`NO_COLOR` / `TERM=dumb`
  / stdout が TTY でない場合に自動でカラーを無効化します（回帰テストで
  ロック）。
- **`rallish config` コマンド グループ。** `list`（全キー値とソースの
  テーブル）、`get <key>`、`set <key> <value>`（`wait_mode` / `telemetry`
  / `coding_cli` の列挙型検証）、`path`、`edit`（`~/.rallish/config.yaml`
  にデフォルトを書いてから `$EDITOR` を起動）。スキーマは
  `internal/config` にあります。
- **コンパクトな bootstrap ウィザード。** `rallish bootstrap` を 4ステップ
  1画面のインタラクティブ フロー（skill install → config ウィザード →
  サマリー → daemon チェック）に書き換え、頻出 3 設定（default preset、
  coding-CLI ベンダー、wait mode）だけを尋ねます。`--yes` は CI 用、
  `--skip-skill` / `--skip-config` は個別ステップのスキップ。
- **`rallish add` インタラクティブ ピッカー。** 引数なしの `rallish add`
  は npx スタイル type → name → scope ウィザードを起動します。
- **グループ化された root help。** `rallish --help` がコマンドを
  Setup / Rally / Manage / System の見出しで整理します。引数なしの
  `rallish` は 4 行のヒント バナーを出力します。cobra `--version` を追加。
- **`rallish doctor` ビュー。** 構造化された `doctor.Inspect()` API が
  typed `Check` レコードを返し、CLI がグリフでレンダリングします。
- **`scripts/check-no-raw-ANSI.sh` ガードレール** + lefthook フック —
  `internal/ui` の外で `\x1b[` エスケープが見つかればコミットを失敗させます。

## [0.2.1] - 2026-05-18

### 変更

- **スキル名変更: `rallish-operator` → `rallish`.** スキルバンドルの
  識別子、インストールディレクトリ、フロントマター `name:` フィールドが
  `rallish-operator` からシンプルな `rallish` に統一されました —
  プロジェクトのベンダー中立な名前が確立された時点で `-operator` サフィックスは
  不要になりました。`~/.claude/skills/rallish-operator/` の既存インストールは
  自動マイグレーションされません; アップグレード後に `rm -rf
  ~/.claude/skills/rallish-operator/ && rallish bootstrap` を実行してください。
  トリガーフレーズ (`랠리보낼 준비해`、`let's serve`、…) は変更なし; フロントマター
  `name:` またはトリガー文字列でスキルを解決するエージェントは再ブートストラップ後に
  すぐ動作し続けます。`go:embed` パス、`defaultSkillTarget()`、リポルートの
  `skills/rallish` シンボリックリンクはすべて整合しています。

## [0.2.0] - 2026-05-18

### 追加

- **ラリー自動ループ** — スキルが各サイドで 1 回のセットアップトリガー後に
  両サイドのラリーを自律的に駆動します。サーバー側は `rally new --first server`
  でバトンを事前割り当てします (SSE phantom-join 不要)。レシーバー側は最初の
  `rally status` ポーリングでバトンを受け取ります。デフォルト `WAIT_MODE=yield`:
  エージェントが `rally done` 後にユーザーに制御を返し、次のユーザーメッセージで
  status を確認して自分のターンなら続行します。オプトイン `WAIT_MODE=block` は
  `rally join --once --timeout <dur>` で利用可能 (既に準備済みのセッション用)。
  パターン別終了シグナルでループが自動終了します。
- **クロスベンダー互換性を検証**: rallish-operator スキルがブランドグループ
  パス `~/.claude/skills/` を通じて Claude Code、Kimi、Codex、Cursor などの
  スキル対応 CLI で自動検出されます。ベンダーごとの設定は不要。ライブ検証:
  Claude Code と Kimi 間の discuss パターンラリーが 4 ターンで相互 `[agree]`
  に到達。スキル本体とハンドブックにクロスベンダーの callout を追加。
- **どのプロジェクトからでも使用可能**: rallish スキル、デーモン、バイナリ
  はすべてグローバル (`~/.claude/skills/`、`~/.rallish/`、`/usr/local/bin`)
  に配置されます。初回インストール後はソースツリーへの依存がありません。
  新しいハンドブックセクション
  [どのプロジェクトからでも rallish を使う](docs/handbook.md#using-rallish-from-any-project)
  と README の callout でプロジェクト非依存ワークフローを文書化。
  `rallish squash` の `--repo` フラグはセッションメタデータのみで、スキルや
  デーモンの場所とは無関係。

### 変更

- **シングルインスタンスデーモン保護**: `rallish daemon` が
  `~/.rallish/rallish.sock` に既にバインドされているインスタンスがある場合、
  起動を拒否して以下を出力し非正常終了します:
  `rallish daemon already running at <path> — not starting a second instance`
  以前は 2 回目の呼び出しがライブデーモンのソケットファイルを静かに unlink
  して最初のデーモンを孤立させていました。復旧: `kill -TERM $(pgrep -f
  "rallish daemon")` 後に再起動。

- 自動ループを可能にする新しい CLI オプション 2 つ:
  - `rally new --first <name>` — セッション作成時にバトンを事前割り当て;
    SSE phantom-join トリックが不要。
  - `rally join --once [--timeout <dur>]` — 最初のバトンイベント後に
    クリーンに終了、タイムアウト時は exit code 2。フラグ不使用時の
    デフォルト動作 (無限ブロッキング、複数イベント受信) は維持。
  後方互換: 既存のセッションおよび CLI 呼び出しは変更なし。

## [0.1.2] - 2026-05-17

### 追加

- **ラリーパターン** — ラリープリミティブの上にレイヤーされた 3 つの動作
  パターン: **cycle** (プランナー ↔ エグゼキューター、`[plan]`/`[result]`/
  `[review]` ノート規約)、**discuss** (ピア ↔ ピア、相互 `[agree]` に収束
  する設計議論)、**help** (オーナー ↔ ヘルパー、`[stuck]`/`[hint]`/`[try]`/
  `[resolved]` による短い非対称交換)。パターンはサーバー準備時に自然言語
  キューで選択 (`"사이클로 가자"`、`"논의 랠리"`、`"막혔어 도와줘"`)。
  ブローカー / CLI / コントラクト変更なし; rallish-operator スキル本体に
  規約としてエンコード (v0.1.0 → v0.2.0)。参照:
  [docs/prd-rally-patterns.md](docs/prd-rally-patterns.md) および
  [docs/runbook-rally-mode.md#rally-patterns](docs/runbook-rally-mode.md#rally-patterns)。

## [0.1.1] - 2026-05-17

### 変更

- `.goreleaser.yaml` の `brews:` ブロックを一時的に無効化。Homebrew
  tap リポジトリ (`jazz1x/homebrew-rallish`) と `TAP_GITHUB_TOKEN`
  シークレットがまだ未設定で、v0.1.0 のリリースパイプラインが brew
  publish ステップで失敗。tap セットアップまでは curl ワンライナー、
  `npx skills add`、またはソースビルドで使用。Homebrew は次のリリースで
  復活予定。

## [0.1.0] - 2026-05-17

2 つのライブコーディング CLI セッション間のライブバトン受け渡し
(ラリーモード) を追加し、運用プレイブックをベンダー中立なスキル
バンドルとしてパッケージ化し、IPC + タグパイプラインを強化。タグ
発行時に v0.1.0 としてリリース予定。

### 追加

- **ラリーモード** — ライブバトン受け渡しプリミティブ
  (`rally new/join/done/status`)。
  - エージェント主導 UX: 自然言語トリガー 3 つ (`랠리보낼 준비해` /
    `랠리받을 준비해 <SID>` / `시작` / `내 차례` / `끝`) でセッション全体を
    駆動; エージェントがすべての rallish CLI 呼び出しを代行。
  - テニステーマ(🎾): `server` / `returner` ロール、同時 1 バトン。
  - セッション ID フォーマット `rly_<unixmillis>_<rand4hex>`; SSE
    ハートビート (15 秒); 非活性参加者検出; 排他的ホルダー強制 (409)。
  - `broker.CloseAllRallies()` が SIGTERM 時に `{"closed":true}` を
    ブロードキャストし、SSE クライアントが 5 秒シャットダウン期限内に
    正常終了。
- **`rallish-operator` スキルバンドル** — `skills/rallish-operator/` の
  ベンダー中立スキル (canonical は `internal/skills/rallish-operator/`、
  シンボリックリンク)。ワンライナーインストール:
  `npx skills add jazz1x/rallish`。
  - `scripts/install-binary.sh` をバンドル — 初回トリガー時に `rallish` が
    PATH に無いと検出するとエージェントがバンドル済みインストーラを実行
    (uname → GitHub Release tarball → `/usr/local/bin` または
    `~/.local/bin`)。
  - `//go:embed all:rallish-operator` でバイナリに埋め込み;
    `rallish skill install` / `rallish bootstrap` で展開。
- **Squash アンブレラ** — `rallish squash` が `rallish start` を置き換え、
  ヘッドレスプリセットオーケストレータ (`solo-ralph`, `pair-review`) を
  カバー。後方互換エイリアスなし (AGENTS.md 規約)。
- **Unix ドメインソケット IPC** — `~/.rallish/rallish.sock` (モード `0600`)
  をプライマリ CLI↔Daemon トランスポートに; A2A クライアントと Windows
  フォールバック (ビルドタグスタブが `ErrUnsupported` を返す) 用 TCP
  ループバックを保持。
- **`rallish doctor`** — ソケット経由デーモン到達性を報告、ソケット権限を
  チェック (0600 より緩い場合は警告)。
- **A2A プロトコルレイヤー** — `GET /.well-known/agent.json`, `POST /a2a`
  (JSON-RPC 2.0: `tasks/send`, `tasks/get`, `tasks/cancel`,
  `tasks/sendSubscribe` SSE)、`pkg/contract/a2a.go`。
- **トークン予算の強制適用** — ブローカー (`handleNextTurn`)。
- **`internal/scratch/scratch.go`** — `max_kb` 超過時に自動圧縮;
  アダプタープロンプトへのモデルヒント注入。
- **`internal/safepath/`** — ユーザー入力パスの traversal ガード; ラリー
  `--repo` フラグで使用。
- **リリースヘルパー** — `make release-{patch,minor,major,dry-run}`。
  `scripts/release.sh` が VERSION バンプ、README バッジ伝播、コミット、
  タグ、プッシュ; ダーティーツリー / 非 main ブランチ / 未プッシュコミット
  / 非単調バージョン / 既存タグ (ローカル + リモート) を拒否。
- **Lefthook フック** — `commit-msg` (conventional prefix 強制)、
  `pre-commit` (fmt/vet/test/lint)、`pre-push` (build/vet/test)。
- **LICENSE** (MIT) — README バッジと goreleaser アーカイブ `files:`
  グロブのバッキング。
- **PRD + ランブック** — `docs/prd-rally-mode.md`, 
  `docs/runbook-rally-mode.md`。

### 変更

- `rallish start` 削除; 既存スクリプトは `rallish squash` へ移行必須。
- Runner HTTP クライアントがソケット対応トランスポートを使用。以前は
  すべての `next`/`turn` リクエストが汎用 `http.Client` を通り、
  `http://rallish.local` の DNS lookup が silent fail。
- デーモンクリーンアップを強化: TCP serve エラー時も Unix listener を閉じ、
  socket-pointer + port ファイルを削除。
- AGENTS.md: conventional commits が `commit-msg` フックで機械強制。
  許可される prefix: `feat fix refactor docs test chore sec ci build
  perf style`。Feature Documentation Workflow + 新規パッケージ layout
  行を追加。
- README / CHANGELOG / DESIGN.md を EN / KO / JP 3 言語で lockstep
  同期。
- 3 言語の README を再編成: 単一の `npx skills add jazz1x/rallish`
  ヘッドライン; パワーユーザー向けインストールパスは `<details>`
  ブロックに格下げ。

### 修正

- Runner HTTP クライアントがソケット対応でなかった — 初回セッション作成後
  すべてのポーリングが失敗 (ブローカーのラリー `data:` 行完了が届かない)。
  `runner.NewLoopWithClient` で修正。
- `handleRallyBaton` の遅延 join 分岐が history から前回 note を読まない
  ため、ハンドオフ後に join した参加者が `note=""` を見ていた問題を修正。
- デーモン SIGTERM 時、graceful shutdown 前に TCP serve エラーが発生
  すると `rallish.sock`, `socket`, `port` がディスクに残るリークを修正。
- Unix ドメインソケットが既定の `0755` 権限で作成されていた; 現在は
  `Listen` 後に明示的に `chmod 0600`。
- `daemon` と `doctor` cobra コマンドの `Short` 説明が `--help` で空に
  なっていた問題を修正。
- Cobra エラーが `SilenceErrors: true` で出力されない — 無効な参加者名
  などの検証失敗が stderr なしで exit 1。現在は `Error: ...` を stderr に
  出力してから終了。
- `make check` が `.toolchain/bin/` にピン留めされた `golangci-lint` では
  なく `$PATH` のものを探していた問題を修正。Makefile が toolchain
  バイナリを自動検出 + Go ランタイムを prefix。

### セキュリティ

- Unix ソケット権限を `0600` に強化 (ブローカー側)。
- Socket-pointer ファイル (`~/.rallish/socket`) を `cli.RunStart` で
  使用前に rallish home ルート内にあるか検証し、改竄されたポインタによる
  traversal を遮断。
- `--repo` パスが `internal/safepath.Clean` と明示的 `os.Stat` ディレクトリ
  チェックを経てブローカーに渡る。
- `forbidigo` lint ルールでライブラリコード内の `os.Environ()` と
  `exec.Command("sh"…)` を禁止 (DESIGN.md §14)。
- `govulncheck` をすべての push/PR で実行 (`.github/workflows/ci.yml`)。
- すべてのリリースアーティファクトに `cosign` keyless 署名 + `syft` SBOM
  (`.goreleaser.yaml`)。

### CI / パイプライン

- `release.yml` が push されたタグを `^v[0-9]+\.[0-9]+\.[0-9]+$` 正規表現
  で検証してから goreleaser を呼び出し; 権限スコープを最小
  (`contents:write` + `id-token:write`) にトリム。
- `ci.yml` build ジョブが `CGO_ENABLED=0` (goreleaser と一致) を明示;
  build 後に `dist/rallish version` のスモークラン; マトリックスは
  macOS + Linux をカバー。
- Dependabot が `gomod` と `github-actions` の両エコシステムを追跡。

### 既知の follow-up (次リリースに延期)

- サードパーティ GitHub Actions を SHA でピン留め (現在 mutable タグ)。
- CodeQL ワークフロー追加。
- CI ビルドマトリックスに Windows を追加。
- 70% カバレッジフロアを CI で強制 (現在 AGENTS.md 文書のみ)。
- `SECURITY.md` / `CODE_OF_CONDUCT.md`。
