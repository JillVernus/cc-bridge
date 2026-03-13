<template>
  <div class="report-view">
    <!-- Snackbar -->
    <v-snackbar v-model="showError" color="error" :timeout="3000" location="top">
      {{ errorMessage }}
      <template #actions>
        <v-btn variant="text" @click="showError = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>

    <!-- Period Selector Toolbar -->
    <div class="report-toolbar d-flex align-center flex-wrap ga-2 mb-4">
      <v-btn-toggle v-model="selectedPreset" mandatory density="compact" variant="outlined" divided>
        <v-btn v-for="p in presets" :key="p.value" :value="p.value" size="small">{{ p.label }}</v-btn>
      </v-btn-toggle>

      <!-- Custom date range -->
      <div class="d-flex align-center ga-1">
        <input type="date" v-model="customFrom" class="date-input" :max="customTo || todayStr" />
        <span class="text-medium-emphasis">—</span>
        <input type="date" v-model="customTo" class="date-input" :min="customFrom" :max="todayStr" />
        <v-btn size="small" variant="tonal" @click="applyCustomRange" :disabled="!customFrom || !customTo">
          {{ t('common.confirm') }}
        </v-btn>
      </div>

      <!-- Endpoint filter -->
      <v-btn-toggle v-model="selectedEndpoint" mandatory density="compact" variant="outlined" divided>
        <v-btn value="all" size="small">{{ t('chart.endpoint.all') }}</v-btn>
        <v-btn value="messages" size="small">{{ t('chart.endpoint.messages') }}</v-btn>
        <v-btn value="responses" size="small">{{ t('chart.endpoint.responses') }}</v-btn>
        <v-btn value="gemini" size="small">{{ t('chart.endpoint.gemini') }}</v-btn>
        <v-btn value="chat" size="small">{{ t('chart.endpoint.chat') }}</v-btn>
      </v-btn-toggle>

      <v-btn icon size="small" variant="text" @click="fetchData" :loading="isLoading">
        <v-icon size="small">mdi-refresh</v-icon>
      </v-btn>
    </div>

    <!-- Loading -->
    <div v-if="isLoading && !stats" class="d-flex justify-center py-12">
      <v-progress-circular indeterminate size="48" color="primary" />
    </div>

    <template v-else>
      <!-- Summary Cards -->
      <div class="summary-row d-flex flex-wrap ga-3 mb-5">
        <div class="report-summary-card">
          <div class="rsc-label">{{ t('report.totalRequests') }}</div>
          <div class="rsc-value">{{ fmtNum(stats?.totalRequests ?? 0) }}</div>
          <div class="rsc-sub">{{ t('report.successRate') }}: {{ successRate }}%</div>
        </div>
        <div class="report-summary-card">
          <div class="rsc-label">{{ t('report.totalCost') }}</div>
          <div class="rsc-value">{{ fmtCurrency(stats?.totalCost ?? 0) }}</div>
        </div>
        <div class="report-summary-card">
          <div class="rsc-label">{{ t('report.totalTokens') }}</div>
          <div class="rsc-value">{{ fmtNum(totalTokens) }}</div>
          <div class="rsc-sub">
            {{
              t('report.inputOutput', {
                input: fmtNum(stats?.totalTokens?.inputTokens ?? 0),
                output: fmtNum(stats?.totalTokens?.outputTokens ?? 0)
              })
            }}
          </div>
        </div>
        <div class="report-summary-card">
          <div class="rsc-label">{{ t('report.cacheHitRate') }}</div>
          <div class="rsc-value">{{ cacheHitRate }}%</div>
        </div>
        <div class="report-summary-card">
          <div class="rsc-label">{{ t('report.avgLatency') }}</div>
          <div class="rsc-value">{{ fmtDuration(stats?.avgLatencyMs ?? 0) }}</div>
          <div class="rsc-sub">P95: {{ fmtDuration(stats?.p95LatencyMs ?? 0) }}</div>
        </div>
      </div>

      <!-- Charts Section -->
      <v-row class="mb-5">
        <v-col cols="12" md="6">
          <v-card elevation="0" class="report-chart-card pa-3">
            <div class="text-caption text-medium-emphasis mb-2">{{ t('report.dailyCostTrend') }}</div>
            <apexchart
              v-if="dailyCostSeries.length"
              type="bar"
              height="220"
              :options="dailyCostOptions"
              :series="dailyCostSeries"
            />
            <div v-else class="d-flex justify-center align-center text-medium-emphasis" style="height: 220px">
              <span class="text-caption">{{ t('chart.noData') }}</span>
            </div>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card elevation="0" class="report-chart-card pa-3">
            <div class="text-caption text-medium-emphasis mb-2">{{ t('report.modelDistribution') }}</div>
            <apexchart
              v-if="modelDonutSeries.length"
              type="donut"
              height="220"
              :options="modelDonutOptions"
              :series="modelDonutSeries"
            />
            <div v-else class="d-flex justify-center align-center text-medium-emphasis" style="height: 220px">
              <span class="text-caption">{{ t('chart.noData') }}</span>
            </div>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card elevation="0" class="report-chart-card pa-3">
            <div class="text-caption text-medium-emphasis mb-2">{{ t('report.tokenBreakdown') }}</div>
            <apexchart
              v-if="tokenBreakdownSeries.length"
              type="bar"
              height="220"
              :options="tokenBreakdownOptions"
              :series="tokenBreakdownSeries"
            />
            <div v-else class="d-flex justify-center align-center text-medium-emphasis" style="height: 220px">
              <span class="text-caption">{{ t('chart.noData') }}</span>
            </div>
          </v-card>
        </v-col>
      </v-row>

      <!-- Breakdown Tables -->
      <v-card elevation="0" class="report-breakdown-card">
        <v-tabs v-model="activeBreakdown" density="compact">
          <v-tab value="channel">{{ t('report.byChannel') }}</v-tab>
          <v-tab value="model">{{ t('report.byModel') }}</v-tab>
          <v-tab value="apikey">{{ t('report.byApiKey') }}</v-tab>
          <v-tab value="client">{{ t('report.byClient') }}</v-tab>
        </v-tabs>

        <div class="pa-3">
          <div class="d-flex justify-end mb-2">
            <v-btn size="small" variant="tonal" prepend-icon="mdi-download" @click="exportCSV">
              {{ t('report.exportCsv') }}
            </v-btn>
          </div>

          <!-- Channel breakdown -->
          <v-table v-if="activeBreakdown === 'channel'" density="compact" class="report-table">
            <thead>
              <tr>
                <th>{{ t('report.channelName') }}</th>
                <th class="text-right">{{ t('report.requests') }}</th>
                <th class="text-right">{{ t('report.successRateShort') }}</th>
                <th class="text-right">{{ t('report.inputTokensShort') }}</th>
                <th class="text-right">{{ t('report.outputTokensShort') }}</th>
                <th class="text-right">{{ t('report.cacheTokens') }}</th>
                <th class="text-right">{{ t('report.cost') }}</th>
                <th class="text-right">{{ t('report.avgLatencyShort') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in channelRows" :key="row.key">
                <td>{{ row.name }}</td>
                <td class="text-right">{{ fmtNum(row.count) }}</td>
                <td class="text-right">{{ row.count > 0 ? ((row.success / row.count) * 100).toFixed(1) : '0.0' }}%</td>
                <td class="text-right">{{ fmtNum(row.inputTokens) }}</td>
                <td class="text-right">{{ fmtNum(row.outputTokens) }}</td>
                <td class="text-right">{{ fmtNum(getCacheTokens(row)) }}</td>
                <td class="text-right">{{ fmtCurrency(row.cost) }}</td>
                <td class="text-right">{{ fmtDuration(row.avgLatencyMs) }}</td>
              </tr>
              <tr v-if="!channelRows.length">
                <td colspan="8" class="text-center text-medium-emphasis">{{ t('common.noData') }}</td>
              </tr>
            </tbody>
          </v-table>

          <!-- Model breakdown -->
          <v-table v-if="activeBreakdown === 'model'" density="compact" class="report-table">
            <thead>
              <tr>
                <th>{{ t('report.modelName') }}</th>
                <th class="text-right">{{ t('report.requests') }}</th>
                <th class="text-right">{{ t('report.inputTokensShort') }}</th>
                <th class="text-right">{{ t('report.outputTokensShort') }}</th>
                <th class="text-right">{{ t('report.cost') }}</th>
                <th class="text-right">{{ t('report.avgCostPerReq') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in modelRows" :key="row.key">
                <td>{{ row.name }}</td>
                <td class="text-right">{{ fmtNum(row.count) }}</td>
                <td class="text-right">{{ fmtNum(row.inputTokens) }}</td>
                <td class="text-right">{{ fmtNum(row.outputTokens) }}</td>
                <td class="text-right">{{ fmtCurrency(row.cost) }}</td>
                <td class="text-right">{{ fmtCurrency(row.count > 0 ? row.cost / row.count : 0) }}</td>
              </tr>
              <tr v-if="!modelRows.length">
                <td colspan="6" class="text-center text-medium-emphasis">{{ t('common.noData') }}</td>
              </tr>
            </tbody>
          </v-table>

          <!-- API Key breakdown -->
          <v-table v-if="activeBreakdown === 'apikey'" density="compact" class="report-table">
            <thead>
              <tr>
                <th>{{ t('report.apiKeyId') }}</th>
                <th class="text-right">{{ t('report.requests') }}</th>
                <th class="text-right">{{ t('report.totalTokensShort') }}</th>
                <th class="text-right">{{ t('report.cost') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in apiKeyRows" :key="row.key">
                <td>{{ row.name }}</td>
                <td class="text-right">{{ fmtNum(row.count) }}</td>
                <td class="text-right">{{ fmtNum(row.inputTokens + row.outputTokens) }}</td>
                <td class="text-right">{{ fmtCurrency(row.cost) }}</td>
              </tr>
              <tr v-if="!apiKeyRows.length">
                <td colspan="4" class="text-center text-medium-emphasis">{{ t('common.noData') }}</td>
              </tr>
            </tbody>
          </v-table>

          <!-- Client breakdown -->
          <v-table v-if="activeBreakdown === 'client'" density="compact" class="report-table">
            <thead>
              <tr>
                <th>{{ t('report.clientId') }}</th>
                <th class="text-right">{{ t('report.requests') }}</th>
                <th class="text-right">{{ t('report.totalTokensShort') }}</th>
                <th class="text-right">{{ t('report.cost') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in clientRows" :key="row.key">
                <td>{{ row.name }}</td>
                <td class="text-right">{{ fmtNum(row.count) }}</td>
                <td class="text-right">{{ fmtNum(row.inputTokens + row.outputTokens) }}</td>
                <td class="text-right">{{ fmtCurrency(row.cost) }}</td>
              </tr>
              <tr v-if="!clientRows.length">
                <td colspan="4" class="text-center text-medium-emphasis">{{ t('common.noData') }}</td>
              </tr>
            </tbody>
          </v-table>
        </div>
      </v-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useTheme } from 'vuetify'
import { useI18n } from 'vue-i18n'
import VueApexCharts from 'vue3-apexcharts'
import { api, type RequestLogStats, type DailyStatsResponse, type GroupStats, type APIKey } from '../services/api'

const apexchart = VueApexCharts
const { t } = useI18n()
const theme = useTheme()
const isDark = computed(() => theme.global.current.value.dark)

type EndpointFilter = 'all' | 'messages' | 'responses' | 'gemini' | 'chat'
type PresetValue = 'today' | 'yesterday' | '7d' | '30d' | 'thisMonth' | 'lastMonth' | 'custom'

const endpointMap: Record<string, string> = {
  messages: '/v1/messages',
  responses: '/v1/responses',
  gemini: '/v1/gemini',
  chat: '/v1/chat/completions'
}

// State
const selectedPreset = ref<PresetValue>('7d')
const selectedEndpoint = ref<EndpointFilter>('all')
const customFrom = ref('')
const customTo = ref('')
const isLoading = ref(false)
const showError = ref(false)
const errorMessage = ref('')
const stats = ref<RequestLogStats | null>(null)
const dailyData = ref<DailyStatsResponse | null>(null)
const activeBreakdown = ref('channel')
const apiKeys = ref<APIKey[]>([])

const apiKeyMap = computed(() => {
  const map = new Map<number, string>()
  for (const key of apiKeys.value) {
    map.set(key.id, key.name)
  }
  return map
})

const todayStr = computed(() => {
  const d = new Date()
  return d.toISOString().slice(0, 10)
})

const presets = computed(() => [
  { value: 'today' as PresetValue, label: t('report.today') },
  { value: 'yesterday' as PresetValue, label: t('report.yesterday') },
  { value: '7d' as PresetValue, label: t('report.last7Days') },
  { value: '30d' as PresetValue, label: t('report.last30Days') },
  { value: 'thisMonth' as PresetValue, label: t('report.thisMonth') },
  { value: 'lastMonth' as PresetValue, label: t('report.lastMonth') }
])

// Compute date range from preset
function getDateRange(): { from: string; to: string } {
  const now = new Date()
  const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate())
  const endOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate(), 23, 59, 59, 999)

  switch (selectedPreset.value) {
    case 'today':
      return { from: startOfDay(now).toISOString(), to: now.toISOString() }
    case 'yesterday': {
      const y = new Date(now)
      y.setDate(y.getDate() - 1)
      return { from: startOfDay(y).toISOString(), to: endOfDay(y).toISOString() }
    }
    case '7d': {
      const d = new Date(now)
      d.setDate(d.getDate() - 6)
      return { from: startOfDay(d).toISOString(), to: now.toISOString() }
    }
    case '30d': {
      const d = new Date(now)
      d.setDate(d.getDate() - 29)
      return { from: startOfDay(d).toISOString(), to: now.toISOString() }
    }
    case 'thisMonth':
      return { from: new Date(now.getFullYear(), now.getMonth(), 1).toISOString(), to: now.toISOString() }
    case 'lastMonth': {
      const first = new Date(now.getFullYear(), now.getMonth() - 1, 1)
      const last = new Date(now.getFullYear(), now.getMonth(), 0, 23, 59, 59, 999)
      return { from: first.toISOString(), to: last.toISOString() }
    }
    case 'custom':
      if (customFrom.value && customTo.value) {
        return {
          from: new Date(customFrom.value + 'T00:00:00').toISOString(),
          to: new Date(customTo.value + 'T23:59:59.999').toISOString()
        }
      }
      return {
        from: startOfDay(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6)).toISOString(),
        to: now.toISOString()
      }
    default:
      return {
        from: startOfDay(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6)).toISOString(),
        to: now.toISOString()
      }
  }
}

function applyCustomRange() {
  selectedPreset.value = 'custom'
  fetchData()
}

const endpointParam = computed(() => {
  if (selectedEndpoint.value === 'all') return undefined
  return endpointMap[selectedEndpoint.value]
})

async function fetchData() {
  isLoading.value = true
  try {
    const { from, to } = getDateRange()
    const [s, d] = await Promise.all([
      api.getReportStats(from, to, endpointParam.value),
      api.getReportDailyStats(from, to, endpointParam.value)
    ])
    stats.value = s
    dailyData.value = d
  } catch (e: any) {
    errorMessage.value = e?.message || 'Failed to load report data'
    showError.value = true
  } finally {
    isLoading.value = false
  }
}

// Computed summary values
const successRate = computed(() => {
  if (!stats.value || stats.value.totalRequests === 0) return '0.0'
  return ((stats.value.totalSuccess / stats.value.totalRequests) * 100).toFixed(1)
})

const totalTokens = computed(() => {
  if (!stats.value) return 0
  const t = stats.value.totalTokens
  return t.inputTokens + t.outputTokens
})

const cacheHitRate = computed(() => {
  if (!stats.value) return '0.0'
  const t = stats.value.totalTokens
  const total = t.inputTokens + t.cacheReadInputTokens + t.cacheCreationInputTokens
  if (total === 0) return '0.0'
  return ((t.cacheReadInputTokens / total) * 100).toFixed(1)
})

// Breakdown rows (sorted by cost desc)
interface BreakdownRow extends GroupStats {
  key: string
  name: string
}

function getCacheTokens(row: Pick<GroupStats, 'cacheCreationInputTokens' | 'cacheReadInputTokens'>): number {
  return row.cacheCreationInputTokens + row.cacheReadInputTokens
}

function toRows(map: Record<string, GroupStats> | undefined): BreakdownRow[] {
  if (!map) return []
  return Object.entries(map)
    .map(([name, s]) => ({ key: name, name, ...s }))
    .sort((a, b) => b.cost - a.cost)
}

function getAPIKeyDisplayName(key: string): string {
  if (key === '<unknown>' || key === 'master') return key
  const apiKeyID = parseInt(key, 10)
  if (!Number.isNaN(apiKeyID)) {
    return apiKeyMap.value.get(apiKeyID) || key
  }
  return key
}

async function loadAPIKeys() {
  try {
    const response = await api.getAPIKeys({ limit: 1000 })
    apiKeys.value = response.keys || []
  } catch (e) {
    console.error('Failed to load API keys for report:', e)
  }
}

const channelRows = computed(() => toRows(stats.value?.byProvider))
const modelRows = computed(() => toRows(stats.value?.byModel))
const apiKeyRows = computed(() =>
  toRows(stats.value?.byApiKey).map(row => ({
    ...row,
    name: getAPIKeyDisplayName(row.key)
  }))
)
const clientRows = computed(() => toRows(stats.value?.byClient))

// Format helpers
function fmtNum(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return n.toFixed(0)
}

function fmtCurrency(n: number): string {
  if (typeof n !== 'number' || Number.isNaN(n)) return '--'
  const abs = Math.abs(n)
  if (abs >= 1_000) return '$' + (n / 1_000).toFixed(1) + 'K'
  if (abs >= 0.01) return '$' + n.toFixed(2)
  if (abs > 0) return '$' + n.toFixed(4)
  return '$0.00'
}

function fmtDuration(ms: number): string {
  if (!ms || ms <= 0) return '--'
  if (ms >= 10_000) return (ms / 1_000).toFixed(0) + 's'
  if (ms >= 1_000) return (ms / 1_000).toFixed(1) + 's'
  return Math.round(ms) + 'ms'
}

// CSV export
function exportCSV() {
  let rows: BreakdownRow[] = []
  let headers: string[] = []
  let filename = 'report'

  switch (activeBreakdown.value) {
    case 'channel':
      rows = channelRows.value
      headers = [
        'Channel',
        'Requests',
        'Success',
        'Failure',
        'Input Tokens',
        'Output Tokens',
        'Cache Tokens',
        'Cost',
        'Avg Latency (ms)'
      ]
      filename = 'report-by-channel'
      break
    case 'model':
      rows = modelRows.value
      headers = ['Model', 'Requests', 'Input Tokens', 'Output Tokens', 'Cost', 'Avg Cost/Req']
      filename = 'report-by-model'
      break
    case 'apikey':
      rows = apiKeyRows.value
      headers = ['API Key', 'Requests', 'Tokens', 'Cost']
      filename = 'report-by-apikey'
      break
    case 'client':
      rows = clientRows.value
      headers = ['Client', 'Requests', 'Tokens', 'Cost']
      filename = 'report-by-client'
      break
  }

  const csvRows = [headers.join(',')]
  for (const r of rows) {
    let line: string[]
    switch (activeBreakdown.value) {
      case 'channel':
        line = [
          r.name,
          String(r.count),
          String(r.success),
          String(r.failure),
          String(r.inputTokens),
          String(r.outputTokens),
          String(getCacheTokens(r)),
          r.cost.toFixed(4),
          r.avgLatencyMs.toFixed(0)
        ]
        break
      case 'model':
        line = [
          r.name,
          String(r.count),
          String(r.inputTokens),
          String(r.outputTokens),
          r.cost.toFixed(4),
          r.count > 0 ? (r.cost / r.count).toFixed(4) : '0'
        ]
        break
      default:
        line = [r.name, String(r.count), String(r.inputTokens + r.outputTokens), r.cost.toFixed(4)]
    }
    csvRows.push(line.map(v => `"${v}"`).join(','))
  }

  const blob = new Blob([csvRows.join('\n')], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${filename}.csv`
  a.click()
  URL.revokeObjectURL(url)
}

// Chart colors
const chartColors = [
  '#3b82f6',
  '#10b981',
  '#f59e0b',
  '#8b5cf6',
  '#ef4444',
  '#06b6d4',
  '#ec4899',
  '#84cc16',
  '#f97316',
  '#14b8a6'
]

// Daily cost chart
const dailyCostSeries = computed(() => {
  if (!dailyData.value?.dataPoints?.length) return []
  return [
    {
      name: t('report.cost'),
      data: dailyData.value.dataPoints.map(dp => ({ x: dp.date, y: parseFloat(dp.cost.toFixed(4)) }))
    }
  ]
})

const dailyCostOptions = computed(() => ({
  chart: { type: 'bar' as const, toolbar: { show: false }, background: 'transparent' },
  theme: { mode: (isDark.value ? 'dark' : 'light') as 'dark' | 'light' },
  colors: ['#3b82f6'],
  plotOptions: { bar: { borderRadius: 3, columnWidth: '60%' } },
  xaxis: { type: 'category' as const, labels: { style: { fontSize: '10px' } } },
  yaxis: { labels: { formatter: (v: number) => fmtCurrency(v) } },
  tooltip: { y: { formatter: (v: number) => fmtCurrency(v) } },
  dataLabels: { enabled: false },
  grid: { borderColor: isDark.value ? '#333' : '#e5e7eb' }
}))

// Model distribution donut
const modelDonutSeries = computed(() => {
  if (!stats.value?.byModel) return []
  const entries = Object.entries(stats.value.byModel)
    .sort((a, b) => b[1].cost - a[1].cost)
    .slice(0, 8)
  return entries.map(([, s]) => parseFloat(s.cost.toFixed(4)))
})

const modelDonutLabels = computed(() => {
  if (!stats.value?.byModel) return []
  return Object.entries(stats.value.byModel)
    .sort((a, b) => b[1].cost - a[1].cost)
    .slice(0, 8)
    .map(([name]) => name)
})

const modelDonutOptions = computed(() => ({
  chart: { type: 'donut' as const, background: 'transparent' },
  theme: { mode: (isDark.value ? 'dark' : 'light') as 'dark' | 'light' },
  labels: modelDonutLabels.value,
  colors: chartColors,
  legend: { position: 'bottom' as const, fontSize: '10px' },
  tooltip: { y: { formatter: (v: number) => fmtCurrency(v) } },
  dataLabels: { enabled: false },
  plotOptions: { pie: { donut: { size: '60%' } } }
}))

// Token breakdown stacked bar
const tokenBreakdownSeries = computed(() => {
  if (!dailyData.value?.dataPoints?.length) return []
  const dps = dailyData.value.dataPoints
  return [
    { name: 'Input', data: dps.map(dp => ({ x: dp.date, y: dp.inputTokens })) },
    { name: 'Output', data: dps.map(dp => ({ x: dp.date, y: dp.outputTokens })) },
    { name: 'Cache Create', data: dps.map(dp => ({ x: dp.date, y: dp.cacheCreationInputTokens })) },
    { name: 'Cache Read', data: dps.map(dp => ({ x: dp.date, y: dp.cacheReadInputTokens })) }
  ]
})

const tokenBreakdownOptions = computed(() => ({
  chart: { type: 'bar' as const, stacked: true, toolbar: { show: false }, background: 'transparent' },
  theme: { mode: (isDark.value ? 'dark' : 'light') as 'dark' | 'light' },
  colors: ['#8b5cf6', '#f97316', '#22c55e', '#eab308'],
  plotOptions: { bar: { borderRadius: 2, columnWidth: '60%' } },
  xaxis: { type: 'category' as const, labels: { style: { fontSize: '10px' } } },
  yaxis: { labels: { formatter: (v: number) => fmtNum(v) } },
  tooltip: { y: { formatter: (v: number) => fmtNum(v) } },
  dataLabels: { enabled: false },
  legend: { position: 'bottom' as const, fontSize: '10px' },
  grid: { borderColor: isDark.value ? '#333' : '#e5e7eb' }
}))

// Watchers
watch(selectedPreset, () => {
  if (selectedPreset.value !== 'custom') fetchData()
})

watch(selectedEndpoint, () => {
  fetchData()
})

onMounted(() => {
  loadAPIKeys()
  fetchData()
})
</script>

<style scoped>
.report-view {
  max-width: 1400px;
  margin: 0 auto;
}

.date-input {
  padding: 4px 8px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 4px;
  font-size: 13px;
  background: transparent;
  color: inherit;
  height: 32px;
}

.report-summary-card {
  flex: 1 1 160px;
  min-width: 140px;
  padding: 12px 16px;
  border-radius: 8px;
  background: rgba(var(--v-theme-surface-variant), 0.4);
  border: 1px solid rgba(var(--v-border-color), 0.12);
}

.rsc-label {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  opacity: 0.6;
  margin-bottom: 4px;
}

.rsc-value {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.2;
}

.rsc-sub {
  font-size: 11px;
  opacity: 0.5;
  margin-top: 2px;
}

.report-chart-card {
  border: 1px solid rgba(var(--v-border-color), 0.12);
  border-radius: 8px;
}

.report-breakdown-card {
  border: 1px solid rgba(var(--v-border-color), 0.12);
  border-radius: 8px;
}

.report-table th {
  font-size: 11px !important;
  text-transform: uppercase;
  letter-spacing: 0.3px;
  white-space: nowrap;
}

.report-table td {
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}
</style>
