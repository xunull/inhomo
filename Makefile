.PHONY: all deps frontend build install test dist brew-formula clean

# 本地发布预演的注入版本（CI 用 tag；本地默认 dev）：make dist VERSION=v0.1.0
VERSION ?= dev

# make —— 构建前端(→ web/dist) + go build(内嵌)，产出单二进制 ./inhomo
all: frontend build

deps: ## 装前端依赖（首次或 package.json 变更后跑一次）
	npm --prefix web install

frontend: ## 构建前端 → web/dist（go:embed 需要它已存在）
	npm --prefix web run build

build: ## go build（CGO，内嵌 web/dist）
	CGO_ENABLED=1 go build -o inhomo .

install: frontend ## go install（CGO，先重建前端再装到 $GOBIN/$GOPATH/bin，覆盖 PATH 上的 inhomo）
	CGO_ENABLED=1 go install .

dist: frontend ## 本地发布预演：注入版本 → 打当前平台 tar.gz + 校验和到 dist/（对应 CI 各 runner 的动作）
	CGO_ENABLED=1 go build -ldflags "-s -w -X main.version=$(VERSION)" -o inhomo .
	@mkdir -p dist
	@OS=$$(go env GOOS); ARCH=$$(go env GOARCH); \
	  A="inhomo_$(VERSION)_$${OS}_$${ARCH}.tar.gz"; \
	  tar -czf "dist/$$A" inhomo LICENSE README.md; \
	  ( cd dist && shasum -a 256 -- *.tar.gz > checksums.txt ); \
	  echo "→ dist/$$A（版本：$$(./inhomo version)）"

brew-formula: ## 由 dist/checksums.txt 生成 Homebrew formula → dist/inhomo.rb（需 4 目标齐全的 checksums：CI 或下载的 Release checksums；本机 `make dist` 只含单平台会 fail-closed 报错）
	go run ./tools/brewgen -version $(VERSION) -checksums dist/checksums.txt -out dist/inhomo.rb

test: ## go test（CGO）+ web 单测（vitest）
	CGO_ENABLED=1 go test ./...
	npm --prefix web run test

clean:
	rm -f inhomo
