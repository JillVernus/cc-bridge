<template>
  <div class="request-log-container">
    <!-- 统计面板和日期筛选 -->
    <div class="top-panels-container mb-4" ref="topPanelsContainer">
      <!-- 统一的统计表格 -->
      <div class="panel-wrapper" :style="{ width: panelWidths.summary + '%' }">
        <v-card class="summary-card" density="compact">
          <!-- Group by selector header -->
          <div class="summary-header d-flex align-center px-2 py-1">
            <v-select
              v-model="summaryGroupBy"
              :items="summaryGroupByOptions"
              item-title="label"
              item-value="value"
              density="compact"
              variant="outlined"
              hide-details
              class="group-by-select"
            />
          </div>
          <!-- Header table -->
          <div class="summary-table-header-wrapper">
            <table class="summary-table-custom resizable-summary-table" :style="{ width: summaryTableWidth + 'px' }">
              <thead>
                <tr>
                  <th class="resizable-summary-header" :style="{ width: summaryColumnWidths.name + 'px' }">
                    {{ summaryNameHeaderTitle }}
                    <div class="resize-handle" @mousedown="startSummaryResize($event, 'name')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: summaryColumnWidths.requests + 'px' }">
                    {{ t('requestLog.requests') }}
                    <div class="resize-handle" @mousedown="startSummaryResize($event, 'requests')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: summaryColumnWidths.input + 'px' }">
                    {{ t('requestLog.input') }}
                    <div class="resize-handle" @mousedown="startSummaryResize($event, 'input')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: summaryColumnWidths.output + 'px' }">
                    {{ t('requestLog.output') }}
                    <div class="resize-handle" @mousedown="startSummaryResize($event, 'output')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: summaryColumnWidths.cacheCreation + 'px' }">
                    {{ t('requestLog.cacheCreation') }}
                    <div class="resize-handle" @mousedown="startSummaryResize($event, 'cacheCreation')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: summaryColumnWidths.cacheHit + 'px' }">
                    {{ t('requestLog.cacheHit') }}
                    <div class="resize-handle" @mousedown="startSummaryResize($event, 'cacheHit')"></div>
                  </th>
                  <th class="text-end resizable-summary-header-last" :style="{ width: summaryColumnWidths.cost + 'px' }">
                    {{ t('requestLog.cost') }}
                  </th>
                </tr>
              </thead>
            </table>
          </div>
          <!-- Body table (scrollable) -->
          <div class="summary-table-body-wrapper">
            <table class="summary-table-custom" :style="{ width: summaryTableWidth + 'px' }">
              <tbody>
                <tr
                  v-for="[key, data] in currentSortedData"
                  :key="key"
                  :class="{ 'summary-row-flash': currentUpdatedSet.has(String(key)) }"
                >
                  <td class="text-caption font-weight-bold summary-name-cell" :style="{ width: summaryColumnWidths.name + 'px', maxWidth: summaryColumnWidths.name + 'px' }">
                    <v-tooltip
                      v-if="(summaryGroupBy === 'user' || summaryGroupBy === 'session') && String(key) && String(key) !== '<unknown>'"
                      location="top"
                      max-width="600"
                    >
                      <template v-slot:activator="{ props }">
                        <span v-bind="props">{{ formatSummaryKey(String(key)) }}</span>
                      </template>
                      <span class="id-tooltip">{{ summaryGroupBy === 'user' ? normalizeUserId(String(key)) : String(key) }}</span>
                    </v-tooltip>
                    <span v-else>{{ formatSummaryKey(String(key)) }}</span>
                  </td>
                  <td class="text-end text-caption" :style="{ width: summaryColumnWidths.requests + 'px' }">{{ data.count }}</td>
                  <td class="text-end text-caption" :style="{ width: summaryColumnWidths.input + 'px' }">{{ formatNumber(data.inputTokens) }}</td>
                  <td class="text-end text-caption" :style="{ width: summaryColumnWidths.output + 'px' }">{{ formatNumber(data.outputTokens) }}</td>
                  <td class="text-end text-caption text-success" :style="{ width: summaryColumnWidths.cacheCreation + 'px' }">{{ formatNumber(data.cacheCreationInputTokens) }}</td>
                  <td class="text-end text-caption text-warning" :style="{ width: summaryColumnWidths.cacheHit + 'px' }">{{ formatNumber(data.cacheReadInputTokens) }}</td>
                  <td class="text-end text-caption cost-cell" :style="{ width: summaryColumnWidths.cost + 'px' }">{{ formatPriceSummary(data.cost) }}</td>
                </tr>
                <tr v-if="currentSortedData.length === 0">
                  <td colspan="7" class="text-center text-caption text-grey">{{ t('common.noData') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <!-- Footer table -->
          <div class="summary-table-footer-fixed">
            <table class="summary-table-custom" :style="{ width: summaryTableWidth + 'px' }">
              <tbody>
                <tr class="total-row">
                  <td class="text-caption font-weight-bold" :style="{ width: summaryColumnWidths.name + 'px' }">Total</td>
                  <td class="text-end text-caption font-weight-bold" :style="{ width: summaryColumnWidths.requests + 'px' }">{{ currentTotals.count }}</td>
                  <td class="text-end text-caption font-weight-bold" :style="{ width: summaryColumnWidths.input + 'px' }">{{ formatNumber(currentTotals.inputTokens) }}</td>
                  <td class="text-end text-caption font-weight-bold" :style="{ width: summaryColumnWidths.output + 'px' }">{{ formatNumber(currentTotals.outputTokens) }}</td>
                  <td class="text-end text-caption font-weight-bold text-success" :style="{ width: summaryColumnWidths.cacheCreation + 'px' }">{{ formatNumber(currentTotals.cacheCreationInputTokens) }}</td>
                  <td class="text-end text-caption font-weight-bold text-warning" :style="{ width: summaryColumnWidths.cacheHit + 'px' }">{{ formatNumber(currentTotals.cacheReadInputTokens) }}</td>
                  <td class="text-end text-caption font-weight-bold cost-cell" :style="{ width: summaryColumnWidths.cost + 'px' }">{{ formatPriceSummary(currentTotals.cost) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </v-card>
      </div>

      <!-- Splitter 1 -->
      <div class="panel-splitter" @mousedown="startPanelResize($event, 'splitter1')">
        <div class="splitter-handle"></div>
      </div>

      <!-- 预留区域 -->
      <div class="panel-wrapper" :style="{ width: panelWidths.reserved + '%' }">
        <v-card class="summary-card" density="compact">
          <!-- Reserved for future use -->
        </v-card>
      </div>

      <!-- Splitter 2 -->
      <div class="panel-splitter" @mousedown="startPanelResize($event, 'splitter2')">
        <div class="splitter-handle"></div>
      </div>

      <!-- 日期筛选 -->
      <div class="panel-wrapper" :style="{ width: panelWidths.dateFilter + '%' }">
        <v-card class="summary-card date-filter-card" density="compact">
          <div class="date-filter d-flex flex-column align-center justify-center pa-2">
            <!-- Unit selector -->
            <v-btn-toggle
              v-model="dateUnit"
              mandatory
              density="compact"
              class="unit-toggle mb-2"
            >
              <v-btn value="day" size="x-small">{{ t('requestLog.unitDay') }}</v-btn>
              <v-btn value="week" size="x-small">{{ t('requestLog.unitWeek') }}</v-btn>
              <v-btn value="month" size="x-small">{{ t('requestLog.unitMonth') }}</v-btn>
            </v-btn-toggle>
            <!-- Date display: 3 lines -->
            <div class="date-year">{{ displayYear }}</div>
            <div class="date-nav d-flex align-center">
              <v-btn icon variant="text" size="small" class="neo-btn" @click="prevPeriod">
                <v-icon size="20">mdi-chevron-left</v-icon>
              </v-btn>
              <div class="date-month mx-2">{{ displayMonth }}</div>
              <v-btn icon variant="text" size="small" class="neo-btn" @click="nextPeriod" :disabled="isCurrentPeriod">
                <v-icon size="20">mdi-chevron-right</v-icon>
              </v-btn>
            </div>
            <div class="date-days">{{ displayDays }}</div>
          </div>
        </v-card>
      </div>
    </div>

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

          <div class="settings-section mb-4">
            <div class="text-subtitle-2 mb-2">{{ t('requestLog.retentionCleanup') }}</div>
            <div class="d-flex align-center ga-2">
              <v-select
                v-model="retentionDays"
                :items="retentionOptions"
                item-title="label"
                item-value="value"
                density="compact"
                variant="outlined"
                hide-details
                style="max-width: 180px"
              />
              <v-btn
                color="warning"
                variant="tonal"
                @click="confirmCleanupLogs"
                :loading="cleaning"
              >
                <v-icon class="mr-1">mdi-broom</v-icon>
                {{ t('requestLog.cleanup') }}
              </v-btn>
            </div>
            <div class="text-caption text-grey mt-2">{{ t('requestLog.retentionCleanupDesc') }}</div>
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

    <!-- 确认清理对话框 -->
    <v-dialog v-model="showConfirmCleanup" max-width="400">
      <v-card>
        <v-card-title class="text-warning">{{ t('requestLog.confirmCleanup') }}</v-card-title>
        <v-card-text>{{ t('requestLog.confirmCleanupDesc', { days: retentionDays }) }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showConfirmCleanup = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="warning" variant="flat" @click="cleanupLogs" :loading="cleaning">{{ t('requestLog.confirmCleanupBtn') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 清理结果对话框 -->
    <v-dialog v-model="showCleanupResult" max-width="400">
      <v-card>
        <v-card-title class="text-success d-flex align-center">
          <v-icon class="mr-2" color="success">mdi-check-circle</v-icon>
          {{ t('requestLog.cleanupComplete') }}
        </v-card-title>
        <v-card-text>
          <div class="text-body-1">
            {{ t('requestLog.cleanupResultDesc', { count: cleanupResultCount, days: cleanupResultDays }) }}
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn color="primary" variant="flat" @click="showCleanupResult = false">{{ t('common.close') }}</v-btn>
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
        <template v-slot:[`header.userId`]="{ column }">
          <div class="resizable-header">
            {{ column.title }}
            <div class="resize-handle" @mousedown="startResize($event, 'userId')"></div>
          </div>
        </template>
        <template v-slot:[`header.sessionId`]="{ column }">
          <div class="resizable-header">
            {{ column.title }}
            <div class="resize-handle" @mousedown="startResize($event, 'sessionId')"></div>
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

        <template v-slot:item.userId="{ item }">
          <v-tooltip v-if="item.userId" location="top" max-width="600">
            <template v-slot:activator="{ props }">
              <span v-bind="props" class="text-caption mono-text id-cell">
                {{ formatUserId(item.userId) }}
              </span>
            </template>
            <span class="id-tooltip">{{ normalizeUserId(item.userId) }}</span>
          </v-tooltip>
          <span v-else class="text-caption mono-text id-cell">—</span>
        </template>

        <template v-slot:item.sessionId="{ item }">
          <v-tooltip v-if="item.sessionId" location="top" max-width="600">
            <template v-slot:activator="{ props }">
              <span v-bind="props" class="text-caption mono-text id-cell">
                {{ formatId(item.sessionId) }}
              </span>
            </template>
            <span class="id-tooltip">{{ item.sessionId }}</span>
          </v-tooltip>
          <span v-else class="text-caption mono-text id-cell">—</span>
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
const updatedUsers = ref<Set<string>>(new Set())
const updatedSessions = ref<Set<string>>(new Set())

// Summary table group by state
type SummaryGroupBy = 'model' | 'provider' | 'user' | 'session'
const summaryGroupBy = ref<SummaryGroupBy>('model')
const summaryGroupByOptions = computed(() => [
  { label: t('requestLog.groupByModel'), value: 'model' },
  { label: t('requestLog.groupByProvider'), value: 'provider' },
  { label: t('requestLog.groupByUser'), value: 'user' },
  { label: t('requestLog.groupBySession'), value: 'session' },
])

const summaryNameHeaderTitle = computed(() => {
  switch (summaryGroupBy.value) {
    case 'model':
      return t('requestLog.model')
    case 'provider':
      return t('requestLog.channel')
    case 'user':
      return t('requestLog.userId')
    case 'session':
      return t('requestLog.sessionId')
    default:
      return t('requestLog.model')
  }
})

// Settings
const showSettings = ref(false)
const showConfirmClear = ref(false)
const clearing = ref(false)

// Cleanup
const showConfirmCleanup = ref(false)
const showCleanupResult = ref(false)
const cleaning = ref(false)
const retentionDays = ref(30)
const cleanupResultCount = ref(0)
const cleanupResultDays = ref(0)
const retentionOptions = computed(() => [
  { label: t('requestLog.retention30'), value: 30 },
  { label: t('requestLog.retention60'), value: 60 },
  { label: t('requestLog.retention90'), value: 90 },
  { label: t('requestLog.retention180'), value: 180 },
  { label: t('requestLog.retention365'), value: 365 },
])

// Date filter - use local date
const getLocalDateString = (date: Date) => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

// Date unit and current reference date
type DateUnit = 'day' | 'week' | 'month'
const dateUnit = ref<DateUnit>('day')
const currentDate = ref(new Date()) // Reference date for navigation

// Display computed values
const displayYear = computed(() => currentDate.value.getFullYear())

// Line 2: Month (with navigation arrows)
const displayMonth = computed(() => {
  const d = currentDate.value
  return String(d.getMonth() + 1).padStart(2, '0')
})

// Line 3: Day or day range
const displayDays = computed(() => {
  const d = currentDate.value
  switch (dateUnit.value) {
    case 'day': {
      return String(d.getDate()).padStart(2, '0')
    }
    case 'week': {
      // Get Monday and Sunday of the week
      const monday = getWeekStart(d)
      const sunday = new Date(monday)
      sunday.setDate(monday.getDate() + 6)
      const mDay = String(monday.getDate()).padStart(2, '0')
      const sDay = String(sunday.getDate()).padStart(2, '0')
      return `${mDay}-${sDay}`
    }
    case 'month': {
      // First to last day of month
      const lastDay = new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate()
      return `01-${lastDay}`
    }
  }
})

// Get Monday of a given week
const getWeekStart = (date: Date): Date => {
  const d = new Date(date)
  const dayOfWeek = d.getDay()
  const diff = dayOfWeek === 0 ? 6 : dayOfWeek - 1
  d.setDate(d.getDate() - diff)
  return d
}

// Check if current period includes today
const isCurrentPeriod = computed(() => {
  const today = new Date()
  const d = currentDate.value
  switch (dateUnit.value) {
    case 'day':
      return getLocalDateString(d) === getLocalDateString(today)
    case 'week': {
      const todayWeekStart = getWeekStart(today)
      const currentWeekStart = getWeekStart(d)
      return getLocalDateString(todayWeekStart) === getLocalDateString(currentWeekStart)
    }
    case 'month':
      return d.getFullYear() === today.getFullYear() && d.getMonth() === today.getMonth()
  }
})

// Navigate to previous period
const prevPeriod = () => {
  const d = new Date(currentDate.value)
  switch (dateUnit.value) {
    case 'day':
      d.setDate(d.getDate() - 1)
      break
    case 'week':
      d.setDate(d.getDate() - 7)
      break
    case 'month':
      d.setMonth(d.getMonth() - 1)
      break
  }
  currentDate.value = d
}

// Navigate to next period
const nextPeriod = () => {
  if (isCurrentPeriod.value) return
  const d = new Date(currentDate.value)
  switch (dateUnit.value) {
    case 'day':
      d.setDate(d.getDate() + 1)
      break
    case 'week':
      d.setDate(d.getDate() + 7)
      break
    case 'month':
      d.setMonth(d.getMonth() + 1)
      break
  }
  currentDate.value = d
}

// Get date range based on current unit and date
const getDateRange = () => {
  const d = currentDate.value
  let from: string
  let to: string

  switch (dateUnit.value) {
    case 'day': {
      const dateStr = getLocalDateString(d)
      from = `${dateStr}T00:00:00+08:00`
      to = `${dateStr}T23:59:59+08:00`
      break
    }
    case 'week': {
      const monday = getWeekStart(d)
      const sunday = new Date(monday)
      sunday.setDate(monday.getDate() + 6)
      from = `${getLocalDateString(monday)}T00:00:00+08:00`
      to = `${getLocalDateString(sunday)}T23:59:59+08:00`
      break
    }
    case 'month': {
      const firstDay = new Date(d.getFullYear(), d.getMonth(), 1)
      const lastDay = new Date(d.getFullYear(), d.getMonth() + 1, 0)
      from = `${getLocalDateString(firstDay)}T00:00:00+08:00`
      to = `${getLocalDateString(lastDay)}T23:59:59+08:00`
      break
    }
  }

  return { from, to }
}

// When unit changes, reset to current period
watch(dateUnit, () => {
  currentDate.value = new Date()
})

// Sorted stats by cost (descending), then by total tokens (descending)
const getTotalTokens = (data: GroupStats) => {
  return data.inputTokens + data.outputTokens + data.cacheCreationInputTokens + data.cacheReadInputTokens
}

const detectUpdatedGroups = (oldData: Record<string, GroupStats> | undefined, newData: Record<string, GroupStats> | undefined) => {
  const updated = new Set<string>()
  if (!newData) return updated

  for (const key of Object.keys(newData)) {
    const oldVal = oldData?.[key]
    const newVal = newData[key]
    if (!oldVal || oldVal.count !== newVal.count || oldVal.cost !== newVal.cost || getTotalTokens(oldVal) !== getTotalTokens(newVal)) {
      updated.add(key)
    }
  }

  return updated
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

const sortedByUser = computed(() => {
  if (!stats.value?.byUser) return []
  return Object.entries(stats.value.byUser)
    .sort(([, a], [, b]) => {
      const costDiff = b.cost - a.cost
      if (costDiff !== 0) return costDiff
      return getTotalTokens(b) - getTotalTokens(a)
    })
})

const sortedBySession = computed(() => {
  if (!stats.value?.bySession) return []
  return Object.entries(stats.value.bySession)
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

const userTotals = computed(() => {
  const totals = { count: 0, inputTokens: 0, outputTokens: 0, cacheCreationInputTokens: 0, cacheReadInputTokens: 0, cost: 0 }
  for (const [, data] of sortedByUser.value) {
    totals.count += data.count
    totals.inputTokens += data.inputTokens
    totals.outputTokens += data.outputTokens
    totals.cacheCreationInputTokens += data.cacheCreationInputTokens
    totals.cacheReadInputTokens += data.cacheReadInputTokens
    totals.cost += data.cost
  }
  return totals
})

const sessionTotals = computed(() => {
  const totals = { count: 0, inputTokens: 0, outputTokens: 0, cacheCreationInputTokens: 0, cacheReadInputTokens: 0, cost: 0 }
  for (const [, data] of sortedBySession.value) {
    totals.count += data.count
    totals.inputTokens += data.inputTokens
    totals.outputTokens += data.outputTokens
    totals.cacheCreationInputTokens += data.cacheCreationInputTokens
    totals.cacheReadInputTokens += data.cacheReadInputTokens
    totals.cost += data.cost
  }
  return totals
})

// Unified summary computed properties
const currentSortedData = computed(() => {
  switch (summaryGroupBy.value) {
    case 'model':
      return sortedByModel.value
    case 'provider':
      return sortedByProvider.value
    case 'user':
      return sortedByUser.value
    case 'session':
      return sortedBySession.value
    default:
      return []
  }
})

const currentTotals = computed(() => {
  switch (summaryGroupBy.value) {
    case 'model':
      return modelTotals.value
    case 'provider':
      return providerTotals.value
    case 'user':
      return userTotals.value
    case 'session':
      return sessionTotals.value
    default:
      return modelTotals.value
  }
})

const currentUpdatedSet = computed(() => {
  switch (summaryGroupBy.value) {
    case 'model':
      return updatedModels.value
    case 'provider':
      return updatedProviders.value
    case 'user':
      return updatedUsers.value
    case 'session':
      return updatedSessions.value
    default:
      return new Set()
  }
})

// Computed total width for summary table
const summaryTableWidth = computed(() => {
  return summaryColumnWidths.value.name +
    summaryColumnWidths.value.requests +
    summaryColumnWidths.value.input +
    summaryColumnWidths.value.output +
    summaryColumnWidths.value.cacheCreation +
    summaryColumnWidths.value.cacheHit +
    summaryColumnWidths.value.cost
})

// Summary table column widths
const defaultSummaryColumnWidths: Record<string, number> = {
  name: 200,
  requests: 70,
  input: 80,
  output: 80,
  cacheCreation: 80,
  cacheHit: 80,
  cost: 80
}

const summaryColumnWidths = ref<Record<string, number>>({ ...defaultSummaryColumnWidths })

// Load summary column widths from localStorage
const loadSummaryColumnWidths = () => {
  try {
    const saved = localStorage.getItem('requestlog-summary-column-widths')
    if (saved) {
      summaryColumnWidths.value = { ...defaultSummaryColumnWidths, ...JSON.parse(saved) }
    }
  } catch (e) {
    console.error('Failed to load summary column widths:', e)
  }
}

// Save summary column widths to localStorage
const saveSummaryColumnWidths = () => {
  try {
    localStorage.setItem('requestlog-summary-column-widths', JSON.stringify(summaryColumnWidths.value))
  } catch (e) {
    console.error('Failed to save summary column widths:', e)
  }
}

// Summary column resize logic
const resizingSummaryColumn = ref<string | null>(null)
const summaryResizeStartX = ref(0)
const summaryResizeStartWidth = ref(0)

const startSummaryResize = (e: MouseEvent, columnKey: string) => {
  e.preventDefault()
  resizingSummaryColumn.value = columnKey
  summaryResizeStartX.value = e.pageX
  summaryResizeStartWidth.value = summaryColumnWidths.value[columnKey]

  document.addEventListener('mousemove', onSummaryResize)
  document.addEventListener('mouseup', stopSummaryResize)
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
}

const onSummaryResize = (e: MouseEvent) => {
  if (!resizingSummaryColumn.value) return

  const diff = e.pageX - summaryResizeStartX.value
  const newWidth = Math.max(50, Math.min(400, summaryResizeStartWidth.value + diff))
  summaryColumnWidths.value[resizingSummaryColumn.value] = newWidth
}

const stopSummaryResize = () => {
  if (resizingSummaryColumn.value) {
    saveSummaryColumnWidths()
  }
  resizingSummaryColumn.value = null
  document.removeEventListener('mousemove', onSummaryResize)
  document.removeEventListener('mouseup', stopSummaryResize)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
}

// Panel widths (percentages) - default: 42% / 42% / 16%
const defaultPanelWidths = {
  summary: 42,
  reserved: 42,
  dateFilter: 16
}

const panelWidths = ref<{ summary: number; reserved: number; dateFilter: number }>({ ...defaultPanelWidths })
const topPanelsContainer = ref<HTMLElement | null>(null)

// Load panel widths from localStorage
const loadPanelWidths = () => {
  try {
    const saved = localStorage.getItem('requestlog-panel-widths')
    if (saved) {
      panelWidths.value = { ...defaultPanelWidths, ...JSON.parse(saved) }
    }
  } catch (e) {
    console.error('Failed to load panel widths:', e)
  }
}

// Save panel widths to localStorage
const savePanelWidths = () => {
  try {
    localStorage.setItem('requestlog-panel-widths', JSON.stringify(panelWidths.value))
  } catch (e) {
    console.error('Failed to save panel widths:', e)
  }
}

// Panel resize logic
const resizingSplitter = ref<string | null>(null)
const panelResizeStartX = ref(0)
const panelResizeStartWidths = ref<{ summary: number; reserved: number; dateFilter: number }>({ ...defaultPanelWidths })

const startPanelResize = (e: MouseEvent, splitter: string) => {
  e.preventDefault()
  resizingSplitter.value = splitter
  panelResizeStartX.value = e.pageX
  panelResizeStartWidths.value = { ...panelWidths.value }

  document.addEventListener('mousemove', onPanelResize)
  document.addEventListener('mouseup', stopPanelResize)
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
}

const onPanelResize = (e: MouseEvent) => {
  if (!resizingSplitter.value || !topPanelsContainer.value) return

  const containerWidth = topPanelsContainer.value.offsetWidth
  const diffPx = e.pageX - panelResizeStartX.value
  const diffPercent = (diffPx / containerWidth) * 100

  const minWidth = 10 // minimum 10% for any panel

  if (resizingSplitter.value === 'splitter1') {
    // Adjust summary and reserved panels
    let newSummary = panelResizeStartWidths.value.summary + diffPercent
    let newReserved = panelResizeStartWidths.value.reserved - diffPercent

    // Enforce minimum widths
    if (newSummary < minWidth) {
      newSummary = minWidth
      newReserved = panelResizeStartWidths.value.summary + panelResizeStartWidths.value.reserved - minWidth
    }
    if (newReserved < minWidth) {
      newReserved = minWidth
      newSummary = panelResizeStartWidths.value.summary + panelResizeStartWidths.value.reserved - minWidth
    }

    panelWidths.value.summary = newSummary
    panelWidths.value.reserved = newReserved
  } else if (resizingSplitter.value === 'splitter2') {
    // Adjust reserved and dateFilter panels
    let newReserved = panelResizeStartWidths.value.reserved + diffPercent
    let newDateFilter = panelResizeStartWidths.value.dateFilter - diffPercent

    // Enforce minimum widths
    if (newReserved < minWidth) {
      newReserved = minWidth
      newDateFilter = panelResizeStartWidths.value.reserved + panelResizeStartWidths.value.dateFilter - minWidth
    }
    if (newDateFilter < minWidth) {
      newDateFilter = minWidth
      newReserved = panelResizeStartWidths.value.reserved + panelResizeStartWidths.value.dateFilter - minWidth
    }

    panelWidths.value.reserved = newReserved
    panelWidths.value.dateFilter = newDateFilter
  }
}

const stopPanelResize = () => {
  if (resizingSplitter.value) {
    savePanelWidths()
  }
  resizingSplitter.value = null
  document.removeEventListener('mousemove', onPanelResize)
  document.removeEventListener('mouseup', stopPanelResize)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
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
  userId: 220,
  sessionId: 240,
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
  { title: t('requestLog.user'), key: 'userId', sortable: false, width: `${columnWidths.value.userId}px` },
  { title: t('requestLog.session'), key: 'sessionId', sortable: false, width: `${columnWidths.value.sessionId}px` },
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

// Watch date/unit changes - debounced to avoid rapid re-fetches
let dateChangeTimeout: ReturnType<typeof setTimeout> | null = null
watch([currentDate, dateUnit], () => {
  if (dateChangeTimeout) clearTimeout(dateChangeTimeout)
  dateChangeTimeout = setTimeout(() => {
    offset.value = 0
    refreshLogs()
  }, 100)
}, { immediate: false })

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

const formatId = (value?: string) => {
  if (!value) return '—'
  const trimmed = value.trim()
  if (trimmed.length <= 8) return trimmed
  return `${trimmed.slice(0, 4)}...${trimmed.slice(-4)}`
}

const normalizeUserId = (userID?: string) => {
  if (!userID) return ''
  const trimmed = userID.trim()
  if (trimmed.startsWith('user_')) return trimmed.slice('user_'.length)
  return trimmed
}

const formatUserId = (userID?: string) => {
  const normalized = normalizeUserId(userID)
  if (!normalized) return '—'
  return formatId(normalized)
}

const formatSummaryKey = (key: string) => {
  if (key === '<unknown>') return key
  switch (summaryGroupBy.value) {
    case 'user':
      return formatUserId(key)
    case 'session':
      return formatId(key)
    default:
      return key
  }
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

const confirmCleanupLogs = () => {
  showConfirmCleanup.value = true
}

const cleanupLogs = async () => {
  cleaning.value = true
  try {
    const result = await api.cleanupRequestLogs(retentionDays.value)
    showConfirmCleanup.value = false
    cleanupResultCount.value = result.deletedCount
    cleanupResultDays.value = result.retentionDays
    showCleanupResult.value = true
    refreshLogs()
  } catch (error) {
    console.error('Failed to cleanup logs:', error)
  } finally {
    cleaning.value = false
  }
}

onMounted(() => {
  loadColumnWidths()
  loadSummaryColumnWidths()
  loadPanelWidths()
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

    // Detect updated groups in stats
    const newUpdatedModels = detectUpdatedGroups(stats.value?.byModel, statsRes?.byModel)
    const newUpdatedProviders = detectUpdatedGroups(stats.value?.byProvider, statsRes?.byProvider)
    const newUpdatedUsers = detectUpdatedGroups(stats.value?.byUser, statsRes?.byUser)
    const newUpdatedSessions = detectUpdatedGroups(stats.value?.bySession, statsRes?.bySession)

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
    if (newUpdatedUsers.size > 0) {
      updatedUsers.value = newUpdatedUsers
      setTimeout(() => {
        updatedUsers.value = new Set()
      }, 1000)
    }
    if (newUpdatedSessions.size > 0) {
      updatedSessions.value = newUpdatedSessions
      setTimeout(() => {
        updatedSessions.value = new Set()
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

/* Top panels container with flex layout */
.top-panels-container {
  display: flex;
  align-items: stretch;
  gap: 0;
}

.panel-wrapper {
  flex-shrink: 0;
  min-width: 0;
}

.panel-wrapper .summary-card {
  height: 100%;
}

/* Panel splitter */
.panel-splitter {
  width: 12px;
  flex-shrink: 0;
  cursor: col-resize;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  z-index: 10;
}

.panel-splitter:hover .splitter-handle,
.panel-splitter:active .splitter-handle {
  background: rgb(var(--v-theme-primary));
  opacity: 1;
}

.splitter-handle {
  width: 4px;
  height: 40px;
  background: rgb(var(--v-theme-on-surface));
  opacity: 0.3;
  border-radius: 2px;
  transition: all 0.2s ease;
}

.v-theme--dark .splitter-handle {
  background: rgba(255, 255, 255, 0.5);
}

/* Mobile: stack panels vertically */
@media (max-width: 960px) {
  .top-panels-container {
    flex-direction: column;
    gap: 16px;
  }

  .panel-wrapper {
    width: 100% !important;
  }

  .panel-splitter {
    display: none;
  }
}

/* Provider chip with custom icon */
.provider-chip :deep(.custom-icon) {
  margin-right: 4px;
}

.provider-chip :deep(.custom-icon svg) {
  width: 14px;
  height: 14px;
}

.mono-text {
  font-family: 'Courier New', monospace;
}

.id-tooltip {
  font-family: 'Courier New', monospace;
  font-size: 12px;
  white-space: nowrap;
  color: #fff;
}

.id-cell {
  display: inline-block;
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  max-height: 110px; /* 4 rows (4 × 26px) + 6px buffer */
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

/* Summary header with group by select */
.summary-header {
  border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.1);
}

.group-by-select {
  max-width: 220px;
}

.group-by-select :deep(.v-field) {
  font-size: 0.8rem;
}

.group-by-select :deep(.v-field__input) {
  padding: 4px 8px;
  min-height: 28px;
}

/* Summary table header wrapper */
.summary-table-header-wrapper {
  overflow: hidden;
}

/* Summary table body wrapper (scrollable) */
.summary-table-body-wrapper {
  max-height: 110px; /* 4 rows (4 × 26px) + 6px buffer */
  overflow-y: auto;
  overflow-x: hidden;
}

/* Custom summary table (replaces v-table) */
.summary-table-custom {
  border-collapse: collapse;
  font-size: 0.75rem;
  font-family: 'Courier New', monospace;
  table-layout: fixed;
}

.summary-table-custom th {
  font-size: 0.75rem !important;
  font-weight: 700 !important;
  font-family: 'Courier New', monospace !important;
  padding: 6px 8px !important;
  text-align: left;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  position: relative;
}

.summary-table-custom td {
  padding: 4px 8px !important;
  font-family: 'Courier New', monospace !important;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Name cell with proper ellipsis */
.summary-name-cell {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Resizable summary headers */
.resizable-summary-header,
.resizable-summary-header-last {
  position: relative;
}

.resizable-summary-table .resize-handle {
  position: absolute;
  top: 0;
  right: -4px;
  bottom: 0;
  width: 8px;
  cursor: col-resize;
  user-select: none;
  z-index: 100;
  background: transparent;
  transition: all 0.2s ease;
}

.resizable-summary-table .resize-handle::before {
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

.resizable-summary-table .resize-handle:hover {
  background: rgba(var(--v-theme-primary), 0.2);
  width: 12px;
  right: -6px;
}

.resizable-summary-table .resize-handle:hover::before {
  height: 18px;
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

.unit-toggle {
  border: 1px solid rgba(var(--v-theme-on-surface), 0.2) !important;
  border-radius: 4px !important;
}

.unit-toggle .v-btn {
  font-size: 0.7rem !important;
  min-width: 40px !important;
  padding: 0 8px !important;
  text-transform: none !important;
  letter-spacing: normal !important;
}

.date-year {
  font-size: 1.1rem;
  font-weight: 700;
  font-family: 'Courier New', monospace;
  color: rgba(var(--v-theme-on-surface), 0.6);
  margin-bottom: 2px;
}

.date-nav {
  display: flex;
  align-items: center;
}

.date-month {
  font-size: 1.5rem;
  font-weight: 700;
  font-family: 'Courier New', monospace;
  min-width: 36px;
  text-align: center;
}

.date-days {
  font-size: 1.25rem;
  font-weight: 700;
  font-family: 'Courier New', monospace;
  color: rgb(var(--v-theme-primary));
  margin-top: 2px;
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

/* =========================================
   Mobile Responsive Styles (≤600px)
   ========================================= */
@media (max-width: 600px) {
  /* Date filter - compact layout on mobile */
  .date-filter-card {
    min-height: auto !important;
  }

  .date-filter {
    padding: 8px !important;
  }

  .unit-toggle .v-btn {
    font-size: 0.6rem !important;
    min-width: 32px !important;
    padding: 0 6px !important;
  }

  .date-year {
    font-size: 0.9rem;
  }

  .date-month {
    font-size: 1.2rem;
  }

  .date-days {
    font-size: 1rem;
  }

  /* Summary tables - hide some columns on mobile */
  .summary-table .col-small:nth-child(n+4) {
    display: none;
  }

  .summary-table-footer-fixed .col-small:nth-child(n+4) {
    display: none;
  }

  .col-model {
    width: 50%;
  }

  .col-small {
    width: 25%;
  }

  /* Action bar - stack elements */
  .action-bar {
    flex-wrap: wrap;
    gap: 8px;
    padding: 8px !important;
  }

  /* Touch-friendly buttons - minimum 44x44px */
  .neo-btn {
    min-width: 44px !important;
    min-height: 44px !important;
  }

  /* Log table - horizontal scroll */
  .log-table-card {
    overflow-x: auto;
  }

  .log-table {
    min-width: 800px;
  }

  /* Pagination - stack on mobile */
  .pagination-container {
    flex-direction: column;
    gap: 8px;
    align-items: stretch !important;
  }

  .pagination-container > * {
    justify-content: center;
  }
}


</style>
