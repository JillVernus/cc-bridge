const notFoundMarkers = ['debug log not found', '404']

export const isRetryableDebugLogError = (error: unknown): boolean => {
  if (!(error instanceof Error)) {
    return false
  }

  const message = error.message.trim().toLowerCase()
  return notFoundMarkers.some(marker => message.includes(marker))
}
