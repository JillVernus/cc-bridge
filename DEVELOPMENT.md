# 开发指南

本文档为开发者提供开发环境配置、工作流程、调试技巧和最佳实践。

> 📚 **相关文档**
> - 架构设计和技术选型: [ARCHITECTURE.md](ARCHITECTURE.md)
> - 环境变量配置: [ENVIRONMENT.md](ENVIRONMENT.md)
> - 贡献规范: [CONTRIBUTING.md](CONTRIBUTING.md)

---

## 🎯 推荐开发方式

| 开发方式 | 启动速度 | 热重载 | 适用场景 |
|---------|---------|-------|---------|
| **🚀 Go 开发** | ⚡ 极快 | ✅ 支持 | **推荐：后端开发** |
| **🐳 Docker** | 🔄 中等 | ❌ 需重启 | 生产环境测试 |
| 🔧 Node.js/Bun | 🟢 较快 | ✅ 支持 | 备用：调试 JS/TS |

---

## 方式一：🚀 Go 版本开发（推荐）

**适合后端开发和性能优化，启动时间 <100ms**

### 快速开始

```bash
cd backend-go

# 查看所有可用命令
make help

# 开发模式（支持热重载）
make dev

# 构建并运行
make build-run

# 仅构建
make build-current
```

### 常用开发命令

```bash
# 配置管理
make config-interactive    # 交互式配置
make config-show          # 显示当前配置
make config-reset         # 重置配置

# 开发调试
make dev                  # 热重载开发模式
make test                 # 运行测试
make clean                # 清理构建产物
```

### Go 开发环境要求

- Go 1.22+
- Make（构建工具）
- Bun（前端构建）

> 📚 详细 Go 开发说明请参考 `backend-go/README.md`

---

## 🪟 Windows 环境配置

Windows 用户在开发本项目时可能遇到一些工具缺失的问题，以下是常见问题的解决方案。

### 问题 1: 没有 `make` 命令

Windows 默认不包含 `make` 工具，有以下几种解决方案：

#### 方案 A: 安装 Make (推荐)

```powershell
# 使用 Chocolatey (推荐)
choco install make

# 或使用 Scoop
scoop install make

# 或使用 winget
winget install GnuWin32.Make
```

#### 方案 B: 直接使用 Go 命令 (无需安装 make)

```powershell
cd backend-go

# 替代 make dev (需要先安装 air: go install github.com/air-verse/air@latest)
air

# 替代 make build
go build -o cc-bridge.exe .

# 替代 make run
go run main.go

# 替代 make test
go test ./...

# 替代 make fmt
go fmt ./...
```

### 问题 2: 没有 `vite` 命令

这是因为前端依赖未安装，`vite` 是项目的开发依赖。

#### 解决步骤

```powershell
cd frontend

# 使用 bun 安装依赖 (推荐)
bun install

# 或使用 npm
npm install

# 安装完成后运行开发服务器
bun run dev    # 或 npm run dev
```

### Windows 完整开发环境配置

#### 1. 安装包管理器 (可选但推荐)

```powershell
# 安装 Scoop (无需管理员权限)
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
irm get.scoop.sh | iex

# 或安装 Chocolatey (需要管理员权限)
Set-ExecutionPolicy Bypass -Scope Process -Force
[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))
```

#### 2. 安装开发工具

```powershell
# 使用 Scoop
scoop install git go bun make

# 或使用 Chocolatey
choco install git golang bun make -y
```

#### 3. 验证安装

```powershell
go version      # 应显示 go1.22+
bun --version   # 应显示版本号
make --version  # 应显示 GNU Make 版本
git --version   # 应显示 git 版本
```

### Windows 快速启动流程

```powershell
# 1. 克隆项目
git clone https://github.com/BenedictKing/cc-bridge
cd cc-bridge

# 2. 安装前端依赖
cd frontend
bun install    # 或 npm install

# 3. 配置环境变量
cd ../backend-go
copy .env.example .env
# 编辑 .env 文件设置 PROXY_ACCESS_KEY

# 4. 启动后端 (选择以下方式之一)

# 方式 A: 使用 make (如果已安装)
make dev

# 方式 B: 直接使用 Go
go run main.go

# 5. 另开终端，启动前端开发服务器 (如需单独开发前端)
cd frontend
bun run dev
```

### Windows 常见问题

#### PowerShell 执行策略限制

```powershell
# 如果遇到脚本执行限制，以管理员身份运行
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
```

#### 端口被占用

```powershell
# 查看端口占用
netstat -ano | findstr :3000

# 终止占用进程 (替换 PID 为实际进程 ID)
taskkill /PID <PID> /F
```

#### 路径包含空格

确保项目路径不包含空格和中文字符，推荐使用如 `C:\projects\cc-bridge` 这样的路径。

---

## 方式二：🔧 Node.js/Bun 开发（备用）

**仅推荐用于前端开发或 JavaScript/TypeScript 调试**

### 开发脚本说明

#### 生产环境

```bash
bun run start                 # 启动生产服务器
```

#### 开发环境

```bash
bun run dev                   # 启动开发服务器（源码文件变化时自动重启）
bun run build                 # 构建项目验证代码质量
bun run type-check            # TypeScript 类型检查
```

## 文件监听策略

### 源码文件（需要重启）

- `src/**/*.ts` - 所有源码文件
- `server.ts` - 主服务器文件
- `dev-runner.ts` - 自动重启脚本

**注意**: `config.json` 已被排除在源码监听之外，不会触发重启

**变化时**: 使用 `bun run dev` 时，服务器会自动重启。

### 配置文件（无需重启）

- `backend/.config/config.json` - 主配置文件

备份策略：每次写入前会在 `backend/.config/backups/` 目录生成时间戳备份，最多保留 10 个（自动轮转）。

**变化时**: 自动重载配置，保持服务器运行

### 环境变量文件（需要重启）

- `backend/.env` - 环境变量文件
- `backend/.env.example` - 环境变量示例

**变化时**: 需要重启服务器以加载新的环境变量

## 开发模式特性

### 1. 自动重启 (`dev`)

- ✅ 源码文件变化自动重启
- ✅ 配置文件变化自动重载（不重启）
- ✅ 智能重启控制（最多10次）
- ✅ 优雅关闭处理
- ✅ 详细的开发日志

### 2. 主服务器 (server.ts)

- ✅ 生产/开发环境自适应
- ✅ 开发模式端点和中间件
- ✅ 配置自动重载
- ✅ 详细的开发日志

### 3. 配置热重载

- ✅ 配置文件变化自动重载
- ✅ 基于文件的配置管理
- ✅ 手动重载端点
- ✅ 无需重启服务器

## 开发模式端点

### 健康检查

```
GET /health                # 基础健康检查
```

### 开发信息

```
GET /admin/dev/info        # 开发环境信息（需要 x-api-key）
```

### 配置重载

```
POST /admin/config/reload  # 手动重载配置
```

## 环境变量

```bash
# 开发环境
NODE_ENV=development                   # 开发模式
```

## 开发工作流

1. **启动开发服务器**

   ```bash
   bun run dev
   ```

2. **修改源码**
   - 服务器会自动重启
   - 保持请求会话

3. **修改配置**
   - 使用 `bun run config` 命令
   - 或直接编辑 `config.json`
   - 配置会自动重载，无需重启

4. **测试**
   - 使用 `/admin/dev/info`（带 x-api-key）查看状态
   - 使用健康检查端点验证

## 文件变化处理

| 文件类型 | 监听模式 | 处理方式 | 是否重启 |
| -------- | -------- | -------- | -------- |
| 源码文件 | 源码监听 | 自动重启 | ✅ 是    |
| 配置文件 | 配置监听 | 自动重载 | ❌ 否    |
| 环境变量 | 环境监听 | 需要重启 | ✅ 是    |

## 故障排除

### 端口占用

```bash
lsof -i :3000              # 查看端口占用
kill -9 <PID>              # 强制终止进程
```

### 配置重载失败

```bash
# 检查配置文件语法
cat backend/.config/config.json | jq .

# 手动重载配置
curl -X POST http://localhost:3000/admin/config/reload -H "x-api-key: <your-access-key>"
```

### 文件监听问题

- 确保没有在node_modules中
- 检查文件权限
- 重启开发服务器

## 最佳实践

1. **开发时使用 `dev`**
2. **生产环境使用 `start`**
3. **配置管理基于文件**
4. **定期检查日志输出**
5. **使用健康检查监控状态**
6. **配置修改无需重启**
7. **源码修改会自动重启**

## 🎯 代码质量标准

> 📚 完整的编码规范和设计模式请参考 [ARCHITECTURE.md](ARCHITECTURE.md)

### 编程原则

项目严格遵循以下软件工程原则：

#### 1. KISS 原则 (Keep It Simple, Stupid)
- 追求代码和设计的极致简洁
- 优先选择最直观的解决方案
- 使用正则表达式替代复杂的字符串处理逻辑

#### 2. DRY 原则 (Don't Repeat Yourself)  
- 消除重复代码，提取共享函数
- 统一相似功能的实现方式
- 例：`normalizeClaudeRole` 函数的提取和共享

#### 3. YAGNI 原则 (You Aren't Gonna Need It)
- 仅实现当前明确所需的功能
- 删除未使用的代码和依赖
- 避免过度设计和未来特性预留

#### 4. 函数式编程优先
- 使用 `map`、`reduce`、`filter` 等函数式方法
- 优先使用不可变数据操作
- 例：命令行参数解析使用 `reduce()` 替代传统循环

### 代码优化检查清单

在提交代码前，请确保：

- [ ] 使用正则表达式处理字符串匹配
- [ ] 避免重复的 `toLowerCase()` 调用
- [ ] 提取重复的函数到共享模块
- [ ] 使用 `slice()` 替代 `substring()`  
- [ ] 函数式方法替代传统循环
- [ ] 通过 `bun run type-check` 类型检查
- [ ] 通过 `bun run build` 构建验证

### 性能优化指导

#### 字符串处理优化
```typescript
// ❌ 避免
if (str.toLowerCase().startsWith('bearer ')) {
  return str.substring(7)
}

// ✅ 推荐  
return str.replace(/^bearer\s+/i, '')
```

#### 正则表达式最佳实践
```typescript
// ❌ 避免复杂的条件判断
if (line.startsWith('data: ')) {
  jsonStr = line.substring(6)
} else if (line.startsWith('data:')) {
  jsonStr = line.substring(5)
}

// ✅ 使用正则表达式
const match = line.match(/^data:\s*(.*)$/)
const jsonStr = match ? match[1] : line
```

### TypeScript 规范

- 使用严格的 TypeScript 配置
- 所有函数和变量都有明确的类型声明
- 使用接口定义数据结构
- 避免使用 `any` 类型

### 命名规范

- **文件名**: kebab-case (例: `config-manager.ts`)
- **类名**: PascalCase (例: `ConfigManager`)
- **函数名**: camelCase (例: `getNextApiKey`)
- **常量名**: SCREAMING_SNAKE_CASE (例: `DEFAULT_CONFIG`)

### 错误处理

- 使用 try-catch 捕获异常
- 提供有意义的错误消息
- 记录错误日志
- 优雅降级处理

```typescript
try {
  const result = await riskyOperation()
  return result
} catch (error) {
  console.error('Operation failed:', error)
  throw new Error('Specific error message for user')
}
```

### 日志规范

使用分级日志系统：

```typescript
console.error('严重错误信息') // 错误级别
console.warn('警告信息') // 警告级别
console.log('一般信息') // 信息级别
console.debug('调试信息') // 调试级别
```

## 🧪 测试策略

### 手动测试

#### 1. 基础功能测试

```bash
# 测试健康检查
curl http://localhost:3000/health

# 测试基础对话
curl -X POST http://localhost:3000/v1/messages \
  -H "x-api-key: test-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-5-sonnet-20241022","max_tokens":100,"messages":[{"role":"user","content":"Hello"}]}'

# 测试流式响应
curl -X POST http://localhost:3000/v1/messages \
  -H "x-api-key: test-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-5-sonnet-20241022","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"Count to 10"}]}'
```

#### 2. 负载均衡测试

```bash
# 添加多个 API 密钥
bun run config key test-upstream add key1 key2 key3

# 设置轮询策略
bun run config balance round-robin

# 发送多个请求观察密钥轮换
for i in {1..5}; do
  curl -X POST http://localhost:3000/v1/messages \
    -H "x-api-key: test-key" \
    -H "Content-Type: application/json" \
    -d '{"model":"claude-3-5-sonnet-20241022","max_tokens":10,"messages":[{"role":"user","content":"Test '$i'"}]}'
done
```

### 集成测试

#### Claude Code 集成测试

1. 配置 Claude Code 使用本地代理
2. 测试基础对话功能
3. 测试工具调用功能
4. 测试流式响应
5. 验证错误处理

#### 压力测试

```bash
# 使用 ab (Apache Bench) 进行压力测试
ab -n 100 -c 10 -p request.json -T application/json \
  -H "x-api-key: test-key" \
  http://localhost:3000/v1/messages
```

## 🔧 调试技巧

### 1. 日志分析

```bash
# 实时查看日志
tail -f server.log

# 过滤错误日志
grep -i "error" server.log

# 分析请求模式
grep -o "POST /v1/messages" server.log | wc -l
```

### 2. 配置调试

```bash
# 验证配置文件
cat config.json | jq .

# 检查环境变量
env | grep -E "(PORT|LOG_LEVEL)"
```

### 3. 网络调试

```bash
# 测试上游连接
curl -I https://api.openai.com

# 检查 DNS 解析
nslookup api.openai.com

# 测试端口连通性
telnet localhost 3000
```

## 🚀 部署指南

### 开发环境部署

```bash
# 1. 安装依赖
bun install

# 2. 配置环境变量
cp backend/.env.example backend/.env
vim backend/.env

# 3. 启动开发服务器
bun run dev
```

### 生产环境部署

```bash
# 1. 安装依赖
bun install --production

# 2. 配置环境变量
export NODE_ENV=production
export PORT=3000
# 3. 启动服务器
bun run start

# 4. 设置进程管理 (推荐 PM2)
pm2 start server.ts --name cc-bridge
pm2 save
pm2 startup
```

### Docker 部署

```dockerfile
FROM oven/bun:1 as base
WORKDIR /app

# 安装依赖
COPY package.json bun.lockb ./
RUN bun install --frozen-lockfile

# 复制源码
COPY . .

# 暴露端口并启动
EXPOSE 3000
CMD ["bun", "run", "start"]
```

```bash
# 构建和运行
docker build -t claude-api-proxy .
docker run -p 3000:3000 -v $(pwd)/backend/.config:/app/.config -v $(pwd)/backend/.env:/app/.env --name cc-bridge-container claude-api-proxy
```

## 🤝 贡献与发布

### 贡献指南

欢迎提交 Issue 和 Pull Request！

> 📚 详细的贡献规范和提交指南请参考 [CONTRIBUTING.md](CONTRIBUTING.md)

### 版本发布

> 📚 维护者版本发布流程请参考 [RELEASE.md](RELEASE.md)
