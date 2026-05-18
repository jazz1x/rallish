# 変更履歴

このプロジェクトのすべての主要な変更はこのファイルに記録されます。

形式は [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) に基づいており、
このプロジェクトは [Semantic Versioning](https://semver.org/spec/v2.0.0.html) に準拠しています。

## [未発表]

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
