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
  test('clears quota polling on unmount', async () => {
    const source = await readChannelOrchestrationSource()

    expect(source).toContain('const clearUsageQuotaRefreshTimer = () => {')
    expect(source).toContain('const clearOAuthQuotaRefreshTimer = () => {')
    const unmountedBlock = source.match(/onUnmounted\(\(\) => \{[\s\S]*?\n\}\)/)?.[0] ?? ''

    expect(unmountedBlock).toContain('clearOAuthQuotaResetTimer()')
    expect(unmountedBlock).toContain('clearOAuthQuotaRefreshTimer()')
    expect(unmountedBlock).toContain('clearUsageQuotaRefreshTimer()')
  })

  test('restarts quota polling after tab changes by clearing the old intervals first', async () => {
    const source = await readChannelOrchestrationSource()
    const watcherBlock =
      source.match(/watch\(\s*\n\s*\(\) => props\.channelType,[\s\S]*?\n\s*\)\n\nconst channelQuotaCacheKey/)?.[0] ?? ''

    expect(watcherBlock).toContain('clearOAuthQuotaRefreshTimer()')
    expect(watcherBlock).toContain('clearUsageQuotaRefreshTimer()')
    expect(watcherBlock).toContain('oauthQuotaRefreshTimer = setInterval')
    expect(watcherBlock).toContain('usageQuotaRefreshTimer = setInterval')
    expect(watcherBlock.indexOf('clearOAuthQuotaRefreshTimer()')).toBeLessThan(
      watcherBlock.indexOf('oauthQuotaRefreshTimer = setInterval')
    )
    expect(watcherBlock.indexOf('clearUsageQuotaRefreshTimer()')).toBeLessThan(
      watcherBlock.indexOf('usageQuotaRefreshTimer = setInterval')
    )
  })

  test('quota polling only refreshes while the tab is visible', async () => {
    const source = await readChannelOrchestrationSource()
    const oauthIntervalBlocks = [
      ...source.matchAll(/oauthQuotaRefreshTimer = setInterval\(\(\) => \{[\s\S]*?\n\s*\}, 10000\)/g)
    ]
    const intervalBlocks = [...source.matchAll(/usageQuotaRefreshTimer = setInterval\(\(\) => \{[\s\S]*?\n\s*\}, 10000\)/g)]

    expect(oauthIntervalBlocks.length).toBeGreaterThanOrEqual(2)
    for (const block of oauthIntervalBlocks) {
      expect(block[0]).toContain("document.visibilityState === 'visible'")
      expect(block[0]).toContain('fetchOAuthQuotas()')
    }

    expect(intervalBlocks.length).toBeGreaterThanOrEqual(2)
    for (const block of intervalBlocks) {
      expect(block[0]).toContain("document.visibilityState === 'visible'")
      expect(block[0]).toContain('fetchUsageQuotas()')
    }
  })
})
