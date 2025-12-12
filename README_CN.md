# CC-Bridge

[English](README.md) | [中文](README_CN.md)

[![GitHub release](https://img.shields.io/github/v/release/JillVernus/cc-bridge)](https://github.com/JillVernus/cc-bridge/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Docker Image](https://img.shields.io/badge/ghcr.io-jillvernus%2Fcc--bridge-blue?logo=docker)](https://github.com/JillVernus/cc-bridge/pkgs/container/cc-bridge)

> **Fork 声明**: 本项目基于 [BenedictKing/claude-proxy v2.0.44](https://github.com/BenedictKing/claude-proxy/tree/v2.0.44) 分叉开发，遵循 MIT 许可证。
>
> **免责声明**: 本仓库为个人自用开发，功能根据个人需求添加，可能不适用于所有场景。使用风险自负。

一个高性能的多供应商 AI 代理服务器，支持 OpenAI、Claude 及自定义 API，提供负载均衡、多 API 密钥管理和统一 API 入口。

---

## ✨ 新增功能（相比上游）

### 📊 请求日志系统
- **请求日志页面**：功能完整的日志查看器，使用 SQLite 存储
- **统计汇总**：按模型和供应商查看使用情况
- **自动刷新**：实时日志更新，可配置刷新间隔
- **详细日志**：包含时间戳、模型、供应商、Token 数（输入/输出/缓存读取/缓存写入）、费用、耗时、状态
- **日期筛选**：按日期范围筛选日志
- **重置数据库**：Web UI 中一键重置 SQLite 按钮

### 💰 计费系统
- **基础价格模型**：为每个模型配置基础价格
- **供应商倍率**：按供应商设置价格倍率（如高级供应商 1.2 倍）
- **模型倍率**：按模型设置价格倍率
- **Token 类型计费**：输入/输出/缓存 Token 分别计费

### 🎨 UI 改进
- **重构头部**：设置齿轮图标，Messages/Responses 供应商类型分开按钮，日志页面按钮
- **改进渠道编排**：优化故障转移序列按钮布局，调整备用资源池供应商名称空间
- **Claude & Codex 图标**：供应商类型视觉区分

### 🔧 其他增强
- **请求日志支持 Codex**：同时追踪 Claude Messages API 和 Codex Responses API 请求
- **特殊供应商类型**：支持额外供应商配置

---

## 🚀 核心功能（继承自上游）

- **🖥️ 一体化架构**：后端 + 前端单容器部署，替代 Nginx
- **🔐 统一认证**：单密钥保护所有入口（Web UI、管理 API、代理 API）
- **📱 Web 管理面板**：现代化 UI，渠道管理、实时监控
- **双 API 支持**：Claude Messages API (`/v1/messages`) 和 Codex Responses API (`/v1/responses`)
- **多供应商支持**：OpenAI（及兼容 API）、Claude
- **🔌 协议转换**：自动转换 Claude/OpenAI 格式
- **🎯 智能调度**：优先级路由、健康检查、自动熔断
- **📊 渠道编排**：拖拽调整优先级，实时健康状态
- **🔄 Trace 亲和**：同一用户会话绑定同一渠道
- **负载均衡**：轮询、随机、故障转移策略
- **多 API 密钥**：每个上游多密钥自动轮换
- **自动重试与密钥降级**：额度/余额不足自动切换
- **⚡ 自动熔断**：滑动窗口健康检测，15 分钟自动恢复
- **热重载**：配置修改无需重启
- **📡 流式/非流式**：完整支持两种模式
- **🛠️ 工具调用**：完整工具/函数调用支持
- **💬 会话管理**：Responses API 多轮对话追踪

## 🏗️ 架构设计

项目采用一体化架构，单容器部署，完全替代 Nginx：

```
用户 → 后端:3000 →
     ├─ / → 前端界面（需要密钥）
     ├─ /api/* → 管理API（需要密钥）
     ├─ /v1/messages → Claude Messages API 代理（需要密钥）
     └─ /v1/responses → Codex Responses API 代理（需要密钥）
```

**核心优势**：单端口、统一认证、无跨域问题、资源占用低

> 📚 详细架构设计和技术选型请参考 [ARCHITECTURE.md](ARCHITECTURE.md)

## 🏁 快速开始

### 📋 环境要求

**Docker 部署（推荐）：**
- Docker 20.10+
- Docker Compose v2+（可选）

**源码构建：**
- Go 1.22+
- Bun 1.0+（或 Node.js 18+ 配合 npm）
- Make（可选，用于 Makefile 命令）
- Git

<details>
<summary>📦 安装命令</summary>

**macOS:**
```bash
# 先安装 Homebrew（如果没有）
brew install go bun make
```

**Ubuntu/Debian:**
```bash
# Go
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Bun
curl -fsSL https://bun.sh/install | bash

# Make
sudo apt install make
```

**Windows:**
```powershell
# 使用 Chocolatey
choco install golang bun make

# 或使用 Scoop
scoop install go bun make
```
</details>

---

### 📦 推荐部署方式

| 部署方式       | 启动时间 | 内存占用 | 适用场景           |
| -------------- | -------- | -------- | ------------------ |
| **🐳 Docker**  | ~2s      | ~25MB    | 生产环境、一键部署（推荐） |
| **🚀 源码构建** | <100ms   | ~20MB    | 开发调试、自定义   |

> **注意**: 本项目不提供预编译的可执行文件，请使用 Docker 或从源码构建。

---

### 方式一：🐳 Docker 部署（推荐）

**适合所有用户，无需安装依赖，一键启动**

#### 直接拉取镜像运行（最简单）

```bash
# 拉取并运行最新版本
docker run -d \
  --name cc-bridge \
  -p 3000:3000 \
  -e PROXY_ACCESS_KEY=your-super-strong-secret-key \
  -v $(pwd)/.config:/app/.config \
  ghcr.io/jillvernus/cc-bridge:latest
```

**可用镜像标签：**

| 标签 | 说明 |
|------|------|
| `latest` | 最新稳定版本 |
| `v1.0.0`, `v1.0.1`, ... | 特定版本号 |

```bash
# 使用特定版本
docker pull ghcr.io/jillvernus/cc-bridge:v1.0.1

# 查看可用标签
# https://github.com/JillVernus/cc-bridge/pkgs/container/cc-bridge
```

#### 使用 docker-compose

```bash
# 1. 创建 docker-compose.yml（或克隆项目获取）
git clone https://github.com/JillVernus/cc-bridge
cd cc-bridge

# 2. 修改 docker-compose.yml 中的 PROXY_ACCESS_KEY

# 3. 启动服务
docker-compose up -d
```

访问地址：

- **Web 管理界面**: http://localhost:3000
- **Messages API 端点**: http://localhost:3000/v1/messages
- **Responses API 端点**: http://localhost:3000/v1/responses
- **健康检查**: http://localhost:3000/health

---

### 方式二：🚀 源码构建部署

**适合追求极致性能或需要自定义的用户**

```bash
# 1. 克隆项目
git clone https://github.com/JillVernus/cc-bridge
cd cc-bridge

# 2. 配置环境变量
cp backend-go/.env.example backend-go/.env
# 编辑 backend-go/.env 文件，设置你的配置

# 3. 启动服务
make run           # 普通用户运行（推荐）
# 或 make dev       # 开发调试（热重载）
# 或 make help      # 查看所有命令
```

**快捷命令说明：**

```bash
make run           # 普通用户运行（自动构建前端并启动后端）
make dev           # 开发调试（后端热重载）
make help          # 查看所有可用命令
```

---

## 🔧 配置管理

**两种配置方式**:

1. **Web 界面**（推荐）: 访问 `http://localhost:3000` → 输入密钥 → 可视化管理
2. **命令行工具**: `cd backend-go && make help`

> 📚 环境变量配置详见 [ENVIRONMENT.md](ENVIRONMENT.md)

## 🔐 安全配置

### 统一访问控制

所有访问入口均受 `PROXY_ACCESS_KEY` 保护：

1. **前端管理界面** (`/`) - 通过查询参数或本地存储验证密钥
2. **管理 API** (`/api/*`) - 需要 `x-api-key` 请求头
3. **代理 API** (`/v1/messages`) - 需要 `x-api-key` 请求头
4. **健康检查** (`/health`) - 公开访问，无需密钥

### 生产环境安全清单

```bash
# 1. 生成强密钥（必须！）
PROXY_ACCESS_KEY=$(openssl rand -base64 32)
echo "生成的密钥: $PROXY_ACCESS_KEY"

# 2. 生产环境配置
ENV=production
ENABLE_REQUEST_LOGS=false
ENABLE_RESPONSE_LOGS=false
LOG_LEVEL=warn
ENABLE_WEB_UI=true

# 3. 网络安全
# - 使用 HTTPS（推荐 Cloudflare CDN）
# - 配置防火墙规则
# - 定期轮换访问密钥
# - 启用访问日志监控
```

## 📖 API 使用

本服务支持两种 API 格式：

1. **Messages API** (`/v1/messages`) - 标准的 Claude API 格式
2. **Responses API** (`/v1/responses`) - Codex 格式，支持会话管理

### Messages API - 标准 Claude API 调用

```bash
curl -X POST http://localhost:3000/v1/messages \
  -H "x-api-key: your-proxy-access-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "max_tokens": 100,
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

### 流式响应

```bash
curl -X POST http://localhost:3000/v1/messages \
  -H "x-api-key: your-proxy-access-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "stream": true,
    "max_tokens": 100,
    "messages": [
      {"role": "user", "content": "Count to 10"}
    ]
  }'
```

### Responses API - Codex 格式调用

Responses API 支持会话管理和多轮对话，自动跟踪上下文：

```bash
curl -X POST http://localhost:3000/v1/responses \
  -H "x-api-key: your-proxy-access-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5",
    "max_tokens": 100,
    "input": "你好！请介绍一下你自己。"
  }'
```

### 管理 API

```bash
# 获取渠道列表
curl -H "x-api-key: your-proxy-access-key" \
  http://localhost:3000/api/channels

# 测试渠道连通性
curl -H "x-api-key: your-proxy-access-key" \
  http://localhost:3000/api/ping
```

## 📊 监控和日志

### 健康检查

```bash
# 健康检查端点（无需认证）
GET /health

# 返回示例
{
  "status": "healthy",
  "timestamp": "2024-01-01T00:00:00.000Z",
  "uptime": 3600,
  "mode": "production",
  "config": {
    "upstreamCount": 3,
    "loadBalance": "round-robin"
  }
}
```

### 日志级别

```bash
LOG_LEVEL=debug  # debug, info, warn, error
ENABLE_REQUEST_LOGS=true   # 记录请求日志
ENABLE_RESPONSE_LOGS=true  # 记录响应日志
```

## 🔧 故障排除

### 常见问题

1. **认证失败**

   ```bash
   # 检查密钥设置
   echo $PROXY_ACCESS_KEY

   # 验证密钥格式
   curl -H "x-api-key: $PROXY_ACCESS_KEY" http://localhost:3000/health
   ```

2. **容器启动失败**

   ```bash
   # 检查日志
   docker-compose logs cc-bridge

   # 检查端口占用
   lsof -i :3000
   ```

3. **前端界面无法访问**

   ```bash
   # 方案1: 重新构建（推荐）
   make build-current
   cd backend-go && ./dist/cc-bridge

   # 方案2: 验证构建产物是否存在
   ls -la frontend/dist/index.html

   # 方案3: 临时禁用 Web UI
   ENABLE_WEB_UI=false
   ```

### 重置配置

```bash
# 停止服务
docker-compose down

# 清理配置文件
rm -rf .config/*

# 重新启动
docker-compose up -d
```

## 🔄 更新升级

```bash
# 获取最新代码
git pull origin main

# 重新构建并启动
docker-compose up -d --build
```

## 📖 相关文档

- **📐 架构设计**: [ARCHITECTURE.md](ARCHITECTURE.md) - 技术选型、设计模式、数据流
- **⚙️ 环境配置**: [ENVIRONMENT.md](ENVIRONMENT.md) - 环境变量、配置场景、故障排除
- **🔨 开发指南**: [DEVELOPMENT.md](DEVELOPMENT.md) - 开发流程、调试技巧、最佳实践
- **🤝 贡献规范**: [CONTRIBUTING.md](CONTRIBUTING.md) - 提交规范、代码质量标准
- **📝 版本历史**: [CHANGELOG.md](CHANGELOG.md) - 完整变更记录和升级指南

## 📄 许可证

本项目基于 MIT 许可证开源 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🙏 致谢

- [BenedictKing/claude-proxy](https://github.com/BenedictKing/claude-proxy) - 上游项目
- [Anthropic](https://www.anthropic.com/) - Claude API
- [OpenAI](https://openai.com/) - GPT API
