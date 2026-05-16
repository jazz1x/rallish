# 変更履歴

このプロジェクトのすべての主要な変更はこのファイルに記録されます。

形式は [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) に基づいており、
このプロジェクトは [Semantic Versioning](https://semver.org/spec/v2.0.0.html) に準拠しています。

## [未発表]

### 追加

- ラリーモード: インタラクティブ CLI セッション間のライブバトン受け渡し (`rally new/join/done/status`); セッション ID プレフィックス `rly_`; 排他的ホルダー強制; SSE ハートビート + 非活性検出。
- Squash 名称変更: `rallish start` → `rallish squash`; 後方互換エイリアスなし。
- CLI ↔ デーモンの主要トランスポートとして Unix ドメインソケット
  (`~/.rallish/rallish.sock`) を追加。TCP ループバックはフォールバックおよび
  A2A クライアント用に保持。デーモンはソケット権限を `0600` に強制し、CLI は
  ソケットポインタの改竄を防止。Windows ビルドはビルドタグスタブで TCP に
  フォールバック。
- `rallish doctor` がソケット経由のデーモン到達性を報告。
- A2A プロトコル レイヤー: `GET /.well-known/agent.json`, `POST /a2a` (JSON-RPC 2.0)
  - `tasks/send`, `tasks/get`, `tasks/cancel`, `tasks/sendSubscribe` (SSE)
- AgentCard, TaskState, JSON-RPC エンベロープを含む `pkg/contract/a2a.go`
- ブローカーでのトークン予算の強制適用 (`handleNextTurn`)
- `max_kb` 超過時に自動圧縮(compaction)を行う `internal/scratch/scratch.go`
- アダプタープロンプトへのモデルヒント注入

### 変更

- すべての "hocket" 用語を削除; "turn-taking" / "relay" に置き換え
- `lefthook.yml` をステージングされた Go ファイルのみスキャンするように更新
- `.golangci.yml` を v2 形式にアップグレードし `.toolchain` を除外
- `internal/cli/start.go` の forbidigo および errcheck 違反を修正

### 修正

- レジストリから実際の Claude/Kimi アダプターを復元
