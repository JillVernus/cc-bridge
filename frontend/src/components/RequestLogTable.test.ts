import { describe, expect, test } from 'bun:test'

const requestLogTableSourcePath = new URL('./RequestLogTable.vue', import.meta.url)

const readRequestLogTableSource = async () => Bun.file(requestLogTableSourcePath).text()

describe('RequestLogTable top panels', () => {
  test('hides the active session card while keeping the date filter resize splitter', async () => {
    const source = await readRequestLogTableSource()
    const dateFilterIndex = source.indexOf('<!-- 日期筛选 -->')
    expect(dateFilterIndex).toBeGreaterThan(0)

    const topPanelTemplate = source.slice(0, dateFilterIndex)

    expect(topPanelTemplate).not.toContain("t('requestLog.activeSessions')")
    expect(topPanelTemplate).toContain(`@pointerdown="startPanelResize($event, 'splitter2')"`)
  })
})
