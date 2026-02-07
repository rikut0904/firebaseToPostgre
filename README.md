# Firebase to PostgreSQL Migration Tool

Firebase（Firestore / Realtime Database）のデータを PostgreSQL へマイグレーションする Web ベースのツールです。

## 特徴

- **Firestore / Realtime Database 両対応** - ソースタイプを選択するだけで切り替え可能
- **Web UI によるノーコード操作** - ブラウザから設定・実行・監視をすべて完結
- **リアルタイム進捗表示** - Server-Sent Events (SSE) でマイグレーション進捗をライブ配信
- **高速並行処理** - goroutine + errgroup によるマッピング並行実行、サブバッチ並行 INSERT
- **スキーマ推論** - Firebase のサンプルデータから PostgreSQL のカラム型を自動推定
- **DDL 自動生成** - マッピング定義から CREATE TABLE 文を生成・実行
- **Dry-run モード** - 実データを書き込まずにマイグレーションをシミュレーション
- **チェックポイント/レジューム** - 中断したマイグレーションの途中再開が可能
- **設定の保存/読み込み** - JSON ファイルとして設定を永続化

## クイックスタート

### 前提条件

- Go 1.25 以上
- PostgreSQL 14 以上
- Firebase サービスアカウントの JSON キーファイル

### ビルドと起動

```bash
# 依存関係の取得
make tidy

# ビルド
make build

# 起動（デフォルト: http://localhost:8081）
make run
```

### コマンドラインオプション

```
Usage: firebaseToPostgre [options]

  -addr string
        HTTP リッスンアドレス (default ":8081")
  -config-dir string
        設定ファイル保存ディレクトリ (default "configs")
  -web-dir string
        Web 静的ファイルディレクトリ (default "web")
  -log-level string
        ログレベル: debug, info, warn, error (default "info")
```

### 基本的な使い方

1. ブラウザで `http://localhost:8081` を開く
2. **設定タブ** で Firebase / PostgreSQL の接続情報を入力し、接続テスト
3. **マッピング** でコレクション→テーブルの対応とカラム変換を定義
4. **スキーマタブ** で DDL を生成・実行してテーブルを作成
5. **データプレビュータブ** でサンプルデータと変換結果を確認
6. **移行実行タブ** でマイグレーションを開始

## プロジェクト構成

```
firebaseToPostgre/
├── main.go                         # エントリポイント
├── Makefile                        # ビルド・テスト・lint ターゲット
├── go.mod / go.sum                 # Go モジュール定義
├── configs/                        # 保存済み設定ファイル
├── internal/
│   ├── config/                     # 設定管理・バリデーション
│   │   ├── config.go               #   Config / MappingConfig 定義
│   │   ├── store.go                #   ファイル永続化
│   │   └── config_test.go          #   ユニットテスト
│   ├── engine/                     # マイグレーションエンジン
│   │   ├── engine.go               #   オーケストレーション・並行実行
│   │   ├── progress.go             #   リアルタイム進捗トラッキング
│   │   └── resume.go               #   チェックポイント/レジューム
│   ├── logger/                     # 構造化ログ (zerolog)
│   │   └── logger.go
│   ├── server/                     # HTTP API サーバー
│   │   ├── server.go               #   ルーティング・SSE 配信
│   │   ├── middleware.go           #   CORS・ログ・リカバリ
│   │   ├── handler_config.go       #   設定管理エンドポイント
│   │   ├── handler_migrate.go      #   マイグレーション制御
│   │   ├── handler_schema.go       #   DDL 生成・実行
│   │   └── handler_preview.go      #   データプレビュー
│   ├── source/                     # Firebase データソース
│   │   └── source.go               #   Firestore / RTDB コネクタ
│   ├── sink/                       # PostgreSQL データシンク
│   │   ├── sink.go                 #   接続プール・並行バッチ INSERT
│   │   └── sink_test.go            #   ユニットテスト
│   └── transform/                  # データ変換パイプライン
│       ├── transformer.go          #   マップフラット化・型変換
│       └── transformer_test.go     #   ユニットテスト
├── web/                            # フロントエンド (HTML/CSS/JS)
│   ├── index.html
│   ├── css/style.css
│   └── js/
│       ├── app.js
│       ├── config.js
│       ├── migration.js
│       ├── schema.js
│       └── preview.js
└── doc/                            # 詳細ドキュメント
    ├── architecture.md             #   アーキテクチャ設計
    ├── api.md                      #   API リファレンス
    └── configuration.md            #   設定リファレンス
```

## ドキュメント

| ドキュメント | 内容 |
|---|---|
| [doc/architecture.md](doc/architecture.md) | アーキテクチャ設計・並行処理モデル・データフロー |
| [doc/api.md](doc/api.md) | REST API エンドポイント一覧 |
| [doc/configuration.md](doc/configuration.md) | 設定ファイルのフォーマットと全パラメータ |

## 開発

```bash
# テスト実行
make test

# Lint
make lint

# クリーン
make clean
```

## 技術スタック

| レイヤー | 技術 |
|---|---|
| 言語 | Go 1.25 |
| Firebase SDK | firebase.google.com/go/v4, cloud.google.com/go/firestore |
| PostgreSQL ドライバ | github.com/jackc/pgx/v5 (コネクションプール対応) |
| ログ | github.com/rs/zerolog |
| 並行処理 | golang.org/x/sync/errgroup, sync.WaitGroup |
| フロントエンド | Vanilla HTML/CSS/JavaScript |

## ライセンス

Private
