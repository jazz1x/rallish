# rallish

[![version](https://img.shields.io/badge/version-0.2.0-blue)](CHANGELOG.jp.md)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![go](https://img.shields.io/badge/go-1.25+-blue)](go.mod)

> *マルチエージェントのターン制実行のためのローカルブローカー、A2A対応。*

[English](README.md) · [한국어](README.ko.md)

## rallish とは

rallish は複数のエージェントランタイム間に配置される小さなローカルブローカープロセスです。各ランタイムは `claude`, `kimi`, `codex` などの既製のコーディング CLI です。ブローカーは会話状態を管理し、誰のターンかを決定し、エージェント間で簡潔なターンのペイロードを中継します。

ワイヤフォーマットは合理的な範囲で **A2A（Agent2Agent）プロトコル** に準拠しており、アダプターを介して A2A 対応エージェントを接続できます。

## 機能

| 機能 | 説明 |
|------|------|
| **ターン制実行** | 共有ブローカーを介してエージェントが交互に実行 |
| **A2A プロトコル** | `/.well-known/agent.json`, JSON-RPC 2.0 タスク, SSE ストリーミング |
| **トークン予算** | セッションごとのトークン、ターン数、時間の上限を強制 |
| **スクラッチパッド** | 自動圧縮(compaction)が適用されたローリング共有スクラッチ |
| **プリセット** | 役割、ルーティング、終了条件を定義した YAML テンプレート |
| **セキュリティ** | パストラバーサル防御、シークレットマスキング、最小限の環境変数許可リスト |

## クイックスタート

### 前提条件

- Go 1.25+
- `claude` CLI および/または `kimi` CLI のインストールと認証完了

### ビルド

```bash
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
```

### 実行

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

## プリセット

同梱プリセットは `internal/preset/presets/` にあります:

| プリセット | 役割 | 説明 |
|------------|------|------|
| `solo-ralph` | 1 × claude | 予算制限付きの単一エージェント実行 |
| `pair-review` | planner, executor, reviewer | 構造化されたレビューループ |

カスタムプリセットは `~/.rallish/presets/<name>.yaml` に配置できます。

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
   │   claude   │       │    kimi    │
   └────────────┘       └────────────┘
```

## セキュリティ

脅威モデルの詳細は [DESIGN.md](DESIGN.md) §14 および [docs/handbook.md](docs/handbook.md) を参照してください。

## 貢献

1. `make check` がパスすること (`go vet`, `golangci-lint`, `go test -race`)
2. Conventional Commits に従うこと (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`)
3. `internal/session`, `internal/router`, `internal/exit`, `internal/preset`, `pkg/contract` でテストカバレッジ 70% 以上を維持すること

## ライセンス

MIT © jazz1x
