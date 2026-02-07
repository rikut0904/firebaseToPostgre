# API リファレンス

ベース URL: `http://localhost:8081/api`

全エンドポイントは JSON でリクエスト/レスポンスを行います。エラー時は `{"error": "メッセージ"}` を返します。

---

## 設定管理

### GET /api/config

現在のアクティブな設定を取得します。

**レスポンス**

```json
{
  "firebase": {
    "source_type": "firestore",
    "project_id": "my-project",
    "credentials_json": "..."
  },
  "postgres": {
    "host": "localhost",
    "port": 5432,
    "database": "mydb",
    "user": "postgres",
    "password": "...",
    "sslmode": "disable"
  },
  "mappings": [...],
  "options": {
    "batch_size": 500,
    "parallelism": 3,
    "error_strategy": "continue",
    "error_limit": 100
  }
}
```

### POST /api/config

設定を保存します。`name` を指定するとファイルにも永続化されます。

**リクエスト**

```json
{
  "name": "production-config",
  "config": { ... }
}
```

**レスポンス**

```json
{ "status": "saved" }
```

### POST /api/config/validate

設定のバリデーションを実行します。

**リクエスト**: `Config` オブジェクト全体

**レスポンス**

```json
// 有効な場合
{ "valid": true }

// 無効な場合
{ "valid": false, "errors": "firebase.project_id is required; ..." }
```

### POST /api/config/test-firebase

Firebase への接続テストを行います。

**リクエスト**

```json
{
  "source_type": "firestore",
  "project_id": "my-project",
  "credentials_json": "{...サービスアカウントJSON...}"
}
```

**レスポンス**

```json
// 成功
{ "success": true, "message": "Firebase connection successful" }

// 失敗
{ "success": false, "error": "エラー詳細" }
```

### POST /api/config/test-postgres

PostgreSQL への接続テストを行います。

**リクエスト**

```json
{
  "host": "localhost",
  "port": 5432,
  "database": "mydb",
  "user": "postgres",
  "password": "secret",
  "sslmode": "disable"
}
```

**レスポンス**

```json
{ "success": true, "message": "PostgreSQL connection successful" }
```

### GET /api/config/list

保存済み設定の一覧を取得します。

**レスポンス**

```json
{ "configs": ["production-config", "staging-config"] }
```

### GET /api/config/load/{name}

指定した名前の設定を読み込み、アクティブ設定として適用します。

**レスポンス**: `Config` オブジェクト全体

---

## マイグレーション制御

### POST /api/migrate/start

マイグレーションをバックグラウンドで開始します。

**リクエスト**

```json
{
  "mapping_name": "",
  "dry_run": false
}
```

| フィールド | 型 | 説明 |
|---|---|---|
| `mapping_name` | string | 特定のマッピングのみ実行する場合に指定（省略時は全マッピング） |
| `dry_run` | bool | `true` にすると PostgreSQL への書き込みをスキップ |

**レスポンス**

```json
{ "status": "started", "message": "migration started (mode: live)" }
```

**エラー**

| ステータス | 条件 |
|---|---|
| 400 | 設定が無効 |
| 409 | 別のマイグレーションが実行中 |

### POST /api/migrate/stop

実行中のマイグレーションを停止します。

**レスポンス**

```json
{ "status": "stopping" }
```

### GET /api/migrate/status

マイグレーションの現在のステータスを取得します。

**レスポンス**

```json
{
  "running": true,
  "mappings": [
    {
      "mapping_name": "users",
      "phase": "writing",
      "read": 5000,
      "written": 4800,
      "skipped": 10,
      "errors": 2,
      "total": 10000,
      "percent": 48.0,
      "speed": 1200.5,
      "elapsed": "00:00:04"
    }
  ]
}
```

### GET /api/migrate/progress

Server-Sent Events (SSE) でリアルタイム進捗を受信します。

**接続**

```javascript
const es = new EventSource('/api/migrate/progress');

es.addEventListener('connected', () => {
  console.log('接続完了');
});

es.addEventListener('progress', (e) => {
  const data = JSON.parse(e.data);
  console.log(data);
});
```

**イベント形式**

| イベント名 | 説明 |
|---|---|
| `connected` | 接続確立時に送信 |
| `progress` | 進捗更新ごとに送信 |

**`progress` データ**

```json
{
  "mapping_name": "users",
  "phase": "writing",
  "read": 5000,
  "written": 4800,
  "skipped": 10,
  "errors": 2,
  "total": 10000,
  "percent": 48.0,
  "speed": 1200.5,
  "elapsed": "00:00:04",
  "message": ""
}
```

| `phase` 値 | 意味 |
|---|---|
| `reading` | Firebase からデータ読み取り中 |
| `writing` | PostgreSQL へデータ書き込み中 |
| `done` | マッピング完了 |
| `error` | エラー発生 |

---

## スキーマ管理

### POST /api/schema/generate

マッピング定義から DDL (CREATE TABLE) を生成します。

**リクエスト**

```json
{
  "mapping_name": ""
}
```

`mapping_name` 省略時は全マッピングの DDL を生成します。

**レスポンス**

```json
{
  "schemas": [
    {
      "mapping_name": "users",
      "table": "users",
      "ddl": "CREATE TABLE IF NOT EXISTS \"users\" (\n  \"id\" TEXT NOT NULL,\n  \"name\" TEXT,\n  PRIMARY KEY (\"id\")\n);"
    }
  ]
}
```

### POST /api/schema/execute

任意の DDL を PostgreSQL 上で実行します。

**リクエスト**

```json
{
  "ddl": "CREATE TABLE IF NOT EXISTS ..."
}
```

**レスポンス**

```json
{ "status": "success", "message": "DDL executed successfully" }
```

### POST /api/schema/infer

Firebase コレクションのサンプルデータからスキーマを推論します。

**リクエスト**

```json
{
  "collection": "users",
  "sample_size": 10
}
```

**レスポンス**

```json
{
  "collection": "users",
  "fields": {
    "_id": "TEXT",
    "name": "TEXT",
    "age": "BIGINT",
    "tags": "JSONB",
    "created_at": "TIMESTAMPTZ"
  },
  "columns": [
    {
      "source": "_id",
      "target": "_id",
      "type": "TEXT",
      "primary_key": true,
      "nullable": false
    },
    {
      "source": "name",
      "target": "name",
      "type": "TEXT",
      "primary_key": false,
      "nullable": true
    }
  ]
}
```

---

## データプレビュー

### POST /api/preview/source

Firebase コレクションからサンプルレコードを取得します。

**リクエスト**

```json
{
  "collection": "users",
  "limit": 5
}
```

| フィールド | 型 | デフォルト | 制約 |
|---|---|---|---|
| `collection` | string | 必須 | - |
| `limit` | int | 5 | 1〜100 |

**レスポンス**

```json
{
  "collection": "users",
  "count": 5,
  "records": [
    { "_id": "abc123", "name": "Alice", "age": 30 },
    { "_id": "def456", "name": "Bob", "age": 25 }
  ]
}
```

### POST /api/preview/transform

レコードに対してカラムマッピングの変換結果をプレビューします。

**リクエスト**

```json
{
  "collection": "users",
  "record": { "_id": "abc", "name": "Alice", "age": 30 },
  "columns": [
    { "source": "_id", "target": "id", "type": "TEXT", "primary_key": true, "nullable": false },
    { "source": "name", "target": "user_name", "type": "TEXT", "primary_key": false, "nullable": true }
  ]
}
```

- `record` 省略時は `collection` からサンプルを1件取得して使用
- `columns` 省略時は現在のアクティブ設定のマッピングを使用

**レスポンス**

```json
{
  "source": { "_id": "abc", "name": "Alice", "age": 30 },
  "transformed": { "id": "abc", "user_name": "Alice" },
  "columns": ["id", "user_name"]
}
```
