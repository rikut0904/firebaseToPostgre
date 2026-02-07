# AI マッピング生成プロンプト

## 目的
Firebase のコレクション/サブコレクションのサンプル情報から、PostgreSQL 向けのマッピング設定を生成する。

## 指示（日本語）
あなたはデータ移行アシスタントです。以下の要件を満たす PostgreSQL マッピング設定を作成してください。

### 出力ルール
- 形式は **JSONのみ**（余計なテキストは出力しない）
- JSONは与えられた **JSON Schema** に厳密に従う

### 型推論のルール
- 文字列は原則 `TEXT`
- 数値が整数として扱える場合は `BIGINT`
- 小数が混在する場合は `DOUBLE PRECISION`
- 配列/オブジェクトは `JSONB`

### サブコレクションのルール
- コレクショングループ形式の `parent/*/sub` を扱う
- **外部キー列**は `{parent}_id` を必ず追加し、`TEXT`, `nullable:false`

### 予約語の回避
- 予約語や衝突しそうなカラム名は **意味を保ったまま変更**する
  - 例: `group` → `group_name`
  - 例: `type` → `type_name` または `*_type`
- **元のフィールド名（source）は変更しない**。変更は `target` 側だけに適用する。

### ID の扱い
- FirestoreのドキュメントIDは `source: "_id"` として扱う
- `target: "id"` を主キーとして使用する

### テーブル名
- 原則 `public.<collection>` を用いる
- サブコレクションは `public.<parent>_<sub>` を用いる

