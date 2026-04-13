import { describe, expect, test } from 'bun:test'

import { isRetryableDebugLogError } from './debugLogRetry'

describe('isRetryableDebugLogError', () => {
  test('retries when backend says debug log not found', () => {
    expect(isRetryableDebugLogError(new Error('Debug log not found'))).toBe(true)
  })

  test('retries when the message includes 404', () => {
    expect(isRetryableDebugLogError(new Error('Request failed with status 404'))).toBe(true)
  })

  test('does not retry on unrelated errors', () => {
    expect(isRetryableDebugLogError(new Error('认证失败，请重新输入访问密钥'))).toBe(false)
    expect(isRetryableDebugLogError(new Error('internal server error'))).toBe(false)
  })
})
