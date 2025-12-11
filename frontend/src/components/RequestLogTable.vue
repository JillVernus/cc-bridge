<template>
  <div class="request-log-container">
    <!-- 统计面板和日期筛选 -->
    <v-row class="mb-4">
      <!-- 模型统计 -->
      <v-col cols="4">
        <v-card class="summary-card" density="compact">
          <v-card-title class="text-subtitle-2 pa-2">按模型统计</v-card-title>
          <v-table density="compact" class="summary-table">
            <thead>
              <tr>
                <th>模型</th>
                <th class="text-end">请求</th>
                <th class="text-end">输入</th>
                <th class="text-end">输出</th>
                <th class="text-end">缓存创建</th>
                <th class="text-end">缓存命中</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(data, model) in stats?.byModel" :key="model">
                <td class="text-caption">{{ truncateModel(String(model)) }}</td>
                <td class="text-end text-caption">{{ data.count }}</td>
                <td class="text-end text-caption">{{ formatNumber(data.inputTokens) }}</td>
                <td class="text-end text-caption">{{ formatNumber(data.outputTokens) }}</td>
                <td class="text-end text-caption text-success">{{ formatNumber(data.cacheCreationInputTokens) }}</td>
                <td class="text-end text-caption text-warning">{{ formatNumber(data.cacheReadInputTokens) }}</td>
              </tr>
              <tr v-if="!stats?.byModel || Object.keys(stats.byModel).length === 0">
                <td colspan="6" class="text-center text-caption text-grey">暂无数据</td>
              </tr>
            </tbody>
          </v-table>
        </v-card>
      </v-col>

      <!-- 渠道统计 -->
      <v-col cols="4">
        <v-card class="summary-card" density="compact">
          <v-card-title class="text-subtitle-2 pa-2">按渠道统计</v-card-title>
          <v-table density="compact" class="summary-table">
            <thead>
              <tr>
                <th>渠道</th>
                <th class="text-end">请求</th>
                <th class="text-end">输入</th>
                <th class="text-end">输出</th>
                <th class="text-end">缓存创建</th>
                <th class="text-end">缓存命中</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(data, provider) in stats?.byProvider" :key="provider">
                <td class="text-caption">{{ provider }}</td>
                <td class="text-end text-caption">{{ data.count }}</td>
                <td class="text-end text-caption">{{ formatNumber(data.inputTokens) }}</td>
                <td class="text-end text-caption">{{ formatNumber(data.outputTokens) }}</td>
                <td class="text-end text-caption text-success">{{ formatNumber(data.cacheCreationInputTokens) }}</td>
                <td class="text-end text-caption text-warning">{{ formatNumber(data.cacheReadInputTokens) }}</td>
              </tr>
              <tr v-if="!stats?.byProvider || Object.keys(stats.byProvider).length === 0">
                <td colspan="6" class="text-center text-caption text-grey">暂无数据</td>
              </tr>
            </tbody>
          </v-table>
        </v-card>
      </v-col>

      <!-- 日期筛选 -->
      <v-col cols="4">
        <v-card class="summary-card date-filter-card" density="compact">
          <v-card-title class="text-subtitle-2 pa-2">日期筛选</v-card-title>
          <div class="date-filter d-flex align-center justify-center pa-4">
            <v-btn icon variant="text" size="small" class="neo-btn" @click="prevDay">
              <v-icon size="20">mdi-chevron-left</v-icon>
            </v-btn>
            <span class="date-display mx-4 text-h6">{{ filterDate }}</span>
            <v-btn icon variant="text" size="small" class="neo-btn" @click="nextDay" :disabled="isToday">
              <v-icon size="20">mdi-chevron-right</v-icon>
            </v-btn>
          </div>
        </v-card>
      </v-col>
    </v-row>

    <!-- 设置对话框 -->
    <v-dialog v-model="showSettings" max-width="500">
      <v-card class="settings-card">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-cog</v-icon>
          设置
        </v-card-title>
        <v-card-text>
          <div class="settings-section mb-4">
            <div class="text-subtitle-2 mb-2">列宽设置</div>
            <v-btn
              color="primary"
              variant="tonal"
              @click="resetColumnWidths"
            >
              <v-icon class="mr-1">mdi-table-refresh</v-icon>
              重置列宽
            </v-btn>
            <div class="text-caption text-grey mt-2">将所有列宽恢复为默认值</div>
          </div>

          <v-divider class="my-4" />

          <div class="settings-section">
            <div class="text-subtitle-2 mb-2">数据库操作</div>
            <v-btn
              color="error"
              variant="tonal"
              @click="confirmClearLogs"
              :loading="clearing"
            >
              <v-icon class="mr-1">mdi-delete</v-icon>
              清空日志数据
            </v-btn>
            <div class="text-caption text-grey mt-2">删除所有请求日志记录，此操作不可恢复</div>
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showSettings = false">关闭</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 确认对话框 -->
    <v-dialog v-model="showConfirmClear" max-width="400">
      <v-card>
        <v-card-title class="text-error">确认清空</v-card-title>
        <v-card-text>确定要删除所有日志数据吗？此操作不可恢复。</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showConfirmClear = false">取消</v-btn>
          <v-btn color="error" variant="flat" @click="clearLogs" :loading="clearing">确认删除</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 操作栏 -->
    <div class="action-bar mb-4">
      <v-btn
        variant="text"
        size="small"
        class="neo-btn"
        :class="{ 'neo-btn-active': autoRefreshEnabled }"
        @click="toggleAutoRefresh"
      >
        <v-icon size="18" class="mr-1">{{ autoRefreshEnabled ? 'mdi-sync' : 'mdi-sync-off' }}</v-icon>
        {{ autoRefreshEnabled ? '自动刷新中...' : '自动刷新已关闭' }}
      </v-btn>
      <v-spacer />
      <v-chip class="mr-2" size="small" variant="tonal">
        共 {{ total }} 条记录
      </v-chip>
      <v-btn icon variant="text" size="small" class="neo-btn" @click="showSettings = true">
        <v-icon size="20">mdi-cog</v-icon>
      </v-btn>
    </div>

    <!-- 日志表格 -->
    <v-card class="log-table-card">
      <v-data-table
        :headers="headers"
        :items="logs"
        :loading="loading"
        :items-per-page="pageSize"
        density="compact"
        class="log-table resizable-table"
        :row-props="getRowProps"
      >
        <!-- Custom headers with resize handles -->
        <template v-slot:[`header.status`]="{ column }">
          <div class="resizable-header">
            {{ column.title }}
            <div class="resize-handle" @mousedown="startResize($event, 'status')"></div>
          </div>
        </template>
        <template v-slot:[`header.initialTime`]="{ column }">
          <div class="resizable-header">
            {{ column.title }}
            <div class="resize-handle" @mousedown="startResize($event, 'initialTime')"></div>
          </div>
        </template>
        <template v-slot:[`header.durationMs`]="{ column }">
          <div class="resizable-header">
            {{ column.title }}
            <div class="resize-handle" @mousedown="startResize($event, 'durationMs')"></div>
          </div>
        </template>
        <template v-slot:[`header.providerName`]="{ column }">
          <div class="resizable-header">
            {{ column.title }}
            <div class="resize-handle" @mousedown="startResize($event, 'providerName')"></div>
          </div>
        </template>
        <template v-slot:[`header.model`]="{ column }">
          <div class="resizable-header">
            {{ column.title }}
            <div class="resize-handle" @mousedown="startResize($event, 'model')"></div>
          </div>
        </template>
        <template v-slot:[`header.tokens`]>
          <div class="resizable-header tokens-header">
            <div class="tokens-header-row">
              <span class="token-label input-color">In</span>
              <span class="token-label output-color">Out</span>
              <span class="token-label cache-create-color">Cache</span>
              <span class="token-label cache-hit-color">Hit</span>
            </div>
            <div class="resize-handle" @mousedown="startResize($event, 'tokens')"></div>
          </div>
        </template>
        <template v-slot:[`header.httpStatus`]="{ column }">
          <div class="resizable-header-last">
            {{ column.title }}
          </div>
        </template>

        <template v-slot:item.status="{ item }">
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="16"
            width="2"
            color="warning"
          />
          <v-chip
            v-else
            size="x-small"
            :color="getRequestStatusColor(item.status)"
            variant="flat"
          >
            {{ getRequestStatusLabel(item.status) }}
          </v-chip>
        </template>

        <template v-slot:item.initialTime="{ item }">
          <span class="text-caption">{{ formatTime(item.initialTime) }}</span>
        </template>

        <template v-slot:item.durationMs="{ item }">
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="16"
            width="2"
            color="grey"
          />
          <v-chip v-else size="x-small" :color="getDurationColor(item.durationMs)" variant="tonal">
            {{ item.durationMs }}ms
          </v-chip>
        </template>

        <template v-slot:item.providerName="{ item }">
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="16"
            width="2"
            color="grey"
          />
          <v-chip v-else size="x-small" :color="getProviderColor(item.type)" variant="flat">
            {{ item.providerName || item.type }}<span v-if="item.type" class="text-caption ml-1 opacity-60">({{ item.type }})</span>
          </v-chip>
        </template>

        <template v-slot:item.model="{ item }">
          <span class="text-caption font-weight-medium">{{ item.model }}</span>
        </template>

        <template v-slot:item.tokens="{ item }">
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="16"
            width="2"
            color="grey"
          />
          <div v-else class="tokens-cell">
            <span class="token-value input-color">{{ formatNumber(item.inputTokens) }} (<v-icon size="12">mdi-arrow-up</v-icon>)</span>
            <span class="token-value output-color">{{ formatNumber(item.outputTokens) }} (<v-icon size="12">mdi-arrow-down</v-icon>)</span>
            <span class="token-value cache-create-color">{{ formatNumber(item.cacheCreationInputTokens) }} (<v-icon size="12">mdi-arrow-up</v-icon>)</span>
            <span class="token-value cache-hit-color">{{ formatNumber(item.cacheReadInputTokens) }} (<v-icon size="12">mdi-flash</v-icon>)</span>
          </div>
        </template>

        <template v-slot:item.httpStatus="{ item }">
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="16"
            width="2"
            color="grey"
          />
          <v-tooltip v-else-if="item.error" location="top">
            <template v-slot:activator="{ props }">
              <v-chip v-bind="props" size="x-small" :color="getStatusColor(item.httpStatus)" variant="tonal">
                {{ item.httpStatus }}
              </v-chip>
            </template>
            <span>{{ item.error }}</span>
          </v-tooltip>
          <v-chip v-else size="x-small" :color="getStatusColor(item.httpStatus)" variant="tonal">
            {{ item.httpStatus }}
          </v-chip>
        </template>

        <template v-slot:bottom>
          <div class="d-flex align-center justify-end pa-2">
            <v-btn
              variant="text"
              size="small"
              :disabled="offset === 0"
              @click="prevPage"
            >
              上一页
            </v-btn>
            <span class="mx-2 text-caption">
              {{ offset + 1 }} - {{ Math.min(offset + pageSize, total) }} / {{ total }}
            </span>
            <v-btn
              variant="text"
              size="small"
              :disabled="!hasMore"
              @click="nextPage"
            >
              下一页
            </v-btn>
          </div>
        </template>
      </v-data-table>
    </v-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { api, type RequestLog, type RequestLogStats, type GroupStats } from '../services/api'

const logs = ref<RequestLog[]>([])
const stats = ref<RequestLogStats | null>(null)
const loading = ref(false)
const total = ref(0)
const hasMore = ref(false)
const offset = ref(0)
const pageSize = 50
const updatedIds = ref<Set<string>>(new Set())

// Settings
const showSettings = ref(false)
const showConfirmClear = ref(false)
const clearing = ref(false)

// Date filter - use local date
const getLocalDateString = (date: Date) => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
const filterDate = ref(getLocalDateString(new Date()))
const isToday = computed(() => filterDate.value === getLocalDateString(new Date()))

const getDateRange = () => {
  // RFC3339 format with timezone offset
  const from = `${filterDate.value}T00:00:00+08:00`
  const to = `${filterDate.value}T23:59:59+08:00`
  return { from, to }
}

const prevDay = () => {
  const d = new Date(filterDate.value)
  d.setDate(d.getDate() - 1)
  filterDate.value = d.toISOString().split('T')[0]
}

const nextDay = () => {
  if (!isToday.value) {
    const d = new Date(filterDate.value)
    d.setDate(d.getDate() + 1)
    filterDate.value = d.toISOString().split('T')[0]
  }
}

// Auto-refresh
const autoRefreshEnabled = ref(true)
const AUTO_REFRESH_INTERVAL = 3000
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null

// Column widths with defaults
const defaultColumnWidths: Record<string, number> = {
  status: 80,
  initialTime: 140,
  durationMs: 80,
  providerName: 120,
  model: 200,
  tokens: 400,
  httpStatus: 70
}

const columnWidths = ref<Record<string, number>>({ ...defaultColumnWidths })

// Load column widths from localStorage
const loadColumnWidths = () => {
  try {
    const saved = localStorage.getItem('requestlog-column-widths')
    if (saved) {
      columnWidths.value = { ...defaultColumnWidths, ...JSON.parse(saved) }
    }
  } catch (e) {
    console.error('Failed to load column widths:', e)
  }
}

// Save column widths to localStorage
const saveColumnWidths = () => {
  try {
    localStorage.setItem('requestlog-column-widths', JSON.stringify(columnWidths.value))
  } catch (e) {
    console.error('Failed to save column widths:', e)
  }
}

// Reset column widths to defaults
const resetColumnWidths = () => {
  columnWidths.value = { ...defaultColumnWidths }
  saveColumnWidths()
}

const headers = computed(() => [
  { title: '状态', key: 'status', sortable: false, width: `${columnWidths.value.status}px` },
  { title: '时间', key: 'initialTime', sortable: false, width: `${columnWidths.value.initialTime}px` },
  { title: '耗时', key: 'durationMs', sortable: false, width: `${columnWidths.value.durationMs}px` },
  { title: '渠道', key: 'providerName', sortable: false, width: `${columnWidths.value.providerName}px` },
  { title: '模型', key: 'model', sortable: false, width: `${columnWidths.value.model}px` },
  { title: 'Tokens', key: 'tokens', sortable: false, width: `${columnWidths.value.tokens}px` },
  { title: 'HTTP', key: 'httpStatus', sortable: false, width: `${columnWidths.value.httpStatus}px` },
])

// Column resize logic
const resizingColumn = ref<string | null>(null)
const resizeStartX = ref(0)
const resizeStartWidth = ref(0)

const startResize = (e: MouseEvent, columnKey: string) => {
  e.preventDefault()
  resizingColumn.value = columnKey
  resizeStartX.value = e.pageX
  resizeStartWidth.value = columnWidths.value[columnKey]

  document.addEventListener('mousemove', onResize)
  document.addEventListener('mouseup', stopResize)
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
}

const onResize = (e: MouseEvent) => {
  if (!resizingColumn.value) return

  const diff = e.pageX - resizeStartX.value
  const newWidth = Math.max(50, Math.min(500, resizeStartWidth.value + diff))
  columnWidths.value[resizingColumn.value] = newWidth
}

const stopResize = () => {
  if (resizingColumn.value) {
    saveColumnWidths()
  }
  resizingColumn.value = null
  document.removeEventListener('mousemove', onResize)
  document.removeEventListener('mouseup', stopResize)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
}

const refreshLogs = async () => {
  loading.value = true
  try {
    const { from, to } = getDateRange()
    const [logsRes, statsRes] = await Promise.all([
      api.getRequestLogs({ limit: pageSize, offset: offset.value, from, to }),
      api.getRequestLogStats({ from, to })
    ])
    logs.value = logsRes.requests || []
    total.value = logsRes.total
    hasMore.value = logsRes.hasMore
    stats.value = statsRes
  } catch (error) {
    console.error('Failed to load logs:', error)
  } finally {
    loading.value = false
  }
}

// Watch date changes
watch(filterDate, () => {
  offset.value = 0
  refreshLogs()
})

const prevPage = () => {
  if (offset.value > 0) {
    offset.value = Math.max(0, offset.value - pageSize)
    refreshLogs()
  }
}

const nextPage = () => {
  if (hasMore.value) {
    offset.value += pageSize
    refreshLogs()
  }
}

const formatTime = (time: string) => {
  const d = new Date(time)
  return d.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

const formatNumber = (n: number) => {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return String(n)
}

const truncateModel = (model: string) => {
  if (model.length > 25) return model.slice(0, 22) + '...'
  return model
}

const calcHitRate = (data: GroupStats) => {
  const total = data.inputTokens + data.cacheReadInputTokens
  if (total === 0) return 0
  return Math.round((data.cacheReadInputTokens / total) * 100)
}

const getDurationColor = (ms: number) => {
  if (ms <= 5000) return 'success'
  if (ms <= 15000) return 'warning'
  return 'error'
}

const getProviderColor = (provider: string) => {
  const colors: Record<string, string> = {
    claude: 'deep-purple',
    openai: 'green',
    gemini: 'blue'
  }
  return colors[provider] || 'grey'
}

const getStatusColor = (status: number) => {
  if (status >= 200 && status < 300) return 'success'
  if (status >= 400 && status < 500) return 'warning'
  return 'error'
}

const getRequestStatusColor = (status: string) => {
  const colors: Record<string, string> = {
    pending: 'warning',
    completed: 'success',
    error: 'error'
  }
  return colors[status] || 'grey'
}

const getRequestStatusLabel = (status: string) => {
  const labels: Record<string, string> = {
    pending: '进行中',
    completed: '完成',
    error: '错误'
  }
  return labels[status] || status
}

const getRowProps = ({ item }: { item: RequestLog }) => {
  return {
    class: updatedIds.value.has(item.id) ? 'row-flash' : ''
  }
}

const confirmClearLogs = () => {
  showConfirmClear.value = true
}

const clearLogs = async () => {
  clearing.value = true
  try {
    await api.clearRequestLogs()
    showConfirmClear.value = false
    showSettings.value = false
    refreshLogs()
  } catch (error) {
    console.error('Failed to clear logs:', error)
  } finally {
    clearing.value = false
  }
}

onMounted(() => {
  loadColumnWidths()
  refreshLogs()
  startAutoRefresh()
})

onUnmounted(() => {
  stopAutoRefresh()
})

const startAutoRefresh = () => {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
  }
  if (autoRefreshEnabled.value) {
    autoRefreshTimer = setInterval(() => {
      if (autoRefreshEnabled.value) {
        silentRefresh()
      }
    }, AUTO_REFRESH_INTERVAL)
  }
}

const stopAutoRefresh = () => {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}

const toggleAutoRefresh = () => {
  autoRefreshEnabled.value = !autoRefreshEnabled.value
  if (autoRefreshEnabled.value) {
    startAutoRefresh()
  } else {
    stopAutoRefresh()
  }
}

// Silent refresh without loading indicator
const silentRefresh = async () => {
  try {
    const { from, to } = getDateRange()
    const [logsRes, statsRes] = await Promise.all([
      api.getRequestLogs({ limit: pageSize, offset: offset.value, from, to }),
      api.getRequestLogStats({ from, to })
    ])

    // Detect updated/new records
    const oldLogsMap = new Map(logs.value.map(l => [l.id, l]))
    const newUpdatedIds = new Set<string>()

    for (const newLog of logsRes.requests || []) {
      const oldLog = oldLogsMap.get(newLog.id)
      if (!oldLog || oldLog.status !== newLog.status) {
        newUpdatedIds.add(newLog.id)
      }
    }

    logs.value = logsRes.requests || []
    total.value = logsRes.total
    hasMore.value = logsRes.hasMore
    stats.value = statsRes

    if (newUpdatedIds.size > 0) {
      updatedIds.value = newUpdatedIds
      setTimeout(() => {
        updatedIds.value = new Set()
      }, 1000)
    }
  } catch (error) {
    console.error('Failed to refresh logs:', error)
  }
}
</script>

<style scoped>
.request-log-container {
  padding: 0;
}

.summary-card {
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface));
  border-radius: 0 !important;
  height: 100%;
}

.v-theme--dark .summary-card {
  border-color: rgba(255, 255, 255, 0.7);
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7);
}

.summary-table {
  font-size: 0.75rem;
}

.summary-table th {
  font-size: 0.85rem !important;
  font-weight: 700 !important;
  padding: 6px 8px !important;
}

.summary-table td {
  padding: 4px 8px !important;
}

.date-filter-card {
  display: flex;
  flex-direction: column;
}

.date-filter {
  flex: 1;
}

.date-display {
  font-family: 'Courier New', monospace;
  min-width: 120px;
  text-align: center;
}

.action-bar {
  display: flex;
  align-items: center;
  padding: 12px 16px;
  background: rgb(var(--v-theme-surface));
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface));
}

.v-theme--dark .action-bar {
  border-color: rgba(255, 255, 255, 0.7);
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7);
}

.log-table-card {
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface)) !important;
  border-radius: 0 !important;
}

.v-theme--dark .log-table-card {
  border-color: rgba(255, 255, 255, 0.7) !important;
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7) !important;
}

.log-table {
  font-family: 'Courier New', monospace;
}

.log-table :deep(th) {
  font-weight: 700 !important;
  font-size: 0.9rem !important;
}

.log-table :deep(td) {
  font-size: 0.9rem !important;
  font-family: inherit !important;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 0; /* This forces the cell to respect column width */
}

.log-table :deep(td .text-caption) {
  font-size: 0.9rem !important;
  font-family: inherit !important;
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-table :deep(td .v-chip) {
  font-size: 0.9rem !important;
  font-family: inherit !important;
  text-transform: none !important;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Row flash animation for updated records */
@keyframes row-flash {
  0% { background-color: rgba(76, 175, 80, 0.4); }
  50% { background-color: rgba(76, 175, 80, 0.2); }
  100% { background-color: transparent; }
}

.log-table :deep(tr.row-flash) {
  animation: row-flash 1s ease-out;
}

/* Neo-brutalist button style */
.neo-btn {
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 2px 2px 0 0 rgb(var(--v-theme-on-surface)) !important;
  transition: all 0.1s ease !important;
}

.v-theme--dark .neo-btn {
  border-color: rgba(255, 255, 255, 0.6) !important;
  box-shadow: 2px 2px 0 0 rgba(255, 255, 255, 0.6) !important;
}

.neo-btn:hover {
  background: rgba(var(--v-theme-primary), 0.1);
  transform: translate(-1px, -1px);
  box-shadow: 3px 3px 0 0 rgb(var(--v-theme-on-surface)) !important;
}

.neo-btn:active {
  transform: translate(2px, 2px) !important;
  box-shadow: none !important;
}

.neo-btn:disabled {
  opacity: 0.5;
}

.neo-btn-active {
  background-color: rgba(76, 175, 80, 0.2) !important;
  color: #4caf50 !important;
}

.v-theme--dark .neo-btn-active {
  background-color: rgba(76, 175, 80, 0.3) !important;
}

/* Resize handle styles */
.resizable-header,
.resizable-header-last {
  position: relative;
  display: inline-block;
  width: 100%;
}

.resize-handle {
  position: absolute;
  top: -8px;
  right: -4px;
  bottom: -8px;
  width: 8px;
  cursor: col-resize;
  user-select: none;
  z-index: 100;
  background: transparent;
  transition: all 0.2s ease;
}

.resize-handle::before {
  content: '';
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 2px;
  height: 0;
  background: rgb(var(--v-theme-primary));
  border-radius: 2px;
  transition: height 0.2s ease;
}

.resize-handle:hover {
  background: rgba(var(--v-theme-primary), 0.2);
  width: 12px;
  right: -6px;
}

.resize-handle:hover::before {
  height: 24px;
}

.resizable-table :deep(thead th) {
  position: relative !important;
  overflow: visible !important;
}

.resizable-table :deep(thead) {
  position: relative;
  z-index: 10;
}

/* Token column styles */
.tokens-header {
  font-size: 0.75rem;
  text-align: left;
}

.tokens-header-row {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 2px;
}

.token-label {
  font-size: 0.7rem;
  font-weight: 600;
  min-width: 90px;
  display: inline-block;
  text-align: right;
}

.token-separator {
  opacity: 0.4;
  font-size: 0.7rem;
}

.tokens-cell {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 4px;
  font-size: 0.85rem;
  font-family: inherit;
  white-space: nowrap;
}

.token-value {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  font-weight: 500;
  min-width: 90px;
}

.token-value .v-icon {
  margin: 0 !important;
}

.token-separator {
  opacity: 0.4;
  font-size: 0.7rem;
  flex-shrink: 0;
}

/* Token color coding */
.input-color {
  color: #2196F3 !important; /* Blue for input */
}

.output-color {
  color: #4CAF50 !important; /* Green for output */
}

.cache-create-color {
  color: #00BCD4 !important; /* Cyan for cache creation */
}

.cache-hit-color {
  color: #FF9800 !important; /* Orange for cache hit */
}

.v-theme--dark .input-color {
  color: #64B5F6 !important;
}

.v-theme--dark .output-color {
  color: #81C784 !important;
}

.v-theme--dark .cache-create-color {
  color: #4DD0E1 !important;
}

.v-theme--dark .cache-hit-color {
  color: #FFB74D !important;
}


</style>
