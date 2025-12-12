# CC-Bridge

[English](README.md) | [中文](README_CN.md)

[![GitHub release](https://img.shields.io/github/v/release/JillVernus/cc-bridge)](https://github.com/JillVernus/cc-bridge/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Docker Image](https://img.shields.io/badge/ghcr.io-jillvernus%2Fcc--bridge-blue?logo=docker)](https://github.com/JillVernus/cc-bridge/pkgs/container/cc-bridge)

> **Fork Notice**: This project is forked from [BenedictKing/claude-proxy v2.0.44](https://github.com/BenedictKing/claude-proxy/tree/v2.0.44) under MIT License.
>
> **Disclaimer**: This repository is developed for personal use. Features are added based on personal needs and may not be suitable for all use cases. Use at your own risk.

A multi-provider AI proxy server with Web UI, supporting OpenAI/Claude protocol conversion, load balancing, and unified API access.

## Quick Start

```bash
docker run -d \
  --name cc-bridge \
  -p 3000:3000 \
  -e PROXY_ACCESS_KEY=your-secret-key \
  -v $(pwd)/.config:/app/.config \
  ghcr.io/jillvernus/cc-bridge:latest
```

Then visit http://localhost:3000 and enter your key.

## Features

### Core Features
- **All-in-One**: Backend + Frontend in single container (replaces Nginx)
- **Dual API**: Claude Messages API (`/v1/messages`) + Codex Responses API (`/v1/responses`)
- **Protocol Conversion**: Auto-convert between Claude/OpenAI formats
- **Multi-Provider**: OpenAI, Claude, and compatible APIs
- **Smart Scheduling**: Priority routing, health checks, auto circuit-breaker
- **Load Balancing**: Round-robin, random, failover strategies
- **Hot Reload**: Config changes apply without restart

### New Features (vs Upstream)
- **Request Logs**: SQLite storage, usage stats by model/provider, date filters
- **Pricing System**: Base prices, provider/model multipliers, token-type pricing
- **UI Improvements**: Redesigned header, better channel orchestration, Claude/Codex icons

## Architecture

```
User → Backend:3000 →
     ├─ /           → Web UI (requires key)
     ├─ /api/*      → Admin API (requires key)
     ├─ /v1/messages   → Claude Messages API (requires key)
     ├─ /v1/responses  → Codex Responses API (requires key)
     └─ /health     → Health check (public)
```

## Installation

### Docker (Recommended)

**Pull from GHCR:**
```bash
# Latest version
docker pull ghcr.io/jillvernus/cc-bridge:latest

# Specific version
docker pull ghcr.io/jillvernus/cc-bridge:v1.0.1
```

**Run with docker-compose:**
```bash
git clone https://github.com/JillVernus/cc-bridge
cd cc-bridge
# Edit PROXY_ACCESS_KEY in docker-compose.yml
docker-compose up -d
```

**Supported architectures:** `linux/amd64`, `linux/arm64`

### Build from Source

```bash
git clone https://github.com/JillVernus/cc-bridge
cd cc-bridge
cp backend-go/.env.example backend-go/.env
# Edit backend-go/.env
make run
```

## Configuration

### Web UI (Recommended)
Visit http://localhost:3000 → Enter key → Visual management

### Environment Variables
See [ENVIRONMENT.md](ENVIRONMENT.md) for all options.

Key variables:
| Variable | Description | Default |
|----------|-------------|---------|
| `PROXY_ACCESS_KEY` | Access key for all endpoints | (required) |
| `ENABLE_WEB_UI` | Enable Web UI | `true` |
| `LOG_LEVEL` | Log level (debug/info/warn/error) | `info` |

## API Usage

### Messages API (Claude format)

```bash
curl -X POST http://localhost:3000/v1/messages \
  -H "x-api-key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Responses API (Codex format)

```bash
curl -X POST http://localhost:3000/v1/responses \
  -H "x-api-key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5",
    "max_tokens": 100,
    "input": "Hello!"
  }'
```

### Streaming

Add `"stream": true` to the request body.

### Multi-turn Conversations (Responses API)

Use `previous_response_id` from the response to continue conversations.

## Cloud Deployment

<details>
<summary>Railway / Render / Fly.io / Zeabur</summary>

**Railway:**
```bash
# Connect GitHub repo, set environment variables:
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
Connect GitHub repo → Set environment variables → Auto deploy

</details>

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Auth failed | Check `PROXY_ACCESS_KEY` is set correctly |
| Container won't start | Run `docker-compose logs cc-bridge` |
| Frontend 404 | Ensure `ENABLE_WEB_UI=true`, rebuild if needed |
| Port conflict | Check `lsof -i :3000` |

**Reset configuration:**
```bash
docker-compose down
rm -rf .config/*
docker-compose up -d
```

## Documentation

| Document | Description |
|----------|-------------|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Technical design, patterns, data flow |
| [ENVIRONMENT.md](ENVIRONMENT.md) | Environment variables reference |
| [DEVELOPMENT.md](DEVELOPMENT.md) | Development workflow, debugging |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Contribution guidelines |
| [CHANGELOG.md](CHANGELOG.md) | Version history |
| [RELEASE.md](RELEASE.md) | Release process |

## License

MIT License - see [LICENSE](LICENSE)

## Acknowledgments

- [BenedictKing/claude-proxy](https://github.com/BenedictKing/claude-proxy) - Upstream project
- [Anthropic](https://www.anthropic.com/) - Claude API
- [OpenAI](https://openai.com/) - GPT API
