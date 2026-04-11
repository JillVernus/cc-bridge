type RequestLogTpsSource = {
  status?: string
  outputTokens?: number
  durationMs?: number
  firstTokenDurationMs?: number
}

export const calculateRequestLogTps = (item: RequestLogTpsSource): number | null => {
  if (!item || item.status === 'pending') return null

  const outputTokens = item.outputTokens ?? 0
  const durationMs = item.durationMs ?? 0
  const firstTokenDurationMs = item.firstTokenDurationMs

  if (outputTokens <= 0) return null
  if (typeof firstTokenDurationMs !== 'number') return null

  const generationWindowMs = durationMs - firstTokenDurationMs
  if (generationWindowMs <= 0) return null

  return outputTokens / (generationWindowMs / 1000)
}

export const formatRequestLogTps = (tps: number | null): string => {
  if (tps === null || !Number.isFinite(tps) || tps <= 0) return '-'
  return `${tps.toFixed(1)} tok/s`
}
