import { describe, expect, test } from 'bun:test'

const appSourcePath = new URL('./App.vue', import.meta.url)

const readAppSource = async () => Bun.file(appSourcePath).text()

const extractNavButtonValues = (source: string) => {
  const toggleMatch = source.match(/<v-btn-toggle[\s\S]*?<\/v-btn-toggle>/)
  expect(toggleMatch).not.toBeNull()

  return Array.from(toggleMatch![0].matchAll(/<v-btn\s+value="([^"]+)"/g), match => match[1])
}

const extractInitialActiveTab = (source: string) => {
  const activeTabMatch = source.match(/const activeTab = ref<[\s\S]*?>\('([^']+)'\)/)
  expect(activeTabMatch).not.toBeNull()

  return activeTabMatch![1]
}

describe('App navigation tabs', () => {
  test('shows Logs first, then Codex and Claude, while preserving the remaining tab order', async () => {
    const source = await readAppSource()

    expect(extractNavButtonValues(source)).toEqual([
      'logs',
      'responses',
      'messages',
      'gemini',
      'chat',
      'apikeys',
      'report',
      'forward-proxy-discovery'
    ])
  })

  test('defaults to the Logs tab', async () => {
    const source = await readAppSource()

    expect(extractInitialActiveTab(source)).toBe('logs')
  })

  test('does not mount protected main content before authentication', async () => {
    const source = await readAppSource()

    expect(source).toContain('<v-container v-if="isAuthenticated"')
  })
})
