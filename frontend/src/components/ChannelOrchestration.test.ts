import { describe, expect, test } from 'bun:test'

const channelOrchestrationSourcePath = new URL('./ChannelOrchestration.vue', import.meta.url)

const readChannelOrchestrationSource = async () => Bun.file(channelOrchestrationSourcePath).text()

describe('ChannelOrchestration recent calls display', () => {
  test('uses a compact two-line layout at narrow desktop widths', async () => {
    const source = await readChannelOrchestrationSource()

    expect(source).toContain('@media (max-width: 1280px)')
    expect(source).toContain('grid-template-columns: repeat(10, 6px)')
    expect(source).toContain('grid-auto-rows: 6px')
    expect(source).toContain('recent-calls-display')
    expect(source).toContain('recent-calls-blocks')
  })
})

describe('ChannelOrchestration timer lifecycle guards', () => {
  test('clears usage quota polling on unmount', async () => {
    const source = await readChannelOrchestrationSource()

    expect(source).toContain('const clearUsageQuotaRefreshTimer = () => {')
    const unmountedBlock = source.match(/onUnmounted\(\(\) => \{[\s\S]*?\n\}\)/)?.[0] ?? ''

    expect(unmountedBlock).toContain('clearOAuthQuotaResetTimer()')
    expect(unmountedBlock).toContain('clearUsageQuotaRefreshTimer()')
  })

  test('restarts usage quota polling after tab changes by clearing the old interval first', async () => {
    const source = await readChannelOrchestrationSource()
    const watcherBlock =
      source.match(/watch\(\s*\n\s*\(\) => props\.channelType,[\s\S]*?\n\s*\)\n\nconst channelQuotaCacheKey/)?.[0] ?? ''

    expect(watcherBlock).toContain('clearUsageQuotaRefreshTimer()')
    expect(watcherBlock).toContain('usageQuotaRefreshTimer = setInterval')
    expect(watcherBlock.indexOf('clearUsageQuotaRefreshTimer()')).toBeLessThan(
      watcherBlock.indexOf('usageQuotaRefreshTimer = setInterval')
    )
  })

  test('usage quota polling only refreshes while the tab is visible', async () => {
    const source = await readChannelOrchestrationSource()
    const intervalBlocks = [...source.matchAll(/usageQuotaRefreshTimer = setInterval\(\(\) => \{[\s\S]*?\n\s*\}, 10000\)/g)]

    expect(intervalBlocks.length).toBeGreaterThanOrEqual(2)
    for (const block of intervalBlocks) {
      expect(block[0]).toContain("document.visibilityState === 'visible'")
      expect(block[0]).toContain('fetchUsageQuotas()')
    }
  })
})
