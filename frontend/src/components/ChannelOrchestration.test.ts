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
