# rallish

> マルチエージェントのターン制実行のためのローカルブローカー、A2A対応。

![version](https://img.shields.io/badge/version-0.3.0-blue)
![license](https://img.shields.io/badge/license-MIT-green)
![go](https://img.shields.io/badge/go-1.25+-blue)


> **注:** `main` ブランチは最新タグより多くの機能を含んでいます. 最新の変更は
> [CHANGELOG.md](CHANGELOG.md) の `[Unreleased]` セクションを参照してください.
**rallish** は複数のエージェントランタイム間に配置される小さなローカルブローカープロセスです。各ランタイムはアダプタさえあれば、どのコーディング CLI（Claude, Kimi, Cursor, Codex など、同種でも異なるコンテキストで実行する場合も含む）も使用できます。ブローカーは会話状態を管理し、誰のターンかを決定し、エージェント間で簡潔なターンのペイロードを中継します。

すべてローカルで実行されます。クラウドブローカーや外部調整サービスはありません。ワイヤフォーマットは合理的な範囲で **A2A（Agent2Agent）プロトコル** に準拠しており、アダプターを介して A2A 対応エージェントを接続できます。

[English](./README.md) · [한국어](./README.ko.md)

## 機能

| 機能 | 説明 |
|------|------|
| **Squash（ヘッドレス）** | `rallish squash` でヘッドレスプリセットセッションを実行（`solo-ralph`、`pair-review`）; ブローカーがアダプターを自動スポーン |
| **Rally（インタラクティブ）** | `rallish rally` で 2 つのコーディング CLI セッション間のライブバトン受け渡し; エージェントがピンポンを自律ループ (ターンごとのユーザートリガー不要); SSE による排他的ホルダー強制 |
| **A2A プロトコル** | A2A v1.0 ワイヤ形状: `/.well-known/agent-card.json`, `protocolVersion`, PascalCase JSON-RPC タスク, SSE ストリーミング. 署名済みカードと相互認証は未実装（先送り）. |
| **トークン予算** | セッションごとのトークン、ターン数、時間の上限を強制 |
| **スクラッチパッド** | 自動圧縮(compaction)が適用されたローリング共有スクラッチ |
| **プリセット** | 役割、ルーティング、終了条件を定義した YAML テンプレート |
| **Unix ソケット IPC** | CLI↔Daemon が `~/.rallish/rallish.sock`(`0600`) 経由。A2A 外部クライアントと Windows フォールバック用に TCP ループバックを保持 |
| **自動デーモン** | `rallish squash` がブローカー未起動時に自動スポーン。`rallish doctor` がソケット到達性を報告 |
| **セキュリティ** | パストラバーサル防御、シークレットマスキング、最小限の環境変数許可リスト |

## 自律作業ハーネス (Autonomous Work Harness)

rallish はベンダー中立・リポローカルの**作業ハーネス**です。エージェントランタイムが長期の自律リポジトリ作業を安全に、再開可能に、検証可能に、監査可能に実行できるようにします — ループそのものにはなりません。六つのガードレール柱:

- **Safety & resumability** — アトミックな `.bak` 回復チェックポイント状態; `cycle run --once` は cron/スケジューラが呼び出す有界基準ドライバです (終了コード = 停止理由)。
- **Verification gates** — parse-don't-validate エージェントハンドシェイク、gate self-eval、ハッシュ固定 gate 定義。
- **Interop** — A2A v1.0 ワイヤ形状 (`/.well-known/agent-card.json` の Agent Card、実際の `protocolVersion`、厳格な型付きインテーク). 署名済みカードと相互認証は未実装（先送り）.
- **Audit** — `schema_version` スタンプ、ハッシュチェーン、再生可能な台帳 + RFC 9162 Merkle 包含/一貫性証明。
- **Anti-spin** — スタック/予算サーキットブレーカー + スティッキーホルト復活防止ガード (cron が再起動したスピニング実行は自己停止し、復活しない)。
- **Action-gate** — 実行前の破壊的コマンド拒否リスト + シークレット封じ込め; rallish が決定を宣言・記録し、ランタイムフックが強制します。

全体の方針と根拠: `docs/north-star.md`.

## アーキテクチャ

```
┌──────────────────────────────────────────┐
│  rallish ブローカー (Go)                 │
│  POST /sessions                          │
│  GET  /sessions/:id/next?as=<role> (SSE) │
│  POST /sessions/:id/turn                 │
│  GET  /.well-known/agent-card.json       │
│  POST /a2a                               │
│  GET  /mcp/sse                           │
│  POST /mcp/message                       │
└──┬───────────────┬───────────────────┬───┘
   │ unix socket   │ unix socket       │ tcp ループバック
   │ ~/.rallish/   │ ~/.rallish/       │ 127.0.0.1:<port>
   │ rallish.sock  │ rallish.sock      │ (A2A + フォールバック)
┌──┴─────────┐   ┌─┴────────┐      ┌──┴───────────┐
│エージェントA│   │エージェントB│      │ 外部 A2A     │
│  (CLI)     │   │  (CLI)   │      │ クライアント │
└────────────┘   └──────────┘      └──────────────┘
```

同じブローカーが両トランスポートを同時に提供します。CLI(`rallish squash`, `rallish rally`, `rallish doctor`) は Unix ソケットを優先し、外部 A2A クライアントは TCP ループバックを使用します。

## 前提条件

- **Go 1.25+** (ソースビルド用)
- 互換性のあるエージェント CLI が 1 つ以上インストールされ、認証済みであること（サポートされているアダプターを参照）

確認方法:

```bash
go version        # 1.25+ であること
which claude      # $PATH 上のサポート対象アダプターバイナリ
```

## インストール

コマンド 1 つ:

```bash
npx skills add jazz1x/rallish
```

スキルバンドル (SKILL.md + バイナリインストーラ) を
`~/.claude/skills/rallish/` に配置します。
[skills.sh](https://www.skills.sh) 経由で解決。

任意のプロジェクトで Claude Code (またはスキル対応の他のコーディング CLI)
を開き、`랠리보낼 준비해` / `let's serve` と入力。初回使用時にバンドル済み
のプラットフォーム検出スクリプト (`scripts/install-binary.sh`) で `rallish`
バイナリを自動インストール (最新 GitHub Release → `/usr/local/bin` または
`~/.local/bin`)。

<details>
<summary><b>パワーユーザー向け (バンドルをバイパス)</b></summary>

| 方法 | コマンド |
|---|---|
| **curl** (Unix 全般) | `curl -fsSL https://raw.githubusercontent.com/jazz1x/rallish/main/install.sh \| sh` |
| **Homebrew tap** (macOS) | _準備中_ — リポジトリ issue で追跡中; まだ提供されていません |
| **ソースビルド** | `git clone https://github.com/jazz1x/rallish && cd rallish && make build` |
| **`go install`** | `go install github.com/jazz1x/rallish/cmd/rallish@latest` |

バイナリが `$PATH` にあれば `rallish bootstrap` (冪等) がスキルバンドル
インストールとデーモン検証を行います。
</details>

> ✓ rallish はプロジェクトごとではなくユーザーごとに一度だけ実行されます。
> 初回インストール後は rallish ソースツリー内にいる必要はなく、
> どのプロジェクトディレクトリからでも rally を使用できます。
> デーモンは `~/.rallish/` にグローバルに配置されます。
> プロジェクト非依存のワークフローは
> [docs/handbook.md#using-rallish-from-any-project](docs/handbook.md#using-rallish-from-any-project)
> を参照してください。

## クイックスタート

```bash
# 1コマンドのセットアップ ウィザード（スキル + 3 つの質問）
./dist/rallish bootstrap

# 環境チェック（アダプター + デーモンをグリフ ステータスで表示）
./dist/rallish doctor

# 設定の確認 / 変更（~/.rallish/config.yaml）
./dist/rallish config list
./dist/rallish config set wait_mode block
./dist/rallish config edit              # $EDITOR を起動

# インタラクティブな コンポーネント ピッカー（npx スタイル）
./dist/rallish add

# 同梱アダプター/プリセット一覧
./dist/rallish add --list

# 自然言語フレーズでバンドルスキルを実行 (例: 自律サイクル)
rallish trigger "자율 사이클"   # --dry-run で実行せずにコマンドをプレビュー

# ヘッドレスプリセットセッションを開始 (デーモン自動スポーン)
./dist/rallish squash \
  --preset pair-review \
  --task "OAuth2 サポートを追加" \
  --repo ./my-project

# 実アダプターなしでスモークテスト (fake / 決定論的, 3 ターン)
cat > ~/.rallish/presets/fake-demo.yaml <<'EOF'
name: fake-demo
roles:
  - {id: ralph, runtime: fake, model: fake-1}
routing: round_robin
budget: {max_turns: 3, max_tokens: 10000, deadline_minutes: 5}
exit_when: [turns_exhausted]
scratch: {max_kb: 16}
EOF
./dist/rallish squash --preset fake-demo --task "smoke test" --repo /tmp

# `bootstrap` 以降は任意のディレクトリから bare `rallish` コマンドを使用します。
# `./dist/rallish` はソースビルドを直接実行する場合のみ使用します。

# 2 ターミナル テニスラリー (ライブバトン受け渡し)
# skills/rallish による自然言語 UX を推奨 —
# エージェント (Claude Code, Cursor など) がすべての rally コマンドを代行します。
# ターミナル A のコーディング CLI セッション:        "랠리보낼 준비해 — 사이클로 가자"
# エージェント: rally new --first server + role=server, SID 出力, 初回ターンをサーブ, yield。
# ターミナル B のコーディング CLI セッション:        "랠리받을 준비해 <SID>"
# エージェント: パターン解析, role=returner, 即座にバトンを受け取り, yield。
# 各ターン終了後エージェントはユーザーに制御を返し、次のユーザーメッセージで
# status を確認して自分のターンなら続行します。ターンごとの "내 차례" は不要。
# どちらでも、いつでも終了するには:               "끝"
#
# Raw CLI (スキルが内部呼び出し / スクリプト用):
SESSION=$(./dist/rallish rally new --participants server,returner --task "warm-up rally")
./dist/rallish rally status --session-id $SESSION
./dist/rallish rally done   --session-id $SESSION --as server --note "draft v1"

# cron/スケジューラが呼び出す有界ワンショットパス (終了コード = 停止理由)
rallish cycle run --once --cycle-id <id>
# ランタイム PreToolUse フックが呼び出す実行前ポリシーゲート (宣言 + 記録; フックが強制)
rallish gate tooluse --command 'rm -rf /'    # -> {"verdict":"deny",...}  exit 13
# ゲート終了コード: 0=許可, 13=拒否, 14=エラー; --cycle-id で判決を記録できます。

# MCP 経由の rally (デーモンの MCP 2025-03-26 表面を使うワンショットクライアント)
rallish rally mcp-agent --mode create --participants alice,bob --task "refactor auth"
rallish rally mcp-agent --mode join  --session-id <id> --as alice --timeout 30s
rallish rally mcp-agent --mode done  --session-id <id> --as alice --handoff-to bob
rallish rally mcp-agent --mode status --session-id <id>

# A2A discovery (外部クライアントは TCP ループバックを使用)
curl http://127.0.0.1:$(cat ~/.rallish/port)/.well-known/agent-card.json

# A2A タスク送信 (v1.0 メソッド名; tasks/send はレガシーエイリアスとして引き続き動作)
curl -X POST http://127.0.0.1:$(cat ~/.rallish/port)/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"Hello"}]}}}'
```

ターンごとのリクエスト/レスポンスは `~/.rallish/sessions/<id>/log.jsonl` に記録されます。

## 使い方

### 1. ヘッドレスセッションを開始

```bash
rallish squash --preset <name> --task "<説明>" --repo <パス>
```

プリセットは `internal/preset/presets/` (同梱) または `~/.rallish/presets/` (カスタム) にあります。プリセットの作成方法は [docs/handbook.md](docs/handbook.md) を参照してください。

### 1b. インタラクティブラリーセッションを開始

**エージェント主導 (推奨).** スキルを自動検出するコーディング CLI
(Claude Code, Cursor など) でこのリポを開くと、
[`skills/rallish`](skills/rallish/SKILL.md) スキルが
以下の自然言語トリガーでロードされます:

| 発話 | エージェント動作 |
|---|---|
| `랠리보낼 준비해` / `let's serve` | `rally new` 実行、ROLE=`server`、SID 出力 |
| `랠리받을 준비해 <SID>` / `let's return` | `rally status` 実行、ROLE=`returner`、待機 |
| `시작` / `go` (サーバー側) | 最初のターンをサーブ後、要約 note 付きで `rally done` |
| `내 차례` / `is it my turn` (レシーバー側) | `rally status`; 自分のターンなら前 note を読み作業後 `rally done` |
| `끝` / `match over` | クリーン終了 |

`시작` / `go` / `끝` / `내 차례` のような短いトリガーは、直前の prep
トリガーで ROLE+SID が既に設定された場合にのみ有効化 — 無関係な汎用語は
無視されます。

**Raw CLI (スクリプトやスキル未対応クライアント用):**

```bash
rallish rally new       --participants <a>,<b> [--task "<説明>"]
rallish rally join      --session-id <id> --as <名前>           # SSE ブロック
rallish rally done      --session-id <id> --as <名前> [--note "<サマリ>"] [--handoff-to <名前>]
rallish rally status    --session-id <id>
rallish rally mcp-agent --mode create|join|done|status|interrupt [...]
```

`mcp-agent` サブコマンドは MCP 2025-03-26 ワンショットクライアントです.
SSE と JSON-RPC を内部で処理し、ツール結果の JSON を出力します. 詳細は
[docs/mcp-compatibility.md](docs/mcp-compatibility.md) と
[docs/runbook-rally-mcp-agent.md](docs/runbook-rally-mcp-agent.md) を参照.

完全な 2 ターミナルウォークスルーは [docs/runbook-rally-mode.md](docs/runbook-rally-mode.md)
を参照してください。

### 2. A2A 連携

A2A 対応クライアントはタスクを発見して送信できます:

| メソッド | パス | 説明 |
|----------|------|------|
| `GET` | `/.well-known/agent-card.json` | Agent Card (v1.0; `/.well-known/agent.json` レガシーエイリアス) |
| `POST` | `/a2a` | JSON-RPC 2.0 (SendMessage, GetTask, CancelTask, SubscribeToTask; レガシー `tasks/*` エイリアス) |

完全なマッピングは [docs/a2a-compatibility.md](docs/a2a-compatibility.md) を参照してください。

### 3. 同種ペアリング

Claude 2 つ、Kimi 2 つ、または任意の組み合わせも可能です。ブローカーはターン順序のみを管理し、ベンダーは気にしません。

### 4. 予算状態の確認

予算（トークン、ターン数、デッドライン）はセッションごとに強制されます。予算が尽きると、ブローカーは `410 Gone` を返し、再開できるようにスクラッチパッドを保存します。

### 5. カスタムプリセット

`~/.rallish/presets/<name>.yaml` に YAML ファイルを配置してください:

```yaml
name: my-preset
description: 任意の 1 行サマリ。
roles:
  - id: planner
    runtime: claude
    model: opus
  - id: executor
    runtime: kimi
    model: kimi-k2
routing: handoff_then_round_robin    # または round_robin
budget:
  max_turns: 20
  max_tokens: 400000
  deadline_minutes: 60
exit_when: [tests_pass, reviewer_approved, turns_exhausted]
scratch:
  max_kb: 64
  summarize_with: claude-haiku
```

### 6. 自律サイクル（ハーネス）

`cycle` サブコマンドはブローカーを経由するため、**先にデーモンを起動**しておく必要があります. ただし `cycle run --once` はファイルから状態を直接再開します:

```bash
rallish daemon &                                       # 先にデーモンを起動
rallish cycle new --goal "feat: 認証追加" --branch feat/auth
rallish cycle start --cycle-id <id>                    # ワンショット create + orchestrate + watch
rallish cycle run --once --cycle-id <id>               # デーモン不要 — ファイルから直接再開
rallish cycle status --cycle-id <id>                   # 進捗、ゲート、台帳サマリー
rallish cycle ledger --cycle-id <id>                   # append-only ハーネス台帳を出力
rallish cycle next --cycle-id <id>                     # 対話的に 1 ターン進める
rallish cycle watch --cycle-id <id>                    # ターンを実行せずに状態を追跡
rallish cycle halt --cycle-id <id>                     # 実行中のサイクルを停止
```

作成時によく使うフラグ:

```bash
rallish cycle new --goal "テスト修正" --branch feat/fix \
  --audit-cmd "npm test" \
  --polish-test-cmd "npm test" \
  --agents claude,kimi \
  --max-lifetime-turns 100 \
  --max-duration 4h \
  --local-gate "make lint"
```

- `--audit-cmd` はデフォルトの `make check-all` オーディットゲートを上書きします.
- `--polish-test-cmd` はデフォルトの `go test -race ./...` ポリッシュゲートを上書きします.
- `--agents` は参加者のローテーションを設定します.
- `--max-lifetime-turns` / `--max-duration` はハードな anti-spin 上限です.
- `--local-gate` はサイクル状態に保持されるプロジェクト固有のチェックを追加します.

`--audit-cmd` や `--polish-test-cmd` に空白のみを指定するとエラーになり、デフォルトへのサイレントフォールバックは行われません. `scripts/check-no-raw-ansi.sh` チェックは rallish リポジトリ専用であり、スクリプトが存在しないリポジトリでは自動的にスキップされます（エラーではありません）.

`cycle run --once` は `~/.rallish/cycles/cycle-<id>.json` から直接再開します. このパスは作業対象リポジトリの**外**にあるため、サイクルが自身の Preflight が要求するクリーンなワーキングツリーを汚しません. `--state-dir` で変更できます.
### 7. デーモンのライフサイクル

```bash
rallish daemon &                            # 明示起動 (任意 — squash が自動スポーン)
ls ~/.rallish/                              # rallish.sock (0600), socket, port, sessions/
kill -TERM $(pgrep -f "rallish daemon")     # graceful 終了で 3 ファイル全て削除
```

`rallish doctor` で到達性確認:

```
daemon reachable via unix socket path=/Users/<you>/.rallish/rallish.sock perm=-rw-------
```

Windows ではブローカーは TCP ループバックのみを使用します (Unix ソケットスタブが `ErrUnsupported` を返却)。

## セキュリティ

- ブローカーは `127.0.0.1` にのみバインドされます。
- v0 には認証レイヤーがありません。共有マシンではリバースプロキシまたはローカルファイアウォールを使用してください。
- プリセットファイルは読み込み前にパストラバーサル攻撃を検証します。
- 環境変数のシークレット情報はログからマスキングされます。

脅威モデルの詳細は [DESIGN.md](DESIGN.md) §14 を参照してください。

## 開発

クローン後、一度だけプレコミットフックを有効にしてください:

```bash
make setup-hooks
```

完全な検証スイートを実行 (CI と同じゲート):

```bash
make check-all   # gofmt + go vet + go test -race + golangci-lint + no-raw-ansi + go mod verify
```

高速な部分集合は `make check` (go vet + golangci-lint + go test -race) を使用してください.

### テスト

```bash
make test    # go test ./...
make bench   # go test -bench=. -benchmem ./...
```

カバレッジ下限: `internal/session`, `internal/router`, `internal/exit`, `internal/preset`, `pkg/contract` で 70% 以上。

## 規約

コーディングガイドライン、プロジェクトレイアウト、コミット規則は [AGENTS.md](AGENTS.md) を参照してください。

## ライセンス

MIT — [LICENSE](./LICENSE) 参照。

> *"ラリーのように、誰もボールを独占しない時に最高のターンが生まれる。"*
