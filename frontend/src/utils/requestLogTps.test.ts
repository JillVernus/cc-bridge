import { describe, expect, test } from 'bun:test'

import { calculateRequestLogTps, formatRequestLogTps } from './requestLogTps'

describe('requestLogTps', () => {
  test('calculates post-first-token TPS for a completed request', () => {
    const tps = calculateRequestLogTps({
      status: 'completed',
      outputTokens: 120,
      durationMs: 5000,
      firstTokenDurationMs: 2000
    })

    expect(tps).toBe(40)
    expect(formatRequestLogTps(tps)).toBe('40.0 tok/s')
  })

  test('returns null for pending requests', () => {
    expect(
      calculateRequestLogTps({
        status: 'pending',
        outputTokens: 120,
        durationMs: 5000,
        firstTokenDurationMs: 2000
      })
    ).toBeNull()
  })

  test('returns null when first-token timing is missing', () => {
    expect(
      calculateRequestLogTps({
        status: 'completed',
        outputTokens: 120,
        durationMs: 5000
      })
    ).toBeNull()
  })

  test('returns null when generation window is zero or negative', () => {
    expect(
      calculateRequestLogTps({
        status: 'completed',
        outputTokens: 120,
        durationMs: 2000,
        firstTokenDurationMs: 2000
      })
    ).toBeNull()
  })

  test('returns null when output tokens are zero', () => {
    expect(
      calculateRequestLogTps({
        status: 'completed',
        outputTokens: 0,
        durationMs: 5000,
        firstTokenDurationMs: 2000
      })
    ).toBeNull()
    expect(formatRequestLogTps(null)).toBe('-')
  })
})
