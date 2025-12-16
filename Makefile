# CC-Bridge Makefile

GREEN=\033[0;32m
YELLOW=\033[0;33m
NC=\033[0m

.PHONY: help dev run build clean frontend-dev frontend-build embed-frontend sync-version

help:
	@echo "$(GREEN)CC-Bridge - 可用命令:$(NC)"
	@echo ""
	@echo "$(YELLOW)开发:$(NC)"
	@echo "  make dev            - Go 后端热重载开发(不含前端)"
	@echo "  make run            - 构建前端并运行 Go 后端"
	@echo "  make frontend-dev   - 前端开发服务器"
	@echo ""
	@echo "$(YELLOW)构建:$(NC)"
	@echo "  make build          - 构建前端并编译 Go 后端"
	@echo "  make frontend-build - 仅构建前端"
	@echo "  make clean          - 清理构建文件"
	@echo ""
	@echo "$(YELLOW)版本:$(NC)"
	@echo "  make sync-version   - 同步 VERSION 到 frontend/package.json"

dev:
	@echo "$(GREEN)🚀 启动前后端开发模式...$(NC)"
	@cd frontend && bun run dev &
	@cd backend-go && $(MAKE) dev

run: embed-frontend
	@cd backend-go && $(MAKE) run

build: embed-frontend
	@cd backend-go && $(MAKE) build

# Sync VERSION to frontend/package.json
sync-version:
	@echo "$(GREEN)🔄 同步版本号...$(NC)"
	@VERSION=$$(cat VERSION | tr -d 'v' | tr -d '\n'); \
	if [ -f frontend/package.json ]; then \
		sed -i.bak 's/"version": "[^"]*"/"version": "'$$VERSION'"/' frontend/package.json && \
		rm -f frontend/package.json.bak && \
		echo "$(GREEN)✅ frontend/package.json 版本已更新为 $$VERSION$(NC)"; \
	fi

embed-frontend: sync-version
	@echo "$(GREEN)📦 构建前端...$(NC)"
	@cd frontend && bun run build
	@echo "$(GREEN)📋 嵌入前端到 Go 后端...$(NC)"
	@rm -rf backend-go/frontend/dist
	@mkdir -p backend-go/frontend/dist
	@cp -r frontend/dist/* backend-go/frontend/dist/

clean:
	@cd backend-go && $(MAKE) clean
	@rm -rf frontend/dist

frontend-dev:
	@cd frontend && bun run dev

frontend-build: sync-version
	@cd frontend && bun run build
