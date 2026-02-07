# firebaseToPostgre 最終まとめ

## 概要
Firebase → PostgreSQL 移行ツールを、postgreToPostgre と同じアーキテクチャへ全面リライト。Firebase固有処理は維持しつつ、WebSocket進捗・リクエストベース設定・Manager/Migrator構成へ移行した。

## 重要な注意点
- **RailwayのDATABASE_URLでのみデバッグ済み**。ローカルDBや他環境での検証は未実施。
- サブコレクション対応は **Firestoreのみ**。RTDBは collection group 非対応。
- public スキーマの全オブジェクトを移行前にDROPする仕様。

## 新構成
- `internal/firebase` : Firestore/RTDB 読み取り
- `internal/database` : PostgreSQL 接続・クエリ
- `internal/migration` : Manager/Migrator/Progress
- `internal/websocket` : WebSocket hub/client
- `internal/server` : API/WS

## 主要API
- `POST /api/test-firebase`
- `POST /api/test-postgres`
- `POST /api/collections`
- `POST /api/collections/tree`
- `POST /api/mappings/ai`
- `POST /api/migrate`
- `POST /api/migrate/cancel`
- `GET  /api/migrate/status`
- `POST /api/preview`
- `POST /api/schema/infer`（UI未使用）
- `GET  /ws/progress`

## サブコレクションのRDB化
- コレクショングループ形式: `parent/*/sub`
- 例: `users/*/clients`
- FK列は `{parent}_id` を自動追加（`TEXT`, `nullable:false`）

## 進捗/エラー対策
- read == written を満たす場合は次フェーズへ進む
- read の連続エラーが3回発生したら停止
- 変換エラー発生時に自動再推論（1回のみ）

## OpenAI マッピング生成
- `OPENAI_API_KEY` を利用
- プロンプトは `prompt/ai-mapping.md` に分離
- 予約語は target 側で名称変更（例: `group` → `group_name`）

## UI/UX
- AIマッピング実行状態を表示
- コレクション取得で親/サブコレクション一覧からマッピング生成
- カラム自動取得は廃止（AIベース）

## 破壊的挙動
- 移行開始時に **public スキーマの全テーブル/ビュー/シーケンス/マテビューをDROP** する

## .env.example
```
ADDR=:8081
OPENAI_API_KEY=your_openai_api_key_here
OPENAI_MODEL=gpt-4o-mini
```

## 未実施/今後の確認
- ローカル/他環境の接続確認
- 予約語リストの拡充
- 生成スキーマの一括レビューUI
