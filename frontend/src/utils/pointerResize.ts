export const POINTER_RESIZE_EVENTS = {
  move: 'pointermove',
  stop: 'pointerup',
  cancel: 'pointercancel'
} as const

export interface PointerResizeBounds {
  startX: number
  startWidth: number
  currentX: number
  minWidth: number
  maxWidth?: number
}

export interface PointerCaptureTarget {
  setPointerCapture?: (pointerId: number) => void
  releasePointerCapture?: (pointerId: number) => void
}

export interface PointerCaptureEvent {
  currentTarget: unknown
  pointerId: number
}

export const calculatePointerResizeWidth = ({
  startX,
  startWidth,
  currentX,
  minWidth,
  maxWidth
}: PointerResizeBounds): number => {
  const resizedWidth = Math.max(minWidth, startWidth + currentX - startX)
  return typeof maxWidth === 'number' ? Math.min(maxWidth, resizedWidth) : resizedWidth
}

export const capturePointerResize = ({
  currentTarget,
  pointerId
}: PointerCaptureEvent): PointerCaptureTarget | null => {
  const target = currentTarget as PointerCaptureTarget | null
  try {
    target?.setPointerCapture?.(pointerId)
  } catch {
    // Pointer capture is best-effort; resize still works via document-level pointer listeners.
  }
  return target
}

export const releasePointerResize = (target: PointerCaptureTarget | null, pointerId: number | null): void => {
  if (pointerId === null) return
  try {
    target?.releasePointerCapture?.(pointerId)
  } catch {
    // The browser may have already released capture after pointerup/cancel.
  }
}
