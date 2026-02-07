.PHONY: build run dev clean deps fmt vet test lint help

BINARY_NAME = migrate
BUILD_DIR   = .

## build: バイナリ migrate をビルド
build:
	go build -buildvcs=false -o $(BUILD_DIR)/$(BINARY_NAME) .

## run: ビルドして起動
up: build
	./$(BUILD_DIR)/$(BINARY_NAME)

## dev: go run で直接起動（ビルドなし）
dev:
	go run -buildvcs=false .

## clean: 生成物を削除
clean:
	rm -f $(BUILD_DIR)/$(BINARY_NAME)

## deps: 依存パッケージ取得 + tidy
deps:
	go mod download
	go mod tidy

## fmt: コードフォーマット
fmt:
	go fmt ./...

## vet: 静的解析
vet:
	go vet ./...

## test: テスト実行
test:
	go test -buildvcs=false ./...

## lint: vet + fmt
lint: vet fmt

## help: ヘルプ表示
help:
	@echo ""
	@echo "使用可能なコマンド:"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  make /' | sed 's/: /\t/'
	@echo ""
