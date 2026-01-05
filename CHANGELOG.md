# 版本历史

> **注意**: v2.0.0 开始为 Go 语言重写版本，v1.x 为 TypeScript 版本

---

## [v1.3.134] - 2026-01-05

### 🔧 改进

- **日志按日期轮转**: 移除 lumberjack 依赖，改用自定义 DailyWriter 实现按日期轮转
  - 日志文件格式: `YYYYMMDD-app.log`（如 `20260105-app.log`）
  - 自动清理过期日志（根据 MaxAge 配置）
  - 每小时检查一次过期日志并清理
  - 减少外部依赖，简化日志管理

---

## [v1.3.133] - 2026-01-05

### ✨ 新功能

- **故障转移信息显示**: 请求日志现在显示故障转移处理详情，格式为 `[错误模式] > [动作] > [详情]`
  - 示例: `429:QUOTA_EXHAUSTED > suspended > next channel`
  - 示例: `429:model_cooldown > retry_wait > 20s`
  - 示例: `429 > threshold > 1/3`
  - 在请求日志表格的 tooltip 和详情弹窗中均可查看

---

## [v1.3.132] - 2026-01-04

### 🐛 Bug 修复

- **修复 429 重试后故障转移日志缺失错误信息**: 当 429 错误触发 `retry_wait` 动作后所有重试失败时，故障转移日志现在正确显示 HTTP 状态码和上游错误信息

---

## [v1.3.131] - 2026-01-04

### 🐛 Bug 修复

- **故障转移日志显示 HTTP 状态码**: 故障转移记录的 Error 字段现在包含 HTTP 状态码（如 "429: failover to next channel (1/2)"），便于快速识别故障原因

### 🔧 改进

- **渠道暂停调试日志**: 添加详细日志记录渠道暂停时使用的 QuotaResetAt 时间或默认 5 分钟超时，便于排查暂停机制问题
- **暂停检查调试日志**: IsChannelSuspended 现在记录暂停状态检查结果，包括过期时间和原因

---

## [v1.3.130] - 2026-01-04

### 🔧 改进

- **故障转移设置重命名**: 将 "Failover Settings" 重命名为 "Quota/Credit Failover Settings"，更清晰地表明设置仅适用于配额渠道

---

## [v1.3.129] - 2026-01-04

### ✨ 新功能

- **故障转移逻辑按配额类型分离**: 根据渠道配额配置应用不同的故障转移策略
  - **请求配额/额度配额渠道**: 使用管理员配置的故障转移规则（支持阈值、等待重试、渠道暂停等）
  - **普通渠道（无配额）**: 使用传统熔断器逻辑（429/401/403 立即切换，其他错误返回客户端）
  - 新增 `LegacyFailover()` 方法实现原始熔断器行为

### 🔧 改进

- **故障转移设置 UI**: 添加信息提示说明设置仅适用于配额渠道
- **代码清理**: 移除冗余的 failoverConfig 获取调用，仅在配额渠道需要时获取

---

## [v1.3.128] - 2026-01-04

### 🐛 Bug 修复

- **修复 ClearChannelSuspension 签名不匹配**: 函数现在返回 `(bool, error)` 以正确表示是否实际清除了暂停状态
- **修复 model_cooldown 忙等待循环**: 当响应中缺少 `reset_seconds` 时添加默认 2 秒等待，避免无限快速重试
- **修复 429 子类型优先级冲突**: 将 QUOTA_EXHAUSTED 检测优先级提高到 model_cooldown 之前，确保配额耗尽优先触发渠道暂停
- **修复 selectFallbackChannel 缺少暂停检查**: 降级选择渠道时现在也会跳过已暂停的渠道

### 🔧 改进

- **SuspendChannel 错误日志记录**: 将所有 `_ = reqLogManager.SuspendChannel(...)` 调用改为正确的错误处理和日志记录
- **区分 sql.ErrNoRows 和真实数据库错误**: IsChannelSuspended 现在会记录真实的数据库错误而非静默忽略
- **优化渠道暂停清理**: 移除每次请求时的 goroutine 清理，改为使用现有的 60 秒周期性清理任务，避免高并发时的 goroutine 雪崩

---

## [v1.3.127] - 2026-01-04

### ✨ 新功能

- **配额渠道暂停机制**: 当配额渠道遇到 429 配额耗尽错误时，自动暂停渠道直到配额重置
  - 新增 `suspend_channel` 动作类型到故障转移规则
  - 渠道暂停状态存储在 SQLite 数据库中，支持跨重启持久化
  - 暂停时间自动设置为渠道的 `quotaResetAt` 时间（若配置）或默认 5 分钟
  - 调度器在选择渠道时自动跳过已暂停的渠道
  - 配额重置操作（手动或自动）会同时清除渠道暂停状态

- **429 错误智能处理扩展到 Responses API**: 将 429 子类型检测从 Messages API 扩展到 Responses API
  - 支持 `retry_same_key`（等待后使用同一密钥重试）
  - 支持 `failover_key`（立即切换到下一个密钥）
  - 支持 `suspend_channel`（暂停渠道并切换到下一个渠道）
  - 单渠道模式下暂停渠道后返回错误给客户端

### 🔧 改进

- 优化故障转移追踪器的决策逻辑，返回结构化的 `FailoverDecision` 包含动作、等待时间和暂停信息
- 新增 `SuspensionChecker` 接口实现调度器与请求日志管理器的解耦
- 前端已支持显示暂停状态渠道（使用警告色徽章和闪烁动画）

---

## [v1.3.126] - 2026-01-04

### ✨ 改进

- **电路断路器与管理员故障转移设置分离**: 解决两个系统之间的冲突
  - 当管理员故障转移阈值设置启用 (`failover.enabled=true`) 时，自动禁用电路断路器
  - 当管理员故障转移阈值设置禁用时，使用传统电路断路器行为（滑动窗口失败率检测）
  - 避免两套故障检测机制同时运行导致的行为冲突
  - 影响 Messages API 和 Responses API 的渠道调度器

---

## [v1.3.125] - 2026-01-04

### ✨ 新功能

- **429 错误智能处理**: 针对 Claude Messages API 的 429 错误增加子类型检测和差异化处理
  - **配额耗尽** (`QUOTA_EXHAUSTED`): 立即故障转移到下一个密钥，绕过阈值计数
  - **模型冷却** (`model_cooldown`): 等待 `reset_seconds + 1s` 后使用同一密钥重试
  - **资源耗尽** (通用 `RESOURCE_EXHAUSTED`): 等待 20 秒后使用同一密钥重试
  - 未知 429 错误回退到现有阈值行为
  - 新增可配置等待时间: `genericResourceWaitSeconds`, `modelCooldownExtraSeconds`, `modelCooldownMaxWaitSeconds`
  - 仅影响 `/v1/messages` (Claude API)，不影响 `/v1/responses` (Codex)

---

## [v1.3.124] - 2026-01-04

### ✨ 新功能

- **调试数据指示器**: 在请求日志表格中添加调试数据可用性指示
  - 状态列新增小虫图标，表示该请求记录了完整的请求/响应头和体
  - 当调试数据因保留期限到期或手动清除后，指示器会自动消失
  - 后端通过 LEFT JOIN 查询高效判断调试数据是否存在

---

## [v1.3.123] - 2026-01-04

### ✨ 改进

- **请求详情弹窗增强**: 点击日志表格行时弹出的详情窗口现在始终显示元数据
  - 新增 "元数据" 标签页，显示请求 ID、端点、流式模式、时间信息和状态
  - 当调试日志关闭时仍可查看基本元数据信息
  - 当调试日志开启时同时显示完整的请求/响应头和体
  - 耗时根据时长自动着色（绿色 ≤5s，黄色 ≤15s，红色 >15s）

---

## [v1.3.122] - 2026-01-04

### 🐛 修复

- **配额自动重置失效**: 修复滚动周期(rolling)和固定周期(fixed)模式下配额无法自动重置的问题
  - 原因: `calculateNextReset()` 始终返回未来时间，导致重置条件永远为 false
  - 滚动模式: 现在直接检查 `quotaResetAt` 是否已过期
  - 固定模式: 新增 `calculatePreviousReset()` 计算最近一次应触发的重置时间
  - 影响 Messages API 和 Responses API 的配额管理

---

## [v1.3.121] - 2026-01-04

### 🐛 修复

- **请求日志永久 pending**: 修复当故障转移阈值未达到时请求日志一直显示"加载中"的问题
  - 当错误发生但未达到故障转移阈值时，请求日志现在会正确更新为"错误"状态
  - 影响 Messages API (`/v1/messages`) 和 Responses API (`/v1/responses`)

---

## [v1.3.120] - 2026-01-03

### ✨ 新功能

- **故障转移阈值设置**: 新增可配置的故障转移阈值功能
  - 支持按错误码分组设置连续失败阈值（如 401,403 需连续 2 次才触发故障转移）
  - 可启用/禁用阈值功能（禁用时任何错误立即触发故障转移）
  - 成功响应（2xx）后自动重置错误计数器
  - 支持一键重置为默认配置
  - 设置界面提供常见 HTTP 错误码参考提示

### 🐛 修复

- **故障转移设置界面**: 修复"重置默认"后错误码未正确显示的问题（字段名 codes→errorCodes 不匹配）

---

## [v1.3.119] - 2026-01-03

### 🐛 修复

- **多渠道故障转移日志**: 修复多渠道模式下第一个渠道失败时请求日志未记录的问题
  - 新增 `failover` 状态标识故障转移请求
  - 每次渠道切换都会记录独立的日志条目，完整追踪故障转移链路
  - 保留原始上游错误信息，同时添加 "failover to next channel" 标识
  - 前端日志表格支持显示故障转移状态（橙色标识）
  - 故障转移请求不计入统计数据

---

## [v1.3.118] - 2026-01-03

### 🐛 修复

- **调试日志响应体捕获**: 修复流式响应模式下响应体未被捕获的问题

---

## [v1.3.117] - 2026-01-03

### ✨ 新功能

- **调试日志功能**: 新增请求/响应调试日志捕获功能，用于排查问题
  - 支持在设置中启用/禁用调试日志
  - 可配置日志保留时间（1-168小时）和最大请求体大小（64KB-2MB）
  - 请求/响应数据使用 gzip 压缩存储，节省约 80-90% 空间
  - 点击日志表格任意行可查看完整的请求/响应详情（请求头 + 请求体）
  - 支持一键清除所有调试日志
  - 后台自动清理过期调试日志

### 🔧 优化

- **构建警告抑制**: 抑制 Sass 弃用警告，使构建日志更清晰

---

## [v1.3.116] - 2026-01-03

### 🐛 修复

- **渠道配额保存问题**: 修复配额设置保存后 quotaResetMode 字段未返回的问题
- **配额字段 NaN 处理**: 修复前端 v-model.number 对空输入返回 NaN 导致保存失败的问题
- **编辑渠道后刷新**: 编辑渠道成功后自动刷新渠道列表以获取最新数据

---

## [v1.3.115] - 2026-01-03

### 🐛 修复

- **渠道配额清除失败**: 修复将渠道配额类型从"请求数"或"额度"切换为"无"时无法保存的问题

---

## [v1.3.114] - 2026-01-03

### ✨ 新功能

- **API 密钥细粒度权限控制**: 支持为 API 密钥设置访问权限限制 (⚠️ 未经测试)
  - **端点限制**: 限制密钥只能访问 `/v1/messages` 或 `/v1/responses` 或两者
  - **模型限制**: 使用 glob 模式限制可用模型（如 `claude-sonnet-*`、`gpt-4*`）
  - **渠道限制**: 分别限制 Messages API 和 Responses API 可用的渠道
  - 权限留空表示不限制（向后兼容）
  - 前端管理界面支持权限配置

---

## [v1.3.113] - 2026-01-02

### ✨ 新功能

- **定价设置复制按钮**: 模型定价表格新增复制按钮，可快速复制现有模型定价创建新条目

### 🐛 修复

- **缓存价格 $0 显示**: 修复保存缓存价格为 $0 时被存储为空值的问题，现在正确显示 $0
- **配额计数包含 400 错误**: 请求数配额类型现在同时计算 HTTP 200 和 400 响应（400 为客户端错误，部分供应商仍计为有效请求）

---

## [v1.3.112] - 2026-01-02

### 🎨 界面优化

- **日志页面图表默认折叠**: 全局统计图表默认折叠，减少页面初始加载内容，可点击展开查看

---

## [v1.3.111] - 2026-01-02

### ✨ 新功能

- **编辑渠道时删除已有 API 密钥**: 支持在编辑渠道时查看和删除已有的 API 密钥
  - 后端返回掩码密钥列表 (`maskedKeys`)，显示密钥前8位和后5位
  - 前端显示已有密钥列表，支持删除和重排序操作
  - 使用基于索引的 API 端点，确保安全性（不在 URL 中传递完整密钥）

### 🔧 技术改进

- **响应头超时默认值调整**: 将默认响应头超时从 30 秒改为 120 秒
  - 更适合长时间推理的模型请求

---

## [v1.3.110] - 2026-01-02

### ✨ 新功能

- **渠道配额模型过滤**: 支持为渠道配额设置模型过滤规则 (⚠️ 未经测试)
  - 新增 `quotaModels` 配置项，支持多个模型匹配规则
  - 使用子字符串匹配（如输入 `opus` 可匹配 `claude-opus-4`、`claude-opus-4-5-20251101` 等）
  - 留空则对所有模型计数（向后兼容）
  - 后端：`UpstreamConfig.ShouldCountQuota()` 方法实现模型匹配逻辑
  - 前端：AddChannelModal 添加 chips 输入组件，支持自由文本输入

### 🔧 技术改进

- **配额设置 UI 优化**: 将字段描述从 persistent-hint 改为 info 图标 + tooltip
  - 解决 hint 文字导致字段被向上推挤的布局问题
  - 增加 First Reset Time 字段宽度以完整显示 AM/PM

---

## [v1.3.109] - 2026-01-02

### 🔧 技术改进

- **请求日志表格列合并**: 将 `status` 和 `httpStatus` 两列合并为单一 `status` 列
  - 移除冗余的 `httpStatus` 列
  - `status` 列移至表格最后位置
  - 状态芯片现在显示 HTTP 状态码 (如 200, 500) 而非文本标签
  - 错误提示框 (error/upstreamError) 从 httpStatus 移至 status 列
  - 移除未使用的 `getStatusColor()` 函数

---

## [v1.3.108] - 2026-01-02

### ✨ 新功能

- **请求日志表格 UI 改进**:
  - **Duration 列格式化**: 改为左对齐，使用固定宽度格式 (7位数字 + 空格 + "ms")，便于视觉对齐
  - **Token 列拆分**: 将原来的单一 Tokens 列拆分为 4 个独立列 (Input/Output/Cache/Hit)
    - 每列使用固定宽度格式 (6位 + 空格 + 符号)
    - 保留 K/M 缩写显示大数字
    - 保留原有颜色编码 (绿/蓝/成功色/警告色)
  - **表格单元格紧凑布局**: 减少 Vuetify 默认的单元格内边距 (16px → 4px)

### 🐛 修复

- **修复列宽调整失效问题**: 当隐藏某些列时，其他列的调整手柄会失效
  - 原因：使用独立的 `v-slot:[header.xxx]` 模板时，Vuetify 的插槽匹配在动态列可见性下出现错位
  - 解决：改用单一的 `v-slot:headers` 模板，通过遍历 columns 数组确保正确绑定

### 🔧 技术改进

- 新增 `formatDuration()` 和 `formatTokens()` 格式化函数
- 新增 `getHeaderColorClass()` 辅助函数用于动态表头颜色
- localStorage 迁移：自动移除旧的 `tokens` 列配置，应用新的 4 列默认值

---

## [v1.3.107] - 2026-01-02

### ✨ 新功能

- **用户自定义渠道配额**: 支持为 Messages 和 Responses 渠道配置使用配额
  - 支持两种配额类型：请求数 (`requests`) 或额度 (`credit`)
  - 可配置首次重置时间和重置间隔（小时/天/周/月）
  - 自动重置：到达重置时间后自动清零
  - 手动重置：点击配额条可手动重置
- **内联配额条**: 在渠道编排列表中显示配额使用情况
  - 显示剩余百分比
  - 颜色编码：绿色 (≥50%)、黄色 (20-50%)、红色 (<20%)
  - 悬停显示详细配额信息
  - 点击打开菜单可手动重置配额
- **配额 Tab**: 在渠道编辑对话框中新增 Quota 配置标签页

### 🐛 修复

- 修复配额自动重置逻辑错误导致配额被意外重置的问题
- 修复配额重置时间的时区显示问题

### 🔧 技术改进

- 新增 `internal/quota/usage_manager.go` 管理配额使用量追踪
- 新增 `internal/handlers/quota_handler.go` 提供配额 API
- 在 proxy.go 和 responses.go 中集成使用量追踪
- 配额数据持久化到 `quota_usage.json`

---

## [v1.3.106] - 2025-12-31

### ✨ 新功能

- **配额持久化**: Codex OAuth 渠道的配额数据现在持久化到 SQLite 数据库，服务重启后自动恢复配额状态
- **内联配额条**: 在 Codex 渠道编排列表中直接显示配额使用情况，无需点击菜单查看
  - 显示短期窗口剩余百分比
  - 颜色编码：绿色 (≥50%)、黄色 (20-50%)、红色 (<20%)
  - 悬停显示详细配额信息
  - 点击打开完整 OAuth 状态对话框

### 🔧 技术改进

- 新增 `channel_quota` 数据库表存储配额数据
- 新增 `quota.Persister` 接口和 `RequestLogAdapter` 适配器
- 配额更新时自动持久化（响应头解析、429 错误、手动清除）

---

## [v1.3.105] - 2025-12-30

### 🔒 安全加固

- **默认关闭 key-in-path 旧接口**: 默认不再注册 `/api/*/keys/:apiKey...` 路由，避免 API keys 出现在 URL（如需兼容旧客户端可临时设置 `ALLOW_INSECURE_DEPRECATED_KEY_PATH_ENDPOINTS=true`）
- **认证日志路径脱敏**: 认证/权限日志的 Path 对 `/keys/<apiKey>` 段做脱敏（即使启用旧接口也避免把 key 写入服务端日志）
- **Write-only keys 编辑修复**: 编辑渠道时不再通过 `PUT /api/*` 发送 `apiKeys`（会覆盖/清空服务端已有 keys），改为只更新配置字段并通过 `POST /keys` 追加新增 keys
- **UI Key 计数显示修复**: 渠道列表使用 `apiKeyCount` 显示 key 数量，避免 GET 不返回 `apiKeys` 导致显示 0

### 📄 配置与文档

- `backend-go/.env.example` 增加 `ALLOW_INSECURE_DEPRECATED_KEY_PATH_ENDPOINTS` 示例说明
- `docs/SECURITY_TODO.md` 更新 P2 说明为默认禁用旧接口

---

## [v1.3.104] - 2025-12-30

### 🔒 安全加固

#### 认证与访问控制

- **禁止默认访问密钥**: 服务器启动时检查 `PROXY_ACCESS_KEY`，禁止使用默认值 `your-proxy-access-key`（除非显式设置 `ALLOW_INSECURE_DEFAULT_KEY=true` 且为开发环境）
- **强制密钥长度**: `PROXY_ACCESS_KEY` 必须至少 16 个字符
- **管理 API 权限收紧**: 所有 `/api/*` 端点现在都需要 admin 权限（之前仅 `/api/keys` 需要）

#### 速率限制与暴力破解防护

- **服务端速率限制**: 实现 `RateLimitMiddleware`，使用 `ENABLE_RATE_LIMIT`、`RATE_LIMIT_WINDOW`、`RATE_LIMIT_MAX_REQUESTS` 配置
- **认证失败限制**: 新增 `AuthFailureRateLimiter`，阶梯式封禁：
  - 5 次失败 → 封禁 1 分钟
  - 10 次失败 → 封禁 5 分钟
  - 20 次失败 → 封禁 30 分钟
- **认证成功自动解封**: 成功认证后清除失败记录

#### 敏感信息保护

- **API 密钥脱敏**: `/api/channels` 和 `/api/responses/channels` 返回的 `apiKeys` 字段现在显示为掩码格式（如 `sk-ant-a***...`）
- **新增 `apiKeyCount` 字段**: 返回密钥数量，方便前端显示
- **DevInfo 端点脱敏**: `/admin/dev/info` 不再返回完整配置和环境变量，仅返回安全摘要
- **变更响应脱敏**: `AddUpstream`、`UpdateUpstream`、`DeleteUpstream` 不再返回 upstream 数据

### 📁 新增文件

- `backend-go/internal/middleware/ratelimit.go` - 速率限制中间件

---

## [v1.3.103] - 2025-12-28

### 🐛 修复

#### Logs 页面图表范围 & 成本图表稳定性

- **图表范围跟随日志日期筛选**：新增 `duration=period`（from/to）支持，图表默认展示筛选区间内的数据
- **成本图表不再闪现消失**：修复 ApexCharts `parser Error`（过滤非法 timestamp/数值，并避免全 0 区间导致 y 轴计算异常）
- **成本累计按 provider/channel 分线**：成本曲线按 provider/channel 展示累积 cost，基线从筛选区间起点计算
- **UI 细节**：调整图表头部留白，避免关闭按钮与按钮组重叠

---

## [v1.3.102] - 2025-12-28

### 🐛 修复

#### 图表悬停时 Tooltip 消失问题

- **Tooltip 配置优化**：添加 `shared: true` 和 `intersect: false` 配置
  - Tooltip 显示所有系列数据
  - 鼠标靠近数据点即可触发，无需精确悬停
- **悬停时暂停自动刷新**：当鼠标悬停在图表区域时暂停 2 秒自动刷新
  - 防止图表更新导致 Tooltip 消失
  - 移开鼠标后自动恢复刷新
- **影响组件**：GlobalStatsChart 和 ChannelStatsChart

---

## [v1.3.101] - 2025-12-28

### ✨ 新功能

#### 图表增强：缓存数据展示

- **Tokens 视图整合缓存数据**：在 "Tokens" 视图中同时展示 4 条曲线
  - 输入 Tokens（紫色实线）
  - 输出 Tokens（橙色实线）
  - 缓存创建 Cache Create（绿色虚线）
  - 缓存命中 Cache Hit（黄色虚线）
- **统计卡片新增缓存数据**：在图表上方的汇总卡片中增加：
  - 缓存创建 Tokens（绿色，与日志表格一致）
  - 缓存命中 Tokens（黄色，与日志表格一致）
- **GlobalStatsChart 与 ChannelStatsChart 保持一致**
- **国际化支持**：中英文翻译同步更新

---

## [v1.3.100] - 2025-12-28

### 🐛 修复

- **修复图表数据为空问题**：修复 `GetStatsHistory` 和 `GetChannelStatsHistory` 方法中的时间格式不匹配问题
  - SQLite 存储时间使用 Go 默认格式，但查询时错误使用了 RFC3339 格式
  - 改为直接传递 `time.Time` 对象，由驱动自动处理格式转换

---

## [v1.3.012] - 2025-12-27

### ✨ 新功能

#### 缓存命中率统计

- **新增 Hit% 列**：在请求日志汇总表和活跃会话表中新增缓存命中率列
- **计算公式**：`命中率 = 缓存读取 / (输入 + 缓存读取 + 缓存创建) × 100%`
- **悬停提示**：鼠标悬停在命中率数值上显示计算公式说明
- **多语言支持**：支持中英文显示

#### 汇总表排序功能

- **点击表头排序**：点击汇总表任意列标题可按该列排序
- **切换排序方向**：再次点击同一列切换升序/降序
- **排序指示器**：显示当前排序列的方向箭头（↑/↓）
- **默认排序**：默认按费用降序排列

---

## [v1.3.011] - 2025-12-25

### ✨ 新功能

#### OpenAI Chat 兼容渠道（仅 /v1/chat/completions）

- **新增 `openai_chat` 服务类型**：用于上游不支持 `/v1/messages`、仅支持 OpenAI Chat Completions 的场景
- **工具调用转换**：将 Claude tools 转为系统提示注入的 XML 协议（trigger + `<invoke>`），并在检测到工具调用时返回 `stop_reason: "tool_use"`
- **非流式支持**：支持 `stream: false` 请求（非 SSE）

### 🐛 修复

- **请求日志不再卡住**：修复 `openai_chat` 请求在 WebUI Logs 页面一直 pending 的问题
- **Token 统计可见**：为 `openai_chat` 请求补齐 usage/token 统计（上游缺失 usage 时使用估算）
- **思考标签泄漏**：修复响应中出现 `<antml\b:thinking>` 等标签的显示问题

---

## [v1.3.010] - 2025-12-17

### ✨ 新功能

#### 请求日志 API Key 追踪

- **API Key 列**：日志表格新增 API Key 列，显示每个请求使用的 API 密钥名称
- **Master Key 支持**：`.env` 中的 `PROXY_ACCESS_KEY` 显示为 "master"（警告色），数据库 API Key 显示为密钥名称（主色）
- **按 API Key 分组统计**：统计面板新增按 API Key 分组选项（钥匙图标），可查看各密钥的使用量和费用

---

## [v1.3.009] - 2025-12-17

### 🐛 修复

#### 日志页面统计表格布局优化

- **统计表格列宽自适应**：移除 `table-layout: fixed`，使统计表格（左侧和中间 v-card）的列宽可以像主日志表格一样自动调整
- **日期筛选面板对齐**：修复日期筛选 v-card 超出页面右边距的问题，调整 flex 布局使三个面板正确适应分隔条宽度

---

## [v1.3.008] - 2025-12-17

### ✨ 新功能

#### 日志表格列显示管理

在日志表格设置中新增列显示管理功能：

- **列可见性切换**：可单独显示/隐藏每一列
- **设置持久化**：列显示设置保存在 localStorage
- **显示全部按钮**：一键恢复所有列的显示
- **安全保护**：至少保留一列可见，防止全部隐藏

---

## [v1.3.007] - 2025-12-16

### 🔄 重构

#### 重命名 user_id 为 client_id

由于 API Key 现在用于标识用户，原来的 `user_id` 字段实际上标识的是客户端/机器，因此重命名为 `client_id`：

- **后端**：
  - 数据库字段 `user_id` 自动迁移为 `client_id`
  - API 响应中 `byUser` 改为 `byClient`
  - 请求日志结构体字段更新

- **前端**：
  - 日志表格列标题从 "User" 改为 "Client"
  - 分组选项从 "User ID" 改为 "Client ID"
  - 别名对话框标签更新
  - 中英文翻译同步更新

---

## [v1.3.006] - 2025-12-16

### ✨ 新功能

#### 多 API Key 管理 (Phase 1)

支持多个命名 API Key，实现团队使用场景：

- **API Key 管理**：
  - 创建/编辑/删除 API Key
  - 启用/禁用/撤销 Key 状态管理
  - Admin 与普通 Key 权限区分
  - Key 前缀显示 (sk-xxx...)，完整 Key 仅创建时显示一次

- **后端 API**：新增 `/api/keys` 端点
  - `GET /api/keys` - 获取所有 Key（需 Admin 权限）
  - `POST /api/keys` - 创建新 Key
  - `GET/PUT/DELETE /api/keys/:id` - 单个 Key 操作
  - `POST /api/keys/:id/enable|disable|revoke` - 状态管理

- **认证增强**：
  - SQLite 存储 Key（SHA-256 哈希）
  - 内存缓存加速验证
  - `PROXY_ACCESS_KEY` 作为 bootstrap admin（向后兼容）
  - 请求日志关联 API Key ID

- **前端界面**：
  - 新增 "Keys" 标签页（快捷键 3）
  - 完整的 CRUD 管理界面
  - 中英文国际化支持

- **SQLite 优化**：
  - 单连接模式避免锁竞争
  - busy_timeout 配置

---

## [v1.3.005] - 2025-12-16

### ✨ 新功能

#### 用户别名后端存储

将用户 ID 别名从浏览器 localStorage 迁移到后端 SQLite 存储，实现跨设备同步：

- **后端 API**：新增 `/api/aliases` 端点，支持 CRUD 操作
  - `GET /api/aliases` - 获取所有别名
  - `PUT /api/aliases/:userId` - 设置别名
  - `DELETE /api/aliases/:userId` - 删除别名
  - `POST /api/aliases/import` - 批量导入（用于迁移）
- **SQLite 存储**：`user_aliases` 表，支持唯一性约束
- **自动迁移**：首次加载时自动将 localStorage 中的别名迁移到后端
- **跨设备同步**：别名存储在服务器，可在不同浏览器/设备间同步
- **优雅降级**：后端不可用时回退到 localStorage

---

## [v1.3.004] - 2025-12-16

### 🐛 Bug 修复

#### OAuth 渠道编辑修复

- **编辑模式验证**：修复编辑 OAuth 渠道时 Update 按钮不可点击的问题
- **表单验证逻辑**：编辑模式下不再强制要求重新输入 OAuth tokens
- **卡片状态显示**：编辑模式下 OAuth 卡片不再显示 "Required" 标签

---

## [v1.3.003] - 2025-12-16

### 🎨 UI 改进

#### 日志表格 Reasoning Effort 图标

- **Gauge 图标系统**：将 Codex 请求的 reasoning effort 从 lightbulb 图标改为 4 级 gauge 图标
  - Low: `mdi-gauge-empty` (绿色)
  - Medium: `mdi-gauge-low` (蓝色)
  - High: `mdi-gauge` (橙色)
  - xHigh: `mdi-gauge-full` (红色)
- **图标尺寸**：20px，更易于识别
- **Tooltip 颜色**：根据 effort 级别显示对应颜色

---

## [v1.3.002] - 2025-12-16

### 🔧 维护

- **Claude CLI User-Agent 更新**：fallback User-Agent 从 `2.0.34` 更新到 `2.0.70`
- **CLAUDE.md 规则**：添加版本更新规则，commit 时必须更新 VERSION

---

## [v1.3.001] - 2025-12-16

### 🔧 OpenAI OAuth 头部优化

#### 请求头清理

- **移除多余头部**：移除 `Version`、`Openai-Beta`、`Connection` 头部（官方 CLI 不使用）
- **转发原始头部**：转发 `Conversation_id`、`Session_id`、`Originator` 从入站请求
- **智能 User-Agent**：转发原始 User-Agent（如果是 `codex_cli_rs/` 开头），否则使用 fallback

#### 日志优化

- **移除 Active Session 日志**：移除 requestlog manager 中的 debug 日志
- **禁用 GIN Logger**：禁用 GIN 默认 Logger 中间件，减少 API 请求日志噪音

#### 前端修复

- **OAuth 渠道编辑**：修复编辑 OAuth 渠道时必须重新粘贴 auth.json 的问题

---

## [v1.3.000] - 2025-12-16

### ✨ 新功能

#### OpenAI OAuth (Codex) 渠道支持

- **新增 `openai-oauth` 服务类型**：支持使用 ChatGPT Plus 订阅的 OAuth 认证
- **auth.json 导入**：用户可直接粘贴官方 Codex CLI 的 `~/.codex/auth.json` 内容
- **自动 Token 刷新**：OAuth token 过期时自动刷新并保存
- **固定 API 端点**：OAuth 渠道使用固定的 `chatgpt.com/backend-api/codex/responses` 端点

---

## [v1.2.100] - 2025-12-16

### 🎨 UI 改进

#### 渠道管理页面简化

- **移除刷新按钮**：渠道列表已有 2 秒自动刷新，手动刷新按钮冗余
- **系统状态卡片占位**：原状态卡片为静态装饰（无真实健康检查），改为占位符待后续使用

### 📦 版本管理

- **3 位补丁版本号**：从 `1.2.3` 改为 `1.2.100` 格式，为频繁更新预留空间

---

## [v1.2.3] - 2025-12-16

### ✨ 新功能

#### 用户ID别名系统

为日志页面添加用户ID别名功能，便于识别和追踪不同用户：

- **快速设置**：点击表格中的用户ID即可弹出别名设置对话框
- **别名显示**：设置别名后，表格和统计面板显示别名而非长ID
- **悬停查看**：鼠标悬停显示别名和完整用户ID
- **唯一性验证**：别名必须唯一，不允许重复
- **设置管理**：在设置对话框中可查看、编辑、删除所有别名
- **本地存储**：别名存储在浏览器 localStorage 中

### 🐛 Bug 修复

- **TypeScript 类型修复**：修复 v-data-table headers align 属性类型推断问题

---

## [v1.2.2] - 2025-12-16

### 🎨 UI 改进

#### 日志表格优化

- **Duration 列右对齐**：Duration 列改为右对齐，数字对齐更整齐
- **简化 Duration 显示**：移除 v-chip 包装，改用简单文本显示，保留颜色编码（绿/黄/红）
- **数字与单位间距**：Duration 显示改为 `8213 ms` 格式，数字与单位之间添加空格

#### 活跃会话面板

- **移除旋转指示器**：Active Sessions 图标移除旋转加载圈，简化视觉效果

---

## [v1.2.1] - 2025-12-16

### 🎨 UI 改进

#### 日志页面统计面板优化

- **图标按钮替代下拉菜单**：将分组选择器从下拉菜单改为图标按钮，减少点击次数
  - Provider (云图标) - 默认选中
  - Model (AI 头脑图标)
  - User (用户图标)
  - Session (聊天图标)
- **活跃会话图标**：将"Active Sessions"文字标题改为图标，活跃时显示旋转加载指示器
- **面板对齐**：修复统计面板和活跃会话面板的表头对齐问题

### 🐛 Bug 修复

- **Tooltip 颜色**：修复 Tooltip 在浅色/深色模式下文字颜色不可见的问题，统一使用深色背景白色文字

---

## [v1.2.0] - 2025-12-16

### ✨ 新功能

#### 活跃会话面板

在日志页面新增实时活跃会话面板，显示最近 30 分钟内有活动的会话：

- **会话信息展示**：会话图标（Claude/Codex）、会话 ID、存活时长、请求数、Token 统计、费用
- **存活时长格式**：`Xm:Xs`（如 `102m:36s`），无小时单位
- **实时刷新**：自动刷新，更新时显示闪烁动画
- **可调整列宽**：列宽可拖拽调整，持久化到 localStorage

**后端**：
- 新增 `ActiveSession` 类型和 `GetActiveSessions()` 查询方法
- 新增 `GET /api/logs/sessions/active` 端点
- 修复 Go 内部时间格式的日期解析问题

**前端**：
- 新增 `ActiveSession` 接口和 `getActiveSessions()` API 方法
- 新增活跃会话表格，支持可调整列宽
- 新增会话更新时的闪烁动画
- 新增 i18n 翻译（en/zh-CN）

---

## [v1.1.91] - 2025-12-15

### 🎨 UI 改进

#### 可调整面板分隔器

- **灵活布局**：将日志页面顶部三个面板从固定 v-row/v-col 布局改为 flex 容器
- **可拖拽分隔器**：面板之间添加可拖拽的分隔器，自由调整宽度比例
- **持久化**：面板宽度比例保存到 localStorage
- **响应式**：移动端自动垂直堆叠显示

---

## [v1.1.9] - 2025-12-15

### 🔧 维护

- 版本号更新

---

## [v1.1.8] - 2025-12-14

### ✨ 新功能

#### 请求日志用户/会话追踪

- **用户 ID 解析**：解析 Claude Code 复合 user_id 为独立的 user_id 和 session_id
- **数据库扩展**：新增 session_id 列和索引
- **统计分组**：新增按用户（byUser）和按会话（bySession）的统计分组
- **筛选支持**：支持按用户 ID 和会话 ID 筛选日志
- **UI 改进**：优化 ID 显示格式，添加 tooltip 显示完整 ID
- **数据迁移工具**：新增 dbtool 用于回填历史记录

### 🐛 Bug 修复

- **Codex 会话追踪**：修复 Codex 的 `prompt_cache_key` 现在正确记录为 session_id

---

## [v1.1.7] - 2025-12-14

### ✨ 新功能

#### 增强日期筛选器

- **单位选择器**：新增日/周/月单位选择器
- **日期显示重设计**：改为三行显示（年份、月份带导航、日期范围）
- **导航适配**：根据单位自动调整导航步进（按天/按周/按月）

### 🎨 UI 改进

- **简化统计表格**：移除统计表格的卡片标题，界面更简洁
- **清理**：删除 PROJECT_REVIEW.md 文件

---

## [v1.1.6] - 2025-12-13

### 🎨 UX 改进

- **备份与恢复功能**：新增服务器端配置备份系统，支持创建、恢复、删除备份（设置菜单 → 备份与恢复）
- **键盘快捷键**：按 `1/2/3` 切换 Claude/Codex/Logs 标签页，按 `Esc` 关闭对话框
- **移动端优化**：
  - 日志页面统计表格在移动端垂直堆叠显示
  - 所有按钮增加触摸友好的最小尺寸 (44px)
  - 日期筛选器在移动端改为水平布局
- **UI 一致性**：设置菜单和对话框标题统一使用复古像素主题字体

### 🐛 Bug 修复

- **删除确认对话框**：删除渠道和 API 密钥现在使用 Vuetify 对话框替代浏览器原生 confirm()

---

## [v1.1.4] - 2025-12-13

### 🔒 安全加固

- **修复时序攻击漏洞**：API 密钥验证改用 `crypto/subtle.ConstantTimeCompare`，防止通过响应时间差推断密钥
- **启用默认速率限制**：`ENABLE_RATE_LIMIT` 默认值改为 `true`（100 请求/分钟），防止暴力破解
- **收紧 CORS 默认配置**：`CORS_ORIGIN` 默认值从 `*` 改为空字符串，阻止跨站请求
- **添加安全响应头**：Web UI 响应现在包含 `X-Content-Type-Options`、`X-Frame-Options`、`X-XSS-Protection`、`Referrer-Policy`
- **InsecureSkipVerify 警告**：启用 TLS 证书跳过验证的渠道会在启动时打印安全警告日志

### 🐛 Bug 修复

- **修复渠道 ID 解析错误**：`MoveApiKeyToTop/Bottom` 等接口现在正确处理无效 ID 参数，返回 400 错误而非静默失败

---

## [v1.1.3] - 2025-12-13

### 🔒 安全加固

- **修复管理端点未认证问题**：`/admin/config/reload` 现在需要访问密钥认证
- **修复开发端点安全风险**：`/admin/dev/info` 在开发模式下也需要访问密钥
- **移除 Query 参数认证**：后端不再接受 `?key=` 查询参数传递访问密钥，仅支持请求头认证
- **前端密钥存储优化**：访问密钥从 `localStorage` 迁移到 `sessionStorage`，并移除 URL `?key=` 导入功能

### 🛡️ 稳定性改进

- **添加请求体大小限制**：代理端点新增可配置的请求体大小限制（`MAX_REQUEST_BODY_MB`，默认 20MB），超限返回 `413`
- **增大 SSE 流解析缓冲区**：Provider 流式解析器的 `bufio.Scanner` 缓冲区从默认 64KB 增大到 4MB，修复大型 SSE 行（如工具调用）导致的 "token too long" 错误
- **修复 Responses 会话链接错误**：`previous_id` 现在正确链接到前一个响应，不再错误指向当前响应
- **修复 Token 统计重复计算**：Responses API 的 token 使用量现在每个响应只计算一次，不再按输出项重复累加

### 🔧 CORS 配置改进

- **严格化 CORS 行为**：CORS 现在由 `ENABLE_CORS` 和 `CORS_ORIGIN` 严格控制
- **修复开发模式 origin 匹配**：移除不安全的 `strings.Contains(origin, "localhost")` 子串匹配

### 📝 文档更新

- **环境变量文档**：未实现的环境变量（如 rate limit）现在标注为 "(planning)" 避免误导

---

## [v1.1.2] - 2025-12-12

### 🐛 Bug 修复

- **修复 responseHeaderTimeout 配置**：非流式请求现在也会应用渠道配置的超时时间（此前仅对流式请求生效）
- **修复请求日志 Tooltip 颜色**：深色背景上的 tooltip 文字现在可见

### 🎨 UI 改进

- **标题更新**：UI 标题从 "Claude API Proxy" 更新为 "CC-Bridge"
- **表格动画**：为统计表格（按模型/按渠道）添加行闪烁动画效果

---

## [v1.1.1] - 2025-12-12

### 🌐 国际化支持

- **i18n 框架**：新增 vue-i18n v11.2.2 国际化支持
- **语言切换**：支持 English / 简体中文，语言偏好持久化到 localStorage
- **新增文件**：`src/locales/`, `src/plugins/i18n.ts`, `src/composables/useLocale.ts`

### 🎨 UI 改进

- **操作按钮**：更大的操作按钮尺寸，提升可点击性
- **定价对话框**：更宽的定价设置对话框

---

## [v1.1.0] - 2025-12-12

### 🔄 项目重命名

- **项目名称**：从 `claude-proxy` 重命名为 `cc-bridge`
- **Go 模块路径**：`github.com/JillVernus/cc-bridge`
- **Fork 来源**：基于上游 [BenedictKing/claude-proxy v2.0.44](https://github.com/BenedictKing/claude-proxy/tree/v2.0.44) 分叉

---

## [v2.0.20-go] - 2025-12-08

### 🐛 Bug 修复

- **修复单渠道模式渠道选择逻辑**：`disabled` 状态的渠道不再被错误选中，现在优先选择第一个 `active` 渠道

### 🧹 代码清理

- 移除废弃的 `currentUpstream` 相关代码和 API 接口

---

## [v2.0.11-go] - 2025-12-06

### 🚀 重大功能

#### 多渠道智能调度器

新增完整的多渠道调度系统，支持智能故障转移和负载均衡：

**核心模块**：

- **ChannelScheduler** (`internal/scheduler/channel_scheduler.go`)
  - 基于优先级的渠道选择
  - Trace 亲和性支持（同一用户会话绑定到同一渠道）
  - 失败率检测和自动熔断
  - 降级选择（选择失败率最低的渠道）

- **MetricsManager** (`internal/metrics/channel_metrics.go`)
  - 滑动窗口算法计算实时成功率
  - 可配置窗口大小（默认 10 次请求）
  - 可配置失败率阈值（默认 50%）
  - 自动熔断和恢复机制
  - 熔断自动恢复（默认 15 分钟后自动尝试恢复）
  - 熔断时间戳记录（`circuitBrokenAt` 字段）

- **TraceAffinityManager** (`internal/session/trace_affinity.go`)
  - 用户会话与渠道绑定
  - TTL 自动过期（默认 30 分钟）
  - 定期清理过期记录

**调度优先级**：
1. Trace 亲和性（优先使用用户之前成功的渠道）
2. 健康检查（跳过失败率过高的渠道）
3. 优先级顺序（数字越小优先级越高）
4. 降级选择（所有渠道都不健康时选择最佳的）

#### 渠道状态管理

新增渠道状态字段，支持三种状态：

| 状态 | 说明 |
|------|------|
| `active` | 正常运行，参与调度 |
| `suspended` | 暂停状态，保留在故障转移序列但跳过 |
| `disabled` | 备用池，不参与调度 |

> ⚠️ **注意**：`suspended` 是配置层面的状态，需手动恢复；运行时熔断会在 15 分钟后自动恢复。

**配置字段扩展**：
- `priority` - 渠道优先级（数字越小优先级越高）
- `status` - 渠道状态（active/suspended/disabled）

**向后兼容**：
- 旧配置文件自动迁移到新格式
- `currentUpstream` 字段自动转换为 status 状态

#### 渠道密钥自检

- 启动时自动检测无 API Key 的渠道
- 无 Key 渠道自动设置为 `suspended` 状态
- 防止因配置错误导致请求失败

### 🎨 前端 UI

#### 渠道编排面板

新增 `ChannelOrchestration.vue` 组件：

- **拖拽排序**: 通过拖拽调整渠道优先级，自动保存
- **实时指标**: 显示成功率、请求数、延迟等指标
- **状态切换**: 一键切换 active/suspended/disabled 状态
- **备用池管理**: 独立管理备用渠道
- **多渠道/单渠道模式**: 自动检测并显示当前模式

#### 渠道状态徽章

新增 `ChannelStatusBadge.vue` 组件：

- 实时显示渠道健康状态
- 颜色编码：绿色（健康）、黄色（警告）、红色（熔断）
- 悬停显示详细指标

#### 响应式 UI 优化

- 移动端适配优化
- 复古像素主题增强
- 暗色模式操作栏背景色适配

### 🔧 技术改进

#### API 端点

- `GET /api/channels/metrics` - 获取 Messages 渠道指标
- `GET /api/responses/channels/metrics` - 获取 Responses 渠道指标
- `POST /api/channels/:id/resume` - 恢复熔断渠道
- `POST /api/responses/channels/:id/resume` - 恢复 Responses 熔断渠道
- `GET /api/scheduler/stats` - 获取调度器统计信息（含熔断恢复时间）
- `PATCH /api/channels/:id` - 更新渠道配置（支持 priority/status）
- `PATCH /api/channels/order` - 批量更新渠道优先级顺序

#### CORS 增强

- 支持 PATCH 方法
- OPTIONS 预检请求返回 204

#### 代理目标配置

- 新增 `VITE_PROXY_TARGET` 环境变量
- 前端开发时可配置后端代理目标

### 📝 技术细节

**新增模块**：

| 模块 | 路径 | 职责 |
|------|------|------|
| **调度器** | `internal/scheduler/` | 多渠道调度逻辑 |
| **指标** | `internal/metrics/` | 渠道健康度指标 |
| **亲和性** | `internal/session/trace_affinity.go` | 用户会话亲和 |

**架构图**：

```
请求 → 调度器选择渠道 → 执行请求 → 记录指标
           ↓                          ↓
     Trace亲和检查              成功/失败统计
           ↓                          ↓
     健康度检查                 滑动窗口更新
           ↓                          ↓
     优先级排序                 熔断判断
```

---

## [v2.0.10-go] - 2025-12-06

### 🎨 UI 重构

#### 复古像素 (Retro Pixel) 主题

采用 **Neo-Brutalism** 设计语言，完全重构前端样式：

- **无圆角**: 全局 `border-radius: 0`
- **等宽字体**: `Courier New`, `Consolas`, `Liberation Mono`
- **粗实体边框**: `2px solid` 黑色/白色边框
- **硬阴影**: `box-shadow: Npx Npx 0 0` 偏移阴影（无模糊）
- **按压交互**: hover 上浮 + active 按压效果
- **高对比度状态标签**: 实心背景 + 实体边框
- **复古纸张背景**: 亮色模式使用 `#fffbeb`

### 🔧 技术变更

- 移除 DaisyUI 依赖
- 移除玻璃拟态 (Glassmorphism) 效果
- 简化主题配置 (`useTheme.ts`)

---

## [v2.0.9-go] - 2025-12-04

### ✨ 新功能

- 新增 API 密钥排序功能：支持将最后一个密钥置顶、第一个密钥置底
- 前端 API 密钥列表显示置顶/置底按钮（仅当密钥数量 > 1 时）

---

## [v2.0.8-go] - 2025-12-04

### 🐛 Bug 修复

- 修复 429 速率限制错误不触发密钥切换的问题
- 新增中文错误消息 "请求数限制" 的识别支持

---

## [v2.0.7-go] - 2025-11-22

### ✨ 改进

- Codex Responses 负载均衡独立配置：新增 `responsesLoadBalance` 字段和 `/api/responses/loadbalance` 路由，前端在 Codex 标签页单独设置策略，不再影响 Claude 渠道。
- 置顶状态分离：Codex 管理页置顶改用 `codex-proxy-pinned-channels`，不再与 Claude 共享 localStorage。

### 🔧 兼容性

- 旧配置文件若未包含 `responsesLoadBalance` 将自动回退到现有 `loadBalance`，无需手工迁移。

---

## [v2.0.6-go] - 2025-11-18

### 🐛 Bug 修复

#### Responses API 透传模式修复

- **问题**: 透传模式下字段丢失和零值字段污染
  - ❌ 原始请求中的高级字段丢失（`tools`, `tool_choice`, `reasoning`, `metadata`, `betas`）
  - ❌ 实际请求中添加了不存在的零值字段（`frequency_penalty: 0`, `temperature: 0`, `max_tokens: 0`）
  - ❌ 导致上游 API 返回参数错误

- **根因**: `ResponsesPassthroughConverter` 通过 Go 结构体字段映射，而非真正的 JSON 透传
  - 结构体定义不完整，缺少高级字段定义
  - 所有结构体字段都被序列化，包括零值字段

- **修复方案** (`internal/providers/responses.go`)
  - ✅ 透传模式下使用 `map[string]interface{}` 解析原始请求
  - ✅ 保留所有原始字段，不经过结构体映射
  - ✅ 不添加任何零值字段
  - ✅ 只执行必要的模型重定向
  - ✅ 非透传模式保持原有逻辑（结构体 + 会话管理 + 转换器）

- **影响范围**: 仅影响 `serviceType: "responses"` 的上游配置

#### 日志显示优化

- **问题**: Responses API 的 `input`/`output` 字段内容在日志中被简化
  - ❌ 只显示 `{"type": "input_text"}`，实际文本内容丢失
  - ❌ 无法通过日志调试消息内容

- **根因**: `utils/json.go` 的日志格式化函数遗漏了 Responses API 特有类型
  - `compactContentArray()` 只处理 Messages API 类型（`text`, `tool_use`, `tool_result`, `image`）
  - 没有处理 Responses API 的 `input_text` 和 `output_text` 类型

- **修复方案** (`internal/utils/json.go`)
  - ✅ 在 `compactContentArray()` switch 语句中添加 `input_text`/`output_text` case
  - ✅ 保留 `text` 字段内容（超过 200 字符自动截断）
  - ✅ 在 `formatJSONWithCompactArrays()` 中添加类型识别

- **影响范围**: 所有使用 `FormatJSONBytesForLog()` 的日志输出

### 🎯 修复效果

**透传模式**：
```diff
# 修复前
- "tools": 字段丢失
- "reasoning": 字段丢失
+ "frequency_penalty": 0  # 不应添加
+ "temperature": 0        # 不应添加

# 修复后
+ "tools": [...]          # 完整保留
+ "reasoning": {...}      # 完整保留
- 不添加零值字段
```

**日志显示**：
```diff
# 修复前
"input": [{"type": "input_text"}]

# 修复后
"input": [{"type": "input_text", "text": "完整的消息内容..."}]
```

### 📝 技术细节

- **文件修改**:
  - `backend-go/internal/providers/responses.go` (第 31-85 行)
  - `backend-go/internal/utils/json.go` (第 112-120, 369-370 行)

- **符合原则**:
  - ✅ KISS - 透传使用 map，不过度设计
  - ✅ DRY - 复用现有转换器工厂和类型判断
  - ✅ YAGNI - 最小改动，不影响其他模块

---

## [v2.0.5-go] - 2025-11-15

### 🚀 重大重构

#### Responses API 转换器架构重构

- **新增转换器接口** (`internal/converters/converter.go`)
  - 定义统一的 `ResponsesConverter` 接口
  - 支持双向转换：Responses ↔ 上游格式
  - 清晰的职责分离和扩展性

- **策略模式 + 工厂模式实现**
  - `OpenAIChatConverter` - Responses → OpenAI Chat Completions
  - `OpenAICompletionsConverter` - Responses → OpenAI Completions
  - `ClaudeConverter` - Responses → Claude Messages API
  - `ResponsesPassthroughConverter` - Responses → Responses (透传)
  - `ConverterFactory` - 根据上游类型自动选择转换器

- **完整支持 Responses API 标准格式**
  - ✅ `instructions` 字段 - 映射为 system message
  - ✅ 嵌套 `content` 数组 - 支持 `input_text`/`output_text` 类型
  - ✅ `type: "message"` 格式 - 区分 message 和 text 类型
  - ✅ `role` 字段 - 直接从 item.role 获取角色
  - ❌ 移除 `[ASSISTANT]` 前缀 hack - 使用标准 role 字段

### ✨ 新功能

- **内容提取函数** (`extractTextFromContent`)
  - 支持三种格式：string、[]ContentBlock、[]interface{}
  - 自动提取 input_text 和 output_text 类型
  - 智能拼接多个文本块

- **类型定义增强**
  - `ResponsesRequest.Instructions` - 系统指令字段
  - `ResponsesItem.Role` - 角色字段（user/assistant）
  - `ContentBlock` - 内容块结构体（type + text）

### 🔧 代码改进

- **ResponsesProvider 简化**
  - 使用工厂模式替代 switch-case
  - 统一的请求转换流程
  - 减少代码重复（从 ~260 行减少到 ~130 行）

- **测试覆盖**
  - 10 个单元测试全部通过
  - 覆盖核心转换逻辑
  - 测试 instructions、message type、会话历史等场景

### 📚 架构优势

- **易于扩展** - 新增上游只需实现 ResponsesConverter 接口
- **职责清晰** - 转换逻辑与 Provider 解耦
- **可测试性** - 每个转换器可独立测试
- **代码复用** - 公共逻辑提取到基础函数

### ⚠️ 破坏性变更

- **移除向后兼容** - 不再支持 `[ASSISTANT]` 前缀
- **函数签名变更**
  - `ResponsesToClaudeMessages` 新增 `instructions` 参数
  - `ResponsesToOpenAIChatMessages` 新增 `instructions` 参数

### 📖 参考

本次重构参考了 [AIClient-2-API](https://github.com/example/AIClient-2-API) 项目的转换策略设计，特别是：
- Responses API 格式的完整实现
- 策略模式 + 工厂模式的架构设计
- instructions → system message 的映射逻辑

---

## [v2.0.4-go] - 2025-11-14

### ✨ 新功能

#### Responses API 透明转发支持

- **Codex Responses API 端点** (`/v1/responses`)
  - 完整支持 Codex Responses API 格式
  - 透明转发到上游 Responses API 服务
  - 支持流式和非流式响应
  - 自动协议转换和错误处理
  - 与 Messages API 相同的负载均衡和故障转移机制

- **会话管理系统** (`internal/session/`)
  - 自动会话创建和多轮对话跟踪
  - 基于 `previous_response_id` 的会话关联
  - 消息历史自动管理（默认限制 100 条消息）
  - Token 使用统计（默认限制 100k tokens）
  - 自动过期清理机制（默认 24 小时）
  - 线程安全的并发访问支持

- **Responses Provider** (`internal/providers/responses.go`)
  - 实现 Responses API 协议转换
  - 支持 `input` 字段（字符串或数组格式）
  - 响应包含 `id` 和 `previous_id` 链接
  - 自动处理 `store` 参数控制会话存储
  - 完整的流式响应支持

- **独立渠道管理**
  - Responses 渠道与 Messages 渠道完全独立
  - 独立的渠道配置和 API 密钥管理
  - 支持通过 Web UI 和管理 API 配置
  - 独立的负载均衡策略

#### Messages API 协议转换增强

- **多上游协议支持**
  - Claude API (Anthropic) - 原生支持，直接透传
  - OpenAI API - 自动双向转换 (Claude ↔ OpenAI 格式)
  - OpenAI 兼容 API - 支持所有 OpenAI 格式兼容服务
  - Gemini API (Google) - 自动双向转换 (Claude ↔ Gemini 格式)

- **统一客户端接口**
  - 客户端只需使用 Claude Messages API 格式
  - 代理自动识别上游类型并转换协议
  - 无需修改客户端代码即可切换不同 AI 服务
  - 支持灵活的成本优化和服务切换

#### Web UI 标题栏 API 类型切换

- **集成式 API 类型切换器**
  - 在标题栏中显示 `Claude / Codex API Proxy` 格式
  - 点击 "Claude" 切换到 Messages API 渠道
  - 点击 "Codex" 切换到 Responses API 渠道
  - 移除了独立的 Tab 切换卡片，节省垂直空间

- **视觉高亮设计**
  - 激活选项显示下划线高亮效果
  - 激活选项字体加粗（font-weight: 900）
  - 未激活选项降低透明度（opacity: 0.55）
  - 悬停时透明度提升并轻微上浮动画

- **统一数据管理**
  - 自动同步切换所有统计卡片数据
  - 当前渠道、负载均衡策略、渠道列表随 Tab 切换更新
  - 保持用户操作的连贯性

### 🎨 UI/UX 优化

- **空间利用优化**
  - 移除独立 Tab 卡片，UI 更紧凑
  - 标题栏集成切换功能，减少视觉干扰
  - 提升页面内容展示空间

- **交互体验提升**
  - 平滑过渡动画（0.18s ease）
  - 悬停反馈（透明度 + 位移）
  - 清晰的视觉状态反馈

### 📝 技术改进

- **架构增强**
  - 新增 Session Manager 模块支持有状态会话
  - Responses Handler 实现完整的请求/响应生命周期
  - ResponsesProvider 遵循统一的 Provider 接口规范
  - 所有 Responses 相关功能均支持故障转移和密钥降级

- **代码简化**
  - 移除多余的 Tab 组件代码
  - 简化 CSS 样式，仅保留必要的下划线高亮风格
  - 提升代码可维护性

- **响应式设计**
  - 支持移动端和桌面端自适应
  - 字体大小根据屏幕尺寸调整（text-h6/text-h5）
  - 保持在不同设备上的良好体验

- **API 端点扩展**
  - `/v1/responses` - Responses API 主端点
  - `/api/responses/channels` - Responses 渠道管理
  - `/api/responses/channels/:id/keys` - Responses 密钥管理
  - `/api/responses/channels/:id/current` - 设置当前 Responses 渠道

### 🔧 其他改进

- **版本管理**
  - 统一版本号至 v2.0.4-go
  - 更新 VERSION 文件和 package.json
  - 完善更新日志文档

---

## [v2.0.3-go] - 2025-10-13

### 🐛 Bug 修复

#### 流式响应文本块管理优化

- **修复 OpenAI/Gemini 流式响应文本块状态追踪** (`openai.go`, `gemini.go`)
  - 引入 `textBlockStarted` 状态标志，确保文本块正确开启/关闭
  - 修复连续文本片段导致多个 `content_block_start` 事件的问题
  - 确保在工具调用或流结束前正确关闭文本块
  - 改进 `content_block_stop` 事件的发送时机和条件判断

- **增强 Gemini Provider 的流式事件序列**:
  - 首个文本块才发送 `content_block_start` 事件
  - 后续文本增量统一使用 `content_block_delta` 事件
  - 工具调用前自动关闭未完成的文本块
  - 流结束时确保所有文本块已关闭

- **改进 OpenAI Provider 的事件同步**:
  - 统一文本块和工具调用的事件序列管理
  - 修复工具调用和文本内容交错时的状态混乱
  - 删除冗余的 `processTextPart` 辅助函数（90行代码减少）

#### 请求头处理优化

- **新增 `PrepareMinimalHeaders` 函数** (`headers.go`)
  - 针对非 Claude 类型渠道（OpenAI、Gemini）使用最小化请求头
  - 避免转发 Anthropic 特定头部（如 `anthropic-version`）导致上游拒绝请求
  - 仅保留必要头部：`Host` 和 `Content-Type`
  - 不显式设置 `Accept-Encoding`，由 Go 的 `http.Client` 自动处理 gzip 压缩

- **区分 Claude 和非 Claude 渠道的头部策略**:
  - **Claude 渠道**: 使用 `PrepareUpstreamHeaders`（保留原始请求头）
  - **OpenAI/Gemini 渠道**: 使用 `PrepareMinimalHeaders`（最小化头部）
  - 提升与不同上游 API 的兼容性

#### OpenAI URL 路径智能拼接

- **自动检测 baseURL 版本号后缀** (`openai.go`)
  - 使用正则表达式 `/v\d$` 检测 URL 是否已包含版本号
  - 已包含版本号（如 `/v1`、`/v2`）时直接拼接 `/chat/completions`
  - 未包含版本号时自动添加 `/v1/chat/completions`
  - 支持自定义上游 API 的灵活配置

#### 日志格式优化

- **简化流式响应日志输出** (`proxy.go`)
  - 移除多余的 `---` 分隔符，减少日志噪音
  - 统一日志格式：`🛰️ 上游流式响应合成内容:\n{content}`
  - 减少视觉干扰，提升日志可读性

- **区分客户端断开和真实错误**:
  - 检测 `broken pipe` 和 `connection reset` 错误
  - 客户端中断连接使用 `ℹ️` info 级别日志
  - 其他错误使用 `⚠️` warning 级别日志
  - 仅在 info 日志级别启用时输出客户端断开信息

### 📝 技术改进

- **代码简化**:
  - 删除 OpenAI Provider 中的 `processTextPart` 辅助函数（45行）
  - 状态管理从函数式转为声明式，提升可维护性
  - 减少重复代码，遵循 DRY 原则

- **错误处理增强**:
  - 流式传输错误分级处理（client vs server error）
  - 改进错误日志的上下文信息
  - 在开发模式下提供更详细的调试信息

### ⚡ 性能优化

- **减少不必要的函数调用**:
  - 文本块事件生成从函数调用改为内联代码
  - 减少 JSON 序列化次数
  - 降低 CPU 和内存开销

- **优化请求头处理**:
  - 最小化头部策略减少请求体大小
  - 避免转发无关头部提升网络效率

---

## [v2.0.2-go] - 2025-10-12

### ✨ 新功能

#### API密钥复制功能
- **一键复制密钥**: 在渠道卡片和编辑弹框中为每个API密钥添加复制按钮
  - 视觉反馈：复制成功后显示绿色勾选图标，2秒后自动恢复
  - 工具提示：鼠标悬停显示"复制密钥"，复制后显示"已复制!"
  - 兼容性：支持现代浏览器的 Clipboard API，自动降级到传统方法
  - 位置：
    - 渠道卡片：展开"API密钥管理"面板中每个密钥右侧
    - 编辑弹框：编辑/添加渠道对话框的"API密钥管理"区域

#### 前端认证优化
- **自动登录功能**: 保存的访问密钥自动验证登录
  - 首次访问：输入密钥后自动保存到本地存储
  - 后续访问：页面刷新时自动验证密钥并直接进入系统
  - 密钥失效：自动检测并提示用户重新输入
  - 加载提示：显示"正在验证访问权限"加载遮罩，提升用户体验

- **移除后端内置登录页面**: 统一由前端Vue应用处理认证
  - 删除Go后端的HTML登录页面（`getAuthPage()`函数）
  - 优化认证中间件：页面请求直接提供Vue应用，API请求才检查密钥
  - 解决双重登录对话框问题，提升用户体验

### 🎨 UI/UX 优化

- **统一视觉风格**: 复制和删除按钮在两处位置保持一致的布局和交互
- **智能状态管理**: 复制状态独立管理，不干扰其他功能
- **密钥掩码显示**: 保持密钥的安全性，只在复制时使用完整密钥

### 🐛 Bug 修复

- **修复双重登录框问题**:
  - 后端不再返回简单的HTML登录页面
  - 前端Vue应用完全接管认证流程
  - 页面加载时不会出现登录框闪烁

- **修复初始化时序问题**:
  - 添加 `isInitialized` 标志控制对话框显示时机
  - 优化自动认证的异步处理逻辑

### 📝 技术改进

- **前端状态管理优化**:
  - 添加 `copiedKeyIndex` 响应式状态追踪复制状态
  - 添加 `isAutoAuthenticating` 和 `isInitialized` 标志管理认证流程

- **剪贴板API降级方案**:
  - 优先使用 `navigator.clipboard.writeText()`
  - 自动降级到 `document.execCommand('copy')`
  - 确保所有浏览器环境都能正常工作

---

## [v2.0.1-go] - 2025-10-12

### 🐛 重要修复

#### 前端资源加载问题修复

- **修复 Vite base 路径配置** (`vite.config.ts`)
  - 添加 `base: '/'` 配置，使用绝对路径适配 Go 嵌入式部署
  - 修复前端资源加载失败问题（"Expected a JavaScript module but got text/html"）
  - 优化构建配置，添加代码分割（vue-vendor, mdi-icons）

- **修复 NoRoute 处理器逻辑** (`frontend.go`)
  - 智能文件服务：先尝试读取实际文件，不存在才返回 index.html
  - 添加 `getContentType()` 函数，正确设置各类资源的 MIME 类型
  - 支持 .html, .css, .js, .json, .svg, .ico, .woff, .woff2 等文件类型
  - 修复 `/favicon.ico` 等静态资源返回 HTML 的问题
  - **添加 API 路由优先处理**：新增 `isAPIPath()` 函数检测 `/v1/`, `/api/`, `/admin/` 前缀，对不存在的 API 端点返回 JSON 格式 404 错误而非 HTML

- **添加 favicon 支持**
  - 创建 `frontend/public/` 目录
  - 添加 SVG 格式的 favicon（轻量、矢量、支持主题）
  - 自动复制到构建产物中

#### API 路由兼容性修复

- **统一前后端 API 路由** (`main.go`)
  - 修改 `/api/upstreams` → `/api/channels`（与前端保持一致）
  - 添加缺失的 handler 函数：
    - `UpdateLoadBalance` - 更新负载均衡策略
    - `PingChannel` - 单个渠道延迟测试
    - `PingAllChannels` - 批量延迟测试
  - 修复 `DeleteApiKey` 支持 URL 路径参数
  - 优化 `GetUpstreams` 返回格式（包含 channels, current, loadBalance）

#### 环境变量优化

- **ENV 变量标准化** (`env.go`, `.env.example`)
  - `NODE_ENV` → `ENV`（更通用的命名）
  - 保持向后兼容（优先读取 `ENV`，回退到 `NODE_ENV`）
  - 添加详细的配置影响说明文档

#### 版本注入修复

- **Makefile 版本信息注入** (`Makefile`)
  - 修复 `make run`、`make dev`、`make dev-backend` 缺少 `-ldflags` 参数
  - 确保运行时显示正确的版本号、构建时间和 Git commit

### ⚡ 性能优化

#### 前端构建缓存机制

- **智能缓存系统** (`Makefile`)
  - 添加 `.build-marker` 标记文件追踪构建状态
  - 自动检测 `frontend/src` 目录文件变更
  - 未变更时跳过编译，**启动速度提升 142 倍**（10秒 → 0.07秒）
  - 新增 `ensure-frontend-built` 目标实现智能构建逻辑

- **缓存性能对比**:
  | 场景 | 之前 | 现在 | 提升 |
  |------|------|------|------|
  | 首次构建 | ~10秒 | ~10秒 | 无变化 |
  | **无变更重启** | ~10秒 | **0.07秒** | **142倍** 🚀 |
  | 有变更重新构建 | ~10秒 | ~8.5秒 | 15%提升 |

### 📝 文档更新

- **README.md 更新**
  - 添加智能缓存机制说明
  - 添加 ENV 环境变量影响详解
  - 更新开发流程最佳实践
  - 添加缓存命令使用说明

- **前端构建优化文档**
  - 说明 Makefile 缓存原理
  - 提供典型开发场景示例
  - Bun vs npm 对比说明

### 🔧 技术改进

- **代码分割优化**
  - 分离 vue-vendor (137KB) 和 mdi-icons 模块
  - 移除无法分割的 @mdi/font 依赖
  - 优化首屏加载性能

- **Content-Type 准确性**
  - 所有静态资源返回正确的 MIME 类型
  - 支持字体文件正确加载
  - 修复浏览器控制台 MIME 类型警告

### 📦 构建系统

- **Makefile 增强**
  - 添加 `build-frontend-internal` 内部目标
  - 优化 `clean` 命令清除缓存标记
  - 改进 `dev-backend` 前端构建检查逻辑

---

## [v2.0.0-go] - 2025-01-15

### 🎉 Go 语言重写版本首次发布

这是 CC-Bridge 的完整 Go 语言重写版本，保留所有 TypeScript 版本功能的同时，带来显著的性能提升和部署便利性。

#### ✨ 新特性

- **🚀 高性能重写**
  - 使用 Go 语言完整重写所有后端代码
  - 原生并发支持（Goroutine）
  - 启动速度提升 20 倍（< 100ms vs 2-3s）
  - 内存占用降低 70%（~20MB vs 50-100MB）

- **📦 单文件部署**
  - 前端资源通过 `embed.FS` 嵌入二进制文件
  - 无需 Node.js 运行时
  - 单个可执行文件包含所有功能
  - 跨平台编译支持（Linux/macOS/Windows，amd64/arm64）

- **🎯 完整功能移植**
  - ✅ 所有 4 种上游服务适配器（OpenAI、Gemini、Claude、OpenAI Old）
  - ✅ 完整的协议转换逻辑
  - ✅ 流式响应和工具调用支持
  - ✅ 配置管理和热重载
  - ✅ API 密钥管理和负载均衡
  - ✅ Web 管理界面（完整嵌入）
  - ✅ Failover 故障转移机制

- **⚙️ 改进的版本管理**
  - 集中式版本控制（`VERSION` 文件）
  - 构建时自动注入版本信息
  - Git commit hash 追踪
  - 健康检查 API 包含版本信息

- **🛠️ 增强的构建系统**
  - 统一的 Makefile 构建系统
  - 支持多平台交叉编译
  - 自动化构建脚本
  - 发布包自动打包

#### 📊 性能对比

| 指标 | TypeScript 版本 | Go 版本 | 提升 |
|------|----------------|---------|------|
| 启动时间 | 2-3s | < 100ms | **20x** |
| 内存占用 | 50-100MB | ~20MB | **70%↓** |
| 部署包大小 | 200MB+ | ~15MB | **90%↓** |
| 并发处理 | 事件循环 | 原生 Goroutine | ⭐⭐⭐ |

#### 🎨 技术栈

- **后端**: Go 1.22+, Gin Framework
- **配置**: fsnotify (热重载), godotenv
- **嵌入**: Go embed.FS
- **构建**: Makefile, Shell Scripts

#### 📝 版本管理优化

现在升级版本只需修改一个文件：

```bash
# 只需编辑根目录的 VERSION 文件
echo "v2.1.0" > VERSION

# 重新构建即可
make build
```

所有构建产物（二进制文件、健康检查 API、启动信息）会自动包含新版本！

#### 🔄 迁移指南

从 TypeScript 版本迁移到 Go 版本：

1. 配置文件完全兼容（`.config/config.json`）
2. 环境变量完全兼容（`.env`）
3. API 端点完全兼容（`/v1/messages`、`/health` 等）
4. Web 管理界面功能一致

只需：
```bash
# 1. 构建 Go 版本
make build

# 2. 使用相同的配置文件
cp -r backend/.config backend-go/.config
cp backend/.env backend-go/.env

# 3. 运行
./backend-go/dist/cc-bridge-linux-amd64
```

#### ⚠️ 已知限制

- 暂无 Docker 镜像（计划在 v2.1.0 提供）
- 配置文件加密功能待实现

---

## v1.2.0 - 2025-09-19

### ✨ 新功能

- **Web管理界面全面升级**: 添加了完整的Web管理面板，支持可视化管理API渠道
- **模型映射功能**: 支持将请求中的模型名重定向到目标模型（如 "opus" → "claude-3-5-sonnet"）
- **渠道置顶功能**: 支持将常用渠道置顶显示，提升管理效率
- **API密钥故障转移**: 实现多密钥负载均衡和自动故障转移机制
- **ESC键快捷操作**: 编辑渠道modal支持ESC键快速关闭

### 🎨 UI/UX 优化

- **暗色模式支持**: 全面支持暗色模式，自动适配系统主题设置
- **渠道卡片重设计**: 采用现代化设计语言，提升视觉体验
- **绿色主题边框**: 统一使用绿色主题色，提升界面一致性
- **密钥数量优化**: 将密钥数量显示移至管理标题栏，界面更紧凑
- **模型选择优化**: 源模型名改为下拉选择（opus/sonnet/haiku），避免输入错误

### 🐛 Bug 修复

- **TypeScript类型错误**: 修复变量作用域相关的类型检查错误
- **CSS变量规范**: 根据Vuetify官方文档修复CSS变量使用方式
- **Header配色问题**: 修复编辑渠道modal在暗色模式下的配色问题
- **图标颜色统一**: 统一modal内图标颜色，保持视觉一致性
- **负载均衡策略**: 修复上游负载均衡策略不生效的问题

### ♻️ 重构

- **项目结构**: 重构为monorepo架构，分离前后端代码
- **渠道卡片样式**: 全面重构渠道卡片组件，优化代码结构
- **主题系统**: 基于Vuetify最佳实践重构主题系统

### ⚙️ 其他

- **构建系统**: 添加TypeScript类型检查和构建验证
- **发布流程**: 完善版本发布指南和自动化流程

## v1.1.0 - 2025-09-17

### 🚀 重大优化更新

这个版本专注于代码质量提升，大幅优化了字符串处理、正则表达式使用和代码结构。

#### ✨ 代码优化

- **SSE 数据解析优化**: 
  - 统一使用正则表达式 `/^data:\s*(.*)$/` 处理 Server-Sent Events 数据
  - 支持多种 SSE 格式（`data:`、`data: `、`data:  ` 等）
  - 提升解析健壮性，减少代码复杂度

- **Bearer Token 处理简化**:
  - 使用正则表达式 `/^bearer\s+/i` 替代复杂的字符串判断
  - 代码行数减少 60%，性能提升明显

- **敏感头部处理重构**:
  - 使用函数式的 `replace()` 回调处理 Authorization 头
  - 统一 API Key 掩码逻辑，提升安全性

- **请求头过滤优化**:
  - 缓存 `toLowerCase()` 转换结果，避免重复计算
  - 提升请求处理性能

- **API Key 掩码函数简化**:
  - 使用 `slice()` 替代 `substring()`
  - 条件逻辑简化，代码更清晰

- **参数解析现代化**:
  - 传统 `for` 循环重构为函数式 `reduce()`
  - 使用正则表达式简化命令行参数解析

#### 🧹 代码重构

- **重复代码消除**:
  - 提取 `normalizeClaudeRole` 函数到 `utils.ts` 共享模块
  - 遵循 DRY 原则，便于维护

- **User-Agent 检查优化**:
  - 使用正则表达式 `/^claude-cli/i` 进行大小写不敏感匹配
  - 提升代码可读性

#### 🔧 构建系统改进

- **新增构建脚本**:
  - 添加 `bun run build` 命令用于项目构建验证
  - 添加 `bun run type-check` 命令用于 TypeScript 类型检查

#### 📈 性能提升

- **代码行数减少**: 总计减少约 30% 的代码行数
- **性能改进**: 减少重复的字符串操作和条件判断
- **内存优化**: 更高效的字符串处理逻辑

#### 🛠️ Claude API 流式响应修复

- **修复 Claude API 流式响应解析**:
  - 正确处理 `content_block_delta` 事件中的 `text_delta` 内容
  - 支持 `input_json_delta` 类型的工具调用内容解析
  - 改进工具调用内容的合成显示格式

- **SSE 格式兼容性增强**:
  - 支持标准 `data: ` 格式和紧凑 `data:` 格式
  - 提升与不同上游服务的兼容性

#### 🧪 质量保证

- **类型安全**: 所有修改通过 TypeScript 类型检查
- **构建验证**: 确保所有优化不影响功能完整性
- **向后兼容**: 保持所有现有 API 接口不变

### 🔄 技术债务清理

这次更新严格遵循了软件工程最佳实践：

- **KISS 原则**: 追求代码和设计的极致简洁
- **DRY 原则**: 消除重复代码，统一处理逻辑  
- **YAGNI 原则**: 删除未使用的代码分支
- **函数式编程**: 优先使用函数式方法处理数据转换

---

## v1.0.0 - 2025-09-13

### 🎉 初始版本发布

这是 CC-Bridge的第一个稳定版本。

#### ✨ 主要功能

- **多上游支持**: 内置 `openai`, `openaiold`, `gemini`, 和 `claude` 提供商，实现协议转换。
- **配置管理**:
  - 通过 `config.json` 文件管理上游服务。
  - 提供 `bun run config` 命令行工具，用于动态增、删、改、查上游配置。
  - 支持配置热重载，修改配置无需重启服务。
- **负载均衡**:
  - 支持对单个上游内的多个 API 密钥进行负载均衡。
  - 提供 `round-robin`（轮询）、`random`（随机）和 `failover`（故障转移）三种策略。
- **统一访问入口**: 所有请求通过 `/v1/messages` 代理，简化客户端配置。
- **全面的 API 兼容性**:
  - 支持流式（stream）和非流式响应。
  - 支持工具调用（Tool Use）。
- **环境配置**: 通过 `.env` 文件管理服务器端口、日志级别、访问密钥等。
- **部署与开发**:
  - 提供 `bun run dev` 开发模式，支持源码修改后自动重启。
  - 提供详细的 `README.md` 和 `DEVELOPMENT.md` 文档，包含 PM2 和 Docker 的部署指南。
- **健壮性与监控**:
  - 内置 `/health` 健康检查端点。
  - 详细的请求与响应日志系统。
  - 对上游流式响应中的错误进行捕获和处理。

---

## v2.0.1 升级指南

> 从 v2.0.0-go 升级到 v2.0.1 的完整指南

### 🎯 升级概述

v2.0.1 主要修复了前端资源加载问题和性能优化，强烈建议所有 v2.0.0 用户升级。

#### 主要改进

- ✅ **修复前端无法加载** - 解决 Vite base 路径配置问题
- ✅ **性能提升 142 倍** - 智能缓存机制，开发时启动仅需 0.07 秒
- ✅ **API 路由修复** - 前后端路由完全匹配
- ✅ **ENV 标准化** - 更通用的环境变量命名

#### 升级步骤

1. **备份配置（可选但推荐）**
   ```bash
   cp backend-go/.config/config.json backend-go/.config/config.json.backup
   cp backend-go/.env backend-go/.env.backup
   ```

2. **更新代码**
   ```bash
   git pull origin main
   ```

3. **更新环境变量（推荐）**
   编辑 `backend-go/.env`：
   ```diff
   - NODE_ENV=development
   + # 运行环境: development | production
   + ENV=development
   ```
   **注意**：旧的 `NODE_ENV` 仍然有效（向后兼容），但建议迁移到 `ENV`。

4. **重新构建**
   ```bash
   make clean
   make build-frontend-internal
   make run
   ```

5. **验证升级**
   ```bash
   make info  # 应该显示 Version: v2.0.1
   curl http://localhost:3001/health | jq '.version'
   ```

#### 新功能使用

**智能缓存**：
- 首次构建：~10 秒
- 无变更重启：**0.07 秒**（提升 142 倍）
- 有变更重新构建：~8.5 秒

**ENV 变量详细配置**：
- `ENV=development`：开发模式（详细日志、开发端点、宽松 CORS）
- `ENV=production`：生产模式（高性能、严格安全）

#### 破坏性变更

**无破坏性变更**。所有 v2.0.0 配置和 API 完全兼容。

#### 回滚到 v2.0.0

如果升级遇到问题，可以回滚：
```bash
git checkout v2.0.0-go
cp backend-go/.config/config.json.backup backend-go/.config/config.json
cp backend-go/.env.backup backend-go/.env
make clean && make build-frontend-internal
```

**升级成功！** 🎉

---
