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
| **ターン制実行** | 共有ブローカーを介してエージェントが交互に実行 |
| **A2A プロトコル** | `/.well-known/agent.json`, JSON-RPC 2.0 タスク, SSE ストリーミング |
| **トークン予算** | セッションごとのトークン、ターン数、時間の上限を強制 |
| **スクラッチパッド** | 自動圧縮(compaction)が適用されたローリング共有スクラッチ |
| **プリセット** | 役割、ルーティング、終了条件を定義した YAML テンプレート |
| **セキュリティ** | パストラバーサル防御、シークレットマスキング、最小限の環境変数許可リスト |

## アーキテクチャ

```
┌──────────────────────────────────────────┐
│  rallish ブローカー (Go, 127.0.0.1)      │
│  POST /sessions                          │
│  GET  /sessions/:id/next?as=<role> (SSE) │
│  POST /sessions/:id/turn                 │
│  GET  /.well-known/agent.json            │
│  POST /a2a                               │
└────────▲─────────────────────▲───────────┘
         │                     │
   ┌─────┴──────┐       ┌─────┴──────┐
   │  エージェントA │       │  エージェントB │
   └────────────┘       └────────────┘
```

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
# 環境チェック
./dist/rallish doctor

# ターン制実行セッションを開始
./dist/rallish start \
  --preset pair-review \
  --task "OAuth2 サポートを追加" \
  --repo ./my-project

# A2A discovery
curl http://127.0.0.1:$(cat ~/.rallish/port)/.well-known/agent.json

# A2A タスク送信
curl -X POST http://127.0.0.1:$(cat ~/.rallish/port)/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"Hello"}]}}}'
```

## 使い方

### 1. セッションを開始

```bash
rallish start --preset <name> --task "<説明>" --repo <パス>
```

プリセットは `internal/preset/presets/` (同梱) または `~/.rallish/presets/` (カスタム) にあります。プリセットの作成方法は [docs/handbook.md](docs/handbook.md) を参照してください。

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

### 4. カスタムプリセット

`~/.rallish/presets/<name>.yaml` に YAML ファイルを配置してください:

```yaml
name: my-preset
roles:
  planner:
    adapter: claude
    model: claude-3-5-sonnet-20241022
routing:
  - planner
exit:
  maxTurns: 10
```

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
