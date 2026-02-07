# アーキテクチャ設計

## 全体構成

```
┌──────────────────────────────────────────────────────────┐
│                     Web ブラウザ                          │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────────┐       │
│  │ 設定 │ │ 移行 │ │スキーマ│ │プレビュー│ │SSE 進捗受信│  │
│  └──┬───┘ └──┬───┘ └──┬───┘ └──┬───┘ └────┬─────┘       │
└─────┼────────┼────────┼────────┼──────────┼──────────────┘
      │ REST   │ REST   │ REST   │ REST     │ EventStream
┌─────┼────────┼────────┼────────┼──────────┼──────────────┐
│     ▼        ▼        ▼        ▼          ▼              │
│  ┌───────────────────────────────────────────────────┐   │
│  │              HTTP Server (server/)                 │   │
│  │  Middleware: CORS → Logging → Recovery             │   │
│  └──┬────────┬──────────┬────────────┬───────────────┘   │
│     │        │          │            │                    │
│  ┌──▼──┐ ┌──▼──────┐ ┌─▼──────┐ ┌──▼──────────┐        │
│  │Config│ │ Engine  │ │ Schema │ │   Preview    │        │
│  │Store │ │(engine/)│ │Handler │ │  Handler     │        │
│  └─────┘ └──┬──────┘ └─┬──────┘ └──┬──────────┘        │
│             │           │           │                    │
│     ┌───────┼───────────┼───────────┘                    │
│     │       │           │                                │
│  ┌──▼───┐ ┌─▼────────┐ ┌▼─────────┐                     │
│  │Source│ │Transform  │ │  Sink    │                     │
│  │      │ │(transform)│ │ (sink/) │                     │
│  └──┬───┘ └──────────┘ └──┬──────┘                     │
│     │                      │                             │
└─────┼──────────────────────┼─────────────────────────────┘
      │                      │
┌─────▼────────┐   ┌────────▼──────────┐
│   Firebase   │   │    PostgreSQL     │
│ Firestore /  │   │  (pgx pool)      │
│    RTDB      │   │                   │
└──────────────┘   └───────────────────┘
```

## パッケージ責務

| パッケージ | 責務 |
|---|---|
| `main` | CLI フラグ解析、サーバー起動、グレースフルシャットダウン |
| `config` | 設定構造体定義、バリデーション、ファイル永続化 |
| `engine` | マイグレーションオーケストレーション、進捗追跡、チェックポイント |
| `server` | HTTP ルーティング、ミドルウェア、SSE 配信、ハンドラー群 |
| `source` | Firebase 読み取りインターフェースと Firestore/RTDB 実装 |
| `transform` | ドキュメント→行変換、マップフラット化、型推論・変換 |
| `sink` | PostgreSQL 書き込み、DDL 生成、コネクションプール管理 |
| `logger` | zerolog ベースの構造化ログ |

## 並行処理モデル

本ツールは複数レイヤーで goroutine を活用し、スループットを最大化しています。

### 1. マッピング並行実行 (engine.go)

```
Engine.runMigration()
  │
  ├─ errgroup.SetLimit(parallelism)
  │
  ├─ goroutine ── runMapping("users")    ─── バッチ読み取り → 変換 → 書き込み
  ├─ goroutine ── runMapping("orders")   ─── バッチ読み取り → 変換 → 書き込み
  └─ goroutine ── runMapping("products") ─── バッチ読み取り → 変換 → 書き込み
```

- `errgroup` の `SetLimit(N)` で同時実行数を制御（設定値: `options.parallelism`）
- 各マッピングは独立したゴルーチンで実行
- エラー戦略（continue / stop）に応じた制御

### 2. サブバッチ並行 INSERT (sink.go)

```
WriteBatch(1000 rows)
  │
  ├─ goroutine ── CopyFrom(rows[0:200])
  ├─ goroutine ── CopyFrom(rows[200:400])
  ├─ goroutine ── CopyFrom(rows[400:600])
  ├─ goroutine ── CopyFrom(rows[600:800])
  └─ goroutine ── CopyFrom(rows[800:1000])
```

- 200行単位でサブバッチに分割
- 各サブバッチを goroutine で並行実行
- `pgxpool`（MaxConns=10）により複数コネクションを活用
- `pgx.CopyFrom` による PostgreSQL COPY プロトコルで高速バルク INSERT
- 失敗時は `INSERT ... ON CONFLICT DO NOTHING` にフォールバック

### 3. レコードパース並行処理 (source.go - RTDB)

```
ReadBatch(RTDB)
  │
  ├─ goroutine ── parse(key1, value1) → Record
  ├─ goroutine ── parse(key2, value2) → Record
  └─ goroutine ── parse(keyN, valueN) → Record
```

- Realtime Database のバッチ読み取り時、各キーのレコードパースを並行化
- インデックス指定の直接配列書き込みで排他制御のオーバーヘッドを最小化

### 4. SSE 進捗配信 (server.go)

```
ProgressTracker ──[chan]──> broadcastProgress() ──> SSE Client 1
                                                ──> SSE Client 2
                                                ──> SSE Client N
```

- `broadcastProgress()` がバックグラウンド goroutine として常駐
- チャネル経由でノンブロッキングにイベントを配信
- クライアントごとにバッファ付きチャネル（容量50）で遅延クライアントを保護

## データフロー

### マイグレーション実行フロー

```
1. Engine.Run()
   ├─ Source.Open()         Firebase への接続確立
   ├─ Sink.Open()           PostgreSQL コネクションプール確立
   │
   ├─ for each mapping (並行):
   │   ├─ Source.EstimateCount()     件数推定（進捗バー用）
   │   ├─ Sink.EnsureTable()         テーブル自動作成（オプション）
   │   ├─ transform.NewPipeline()    変換パイプライン構築
   │   │
   │   └─ loop:
   │       ├─ Source.ReadBatch()      カーソルベースでバッチ読み取り
   │       ├─ Pipeline.Transform()   各レコードを行に変換
   │       ├─ Sink.WriteBatch()      並行サブバッチ INSERT
   │       └─ ProgressTracker        進捗更新 → SSE 配信
   │
   ├─ Source.Close()
   └─ Sink.Close()
```

### データ変換フロー

```
Firebase Document                    PostgreSQL Row
┌─────────────────────┐              ┌─────────────────┐
│ {                   │              │ id    | TEXT     │
│   "_id": "abc123",  │  FlattenMap  │ name  | TEXT     │
│   "profile": {      │ ──────────> │ email | TEXT     │
│     "name": "Alice",│  Transform  │ age   | BIGINT   │
│     "email": "...", │              │ data  | JSONB    │
│   },                │              └─────────────────┘
│   "age": 30         │
│ }                   │
└─────────────────────┘

フラット化結果:
  _id           → "abc123"
  profile_name  → "Alice"
  profile_email → "..."
  profile       → {"name":"Alice","email":"..."} (JSONB 用)
  age           → 30
```

## エラーハンドリング戦略

| 戦略 | 動作 |
|---|---|
| `continue` | エラーをログに記録してスキップし、次のレコード/バッチに進む |
| `stop` | 最初のエラーでマッピングの処理を停止する |

`error_limit` を設定すると、累積エラー数が上限に達した時点で処理を停止します。

## ミドルウェアスタック

リクエストは以下の順でミドルウェアを通過します:

```
Client Request
  → Recovery (パニック回復)
    → Logging (リクエストログ)
      → CORS (クロスオリジン許可)
        → Router (ルーティング)
```
