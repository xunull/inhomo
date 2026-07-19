.PHONY: all deps frontend build install test clean

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

test: ## go test（CGO）
	CGO_ENABLED=1 go test ./...

clean:
	rm -f inhomo
