import { ref, onUnmounted, type Ref } from 'vue'
import api from '../services/api'

// SSE Event types (matching backend events.go)
export type SSEEventType = 'log:created' | 'log:updated' | 'log:debugdata' | 'log:stats' | 'heartbeat' | 'connected'

export interface LogCreatedPayload {
  id: string
  status: string
  durationMs?: number
  httpStatus?: number
  type?: string
  providerName: string
  model: string
  responseModel?: string
  channelId: number
  channelName: string
  endpoint: string
  stream: boolean
  inputTokens?: number
  outputTokens?: number
  cacheCreationInputTokens?: number
  cacheReadInputTokens?: number
  totalTokens?: number
  price?: number
  inputCost?: number
  outputCost?: number
  cacheCreationCost?: number
  cacheReadCost?: number
  apiKeyId?: number
  hasDebugData?: boolean
  clientId?: string
  sessionId?: string
  reasoningEffort?: string
  error?: string
  upstreamError?: string
  failoverInfo?: string
  initialTime: string
  completeTime?: string
}

export interface LogUpdatedPayload {
  id: string
  status: string
  durationMs: number
  httpStatus: number
  type: string
  providerName: string
  channelId: number
  channelName: string
  inputTokens: number
  outputTokens: number
  cacheCreationInputTokens: number
  cacheReadInputTokens: number
  totalTokens: number
  price: number
  inputCost: number
  outputCost: number
  cacheCreationCost: number
  cacheReadCost: number
  apiKeyId?: number
  hasDebugData: boolean
  error?: string
  upstreamError?: string
  failoverInfo?: string
  responseModel?: string
  reasoningEffort?: string
  completeTime: string
}

export interface StatsPayload {
  totalRequests: number
  totalCost: number
  activeSessions: Array<{
    sessionId: string
    type: string
    firstRequestTime: string
    lastRequestTime: string
    count: number
    inputTokens: number
    outputTokens: number
    cost: number
  }>
  byProvider?: Record<string, { count: number; cost: number }>
}

export interface LogDebugDataPayload {
  id: string
  hasDebugData: boolean
}

export interface SSEEvent<T = unknown> {
  type: SSEEventType
  data: T
  timestamp: string
}

export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error'

export interface UseLogStreamOptions {
  onLogCreated?: (payload: LogCreatedPayload) => void
  onLogUpdated?: (payload: LogUpdatedPayload) => void
  onLogDebugData?: (payload: LogDebugDataPayload) => void
  onStats?: (payload: StatsPayload) => void
  onConnectionChange?: (state: ConnectionState) => void
  maxRetries?: number
  autoConnect?: boolean
}

const MAX_BACKOFF_MS = 16000
const INITIAL_BACKOFF_MS = 1000

export function useLogStream(options: UseLogStreamOptions = {}) {
  const {
    onLogCreated,
    onLogUpdated,
    onLogDebugData,
    onStats,
    onConnectionChange,
    maxRetries = 5,
    autoConnect = false
  } = options

  const connectionState: Ref<ConnectionState> = ref('disconnected')
  const retryCount = ref(0)
  const isPollingFallback = ref(false)

  let eventSource: EventSource | null = null
  let reconnectTimeout: ReturnType<typeof setTimeout> | null = null

  const setConnectionState = (state: ConnectionState) => {
    connectionState.value = state
    onConnectionChange?.(state)
  }

  const connect = () => {
    if (eventSource) {
      eventSource.close()
    }

    const apiKey = api.getApiKeyForSSE()
    if (!apiKey) {
      console.warn('🔒 SSE: No API key available, cannot connect')
      setConnectionState('error')
      return
    }

    const url = `${api.getLogStreamURL()}?key=${encodeURIComponent(apiKey)}`
    setConnectionState('connecting')

    try {
      eventSource = new EventSource(url)

      eventSource.onopen = () => {
        console.log('📡 SSE: Connected')
        retryCount.value = 0
        setConnectionState('connected')
        isPollingFallback.value = false
      }

      eventSource.onerror = (error) => {
        console.error('📡 SSE: Error', error)
        eventSource?.close()
        eventSource = null
        setConnectionState('error')
        scheduleReconnect()
      }

      // Handle custom event types
      eventSource.addEventListener('connected', (e: MessageEvent) => {
        try {
          const data = JSON.parse(e.data)
          console.log('📡 SSE: Server acknowledged connection', data.clientId)
        } catch {
          console.log('📡 SSE: Connected event received')
        }
      })

      eventSource.addEventListener('log:created', (e: MessageEvent) => {
        try {
          const event: SSEEvent<LogCreatedPayload> = JSON.parse(e.data)
          onLogCreated?.(event.data)
        } catch (err) {
          console.error('📡 SSE: Failed to parse log:created event', err)
        }
      })

      eventSource.addEventListener('log:updated', (e: MessageEvent) => {
        try {
          const event: SSEEvent<LogUpdatedPayload> = JSON.parse(e.data)
          onLogUpdated?.(event.data)
        } catch (err) {
          console.error('📡 SSE: Failed to parse log:updated event', err)
        }
      })

      eventSource.addEventListener('log:debugdata', (e: MessageEvent) => {
        try {
          const event: SSEEvent<LogDebugDataPayload> = JSON.parse(e.data)
          onLogDebugData?.(event.data)
        } catch (err) {
          console.error('📡 SSE: Failed to parse log:debugdata event', err)
        }
      })

      eventSource.addEventListener('log:stats', (e: MessageEvent) => {
        try {
          const event: SSEEvent<StatsPayload> = JSON.parse(e.data)
          onStats?.(event.data)
        } catch (err) {
          console.error('📡 SSE: Failed to parse log:stats event', err)
        }
      })

      eventSource.addEventListener('heartbeat', () => {
        // Just a keep-alive, no action needed
      })

    } catch (err) {
      console.error('📡 SSE: Failed to create EventSource', err)
      setConnectionState('error')
      scheduleReconnect()
    }
  }

  const scheduleReconnect = () => {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout)
    }

    if (retryCount.value >= maxRetries) {
      console.warn(`📡 SSE: Max retries (${maxRetries}) reached, falling back to polling`)
      isPollingFallback.value = true
      setConnectionState('disconnected')
      return
    }

    const backoff = Math.min(INITIAL_BACKOFF_MS * Math.pow(2, retryCount.value), MAX_BACKOFF_MS)
    console.log(`📡 SSE: Reconnecting in ${backoff}ms (attempt ${retryCount.value + 1}/${maxRetries})`)

    reconnectTimeout = setTimeout(() => {
      retryCount.value++
      connect()
    }, backoff)
  }

  const disconnect = () => {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout)
      reconnectTimeout = null
    }
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
    retryCount.value = 0
    setConnectionState('disconnected')
  }

  const resetRetries = () => {
    retryCount.value = 0
    isPollingFallback.value = false
  }

  // Auto-connect if enabled
  if (autoConnect) {
    connect()
  }

  // Cleanup on unmount
  onUnmounted(() => {
    disconnect()
  })

  return {
    connectionState,
    isPollingFallback,
    retryCount,
    connect,
    disconnect,
    resetRetries
  }
}
