type RequestLogTpsSource = {
  status?: string
  stream?: boolean
  outputTokens?: number
  durationMs?: number
  firstTokenDurationMs?: number
}

export const calculateRequestLogStreamingDurationMs = (item: RequestLogTpsSource): number | null => {
  if (!item || item.status === 'pending' || !item.stream) return null

  const durationMs = item.durationMs ?? 0
  const firstTokenDurationMs = item.firstTokenDurationMs

  if (typeof firstTokenDurationMs !== 'number') return null

  const generationWindowMs = durationMs - firstTokenDurationMs
  if (generationWindowMs <= 0) return null

  return generationWindowMs
}

export const calculateRequestLogTps = (item: RequestLogTpsSource): number | null => {
  if (!item || item.status === 'pending') return null

  const outputTokens = item.outputTokens ?? 0
  const generationWindowMs = calculateRequestLogStreamingDurationMs(item)

  if (outputTokens <= 0) return null
  if (generationWindowMs === null) return null

  return outputTokens / (generationWindowMs / 1000)
}

export const formatRequestLogDurationCompact = (ms: number | null): string => {
  if (ms === null || !Number.isFinite(ms) || ms < 0) return '-'
  if (ms >= 1000000) return (ms / 1000000).toFixed(1) + 'M'
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 'K'
  return String(ms)
}

export const formatRequestLogTps = (tps: number | null): string => {
  if (tps === null || !Number.isFinite(tps) || tps <= 0) return '-'
  return tps.toFixed(1)
}
