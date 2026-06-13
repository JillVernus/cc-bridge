import { describe, expect, test } from 'bun:test'

const requestLogTableSourcePath = new URL('./RequestLogTable.vue', import.meta.url)

const readRequestLogTableSource = async () => Bun.file(requestLogTableSourcePath).text()

const expectSourceOrder = (source: string, markers: string[]) => {
  let previousIndex = -1
  for (const marker of markers) {
    const index = source.indexOf(marker)
    expect(index).toBeGreaterThan(previousIndex)
    previousIndex = index
  }
}

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

describe('RequestLogTable channel filter', () => {
  test('renders the channel filter from the Channel column header', async () => {
    const source = await readRequestLogTableSource()

    expect(source).not.toContain('class="channel-filter-bar')

    const headersSlotStart = source.indexOf('<template v-slot:headers="{ columns }">')
    const headersSlotEnd = source.indexOf('<template v-slot:item.status="{ item }">')
    expect(headersSlotStart).toBeGreaterThan(0)
    expect(headersSlotEnd).toBeGreaterThan(headersSlotStart)

    const headersSlot = source.slice(headersSlotStart, headersSlotEnd)
    expect(headersSlot).toContain("column.key === 'providerName'")
    expect(headersSlot).toContain('channel-header-filter')
    expect(headersSlot).toContain('selectedChannelFilter')
    expect(headersSlot).toContain("t('requestLog.filterByChannel')")
  })

  test('sends the selected channel as an explicit channel request-log filter', async () => {
    const source = await readRequestLogTableSource()

    expect(source).toContain('filter.channel = selectedChannelFilter.value')
    expect(source).not.toContain('filter.provider = selectedChannelFilter.value')
  })
})

describe('RequestLogTable summary Avg TPS', () => {
  test('renders Avg TPS as a sortable summary column', async () => {
    const source = await readRequestLogTableSource()

    expect(source).toContain("avgTps: t('requestLog.tps')")
    expect(source).toContain("toggleSummarySort('avgTps')")
    expect(source).toContain('summaryColumnWidths.avgTps')
    expect(source).toContain('{{ formatSummaryTps(data.avgTps) }}')
    expect(source).toContain('{{ formatSummaryTps(currentTotals.avgTps) }}')
  })

  test('models Avg TPS in request-log group stats and totals', async () => {
    const source = await readRequestLogTableSource()

    expect(source).toContain('avgTps: number')
    expect(source).toContain('avgTpsSampleCount: number')
    expect(source).toContain('averageSummaryTps')
    expect(source).toContain('totals.avgTpsSampleCount += data.avgTpsSampleCount ?? 0')
  })

  test('places Avg TPS between cache hit rate and cost in summary views', async () => {
    const source = await readRequestLogTableSource()

    const mobileRows = source.slice(source.indexOf('<!-- Metrics -->'), source.indexOf('<!-- Mobile Total Card -->'))
    expectSourceOrder(mobileRows, [
      "{{ t('requestLog.cacheHitRate') }}: {{ calcHitRate(data) }}%",
      "{{ t('requestLog.tps') }}: {{ formatSummaryTps(data.avgTps) }}",
      '{{ formatPriceSummary(data.cost) }}'
    ])

    const mobileTotal = source.slice(source.indexOf('<!-- Mobile Total Card -->'), source.indexOf('<!-- Desktop Summary Table -->'))
    expectSourceOrder(mobileTotal, [
      "{{ t('requestLog.cacheHitRate') }}: {{ calcHitRate(currentTotals) }}%",
      "{{ t('requestLog.tps') }}: {{ formatSummaryTps(currentTotals.avgTps) }}",
      '{{ formatPriceSummary(currentTotals.cost) }}'
    ])

    const desktopHeader = source.slice(source.indexOf('<!-- Header table -->'), source.indexOf('<!-- Body table (scrollable) -->'))
    expectSourceOrder(desktopHeader, [
      "toggleSummarySort('cacheHitRate')",
      "toggleSummarySort('avgTps')",
      "toggleSummarySort('cost')"
    ])

    const desktopBody = source.slice(source.indexOf('<!-- Body table (scrollable) -->'), source.indexOf('<!-- Footer table -->'))
    expectSourceOrder(desktopBody, [
      '{{ calcHitRate(data) }}%',
      '{{ formatSummaryTps(data.avgTps) }}',
      '{{ formatPriceSummary(data.cost) }}'
    ])

    const desktopFooterStart = source.indexOf('<!-- Footer table -->')
    const desktopFooter = source.slice(desktopFooterStart, source.indexOf('</tbody>', desktopFooterStart))
    expectSourceOrder(desktopFooter, [
      '{{ calcHitRate(currentTotals) }}%',
      '{{ formatSummaryTps(currentTotals.avgTps) }}',
      '{{ formatPriceSummary(currentTotals.cost) }}'
    ])
  })
})
