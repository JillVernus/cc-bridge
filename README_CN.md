# CC-Bridge

[English](README.md) | [中文](README_CN.md)

[![GitHub release](https://img.shields.io/github/v/release/JillVernus/cc-bridge)](https://github.com/JillVernus/cc-bridge/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Docker Image](https://img.shields.io/badge/ghcr.io-jillvernus%2Fcc--bridge-blue?logo=docker)](https://github.com/JillVernus/cc-bridge/pkgs/container/cc-bridge)

> **Fork 声明**: 本项目基于 [BenedictKing/claude-proxy v2.0.44](https://github.com/BenedictKing/claude-proxy/tree/v2.0.44) 分叉开发，遵循 MIT 许可证。
>
> **免责声明**: 本仓库为个人自用开发，功能根据个人需求添加，可能不适用于所有场景。使用风险自负。

多供应商 AI 代理服务器，带 Web 管理界面，支持 OpenAI/Claude 协议转换、负载均衡和统一 API 入口。

## 快速开始

```bash
docker run -d \
  --name cc-bridge \
  -p 3000:3000 \
  -e PROXY_ACCESS_KEY=your-secret-key \
  -v $(pwd)/.config:/app/.config \
  ghcr.io/jillvernus/cc-bridge:latest
```

然后访问 http://localhost:3000 并输入密钥。

## 功能特性

### 核心功能
- **一体化部署**: 后端 + 前端单容器（替代 Nginx）
- **双 API 支持**: Claude Messages API (`/v1/messages`) + Codex Responses API (`/v1/responses`)
- **协议转换**: 自动转换 Claude/OpenAI 格式
- **多供应商**: OpenAI、Claude 及兼容 API
- **智能调度**: 优先级路由、健康检查、自动熔断
- **负载均衡**: 轮询、随机、故障转移策略
- **热重载**: 配置修改无需重启

### 新增功能（相比上游）
- **请求日志**: SQLite 存储，按模型/供应商统计，日期筛选
- **计费系统**: 基础价格、供应商/模型倍率、Token 类型计费
- **UI 改进**: 重构头部、优化渠道编排、Claude/Codex 图标区分

## 架构

```
用户 → 后端:3000 →
     ├─ /              → Web 界面（需要密钥）
     ├─ /api/*         → 管理 API（需要密钥）
     ├─ /v1/messages   → Claude Messages API（需要密钥）
     ├─ /v1/responses  → Codex Responses API（需要密钥）
     └─ /health        → 健康检查（公开）
```

## 安装

### Docker（推荐）

**从 GHCR 拉取:**
```bash
# 最新版本
docker pull ghcr.io/jillvernus/cc-bridge:latest

# 指定版本
docker pull ghcr.io/jillvernus/cc-bridge:v1.0.1
```

**使用 docker-compose:**
```bash
git clone https://github.com/JillVernus/cc-bridge
cd cc-bridge
# 编辑 docker-compose.yml 中的 PROXY_ACCESS_KEY
docker-compose up -d
```

**支持架构:** `linux/amd64`, `linux/arm64`

### 源码构建

```bash
git clone https://github.com/JillVernus/cc-bridge
cd cc-bridge
cp backend-go/.env.example backend-go/.env
# 编辑 backend-go/.env
make run
```

## 配置

### Web 界面（推荐）
访问 http://localhost:3000 → 输入密钥 → 可视化管理

### 环境变量
详见 [ENVIRONMENT.md](ENVIRONMENT.md)。

主要变量：
| 变量 | 说明 | 默认值 |
|------|------|--------|
| `PROXY_ACCESS_KEY` | 所有端点的访问密钥 | （必填）|
| `ENABLE_WEB_UI` | 启用 Web 界面 | `true` |
| `LOG_LEVEL` | 日志级别（debug/info/warn/error）| `info` |

## API 使用

### Messages API（Claude 格式）

```bash
curl -X POST http://localhost:3000/v1/messages \
  -H "x-api-key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "你好！"}]
  }'
```

### Responses API（Codex 格式）

```bash
curl -X POST http://localhost:3000/v1/responses \
  -H "x-api-key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5",
    "max_tokens": 100,
    "input": "你好！"
  }'
```

### 流式响应

在请求体中添加 `"stream": true`。

### 多轮对话（Responses API）

使用响应中的 `previous_response_id` 继续对话。

## 云平台部署

<details>
<summary>Railway / Render / Fly.io / Zeabur</summary>

**Railway:**
```bash
# 连接 GitHub 仓库，设置环境变量：
PROXY_ACCESS_KEY=your-key
ENABLE_WEB_UI=true
ENV=production
```

**Fly.io:**
```bash
fly launch --dockerfile Dockerfile
fly secrets set PROXY_ACCESS_KEY=your-key
fly deploy
```

**Render / Zeabur:**
连接 GitHub 仓库 → 设置环境变量 → 自动部署

</details>

## 故障排除

| 问题 | 解决方案 |
|------|----------|
| 认证失败 | 检查 `PROXY_ACCESS_KEY` 设置是否正确 |
| 容器启动失败 | 执行 `docker-compose logs cc-bridge` |
| 前端 404 | 确保 `ENABLE_WEB_UI=true`，必要时重新构建 |
| 端口冲突 | 检查 `lsof -i :3000` |

**重置配置:**
```bash
docker-compose down
rm -rf .config/*
docker-compose up -d
```

## 文档

| 文档 | 说明 |
|------|------|
| [ARCHITECTURE.md](ARCHITECTURE.md) | 技术设计、模式、数据流 |
| [ENVIRONMENT.md](ENVIRONMENT.md) | 环境变量参考 |
| [DEVELOPMENT.md](DEVELOPMENT.md) | 开发流程、调试技巧 |
| [CONTRIBUTING.md](CONTRIBUTING.md) | 贡献指南 |
| [CHANGELOG.md](CHANGELOG.md) | 版本历史 |
| [RELEASE.md](RELEASE.md) | 发布流程 |

## 许可证

MIT 许可证 - 详见 [LICENSE](LICENSE)

## 致谢

- [BenedictKing/claude-proxy](https://github.com/BenedictKing/claude-proxy) - 上游项目
- [Anthropic](https://www.anthropic.com/) - Claude API
- [OpenAI](https://openai.com/) - GPT API
