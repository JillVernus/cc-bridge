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

export interface RecentCallStat {
  success: boolean
  statusCode?: number
  timestamp?: string
  model?: string
  channelName?: string
  routedChannelName?: string
}

export interface ChannelMetrics {
  channelIndex: number
  requestCount: number
  successCount: number
  failureCount: number
  successRate: number // 0-100
  errorRate: number // 0-100
  consecutiveFailures: number
  latency: number // ms
  lastSuccessAt?: string
  lastFailureAt?: string
  // 分时段统计 (15m, 1h, 6h, 24h)
  timeWindows?: {
    '15m': TimeWindowStats
    '1h': TimeWindowStats
    '6h': TimeWindowStats
    '24h': TimeWindowStats
  }
  recentCalls?: RecentCallStat[]
}

// OAuth tokens for OpenAI OAuth channels (Codex)
export interface OAuthTokens {
  access_token: string
  account_id: string
  id_token?: string
  refresh_token: string
  last_refresh?: string
}

// OAuth status for Codex channels (derived from tokens, no secrets exposed)
export interface CodexOAuthStatus {
  account_id?: string
  masked_account_id?: string
  email?: string
  plan_type?: string
  subscription_active_start?: string
  subscription_active_until?: string
  subscription_last_checked?: string
  token_expires_at?: string
  last_refresh?: string
}

// Codex-specific quota information from response headers
export interface CodexQuotaInfo {
  plan_type?: string
  // Primary window (short-term, e.g., 5 hours)
  primary_used_percent: number
  primary_window_minutes?: number
  primary_reset_at?: string
  // Secondary window (long-term, e.g., 7 days)
  secondary_used_percent: number
  secondary_window_minutes?: number
  secondary_reset_at?: string
  // Over limit indicator
  primary_over_secondary_limit_percent?: number
  // Credits info
  credits_has_credits?: boolean
  credits_unlimited?: boolean
  credits_balance?: string
  updated_at?: string
}

// Rate limit information from OpenAI response headers
export interface RateLimitInfo {
  limit_requests?: number
  remaining_requests?: number
  reset_requests?: string
  limit_tokens?: number
  remaining_tokens?: number
  reset_tokens?: string
  updated_at?: string
}

// Quota status for a channel
export interface QuotaInfo {
  codex_quota?: CodexQuotaInfo
  rate_limit?: RateLimitInfo
  is_exceeded?: boolean
  exceeded_at?: string
  recover_at?: string
  exceeded_reason?: string
}

export interface OAuthStatusResponse {
  channelId: number
  channelName: string
  serviceType: string
  configured: boolean
  message?: string
  status?: CodexOAuthStatus
  tokenStatus?: 'valid' | 'expiring_soon' | 'expired'
  tokenExpiresIn?: number
  quota?: QuotaInfo
}

// Composite channel model mapping
export type CompositeTargetPool = 'messages' | 'responses'

export interface CompositeTargetRef {
  pool?: CompositeTargetPool // Default: messages
  channelId: string
}

export interface CompositeMapping {
  pattern: string // Model pattern: "haiku", "sonnet", "opus" (mandatory, no wildcard)
  targetChannelId: string // Primary target channel ID
  targetPool?: CompositeTargetPool // Primary target pool (default: messages)
  failoverChain?: string[] // Ordered failover channel IDs (min 1 required)
  failoverTargets?: CompositeTargetRef[] // Canonical failover targets (pool-aware)
  targetModel?: string // Optional model name override
}

export interface ContentFilter {
  enabled: boolean // Whether content filtering is active
  rules?: ContentFilterRule[] // New format: per-keyword status code rules (first match wins)
  keywords?: string[] // Legacy format: keywords to match in response text (case-insensitive substring)
  statusCode?: number // Legacy format: shared HTTP status code for keywords (default 429)
}

export interface ContentFilterRule {
  keyword: string
  statusCode?: number
}

export interface Channel {
  id?: string // Unique channel ID (for composite mapping references)
  name: string
  serviceType: 'openai' | 'openai_chat' | 'openaiold' | 'gemini' | 'claude' | 'responses' | 'openai-oauth' | 'composite'
  baseUrl: string
  apiKeys?: string[] // Only present when creating/updating, not in GET responses
  importFromResponsesChannelId?: string // Messages channel only: copy baseUrl+apiKeys from Responses channel
  apiKeyCount?: number // Number of API keys (returned by GET)
  maskedKeys?: Array<{ index: number; masked: string }> // Masked keys for display/deletion
  description?: string
  website?: string
  insecureSkipVerify?: boolean
  responseHeaderTimeout?: number // 响应头超时（秒），默认30秒
  modelMapping?: Record<string, string>
  latency?: number
  status?: ChannelStatus | 'healthy' | 'error' | 'unknown'
  index: number
  pinned?: boolean
  // 多渠道调度相关字段
  priority?: number // 渠道优先级（数字越小优先级越高）
  metrics?: ChannelMetrics // 实时指标
  suspendReason?: string // 熔断原因
  // 价格乘数配置：key 为模型名称（支持前缀匹配），"_default" 为默认乘数
  priceMultipliers?: Record<string, TokenPriceMultipliers>
  // OAuth tokens for openai-oauth service type
  oauthTokens?: OAuthTokens
  // Codex Responses service_tier override policy for eligible channels
  codexServiceTierOverride?: 'off' | 'force_priority'
  // 配额设置
  quotaType?: 'requests' | 'credit' | '' // 配额类型：请求数 | 额度 | 无
  quotaLimit?: number // 最大配额值
  quotaResetAt?: string // 首次/下次重置时间 (ISO datetime)
  quotaResetInterval?: number // 重置间隔值
  quotaResetUnit?: 'hours' | 'days' | 'weeks' | 'months' // 重置间隔单位
  quotaModels?: string[] // 配额计数模型过滤（子字符串匹配），空数组=全部模型
  quotaResetMode?: 'fixed' | 'rolling' // 重置模式：固定周期 | 滚动周期，默认 fixed
  // Per-channel rate limiting (upstream protection)
  rateLimitRpm?: number // Requests per minute (0 = disabled)
  queueEnabled?: boolean // Enable queue mode instead of reject
  queueTimeout?: number // Max seconds to wait in queue (default 60)
  // Per-channel API key load balancing strategy (overrides global setting)
  keyLoadBalance?: '' | 'round-robin' | 'random' | 'failover'
  // Content filter: detect errors returned as HTTP 200 with error text in body
  contentFilter?: ContentFilter
  // Composite channel mappings
  compositeMappings?: CompositeMapping[] // Model-to-channel mappings for composite channels
}

// 渠道用量配额状态
export interface ChannelUsageStatus {
  quotaType: '' | 'requests' | 'credit' // 配额类型
  limit: number // 最大配额值
  used: number // 已使用量
  remaining: number // 剩余量
  remainingPercent: number // 剩余百分比 (0-100)
  lastResetAt?: string // 上次重置时间 (ISO datetime)
  nextResetAt?: string // 下次重置时间 (ISO datetime)
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

const asFiniteNumber = (value: unknown, fallback = 0): number => {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

const normalizeChannelMetricsEntry = (entry: Record<string, unknown>): ChannelMetrics => {
  const failureRate = asFiniteNumber(entry.failureRate)

  return {
    channelIndex: asFiniteNumber(entry.channelIndex, asFiniteNumber(entry.index)),
    requestCount: asFiniteNumber(entry.requestCount),
    successCount: asFiniteNumber(entry.successCount),
    failureCount: asFiniteNumber(entry.failureCount),
    successRate: asFiniteNumber(entry.successRate, Math.max(0, 100 - failureRate * 100)),
    errorRate: asFiniteNumber(entry.errorRate, failureRate * 100),
    consecutiveFailures: asFiniteNumber(entry.consecutiveFailures),
    latency: asFiniteNumber(entry.latency),
    lastSuccessAt: typeof entry.lastSuccessAt === 'string' ? entry.lastSuccessAt : undefined,
    lastFailureAt: typeof entry.lastFailureAt === 'string' ? entry.lastFailureAt : undefined,
    timeWindows: entry.timeWindows as ChannelMetrics['timeWindows'],
    recentCalls: Array.isArray(entry.recentCalls) ? (entry.recentCalls as RecentCallStat[]) : []
  }
}

const normalizeChannelMetricsResponse = (payload: unknown): ChannelMetrics[] => {
  if (Array.isArray(payload)) {
    return payload.map(item => normalizeChannelMetricsEntry((item ?? {}) as Record<string, unknown>))
  }

  if (
    payload &&
    typeof payload === 'object' &&
    'channels' in payload &&
    Array.isArray((payload as { channels?: unknown }).channels)
  ) {
    return (payload as { channels: unknown[] }).channels.map(item =>
      normalizeChannelMetricsEntry((item ?? {}) as Record<string, unknown>)
    )
  }

  return []
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
    // Deprecated: Use removeApiKeyByIndex instead
    await this.request(`/channels/${channelId}/keys/${encodeURIComponent(apiKey)}`, {
      method: 'DELETE'
    })
  }

  async removeApiKeyByIndex(channelId: number, keyIndex: number): Promise<void> {
    await this.request(`/channels/${channelId}/keys/index/${keyIndex}`, {
      method: 'DELETE'
    })
  }

  async pingChannel(id: number): Promise<PingResult> {
    return this.request(`/ping/${id}`)
  }

  async pingAllChannels(): Promise<Array<{ id: number; name: string; latency: number; status: string }>> {
    return this.request('/ping')
  }

  // Fetch models from upstream provider
  async fetchUpstreamModels(
    channelId: number
  ): Promise<{ success: boolean; models?: Array<{ id: string; object?: string; owned_by?: string }>; error?: string }> {
    return this.request(`/channels/${channelId}/models`)
  }

  // Fetch models from Responses upstream provider
  async fetchResponsesUpstreamModels(
    channelId: number
  ): Promise<{ success: boolean; models?: Array<{ id: string; object?: string; owned_by?: string }>; error?: string }> {
    return this.request(`/responses/channels/${channelId}/models`)
  }

  // Fetch models from Gemini upstream provider
  async fetchGeminiUpstreamModels(
    channelId: number
  ): Promise<{ success: boolean; models?: Array<{ id: string; object?: string; owned_by?: string }>; error?: string }> {
    return this.request(`/gemini/channels/${channelId}/models`)
  }

  // Fetch models from Chat upstream provider
  async fetchChatUpstreamModels(
    channelId: number
  ): Promise<{ success: boolean; models?: Array<{ id: string; object?: string; owned_by?: string }>; error?: string }> {
    return this.request(`/chat/channels/${channelId}/models`)
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

  async updateGeminiLoadBalance(strategy: string): Promise<void> {
    await this.request('/gemini/loadbalance', {
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
    // Deprecated: Use removeResponsesApiKeyByIndex instead
    await this.request(`/responses/channels/${channelId}/keys/${encodeURIComponent(apiKey)}`, {
      method: 'DELETE'
    })
  }

  async removeResponsesApiKeyByIndex(channelId: number, keyIndex: number): Promise<void> {
    await this.request(`/responses/channels/${channelId}/keys/index/${keyIndex}`, {
      method: 'DELETE'
    })
  }

  async moveApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
    // Deprecated: Use moveApiKeyToTopByIndex instead
    await this.request(`/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/top`, {
      method: 'POST'
    })
  }

  async moveApiKeyToTopByIndex(channelId: number, keyIndex: number): Promise<void> {
    await this.request(`/channels/${channelId}/keys/index/${keyIndex}/top`, {
      method: 'POST'
    })
  }

  async moveApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
    // Deprecated: Use moveApiKeyToBottomByIndex instead
    await this.request(`/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/bottom`, {
      method: 'POST'
    })
  }

  async moveApiKeyToBottomByIndex(channelId: number, keyIndex: number): Promise<void> {
    await this.request(`/channels/${channelId}/keys/index/${keyIndex}/bottom`, {
      method: 'POST'
    })
  }

  async moveResponsesApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
    // Deprecated: Use moveResponsesApiKeyToTopByIndex instead
    await this.request(`/responses/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/top`, {
      method: 'POST'
    })
  }

  async moveResponsesApiKeyToTopByIndex(channelId: number, keyIndex: number): Promise<void> {
    await this.request(`/responses/channels/${channelId}/keys/index/${keyIndex}/top`, {
      method: 'POST'
    })
  }

  async moveResponsesApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
    // Deprecated: Use moveResponsesApiKeyToBottomByIndex instead
    await this.request(`/responses/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/bottom`, {
      method: 'POST'
    })
  }

  async moveResponsesApiKeyToBottomByIndex(channelId: number, keyIndex: number): Promise<void> {
    await this.request(`/responses/channels/${channelId}/keys/index/${keyIndex}/bottom`, {
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
    return normalizeChannelMetricsResponse(await this.request('/channels/metrics'))
  }

  // 获取调度器统计信息
  async getSchedulerStats(type?: 'messages' | 'responses' | 'gemini' | 'chat'): Promise<{
    multiChannelMode: boolean
    activeChannelCount: number
    traceAffinityCount: number
    traceAffinityTTL: string
    failureThreshold: number
    windowSize: number
  }> {
    let query = ''
    if (type === 'responses') query = '?type=responses'
    else if (type === 'gemini') query = '?type=gemini'
    else if (type === 'chat') query = '?type=chat'
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
    return normalizeChannelMetricsResponse(await this.request('/responses/channels/metrics'))
  }

  // 获取 Responses 渠道 OAuth 状态（仅适用于 openai-oauth 类型）
  async getResponsesChannelOAuthStatus(channelId: number): Promise<OAuthStatusResponse> {
    return this.request(`/responses/channels/${channelId}/oauth/status`)
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

  // ============== Gemini 渠道管理 API ==============

  async getGeminiChannels(): Promise<ChannelsResponse> {
    return this.request('/gemini/channels')
  }

  async addGeminiChannel(channel: Omit<Channel, 'index' | 'latency' | 'status'>): Promise<void> {
    await this.request('/gemini/channels', {
      method: 'POST',
      body: JSON.stringify(channel)
    })
  }

  async updateGeminiChannel(id: number, channel: Partial<Channel>): Promise<void> {
    await this.request(`/gemini/channels/${id}`, {
      method: 'PUT',
      body: JSON.stringify(channel)
    })
  }

  async deleteGeminiChannel(id: number): Promise<void> {
    await this.request(`/gemini/channels/${id}`, {
      method: 'DELETE'
    })
  }

  async addGeminiApiKey(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/gemini/channels/${channelId}/keys`, {
      method: 'POST',
      body: JSON.stringify({ apiKey })
    })
  }

  async removeGeminiApiKeyByIndex(channelId: number, keyIndex: number): Promise<void> {
    await this.request(`/gemini/channels/${channelId}/keys/index/${keyIndex}`, {
      method: 'DELETE'
    })
  }

  // 重新排序 Gemini 渠道优先级
  async reorderGeminiChannels(order: number[]): Promise<void> {
    await this.request('/gemini/channels/reorder', {
      method: 'POST',
      body: JSON.stringify({ order })
    })
  }

  // 设置 Gemini 渠道状态
  async setGeminiChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
    await this.request(`/gemini/channels/${channelId}/status`, {
      method: 'PATCH',
      body: JSON.stringify({ status })
    })
  }

  // 获取 Gemini 渠道指标
  async getGeminiChannelMetrics(): Promise<ChannelMetrics[]> {
    return normalizeChannelMetricsResponse(await this.request('/gemini/channels/metrics'))
  }

  // ============== Chat (OpenAI Chat Completions) 渠道管理 API ==============

  async getChatChannels(): Promise<ChannelsResponse> {
    return this.request('/chat/channels')
  }

  async addChatChannel(channel: Omit<Channel, 'index' | 'latency' | 'status'>): Promise<void> {
    await this.request('/chat/channels', {
      method: 'POST',
      body: JSON.stringify(channel)
    })
  }

  async updateChatChannel(id: number, channel: Partial<Channel>): Promise<void> {
    await this.request(`/chat/channels/${id}`, {
      method: 'PUT',
      body: JSON.stringify(channel)
    })
  }

  async deleteChatChannel(id: number): Promise<void> {
    await this.request(`/chat/channels/${id}`, {
      method: 'DELETE'
    })
  }

  async addChatApiKey(channelId: number, apiKey: string): Promise<void> {
    await this.request(`/chat/channels/${channelId}/keys`, {
      method: 'POST',
      body: JSON.stringify({ apiKey })
    })
  }

  async removeChatApiKeyByIndex(channelId: number, keyIndex: number): Promise<void> {
    await this.request(`/chat/channels/${channelId}/keys/index/${keyIndex}`, {
      method: 'DELETE'
    })
  }

  // 重新排序 Chat 渠道优先级
  async reorderChatChannels(order: number[]): Promise<void> {
    await this.request('/chat/channels/reorder', {
      method: 'POST',
      body: JSON.stringify({ order })
    })
  }

  // 设置 Chat 渠道状态
  async setChatChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
    await this.request(`/chat/channels/${channelId}/status`, {
      method: 'PATCH',
      body: JSON.stringify({ status })
    })
  }

  // 获取 Chat 渠道指标
  async getChatChannelMetrics(): Promise<ChannelMetrics[]> {
    return normalizeChannelMetricsResponse(await this.request('/chat/channels/metrics'))
  }

  async updateChatLoadBalance(strategy: string): Promise<void> {
    await this.request('/chat/loadbalance', {
      method: 'PUT',
      body: JSON.stringify({ strategy })
    })
  }

  // ============== 请求日志 API ==============

  async getRequestLogs(filter?: RequestLogFilter): Promise<RequestLogListResponse> {
    const params = new URLSearchParams()
    if (filter?.provider) params.set('provider', filter.provider)
    if (filter?.model) params.set('model', filter.model)
    if (filter?.endpoint) params.set('endpoint', filter.endpoint)
    if (filter?.clientId) params.set('clientId', filter.clientId)
    if (filter?.sessionId) params.set('sessionId', filter.sessionId)
    if (filter?.httpStatus) params.set('httpStatus', String(filter.httpStatus))
    if (filter?.limit) params.set('limit', String(filter.limit))
    if (filter?.offset) params.set('offset', String(filter.offset))
    if (filter?.from) params.set('from', filter.from)
    if (filter?.to) params.set('to', filter.to)
    const query = params.toString() ? `?${params.toString()}` : ''
    return this.request(`/logs${query}`)
  }

  async getRequestLogStats(filter?: {
    from?: string
    to?: string
    clientId?: string
    sessionId?: string
  }): Promise<RequestLogStats> {
    const params = new URLSearchParams()
    if (filter?.from) params.set('from', filter.from)
    if (filter?.to) params.set('to', filter.to)
    if (filter?.clientId) params.set('clientId', filter.clientId)
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

  // ============== 用户别名 API ==============

  // 获取所有用户别名
  async getUserAliases(): Promise<Record<string, string>> {
    return this.request('/aliases')
  }

  // 设置用户别名
  async setUserAlias(userId: string, alias: string): Promise<{ message: string }> {
    return this.request(`/aliases/${encodeURIComponent(userId)}`, {
      method: 'PUT',
      body: JSON.stringify({ alias })
    })
  }

  // 删除用户别名
  async deleteUserAlias(userId: string): Promise<{ message: string }> {
    return this.request(`/aliases/${encodeURIComponent(userId)}`, {
      method: 'DELETE'
    })
  }

  // 批量导入别名（用于从 localStorage 迁移）
  async importUserAliases(aliases: Record<string, string>): Promise<{ message: string; imported: number }> {
    return this.request('/aliases/import', {
      method: 'POST',
      body: JSON.stringify(aliases)
    })
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
  async setModelPricing(
    model: string,
    pricing: ModelPricing
  ): Promise<{ message: string; model: string; pricing: ModelPricing }> {
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

  // ============== 模型别名配置 API ==============

  // 获取模型别名配置
  async getModelAliases(): Promise<AliasesConfig> {
    return this.request('/model-aliases')
  }

  // 更新模型别名配置
  async updateModelAliases(config: AliasesConfig): Promise<{ message: string; config: AliasesConfig }> {
    return this.request('/model-aliases', {
      method: 'PUT',
      body: JSON.stringify(config)
    })
  }

  // 重置模型别名配置为默认值
  async resetModelAliasesToDefault(): Promise<{ message: string; config: AliasesConfig }> {
    return this.request('/model-aliases/reset', {
      method: 'POST'
    })
  }

  // ============== 速率限制配置 API ==============

  // 获取速率限制配置
  async getRateLimitConfig(): Promise<RateLimitConfig> {
    return this.request('/ratelimit')
  }

  // 更新速率限制配置
  async updateRateLimitConfig(config: RateLimitConfig): Promise<{ message: string; config: RateLimitConfig }> {
    return this.request('/ratelimit', {
      method: 'PUT',
      body: JSON.stringify(config)
    })
  }

  // 重置速率限制配置为默认值
  async resetRateLimitConfig(): Promise<{ message: string; config: RateLimitConfig }> {
    return this.request('/ratelimit/reset', {
      method: 'POST'
    })
  }

  // ============== 调试日志配置 API ==============

  // 获取调试日志配置
  async getDebugLogConfig(): Promise<DebugLogConfig> {
    return this.request('/config/debug-log')
  }

  // 更新调试日志配置
  async updateDebugLogConfig(config: Partial<DebugLogConfig>): Promise<DebugLogConfig> {
    return this.request('/config/debug-log', {
      method: 'PUT',
      body: JSON.stringify(config)
    })
  }

  // ============== User-Agent 配置 API ==============

  // 获取 User-Agent 配置
  async getUserAgentConfig(): Promise<UserAgentConfig> {
    return this.request('/config/user-agent')
  }

  // 更新 User-Agent 配置
  async updateUserAgentConfig(config: Partial<UserAgentConfig>): Promise<UserAgentConfig> {
    return this.request('/config/user-agent', {
      method: 'PUT',
      body: JSON.stringify(config)
    })
  }

  // 获取请求的调试日志
  async getDebugLog(requestId: string): Promise<DebugLogEntry> {
    return this.request(`/logs/${encodeURIComponent(requestId)}/debug`)
  }

  // 清除所有调试日志
  async purgeDebugLogs(): Promise<{ message: string; deleted: number }> {
    return this.request('/logs/debug', { method: 'DELETE' })
  }

  // 获取调试日志统计
  async getDebugLogStats(): Promise<{ count: number }> {
    return this.request('/logs/debug/stats')
  }

  // ============== 故障转移配置 API ==============

  // 获取故障转移配置
  async getFailoverConfig(): Promise<FailoverConfig> {
    return this.request('/config/failover')
  }

  // 更新故障转移配置
  async updateFailoverConfig(config: Partial<FailoverConfig>): Promise<FailoverConfig> {
    return this.request('/config/failover', {
      method: 'PUT',
      body: JSON.stringify(config)
    })
  }

  // 重置故障转移配置为默认值
  async resetFailoverConfig(): Promise<FailoverConfig> {
    return this.request('/config/failover/reset', { method: 'POST' })
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

  // 获取版本信息（从 /api/health/details 端点，需要认证）
  async getVersion(): Promise<{ version: string; buildTime: string; gitCommit: string }> {
    try {
      const data = await this.request('/health/details')
      return data.version || { version: 'unknown', buildTime: 'unknown', gitCommit: 'unknown' }
    } catch {
      return { version: 'unknown', buildTime: 'unknown', gitCommit: 'unknown' }
    }
  }

  // ============== API Key 管理 API ==============

  // 获取 API Key 列表
  async getAPIKeys(filter?: { status?: string; limit?: number; offset?: number }): Promise<APIKeyListResponse> {
    const params = new URLSearchParams()
    if (filter?.status) params.set('status', filter.status)
    if (filter?.limit) params.set('limit', String(filter.limit))
    if (filter?.offset) params.set('offset', String(filter.offset))
    const query = params.toString() ? `?${params.toString()}` : ''
    return this.request(`/keys${query}`)
  }

  // 创建 API Key
  async createAPIKey(req: CreateAPIKeyRequest): Promise<CreateAPIKeyResponse> {
    return this.request('/keys', {
      method: 'POST',
      body: JSON.stringify(req)
    })
  }

  // 获取单个 API Key
  async getAPIKey(id: number): Promise<APIKey> {
    return this.request(`/keys/${id}`)
  }

  // 更新 API Key
  async updateAPIKey(id: number, req: UpdateAPIKeyRequest): Promise<APIKey> {
    return this.request(`/keys/${id}`, {
      method: 'PUT',
      body: JSON.stringify(req)
    })
  }

  // 删除 API Key
  async deleteAPIKey(id: number): Promise<{ message: string }> {
    return this.request(`/keys/${id}`, {
      method: 'DELETE'
    })
  }

  // 启用 API Key
  async enableAPIKey(id: number): Promise<{ message: string }> {
    return this.request(`/keys/${id}/enable`, {
      method: 'POST'
    })
  }

  // 禁用 API Key
  async disableAPIKey(id: number): Promise<{ message: string }> {
    return this.request(`/keys/${id}/disable`, {
      method: 'POST'
    })
  }

  // 撤销 API Key
  async revokeAPIKey(id: number): Promise<{ message: string }> {
    return this.request(`/keys/${id}/revoke`, {
      method: 'POST'
    })
  }

  // ============== Stats History API (for Charts) ==============

  // Get the URL for SSE log stream (used by useLogStream composable)
  getLogStreamURL(): string {
    const baseUrl = API_BASE.replace(/^\/api$/, '')
    const protocol = window.location.protocol === 'https:' ? 'https:' : 'http:'
    const host = import.meta.env.PROD
      ? window.location.host
      : import.meta.env.VITE_BACKEND_URL?.replace(/^https?:\/\//, '') || window.location.host
    return `${protocol}//${host}${API_BASE}/logs/stream`
  }

  // Get the API key for SSE connections
  getApiKeyForSSE(): string | null {
    return this.apiKey
  }

  // 获取报表统计数据 (enhanced GetStats with endpoint filter)
  async getReportStats(from: string, to: string, endpoint?: string): Promise<RequestLogStats> {
    const params = new URLSearchParams()
    params.set('from', from)
    params.set('to', to)
    if (endpoint) params.set('endpoint', endpoint)
    return this.request(`/logs/stats?${params.toString()}`)
  }

  // 获取每日聚合统计数据 (for report charts)
  async getReportDailyStats(from: string, to: string, endpoint?: string): Promise<DailyStatsResponse> {
    const params = new URLSearchParams()
    params.set('from', from)
    params.set('to', to)
    if (endpoint) params.set('endpoint', endpoint)
    return this.request(`/logs/stats/daily?${params.toString()}`)
  }

  // 获取全局统计历史数据
  async getStatsHistory(
    duration: Duration = '1h',
    endpoint?: string,
    from?: string,
    to?: string
  ): Promise<StatsHistoryResponse> {
    const params = new URLSearchParams()
    params.set('duration', duration)
    if (endpoint) params.set('endpoint', endpoint)
    if (from) params.set('from', from)
    if (to) params.set('to', to)
    return this.request(`/logs/stats/history?${params.toString()}`)
  }

  // 获取按 provider/channel 维度聚合的统计历史数据（用于成本/价格图表）
  async getProviderStatsHistory(
    duration: Duration = '1h',
    endpoint?: string,
    from?: string,
    to?: string
  ): Promise<ProviderStatsHistoryResponse> {
    const params = new URLSearchParams()
    params.set('duration', duration)
    if (endpoint) params.set('endpoint', endpoint)
    if (from) params.set('from', from)
    if (to) params.set('to', to)
    return this.request(`/logs/providers/stats/history?${params.toString()}`)
  }

  // 获取渠道统计历史数据
  async getChannelStatsHistory(
    channelId: number,
    duration: Duration = '1h',
    endpoint?: string
  ): Promise<ChannelStatsHistoryResponse> {
    const params = new URLSearchParams()
    params.set('duration', duration)
    if (endpoint) params.set('endpoint', endpoint)
    return this.request(`/logs/channels/${channelId}/stats/history?${params.toString()}`)
  }

  // ============== 用量配额 API ==============

  // 获取所有 Messages 渠道的用量配额状态
  async getAllChannelUsageQuotas(): Promise<Record<number, ChannelUsageStatus>> {
    return this.request('/channels/usage')
  }

  // 获取单个 Messages 渠道的用量配额状态
  async getChannelUsageQuota(channelId: number): Promise<ChannelUsageStatus> {
    return this.request(`/channels/${channelId}/usage`)
  }

  // 重置 Messages 渠道的用量配额
  async resetChannelUsageQuota(channelId: number): Promise<{ success: boolean; usage: ChannelUsageStatus }> {
    return this.request(`/channels/${channelId}/usage/reset`, {
      method: 'POST'
    })
  }

  // 获取所有 Responses 渠道的用量配额状态
  async getAllResponsesChannelUsageQuotas(): Promise<Record<number, ChannelUsageStatus>> {
    return this.request('/responses/channels/usage')
  }

  // 获取单个 Responses 渠道的用量配额状态
  async getResponsesChannelUsageQuota(channelId: number): Promise<ChannelUsageStatus> {
    return this.request(`/responses/channels/${channelId}/usage`)
  }

  // 重置 Responses 渠道的用量配额
  async resetResponsesChannelUsageQuota(channelId: number): Promise<{ success: boolean; usage: ChannelUsageStatus }> {
    return this.request(`/responses/channels/${channelId}/usage/reset`, {
      method: 'POST'
    })
  }

  // 获取所有 Gemini 渠道的用量配额状态
  async getAllGeminiChannelUsageQuotas(): Promise<Record<number, ChannelUsageStatus>> {
    return this.request('/gemini/channels/usage')
  }

  // 获取单个 Gemini 渠道的用量配额状态
  async getGeminiChannelUsageQuota(channelId: number): Promise<ChannelUsageStatus> {
    return this.request(`/gemini/channels/${channelId}/usage`)
  }

  // 重置 Gemini 渠道的用量配额
  async resetGeminiChannelUsageQuota(channelId: number): Promise<{ success: boolean; usage: ChannelUsageStatus }> {
    return this.request(`/gemini/channels/${channelId}/usage/reset`, {
      method: 'POST'
    })
  }

  // ============== Forward Proxy API ==============

  // Get forward proxy configuration
  async getForwardProxyConfig(): Promise<ForwardProxyConfig> {
    return this.request('/forward-proxy/config')
  }

  // Update forward proxy configuration
  async updateForwardProxyConfig(config: Partial<ForwardProxyConfig>): Promise<ForwardProxyConfig> {
    return this.request('/forward-proxy/config', {
      method: 'PUT',
      body: JSON.stringify(config)
    })
  }

  // Download CA certificate (returns blob URL for download)
  async downloadForwardProxyCACert(): Promise<Blob> {
    const baseUrl = API_BASE
    const headers: Record<string, string> = { Accept: 'application/x-pem-file' }
    if (this.apiKey) {
      headers['x-api-key'] = this.apiKey
    }
    const response = await fetch(`${baseUrl}/forward-proxy/ca-cert`, { headers })
    if (!response.ok) {
      throw new Error(`Failed to download CA certificate: ${response.statusText}`)
    }
    return response.blob()
  }

  async getForwardProxyDiscovery(): Promise<ForwardProxyDiscoveryResponse> {
    return this.request('/forward-proxy/discovery')
  }

  async clearForwardProxyDiscovery(): Promise<ForwardProxyDiscoveryResponse> {
    return this.request('/forward-proxy/discovery', {
      method: 'DELETE'
    })
  }
}

// 请求日志类型
export interface RequestLog {
  id: string
  status: 'pending' | 'completed' | 'error' | 'timeout'
  initialTime: string
  firstTokenTime?: string
  firstTokenDurationMs?: number
  completeTime: string
  durationMs: number
  type: string
  providerName: string
  model: string
  responseModel?: string // 响应中的模型名称（可能与请求不同）
  reasoningEffort?: string // Codex reasoning effort (low/medium/high/xhigh)
  serviceTier?: string // Codex service tier (e.g. priority for fast mode)
  serviceTierOverridden?: boolean // Whether the proxy forced service_tier=priority
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
  channelUid?: string
  channelName: string
  endpoint: string
  clientId?: string
  sessionId?: string
  apiKeyId?: number // API key ID for tracking
  error?: string
  upstreamError?: string // 上游服务原始错误信息
  failoverInfo?: string // Failover handling info (e.g., "429:QUOTA_EXHAUSTED > suspended > next channel")
  hasDebugData?: boolean // Whether debug data (headers/body) is available
  createdAt: string
}

export interface RequestLogFilter {
  provider?: string
  model?: string
  httpStatus?: number
  endpoint?: string
  clientId?: string
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
  success: number
  failure: number
  inputTokens: number
  outputTokens: number
  cacheCreationInputTokens: number
  cacheReadInputTokens: number
  cost: number
  avgLatencyMs: number
}

export interface RequestLogStats {
  totalRequests: number
  totalSuccess: number
  totalFailure: number
  totalTokens: {
    inputTokens: number
    outputTokens: number
    cacheCreationInputTokens: number
    cacheReadInputTokens: number
    totalTokens: number
  }
  totalCost: number
  avgLatencyMs: number
  p95LatencyMs: number
  byProvider: Record<string, GroupStats>
  byModel: Record<string, GroupStats>
  byClient: Record<string, GroupStats>
  bySession: Record<string, GroupStats>
  byApiKey: Record<string, GroupStats>
  timeRange: { from: string; to: string }
}

// Daily stats types for report
export interface DailyStatsDataPoint {
  date: string // YYYY-MM-DD
  requests: number
  success: number
  failure: number
  inputTokens: number
  outputTokens: number
  cacheCreationInputTokens: number
  cacheReadInputTokens: number
  cost: number
  avgDurationMs: number
}

export interface DailyStatsResponse {
  dataPoints: DailyStatsDataPoint[]
}

// 定价配置类型
export interface ModelPricing {
  inputPrice: number // 输入 token 价格 ($/1M tokens)
  outputPrice: number // 输出 token 价格 ($/1M tokens)
  cacheCreationPrice?: number | null // 缓存创建价格 ($/1M tokens)，null/undefined 表示使用默认值，0 表示免费
  cacheReadPrice?: number | null // 缓存读取价格 ($/1M tokens)，null/undefined 表示使用默认值，0 表示免费
  description?: string // 模型描述
  exportToModels?: boolean // 是否导出到 /v1/models API，undefined 或 true 时导出
}

export interface PricingConfig {
  models: Record<string, ModelPricing>
  currency: string
}

// 模型别名配置类型
export interface ModelAlias {
  value: string
  description?: string
}

export interface AliasesConfig {
  messagesModels: ModelAlias[]
  responsesModels: ModelAlias[]
  geminiModels: ModelAlias[]
}

// 渠道价格乘数配置（用于渠道级别的折扣）
export interface TokenPriceMultipliers {
  inputMultiplier?: number // 输入 token 价格乘数，默认 1.0
  outputMultiplier?: number // 输出 token 价格乘数，默认 1.0
  cacheCreationMultiplier?: number // 缓存创建价格乘数，默认 1.0
  cacheReadMultiplier?: number // 缓存读取价格乘数，默认 1.0
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

// Rate Limit Configuration Types
export interface EndpointRateLimit {
  enabled: boolean
  requestsPerMinute: number
}

export interface AuthFailureThreshold {
  failures: number
  blockMinutes: number
}

export interface AuthFailureConfig {
  enabled: boolean
  thresholds: AuthFailureThreshold[]
}

export interface RateLimitConfig {
  api: EndpointRateLimit
  portal: EndpointRateLimit
  authFailure: AuthFailureConfig
}

// Debug Log Configuration Types
export interface DebugLogConfig {
  enabled: boolean
  retentionHours: number
  maxBodySize: number
}

export interface UserAgentEndpointConfig {
  latest: string
  lastCapturedAt?: string
}

export interface UserAgentConfig {
  messages: UserAgentEndpointConfig
  responses: UserAgentEndpointConfig
}

// Debug Log Entry
export interface DebugLogEntry {
  requestId: string
  requestHeaders: Record<string, string>
  requestHeadersRaw?: Record<string, string>
  requestBody: string
  responseHeaders: Record<string, string>
  responseHeadersRaw?: Record<string, string>
  responseBody: string
  createdAt: string
}

// 活跃会话类型
export interface ActiveSession {
  sessionId: string
  type: string // claude, openai, codex, responses
  firstRequestTime: string
  lastRequestTime: string
  count: number
  inputTokens: number
  outputTokens: number
  cacheCreationInputTokens: number
  cacheReadInputTokens: number
  cost: number
}

// API Key 类型
export type APIKeyStatus = 'active' | 'disabled' | 'revoked'

export interface APIKey {
  id: number
  name: string
  keyPrefix: string
  description: string
  status: APIKeyStatus
  isAdmin: boolean
  rateLimitRpm: number // Requests per minute (0 = use global)
  createdAt: string
  updatedAt: string
  lastUsedAt?: string
  // Permission fields (nil/empty = unrestricted)
  allowedEndpoints?: string[] // ["messages"], ["responses"], ["gemini"], ["messages_current_channel"], or any combination
  allowedChannelsMsg?: string[] // stable channel IDs for /v1/messages
  allowedChannelsResp?: string[] // stable channel IDs for /v1/responses
  allowedChannelsGemini?: string[] // stable channel IDs for /v1/gemini (GeminiUpstream)
  allowedChannelsChat?: string[] // stable channel IDs for /v1/chat/completions (ChatUpstream)
  allowedModels?: string[] // glob patterns: ["claude-sonnet-*"]
}

export interface CreateAPIKeyRequest {
  name: string
  description?: string
  isAdmin: boolean
  rateLimitRpm?: number
  // Permission fields (nil/empty = unrestricted)
  allowedEndpoints?: string[]
  allowedChannelsMsg?: string[]
  allowedChannelsResp?: string[]
  allowedChannelsGemini?: string[]
  allowedChannelsChat?: string[]
  allowedModels?: string[]
}

export interface CreateAPIKeyResponse extends APIKey {
  key: string // Full key, only returned once on creation
}

export interface UpdateAPIKeyRequest {
  name?: string
  description?: string
  rateLimitRpm?: number
  // Permission fields (nil = no change, empty array = clear/unrestrict)
  allowedEndpoints?: string[]
  allowedChannelsMsg?: string[]
  allowedChannelsResp?: string[]
  allowedChannelsGemini?: string[]
  allowedChannelsChat?: string[]
  allowedModels?: string[]
}

export interface APIKeyListResponse {
  keys: APIKey[]
  total: number
  hasMore: boolean
}

// ============== Stats History Types (for Charts) ==============

export type Duration = '1h' | '6h' | '24h' | 'today' | 'period'

export interface StatsHistoryDataPoint {
  timestamp: string
  requests: number
  success: number
  failure: number
  avgDurationMs: number
  p50DurationMs: number
  p95DurationMs: number
  inputTokens: number
  outputTokens: number
  cacheCreationInputTokens: number
  cacheReadInputTokens: number
  cost: number
}

export interface StatsHistorySummary {
  totalRequests: number
  totalSuccess: number
  totalFailure: number
  totalInputTokens: number
  totalOutputTokens: number
  totalCacheCreationTokens: number
  totalCacheReadTokens: number
  totalCost: number
  avgSuccessRate: number
  avgDurationMs: number
  p50DurationMs: number
  p95DurationMs: number
  duration: string
}

export interface StatsHistoryResponse {
  dataPoints: StatsHistoryDataPoint[]
  summary: StatsHistorySummary
}

export interface ChannelStatsHistoryResponse {
  channelId: number
  channelName: string
  dataPoints: StatsHistoryDataPoint[]
  summary: StatsHistorySummary
}

export interface ProviderStatsHistorySeries {
  provider: string
  baselineCost: number
  dataPoints: StatsHistoryDataPoint[]
  summary: StatsHistorySummary
}

export interface ProviderStatsHistoryResponse {
  providers: ProviderStatsHistorySeries[]
  summary: StatsHistorySummary
}

// Failover Configuration Types
// New action types for action chain
export type FailoverActionType = 'retry' | 'failover' | 'suspend' | 'none'

// Legacy action types (deprecated, for backward compatibility display only)
export type LegacyFailoverAction = 'failover_immediate' | 'failover_threshold' | 'retry_wait' | 'suspend_channel'

// Single step in the action chain
export interface ActionStep {
  action: FailoverActionType // Action type: "retry", "failover", "suspend", "none"
  waitSeconds?: number // Wait seconds before retry (0 = auto-detect from response body, only for retry)
  maxAttempts?: number // Max retry attempts (only for retry, 99 = indefinite)
}

export interface FailoverRule {
  errorCodes: string // Error code pattern: "401,403" or "429:QUOTA_EXHAUSTED" or "others"
  actionChain?: ActionStep[] // Action chain (new format)
  // Legacy fields (deprecated, used for migration detection only)
  action?: LegacyFailoverAction // [Deprecated] Legacy action type
  threshold?: number // [Deprecated] For failover_threshold: consecutive errors before failover
  waitSeconds?: number // [Deprecated] For retry_wait: seconds to wait
}

export interface FailoverConfig {
  enabled: boolean
  rules: FailoverRule[]
  // 429 smart handling config (Claude API only)
  genericResourceWaitSeconds?: number
  modelCooldownExtraSeconds?: number
  modelCooldownMaxWaitSeconds?: number
}

// Forward Proxy Configuration
export interface XInitiatorOverrideConfig {
  enabled: boolean
  mode: 'fixed_window' | 'relative_countdown'
  durationSeconds: number
}

export interface XInitiatorOverrideRuntimeStatus {
  enabled: boolean
  mode: 'fixed_window' | 'relative_countdown'
  activeDomains: number
  nearestExpiryAt?: string
  nearestRemainingSeconds: number
}

export interface ForwardProxyConfig {
  enabled: boolean
  interceptDomains: string[]
  xInitiatorOverride: XInitiatorOverrideConfig
  xInitiatorOverrideRuntime: XInitiatorOverrideRuntimeStatus
  running: boolean
  port: number
}

export interface ForwardProxyDiscoveryEntry {
  host: string
  port: string
  transport: string
  intercepted: boolean
  seenCount: number
  firstSeenAt: string
  lastSeenAt: string
  lastMethod?: string
  lastPath?: string
  lastRequestHeaders?: Record<string, string>
  lastRequestHeadersRaw?: Record<string, string>
}

export interface ForwardProxyDiscoveryResponse {
  entries: ForwardProxyDiscoveryEntry[]
  running: boolean
}

export const api = new ApiService()
export default api
