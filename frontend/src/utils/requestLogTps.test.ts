import { describe, expect, test } from 'bun:test'

import { calculateRequestLogStreamingDurationMs, calculateRequestLogTps, formatRequestLogDurationCompact, formatRequestLogTps } from './requestLogTps'

describe('requestLogTps', () => {
  test('calculates end-to-end TPS for a completed request', () => {
    const tps = calculateRequestLogTps({
      status: 'completed',
      stream: true,
      outputTokens: 120,
      durationMs: 5000,
      firstTokenDurationMs: 2000
    })

    expect(tps).toBe(24)
    expect(formatRequestLogTps(tps)).toBe('24.0')
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

  test('does not require first-token timing to calculate TPS', () => {
    expect(
      calculateRequestLogTps({
        status: 'completed',
        stream: true,
        outputTokens: 120,
        durationMs: 5000
      })
    ).toBe(24)
  })

  test('returns null when total duration is zero or negative', () => {
    expect(
      calculateRequestLogTps({
        status: 'completed',
        stream: true,
        outputTokens: 120,
        durationMs: 0,
        firstTokenDurationMs: 2000
      })
    ).toBeNull()

    expect(
      calculateRequestLogTps({
        status: 'completed',
        stream: true,
        outputTokens: 120,
        durationMs: -1,
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

  test('returns null streaming duration for non-streaming requests but still calculates TPS', () => {
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
    ).toBe(24)
  })

  test('formats compact durations as ms below one second and seconds above it', () => {
    expect(formatRequestLogDurationCompact(842)).toBe('842')
    expect(formatRequestLogDurationCompact(1234)).toBe('1.23s')
  })
})
