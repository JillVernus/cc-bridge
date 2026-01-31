<template>
  <div class="global-stats-chart-container">
    <!-- Snackbar for error notification -->
    <v-snackbar v-model="showError" color="error" :timeout="3000" location="top">
      {{ errorMessage }}
      <template #actions>
        <v-btn variant="text" @click="showError = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>

    <!-- Header: Endpoint filter + Duration selector + View switcher -->
    <div class="chart-header d-flex align-center justify-space-between mb-3 flex-wrap ga-2">
      <div class="d-flex align-center ga-2 flex-wrap">
        <!-- Endpoint filter -->
        <v-btn-toggle v-model="selectedEndpoint" mandatory density="compact" variant="outlined" divided :disabled="isLoading">
          <v-btn value="all" size="x-small">{{ t('chart.endpoint.all') }}</v-btn>
          <v-btn value="messages" size="x-small">
            <v-icon start size="16" icon="custom:claude" />
            {{ t('chart.endpoint.messages') }}
          </v-btn>
          <v-btn value="responses" size="x-small">
            <v-icon start size="16" icon="custom:codex" />
            {{ t('chart.endpoint.responses') }}
          </v-btn>
          <v-btn value="gemini" size="x-small">
            <v-icon start size="16" icon="custom:gemini" />
            {{ t('chart.endpoint.gemini') }}
          </v-btn>
        </v-btn-toggle>

        <!-- Duration selector -->
        <v-btn-toggle
          v-model="selectedDuration"
          mandatory
          density="compact"
          variant="outlined"
          divided
          :disabled="isLoading"
          @update:model-value="durationTouched = true"
        >
          <v-btn value="1h" size="x-small">{{ t('chart.duration.1h') }}</v-btn>
          <v-btn value="6h" size="x-small">{{ t('chart.duration.6h') }}</v-btn>
          <v-btn value="24h" size="x-small">{{ t('chart.duration.24h') }}</v-btn>
          <v-btn v-if="hasExternalRange" value="period" size="x-small">{{ t('chart.duration.period') }}</v-btn>
          <v-btn v-else value="today" size="x-small">{{ t('chart.duration.today') }}</v-btn>
        </v-btn-toggle>

        <v-btn icon size="x-small" variant="text" @click="refreshData" :loading="isLoading" :disabled="isLoading">
          <v-icon size="small">mdi-refresh</v-icon>
        </v-btn>
      </div>

      <!-- View switcher -->
      <v-btn-toggle v-model="selectedView" mandatory density="compact" variant="outlined" divided :disabled="isLoading">
        <v-btn value="traffic" size="x-small">
          <v-icon size="small" class="mr-1">mdi-chart-line</v-icon>
          {{ t('chart.view.traffic') }}
        </v-btn>
        <v-btn value="tokens" size="x-small">
          <v-icon size="small" class="mr-1">mdi-chart-areaspline</v-icon>
          {{ t('chart.view.tokens') }}
        </v-btn>
        <v-btn value="latency" size="x-small">
          <v-icon size="small" class="mr-1">mdi-timer-outline</v-icon>
          {{ t('chart.view.latency') }}
        </v-btn>
        <v-btn value="cost" size="x-small">
          <v-icon size="small" class="mr-1">mdi-currency-usd</v-icon>
          {{ t('chart.view.cost') }}
        </v-btn>
      </v-btn-toggle>
    </div>

    <!-- Summary cards -->
    <div v-if="summary" class="summary-cards d-flex flex-wrap ga-2 mb-3">
      <div class="summary-card">
        <div class="summary-label">{{ t('chart.summary.totalRequests') }}</div>
        <div class="summary-value">{{ formatNumber(summary.totalRequests) }}</div>
      </div>
      <div class="summary-card">
        <div class="summary-label">{{ t('chart.summary.successRate') }}</div>
        <div class="summary-value" :class="{ 'text-success': summary.avgSuccessRate >= 95, 'text-warning': summary.avgSuccessRate >= 80 && summary.avgSuccessRate < 95, 'text-error': summary.avgSuccessRate < 80 }">
          {{ summary.avgSuccessRate.toFixed(1) }}%
        </div>
      </div>
      <template v-if="selectedView === 'latency'">
        <div class="summary-card">
          <div class="summary-label">{{ t('chart.summary.p50Latency') }}</div>
          <div class="summary-value">{{ formatDurationMs(summary.p50DurationMs) }}</div>
        </div>
        <div class="summary-card">
          <div class="summary-label">{{ t('chart.summary.p95Latency') }}</div>
          <div class="summary-value">{{ formatDurationMs(summary.p95DurationMs) }}</div>
        </div>
      </template>
      <template v-else-if="selectedView === 'cost'">
        <div class="summary-card">
          <div class="summary-label">{{ t('chart.summary.totalCost') }}</div>
          <div class="summary-value">{{ formatCurrency(summary.totalCost) }}</div>
        </div>
        <div class="summary-card">
          <div class="summary-label">{{ t('chart.summary.avgCostPerRequest') }}</div>
          <div class="summary-value">{{ formatCurrency(summary.totalRequests > 0 ? summary.totalCost / summary.totalRequests : 0) }}</div>
        </div>
      </template>
      <template v-else>
        <div class="summary-card">
          <div class="summary-label">{{ t('chart.summary.inputTokens') }}</div>
          <div class="summary-value">{{ formatNumber(summary.totalInputTokens) }}</div>
        </div>
        <div class="summary-card">
          <div class="summary-label">{{ t('chart.summary.outputTokens') }}</div>
          <div class="summary-value">{{ formatNumber(summary.totalOutputTokens) }}</div>
        </div>
        <div class="summary-card">
          <div class="summary-label">{{ t('chart.summary.cacheCreate') }}</div>
          <div class="summary-value text-success">{{ formatNumber(summary.totalCacheCreationTokens) }}</div>
        </div>
        <div class="summary-card">
          <div class="summary-label">{{ t('chart.summary.cacheHit') }}</div>
          <div class="summary-value text-warning">{{ formatNumber(summary.totalCacheReadTokens) }}</div>
        </div>
      </template>
    </div>

    <!-- Loading state -->
    <div v-if="isLoading" class="d-flex justify-center align-center" :style="{ height: chartHeight + 'px' }">
      <v-progress-circular indeterminate size="32" color="primary" />
    </div>

    <!-- Empty state -->
    <div v-else-if="!hasData" class="d-flex flex-column justify-center align-center text-medium-emphasis" :style="{ height: chartHeight + 'px' }">
      <v-icon size="40" color="grey-lighten-1">mdi-chart-timeline-variant</v-icon>
      <div class="text-caption mt-2">{{ t('chart.noData') }}</div>
    </div>

    <!-- Chart -->
    <div v-else class="chart-area" @mouseenter="pauseAutoRefresh" @mouseleave="resumeAutoRefresh">
      <apexchart
        ref="chartRef"
        type="area"
        :height="chartHeight"
        :options="chartOptions"
        :series="renderSeries"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useTheme } from 'vuetify'
import { useI18n } from 'vue-i18n'
import VueApexCharts from 'vue3-apexcharts'
import { api, type ProviderStatsHistoryResponse, type StatsHistoryResponse, type Duration } from '../services/api'

// Register apexchart component
const apexchart = VueApexCharts

// i18n
const { t } = useI18n()

// Types
type ViewMode = 'traffic' | 'tokens' | 'latency' | 'cost'
type EndpointFilter = 'all' | 'messages' | 'responses' | 'gemini'

const props = defineProps<{
  from?: string
  to?: string
  // When false, disables the internal time-based auto refresh timer.
  // Useful when the logs SSE stream is active (avoid redundant polling).
  autoRefresh?: boolean
}>()

// LocalStorage keys for preferences
const getStorageKey = (key: string) => `globalStats:${key}`

// Load saved preferences from localStorage
const loadSavedPreferences = () => {
  const savedEndpoint = localStorage.getItem(getStorageKey('endpoint')) as EndpointFilter | null
  const savedView = localStorage.getItem(getStorageKey('viewMode')) as ViewMode | null
  const savedDuration = localStorage.getItem(getStorageKey('duration')) as Duration | null

  return {
    endpoint: savedEndpoint && ['all', 'messages', 'responses', 'gemini'].includes(savedEndpoint) ? savedEndpoint : 'all',
    view: savedView && ['traffic', 'tokens', 'latency', 'cost'].includes(savedView) ? savedView : 'traffic',
    duration: savedDuration && ['1h', '6h', '24h', 'today', 'period'].includes(savedDuration) ? savedDuration : '6h'
  }
}

// Save preference to localStorage
const savePreference = (key: string, value: string) => {
  localStorage.setItem(getStorageKey(key), value)
}

// Theme
const theme = useTheme()
const isDark = computed(() => theme.global.current.value.dark)

const hasExternalRange = computed(() => Boolean(props.from && props.to))

// Load saved preferences
const savedPrefs = loadSavedPreferences()

// State (initialized from saved preferences)
const durationTouched = ref(false)
const selectedEndpoint = ref<EndpointFilter>(savedPrefs.endpoint)
const selectedView = ref<ViewMode>(savedPrefs.view)
const selectedDuration = ref<Duration>(
  hasExternalRange.value ? 'period' : (savedPrefs.duration === 'period' ? '6h' : savedPrefs.duration)
)
const isLoading = ref(false)
const isRefreshing = ref(false)
const pendingRefresh = ref<boolean | null>(null)
const historyData = ref<StatsHistoryResponse | null>(null)
const providerHistoryData = ref<ProviderStatsHistoryResponse | null>(null)
const showError = ref(false)
const errorMessage = ref('')

// Chart ref for updateSeries
const chartRef = ref<InstanceType<typeof VueApexCharts> | null>(null)
const renderSeries = ref<any[]>([])

// Auto refresh timer (keep charts fresh without hammering the API)
const AUTO_REFRESH_INTERVAL = 10000
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null

const startAutoRefresh = () => {
  if (props.autoRefresh === false) return
  if (document.visibilityState === 'hidden') return
  stopAutoRefresh()
  autoRefreshTimer = setInterval(() => {
    if (document.visibilityState === 'hidden') return
    if (!isRefreshing.value) {
      refreshData(true)
    }
  }, AUTO_REFRESH_INTERVAL)
}

const stopAutoRefresh = () => {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}

// Pause/resume auto refresh on hover (to keep tooltip visible)
const isHovering = ref(false)

const pauseAutoRefresh = () => {
  isHovering.value = true
  stopAutoRefresh()
}

const resumeAutoRefresh = () => {
  isHovering.value = false
  if (props.autoRefresh !== false) {
    startAutoRefresh()
  }
}

// Chart height
const chartHeight = 260

// Summary data
const summary = computed(() => {
  if (selectedView.value === 'cost') return providerHistoryData.value?.summary || null
  return historyData.value?.summary || null
})

// Check if has data
const hasData = computed(() => {
  if (selectedView.value === 'cost') {
    if (!providerHistoryData.value?.providers?.length) return false
    return true
  }

  if (!historyData.value?.dataPoints) return false
  return historyData.value.dataPoints.length > 0 && historyData.value.dataPoints.some(dp => dp.requests > 0)
})

// Chart colors
const chartColors = {
  traffic: ['#3b82f6', '#10b981'],  // Blue for requests, Green for success
  tokens: ['#8b5cf6', '#f97316', '#22c55e', '#eab308'],  // Purple input, Orange output, Green cache create, Yellow cache hit
  latency: ['#06b6d4', '#ef4444'],  // Cyan p50, Red p95
  cost: ['#3b82f6', '#10b981', '#f59e0b', '#8b5cf6', '#ef4444', '#06b6d4', '#ec4899', '#84cc16', '#f97316', '#14b8a6', '#a855f7', '#64748b']
}

// Format number for display
const formatNumber = (num: number): string => {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M'
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K'
  return num.toFixed(0)
}

const formatDurationMs = (ms: number): string => {
  if (!ms || ms <= 0) return '--'
  if (ms >= 10000) return (ms / 1000).toFixed(0) + 's'
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return Math.round(ms) + 'ms'
}

const formatCurrency = (amount: number): string => {
  if (typeof amount !== 'number' || Number.isNaN(amount)) return '--'
  const abs = Math.abs(amount)
  if (abs >= 1000000) return '$' + (amount / 1000000).toFixed(2) + 'M'
  if (abs >= 1000) return '$' + (amount / 1000).toFixed(1) + 'K'
  return '$' + amount.toFixed(2)
}

// Chart options
const chartOptions = computed(() => {
  const mode = selectedView.value

  const yaxis: Record<string, any> = {
    labels: {
      formatter: (val: number) =>
        mode === 'traffic' ? Math.round(val).toString() :
          mode === 'latency' ? (val <= 0 ? '0ms' : formatDurationMs(val)) :
            mode === 'cost' ? formatCurrency(val) :
              formatNumber(val),
      style: { fontSize: '11px' }
    },
    min: 0
  }

  // ApexCharts can throw "parser Error" when the y-range collapses (e.g. all-zero series).
  // Keep a tiny positive range for cost view to avoid NaN path calculations.
  if (mode === 'cost') {
    yaxis.max = (max: number) => (max <= 0 ? 1 : max)
    yaxis.forceNiceScale = true
  }

  return {
    chart: {
      toolbar: { show: false },
      zoom: { enabled: false },
      background: 'transparent',
      fontFamily: 'inherit',
      animations: {
        enabled: true,
        speed: 400,
        animateGradually: { enabled: true, delay: 150 },
        dynamicAnimation: { enabled: true, speed: 350 }
      }
    },
    theme: {
      mode: (isDark.value ? 'dark' : 'light') as 'dark' | 'light'
    },
    colors: chartColors[mode],
    fill: {
      type: 'gradient',
      gradient: {
        shadeIntensity: 1,
        opacityFrom: 0.4,
        opacityTo: 0.08,
        stops: [0, 90, 100]
      }
    },
    dataLabels: {
      enabled: false
    },
    stroke: {
      curve: (mode === 'cost' ? 'straight' : 'smooth') as 'smooth' | 'straight',
      width: 2,
      dashArray: mode === 'tokens' ? [0, 0, 5, 5] : mode === 'latency' ? [0, 5] : [0, 0]
    },
    grid: {
      borderColor: isDark.value ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.1)',
      padding: { left: 10, right: 10 }
    },
    xaxis: {
      type: 'datetime' as const,
      labels: {
        datetimeUTC: false,
        format: 'HH:mm',
        style: { fontSize: '10px' }
      },
      axisBorder: { show: false },
      axisTicks: { show: false }
    },
    yaxis,
    tooltip: {
      shared: true,
      intersect: false,
      x: {
        format: 'MM-dd HH:mm'
      },
      y: {
        formatter: (val: number) =>
          mode === 'traffic'
            ? `${Math.round(val)} ${t('chart.unit.requests')}`
            : mode === 'latency'
              ? formatDurationMs(val)
              : mode === 'cost'
                ? formatCurrency(val)
                : formatNumber(val)
      }
    },
    legend: {
      show: true,
      position: 'top' as const,
      horizontalAlign: 'right' as const,
      fontSize: '11px',
      markers: { size: 4 }
    }
  }
})

// Build chart series
const chartSeries = computed(() => {
  const mode = selectedView.value

  if (mode === 'cost') {
    const providers = providerHistoryData.value?.providers
    if (!providers) return []
    return providers.map(p => ({
      name: p.provider,
      data: (() => {
        let cumulativeCost = toFiniteNumber(p.baselineCost) ?? 0
        return (p.dataPoints || [])
          .map(dp => {
            cumulativeCost += toFiniteNumber(dp.cost) ?? 0
            const x = Number.isFinite(Date.parse(dp.timestamp)) ? Date.parse(dp.timestamp) : null
            if (x === null) {
              console.warn('Invalid timestamp in provider history dataPoint:', dp)
              return null
            }
            return { x, y: cumulativeCost }
          })
          .filter(Boolean)
      })()
    }))
  }

  if (!historyData.value?.dataPoints) return []
  const dataPoints = historyData.value.dataPoints

  if (mode === 'traffic') {
    return [
      {
        name: t('chart.legend.totalRequests'),
        data: dataPoints.map(dp => ({
          x: new Date(dp.timestamp).getTime(),
          y: dp.requests
        }))
      },
      {
        name: t('chart.legend.success'),
        data: dataPoints.map(dp => ({
          x: new Date(dp.timestamp).getTime(),
          y: dp.success
        }))
      }
    ]
  } else if (mode === 'tokens') {
    // tokens mode - show input, output, cache create, cache hit
    return [
      {
        name: t('chart.legend.inputTokens'),
        data: dataPoints.map(dp => ({
          x: new Date(dp.timestamp).getTime(),
          y: dp.inputTokens
        }))
      },
      {
        name: t('chart.legend.outputTokens'),
        data: dataPoints.map(dp => ({
          x: new Date(dp.timestamp).getTime(),
          y: dp.outputTokens
        }))
      },
      {
        name: t('chart.legend.cacheCreate'),
        data: dataPoints.map(dp => ({
          x: new Date(dp.timestamp).getTime(),
          y: dp.cacheCreationInputTokens
        }))
      },
      {
        name: t('chart.legend.cacheHit'),
        data: dataPoints.map(dp => ({
          x: new Date(dp.timestamp).getTime(),
          y: dp.cacheReadInputTokens
        }))
      }
    ]
  } else {
    // latency mode - show p50/p95 duration (ms)
    return [
      {
        name: t('chart.legend.p50Latency'),
        data: dataPoints.map(dp => ({
          x: new Date(dp.timestamp).getTime(),
          y: dp.p50DurationMs
        }))
      },
      {
        name: t('chart.legend.p95Latency'),
        data: dataPoints.map(dp => ({
          x: new Date(dp.timestamp).getTime(),
          y: dp.p95DurationMs
        }))
      }
    ]
  }
})

function toFiniteNumber(value: unknown): number | null {
  const num = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(num) ? num : null
}

const normalizeSeries = (series: any[]) =>
  series
    .map(s => ({
      ...s,
      data: Array.isArray(s.data)
        ? s.data
          .map((p: any) => {
            const x = toFiniteNumber(p?.x)
            const y = toFiniteNumber(p?.y)
            if (x === null || y === null) return null
            return { x, y }
          })
          .filter(Boolean)
        : []
    }))
    .filter(s => Array.isArray(s.data) && s.data.length > 0)

const applySeriesToChart = async (series: any[], animate: boolean) => {
  const nextSeries = normalizeSeries(series)

  if (!chartRef.value) {
    renderSeries.value = nextSeries
    return
  }

  try {
    await chartRef.value.updateSeries(nextSeries, animate)
  } catch (err) {
    console.error('ApexCharts updateSeries failed, re-initializing chart:', err)
    try {
      chartRef.value.destroy()
      renderSeries.value = nextSeries
      await chartRef.value.init()
    } catch (reinitErr) {
      console.error('ApexCharts re-init failed:', reinitErr)
    }
  }
}

// Fetch data
const refreshData = async (isAutoRefresh = false) => {
  if (isRefreshing.value) {
    // Coalesce refreshes: user-triggered refresh should override queued auto refresh
    if (pendingRefresh.value === null || (!isAutoRefresh && pendingRefresh.value)) {
      pendingRefresh.value = isAutoRefresh
    }
    if (!isAutoRefresh) {
      isLoading.value = true
    }
    return
  }
  isRefreshing.value = true
  pendingRefresh.value = null

  if (!isAutoRefresh) {
    isLoading.value = true
  }
  errorMessage.value = ''

  try {
    const endpoint =
      selectedEndpoint.value === 'messages'
        ? '/v1/messages'
        : selectedEndpoint.value === 'responses'
          ? '/v1/responses'
          : selectedEndpoint.value === 'gemini'
            ? '/v1/gemini'
          : undefined

    if (selectedView.value === 'cost') {
      const newData = await api.getProviderStatsHistory(selectedDuration.value, endpoint, props.from, props.to)
      providerHistoryData.value = newData
      await applySeriesToChart(chartSeries.value, false)
      return
    }

    const newData = await api.getStatsHistory(selectedDuration.value, endpoint, props.from, props.to)
    historyData.value = newData
    await applySeriesToChart(chartSeries.value, false)
  } catch (error) {
    console.error('Failed to fetch global stats:', error)
    if (!isAutoRefresh) {
      errorMessage.value = error instanceof Error ? error.message : t('chart.error.fetchFailed')
      showError.value = true
    }
  } finally {
    isRefreshing.value = false
    if (!isAutoRefresh) {
      isLoading.value = false
    }

    const nextRefresh = pendingRefresh.value
    pendingRefresh.value = null
    if (nextRefresh !== null) {
      await refreshData(nextRefresh)
    }
  }
}

// Watchers
watch(hasExternalRange, (hasRange) => {
  if (hasRange) {
    if (!durationTouched.value && selectedDuration.value !== 'period') {
      selectedDuration.value = 'period'
    }
  } else if (selectedDuration.value === 'period') {
    selectedDuration.value = '6h'
  }
})

watch(() => [props.from, props.to], () => {
  refreshData()
})

watch(selectedEndpoint, (newVal) => {
  savePreference('endpoint', newVal)
  refreshData()
})

watch(selectedDuration, (newVal) => {
  savePreference('duration', newVal)
  refreshData()
})

watch(selectedView, (newVal, oldVal) => {
  savePreference('viewMode', newVal)
  if (newVal === 'cost' || oldVal === 'cost') {
    refreshData()
  }
})

// Initial load and start auto refresh
onMounted(() => {
  refreshData()
  if (props.autoRefresh !== false) {
    startAutoRefresh()
  }
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

// Cleanup timer on unmount
onUnmounted(() => {
  stopAutoRefresh()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})

// Allow parent to toggle auto refresh dynamically (e.g. based on SSE state).
watch(() => props.autoRefresh, (enabled) => {
  if (enabled === false) {
    stopAutoRefresh()
    return
  }
  startAutoRefresh()
})

const handleVisibilityChange = () => {
  if (document.visibilityState === 'hidden') {
    stopAutoRefresh()
    return
  }
  if (props.autoRefresh === false) return
  if (isHovering.value) return
  startAutoRefresh()
}

// Expose refresh method
defineExpose({
  refreshData,
  startAutoRefresh,
  stopAutoRefresh
})
</script>

<style scoped>
.global-stats-chart-container {
  padding: 12px 16px;
}

.summary-cards {
  display: flex;
  flex-wrap: wrap;
}

.summary-card {
  flex: 1 1 auto;
  min-width: 80px;
  padding: 8px 12px;
  background: rgba(var(--v-theme-surface-variant), 0.3);
  border-radius: 6px;
  text-align: center;
}

.v-theme--dark .summary-card {
  background: rgba(var(--v-theme-surface-variant), 0.2);
}

.summary-label {
  font-size: 11px;
  color: rgba(var(--v-theme-on-surface), 0.6);
  margin-bottom: 2px;
}

.summary-value {
  font-size: 16px;
  font-weight: 600;
}

.chart-header {
  flex-wrap: wrap;
  gap: 8px;
  padding-right: 48px;
}

.chart-area {
  margin-top: 8px;
}

/* Responsive adjustments */
@media (max-width: 600px) {
  .summary-card {
    min-width: 70px;
    padding: 6px 8px;
  }

  .summary-value {
    font-size: 14px;
  }
}
</style>
