# rallish

> マルチエージェントのターン制実行のためのローカルブローカー、A2A対応。

![version](https://img.shields.io/badge/version-0.0.1-blue)
![license](https://img.shields.io/badge/license-MIT-green)
![go](https://img.shields.io/badge/go-1.25+-blue)

**rallish** は複数のエージェントランタイム間に配置される小さなローカルブローカープロセスです。各ランタイムはアダプタさえあれば、どのコーディング CLI（Claude, Kimi, Cursor, Codex など、同種でも異なるコンテキストで実行する場合も含む）も使用できます。ブローカーは会話状態を管理し、誰のターンかを決定し、エージェント間で簡潔なターンのペイロードを中継します。

すべてローカルで実行されます。クラウドブローカーや外部調整サービスはありません。ワイヤフォーマットは合理的な範囲で **A2A（Agent2Agent）プロトコル** に準拠しており、アダプターを介して A2A 対応エージェントを接続できます。

[English](./README.md) · [한국어](./README.ko.md)

## 機能

| 機能 | 説明 |
|------|------|
| **Squash（ヘッドレス）** | `rallish squash` でヘッドレスプリセットセッションを実行（`solo-ralph`、`pair-review`）; ブローカーがアダプターを自動スポーン |
| **Rally（インタラクティブ）** | `rallish rally` で 2 つ以上の人間ターミナル間のライブバトン受け渡し; SSE による排他的ホルダー強制 |
| **A2A プロトコル** | `/.well-known/agent.json`, JSON-RPC 2.0 タスク, SSE ストリーミング |
| **トークン予算** | セッションごとのトークン、ターン数、時間の上限を強制 |
| **スクラッチパッド** | 自動圧縮(compaction)が適用されたローリング共有スクラッチ |
| **プリセット** | 役割、ルーティング、終了条件を定義した YAML テンプレート |
| **Unix ソケット IPC** | CLI↔Daemon が `~/.rallish/rallish.sock`(`0600`) 経由。A2A 外部クライアントと Windows フォールバック用に TCP ループバックを保持 |
| **自動デーモン** | `rallish squash` がブローカー未起動時に自動スポーン。`rallish doctor` がソケット到達性を報告 |
| **セキュリティ** | パストラバーサル防御、シークレットマスキング、最小限の環境変数許可リスト |

## アーキテクチャ

```
┌──────────────────────────────────────────┐
│  rallish ブローカー (Go)                 │
│  POST /sessions                          │
│  GET  /sessions/:id/next?as=<role> (SSE) │
│  POST /sessions/:id/turn                 │
│  GET  /.well-known/agent.json            │
│  POST /a2a                               │
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

### オプション 1 — ソースからビルド (開発用に推奨)

```bash
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
```

バイナリは `./dist/rallish` に生成されます。

### オプション 2 — Homebrew (初回リリース以降)

```bash
brew tap jazz1x/rallish
brew install rallish
```

### オプション 3 — go install

```bash
go install github.com/jazz1x/rallish@latest
```

## クイックスタート

```bash
# 環境チェック (アダプター有無 + デーモン到達性を報告)
./dist/rallish doctor

# 同梱アダプター/プリセット一覧
./dist/rallish add --list

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

# 2 ターミナルラリー (人間セッション間のライブバトン受け渡し)
# ターミナル A — セッションを作成して alice として参加:
SESSION=$(./dist/rallish rally new --participants alice,bob --task "ping pong")
./dist/rallish rally join --session-id $SESSION --as alice   # ブロック; alice が最初のバトンを取得

# ターミナル B — bob として参加 (alice がパスするまでブロック):
./dist/rallish rally join --session-id $SESSION --as bob

# どちらのターミナルからでも — バトンを渡す:
./dist/rallish rally done --session-id $SESSION --as alice --note "draft v1"

# セッション状態を確認:
./dist/rallish rally status --session-id $SESSION

# A2A discovery (外部クライアントは TCP ループバックを使用)
curl http://127.0.0.1:$(cat ~/.rallish/port)/.well-known/agent.json

# A2A タスク送信
curl -X POST http://127.0.0.1:$(cat ~/.rallish/port)/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"Hello"}]}}}'
```

ターンごとのリクエスト/レスポンスは `~/.rallish/sessions/<id>/log.jsonl` に記録されます。

## 使い方

### 1. ヘッドレスセッションを開始

```bash
rallish squash --preset <name> --task "<説明>" --repo <パス>
```

プリセットは `internal/preset/presets/` (同梱) または `~/.rallish/presets/` (カスタム) にあります。プリセットの作成方法は [docs/handbook.md](docs/handbook.md) を参照してください。

### 1b. インタラクティブラリーセッションを開始

```bash
# セッションを作成; セッション ID を出力
rallish rally new --participants <名前1>,<名前2> [--task "<説明>"]

# 各参加者が自分のターミナルで参加 (バトン待機中にブロック)
rallish rally join --session-id <id> --as <名前>

# 完了時にバトンを渡す
rallish rally done --session-id <id> --as <名前> [--note "<サマリ>"]

# いつでも状態を確認
rallish rally status --session-id <id>
```

完全な 2 ターミナルウォークスルーは [docs/runbook-rally-mode.md](docs/runbook-rally-mode.md) を参照してください。

### 2. A2A 連携

A2A 対応クライアントはタスクを発見して送信できます:

| メソッド | パス | 説明 |
|----------|------|------|
| `GET` | `/.well-known/agent.json` | Agent Card |
| `POST` | `/a2a` | JSON-RPC 2.0 (tasks/send, tasks/get, tasks/cancel, tasks/sendSubscribe) |

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

### 6. デーモンのライフサイクル

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

完全な検証スイートを実行:

```bash
make check   # go vet + golangci-lint + go test -race
```

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
