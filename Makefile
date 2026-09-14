BINARY_NAME=fastgithub
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")
GO_VERSION=$(shell go version | awk '{print $$3}' 2>/dev/null || echo "unknown")

LDFLAGS=-ldflags "-s -w \
	-X github.com/creazyboyone/fastgithub/internal/version.Version=$(VERSION) \
	-X github.com/creazyboyone/fastgithub/internal/version.GitCommit=$(GIT_COMMIT) \
	-X github.com/creazyboyone/fastgithub/internal/version.BuildTime=$(BUILD_TIME) \
	-X github.com/creazyboyone/fastgithub/internal/version.GoVersion=$(GO_VERSION)"

# Windows GUI 模式（不显示控制台窗口）
WIN_LDFLAGS=-ldflags "-s -w -H=windowsgui \
	-X github.com/creazyboyone/fastgithub/internal/version.Version=$(VERSION) \
	-X github.com/creazyboyone/fastgithub/internal/version.GitCommit=$(GIT_COMMIT) \
	-X github.com/creazyboyone/fastgithub/internal/version.BuildTime=$(BUILD_TIME) \
	-X github.com/creazyboyone/fastgithub/internal/version.GoVersion=$(GO_VERSION)"

.PHONY: all build build-systray build-console build-all test clean run

all: build

# 默认构建：Windows GUI 模式（无控制台窗口）+ 系统托盘
build:
	CGO_ENABLED=1 go build $(WIN_LDFLAGS) -tags systray -o $(BINARY_NAME).exe ./cmd/fastgithub

# 带系统托盘 + GUI 模式（推荐发布版本）
build-systray:
	CGO_ENABLED=1 go build $(WIN_LDFLAGS) -tags systray -o $(BINARY_NAME).exe ./cmd/fastgithub

# 控制台模式（用于调试）
build-console:
	CGO_ENABLED=1 go build $(LDFLAGS) -tags systray -o $(BINARY_NAME).exe ./cmd/fastgithub

# 纯 Go 构建（无托盘，无 CGO 依赖，控制台模式）
build-pure:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_NAME).exe ./cmd/fastgithub

# 全平台构建
build-all:
	mkdir -p dist
	# Windows: GUI + systray (需本地 CGO, 此处使用纯 Go GUI 版本)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(WIN_LDFLAGS) -o dist/$(BINARY_NAME)_windows_amd64.exe ./cmd/fastgithub
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build $(WIN_LDFLAGS) -o dist/$(BINARY_NAME)_windows_arm64.exe ./cmd/fastgithub
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o dist/$(BINARY_NAME)_linux_amd64 ./cmd/fastgithub
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o dist/$(BINARY_NAME)_linux_arm64 ./cmd/fastgithub
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o dist/$(BINARY_NAME)_darwin_amd64 ./cmd/fastgithub
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o dist/$(BINARY_NAME)_darwin_arm64 ./cmd/fastgithub

# 单元测试
test:
	go test -short ./...

# 完整测试（含网络集成测试）
test-full:
	go test ./...

clean:
	rm -rf dist $(BINARY_NAME) $(BINARY_NAME).exe fastgithub_notray.exe

run: build-console
	./$(BINARY_NAME).exe -console
