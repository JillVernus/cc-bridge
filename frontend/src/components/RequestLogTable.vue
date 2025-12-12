<template>
  <div class="request-log-container">
    <!-- 统计面板和日期筛选 -->
    <v-row class="mb-4">
      <!-- 模型统计 -->
      <v-col cols="5">
        <v-card class="summary-card" density="compact">
          <v-card-title class="text-subtitle-2 pa-2">{{ t('requestLog.byModel') }}</v-card-title>
          <div class="summary-table-wrapper">
            <v-table density="compact" class="summary-table">
              <thead>
                <tr>
                  <th class="col-model">{{ t('requestLog.model') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.requests') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.input') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.output') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.cacheCreation') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.cacheHit') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.cost') }}</th>
                </tr>
              </thead>
              <tbody class="summary-table-body">
                <tr v-for="[model, data] in sortedByModel" :key="model" :class="{ 'summary-row-flash': updatedModels.has(String(model)) }">
                  <td class="text-caption col-model font-weight-bold">{{ truncateModel(String(model)) }}</td>
                  <td class="text-end text-caption col-small">{{ data.count }}</td>
                  <td class="text-end text-caption col-small">{{ formatNumber(data.inputTokens) }}</td>
                  <td class="text-end text-caption col-small">{{ formatNumber(data.outputTokens) }}</td>
                  <td class="text-end text-caption text-success col-small">{{ formatNumber(data.cacheCreationInputTokens) }}</td>
                  <td class="text-end text-caption text-warning col-small">{{ formatNumber(data.cacheReadInputTokens) }}</td>
                  <td class="text-end text-caption cost-cell col-small">{{ formatPriceSummary(data.cost) }}</td>
                </tr>
                <tr v-if="sortedByModel.length === 0">
                  <td colspan="7" class="text-center text-caption text-grey">{{ t('common.noData') }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>
          <!-- Fixed footer -->
          <div class="summary-table-footer-fixed">
            <table class="total-table">
              <tr class="total-row">
                <td class="text-caption font-weight-bold col-model">Total</td>
                <td class="text-end text-caption font-weight-bold col-small">{{ modelTotals.count }}</td>
                <td class="text-end text-caption font-weight-bold col-small">{{ formatNumber(modelTotals.inputTokens) }}</td>
                <td class="text-end text-caption font-weight-bold col-small">{{ formatNumber(modelTotals.outputTokens) }}</td>
                <td class="text-end text-caption font-weight-bold text-success col-small">{{ formatNumber(modelTotals.cacheCreationInputTokens) }}</td>
                <td class="text-end text-caption font-weight-bold text-warning col-small">{{ formatNumber(modelTotals.cacheReadInputTokens) }}</td>
                <td class="text-end text-caption font-weight-bold cost-cell col-small">{{ formatPriceSummary(modelTotals.cost) }}</td>
              </tr>
            </table>
          </div>
        </v-card>
      </v-col>

      <!-- 渠道统计 -->
      <v-col cols="5">
        <v-card class="summary-card" density="compact">
          <v-card-title class="text-subtitle-2 pa-2">{{ t('requestLog.byChannel') }}</v-card-title>
          <div class="summary-table-wrapper">
            <v-table density="compact" class="summary-table">
              <thead>
                <tr>
                  <th class="col-model">{{ t('requestLog.channel') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.requests') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.input') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.output') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.cacheCreation') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.cacheHit') }}</th>
                  <th class="text-end col-small">{{ t('requestLog.cost') }}</th>
                </tr>
              </thead>
              <tbody class="summary-table-body">
                <tr v-for="[provider, data] in sortedByProvider" :key="provider" :class="{ 'summary-row-flash': updatedProviders.has(String(provider)) }">
                  <td class="text-caption col-model font-weight-bold">{{ provider }}</td>
                  <td class="text-end text-caption col-small">{{ data.count }}</td>
                  <td class="text-end text-caption col-small">{{ formatNumber(data.inputTokens) }}</td>
                  <td class="text-end text-caption col-small">{{ formatNumber(data.outputTokens) }}</td>
                  <td class="text-end text-caption text-success col-small">{{ formatNumber(data.cacheCreationInputTokens) }}</td>
                  <td class="text-end text-caption text-warning col-small">{{ formatNumber(data.cacheReadInputTokens) }}</td>
                  <td class="text-end text-caption cost-cell col-small">{{ formatPriceSummary(data.cost) }}</td>
                </tr>
                <tr v-if="sortedByProvider.length === 0">
                  <td colspan="7" class="text-center text-caption text-grey">{{ t('common.noData') }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>
          <!-- Fixed footer -->
          <div class="summary-table-footer-fixed">
            <table class="total-table">
              <tr class="total-row">
                <td class="text-caption font-weight-bold col-model">Total</td>
                <td class="text-end text-caption font-weight-bold col-small">{{ providerTotals.count }}</td>
                <td class="text-end text-caption font-weight-bold col-small">{{ formatNumber(providerTotals.inputTokens) }}</td>
                <td class="text-end text-caption font-weight-bold col-small">{{ formatNumber(providerTotals.outputTokens) }}</td>
                <td class="text-end text-caption font-weight-bold text-success col-small">{{ formatNumber(providerTotals.cacheCreationInputTokens) }}</td>
                <td class="text-end text-caption font-weight-bold text-warning col-small">{{ formatNumber(providerTotals.cacheReadInputTokens) }}</td>
                <td class="text-end text-caption font-weight-bold cost-cell col-small">{{ formatPriceSummary(providerTotals.cost) }}</td>
              </tr>
            </table>
          </div>
        </v-card>
      </v-col>

      <!-- 日期筛选 -->
      <v-col cols="2">
        <v-card class="summary-card date-filter-card" density="compact">
          <v-card-title class="text-subtitle-2 pa-2">{{ t('requestLog.dateFilter') }}</v-card-title>
          <div class="date-filter d-flex flex-column align-center justify-center pa-2">
            <div class="date-year">{{ filterYear }}</div>
            <div class="date-nav d-flex align-center">
              <v-btn icon variant="text" size="small" class="neo-btn" @click="prevDay">
                <v-icon size="20">mdi-chevron-left</v-icon>
              </v-btn>
              <div class="date-display-vertical mx-2">
                <div class="date-month">{{ filterMonth }}</div>
                <div class="date-day">{{ filterDay }}</div>
              </div>
              <v-btn icon variant="text" size="small" class="neo-btn" @click="nextDay" :disabled="isToday">
                <v-icon size="20">mdi-chevron-right</v-icon>
              </v-btn>
            </div>
          </div>
        </v-card>
      </v-col>
    </v-row>

    <!-- 设置对话框 -->
    <v-dialog v-model="showSettings" max-width="500">
      <v-card class="settings-card">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-cog</v-icon>
          {{ t('requestLog.settings') }}
        </v-card-title>
        <v-card-text>
          <div class="settings-section mb-4">
            <div class="text-subtitle-2 mb-2">{{ t('requestLog.columnWidthSettings') }}</div>
            <v-btn
              color="primary"
              variant="tonal"
              @click="resetColumnWidths"
            >
              <v-icon class="mr-1">mdi-table-refresh</v-icon>
              {{ t('requestLog.resetColumnWidth') }}
            </v-btn>
            <div class="text-caption text-grey mt-2">{{ t('requestLog.resetColumnWidthDesc') }}</div>
          </div>

          <v-divider class="my-4" />

          <div class="settings-section">
            <div class="text-subtitle-2 mb-2">{{ t('requestLog.databaseOps') }}</div>
            <v-btn
              color="error"
              variant="tonal"
              @click="confirmClearLogs"
              :loading="clearing"
            >
              <v-icon class="mr-1">mdi-delete</v-icon>
              {{ t('requestLog.clearLogs') }}
            </v-btn>
            <div class="text-caption text-grey mt-2">{{ t('requestLog.clearLogsDesc') }}</div>
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showSettings = false">{{ t('common.close') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 确认对话框 -->
    <v-dialog v-model="showConfirmClear" max-width="400">
      <v-card>
        <v-card-title class="text-error">{{ t('requestLog.confirmClear') }}</v-card-title>
        <v-card-text>{{ t('requestLog.confirmClearDesc') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showConfirmClear = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" @click="clearLogs" :loading="clearing">{{ t('requestLog.confirmDelete') }}</v-btn>
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
        <v-icon size="18" class="mr-1" :class="{ 'spin': autoRefreshEnabled }">{{ autoRefreshEnabled ? 'mdi-sync' : 'mdi-sync-off' }}</v-icon>
        {{ autoRefreshEnabled ? t('requestLog.autoRefreshing') : t('requestLog.autoRefreshOff') }}
      </v-btn>
      <v-spacer />
      <v-chip class="mr-2" size="small" variant="tonal">
        {{ t('requestLog.totalRecords', { count: total }) }}
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
        <template v-slot:[`header.price`]="{ column }">
          <div class="resizable-header">
            {{ column.title }}
            <div class="resize-handle" @mousedown="startResize($event, 'price')"></div>
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
          <v-chip v-else size="x-small" variant="text" class="provider-chip">
            <v-icon v-if="item.type === 'claude'" start size="14" icon="custom:claude" class="provider-icon mr-1" />
            <v-icon v-else-if="item.type === 'openai' || item.type === 'codex' || item.type === 'responses'" start size="14" icon="custom:codex" class="provider-icon mr-1" />
            <v-icon v-else-if="item.type === 'gemini'" start size="14" class="mr-1">mdi-google</v-icon>
            {{ item.providerName || item.type }}
          </v-chip>
        </template>

        <template v-slot:item.model="{ item }">
          <!-- Model with reasoning effort tooltip -->
          <v-tooltip v-if="item.reasoningEffort" location="top" max-width="300">
            <template v-slot:activator="{ props }">
              <span v-bind="props" class="text-caption font-weight-medium model-with-effort">
                {{ item.model }}
                <v-icon size="12" class="ml-1">mdi-lightbulb</v-icon>
              </span>
            </template>
            <div class="reasoning-effort-tooltip">
              <span class="effort-label">Reasoning Effort:</span>
              <span class="effort-value">{{ item.reasoningEffort }}</span>
            </div>
          </v-tooltip>
          <!-- Model with response model mapping tooltip -->
          <v-tooltip v-else-if="item.responseModel && item.responseModel !== item.model" location="top" max-width="400">
            <template v-slot:activator="{ props }">
              <span v-bind="props" class="text-caption font-weight-medium model-with-mapping">
                {{ item.model }}
                <v-icon size="12" class="ml-1">mdi-swap-horizontal</v-icon>
              </span>
            </template>
            <div class="model-mapping-tooltip">
              <span class="request-model">{{ item.model }}</span>
              <v-icon size="14" class="mx-1">mdi-arrow-right</v-icon>
              <span class="response-model">{{ item.responseModel }}</span>
            </div>
          </v-tooltip>
          <span v-else class="text-caption font-weight-medium">{{ item.model }}</span>
        </template>

        <template v-slot:item.tokens="{ item }">
          <div v-if="item.status === 'pending'" class="tokens-cell">
            <span class="token-value"><v-progress-circular indeterminate size="14" width="2" color="grey" /></span>
            <span class="token-value"><v-progress-circular indeterminate size="14" width="2" color="grey" /></span>
            <span class="token-value"><v-progress-circular indeterminate size="14" width="2" color="grey" /></span>
            <span class="token-value"><v-progress-circular indeterminate size="14" width="2" color="grey" /></span>
          </div>
          <div v-else class="tokens-cell">
            <span class="token-value input-color">{{ formatNumber(item.inputTokens) }} (<v-icon size="12">mdi-arrow-up</v-icon>)</span>
            <span class="token-value output-color">{{ formatNumber(item.outputTokens) }} (<v-icon size="12">mdi-arrow-down</v-icon>)</span>
            <span class="token-value cache-create-color">{{ formatNumber(item.cacheCreationInputTokens) }} (<v-icon size="12">mdi-arrow-up</v-icon>)</span>
            <span class="token-value cache-hit-color">{{ formatNumber(item.cacheReadInputTokens) }} (<v-icon size="12">mdi-flash</v-icon>)</span>
          </div>
        </template>

        <template v-slot:item.price="{ item }">
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="16"
            width="2"
            color="grey"
          />
          <v-tooltip v-else-if="hasCostBreakdown(item)" location="top" max-width="300">
            <template v-slot:activator="{ props }">
              <span v-bind="props" class="text-caption price-value price-with-tooltip" :class="{ 'price-zero': !item.price }">
                {{ formatPriceDetailed(item.price) }}
              </span>
            </template>
            <div class="cost-breakdown-tooltip">
              <div class="cost-breakdown-title">Cost Breakdown</div>
              <div class="cost-breakdown-row" v-if="item.inputCost">
                <span class="cost-label">Input:</span>
                <span class="cost-value">{{ formatPriceDetailed(item.inputCost) }}</span>
              </div>
              <div class="cost-breakdown-row" v-if="item.outputCost">
                <span class="cost-label">Output:</span>
                <span class="cost-value">{{ formatPriceDetailed(item.outputCost) }}</span>
              </div>
              <div class="cost-breakdown-row cache-create-row" v-if="item.cacheCreationCost">
                <span class="cost-label">Cache Create:</span>
                <span class="cost-value">{{ formatPriceDetailed(item.cacheCreationCost) }}</span>
              </div>
              <div class="cost-breakdown-row cache-hit-row" v-if="item.cacheReadCost">
                <span class="cost-label">Cache Hit:</span>
                <span class="cost-value">{{ formatPriceDetailed(item.cacheReadCost) }}</span>
              </div>
              <div class="cost-breakdown-total">
                <span class="cost-label">Total:</span>
                <span class="cost-value">{{ formatPriceDetailed(item.price) }}</span>
              </div>
            </div>
          </v-tooltip>
          <span v-else class="text-caption price-value" :class="{ 'price-zero': !item.price }">
            {{ formatPriceDetailed(item.price) }}
          </span>
        </template>

        <template v-slot:item.httpStatus="{ item }">
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="16"
            width="2"
            color="grey"
          />
          <v-tooltip v-else-if="item.error || item.upstreamError" location="top" max-width="400">
            <template v-slot:activator="{ props }">
              <v-chip v-bind="props" size="x-small" :color="getStatusColor(item.httpStatus)" variant="tonal">
                {{ item.httpStatus }}
              </v-chip>
            </template>
            <div class="error-tooltip">
              <div v-if="item.error" class="error-line">
                <span class="error-label">错误:</span>
                <span>{{ item.error }}</span>
              </div>
              <div v-if="item.upstreamError" class="upstream-error-line">
                <span class="error-label">上游响应:</span>
                <span class="upstream-error-text">{{ item.upstreamError }}</span>
              </div>
            </div>
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
              {{ t('requestLog.prevPage') }}
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
              {{ t('requestLog.nextPage') }}
            </v-btn>
          </div>
        </template>
      </v-data-table>
    </v-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type RequestLog, type RequestLogStats, type GroupStats } from '../services/api'

// i18n
const { t } = useI18n()

const logs = ref<RequestLog[]>([])
const stats = ref<RequestLogStats | null>(null)
const loading = ref(false)
const total = ref(0)
const hasMore = ref(false)
const offset = ref(0)
const pageSize = 50
const updatedIds = ref<Set<string>>(new Set())
const updatedModels = ref<Set<string>>(new Set())
const updatedProviders = ref<Set<string>>(new Set())

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

// Separate date parts for vertical display
const filterYear = computed(() => filterDate.value.split('-')[0])
const filterMonth = computed(() => filterDate.value.split('-')[1])
const filterDay = computed(() => filterDate.value.split('-')[2])

// Sorted stats by cost (descending), then by total tokens (descending)
const getTotalTokens = (data: GroupStats) => {
  return data.inputTokens + data.outputTokens + data.cacheCreationInputTokens + data.cacheReadInputTokens
}

const sortedByModel = computed(() => {
  if (!stats.value?.byModel) return []
  return Object.entries(stats.value.byModel)
    .sort(([, a], [, b]) => {
      const costDiff = b.cost - a.cost
      if (costDiff !== 0) return costDiff
      return getTotalTokens(b) - getTotalTokens(a)
    })
})

const sortedByProvider = computed(() => {
  if (!stats.value?.byProvider) return []
  return Object.entries(stats.value.byProvider)
    .sort(([, a], [, b]) => {
      const costDiff = b.cost - a.cost
      if (costDiff !== 0) return costDiff
      return getTotalTokens(b) - getTotalTokens(a)
    })
})

// Totals for summary tables
const modelTotals = computed(() => {
  const totals = { count: 0, inputTokens: 0, outputTokens: 0, cacheCreationInputTokens: 0, cacheReadInputTokens: 0, cost: 0 }
  for (const [, data] of sortedByModel.value) {
    totals.count += data.count
    totals.inputTokens += data.inputTokens
    totals.outputTokens += data.outputTokens
    totals.cacheCreationInputTokens += data.cacheCreationInputTokens
    totals.cacheReadInputTokens += data.cacheReadInputTokens
    totals.cost += data.cost
  }
  return totals
})

const providerTotals = computed(() => {
  const totals = { count: 0, inputTokens: 0, outputTokens: 0, cacheCreationInputTokens: 0, cacheReadInputTokens: 0, cost: 0 }
  for (const [, data] of sortedByProvider.value) {
    totals.count += data.count
    totals.inputTokens += data.inputTokens
    totals.outputTokens += data.outputTokens
    totals.cacheCreationInputTokens += data.cacheCreationInputTokens
    totals.cacheReadInputTokens += data.cacheReadInputTokens
    totals.cost += data.cost
  }
  return totals
})

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
  price: 80,
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
  { title: t('requestLog.status'), key: 'status', sortable: false, width: `${columnWidths.value.status}px` },
  { title: t('requestLog.time'), key: 'initialTime', sortable: false, width: `${columnWidths.value.initialTime}px` },
  { title: t('requestLog.duration'), key: 'durationMs', sortable: false, width: `${columnWidths.value.durationMs}px` },
  { title: t('requestLog.channel'), key: 'providerName', sortable: false, width: `${columnWidths.value.providerName}px` },
  { title: t('requestLog.model'), key: 'model', sortable: false, width: `${columnWidths.value.model}px` },
  { title: t('requestLog.tokens'), key: 'tokens', sortable: false, width: `${columnWidths.value.tokens}px` },
  { title: t('requestLog.price'), key: 'price', sortable: false, width: `${columnWidths.value.price}px` },
  { title: t('requestLog.http'), key: 'httpStatus', sortable: false, width: `${columnWidths.value.httpStatus}px` },
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

const formatPrice = (price: number) => {
  if (!price || price === 0) return '-'
  if (price < 0.0001) return '<$0.0001'
  if (price < 0.01) return '$' + price.toFixed(4)
  if (price < 1) return '$' + price.toFixed(3)
  return '$' + price.toFixed(2)
}

// Format price with 6 decimal places for detailed view
const formatPriceDetailed = (price: number) => {
  if (!price || price === 0) return '$0.000000'
  return '$' + price.toFixed(6)
}

// Format price with 2 decimal places for summary tables
const formatPriceSummary = (price: number) => {
  if (!price || price === 0) return '$0.00'
  return '$' + price.toFixed(2)
}

// Check if a request log has cost breakdown details
const hasCostBreakdown = (item: RequestLog) => {
  return (item.inputCost && item.inputCost > 0) ||
         (item.outputCost && item.outputCost > 0) ||
         (item.cacheCreationCost && item.cacheCreationCost > 0) ||
         (item.cacheReadCost && item.cacheReadCost > 0)
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
    error: 'error',
    timeout: 'grey'
  }
  return colors[status] || 'grey'
}

const getRequestStatusLabel = (status: string) => {
  const labels: Record<string, string> = {
    pending: t('requestLog.pending'),
    completed: t('requestLog.completed'),
    error: t('requestLog.error'),
    timeout: t('requestLog.timeout')
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

    // Detect updated models and providers in stats
    const newUpdatedModels = new Set<string>()
    const newUpdatedProviders = new Set<string>()

    if (stats.value && statsRes) {
      // Check models
      const oldByModel = stats.value.byModel || {}
      const newByModel = statsRes.byModel || {}
      for (const model of Object.keys(newByModel)) {
        const oldData = oldByModel[model]
        const newData = newByModel[model]
        if (!oldData || oldData.count !== newData.count || oldData.cost !== newData.cost) {
          newUpdatedModels.add(model)
        }
      }

      // Check providers
      const oldByProvider = stats.value.byProvider || {}
      const newByProvider = statsRes.byProvider || {}
      for (const provider of Object.keys(newByProvider)) {
        const oldData = oldByProvider[provider]
        const newData = newByProvider[provider]
        if (!oldData || oldData.count !== newData.count || oldData.cost !== newData.cost) {
          newUpdatedProviders.add(provider)
        }
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

    if (newUpdatedModels.size > 0) {
      updatedModels.value = newUpdatedModels
      setTimeout(() => {
        updatedModels.value = new Set()
      }, 1000)
    }

    if (newUpdatedProviders.size > 0) {
      updatedProviders.value = newUpdatedProviders
      setTimeout(() => {
        updatedProviders.value = new Set()
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

/* Provider chip with custom icon */
.provider-chip :deep(.custom-icon) {
  margin-right: 4px;
}

.provider-chip :deep(.custom-icon svg) {
  width: 14px;
  height: 14px;
}

/* Spinning animation for sync icon */
.spin {
  animation: spin 1.5s linear infinite;
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

.summary-card {
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface));
  border-radius: 0 !important;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.v-theme--dark .summary-card {
  border-color: rgba(255, 255, 255, 0.7);
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7);
}

.summary-table-wrapper {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.summary-table {
  font-size: 0.75rem;
  font-family: 'Courier New', monospace;
  flex: 1;
  display: flex;
  flex-direction: column;
}

.summary-table th {
  font-size: 0.75rem !important;
  font-weight: 700 !important;
  font-family: 'Courier New', monospace !important;
  padding: 6px 8px !important;
}

.summary-table td {
  padding: 4px 8px !important;
  font-family: 'Courier New', monospace !important;
}

/* Scrollable tbody for summary tables - 4 rows max */
.summary-table-body {
  display: block;
  max-height: 120px; /* approximately 4 rows */
  overflow-y: auto;
}

.summary-table-body tr {
  display: table;
  width: 100%;
  table-layout: fixed;
}

.summary-table thead {
  display: table;
  width: 100%;
  table-layout: fixed;
}

/* Fixed footer outside table */
.summary-table-footer-fixed {
  border-top: 2px solid rgb(var(--v-theme-on-surface));
  background: rgba(var(--v-theme-surface-variant), 0.3);
}

.v-theme--dark .summary-table-footer-fixed {
  border-top-color: rgba(255, 255, 255, 0.5);
}

.summary-table-footer-fixed .total-table {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
}

.summary-table-footer-fixed .total-row td {
  padding: 6px 8px;
  font-family: 'Courier New', monospace;
}

/* Old tfoot styles - kept for backward compatibility */
.summary-table-footer {
  border-top: 2px solid rgb(var(--v-theme-on-surface));
  background: rgba(var(--v-theme-surface-variant), 0.3);
}

.v-theme--dark .summary-table-footer {
  border-top-color: rgba(255, 255, 255, 0.5);
}

.total-row td {
  padding: 6px 8px !important;
}

/* Column widths for summary tables */
.col-model {
  width: 35%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.col-small {
  width: 10%;
  white-space: nowrap;
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

.date-display-vertical {
  display: flex;
  flex-direction: column;
  align-items: center;
  font-family: 'Courier New', monospace;
  line-height: 1.2;
}

.date-year {
  font-size: 1.25rem;
  font-weight: 700;
  font-family: 'Courier New', monospace;
  color: rgba(var(--v-theme-on-surface), 0.7);
  margin-bottom: 2px;
}

.date-month {
  font-size: 1.25rem;
  font-weight: 700;
}

.date-day {
  font-size: 1.25rem;
  font-weight: 700;
}

.date-nav {
  display: flex;
  align-items: center;
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

/* Summary table row flash animation */
.summary-table tr.summary-row-flash {
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
  color: #4CAF50 !important; /* Green for input */
}

.output-color {
  color: #2196F3 !important; /* Blue for output */
}

.cache-create-color {
  color: rgb(var(--v-theme-success)) !important; /* Same as text-success */
}

.cache-hit-color {
  color: rgb(var(--v-theme-warning)) !important; /* Same as text-warning */
}

.v-theme--dark .input-color {
  color: #81C784 !important;
}

.v-theme--dark .output-color {
  color: #64B5F6 !important;
}

.v-theme--dark .cache-create-color {
  color: rgb(var(--v-theme-success)) !important;
}

.v-theme--dark .cache-hit-color {
  color: rgb(var(--v-theme-warning)) !important;
}

/* Error tooltip styles */
.error-tooltip {
  font-size: 0.85rem;
  line-height: 1.4;
  color: #fff;
}

.error-line {
  margin-bottom: 8px;
  color: #fff;
}

.upstream-error-line {
  padding-top: 8px;
  border-top: 1px solid rgba(255, 255, 255, 0.2);
  color: #fff;
}

.error-label {
  font-weight: 600;
  margin-right: 4px;
  color: #ff9800;
}

.upstream-error-text {
  font-family: 'Courier New', monospace;
  font-size: 0.8rem;
  word-break: break-all;
  white-space: pre-wrap;
  display: block;
  margin-top: 4px;
  max-height: 200px;
  overflow-y: auto;
  color: rgba(255, 255, 255, 0.9);
}

/* Price column styles */
.price-value {
  font-family: 'Courier New', monospace;
  font-weight: 600;
  color: #4CAF50;
}

.price-zero {
  color: #9e9e9e;
  font-weight: 400;
}

.price-with-tooltip {
  cursor: pointer;
  text-decoration: underline dotted;
  text-underline-offset: 3px;
}

.v-theme--dark .price-value {
  color: #81C784;
}

.v-theme--dark .price-zero {
  color: #757575;
}

/* Cost breakdown tooltip */
.cost-breakdown-tooltip {
  padding: 4px 0;
  font-family: 'Courier New', monospace;
  font-size: 12px;
  color: #fff;
}

.cost-breakdown-title {
  font-weight: 600;
  margin-bottom: 6px;
  padding-bottom: 4px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.2);
  color: #fff;
}

.cost-breakdown-row {
  display: flex;
  justify-content: space-between;
  margin: 2px 0;
  gap: 16px;
}

.cost-breakdown-row .cost-label {
  color: rgba(255, 255, 255, 0.7);
}

.cost-breakdown-row .cost-value {
  font-weight: 500;
  color: #fff;
}

.cost-breakdown-row.cache-create-row .cost-value {
  color: #e0f7fa; /* 青色调 */
}

.cost-breakdown-row.cache-hit-row .cost-value {
  color: #fff9c4; /* 黄色调 */
}

.cost-breakdown-total {
  display: flex;
  justify-content: space-between;
  margin-top: 6px;
  padding-top: 4px;
  border-top: 1px solid rgba(255, 255, 255, 0.2);
  font-weight: 600;
  gap: 16px;
  color: #fff;
}

.cost-breakdown-total .cost-label {
  color: rgba(255, 255, 255, 0.9);
}

.cost-breakdown-total .cost-value {
  color: #81C784;
}

/* Cost cell in summary tables */
.cost-cell {
  font-family: 'Courier New', monospace;
  font-weight: 600;
  color: #4CAF50 !important;
}

.v-theme--dark .cost-cell {
  color: #81C784 !important;
}

/* Model mapping tooltip styles */
.model-with-mapping,
.model-with-effort {
  cursor: pointer;
  text-decoration: underline dotted;
  text-underline-offset: 3px;
}

.model-mapping-tooltip {
  display: flex;
  align-items: center;
  font-family: 'Courier New', monospace;
  font-size: 12px;
  white-space: nowrap;
  color: #fff;
}

.model-mapping-tooltip .request-model {
  color: #64B5F6;
  font-weight: 500;
}

.model-mapping-tooltip .v-icon {
  color: rgba(255, 255, 255, 0.7);
}

.model-mapping-tooltip .response-model {
  color: #81C784;
  font-weight: 500;
}

/* Reasoning effort tooltip styles */
.reasoning-effort-tooltip {
  display: flex;
  align-items: center;
  gap: 8px;
  font-family: 'Courier New', monospace;
  font-size: 12px;
  color: #fff;
}

.reasoning-effort-tooltip .effort-label {
  color: rgba(255, 255, 255, 0.7);
}

.reasoning-effort-tooltip .effort-value {
  color: #FFB74D;
  font-weight: 600;
  text-transform: uppercase;
}

.settings-card {
  max-height: 80vh;
  overflow-y: auto;
}


</style>
