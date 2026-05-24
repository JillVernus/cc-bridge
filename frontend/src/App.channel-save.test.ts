import { describe, expect, test } from 'bun:test'

const appSourcePath = new URL('./App.vue', import.meta.url)
const modalSourcePath = new URL('./components/AddChannelModal.vue', import.meta.url)
const apiSourcePath = new URL('./services/api.ts', import.meta.url)

const readSource = async (path: URL) => Bun.file(path).text()

describe('channel save sequencing', () => {
  test('modal disables repeat submit while a save is in flight', async () => {
    const [appSource, modalSource] = await Promise.all([readSource(appSourcePath), readSource(modalSourcePath)])

    expect(appSource).toContain('const isSavingChannel = ref(false)')
    expect(appSource).toContain(':saving="isSavingChannel"')
    expect(appSource).toContain('if (isSavingChannel.value) return')

    expect(modalSource).toContain('saving?: boolean')
    expect(modalSource).toContain(':loading="saving"')
    expect(modalSource).toMatch(/:disabled="[^"]*saving/)
    expect(modalSource).toMatch(/const handleSubmit = async \(\) => \{\s*if \(props\.saving\) return/)
    expect(modalSource).toMatch(/const handleQuickSubmit = \(\) => \{\s*if \(props\.saving\) return/)
  })

  test('immediate edit after create uses the returned stable identity before refresh', async () => {
    const appSource = await readSource(appSourcePath)

    expect(appSource).toContain('const buildCreatedChannel =')
    expect(appSource).toContain('const mergeSavedChannel =')
    expect(appSource).toContain('const mutationResponse = await createChannel')
    expect(appSource).toContain('const createdChannel = buildCreatedChannel(channel, mutationResponse)')
    expect(appSource).toContain('mergeSavedChannel(createdChannel)')
    expect(appSource).toContain('const quickAddChannel = createdChannel')
    expect(appSource).not.toContain('await refreshChannels() // 先刷新获取新渠道的 index')
  })

  test('409 Conflict shows a conflict toast and refreshes channel state', async () => {
    const [appSource, apiSource] = await Promise.all([readSource(appSourcePath), readSource(apiSourcePath)])

    expect(apiSource).toContain('export class ApiError extends Error')
    expect(apiSource).toContain('this.status = status')
    expect(apiSource).toContain("response.headers.get('ETag')")
    expect(apiSource).toContain('cacheChannelConfigEtag')
    expect(apiSource).toContain('nextRevision <= currentRevision')
    expect(apiSource).toContain('getChannelMutationOptions')
    expect(apiSource).toContain("'If-Match': options.ifMatch")

    expect(appSource).toContain('const isConflictError =')
    expect(appSource).toContain(
      "showToast('Channel configuration changed. Refreshed latest channel state; please retry.', 'warning')"
    )
    expect(appSource).toMatch(/if \(isConflictError\(error\)\) \{\s*showToast[\s\S]*?await refreshChannels\(\)/)
  })

  test('quick-add follow-up conflicts are routed through the shared 409 handler', async () => {
    const appSource = await readSource(appSourcePath)

    expect(appSource).toMatch(/catch \(err\) \{\s*if \(isConflictError\(err\)\) \{\s*throw err\s*\}/)
    expect(appSource).not.toMatch(
      /catch \(err\) \{\s*console\.warn\('设置快速添加优先级失败:', err\)\s*\/\/ 不影响主流程，只是提示\s*\}/
    )
  })
})
