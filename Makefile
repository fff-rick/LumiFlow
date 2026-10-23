SHELL := /bin/bash
.DEFAULT_GOAL := help
.NOTPARALLEL:

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
BACKEND_DIR := $(ROOT_DIR)/live-platform
FRONTEND_DIR := $(ROOT_DIR)/frontend/next-app

COMPOSE_PROJECT ?= lumiflow
WEB_PORT ?= 8080
S ?=

PUBLIC_URL := http://127.0.0.1:$(WEB_PORT)
COMPOSE := docker compose \
	--project-directory $(BACKEND_DIR) \
	--project-name $(COMPOSE_PROJECT) \
	-f $(BACKEND_DIR)/docker-compose.yml \
	-f $(BACKEND_DIR)/docker-compose.integration.yml
COMPOSE_ENV := LUMIFLOW_WEB_PORT=$(WEB_PORT) FLUXFEED_PUBLIC_URL=$(PUBLIC_URL)
FRONTEND_STAMP := $(FRONTEND_DIR)/node_modules/.package-lock.json

.PHONY: help
help: ## 显示可用命令
	@echo "LumiFlow 单入口融合系统："
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: doctor
doctor: ## 检查 Docker、Compose 与 curl
	@for command in docker curl; do \
		command -v "$$command" >/dev/null 2>&1 || { echo "缺少命令：$$command"; exit 1; }; \
	done
	@docker compose version >/dev/null
	@docker info >/dev/null 2>&1 || { echo "Docker daemon 未运行"; exit 1; }
	@echo "环境检查通过"

$(FRONTEND_STAMP): $(FRONTEND_DIR)/package.json $(FRONTEND_DIR)/package-lock.json
	cd $(FRONTEND_DIR) && npm ci

.PHONY: frontend-deps
frontend-deps: $(FRONTEND_STAMP) ## 为本地检查安装统一前端依赖

.PHONY: compose-config
compose-config: ## 校验单入口融合 Compose 配置
	@$(COMPOSE_ENV) $(COMPOSE) config --quiet
	@echo "Compose 配置有效"

.PHONY: services-up
services-up: compose-config ## 构建并后台启动全部融合服务
	$(COMPOSE_ENV) $(COMPOSE) up -d --build

.PHONY: frontend
frontend: compose-config ## 构建并启动容器化统一前端及其依赖
	$(COMPOSE_ENV) $(COMPOSE) up -d --build lumiflow-web

.PHONY: wait
wait: ## 等待唯一公开入口就绪
	@echo "等待 LumiFlow $(PUBLIC_URL) ..."; \
	for attempt in $$(seq 1 90); do \
		if curl -fsS $(PUBLIC_URL) >/dev/null; then \
			echo "LumiFlow 已就绪：$(PUBLIC_URL)"; \
			exit 0; \
		fi; \
		sleep 2; \
	done; \
	echo "LumiFlow 在 180 秒内未就绪，请运行 make logs S=lumiflow-web"; \
	$(COMPOSE) ps; \
	exit 1

.PHONY: up
up: ## 一键启动完整系统，默认只公开 127.0.0.1:8080
	@$(MAKE) doctor
	@$(MAKE) services-up
	@$(MAKE) wait

.PHONY: start
start: up ## up 的别名

.PHONY: dev
dev: up ## 启动系统并持续查看统一前端日志
	@$(COMPOSE) logs -f lumiflow-web

.PHONY: down
down: ## 停止全部融合服务并保留数据卷
	$(COMPOSE) down --remove-orphans

.PHONY: ps
ps: ## 查看融合服务状态
	$(COMPOSE) ps

.PHONY: ports
ports: ## 查看当前项目实际公开的宿主机端口
	@docker ps --filter "label=com.docker.compose.project=$(COMPOSE_PROJECT)" \
		--format 'table {{.Names}}\t{{.Ports}}'

.PHONY: logs
logs: ## 查看容器日志；可用 S=lumiflow-web 或 S=live-api 筛选
	$(COMPOSE) logs -f $(S)

.PHONY: health
health: ## 检查公开入口和内部网关健康状态
	@curl -fsS $(PUBLIC_URL) >/dev/null
	@$(COMPOSE) exec -T live-api wget -qO- http://127.0.0.1:8080/health
	@echo

.PHONY: check
check: frontend-deps ## 运行两个 Go 后端测试及统一前端检查
	cd $(ROOT_DIR)/FluxFeed/apps/api && go test ./...
	cd $(BACKEND_DIR) && go test ./...
	cd $(FRONTEND_DIR) && npm run typecheck && npm run lint && npm run build
