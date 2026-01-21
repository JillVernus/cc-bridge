<template>
  <div class="channel-stats-chart-container">
    <!-- Snackbar for error notification -->
    <v-snackbar v-model="showError" color="error" :timeout="3000" location="top">
      {{ errorMessage }}
      <template #actions>
        <v-btn variant="text" @click="showError = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>

    <!-- Header: Duration selector + View switcher + Close button -->
    <div class="chart-header d-flex align-center justify-space-between mb-3">
      <div class="d-flex align-center ga-2">
        <!-- Duration selector -->
        <v-btn-toggle v-model="selectedDuration" mandatory density="compact" variant="outlined" divided :disabled="isLoading">
          <v-btn value="1h" size="x-small">{{ t('chart.duration.1h') }}</v-btn>
          <v-btn value="6h" size="x-small">{{ t('chart.duration.6h') }}</v-btn>
          <v-btn value="24h" size="x-small">{{ t('chart.duration.24h') }}</v-btn>
          <v-btn value="today" size="x-small">{{ t('chart.duration.today') }}</v-btn>
        </v-btn-toggle>

        <v-btn icon size="x-small" variant="text" @click="refreshData" :loading="isLoading" :disabled="isLoading">
          <v-icon size="small">mdi-refresh</v-icon>
        </v-btn>
      </div>

      <div class="d-flex align-center ga-2">
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
        </v-btn-toggle>

        <!-- Close button -->
        <v-btn icon size="x-small" variant="text" @click="$emit('close')" :title="t('common.close')">
          <v-icon size="small">mdi-chevron-up</v-icon>
        </v-btn>
      </div>
    </div>

    <!-- Loading state -->
    <div v-if="isLoading" class="d-flex justify-center align-center" style="height: 200px">
      <v-progress-circular indeterminate size="32" color="primary" />
    </div>

    <!-- Empty state -->
    <div v-else-if="!hasData" class="d-flex flex-column justify-center align-center text-medium-emphasis" style="height: 200px">
      <v-icon size="40" color="grey-lighten-1">mdi-chart-timeline-variant</v-icon>
      <div class="text-caption mt-2">{{ t('chart.noDataChannel') }}</div>
    </div>

    <!-- Chart -->
    <div v-else class="chart-area" @mouseenter="pauseAutoRefresh" @mouseleave="resumeAutoRefresh">
      <apexchart
        ref="chartRef"
        type="area"
        height="280"
        :options="chartOptions"
        :series="chartSeries"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useTheme } from 'vuetify'
import { useI18n } from 'vue-i18n'
import VueApexCharts from 'vue3-apexcharts'
import { api, type ChannelStatsHistoryResponse, type Duration } from '../services/api'

// Register apexchart component
const apexchart = VueApexCharts

// i18n
const { t } = useI18n()

// Props
const props = defineProps<{
  channelId: number
  channelType: 'messages' | 'responses' | 'gemini'
}>()

// Emits
defineEmits<{
  close: []
}>()

// View mode type
type ViewMode = 'traffic' | 'tokens'

// Theme
const theme = useTheme()
const isDark = computed(() => theme.global.current.value.dark)

// State
const selectedView = ref<ViewMode>('traffic')
const selectedDuration = ref<Duration>('1h')
const isLoading = ref(false)
const historyData = ref<ChannelStatsHistoryResponse | null>(null)
const showError = ref(false)
const errorMessage = ref('')

// Chart ref for updateSeries
const chartRef = ref<InstanceType<typeof VueApexCharts> | null>(null)

// Auto refresh timer (2 seconds interval)
const AUTO_REFRESH_INTERVAL = 2000
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null

const startAutoRefresh = () => {
  stopAutoRefresh()
  autoRefreshTimer = setInterval(() => {
    if (!isLoading.value) {
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
  startAutoRefresh()
}

// Chart colors
const chartColors = {
  traffic: ['#3b82f6', '#10b981'],  // Blue for requests, Green for success
  tokens: ['#8b5cf6', '#f97316', '#22c55e', '#eab308']  // Purple input, Orange output, Green cache create, Yellow cache hit
}

// Check if has data
const hasData = computed(() => {
  if (!historyData.value?.dataPoints) return false
  return historyData.value.dataPoints.length > 0 &&
    historyData.value.dataPoints.some(dp => dp.requests > 0)
})

// Helper: format number for display
const formatNumber = (num: number): string => {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M'
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K'
  return num.toFixed(0)
}

// Chart options
const chartOptions = computed(() => {
  const mode = selectedView.value

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
      curve: 'smooth' as const,
      width: 2,
      dashArray: mode === 'tokens' ? [0, 0, 5, 5] : [0, 0]
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
    yaxis: {
      labels: {
        formatter: (val: number) => mode === 'traffic' ? Math.round(val).toString() : formatNumber(val),
        style: { fontSize: '11px' }
      },
      min: 0
    },
    tooltip: {
      shared: true,
      intersect: false,
      x: {
        format: 'MM-dd HH:mm'
      },
      y: {
        formatter: (val: number) => mode === 'traffic'
          ? `${Math.round(val)} ${t('chart.unit.requests')}`
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
  if (!historyData.value?.dataPoints) return []

  const dataPoints = historyData.value.dataPoints
  const mode = selectedView.value

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
  } else {
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
  }
})

// Fetch data
const refreshData = async (isAutoRefresh = false) => {
  if (!isAutoRefresh) {
    isLoading.value = true
  }
  errorMessage.value = ''

  try {
    const endpoint =
      props.channelType === 'responses'
        ? '/v1/responses'
        : props.channelType === 'gemini'
          ? '/v1/gemini'
          : '/v1/messages'
    const newData = await api.getChannelStatsHistory(props.channelId, selectedDuration.value, endpoint)

    // Check if we can use updateSeries for smooth update
    const canUpdateInPlace = isAutoRefresh &&
      chartRef.value &&
      historyData.value?.dataPoints?.length === newData.dataPoints?.length

    if (canUpdateInPlace) {
      historyData.value = newData
      const series = chartSeries.value
      chartRef.value?.updateSeries(series, false)
    } else {
      historyData.value = newData
    }
  } catch (error) {
    console.error('Failed to fetch channel stats:', error)
    errorMessage.value = error instanceof Error ? error.message : t('chart.error.fetchFailed')
    showError.value = true
    historyData.value = null
  } finally {
    if (!isAutoRefresh) {
      isLoading.value = false
    }
  }
}

// Watchers
watch(selectedDuration, () => {
  refreshData()
})

// Initial load and start auto refresh
onMounted(() => {
  refreshData()
  startAutoRefresh()
})

// Cleanup timer on unmount
onUnmounted(() => {
  stopAutoRefresh()
})

// Expose refresh method
defineExpose({
  refreshData
})
</script>

<style scoped>
.channel-stats-chart-container {
  padding: 12px 16px;
  background: rgba(var(--v-theme-surface-variant), 0.3);
  border-top: 1px dashed rgba(var(--v-theme-on-surface), 0.2);
}

.v-theme--dark .channel-stats-chart-container {
  background: rgba(var(--v-theme-surface-variant), 0.2);
  border-top-color: rgba(255, 255, 255, 0.15);
}

.chart-header {
  flex-wrap: wrap;
  gap: 8px;
}

.chart-area {
  margin-top: 8px;
}
</style>
