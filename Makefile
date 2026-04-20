# ===================================================================
# LLM Logger Makefile
# 兼容 Linux / macOS / Windows (git bash, WSL)
# ===================================================================

.PHONY: help up up-deps down logs status restart deploy deploy-prod stop clean-data \
        migrate init-minio bootstrap \
        gateway-build gateway-run gateway-test gateway-fmt gateway-lint gateway-image \
        query-api-install query-api-run query-api-test query-api-fmt query-api-image \
        admin-ui-install admin-ui-dev admin-ui-build admin-ui-image \
        test fmt lint clean

help: ## 显示所有可用命令
	@echo "LLM Logger - 可用命令:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ============ 一键部署 ============

deploy: ## [推荐] 一键构建并启动全栈 (CH + MinIO + Gateway + API + UI)
	@if [ ! -f .env ]; then \
		echo "未找到 .env, 自动从 .env.example 复制"; \
		cp .env.example .env; \
	fi
	docker compose up -d --build
	@echo ""
	@echo "等待 ClickHouse 健康 (~20s)..."
	@sleep 15
	@$(MAKE) bootstrap
	@echo ""
	@echo "=============================================="
	@echo "  部署完成"
	@echo "=============================================="
	@echo "  Gateway  (代理):  http://localhost:$${GATEWAY_PORT:-8080}"
	@echo "  Admin UI (面板):  http://localhost:$${ADMIN_UI_PORT:-8888}"
	@echo "  MinIO Console:    http://localhost:$${MINIO_CONSOLE_PORT:-9001}"
	@echo "=============================================="
	@echo "  1. 浏览器打开 Admin UI, 输入 .env 里的 LLMLOG_API_TOKEN"
	@echo "  2. 把 newapi 客户端的 base_url 改为 Gateway 地址"

deploy-prod: ## [生产] 使用 docker-compose.prod.yml override 部署
	@test -f .env || (echo "请先 cp .env.prod.example .env 并改密码"; exit 1)
	docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
	@sleep 15
	@$(MAKE) bootstrap
	@echo "生产环境部署完成. 建议在 Gateway / Admin UI 前加一层 TLS 反代 (Caddy/Nginx)"

bootstrap: migrate init-minio ## [自动调用] CH migrate + MinIO buckets 初始化

stop: ## 停止全部服务 (保留数据)
	docker compose stop

restart: ## 重启全部服务
	docker compose restart

status: ## 查看服务状态
	docker compose ps

# ============ Docker 环境 (细粒度) ============

up: deploy ## deploy 的别名

up-deps: ## 只启动基础设施 (CH + MinIO) - 本地开发 go run 模式
	docker compose up -d clickhouse minio
	@echo "等待服务就绪..."
	@sleep 5
	@echo "ClickHouse:    http://localhost:$${CH_HTTP_PORT:-8123}"
	@echo "MinIO Console: http://localhost:$${MINIO_CONSOLE_PORT:-9001}"

down: ## 停止 + 删除容器 (保留数据卷)
	docker compose down

logs: ## 查看所有服务日志 (follow)
	docker compose logs -f --tail=100

logs-gateway: ## 只看 gateway 日志
	docker compose logs -f --tail=200 gateway

clean-data: ## 清空所有数据卷 (危险!)
	docker compose down -v
	rm -rf clickhouse_data minio_data

# ============ ClickHouse Migrations ============

migrate: ## 应用 ClickHouse migration
	@echo "应用 ClickHouse migrations..."
	@for f in migrations/clickhouse/*.sql; do \
		echo "  执行 $$f"; \
		docker compose exec -T clickhouse clickhouse-client --multiquery < $$f; \
	done
	@echo "Migrations 完成"

ch-shell: ## 进入 ClickHouse 交互 shell
	docker compose exec clickhouse clickhouse-client --database llm_logger

# ============ MinIO 初始化 ============

init-minio: ## 创建 MinIO bucket (自动读取 .env 中的密钥)
	@echo "初始化 MinIO bucket..."
	@MINIO_USER=$${LLMLOG_S3_ACCESS_KEY:-minioadmin}; \
	MINIO_PASS=$${LLMLOG_S3_SECRET_KEY:-minioadmin}; \
	BUCKET=$${LLMLOG_S3_BUCKET:-llm-logs}; \
	docker compose exec -T minio sh -c " \
		mc alias set local http://localhost:9000 $$MINIO_USER $$MINIO_PASS && \
		mc mb -p local/$$BUCKET && \
		mc mb -p local/clickhouse-cold && \
		echo 'MinIO buckets 创建完成' \
	"

# ============ Go Gateway ============

gateway-build: ## 编译 Go gateway
	cd gateway && go build -o bin/gateway ./cmd/gateway

gateway-run: ## 运行 Go gateway (开发模式)
	cd gateway && go run ./cmd/gateway

gateway-test: ## Go gateway 单元测试
	cd gateway && go test -race -cover ./...

gateway-fmt: ## Go 格式化
	cd gateway && gofmt -w . && goimports -w .

gateway-lint: ## Go lint
	cd gateway && golangci-lint run ./...

gateway-image: ## 构建 Go gateway Docker 镜像
	docker build -t prismai/llm-logger-gateway:latest gateway/

# ============ Python Query API ============

query-api-install: ## 安装 Python 依赖
	cd query-api && uv sync

query-api-run: ## 运行 Query API (开发模式)
	cd query-api && uv run uvicorn app.main:app --reload --host 0.0.0.0 --port 8000

query-api-test: ## Python 单元测试
	cd query-api && uv run pytest

query-api-fmt: ## Python 格式化
	cd query-api && uv run ruff format . && uv run ruff check --fix .

query-api-image: ## 构建 Query API Docker 镜像
	docker build -t prismai/llm-logger-query-api:latest query-api/

# ============ Admin UI ============

admin-ui-install: ## 安装前端依赖
	cd admin-ui && npm install --no-audit --no-fund

admin-ui-dev: ## 启动前端 dev server (vite)
	cd admin-ui && npm run dev

admin-ui-build: ## 构建前端静态资源
	cd admin-ui && npm run build

admin-ui-image: ## 构建 Admin UI Docker 镜像
	docker build -t prismai/llm-logger-admin-ui:latest admin-ui/

# ============ 全栈 ============

test: gateway-test query-api-test ## 运行所有测试

fmt: gateway-fmt query-api-fmt ## 格式化所有代码

clean: ## 清理构建产物
	rm -rf gateway/bin gateway/dist
	rm -rf query-api/.venv query-api/dist
	rm -rf admin-ui/dist admin-ui/node_modules
	find . -name "__pycache__" -exec rm -rf {} + 2>/dev/null || true
