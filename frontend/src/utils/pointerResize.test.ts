import { describe, expect, test } from 'bun:test'

import {
  POINTER_RESIZE_EVENTS,
  calculatePointerResizeWidth,
  capturePointerResize,
  releasePointerResize
} from './pointerResize'

describe('pointerResize', () => {
  test('calculates constrained resize widths from pointer movement', () => {
    expect(
      calculatePointerResizeWidth({ startX: 100, startWidth: 180, currentX: 140, minWidth: 50, maxWidth: 500 })
    ).toBe(220)
    expect(
      calculatePointerResizeWidth({ startX: 100, startWidth: 180, currentX: -100, minWidth: 50, maxWidth: 500 })
    ).toBe(50)
    expect(
      calculatePointerResizeWidth({ startX: 100, startWidth: 480, currentX: 200, minWidth: 50, maxWidth: 500 })
    ).toBe(500)
  })

  test('uses pointer events for resize tracking', () => {
    expect(POINTER_RESIZE_EVENTS.move).toBe('pointermove')
    expect(POINTER_RESIZE_EVENTS.stop).toBe('pointerup')
    expect(POINTER_RESIZE_EVENTS.cancel).toBe('pointercancel')
  })

  test('captures and releases the active pointer when supported', () => {
    const calls: string[] = []
    const target = {
      setPointerCapture(pointerId: number) {
        calls.push(`capture:${pointerId}`)
      },
      releasePointerCapture(pointerId: number) {
        calls.push(`release:${pointerId}`)
      }
    }

    capturePointerResize({ currentTarget: target, pointerId: 7 })
    releasePointerResize(target, 7)

    expect(calls).toEqual(['capture:7', 'release:7'])
  })

  test('ignores pointer capture errors during resize cleanup', () => {
    const target = {
      setPointerCapture() {
        throw new Error('pointer is not capturable')
      },
      releasePointerCapture() {
        throw new Error('pointer capture already released')
      }
    }

    expect(() => capturePointerResize({ currentTarget: target, pointerId: 7 })).not.toThrow()
    expect(() => releasePointerResize(target, 7)).not.toThrow()
  })
})
