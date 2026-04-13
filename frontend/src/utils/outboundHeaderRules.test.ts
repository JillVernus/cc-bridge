import { describe, expect, test } from 'bun:test'

import { appendUniqueHeaderRule } from './outboundHeaderRules'

describe('appendUniqueHeaderRule', () => {
  test('adds a new rule when it is not already present', () => {
    expect(appendUniqueHeaderRule(['Cf-*', 'X-Forwarded-*'], 'True-Client-IP')).toEqual([
      'Cf-*',
      'X-Forwarded-*',
      'True-Client-IP'
    ])
  })

  test('treats rules case-insensitively and does not duplicate them', () => {
    expect(appendUniqueHeaderRule(['Cf-*', 'X-Forwarded-*'], 'cf-*')).toEqual(['Cf-*', 'X-Forwarded-*'])
  })

  test('ignores blank input', () => {
    expect(appendUniqueHeaderRule(['Cf-*'], '   ')).toEqual(['Cf-*'])
  })
})
