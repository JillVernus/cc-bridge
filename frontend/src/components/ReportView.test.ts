import { describe, expect, test } from 'bun:test'

const reportViewSourcePath = new URL('./ReportView.vue', import.meta.url)
const apiSourcePath = new URL('../services/api.ts', import.meta.url)

const readReportViewSource = async () => Bun.file(reportViewSourcePath).text()
const readApiSource = async () => Bun.file(apiSourcePath).text()

describe('ReportView Avg TPS', () => {
  test('renders Avg TPS in report summary, charts, breakdown tables, and CSV export', async () => {
    const source = await readReportViewSource()

    expect(source).toContain("t('report.avgTps')")
    expect(source).toContain('fmtTps(stats?.avgTps ?? 0)')
    expect(source).toContain('dailyTpsSeries')
    expect(source).toContain('dailyTpsOptions')
    expect(source).toContain('dp.avgTps')
    expect(source).toContain('fmtTps(row.avgTps)')
    expect(source).toContain("'Avg TPS'")
    expect(source).toContain('r.avgTps.toFixed(1)')
  })

  test('models Avg TPS in report API response types', async () => {
    const source = await readApiSource()
    const dailyStatsType = source.slice(
      source.indexOf('export interface DailyStatsDataPoint'),
      source.indexOf('export interface DailyStatsResponse')
    )

    expect(source).toContain('avgTps: number')
    expect(source).toContain('avgTpsSampleCount: number')
    expect(dailyStatsType).toContain('avgTps: number')
    expect(dailyStatsType).toContain('avgTpsSampleCount: number')
  })
})

describe('ReportView breakdown sorting', () => {
  test('supports clicking breakdown table headers to toggle sort order', async () => {
    const source = await readReportViewSource()

    expect(source).toContain("const breakdownSortColumn = ref<BreakdownSortColumn>('cost')")
    expect(source).toContain("const breakdownSortDirection = ref<SortDirection>('desc')")
    expect(source).toContain('const toggleBreakdownSort = (column: BreakdownSortColumn) =>')
    expect(source).toContain('sortBreakdownRows(toRows(stats.value?.byProvider))')
    expect(source).toContain('sortBreakdownRows(toRows(stats.value?.byModel))')
    expect(source).toContain('aria-sort')
    expect(source).toContain("toggleBreakdownSort('name')")
    expect(source).toContain("toggleBreakdownSort('requests')")
    expect(source).toContain("toggleBreakdownSort('avgTps')")
    expect(source).toContain("toggleBreakdownSort('avgCostPerReq')")
  })
})
