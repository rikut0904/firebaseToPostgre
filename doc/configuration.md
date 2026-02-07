# 設定リファレンス

設定は JSON 形式で管理され、Web UI から編集・保存・読み込みが可能です。
保存した設定は `configs/` ディレクトリに `{名前}.json` として永続化されます。

## 設定ファイルの全体構造

```json
{
  "firebase": { ... },
  "postgres": { ... },
  "mappings": [ ... ],
  "options": { ... }
}
```

---

## firebase - Firebase 接続設定

| フィールド | 型 | 必須 | 説明 |
|---|---|---|---|
| `source_type` | string | Yes | `"firestore"` または `"rtdb"` |
| `project_id` | string | Yes | Firebase プロジェクト ID |
| `credentials_json` | string | Yes | サービスアカウントキー JSON の文字列 |

### source_type の違い

| | Firestore | Realtime Database |
|---|---|---|
| データモデル | ドキュメント/コレクション | JSON ツリー |
| ページネーション | DocumentID によるカーソル | キー順による OrderByKey |
| 件数取得 | Aggregation Query (正確) | Shallow Query (キー数カウント) |
| ネスト | サブコレクション対応 | パス指定（`users/profiles`） |

```json
{
  "firebase": {
    "source_type": "firestore",
    "project_id": "my-firebase-project",
    "credentials_json": "{\"type\":\"service_account\",\"project_id\":\"...\",...}"
  }
}
```

### サービスアカウントキーの取得方法

1. [Firebase Console](https://console.firebase.google.com/) → プロジェクト設定
2. サービスアカウント → 新しい秘密鍵を生成
3. ダウンロードした JSON の内容を `credentials_json` に設定

---

## postgres - PostgreSQL 接続設定

| フィールド | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `host` | string | Yes | `"localhost"` | ホスト名または IP |
| `port` | int | Yes | `5432` | ポート番号 (1〜65535) |
| `database` | string | Yes | - | データベース名 |
| `user` | string | Yes | - | ユーザー名 |
| `password` | string | No | `""` | パスワード |
| `sslmode` | string | Yes | `"disable"` | SSL モード |

### sslmode の選択肢

| 値 | 説明 |
|---|---|
| `disable` | SSL を使用しない |
| `allow` | サーバーが要求する場合のみ SSL |
| `prefer` | 可能であれば SSL を使用 |
| `require` | SSL を強制（証明書検証なし） |
| `verify-ca` | SSL + CA 証明書検証 |
| `verify-full` | SSL + CA 証明書 + ホスト名検証 |

```json
{
  "postgres": {
    "host": "localhost",
    "port": 5432,
    "database": "migration_target",
    "user": "postgres",
    "password": "secret",
    "sslmode": "disable"
  }
}
```

接続文字列は内部的に以下の形式で構築されます:
```
postgres://user:password@host:port/database?sslmode=MODE
```

---

## mappings - コレクション→テーブルのマッピング定義

1つ以上のマッピングを配列で定義します。各マッピングが1つの「Firebase コレクション → PostgreSQL テーブル」の対応です。

| フィールド | 型 | 必須 | 説明 |
|---|---|---|---|
| `name` | string | Yes | マッピングの識別名（一意） |
| `source_collection` | string | Yes | Firebase のコレクション名またはパス |
| `target_table` | string | Yes | PostgreSQL のテーブル名 |
| `create_table` | bool | No | `true` でテーブルを自動作成 |
| `columns` | array | Yes | カラムマッピング（1つ以上） |

### columns - カラムマッピング

| フィールド | 型 | 必須 | 説明 |
|---|---|---|---|
| `source` | string | Yes | Firebase のフィールドパス |
| `target` | string | Yes | PostgreSQL のカラム名 |
| `type` | string | Yes | PostgreSQL のデータ型 |
| `primary_key` | bool | No | 主キーに含めるか |
| `nullable` | bool | No | NULL 許容か |

### source フィールドの特殊値

| 値 | 意味 |
|---|---|
| `_id` | ドキュメント ID（自動付与） |
| `_key` | `_id` のエイリアス |
| `field_name` | トップレベルフィールド |
| `parent_child` | ネストフィールド（フラット化後の `_` 区切り） |

### ネストフィールドの参照

Firebase ドキュメント:
```json
{
  "profile": {
    "name": "Alice",
    "address": {
      "city": "Tokyo"
    }
  }
}
```

フラット化後のキー:
- `profile_name` → `"Alice"`
- `profile_address_city` → `"Tokyo"`
- `profile_address` → `{"city":"Tokyo"}` （JSONB カラム向け）
- `profile` → `{"name":"Alice","address":{"city":"Tokyo"}}` （JSONB カラム向け）

### 対応する PostgreSQL データ型

| 型 | エイリアス | 自動推論条件 |
|---|---|---|
| `TEXT` | `VARCHAR`, `CHAR(N)`, `VARCHAR(N)` | 文字列値 |
| `BIGINT` | `INT`, `INTEGER`, `INT4`, `INT8`, `SMALLINT` | 整数の float64 |
| `DOUBLE PRECISION` | `FLOAT8`, `NUMERIC`, `DECIMAL` | 小数の float64 |
| `REAL` | `FLOAT4` | float32 |
| `BOOLEAN` | `BOOL` | bool 値 |
| `TIMESTAMPTZ` | `TIMESTAMP` | RFC3339 形式の文字列 |
| `JSONB` | `JSON` | 配列またはオブジェクト |
| `UUID` | - | - |
| `BYTEA` | - | - |

### タイムスタンプの自動変換

以下の形式を自動認識します:

| 入力形式 | 例 |
|---|---|
| RFC3339 | `"2024-01-15T09:30:00Z"` |
| RFC3339Nano | `"2024-01-15T09:30:00.123456789Z"` |
| datetime | `"2024-01-15 09:30:00"` |
| date | `"2024-01-15"` |
| Unix timestamp (秒) | `1705312200` |
| Firestore Timestamp | `{"_seconds": 1705312200, "_nanoseconds": 0}` |

### マッピング例

```json
{
  "mappings": [
    {
      "name": "users",
      "source_collection": "users",
      "target_table": "users",
      "create_table": true,
      "columns": [
        { "source": "_id",          "target": "id",         "type": "TEXT",        "primary_key": true, "nullable": false },
        { "source": "profile_name", "target": "name",       "type": "TEXT",        "nullable": false },
        { "source": "email",        "target": "email",      "type": "TEXT",        "nullable": true },
        { "source": "age",          "target": "age",        "type": "BIGINT",      "nullable": true },
        { "source": "tags",         "target": "tags",       "type": "JSONB",       "nullable": true },
        { "source": "created_at",   "target": "created_at", "type": "TIMESTAMPTZ", "nullable": true }
      ]
    },
    {
      "name": "orders",
      "source_collection": "orders",
      "target_table": "orders",
      "create_table": true,
      "columns": [
        { "source": "_id",       "target": "id",        "type": "TEXT",           "primary_key": true },
        { "source": "user_id",   "target": "user_id",   "type": "TEXT",           "nullable": false },
        { "source": "total",     "target": "total",     "type": "DOUBLE PRECISION", "nullable": false },
        { "source": "items",     "target": "items",     "type": "JSONB",          "nullable": true }
      ]
    }
  ]
}
```

---

## options - マイグレーション実行オプション

| フィールド | 型 | 必須 | デフォルト | 範囲 | 説明 |
|---|---|---|---|---|---|
| `batch_size` | int | Yes | `500` | 1〜10000 | 1回の読み取り/書き込みバッチのレコード数 |
| `parallelism` | int | Yes | `3` | 1〜20 | 同時実行するマッピング数 |
| `error_strategy` | string | Yes | `"continue"` | `continue` / `stop` | エラー時の動作 |
| `error_limit` | int | No | `100` | 0〜 | 累積エラー上限（0 = 無制限） |

### batch_size チューニングガイド

| サイズ | 推奨ケース |
|---|---|
| 50〜100 | 1レコードが大きい場合（JSONB に大量データ） |
| 500 (デフォルト) | 一般的なユースケース |
| 1000〜5000 | 小さいレコードを大量に移行する場合 |
| 5000〜10000 | メモリに余裕があり最速を求める場合 |

### parallelism チューニングガイド

| 値 | 推奨ケース |
|---|---|
| 1 | マッピング間でデータ依存がある場合 |
| 3 (デフォルト) | 一般的なユースケース |
| 5〜10 | 多数の独立したコレクションを移行する場合 |
| 10〜20 | 高性能な PostgreSQL サーバーの場合 |

```json
{
  "options": {
    "batch_size": 500,
    "parallelism": 3,
    "error_strategy": "continue",
    "error_limit": 100
  }
}
```

---

## 設定ファイルの完全な例

```json
{
  "firebase": {
    "source_type": "firestore",
    "project_id": "my-project-12345",
    "credentials_json": "{\"type\":\"service_account\",\"project_id\":\"my-project-12345\",...}"
  },
  "postgres": {
    "host": "localhost",
    "port": 5432,
    "database": "app_production",
    "user": "migrator",
    "password": "secure-password",
    "sslmode": "require"
  },
  "mappings": [
    {
      "name": "users",
      "source_collection": "users",
      "target_table": "public.users",
      "create_table": true,
      "columns": [
        { "source": "_id",          "target": "id",         "type": "TEXT",        "primary_key": true, "nullable": false },
        { "source": "profile_name", "target": "name",       "type": "TEXT",        "nullable": false },
        { "source": "email",        "target": "email",      "type": "TEXT",        "nullable": true },
        { "source": "profile",      "target": "profile",    "type": "JSONB",       "nullable": true },
        { "source": "created_at",   "target": "created_at", "type": "TIMESTAMPTZ", "nullable": true }
      ]
    }
  ],
  "options": {
    "batch_size": 1000,
    "parallelism": 5,
    "error_strategy": "continue",
    "error_limit": 50
  }
}
```
