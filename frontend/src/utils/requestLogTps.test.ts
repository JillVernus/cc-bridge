import { describe, expect, test } from 'bun:test'

import { calculateRequestLogStreamingDurationMs, calculateRequestLogTps, formatRequestLogDurationCompact, formatRequestLogTps } from './requestLogTps'

describe('requestLogTps', () => {
  test('calculates post-first-token TPS for a completed request', () => {
    const tps = calculateRequestLogTps({
      status: 'completed',
      stream: true,
      outputTokens: 120,
      durationMs: 5000,
      firstTokenDurationMs: 2000
    })

    expect(tps).toBe(40)
    expect(formatRequestLogTps(tps)).toBe('40.0')
    expect(
      calculateRequestLogStreamingDurationMs({
        status: 'completed',
        stream: true,
        durationMs: 5000,
        firstTokenDurationMs: 2000
      })
    ).toBe(3000)
    expect(formatRequestLogDurationCompact(3000)).toBe('3.00s')
  })

  test('returns null for pending requests', () => {
    expect(
      calculateRequestLogTps({
        status: 'pending',
        stream: true,
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
        stream: true,
        outputTokens: 120,
        durationMs: 5000
      })
    ).toBeNull()
  })

  test('returns null when generation window is zero or negative', () => {
    expect(
      calculateRequestLogTps({
        status: 'completed',
        stream: true,
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
        stream: true,
        outputTokens: 0,
        durationMs: 5000,
        firstTokenDurationMs: 2000
      })
    ).toBeNull()
    expect(formatRequestLogTps(null)).toBe('-')
  })

  test('returns null streaming duration and TPS for non-streaming requests', () => {
    expect(
      calculateRequestLogStreamingDurationMs({
        status: 'completed',
        stream: false,
        durationMs: 5000,
        firstTokenDurationMs: 2000
      })
    ).toBeNull()

    expect(
      calculateRequestLogTps({
        status: 'completed',
        stream: false,
        outputTokens: 120,
        durationMs: 5000,
        firstTokenDurationMs: 2000
      })
    ).toBeNull()
  })

  test('formats compact durations as ms below one second and seconds above it', () => {
    expect(formatRequestLogDurationCompact(842)).toBe('842')
    expect(formatRequestLogDurationCompact(1234)).toBe('1.23s')
  })
})
