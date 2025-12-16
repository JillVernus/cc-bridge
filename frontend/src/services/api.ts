// API服务模块

// 从环境变量读取配置
const getApiBase = () => {
  // 在生产环境中，API调用会直接请求当前域名
  if (import.meta.env.PROD) {
    return '/api'
  }

  // 在开发环境中，支持从环境变量配置后端地址
  const backendUrl = import.meta.env.VITE_BACKEND_URL
  const apiBasePath = import.meta.env.VITE_API_BASE_PATH || '/api'

  if (backendUrl) {
    return `${backendUrl}${apiBasePath}`
  }

  // fallback到默认配置
  return '/api'
}

const API_BASE = getApiBase()

// 打印当前API配置（仅开发环境）
if (import.meta.env.DEV) {
  console.log('🔗 API Configuration:', {
    API_BASE,
    BACKEND_URL: import.meta.env.VITE_BACKEND_URL,
    IS_DEV: import.meta.env.DEV,
    IS_PROD: import.meta.env.PROD
  })
}

// 渠道状态枚举
export type ChannelStatus = 'active' | 'suspended' | 'disabled'

// 渠道指标
// 分时段统计
export interface TimeWindowStats {
  requestCount: number
  successCount: number
  failureCount: number
  successRate: number
}

export interface ChannelMetrics {
  channelIndex: number
  requestCount: number
  successCount: number
  failureCount: number
  successRate: number       // 0-100
  errorRate: number         // 0-100
  consecutiveFailures: number
  latency: number           // ms
  lastSuccessAt?: string
  lastFailureAt?: string
  // 分时段统计 (15m, 1h, 6h, 24h)
  timeWindows?: {
    '15m': TimeWindowStats
    '1h': TimeWindowStats
    '6h': TimeWindowStats
    '24h': TimeWindowStats
  }
}

// OAuth tokens for OpenAI OAuth channels (Codex)
export interface OAuthTokens {
  access_token: string
  account_id: string
  id_token?: string
  refresh_token: string
  last_refresh?: string
}

export interface Channel {
  name: string
  serviceType: 'openai' | 'openaiold' | 'gemini' | 'claude' | 'responses' | 'openai-oauth'
  baseUrl: string
  apiKeys: string[]
  description?: string
  website?: string
  insecureSkipVerify?: boolean
  responseHeaderTimeout?: number  // 响应头超时（秒），默认30秒
  modelMapping?: Record<string, string>
  latency?: number
  status?: ChannelStatus | 'healthy' | 'error' | 'unknown'
  index: number
  pinned?: boolean
  // 多渠道调度相关字段
  priority?: number          // 渠道优先级（数字越小优先级越高）
  metrics?: ChannelMetrics   // 实时指标
  suspendReason?: string     // 熔断原因
  // 价格乘数配置：key 为模型名称（支持前缀匹配），"_default" 为默认乘数
  priceMultipliers?: Record<string, TokenPriceMultipliers>
  // OAuth tokens for openai-oauth service type
  oauthTokens?: OAuthTokens
}

export interface ChannelsResponse {
  channels: Channel[]
  current: number
  loadBalance: string
}

export interface PingResult {
  success: boolean
  latency: number
  status: string
  error?: string
}

class ApiService {
  private apiKey: string | null = null

  // 设置API密钥
  setApiKey(key: string | null) {
    this.apiKey = key
  }

  // 获取当前API密钥
  getApiKey(): string | null {
    return this.apiKey
  }

  // 初始化密钥（从 sessionStorage，必要时从 localStorage 迁移并清理）
  initializeAuth() {
    const sessionKey = sessionStorage.getItem('proxyAccessKey')
    if (sessionKey) {
      this.setApiKey(sessionKey)
      return sessionKey
    }

    // 兼容：将旧的 localStorage key 迁移到 sessionStorage 并清理
    const legacyKey = localStorage.getItem('proxyAccessKey')
    if (legacyKey) {
      sessionStorage.setItem('proxyAccessKey', legacyKey)
      localStorage.removeItem('proxyAccessKey')
      this.setApiKey(legacyKey)
      return legacyKey
    }

    return null
  }

  // 清除认证信息
  clearAuth() {
    this.apiKey = null
    sessionStorage.removeItem('proxyAccessKey')
    localStorage.removeItem('proxyAccessKey')
  }

  private async request(url: string, options: RequestInit = {}): Promise<any> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string>)
    }

    // 添加API密钥到请求头
    if (this.apiKey) {
      headers['x-api-key'] = this.apiKey
    }

    const response = await fetch(`${API_BASE}${url}`, {
      ...options,
      headers
    })

    if (!response.ok) {
      const error = await response.json().catch(() => ({ error: 'Unknown error' }))

      // 如果是401错误，清除本地认证信息并提示用户重新登录
      if (response.status === 401) {
        this.clearAuth()
        // 记录认证失败(前端日志)
        console.warn('🔒 认证失败 - 时间:', new Date().toISOString())
        throw new Error('认证失败，请重新输入访问密钥')
      }

      throw new Error(error.error || error.message || 'Request failed')
    }

    return response.json()
  }

  async getChannels(): Promise<ChannelsResponse> {
    return this.request('/channels')
  }

  async addChannel(channel: Omit<Channel, 'index' | 'latency' | 'status'>): Promise<void> {
    await this.request('/channels', {
      method: 'POST',
      body: JSON.stringify(channel)
    })
  }

  async updateChannel(id: number, channel: Partial<Channel>): Promise<void> {
    await this.request(`/channels/${id}`, {
      method: 'PUT',
      body: JSON.stringify(channel)
    })
  }

  async deleteChannel(id: number): Promise<void> {
    await this.request(`/channels/${id}`, {
      method: 'DELETE'
    })
  }

  async addApiKey(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/channels/${channelId}/keys`, {
      method: 'POST',
      body: JSON.stringify({ apiKey })
    })
  }

  async removeApiKey(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/channels/${channelId}/keys/${encodeURIComponent(apiKey)}`, {
      method: 'DELETE'
    })
  }

  async pingChannel(id: number): Promise<PingResult> {
    return this.request(`/ping/${id}`)
  }

  async pingAllChannels(): Promise<Array<{ id: number; name: string; latency: number; status: string }>> {
    return this.request('/ping')
  }

  async updateLoadBalance(strategy: string): Promise<void> {
    await this.request('/loadbalance', {
      method: 'PUT',
      body: JSON.stringify({ strategy })
    })
  }

  async updateResponsesLoadBalance(strategy: string): Promise<void> {
    await this.request('/responses/loadbalance', {
      method: 'PUT',
      body: JSON.stringify({ strategy })
    })
  }

  // ============== Responses 渠道管理 API ==============

  async getResponsesChannels(): Promise<ChannelsResponse> {
    return this.request('/responses/channels')
  }

  async addResponsesChannel(channel: Omit<Channel, 'index' | 'latency' | 'status'>): Promise<void> {
    await this.request('/responses/channels', {
      method: 'POST',
      body: JSON.stringify(channel)
    })
  }

  async updateResponsesChannel(id: number, channel: Partial<Channel>): Promise<void> {
    await this.request(`/responses/channels/${id}`, {
      method: 'PUT',
      body: JSON.stringify(channel)
    })
  }

  async deleteResponsesChannel(id: number): Promise<void> {
    await this.request(`/responses/channels/${id}`, {
      method: 'DELETE'
    })
  }

  async addResponsesApiKey(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/responses/channels/${channelId}/keys`, {
      method: 'POST',
      body: JSON.stringify({ apiKey })
    })
  }

  async removeResponsesApiKey(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/responses/channels/${channelId}/keys/${encodeURIComponent(apiKey)}`, {
      method: 'DELETE'
    })
  }

  async moveApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/top`, {
      method: 'POST'
    })
  }

  async moveApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/bottom`, {
      method: 'POST'
    })
  }

  async moveResponsesApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/responses/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/top`, {
      method: 'POST'
    })
  }

  async moveResponsesApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/responses/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/bottom`, {
      method: 'POST'
    })
  }

  // ============== 多渠道调度 API ==============

  // 重新排序渠道优先级
  async reorderChannels(order: number[]): Promise<void> {
    await this.request('/channels/reorder', {
      method: 'POST',
      body: JSON.stringify({ order })
    })
  }

  // 设置渠道状态
  async setChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
    await this.request(`/channels/${channelId}/status`, {
      method: 'PATCH',
      body: JSON.stringify({ status })
    })
  }

  // 恢复熔断渠道（重置错误计数）
  async resumeChannel(channelId: number): Promise<void> {
    await this.request(`/channels/${channelId}/resume`, {
      method: 'POST'
    })
  }

  // 获取渠道指标
  async getChannelMetrics(): Promise<ChannelMetrics[]> {
    return this.request('/channels/metrics')
  }

  // 获取调度器统计信息
  async getSchedulerStats(type?: 'messages' | 'responses'): Promise<{
    multiChannelMode: boolean
    activeChannelCount: number
    traceAffinityCount: number
    traceAffinityTTL: string
    failureThreshold: number
    windowSize: number
  }> {
    const query = type === 'responses' ? '?type=responses' : ''
    return this.request(`/channels/scheduler/stats${query}`)
  }

  // ============== Responses 多渠道调度 API ==============

  // 重新排序 Responses 渠道优先级
  async reorderResponsesChannels(order: number[]): Promise<void> {
    await this.request('/responses/channels/reorder', {
      method: 'POST',
      body: JSON.stringify({ order })
    })
  }

  // 设置 Responses 渠道状态
  async setResponsesChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
    await this.request(`/responses/channels/${channelId}/status`, {
      method: 'PATCH',
      body: JSON.stringify({ status })
    })
  }

  // 恢复 Responses 熔断渠道
  async resumeResponsesChannel(channelId: number): Promise<void> {
    await this.request(`/responses/channels/${channelId}/resume`, {
      method: 'POST'
    })
  }

  // 获取 Responses 渠道指标
  async getResponsesChannelMetrics(): Promise<ChannelMetrics[]> {
    return this.request('/responses/channels/metrics')
  }

  // ============== 促销期管理 API ==============

  // 设置 Messages 渠道促销期
  async setChannelPromotion(channelId: number, durationSeconds: number): Promise<void> {
    await this.request(`/channels/${channelId}/promotion`, {
      method: 'POST',
      body: JSON.stringify({ duration: durationSeconds })
    })
  }

  // 设置 Responses 渠道促销期
  async setResponsesChannelPromotion(channelId: number, durationSeconds: number): Promise<void> {
    await this.request(`/responses/channels/${channelId}/promotion`, {
      method: 'POST',
      body: JSON.stringify({ duration: durationSeconds })
    })
  }

  // ============== 请求日志 API ==============

  async getRequestLogs(filter?: RequestLogFilter): Promise<RequestLogListResponse> {
    const params = new URLSearchParams()
    if (filter?.provider) params.set('provider', filter.provider)
    if (filter?.model) params.set('model', filter.model)
    if (filter?.endpoint) params.set('endpoint', filter.endpoint)
    if (filter?.userId) params.set('userId', filter.userId)
    if (filter?.sessionId) params.set('sessionId', filter.sessionId)
    if (filter?.httpStatus) params.set('httpStatus', String(filter.httpStatus))
    if (filter?.limit) params.set('limit', String(filter.limit))
    if (filter?.offset) params.set('offset', String(filter.offset))
    if (filter?.from) params.set('from', filter.from)
    if (filter?.to) params.set('to', filter.to)
    const query = params.toString() ? `?${params.toString()}` : ''
    return this.request(`/logs${query}`)
  }

  async getRequestLogStats(filter?: { from?: string; to?: string; userId?: string; sessionId?: string }): Promise<RequestLogStats> {
    const params = new URLSearchParams()
    if (filter?.from) params.set('from', filter.from)
    if (filter?.to) params.set('to', filter.to)
    if (filter?.userId) params.set('userId', filter.userId)
    if (filter?.sessionId) params.set('sessionId', filter.sessionId)
    const query = params.toString() ? `?${params.toString()}` : ''
    return this.request(`/logs/stats${query}`)
  }

  // 获取活跃会话
  async getActiveSessions(threshold?: string): Promise<ActiveSession[]> {
    const params = new URLSearchParams()
    if (threshold) params.set('threshold', threshold)
    const query = params.toString() ? `?${params.toString()}` : ''
    return this.request(`/logs/sessions/active${query}`)
  }

  // 清空所有日志
  async clearRequestLogs(): Promise<{ message: string }> {
    return this.request('/logs', { method: 'DELETE' })
  }

  // 清理指定天数前的日志
  async cleanupRequestLogs(days: number): Promise<{ message: string; deletedCount: number; retentionDays: number }> {
    return this.request(`/logs/cleanup?days=${days}`, { method: 'POST' })
  }

  // ============== 定价配置 API ==============

  // 获取定价配置
  async getPricing(): Promise<PricingConfig> {
    return this.request('/pricing')
  }

  // 更新整个定价配置
  async updatePricing(config: PricingConfig): Promise<{ message: string; config: PricingConfig }> {
    return this.request('/pricing', {
      method: 'PUT',
      body: JSON.stringify(config)
    })
  }

  // 添加或更新单个模型的定价
  async setModelPricing(model: string, pricing: ModelPricing): Promise<{ message: string; model: string; pricing: ModelPricing }> {
    return this.request(`/pricing/models/${encodeURIComponent(model)}`, {
      method: 'PUT',
      body: JSON.stringify(pricing)
    })
  }

  // 删除单个模型的定价
  async deleteModelPricing(model: string): Promise<{ message: string; model: string }> {
    return this.request(`/pricing/models/${encodeURIComponent(model)}`, {
      method: 'DELETE'
    })
  }

  // 重置定价配置为默认值
  async resetPricingToDefault(): Promise<{ message: string; config: PricingConfig }> {
    return this.request('/pricing/reset', {
      method: 'POST'
    })
  }

  // ============== 备份/恢复 API ==============

  // 创建备份
  async createBackup(): Promise<BackupCreateResponse> {
    return this.request('/config/backup', {
      method: 'POST'
    })
  }

  // 获取备份列表
  async listBackups(): Promise<BackupListResponse> {
    return this.request('/config/backups')
  }

  // 恢复备份
  async restoreBackup(filename: string): Promise<BackupRestoreResponse> {
    return this.request(`/config/restore/${encodeURIComponent(filename)}`, {
      method: 'POST'
    })
  }

  // 删除备份
  async deleteBackup(filename: string): Promise<{ message: string; filename: string }> {
    return this.request(`/config/backups/${encodeURIComponent(filename)}`, {
      method: 'DELETE'
    })
  }

  // ============== 系统信息 API ==============

  // 获取版本信息（从 /health 端点）
  async getVersion(): Promise<{ version: string; buildTime: string; gitCommit: string }> {
    // health 端点不在 /api 路径下，需要直接访问
    const baseUrl = import.meta.env.PROD ? '' : (import.meta.env.VITE_BACKEND_URL || '')
    const response = await fetch(`${baseUrl}/health`, {
      headers: this.apiKey ? { 'x-api-key': this.apiKey } : {}
    })
    if (!response.ok) {
      throw new Error('Failed to fetch version')
    }
    const data = await response.json()
    return data.version || { version: 'unknown', buildTime: 'unknown', gitCommit: 'unknown' }
  }
}

// 请求日志类型
export interface RequestLog {
  id: string
  status: 'pending' | 'completed' | 'error' | 'timeout'
  initialTime: string
  completeTime: string
  durationMs: number
  type: string
  providerName: string
  model: string
  responseModel?: string  // 响应中的模型名称（可能与请求不同）
  reasoningEffort?: string  // Codex reasoning effort (low/medium/high/xhigh)
  inputTokens: number
  outputTokens: number
  cacheCreationInputTokens: number
  cacheReadInputTokens: number
  totalTokens: number
  price: number
  // 成本明细
  inputCost?: number
  outputCost?: number
  cacheCreationCost?: number
  cacheReadCost?: number
  // 其他字段
  httpStatus: number
  stream: boolean
  channelId: number
  channelName: string
  endpoint: string
  userId?: string
  sessionId?: string
  error?: string
  upstreamError?: string  // 上游服务原始错误信息
  createdAt: string
}

export interface RequestLogFilter {
  provider?: string
  model?: string
  httpStatus?: number
  endpoint?: string
  userId?: string
  sessionId?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export interface RequestLogListResponse {
  requests: RequestLog[]
  total: number
  hasMore: boolean
}

export interface GroupStats {
  count: number
  inputTokens: number
  outputTokens: number
  cacheCreationInputTokens: number
  cacheReadInputTokens: number
  cost: number
}

export interface RequestLogStats {
  totalRequests: number
  totalTokens: {
    inputTokens: number
    outputTokens: number
    cacheCreationInputTokens: number
    cacheReadInputTokens: number
    totalTokens: number
  }
  totalCost: number
  byProvider: Record<string, GroupStats>
  byModel: Record<string, GroupStats>
  byUser: Record<string, GroupStats>
  bySession: Record<string, GroupStats>
  timeRange: { from: string; to: string }
}

// 定价配置类型
export interface ModelPricing {
  inputPrice: number              // 输入 token 价格 ($/1M tokens)
  outputPrice: number             // 输出 token 价格 ($/1M tokens)
  cacheCreationPrice?: number | null  // 缓存创建价格 ($/1M tokens)，null/undefined 表示使用默认值，0 表示免费
  cacheReadPrice?: number | null      // 缓存读取价格 ($/1M tokens)，null/undefined 表示使用默认值，0 表示免费
  description?: string            // 模型描述
}

export interface PricingConfig {
  models: Record<string, ModelPricing>
  currency: string
}

// 渠道价格乘数配置（用于渠道级别的折扣）
export interface TokenPriceMultipliers {
  inputMultiplier?: number         // 输入 token 价格乘数，默认 1.0
  outputMultiplier?: number        // 输出 token 价格乘数，默认 1.0
  cacheCreationMultiplier?: number // 缓存创建价格乘数，默认 1.0
  cacheReadMultiplier?: number     // 缓存读取价格乘数，默认 1.0
}

// 备份信息类型
export interface BackupInfo {
  filename: string
  createdAt: string
  size: number
}

export interface BackupListResponse {
  backups: BackupInfo[]
}

export interface BackupCreateResponse {
  message: string
  filename: string
  createdAt: string
  size: number
}

export interface BackupRestoreResponse {
  message: string
  filename: string
  restoredAt: string
}

// 活跃会话类型
export interface ActiveSession {
  sessionId: string
  type: string  // claude, openai, codex, responses
  firstRequestTime: string
  lastRequestTime: string
  count: number
  inputTokens: number
  outputTokens: number
  cacheCreationInputTokens: number
  cacheReadInputTokens: number
  cost: number
}

export const api = new ApiService()
export default api
