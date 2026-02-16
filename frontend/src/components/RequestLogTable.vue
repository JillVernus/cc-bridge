<template>
  <div class="request-log-container">
    <!-- 统计面板和日期筛选 -->
    <div class="top-panels-container mb-4" ref="topPanelsContainer">
      <!-- 统一的统计表格 -->
      <div class="panel-wrapper" :style="{ width: panelWidths.summary + '%' }">
        <v-card class="summary-card" density="compact">
          <!-- Group by selector header -->
          <div class="summary-header d-flex align-center px-2 py-1">
            <v-btn-toggle
              v-model="summaryGroupBy"
              mandatory
              density="compact"
              class="group-by-toggle"
            >
              <v-tooltip :text="t('requestLog.groupByProvider')" location="top">
                <template v-slot:activator="{ props }">
                  <v-btn v-bind="props" value="provider" size="x-small">
                    <v-icon size="18">mdi-cloud-outline</v-icon>
                  </v-btn>
                </template>
              </v-tooltip>
              <v-tooltip :text="t('requestLog.groupByModel')" location="top">
                <template v-slot:activator="{ props }">
                  <v-btn v-bind="props" value="model" size="x-small">
                    <v-icon size="18">mdi-head-snowflake-outline</v-icon>
                  </v-btn>
                </template>
              </v-tooltip>
              <v-tooltip :text="t('requestLog.groupByClient')" location="top">
                <template v-slot:activator="{ props }">
                  <v-btn v-bind="props" value="client" size="x-small">
                    <v-icon size="18">mdi-laptop</v-icon>
                  </v-btn>
                </template>
              </v-tooltip>
              <v-tooltip :text="t('requestLog.groupBySession')" location="top">
                <template v-slot:activator="{ props }">
                  <v-btn v-bind="props" value="session" size="x-small">
                    <v-icon size="18">mdi-chat-processing-outline</v-icon>
                  </v-btn>
                </template>
              </v-tooltip>
              <v-tooltip :text="t('requestLog.groupByApiKey')" location="top">
                <template v-slot:activator="{ props }">
                  <v-btn v-bind="props" value="apiKey" size="x-small">
                    <v-icon size="18">mdi-key-outline</v-icon>
                  </v-btn>
                </template>
              </v-tooltip>
            </v-btn-toggle>
          </div>
          <!-- Header table -->
          <div class="summary-table-header-wrapper">
            <table class="summary-table-custom resizable-summary-table" :style="{ width: summaryTableWidth + 'px' }">
              <thead>
                <tr>
                  <th class="resizable-summary-header sortable-header" :style="{ width: summaryColumnWidths.name + 'px' }" @click="toggleSummarySort('name')">
                    <span class="header-content">
                      {{ summaryNameHeaderTitle }}
                      <v-icon v-if="summarySortColumn === 'name'" size="14" class="sort-icon">{{ summarySortDirection === 'asc' ? 'mdi-arrow-up' : 'mdi-arrow-down' }}</v-icon>
                    </span>
                    <div class="resize-handle" @mousedown.stop="startSummaryResize($event, 'name')"></div>
                  </th>
                  <th class="text-end resizable-summary-header sortable-header" :style="{ width: summaryColumnWidths.requests + 'px' }" @click="toggleSummarySort('requests')">
                    <span class="header-content">
                      {{ t('requestLog.requests') }}
                      <v-icon v-if="summarySortColumn === 'requests'" size="14" class="sort-icon">{{ summarySortDirection === 'asc' ? 'mdi-arrow-up' : 'mdi-arrow-down' }}</v-icon>
                    </span>
                    <div class="resize-handle" @mousedown.stop="startSummaryResize($event, 'requests')"></div>
                  </th>
                  <th class="text-end resizable-summary-header sortable-header" :style="{ width: summaryColumnWidths.input + 'px' }" @click="toggleSummarySort('input')">
                    <span class="header-content">
                      {{ t('requestLog.input') }}
                      <v-icon v-if="summarySortColumn === 'input'" size="14" class="sort-icon">{{ summarySortDirection === 'asc' ? 'mdi-arrow-up' : 'mdi-arrow-down' }}</v-icon>
                    </span>
                    <div class="resize-handle" @mousedown.stop="startSummaryResize($event, 'input')"></div>
                  </th>
                  <th class="text-end resizable-summary-header sortable-header" :style="{ width: summaryColumnWidths.output + 'px' }" @click="toggleSummarySort('output')">
                    <span class="header-content">
                      {{ t('requestLog.output') }}
                      <v-icon v-if="summarySortColumn === 'output'" size="14" class="sort-icon">{{ summarySortDirection === 'asc' ? 'mdi-arrow-up' : 'mdi-arrow-down' }}</v-icon>
                    </span>
                    <div class="resize-handle" @mousedown.stop="startSummaryResize($event, 'output')"></div>
                  </th>
                  <th class="text-end resizable-summary-header sortable-header" :style="{ width: summaryColumnWidths.cacheCreation + 'px' }" @click="toggleSummarySort('cacheCreation')">
                    <span class="header-content">
                      {{ t('requestLog.cacheCreation') }}
                      <v-icon v-if="summarySortColumn === 'cacheCreation'" size="14" class="sort-icon">{{ summarySortDirection === 'asc' ? 'mdi-arrow-up' : 'mdi-arrow-down' }}</v-icon>
                    </span>
                    <div class="resize-handle" @mousedown.stop="startSummaryResize($event, 'cacheCreation')"></div>
                  </th>
                  <th class="text-end resizable-summary-header sortable-header" :style="{ width: summaryColumnWidths.cacheHit + 'px' }" @click="toggleSummarySort('cacheHit')">
                    <span class="header-content">
                      {{ t('requestLog.cacheHit') }}
                      <v-icon v-if="summarySortColumn === 'cacheHit'" size="14" class="sort-icon">{{ summarySortDirection === 'asc' ? 'mdi-arrow-up' : 'mdi-arrow-down' }}</v-icon>
                    </span>
                    <div class="resize-handle" @mousedown.stop="startSummaryResize($event, 'cacheHit')"></div>
                  </th>
                  <th class="text-end resizable-summary-header sortable-header" :style="{ width: summaryColumnWidths.cacheHitRate + 'px' }" @click="toggleSummarySort('cacheHitRate')">
                    <span class="header-content">
                      {{ t('requestLog.cacheHitRate') }}
                      <v-icon v-if="summarySortColumn === 'cacheHitRate'" size="14" class="sort-icon">{{ summarySortDirection === 'asc' ? 'mdi-arrow-up' : 'mdi-arrow-down' }}</v-icon>
                    </span>
                    <div class="resize-handle" @mousedown.stop="startSummaryResize($event, 'cacheHitRate')"></div>
                  </th>
                  <th class="text-end resizable-summary-header sortable-header" :style="{ width: summaryColumnWidths.cost + 'px' }" @click="toggleSummarySort('cost')">
                    <span class="header-content">
                      {{ t('requestLog.cost') }}
                      <v-icon v-if="summarySortColumn === 'cost'" size="14" class="sort-icon">{{ summarySortDirection === 'asc' ? 'mdi-arrow-up' : 'mdi-arrow-down' }}</v-icon>
                    </span>
                    <div class="resize-handle" @mousedown.stop="startSummaryResize($event, 'cost')"></div>
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
                      v-if="(summaryGroupBy === 'client' || summaryGroupBy === 'session') && String(key) && String(key) !== '<unknown>'"
                      location="top"
                      max-width="600"
                    >
                      <template v-slot:activator="{ props }">
                        <span
                          v-bind="props"
                          :class="{ 'clickable-id': summaryGroupBy === 'client' }"
                          @click.stop="summaryGroupBy === 'client' && openAliasDialog(String(key))"
                        >
                          {{ formatSummaryKey(String(key)) }}
                        </span>
                      </template>
                      <div v-if="summaryGroupBy === 'client'" class="id-tooltip">
                        <div v-if="getUserAlias(String(key))" class="alias-tooltip-row">
                          <span class="alias-label">{{ t('requestLog.alias') }}:</span> {{ getUserAlias(String(key)) }}
                        </div>
                        <div>
                          <span class="id-label">{{ t('requestLog.clientId') }}:</span> {{ normalizeUserId(String(key)) }}
                        </div>
                      </div>
                      <span v-else class="id-tooltip">{{ String(key) }}</span>
                    </v-tooltip>
                    <span v-else>{{ formatSummaryKey(String(key)) }}</span>
                  </td>
                  <td class="text-end text-caption" :style="{ width: summaryColumnWidths.requests + 'px' }">{{ data.count }}</td>
                  <td class="text-end text-caption" :style="{ width: summaryColumnWidths.input + 'px' }">{{ formatNumber(data.inputTokens) }}</td>
                  <td class="text-end text-caption" :style="{ width: summaryColumnWidths.output + 'px' }">{{ formatNumber(data.outputTokens) }}</td>
                  <td class="text-end text-caption text-success" :style="{ width: summaryColumnWidths.cacheCreation + 'px' }">{{ formatNumber(data.cacheCreationInputTokens) }}</td>
                  <td class="text-end text-caption text-warning" :style="{ width: summaryColumnWidths.cacheHit + 'px' }">{{ formatNumber(data.cacheReadInputTokens) }}</td>
                  <td class="text-end text-caption" :style="{ width: summaryColumnWidths.cacheHitRate + 'px' }">
                    <v-tooltip :text="t('requestLog.cacheHitRateTooltip')" location="top">
                      <template #activator="{ props }">
                        <span v-bind="props" class="hit-rate-value">{{ calcHitRate(data) }}%</span>
                      </template>
                    </v-tooltip>
                  </td>
                  <td class="text-end text-caption cost-cell" :style="{ width: summaryColumnWidths.cost + 'px' }">{{ formatPriceSummary(data.cost) }}</td>
                </tr>
                <tr v-if="currentSortedData.length === 0">
                  <td colspan="8" class="text-center text-caption text-grey">{{ t('common.noData') }}</td>
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
                  <td class="text-end text-caption font-weight-bold" :style="{ width: summaryColumnWidths.cacheHitRate + 'px' }">
                    <v-tooltip :text="t('requestLog.cacheHitRateTooltip')" location="top">
                      <template #activator="{ props }">
                        <span v-bind="props" class="hit-rate-value">{{ calcHitRate(currentTotals) }}%</span>
                      </template>
                    </v-tooltip>
                  </td>
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

      <!-- 活跃会话 -->
      <div class="panel-wrapper" :style="{ width: panelWidths.reserved + '%' }">
        <v-card class="summary-card" density="compact">
          <!-- Header -->
          <div class="summary-header d-flex align-center px-2 py-1">
            <v-tooltip :text="t('requestLog.activeSessions')" location="top">
              <template v-slot:activator="{ props }">
                <v-icon v-bind="props" size="18">mdi-chat-processing-outline</v-icon>
              </template>
            </v-tooltip>
            <v-spacer />
            <span class="text-caption text-grey">{{ activeSessions.length }} {{ t('requestLog.sessions') }}</span>
          </div>
          <!-- Header table -->
          <div class="summary-table-header-wrapper">
            <table class="summary-table-custom resizable-summary-table" :style="{ width: activeSessionsTableWidth + 'px' }">
              <thead>
                <tr>
                  <th class="resizable-summary-header" :style="{ width: activeSessionColumnWidths.session + 'px' }">
                    {{ t('requestLog.session') }}
                    <div class="resize-handle" @mousedown="startActiveSessionResize($event, 'session')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: activeSessionColumnWidths.live + 'px' }">
                    {{ t('requestLog.live') }}
                    <div class="resize-handle" @mousedown="startActiveSessionResize($event, 'live')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: activeSessionColumnWidths.requests + 'px' }">
                    {{ t('requestLog.requests') }}
                    <div class="resize-handle" @mousedown="startActiveSessionResize($event, 'requests')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: activeSessionColumnWidths.input + 'px' }">
                    {{ t('requestLog.input') }}
                    <div class="resize-handle" @mousedown="startActiveSessionResize($event, 'input')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: activeSessionColumnWidths.output + 'px' }">
                    {{ t('requestLog.output') }}
                    <div class="resize-handle" @mousedown="startActiveSessionResize($event, 'output')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: activeSessionColumnWidths.cache + 'px' }">
                    {{ t('requestLog.cacheCreation') }}
                    <div class="resize-handle" @mousedown="startActiveSessionResize($event, 'cache')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: activeSessionColumnWidths.hit + 'px' }">
                    {{ t('requestLog.cacheHit') }}
                    <div class="resize-handle" @mousedown="startActiveSessionResize($event, 'hit')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: activeSessionColumnWidths.hitRate + 'px' }">
                    {{ t('requestLog.cacheHitRate') }}
                    <div class="resize-handle" @mousedown="startActiveSessionResize($event, 'hitRate')"></div>
                  </th>
                  <th class="text-end resizable-summary-header" :style="{ width: activeSessionColumnWidths.cost + 'px' }">
                    {{ t('requestLog.cost') }}
                    <div class="resize-handle" @mousedown="startActiveSessionResize($event, 'cost')"></div>
                  </th>
                </tr>
              </thead>
            </table>
          </div>
          <!-- Body table (scrollable) -->
          <div class="summary-table-body-wrapper">
            <table class="summary-table-custom" :style="{ width: activeSessionsTableWidth + 'px' }">
              <tbody>
                <tr
                  v-for="session in activeSessions"
                  :key="session.sessionId"
                  :class="{ 'summary-row-flash': updatedActiveSessions.has(session.sessionId) }"
                >
                  <td :style="{ width: activeSessionColumnWidths.session + 'px', maxWidth: activeSessionColumnWidths.session + 'px' }">
                    <div class="d-flex align-center">
                      <v-icon v-if="session.type === 'claude'" size="14" icon="custom:claude" class="mr-1" />
                      <v-icon v-else-if="session.type === 'gemini'" size="14" icon="custom:gemini" class="mr-1" />
                      <v-icon v-else size="14" icon="custom:codex" class="mr-1" />
                      <v-tooltip location="top" max-width="400">
                        <template v-slot:activator="{ props }">
                          <span v-bind="props" class="text-caption">{{ formatId(session.sessionId) }}</span>
                        </template>
                        <span class="id-tooltip">{{ session.sessionId }}</span>
                      </v-tooltip>
                    </div>
                  </td>
                  <td class="text-end text-caption" :style="{ width: activeSessionColumnWidths.live + 'px', maxWidth: activeSessionColumnWidths.live + 'px' }">{{ formatLiveTime(session.firstRequestTime) }}</td>
                  <td class="text-end text-caption" :style="{ width: activeSessionColumnWidths.requests + 'px', maxWidth: activeSessionColumnWidths.requests + 'px' }">{{ session.count }}</td>
                  <td class="text-end text-caption" :style="{ width: activeSessionColumnWidths.input + 'px', maxWidth: activeSessionColumnWidths.input + 'px' }">{{ formatNumber(session.inputTokens) }}</td>
                  <td class="text-end text-caption" :style="{ width: activeSessionColumnWidths.output + 'px', maxWidth: activeSessionColumnWidths.output + 'px' }">{{ formatNumber(session.outputTokens) }}</td>
                  <td class="text-end text-caption text-success" :style="{ width: activeSessionColumnWidths.cache + 'px', maxWidth: activeSessionColumnWidths.cache + 'px' }">{{ formatNumber(session.cacheCreationInputTokens) }}</td>
                  <td class="text-end text-caption text-warning" :style="{ width: activeSessionColumnWidths.hit + 'px', maxWidth: activeSessionColumnWidths.hit + 'px' }">{{ formatNumber(session.cacheReadInputTokens) }}</td>
                  <td class="text-end text-caption" :style="{ width: activeSessionColumnWidths.hitRate + 'px', maxWidth: activeSessionColumnWidths.hitRate + 'px' }">
                    <v-tooltip :text="t('requestLog.cacheHitRateTooltip')" location="top">
                      <template #activator="{ props }">
                        <span v-bind="props" class="hit-rate-value">{{ calcHitRate(session) }}%</span>
                      </template>
                    </v-tooltip>
                  </td>
                  <td class="text-end text-caption cost-cell" :style="{ width: activeSessionColumnWidths.cost + 'px', maxWidth: activeSessionColumnWidths.cost + 'px' }">{{ formatPriceSummary(session.cost) }}</td>
                </tr>
                <tr v-if="activeSessions.length === 0">
                  <td colspan="9" class="text-center text-caption text-grey pa-4">{{ t('requestLog.noActiveSessions') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </v-card>
      </div>

      <!-- Splitter 2 -->
      <div v-if="!isDateFilterCollapsed" class="panel-splitter" @mousedown="startPanelResize($event, 'splitter2')">
        <div class="splitter-handle"></div>
      </div>
      <!-- Spacer when collapsed -->
      <div v-else class="panel-splitter-spacer"></div>

      <!-- 日期筛选 -->
      <div class="panel-wrapper date-filter-panel" :class="{ collapsed: isDateFilterCollapsed }" :style="{ width: panelWidths.dateFilter + '%' }">
        <v-card class="summary-card date-filter-card" density="compact">
          <!-- Collapsed state: just the expand button -->
          <div v-if="isDateFilterCollapsed" class="date-filter-collapsed d-flex align-center justify-center">
            <v-btn
              icon
              variant="text"
              size="small"
              class="collapse-toggle-btn"
              @click="toggleDateFilterCollapsed"
            >
              <v-icon size="20">mdi-chevron-double-left</v-icon>
            </v-btn>
          </div>
          <!-- Expanded state: full date filter -->
          <div v-else class="date-filter d-flex flex-column align-center justify-center pa-2">
            <!-- Collapse button -->
            <v-btn
              icon
              variant="text"
              size="x-small"
              class="collapse-toggle-btn date-filter-collapse-btn"
              @click="toggleDateFilterCollapsed"
            >
              <v-icon size="18">mdi-chevron-double-right</v-icon>
            </v-btn>
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
      <v-card class="settings-card modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4">
          <v-icon class="mr-2">mdi-cog</v-icon>
          {{ t('requestLog.settings') }}
          <v-spacer />
          <v-btn
            icon
            variant="text"
            size="small"
            @click="showSettings = false"
            class="modal-action-btn"
          >
            <v-icon>mdi-close</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">
          <div class="settings-section mb-4">
            <div class="text-subtitle-2 mb-2">{{ t('requestLog.columnVisibility') }}</div>
            <div class="column-visibility-grid">
              <v-checkbox
                v-for="(name, key) in columnDisplayNames"
                :key="key"
                :model-value="columnVisibility[key]"
                :label="name"
                density="compact"
                hide-details
                class="column-visibility-checkbox"
                @update:model-value="toggleColumnVisibility(key)"
              />
            </div>
            <div class="d-flex align-center ga-2 mt-2">
              <v-btn
                size="small"
                variant="tonal"
                @click="resetColumnVisibility"
              >
                <v-icon class="mr-1" size="16">mdi-eye</v-icon>
                {{ t('requestLog.showAllColumns') }}
              </v-btn>
            </div>
            <div class="text-caption text-grey mt-2">{{ t('requestLog.columnVisibilityDesc') }}</div>
          </div>

          <v-divider class="my-4" />

          <!-- Column Stacking Settings -->
          <div class="settings-section mb-4">
            <div class="text-subtitle-2 mb-2 d-flex align-center">
              <v-icon size="18" class="mr-1">mdi-arrow-collapse-vertical</v-icon>
              {{ t('requestLog.columnStacking') }}
            </div>
            <div class="stacking-options">
              <div
                v-for="config in stackPairConfigs"
                :key="config.primary"
                class="stacking-row"
              >
                <span class="text-body-2 stacking-label">{{ getStackPairLabel(config.primary) }}</span>
                <v-btn-toggle
                  v-model="stackModes[config.primary]"
                  mandatory
                  density="compact"
                  variant="outlined"
                  divided
                  class="stacking-toggle"
                  @update:model-value="saveStackingPrefs"
                >
                  <v-btn value="expanded" size="x-small">
                    {{ t('requestLog.stackExpanded') }}
                  </v-btn>
                  <v-btn value="stacked" size="x-small">
                    {{ t('requestLog.stackStacked') }}
                  </v-btn>
                  <v-btn value="responsive" size="x-small">
                    {{ t('requestLog.stackAuto') }}
                  </v-btn>
                </v-btn-toggle>
              </div>
            </div>
            <div class="d-flex align-center ga-2 mt-2">
              <v-btn size="small" variant="tonal" @click="resetStackingPrefs">
                <v-icon size="16" class="mr-1">mdi-restore</v-icon>
                {{ t('requestLog.resetStacking') }}
              </v-btn>
            </div>
            <div class="text-caption text-grey mt-2">{{ t('requestLog.columnStackingDesc') }}</div>
          </div>

          <v-divider class="my-4" />

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
            <div class="text-subtitle-2 mb-2">{{ t('requestLog.autoRefreshInterval') }}</div>
            <div class="d-flex align-center ga-3">
              <v-slider
                v-model="autoRefreshInterval"
                :min="1"
                :max="60"
                :step="1"
                thumb-label
                hide-details
                style="max-width: 300px"
                @update:model-value="saveAutoRefreshInterval"
              />
              <span class="text-body-2" style="min-width: 60px">{{ autoRefreshInterval }}s</span>
            </div>
            <div class="text-caption text-grey mt-2">{{ t('requestLog.autoRefreshIntervalDesc') }}</div>
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

          <div class="settings-section mb-4">
            <div class="text-subtitle-2 mb-2">{{ t('requestLog.userAliasManagement') }}</div>

            <!-- Alias List -->
            <div v-if="Object.keys(userAliases).length > 0" class="alias-list mb-3">
              <div
                v-for="(alias, visibleUserId) in userAliases"
                :key="visibleUserId"
                class="alias-item d-flex align-center"
              >
                <span class="alias-name font-weight-medium">{{ alias }}</span>
                <span class="mx-2 text-grey">→</span>
                <v-tooltip location="top">
                  <template v-slot:activator="{ props }">
                    <span v-bind="props" class="alias-userid mono-text">
                      {{ formatId(normalizeUserId(String(visibleUserId))) }}
                    </span>
                  </template>
                  <span class="id-tooltip">{{ normalizeUserId(String(visibleUserId)) }}</span>
                </v-tooltip>
                <v-spacer />
                <v-btn icon size="x-small" variant="text" @click="openAliasDialog(String(visibleUserId))">
                  <v-icon size="16">mdi-pencil</v-icon>
                </v-btn>
                <v-btn icon size="x-small" variant="text" color="error" @click="removeAlias(String(visibleUserId))">
                  <v-icon size="16">mdi-delete</v-icon>
                </v-btn>
              </div>
            </div>
            <div v-else class="text-caption text-grey mb-3">
              {{ t('requestLog.noAliases') }}
            </div>

            <div class="text-caption text-grey">{{ t('requestLog.userAliasDesc') }}</div>
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
      </v-card>
    </v-dialog>

    <!-- 确认对话框 -->
    <v-dialog v-model="showConfirmClear" max-width="400">
      <v-card class="modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4 text-error">
          {{ t('requestLog.confirmClear') }}
          <v-spacer />
          <v-btn icon variant="text" size="small" @click="showConfirmClear = false" class="modal-action-btn">
            <v-icon>mdi-close</v-icon>
          </v-btn>
          <v-btn icon variant="flat" size="small" color="error" @click="clearLogs" :loading="clearing" class="modal-action-btn">
            <v-icon>mdi-check</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">{{ t('requestLog.confirmClearDesc') }}</v-card-text>
      </v-card>
    </v-dialog>

    <!-- 确认清理对话框 -->
    <v-dialog v-model="showConfirmCleanup" max-width="400">
      <v-card class="modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4 text-warning">
          {{ t('requestLog.confirmCleanup') }}
          <v-spacer />
          <v-btn icon variant="text" size="small" @click="showConfirmCleanup = false" class="modal-action-btn">
            <v-icon>mdi-close</v-icon>
          </v-btn>
          <v-btn icon variant="flat" size="small" color="warning" @click="cleanupLogs" :loading="cleaning" class="modal-action-btn">
            <v-icon>mdi-check</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">{{ t('requestLog.confirmCleanupDesc', { days: retentionDays }) }}</v-card-text>
      </v-card>
    </v-dialog>

    <!-- 清理结果对话框 -->
    <v-dialog v-model="showCleanupResult" max-width="400">
      <v-card class="modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4 text-success">
          <v-icon class="mr-2" color="success">mdi-check-circle</v-icon>
          {{ t('requestLog.cleanupComplete') }}
          <v-spacer />
          <v-btn icon variant="text" size="small" @click="showCleanupResult = false" class="modal-action-btn">
            <v-icon>mdi-close</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">
          <div class="text-body-1">
            {{ t('requestLog.cleanupResultDesc', { count: cleanupResultCount, days: cleanupResultDays }) }}
          </div>
        </v-card-text>
      </v-card>
    </v-dialog>

    <!-- 用户别名对话框 -->
    <v-dialog v-model="showAliasDialog" max-width="450">
      <v-card class="modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4">
          <v-icon class="mr-2">mdi-account-edit</v-icon>
          {{ t('requestLog.assignAlias') }}
          <v-spacer />
          <v-btn icon variant="text" size="small" @click="showAliasDialog = false" class="modal-action-btn">
            <v-icon>mdi-close</v-icon>
          </v-btn>
          <v-btn
            icon
            variant="flat"
            size="small"
            color="primary"
            @click="saveAlias"
            :disabled="!!aliasError || !aliasInput.trim()"
            class="modal-action-btn"
          >
            <v-icon>mdi-check</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">
          <!-- Full User ID (read-only) -->
          <div class="text-caption text-grey mb-1">{{ t('requestLog.clientId') }}</div>
          <div class="mono-text text-body-2 mb-4 user-id-display">
            {{ normalizeUserId(editingUserId) }}
          </div>

          <!-- Alias Input -->
          <v-text-field
            v-model="aliasInput"
            :label="t('requestLog.aliasName')"
            :error-messages="aliasError"
            :placeholder="t('requestLog.aliasPlaceholder')"
            density="compact"
            variant="outlined"
            maxlength="30"
            counter
            @input="validateAlias"
          />
          <!-- Remove alias button (only shown if alias exists) -->
          <v-btn
            v-if="getUserAlias(editingUserId)"
            color="error"
            variant="tonal"
            size="small"
            @click="removeAlias()"
            class="mt-2"
          >
            <v-icon class="mr-1" size="16">mdi-delete</v-icon>
            {{ t('requestLog.removeAlias') }}
          </v-btn>
        </v-card-text>
      </v-card>
    </v-dialog>

    <!-- 操作栏 -->
    <div class="action-bar mb-4">
      <!-- Live indicator (clickable to toggle SSE) -->
      <v-tooltip :text="sseEnabled ? t('requestLog.clickToDisableSSE') : t('requestLog.clickToEnableSSE')" location="top">
        <template v-slot:activator="{ props }">
          <v-chip
            v-if="sseEnabled && isSSEConnected"
            v-bind="props"
            color="success"
            size="small"
            variant="flat"
            class="mr-2 live-indicator clickable-chip"
            @click="toggleSSEEnabled"
          >
            <v-icon size="12" class="mr-1 pulse-icon">mdi-circle</v-icon>
            Live
          </v-chip>
          <v-chip
            v-else-if="sseEnabled && sseConnectionState === 'connecting'"
            v-bind="props"
            color="warning"
            size="small"
            variant="tonal"
            class="mr-2 clickable-chip"
            @click="toggleSSEEnabled"
          >
            <v-icon size="14" class="mr-1 spin">mdi-loading</v-icon>
            Connecting...
          </v-chip>
          <v-chip
            v-else
            v-bind="props"
            color="grey"
            size="small"
            variant="tonal"
            class="mr-2 clickable-chip"
            @click="toggleSSEEnabled"
          >
            <v-icon size="12" class="mr-1">mdi-circle-outline</v-icon>
            {{ t('requestLog.polling') }}
          </v-chip>
        </template>
      </v-tooltip>
      <v-btn
        variant="text"
        size="small"
        class="neo-btn"
        :class="{ 'neo-btn-active': autoRefreshEnabled }"
        @click="toggleAutoRefresh"
      >
        <v-icon size="18" class="mr-1" :class="{ 'spin': autoRefreshEnabled && !isSSEConnected }">{{ autoRefreshEnabled ? 'mdi-sync' : 'mdi-sync-off' }}</v-icon>
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
        <!-- Custom headers with resize handles - using headers slot for proper dynamic binding -->
        <template v-slot:headers="{ columns }">
          <tr>
            <th
              v-for="column in columns"
              :key="column.key ?? column.title"
              :style="{ width: column.width }"
              class="v-data-table__th"
            >
              <div :class="column.key === 'status' ? 'resizable-header-last' : 'resizable-header'">
                <span :class="getHeaderColorClass(column.key ?? '')">{{ column.title }}</span>
                <div
                  v-if="column.key && column.key !== 'status'"
                  class="resize-handle"
                  @mousedown="startResize($event, column.key)"
                ></div>
              </div>
            </th>
          </tr>
        </template>

        <template v-slot:item.status="{ item }">
          <div class="d-flex align-center ga-1">
            <v-progress-circular
              v-if="item.status === 'pending'"
              indeterminate
              size="16"
              width="2"
              color="warning"
            />
            <v-tooltip v-else-if="item.error || item.upstreamError || item.failoverInfo" location="top" max-width="400">
              <template v-slot:activator="{ props }">
                <v-chip
                  v-bind="props"
                  size="x-small"
                  :color="getRequestStatusColor(item.status)"
                  variant="flat"
                >
                  {{ item.httpStatus || getRequestStatusLabel(item.status) }}
                </v-chip>
              </template>
              <div class="error-tooltip">
                <div v-if="item.failoverInfo" class="failover-info-line">
                  <span class="error-label">{{ t('requestLog.failoverInfo') }}:</span>
                  <span class="failover-info-text">{{ item.failoverInfo }}</span>
                </div>
                <div v-if="item.error" class="error-line">
                  <span class="error-label">{{ t('requestLog.error') }}:</span>
                  <span>{{ item.error }}</span>
                </div>
                <div v-if="item.upstreamError" class="upstream-error-line">
                  <span class="error-label">{{ t('requestLog.upstreamResponse') }}:</span>
                  <span class="upstream-error-text">{{ item.upstreamError }}</span>
                </div>
              </div>
            </v-tooltip>
            <v-chip
              v-else
              size="x-small"
              :color="getRequestStatusColor(item.status)"
              variant="flat"
            >
              {{ item.httpStatus || getRequestStatusLabel(item.status) }}
            </v-chip>
            <!-- Debug data indicator -->
            <v-tooltip v-if="item.hasDebugData" location="top">
              <template v-slot:activator="{ props }">
                <v-icon v-bind="props" size="14" color="info" class="debug-indicator">mdi-bug-outline</v-icon>
              </template>
              {{ t('requestLog.hasDebugData') }}
            </v-tooltip>
          </div>
        </template>

        <template v-slot:item.initialTime="{ item }">
          <!-- Stacked: Time + Duration -->
          <v-tooltip v-if="isStacked('initialTime')" location="top" max-width="300">
            <template v-slot:activator="{ props }">
              <div v-bind="props" class="stacked-cell">
                <span class="text-caption">{{ formatTime(item.initialTime) }}</span>
                <span v-if="item.status === 'pending'" class="stacked-secondary">
                  <v-progress-circular indeterminate size="10" width="1" color="grey" />
                </span>
                <span v-else class="stacked-secondary duration-text" :class="'duration-' + getDurationColor(item.durationMs)">
                  {{ formatDuration(item.durationMs) }}
                </span>
              </div>
            </template>
            <div class="stacked-tooltip">
              <div><strong>{{ t('requestLog.time') }}:</strong> {{ formatTime(item.initialTime) }}</div>
              <div><strong>{{ t('requestLog.duration') }}:</strong> {{ item.status === 'pending' ? '...' : formatDuration(item.durationMs) }}</div>
            </div>
          </v-tooltip>
          <!-- Expanded: Time only -->
          <span v-else class="text-caption">{{ formatTime(item.initialTime) }}</span>
        </template>

        <template v-slot:item.durationMs="{ item }">
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="16"
            width="2"
            color="grey"
          />
          <span v-else class="duration-text" :class="'duration-' + getDurationColor(item.durationMs)">
            {{ formatDuration(item.durationMs) }}
          </span>
        </template>

        <template v-slot:item.providerName="{ item }">
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="16"
            width="2"
            color="grey"
          />
          <!-- Stacked: Channel + Model -->
          <v-tooltip v-else-if="isStacked('providerName')" location="top" max-width="400">
            <template v-slot:activator="{ props }">
              <div v-bind="props" class="stacked-cell">
                <v-chip size="x-small" variant="text" class="provider-chip">
                  <v-icon v-if="item.type === 'claude'" start size="14" icon="custom:claude" class="provider-icon mr-1" />
                  <v-icon v-else-if="item.type === 'openai' || item.type === 'codex' || item.type === 'responses'" start size="14" icon="custom:codex" class="provider-icon mr-1" />
                  <v-icon v-else-if="item.type === 'gemini'" start size="14" icon="custom:gemini" class="provider-icon mr-1" />
                  {{ item.providerName || item.type }}
                </v-chip>
                <span v-if="item.model" class="stacked-secondary">
                  {{ item.model }}
                  <v-icon v-if="item.reasoningEffort" size="12" class="ml-1" :color="getReasoningEffortColor(item.reasoningEffort)">{{ getReasoningEffortIcon(item.reasoningEffort) }}</v-icon>
                  <v-icon v-else-if="item.responseModel && item.responseModel !== item.model" size="10" class="ml-1">mdi-swap-horizontal</v-icon>
                </span>
              </div>
            </template>
            <div class="stacked-tooltip">
              <div><strong>{{ t('requestLog.channel') }}:</strong> {{ item.providerName || item.type }}</div>
              <div><strong>{{ t('requestLog.model') }}:</strong> {{ item.model }}</div>
              <div v-if="item.reasoningEffort"><strong>{{ t('requestLog.reasoningEffort') }}</strong> {{ item.reasoningEffort }}</div>
              <div v-if="item.responseModel && item.responseModel !== item.model">
                <strong>Mapped:</strong> {{ item.model }} → {{ item.responseModel }}
              </div>
            </div>
          </v-tooltip>
          <!-- Expanded: Channel only -->
          <v-chip v-else size="x-small" variant="text" class="provider-chip">
            <v-icon v-if="item.type === 'claude'" start size="14" icon="custom:claude" class="provider-icon mr-1" />
            <v-icon v-else-if="item.type === 'openai' || item.type === 'codex' || item.type === 'responses'" start size="14" icon="custom:codex" class="provider-icon mr-1" />
            <v-icon v-else-if="item.type === 'gemini'" start size="14" icon="custom:gemini" class="provider-icon mr-1" />
            {{ item.providerName || item.type }}
          </v-chip>
        </template>

        <template v-slot:item.model="{ item }">
          <!-- Model with reasoning effort icon -->
          <v-tooltip v-if="item.reasoningEffort" location="top" max-width="300">
            <template v-slot:activator="{ props }">
              <span v-bind="props" class="text-caption font-weight-medium model-with-effort">
                {{ item.model }}
                <v-icon size="20" class="ml-1" :color="getReasoningEffortColor(item.reasoningEffort)">
                  {{ getReasoningEffortIcon(item.reasoningEffort) }}
                </v-icon>
              </span>
            </template>
            <div class="reasoning-effort-tooltip">
              <span class="effort-label">{{ t('logs.reasoningEffort') }}</span>
              <span class="effort-value" :class="'effort-' + item.reasoningEffort.toLowerCase()">
                {{ item.reasoningEffort }}
              </span>
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

        <template v-slot:item.apiKeyId="{ item }">
          <v-tooltip v-if="item.apiKeyId !== undefined && item.apiKeyId !== null && getAPIKeyName(item.apiKeyId)" location="top" max-width="300">
            <template v-slot:activator="{ props }">
              <v-chip v-bind="props" size="x-small" variant="tonal" :color="item.apiKeyId === 0 ? 'warning' : 'primary'">
                {{ getAPIKeyName(item.apiKeyId) }}
              </v-chip>
            </template>
            <span class="id-tooltip">{{ item.apiKeyId === 0 ? 'Master Key (from .env)' : `ID: ${item.apiKeyId}` }}</span>
          </v-tooltip>
          <span v-else class="text-caption mono-text id-cell">—</span>
        </template>

        <template v-slot:item.clientId="{ item }">
          <!-- Stacked: Client + Session -->
          <v-tooltip v-if="isStacked('clientId')" location="top" max-width="600">
            <template v-slot:activator="{ props }">
              <div v-bind="props" class="stacked-cell">
                <span
                  v-if="item.clientId"
                  class="text-caption mono-text id-cell clickable-id"
                  @click.stop="openAliasDialog(item.clientId)"
                >
                  {{ getDisplayUserId(item.clientId) }}
                </span>
                <span v-else class="text-caption mono-text id-cell">—</span>
                <span v-if="item.sessionId" class="stacked-secondary mono-text">
                  {{ formatId(item.sessionId) }}
                </span>
              </div>
            </template>
            <div class="id-tooltip">
              <div v-if="item.clientId">
                <div v-if="getUserAlias(item.clientId)" class="alias-tooltip-row">
                  <span class="alias-label">{{ t('requestLog.alias') }}:</span> {{ getUserAlias(item.clientId) }}
                </div>
                <div>
                  <span class="id-label">{{ t('requestLog.clientId') }}:</span> {{ normalizeUserId(item.clientId) }}
                </div>
              </div>
              <div v-else>
                <span class="id-label">{{ t('requestLog.clientId') }}:</span> —
              </div>
              <div>
                <span class="id-label">{{ t('requestLog.sessionId') }}:</span> {{ item.sessionId || '—' }}
              </div>
            </div>
          </v-tooltip>
          <!-- Expanded: Client only -->
          <v-tooltip v-else-if="item.clientId" location="top" max-width="600">
            <template v-slot:activator="{ props }">
              <span
                v-bind="props"
                class="text-caption mono-text id-cell clickable-id"
                @click.stop="openAliasDialog(item.clientId)"
              >
                {{ getDisplayUserId(item.clientId) }}
              </span>
            </template>
            <div class="id-tooltip">
              <div v-if="getUserAlias(item.clientId)" class="alias-tooltip-row">
                <span class="alias-label">{{ t('requestLog.alias') }}:</span> {{ getUserAlias(item.clientId) }}
              </div>
              <div>
                <span class="id-label">{{ t('requestLog.clientId') }}:</span> {{ normalizeUserId(item.clientId) }}
              </div>
            </div>
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
          <v-progress-circular
            v-if="item.status === 'pending'"
            indeterminate
            size="14"
            width="2"
            color="grey"
          />
          <v-tooltip v-else location="top" max-width="300">
            <template v-slot:activator="{ props }">
              <div v-bind="props" class="tokens-stacked">
                <span class="token-cell input-color"><span class="token-num">{{ formatTokensCompact(item.inputTokens) }}</span><span class="token-sym">↑</span></span>
                <span class="token-cell output-color"><span class="token-num">{{ formatTokensCompact(item.outputTokens) }}</span><span class="token-sym">↓</span></span>
                <template v-if="item.cacheCreationInputTokens || item.cacheReadInputTokens">
                  <span v-if="item.cacheCreationInputTokens" class="token-cell cache-create-color cache-cell"><span class="token-num">{{ formatTokensCompact(item.cacheCreationInputTokens) }}</span><span class="token-sym">+</span></span>
                  <span v-else class="token-cell cache-cell"></span>
                  <span v-if="item.cacheReadInputTokens" class="token-cell cache-hit-color cache-cell"><span class="token-num">{{ formatTokensCompact(item.cacheReadInputTokens) }}</span><span class="token-sym">⚡</span></span>
                  <span v-else class="token-cell cache-cell"></span>
                </template>
              </div>
            </template>
            <div class="tokens-tooltip">
              <div class="tooltip-row"><span class="tooltip-label">{{ t('requestLog.input') }}:</span> <span class="input-color">{{ formatNumber(item.inputTokens) }}</span></div>
              <div class="tooltip-row"><span class="tooltip-label">{{ t('requestLog.output') }}:</span> <span class="output-color">{{ formatNumber(item.outputTokens) }}</span></div>
              <div v-if="item.cacheCreationInputTokens" class="tooltip-row"><span class="tooltip-label">{{ t('requestLog.cacheCreation') }}:</span> <span class="cache-create-color">{{ formatNumber(item.cacheCreationInputTokens) }}</span></div>
              <div v-if="item.cacheReadInputTokens" class="tooltip-row"><span class="tooltip-label">{{ t('requestLog.cacheHit') }}:</span> <span class="cache-hit-color">{{ formatNumber(item.cacheReadInputTokens) }}</span></div>
            </div>
          </v-tooltip>
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

    <!-- Debug Modal -->
    <RequestDebugModal
      v-model="showDebugModal"
      :request-id="selectedRequestId"
      :log-item="selectedLogItem"
    />
  </div>
</template>

<script setup lang="ts">
	import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
	import { useI18n } from 'vue-i18n'
	import { useDisplay } from 'vuetify'
	import { api, type RequestLog, type RequestLogStats, type GroupStats, type ActiveSession, type APIKey } from '../services/api'
	import RequestDebugModal from './RequestDebugModal.vue'
	import { useLogStream, type LogCreatedPayload, type LogUpdatedPayload, type ConnectionState } from '../composables/useLogStream'

	// i18n
	const { t } = useI18n()

	// Viewport detection for responsive stacking
	const { width: viewportWidth } = useDisplay()

		const emit = defineEmits<{
		  (e: 'dateRangeChange', payload: { from: string; to: string }): void
		  (e: 'sseStateChange', state: ConnectionState): void
		  (e: 'pollingEnabledChange', enabled: boolean): void
		}>()

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
const updatedClients = ref<Set<string>>(new Set())
const updatedSessions = ref<Set<string>>(new Set())
const updatedAPIKeys = ref<Set<string>>(new Set())

// Active sessions state
const activeSessions = ref<ActiveSession[]>([])
const updatedActiveSessions = ref<Set<string>>(new Set())

// User alias state
const userAliases = ref<Record<string, string>>({})
const showAliasDialog = ref(false)
const editingUserId = ref<string>('')
const aliasInput = ref<string>('')
const aliasError = ref<string>('')

// Debug modal state
const showDebugModal = ref(false)
const selectedRequestId = ref<string | null>(null)
const selectedLogItem = ref<RequestLog | null>(null)

// API keys state (for displaying key names by ID)
const apiKeys = ref<APIKey[]>([])
const apiKeyMap = computed(() => {
  const map = new Map<number, string>()
  for (const key of apiKeys.value) {
    map.set(key.id, key.name)
  }
  return map
})

// Summary table group by state
type SummaryGroupBy = 'model' | 'provider' | 'client' | 'session' | 'apiKey'
const summaryGroupBy = ref<SummaryGroupBy>('provider')
const summaryGroupByOptions = computed(() => [
  { label: t('requestLog.groupByModel'), value: 'model' },
  { label: t('requestLog.groupByProvider'), value: 'provider' },
  { label: t('requestLog.groupByClient'), value: 'client' },
  { label: t('requestLog.groupBySession'), value: 'session' },
  { label: t('requestLog.groupByApiKey'), value: 'apiKey' },
])

const summaryNameHeaderTitle = computed(() => {
  switch (summaryGroupBy.value) {
    case 'model':
      return t('requestLog.model')
    case 'provider':
      return t('requestLog.channel')
    case 'client':
      return t('requestLog.clientId')
    case 'session':
      return t('requestLog.sessionId')
    case 'apiKey':
      return t('requestLog.apiKey')
    default:
      return t('requestLog.model')
  }
})

// Summary table sorting state
type SummarySortColumn = 'name' | 'requests' | 'input' | 'output' | 'cacheCreation' | 'cacheHit' | 'cacheHitRate' | 'cost'
type SortDirection = 'asc' | 'desc'
const summarySortColumn = ref<SummarySortColumn>('cost')
const summarySortDirection = ref<SortDirection>('desc')

// Toggle sort when clicking header
const toggleSummarySort = (column: SummarySortColumn) => {
  if (summarySortColumn.value === column) {
    // Toggle direction
    summarySortDirection.value = summarySortDirection.value === 'asc' ? 'desc' : 'asc'
  } else {
    // New column, default to desc
    summarySortColumn.value = column
    summarySortDirection.value = 'desc'
  }
}

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

// Detect changes in active sessions
const detectActiveSessionChanges = (oldSessions: ActiveSession[], newSessions: ActiveSession[]) => {
  const updated = new Set<string>()
  if (!newSessions) return updated

  const oldMap = new Map(oldSessions.map(s => [s.sessionId, s]))

  for (const newSession of newSessions) {
    const oldSession = oldMap.get(newSession.sessionId)
    if (!oldSession ||
        oldSession.count !== newSession.count ||
        oldSession.cost !== newSession.cost ||
        oldSession.inputTokens !== newSession.inputTokens ||
        oldSession.outputTokens !== newSession.outputTokens) {
      updated.add(newSession.sessionId)
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

const sortedByClient = computed(() => {
  if (!stats.value?.byClient) return []
  return Object.entries(stats.value.byClient)
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

const sortedByAPIKey = computed(() => {
  if (!stats.value?.byApiKey) return []
  return Object.entries(stats.value.byApiKey)
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

const clientTotals = computed(() => {
  const totals = { count: 0, inputTokens: 0, outputTokens: 0, cacheCreationInputTokens: 0, cacheReadInputTokens: 0, cost: 0 }
  for (const [, data] of sortedByClient.value) {
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

const apiKeyTotals = computed(() => {
  const totals = { count: 0, inputTokens: 0, outputTokens: 0, cacheCreationInputTokens: 0, cacheReadInputTokens: 0, cost: 0 }
  for (const [, data] of sortedByAPIKey.value) {
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
  // Get raw data based on groupBy
  let rawData: Record<string, GroupStats> | undefined
  switch (summaryGroupBy.value) {
    case 'model':
      rawData = stats.value?.byModel
      break
    case 'provider':
      rawData = stats.value?.byProvider
      break
    case 'client':
      rawData = stats.value?.byClient
      break
    case 'session':
      rawData = stats.value?.bySession
      break
    case 'apiKey':
      rawData = stats.value?.byApiKey
      break
  }
  if (!rawData) return []

  // Convert to array and sort
  const entries = Object.entries(rawData)
  const col = summarySortColumn.value
  const dir = summarySortDirection.value

  return entries.sort(([keyA, a], [keyB, b]) => {
    let diff = 0
    switch (col) {
      case 'name':
        diff = keyA.localeCompare(keyB)
        break
      case 'requests':
        diff = a.count - b.count
        break
      case 'input':
        diff = a.inputTokens - b.inputTokens
        break
      case 'output':
        diff = a.outputTokens - b.outputTokens
        break
      case 'cacheCreation':
        diff = a.cacheCreationInputTokens - b.cacheCreationInputTokens
        break
      case 'cacheHit':
        diff = a.cacheReadInputTokens - b.cacheReadInputTokens
        break
      case 'cacheHitRate':
        diff = calcHitRate(a) - calcHitRate(b)
        break
      case 'cost':
        diff = a.cost - b.cost
        break
    }
    // Apply direction
    return dir === 'asc' ? diff : -diff
  })
})

const currentTotals = computed(() => {
  switch (summaryGroupBy.value) {
    case 'model':
      return modelTotals.value
    case 'provider':
      return providerTotals.value
    case 'client':
      return clientTotals.value
    case 'session':
      return sessionTotals.value
    case 'apiKey':
      return apiKeyTotals.value
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
    case 'client':
      return updatedClients.value
    case 'session':
      return updatedSessions.value
    case 'apiKey':
      return updatedAPIKeys.value
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
    summaryColumnWidths.value.cacheHitRate +
    summaryColumnWidths.value.cost
})

// Active sessions column widths (resizable)
const defaultActiveSessionColumnWidths: Record<string, number> = {
  session: 140,
  live: 70,
  requests: 50,
  input: 60,
  output: 60,
  cache: 50,
  hit: 50,
  hitRate: 50,
  cost: 60
}

const activeSessionColumnWidths = ref<Record<string, number>>({ ...defaultActiveSessionColumnWidths })

const activeSessionsTableWidth = computed(() => {
  return activeSessionColumnWidths.value.session +
    activeSessionColumnWidths.value.live +
    activeSessionColumnWidths.value.requests +
    activeSessionColumnWidths.value.input +
    activeSessionColumnWidths.value.output +
    activeSessionColumnWidths.value.cache +
    activeSessionColumnWidths.value.hit +
    activeSessionColumnWidths.value.hitRate +
    activeSessionColumnWidths.value.cost
})

// Load active session column widths from localStorage
const loadActiveSessionColumnWidths = () => {
  try {
    const saved = localStorage.getItem('requestlog-active-session-column-widths')
    if (saved) {
      activeSessionColumnWidths.value = { ...defaultActiveSessionColumnWidths, ...JSON.parse(saved) }
    }
  } catch (e) {
    console.error('Failed to load active session column widths:', e)
  }
}

// Save active session column widths to localStorage
const saveActiveSessionColumnWidths = () => {
  try {
    localStorage.setItem('requestlog-active-session-column-widths', JSON.stringify(activeSessionColumnWidths.value))
  } catch (e) {
    console.error('Failed to save active session column widths:', e)
  }
}

// Active session column resize logic
const resizingActiveSessionColumn = ref<string | null>(null)
const activeSessionResizeStartX = ref(0)
const activeSessionResizeStartWidth = ref(0)

const startActiveSessionResize = (e: MouseEvent, column: string) => {
  e.preventDefault()
  resizingActiveSessionColumn.value = column
  activeSessionResizeStartX.value = e.pageX
  activeSessionResizeStartWidth.value = activeSessionColumnWidths.value[column]

  document.addEventListener('mousemove', onActiveSessionResize)
  document.addEventListener('mouseup', stopActiveSessionResize)
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
}

const onActiveSessionResize = (e: MouseEvent) => {
  if (!resizingActiveSessionColumn.value) return

  const diff = e.pageX - activeSessionResizeStartX.value
  const newWidth = Math.max(30, activeSessionResizeStartWidth.value + diff)
  activeSessionColumnWidths.value[resizingActiveSessionColumn.value] = newWidth
}

const stopActiveSessionResize = () => {
  if (resizingActiveSessionColumn.value) {
    saveActiveSessionColumnWidths()
  }
  resizingActiveSessionColumn.value = null
  document.removeEventListener('mousemove', onActiveSessionResize)
  document.removeEventListener('mouseup', stopActiveSessionResize)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
}

// User alias API functions
const loadUserAliases = async () => {
  try {
    // First, check if there are aliases in localStorage to migrate
    const localAliases = localStorage.getItem('requestlog-user-aliases')
    if (localAliases) {
      const parsed = JSON.parse(localAliases)
      if (Object.keys(parsed).length > 0) {
        // Migrate to backend
        try {
          await api.importUserAliases(parsed)
          localStorage.removeItem('requestlog-user-aliases')
          console.log('Migrated user aliases from localStorage to backend')
        } catch (e) {
          console.warn('Failed to migrate aliases to backend:', e)
        }
      }
    }

    // Load from backend
    userAliases.value = await api.getUserAliases()
  } catch (e) {
    console.error('Failed to load user aliases:', e)
    // Fallback to localStorage if backend fails
    try {
      const saved = localStorage.getItem('requestlog-user-aliases')
      if (saved) {
        userAliases.value = JSON.parse(saved)
      }
    } catch (e2) {
      console.error('Failed to load user aliases from localStorage:', e2)
    }
  }
}

// Load API keys for displaying names
const loadAPIKeys = async () => {
  try {
    const response = await api.getAPIKeys()
    apiKeys.value = response.keys || []
  } catch (e) {
    console.error('Failed to load API keys:', e)
  }
}

// Get API key name by ID
const getAPIKeyName = (apiKeyId?: number): string | null => {
  if (apiKeyId === undefined || apiKeyId === null) return null
  // apiKeyId = 0 means master key (bootstrap admin from .env)
  if (apiKeyId === 0) return 'master'
  return apiKeyMap.value.get(apiKeyId) || null
}

// User alias helper functions
const getUserAlias = (userId: string): string | null => {
  return userAliases.value[userId] || null
}

const isAliasUnique = (alias: string, excludeUserId?: string): boolean => {
  const lowerAlias = alias.toLowerCase().trim()
  for (const [userId, existingAlias] of Object.entries(userAliases.value)) {
    if (excludeUserId && userId === excludeUserId) continue
    if (existingAlias.toLowerCase().trim() === lowerAlias) return false
  }
  return true
}

const getDisplayUserId = (userId: string): string => {
  const alias = getUserAlias(userId)
  if (alias) return alias
  return formatUserId(userId)
}

// Reasoning effort icon mapping (gauge icons)
const getReasoningEffortIcon = (effort: string): string => {
  const effortLower = effort.toLowerCase()
  switch (effortLower) {
    case 'low':
      return 'mdi-gauge-empty'
    case 'medium':
      return 'mdi-gauge-low'
    case 'high':
      return 'mdi-gauge'
    case 'xhigh':
      return 'mdi-gauge-full'
    default:
      return 'mdi-gauge-low'
  }
}

// Reasoning effort color mapping
const getReasoningEffortColor = (effort: string): string => {
  const effortLower = effort.toLowerCase()
  switch (effortLower) {
    case 'low':
      return 'success'
    case 'medium':
      return 'info'
    case 'high':
      return 'warning'
    case 'xhigh':
      return 'error'
    default:
      return 'grey'
  }
}

// Summary table column widths
const defaultSummaryColumnWidths: Record<string, number> = {
  name: 200,
  requests: 70,
  input: 80,
  output: 80,
  cacheCreation: 80,
  cacheHit: 80,
  cacheHitRate: 60,
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

// Date filter collapsed state
const isDateFilterCollapsed = ref(false)
const expandedPanelWidths = ref<{ summary: number; reserved: number; dateFilter: number } | null>(null)

// Load collapsed state and expanded widths from localStorage
const loadDateFilterCollapsed = () => {
  try {
    const saved = localStorage.getItem('requestlog-datefilter-collapsed')
    if (saved === 'true') {
      isDateFilterCollapsed.value = true
    }
    // Load expanded widths separately (these are the widths when expanded)
    const savedExpanded = localStorage.getItem('requestlog-panel-widths-expanded')
    if (savedExpanded) {
      const parsed = JSON.parse(savedExpanded)
      // Validate widths
      if (parsed.summary > 0 && parsed.reserved > 0 && parsed.dateFilter > 0) {
        expandedPanelWidths.value = { ...defaultPanelWidths, ...parsed }
      }
    }
  } catch (e) {
    console.error('Failed to load date filter collapsed state:', e)
  }
}

// Save collapsed state to localStorage
const saveDateFilterCollapsed = () => {
  try {
    localStorage.setItem('requestlog-datefilter-collapsed', String(isDateFilterCollapsed.value))
  } catch (e) {
    console.error('Failed to save date filter collapsed state:', e)
  }
}

// Save expanded widths separately
const saveExpandedPanelWidths = () => {
  try {
    if (expandedPanelWidths.value) {
      localStorage.setItem('requestlog-panel-widths-expanded', JSON.stringify(expandedPanelWidths.value))
    }
  } catch (e) {
    console.error('Failed to save expanded panel widths:', e)
  }
}

// Toggle date filter collapsed state
const toggleDateFilterCollapsed = () => {
  if (isDateFilterCollapsed.value) {
    // Expanding: restore saved widths
    if (expandedPanelWidths.value) {
      panelWidths.value = { ...expandedPanelWidths.value }
    }
    isDateFilterCollapsed.value = false
  } else {
    // Collapsing: save current widths and redistribute
    expandedPanelWidths.value = { ...panelWidths.value }
    saveExpandedPanelWidths()
    const collapsedWidth = 1
    const freedWidth = panelWidths.value.dateFilter - collapsedWidth
    const totalOther = panelWidths.value.summary + panelWidths.value.reserved
    // Guard against division by zero
    if (totalOther > 0) {
      panelWidths.value.summary += freedWidth * (panelWidths.value.summary / totalOther)
      panelWidths.value.reserved += freedWidth * (panelWidths.value.reserved / totalOther)
    } else {
      // Fallback: split evenly
      panelWidths.value.summary = 49
      panelWidths.value.reserved = 49
    }
    panelWidths.value.dateFilter = collapsedWidth
    isDateFilterCollapsed.value = true
  }
  saveDateFilterCollapsed()
}

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
    if (isDateFilterCollapsed.value && resizingSplitter.value === 'splitter1' && expandedPanelWidths.value) {
      // If collapsed and resizing splitter1, update expanded widths proportionally
      const oldTotal = panelResizeStartWidths.value.summary + panelResizeStartWidths.value.reserved
      const newTotal = panelWidths.value.summary + panelWidths.value.reserved
      if (oldTotal > 0 && newTotal > 0) {
        const summaryRatio = panelWidths.value.summary / newTotal
        const reservedRatio = panelWidths.value.reserved / newTotal
        const expandedOtherTotal = expandedPanelWidths.value.summary + expandedPanelWidths.value.reserved
        expandedPanelWidths.value.summary = expandedOtherTotal * summaryRatio
        expandedPanelWidths.value.reserved = expandedOtherTotal * reservedRatio
        saveExpandedPanelWidths()
      }
    } else if (!isDateFilterCollapsed.value) {
      // If not collapsed, expanded widths = current widths
      expandedPanelWidths.value = { ...panelWidths.value }
      saveExpandedPanelWidths()
    }
    savePanelWidths()
  }
  resizingSplitter.value = null
  document.removeEventListener('mousemove', onPanelResize)
  document.removeEventListener('mouseup', stopPanelResize)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
}

	// Auto-refresh
	// Default: if SSE is enabled (default), start with polling disabled and only fall back after a grace period.
	// If user disabled SSE previously, start polling immediately.
	const autoRefreshEnabled = ref(localStorage.getItem('requestlog-sse-enabled') === 'false')
	const autoRefreshInterval = ref(3) // seconds, persisted to localStorage
	let autoRefreshTimer: ReturnType<typeof setInterval> | null = null

	// Avoid thrashing between SSE and polling when the SSE connection blips briefly.
	const POLLING_FALLBACK_DELAY_MS = 3000
	let pollingFallbackTimer: ReturnType<typeof setTimeout> | null = null
	const cancelPollingFallback = () => {
	  if (pollingFallbackTimer) {
	    clearTimeout(pollingFallbackTimer)
	    pollingFallbackTimer = null
	  }
	}
	const schedulePollingFallback = () => {
	  cancelPollingFallback()
	  pollingFallbackTimer = setTimeout(() => {
	    // Only enable polling if SSE is still not connected.
	    if (!sseEnabled.value) return
	    if (sseConnectionState.value === 'connected') return
	    autoRefreshEnabled.value = true
	    startAutoRefresh()
	  }, POLLING_FALLBACK_DELAY_MS)
	}

// Stats refresh timer for SSE mode (low-frequency poll for summary tables)
const SSE_STATS_REFRESH_INTERVAL = 15000 // 15 seconds
let sseStatsRefreshTimer: ReturnType<typeof setInterval> | null = null

const loadAutoRefreshInterval = () => {
  const saved = localStorage.getItem('requestlog-auto-refresh-interval')
  if (saved !== null) {
    const val = parseInt(saved, 10)
    if (val >= 1 && val <= 60) {
      autoRefreshInterval.value = val
    }
  }
}
const saveAutoRefreshInterval = () => {
  localStorage.setItem('requestlog-auto-refresh-interval', String(autoRefreshInterval.value))
  // Restart auto-refresh with new interval if enabled
  if (autoRefreshEnabled.value) {
    stopAutoRefresh()
    startAutoRefresh()
  }
}

// SSE enabled setting (persisted to localStorage)
const sseEnabled = ref(true)
const loadSSEEnabled = () => {
  const saved = localStorage.getItem('requestlog-sse-enabled')
  if (saved !== null) {
    sseEnabled.value = saved === 'true'
  }
}
const saveSSEEnabled = () => {
  localStorage.setItem('requestlog-sse-enabled', String(sseEnabled.value))
}
	const toggleSSEEnabled = () => {
	  sseEnabled.value = !sseEnabled.value
	  saveSSEEnabled()
	  if (sseEnabled.value) {
	    // Enable SSE
	    // Prefer SSE: stop time-based polling immediately, then fall back after a short grace period.
	    cancelPollingFallback()
	    autoRefreshEnabled.value = false
	    stopAutoRefresh()
	    connectSSE()
	    schedulePollingFallback()
	  } else {
	    // Disable SSE, fall back to polling
	    disconnectSSE()
	    sseConnectionState.value = 'disconnected'
	    stopSSEStatsRefresh()
	    cancelPollingFallback()
	    autoRefreshEnabled.value = true
	    startAutoRefresh()
	  }
	}

	// SSE connection state
	const sseConnectionState = ref<ConnectionState>('disconnected')
	const isSSEConnected = computed(() => sseConnectionState.value === 'connected')
	let ssePausedByVisibility = false

// SSE event handlers
const handleLogCreated = (payload: LogCreatedPayload) => {
  const inputTokens = payload.inputTokens ?? 0
  const outputTokens = payload.outputTokens ?? 0
  const cacheCreationInputTokens = payload.cacheCreationInputTokens ?? 0
  const cacheReadInputTokens = payload.cacheReadInputTokens ?? 0
  const totalTokens = payload.totalTokens ?? (inputTokens + outputTokens)

  // Add new log to the beginning of the list
  const newLog: RequestLog = {
    id: payload.id,
    status: payload.status as 'pending' | 'completed' | 'error' | 'timeout',
    initialTime: payload.initialTime,
    completeTime: payload.completeTime || '',
    durationMs: payload.durationMs ?? 0,
    type: payload.type || '',
    providerName: payload.providerName,
    model: payload.model,
    responseModel: payload.responseModel,
    reasoningEffort: payload.reasoningEffort,
    inputTokens,
    outputTokens,
    cacheCreationInputTokens,
    cacheReadInputTokens,
    totalTokens,
    price: payload.price ?? 0,
    inputCost: payload.inputCost ?? 0,
    outputCost: payload.outputCost ?? 0,
    cacheCreationCost: payload.cacheCreationCost ?? 0,
    cacheReadCost: payload.cacheReadCost ?? 0,
    httpStatus: payload.httpStatus ?? 0,
    stream: payload.stream,
    channelId: payload.channelId,
    channelName: payload.channelName,
    endpoint: payload.endpoint,
    apiKeyId: payload.apiKeyId,
    hasDebugData: payload.hasDebugData ?? false,
    clientId: payload.clientId,
    sessionId: payload.sessionId,
    error: payload.error,
    upstreamError: payload.upstreamError,
    failoverInfo: payload.failoverInfo,
    createdAt: payload.initialTime
  }

  // Only add if not already in list
  if (!logs.value.find(l => l.id === newLog.id)) {
    logs.value = [newLog, ...logs.value.slice(0, pageSize - 1)]
    total.value++
  }

  // Flash the new row
  updatedIds.value = new Set([payload.id])
  setTimeout(() => {
    updatedIds.value = new Set()
  }, 1000)

  // Update active sessions incrementally for new request
  if (payload.sessionId) {
    const sessionIndex = activeSessions.value.findIndex(s => s.sessionId === payload.sessionId)
    if (sessionIndex !== -1) {
      // Existing session - increment count and update lastRequestTime
      const session = { ...activeSessions.value[sessionIndex] }
      session.count++
      session.lastRequestTime = payload.initialTime
      activeSessions.value = [
        ...activeSessions.value.slice(0, sessionIndex),
        session,
        ...activeSessions.value.slice(sessionIndex + 1)
      ]
      // Flash the updated session
      updatedActiveSessions.value = new Set([payload.sessionId])
      setTimeout(() => { updatedActiveSessions.value = new Set() }, 1000)
    } else {
      // New session - add to the list
      const newSession: ActiveSession = {
        sessionId: payload.sessionId,
        type: payload.endpoint?.includes('responses') ? 'codex' : 'claude',
        firstRequestTime: payload.initialTime,
        lastRequestTime: payload.initialTime,
        count: 1,
        inputTokens: 0,
        outputTokens: 0,
        cacheCreationInputTokens: 0,
        cacheReadInputTokens: 0,
        cost: 0
      }
      activeSessions.value = [newSession, ...activeSessions.value]
      // Flash the new session
      updatedActiveSessions.value = new Set([payload.sessionId])
      setTimeout(() => { updatedActiveSessions.value = new Set() }, 1000)
    }
  }
}

const handleLogUpdated = (payload: LogUpdatedPayload) => {
  // Find and update the log
  const index = logs.value.findIndex(l => l.id === payload.id)
  if (index !== -1) {
    const oldLog = logs.value[index]
    const updated = { ...oldLog }
    updated.status = payload.status as 'pending' | 'completed' | 'error' | 'timeout'
    updated.durationMs = payload.durationMs
    updated.httpStatus = payload.httpStatus
    updated.type = payload.type
    updated.providerName = payload.providerName
    updated.channelId = payload.channelId
    updated.channelName = payload.channelName
    updated.inputTokens = payload.inputTokens
    updated.outputTokens = payload.outputTokens
    updated.cacheCreationInputTokens = payload.cacheCreationInputTokens
    updated.cacheReadInputTokens = payload.cacheReadInputTokens
    updated.totalTokens = payload.totalTokens
    updated.price = payload.price
    // Cost breakdown
    updated.inputCost = payload.inputCost
    updated.outputCost = payload.outputCost
    updated.cacheCreationCost = payload.cacheCreationCost
    updated.cacheReadCost = payload.cacheReadCost
    // Other fields
    updated.apiKeyId = payload.apiKeyId
    updated.hasDebugData = payload.hasDebugData
    updated.error = payload.error
    updated.upstreamError = payload.upstreamError
    updated.failoverInfo = payload.failoverInfo
    updated.responseModel = payload.responseModel
    updated.reasoningEffort = payload.reasoningEffort
    updated.completeTime = payload.completeTime
    logs.value = [...logs.value.slice(0, index), updated, ...logs.value.slice(index + 1)]

    // Update stats incrementally if request just completed (was pending, now has data)
    if (oldLog.status === 'pending' && payload.status !== 'pending' && stats.value) {
      const model = oldLog.model || payload.responseModel || 'unknown'
      const provider = payload.providerName || 'unknown'
      const client = oldLog.clientId || 'unknown'
      const session = oldLog.sessionId || 'unknown'

      // Helper to update a group stats entry
      const updateGroup = (group: Record<string, GroupStats> | undefined, key: string) => {
        if (!group) return
        if (!group[key]) {
          group[key] = { count: 0, inputTokens: 0, outputTokens: 0, cacheCreationInputTokens: 0, cacheReadInputTokens: 0, cost: 0 }
        }
        group[key].count++
        group[key].inputTokens += payload.inputTokens
        group[key].outputTokens += payload.outputTokens
        group[key].cacheCreationInputTokens += payload.cacheCreationInputTokens
        group[key].cacheReadInputTokens += payload.cacheReadInputTokens
        group[key].cost += payload.price
      }

      // Update all stat groups
      updateGroup(stats.value.byModel, model)
      updateGroup(stats.value.byProvider, provider)
      updateGroup(stats.value.byClient, client)
      updateGroup(stats.value.bySession, session)

      // Update totals
      stats.value.totalRequests++
      stats.value.totalCost += payload.price
      if (stats.value.totalTokens) {
        stats.value.totalTokens.inputTokens += payload.inputTokens
        stats.value.totalTokens.outputTokens += payload.outputTokens
        stats.value.totalTokens.cacheCreationInputTokens += payload.cacheCreationInputTokens
        stats.value.totalTokens.cacheReadInputTokens += payload.cacheReadInputTokens
        stats.value.totalTokens.totalTokens += payload.totalTokens
      }

      // Trigger reactivity by reassigning
      stats.value = { ...stats.value }

      // Flash updated summary groups
      updatedModels.value = new Set([model])
      updatedProviders.value = new Set([provider])
      updatedClients.value = new Set([client])
      updatedSessions.value = new Set([session])
      setTimeout(() => {
        updatedModels.value = new Set()
        updatedProviders.value = new Set()
        updatedClients.value = new Set()
        updatedSessions.value = new Set()
      }, 1000)

      // Update active sessions with token/cost data when request completes
      if (session && session !== 'unknown') {
        const sessionIndex = activeSessions.value.findIndex(s => s.sessionId === session)
        if (sessionIndex !== -1) {
          const activeSession = { ...activeSessions.value[sessionIndex] }
          activeSession.inputTokens += payload.inputTokens
          activeSession.outputTokens += payload.outputTokens
          activeSession.cacheCreationInputTokens += payload.cacheCreationInputTokens
          activeSession.cacheReadInputTokens += payload.cacheReadInputTokens
          activeSession.cost += payload.price
          activeSession.lastRequestTime = payload.completeTime || activeSession.lastRequestTime
          activeSessions.value = [
            ...activeSessions.value.slice(0, sessionIndex),
            activeSession,
            ...activeSessions.value.slice(sessionIndex + 1)
          ]
          // Flash the updated session
          updatedActiveSessions.value = new Set([session])
          setTimeout(() => { updatedActiveSessions.value = new Set() }, 1000)
        }
      }
    }

    // Flash the updated row
    updatedIds.value = new Set([payload.id])
    setTimeout(() => {
      updatedIds.value = new Set()
    }, 1000)
  }
}

const handleLogDebugData = (payload: { id: string; hasDebugData: boolean }) => {
  // Find and update the log's hasDebugData field
  const index = logs.value.findIndex(l => l.id === payload.id)
  if (index !== -1) {
    const updated = { ...logs.value[index] }
    updated.hasDebugData = payload.hasDebugData
    logs.value = [...logs.value.slice(0, index), updated, ...logs.value.slice(index + 1)]
  }
}

	const handleSSEConnectionChange = (state: ConnectionState) => {
	  sseConnectionState.value = state
	  console.log(`📡 SSE connection state: ${state}`)
	  emit('sseStateChange', state)

	  // If the tab isn't visible, keep everything paused (no polling fallback).
	  if (document.visibilityState === 'hidden') {
	    stopAutoRefresh()
	    stopSSEStatsRefresh()
	    cancelPollingFallback()
	    return
	  }

	  if (state === 'connected') {
	    // SSE connected - disable all polling (SSE handlers update everything in real-time)
	    cancelPollingFallback()
	    autoRefreshEnabled.value = false
	    stopAutoRefresh()
	    stopSSEStatsRefresh()
	    silentRefresh()
	    return
	  }

	  // For brief SSE disconnects/connection blips, delay enabling polling to avoid flapping.
	  if (state === 'connecting') {
	    cancelPollingFallback()
	    autoRefreshEnabled.value = false
	    stopAutoRefresh()
	    schedulePollingFallback()
	    return
	  }

	  if (state === 'disconnected' || state === 'error') {
	    stopSSEStatsRefresh()
	    cancelPollingFallback()
	    autoRefreshEnabled.value = false
	    stopAutoRefresh()
	    schedulePollingFallback()
	  }
	}

// Initialize SSE
const { connectionState: sseState, isPollingFallback, connect: connectSSE, disconnect: disconnectSSE } = useLogStream({
  onLogCreated: handleLogCreated,
  onLogUpdated: handleLogUpdated,
  onLogDebugData: handleLogDebugData,
  onConnectionChange: handleSSEConnectionChange,
  maxRetries: 5,
  autoConnect: false
})

// Column widths with defaults
const defaultColumnWidths: Record<string, number> = {
  status: 70,
  initialTime: 140,
  durationMs: 100,
  providerName: 120,
  model: 200,
  apiKeyId: 120,
  clientId: 220,
  sessionId: 240,
  tokens: 160,
  price: 80,
}

const columnWidths = ref<Record<string, number>>({ ...defaultColumnWidths })

// Column visibility settings
const defaultColumnVisibility: Record<string, boolean> = {
  status: true,
  initialTime: true,
  durationMs: true,
  providerName: true,
  model: true,
  apiKeyId: true,
  clientId: true,
  sessionId: true,
  tokens: true,
  price: true,
}

const columnVisibility = ref<Record<string, boolean>>({ ...defaultColumnVisibility })

// === Column Stacking Feature ===
type StackMode = 'expanded' | 'stacked' | 'responsive'

interface StackPairConfig {
  primary: string
  secondary: string
}

const stackPairConfigs: StackPairConfig[] = [
  { primary: 'providerName', secondary: 'model' },
  { primary: 'initialTime', secondary: 'durationMs' },
  { primary: 'clientId', secondary: 'sessionId' },
]

// Get label for a stacking pair (computed at render time for i18n reactivity)
const getStackPairLabel = (primary: string): string => {
  switch (primary) {
    case 'providerName': return `${t('requestLog.channel')} + ${t('requestLog.model')}`
    case 'initialTime': return `${t('requestLog.time')} + ${t('requestLog.duration')}`
    case 'clientId': return `${t('requestLog.client')} + ${t('requestLog.session')}`
    default: return primary
  }
}

const defaultStackModes: Record<string, StackMode> = {
  providerName: 'expanded',
  initialTime: 'expanded',
  clientId: 'expanded',
}

const stackModes = ref<Record<string, StackMode>>({ ...defaultStackModes })

// Returns true if a primary column should render its secondary inline
const isStacked = (primaryKey: string): boolean => {
  const config = stackPairConfigs.find(p => p.primary === primaryKey)
  if (!config) return false
  // Check if secondary column is visible in settings
  if (!columnVisibility.value[config.secondary]) return false
  const mode = stackModes.value[primaryKey]
  if (mode === 'stacked') return true
  if (mode === 'responsive') return viewportWidth.value <= 960
  return false
}

// Returns true if a secondary column should be hidden from headers
const isSecondaryHidden = (key: string): boolean => {
  const config = stackPairConfigs.find(p => p.secondary === key)
  if (!config) return false
  return isStacked(config.primary)
}

const STACKING_KEY = 'requestlog-column-stacking'

const loadStackingPrefs = () => {
  try {
    const saved = localStorage.getItem(STACKING_KEY)
    if (saved) {
      const parsed = JSON.parse(saved) as Record<string, StackMode>
      Object.keys(parsed).forEach(key => {
        if (key in stackModes.value) {
          stackModes.value[key] = parsed[key]
        }
      })
    }
  } catch (e) { console.error('Failed to load stacking prefs:', e) }
}

const saveStackingPrefs = () => {
  try {
    localStorage.setItem(STACKING_KEY, JSON.stringify(stackModes.value))
  } catch (e) { console.error('Failed to save stacking prefs:', e) }
}

const resetStackingPrefs = () => {
  stackModes.value = { ...defaultStackModes }
  saveStackingPrefs()
}

// Column display names for the settings UI
const columnDisplayNames = computed(() => ({
  status: t('requestLog.status'),
  initialTime: t('requestLog.time'),
  durationMs: t('requestLog.duration'),
  providerName: t('requestLog.channel'),
  model: t('requestLog.model'),
  apiKeyId: t('requestLog.apiKey'),
  clientId: t('requestLog.client'),
  sessionId: t('requestLog.session'),
  tokens: t('requestLog.tokens'),
  price: t('requestLog.price'),
}))

// Load column visibility from localStorage
const loadColumnVisibility = () => {
  try {
    const saved = localStorage.getItem('requestlog-column-visibility')
    if (saved) {
      const parsed = JSON.parse(saved)
      // Migration: convert old individual token columns to unified 'tokens' column
      if ('inputTokens' in parsed || 'outputTokens' in parsed || 'cacheCreation' in parsed || 'cacheHit' in parsed) {
        // If any of the old token columns were visible, show the new unified column
        parsed.tokens = parsed.inputTokens || parsed.outputTokens || parsed.cacheCreation || parsed.cacheHit
        delete parsed.inputTokens
        delete parsed.outputTokens
        delete parsed.cacheCreation
        delete parsed.cacheHit
      }
      columnVisibility.value = { ...defaultColumnVisibility, ...parsed }
      // Save migrated data
      saveColumnVisibility()
    }
  } catch (e) {
    console.error('Failed to load column visibility:', e)
  }
}

// Save column visibility to localStorage
const saveColumnVisibility = () => {
  try {
    localStorage.setItem('requestlog-column-visibility', JSON.stringify(columnVisibility.value))
  } catch (e) {
    console.error('Failed to save column visibility:', e)
  }
}

// Toggle column visibility
const toggleColumnVisibility = (columnKey: string) => {
  // Ensure at least one column remains visible
  const visibleCount = Object.values(columnVisibility.value).filter(v => v).length
  if (visibleCount <= 1 && columnVisibility.value[columnKey]) {
    return // Don't allow hiding the last visible column
  }
  columnVisibility.value[columnKey] = !columnVisibility.value[columnKey]
  saveColumnVisibility()
}

// Reset column visibility to defaults
const resetColumnVisibility = () => {
  columnVisibility.value = { ...defaultColumnVisibility }
  saveColumnVisibility()
}

// Load column widths from localStorage
const loadColumnWidths = () => {
  try {
    const saved = localStorage.getItem('requestlog-column-widths')
    if (saved) {
      const parsed = JSON.parse(saved)
      // Migration: convert old individual token columns to unified 'tokens' column
      if ('inputTokens' in parsed || 'outputTokens' in parsed || 'cacheCreation' in parsed || 'cacheHit' in parsed) {
        parsed.tokens = 160
        delete parsed.inputTokens
        delete parsed.outputTokens
        delete parsed.cacheCreation
        delete parsed.cacheHit
      }
      columnWidths.value = { ...defaultColumnWidths, ...parsed }
      // Save migrated data
      saveColumnWidths()
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

const allHeaders = [
  { title: () => t('requestLog.time'), key: 'initialTime', sortable: false },
  { title: () => t('requestLog.duration'), key: 'durationMs', sortable: false },
  { title: () => t('requestLog.channel'), key: 'providerName', sortable: false },
  { title: () => t('requestLog.model'), key: 'model', sortable: false },
  { title: () => t('requestLog.apiKey'), key: 'apiKeyId', sortable: false },
  { title: () => t('requestLog.client'), key: 'clientId', sortable: false },
  { title: () => t('requestLog.session'), key: 'sessionId', sortable: false },
  { title: () => t('requestLog.tokens'), key: 'tokens', sortable: false },
  { title: () => t('requestLog.price'), key: 'price', sortable: false },
  { title: () => t('requestLog.status'), key: 'status', sortable: false },
]

const headers = computed(() =>
  allHeaders
    .filter(h => columnVisibility.value[h.key])
    .filter(h => !isSecondaryHidden(h.key))
    .map(h => ({
      ...h,
      title: h.title(),
      width: `${columnWidths.value[h.key]}px`
    }))
)

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

    // Fetch active sessions separately to avoid breaking main data on error
    try {
      activeSessions.value = await api.getActiveSessions()
    } catch (e) {
      console.warn('Failed to fetch active sessions:', e)
      activeSessions.value = []
    }
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
	    emit('dateRangeChange', getDateRange())
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

// Format duration: 7-digit left-padded number + space + "ms" (e.g., "    352 ms")
// Max 9999999ms = ~2.7 hours, sufficient for most requests
const formatDuration = (ms: number) => {
  const numStr = String(ms).slice(0, 7)
  return numStr.padStart(7, '\u00A0') + '\u00A0ms'
}

// Format tokens: 6-char left-padded abbreviated number + space + symbol (e.g., " 1.2K (↑)")
// Uses K/M abbreviations for readability
const formatTokens = (tokens: number, symbol: string) => {
  let numStr: string
  if (tokens >= 1000000) {
    numStr = (tokens / 1000000).toFixed(1) + 'M'
  } else if (tokens >= 1000) {
    numStr = (tokens / 1000).toFixed(1) + 'K'
  } else {
    numStr = String(tokens)
  }
  return numStr.padStart(6, '\u00A0') + '\u00A0' + symbol
}

// Compact token format for stacked display (2 decimals for precision)
const formatTokensCompact = (tokens: number) => {
  if (tokens >= 1000000) return (tokens / 1000000).toFixed(2) + 'M'
  if (tokens >= 1000) return (tokens / 1000).toFixed(2) + 'K'
  return String(tokens)
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

// Format live time: 56s / 10m:45s (no hours, so 70m:0s instead of 1h10m0s)
const formatLiveTime = (firstRequestTime: string): string => {
  if (!firstRequestTime) return '0s'

  // Parse the time - handle both ISO format and other formats
  const start = new Date(firstRequestTime).getTime()

  // Check if parsing failed
  if (isNaN(start)) return '0s'

  const now = Date.now()
  const diffSec = Math.floor((now - start) / 1000)

  if (diffSec < 0) return '0s'
  if (diffSec < 60) return `${diffSec}s`
  const minutes = Math.floor(diffSec / 60)
  const seconds = diffSec % 60
  return `${minutes}m:${seconds}s`
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
    case 'client':
      return getUserAlias(key) || formatUserId(key)
    case 'session':
      return formatId(key)
    case 'apiKey':
      // key is 'master' or numeric ID string
      if (key === 'master') return 'master'
      const numId = parseInt(key, 10)
      if (!isNaN(numId)) {
        return apiKeyMap.value.get(numId) || key
      }
      return key
    default:
      return key
  }
}

// Cache hit rate formula (Option A):
// cache_read / (input + cache_read + cache_creation)
// This includes cache_creation in denominator because frequent cache creation
// indicates cache mechanism issues (expiry, poor prefix consistency, etc.)
interface TokenStats {
  inputTokens: number
  cacheReadInputTokens: number
  cacheCreationInputTokens: number
}
const calcHitRate = (data: TokenStats) => {
  const total = data.inputTokens + data.cacheReadInputTokens + data.cacheCreationInputTokens
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

const getRequestStatusColor = (status: string) => {
  const colors: Record<string, string> = {
    pending: 'warning',
    completed: 'success',
    error: 'error',
    timeout: 'grey',
    failover: 'orange'
  }
  return colors[status] || 'grey'
}

const getRequestStatusLabel = (status: string) => {
  const labels: Record<string, string> = {
    pending: t('requestLog.pending'),
    completed: t('requestLog.completed'),
    error: t('requestLog.error'),
    timeout: t('requestLog.timeout'),
    failover: t('requestLog.failover')
  }
  return labels[status] || status
}

// Get color class for header based on column key
const getHeaderColorClass = (key: string): string => {
  // Previously used for individual token columns, now tokens are combined
  // Keep the function for potential future use
  return ''
}

const getRowProps = ({ item }: { item: RequestLog }) => {
  return {
    class: updatedIds.value.has(item.id) ? 'row-flash clickable-row' : 'clickable-row',
    onClick: () => openDebugModal(item)
  }
}

const openDebugModal = (item: RequestLog) => {
  selectedRequestId.value = item.id
  selectedLogItem.value = item
  showDebugModal.value = true
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

// User alias dialog functions
const openAliasDialog = (userId: string) => {
  editingUserId.value = userId
  aliasInput.value = getUserAlias(userId) || ''
  aliasError.value = ''
  showAliasDialog.value = true
}

const validateAlias = () => {
  const alias = aliasInput.value.trim()
  if (!alias) {
    aliasError.value = ''
    return
  }
  if (!isAliasUnique(alias, editingUserId.value)) {
    aliasError.value = t('requestLog.aliasNotUnique')
  } else {
    aliasError.value = ''
  }
}

const saveAlias = async () => {
  const alias = aliasInput.value.trim()
  if (!alias || aliasError.value) return

  try {
    await api.setUserAlias(editingUserId.value, alias)
    userAliases.value[editingUserId.value] = alias
    showAliasDialog.value = false
  } catch (e: unknown) {
    const error = e as Error
    if (error.message?.includes('already in use')) {
      aliasError.value = t('requestLog.aliasNotUnique')
    } else {
      console.error('Failed to save alias:', e)
      aliasError.value = 'Failed to save alias'
    }
  }
}

const removeAlias = async (userId?: string) => {
  const targetId = userId || editingUserId.value
  try {
    await api.deleteUserAlias(targetId)
    delete userAliases.value[targetId]
    if (!userId) {
      showAliasDialog.value = false
    }
  } catch (e) {
    console.error('Failed to remove alias:', e)
  }
}

	onMounted(() => {
	  loadColumnWidths()
	  loadColumnVisibility()
	  loadStackingPrefs()
	  loadSummaryColumnWidths()
	  loadActiveSessionColumnWidths()
	  loadPanelWidths()
	  loadDateFilterCollapsed()
	  // Apply collapsed state after loading panel widths
	  if (isDateFilterCollapsed.value) {
	    // Use persisted expanded widths if available, otherwise use loaded panel widths
	    if (!expandedPanelWidths.value) {
	      expandedPanelWidths.value = { ...panelWidths.value }
	    }
	    const collapsedWidth = 1
	    const freedWidth = expandedPanelWidths.value.dateFilter - collapsedWidth
	    const totalOther = expandedPanelWidths.value.summary + expandedPanelWidths.value.reserved
	    if (totalOther > 0) {
	      panelWidths.value.summary = expandedPanelWidths.value.summary + freedWidth * (expandedPanelWidths.value.summary / totalOther)
	      panelWidths.value.reserved = expandedPanelWidths.value.reserved + freedWidth * (expandedPanelWidths.value.reserved / totalOther)
	    } else {
	      panelWidths.value.summary = 49
	      panelWidths.value.reserved = 49
	    }
	    panelWidths.value.dateFilter = collapsedWidth
	  }
	  loadUserAliases()
	  loadAPIKeys()
	  loadSSEEnabled()
	  loadAutoRefreshInterval()
	  emit('dateRangeChange', getDateRange())
	  refreshLogs()
	  if (sseEnabled.value) {
	    // Try SSE first, fall back to polling if it fails
	    connectSSE()
	    // Start polling as backup only if SSE doesn't connect quickly.
	    autoRefreshEnabled.value = false
	    stopAutoRefresh()
	    schedulePollingFallback()
	  } else {
	    // SSE disabled, use polling only
	    startAutoRefresh()
	  }
	  document.addEventListener('visibilitychange', handleVisibilityChange)
	})

onUnmounted(() => {
  stopAutoRefresh()
  stopSSEStatsRefresh()
  disconnectSSE()
  cancelPollingFallback()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})

// Let parent components (e.g. charts) follow the same "effective polling" state.
watch(autoRefreshEnabled, (enabled) => {
  emit('pollingEnabledChange', enabled)
}, { immediate: true })

	const startAutoRefresh = () => {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
  }
  if (autoRefreshEnabled.value) {
    autoRefreshTimer = setInterval(() => {
      if (document.visibilityState === 'hidden') return
      if (autoRefreshEnabled.value) {
        silentRefresh()
      }
    }, autoRefreshInterval.value * 1000)
  }
}

const stopAutoRefresh = () => {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}

	const handleVisibilityChange = () => {
	  if (document.visibilityState === 'hidden') {
	    stopAutoRefresh()
	    cancelPollingFallback()
	    // Disconnect SSE to avoid keeping a live connection open when user isn't watching.
	    // We'll reconnect when the tab becomes visible again.
	    if (sseEnabled.value && sseConnectionState.value !== 'disconnected') {
	      ssePausedByVisibility = true
	      disconnectSSE()
	      sseConnectionState.value = 'disconnected'
	      stopSSEStatsRefresh()
	    }
	    return
	  }

	  // Visible again: follow the same effective rules as the SSE/polling state machine.
	  if (sseEnabled.value) {
	    // Prefer SSE, only poll if SSE stays down past the grace period.
	    stopAutoRefresh()
	    cancelPollingFallback()
	    if (ssePausedByVisibility) {
	      ssePausedByVisibility = false
	      connectSSE()
	    }
	    if (sseConnectionState.value !== 'connected') {
	      schedulePollingFallback()
	    }
	    return
	  }

  // SSE disabled: resume polling only if user left auto-refresh enabled.
  if (autoRefreshEnabled.value) {
    startAutoRefresh()
  }
}

const toggleAutoRefresh = () => {
  autoRefreshEnabled.value = !autoRefreshEnabled.value
  if (autoRefreshEnabled.value) {
    // Disable SSE when enabling auto-refresh (mutual exclusivity)
    if (sseEnabled.value) {
      sseEnabled.value = false
      saveSSEEnabled()
      disconnectSSE()
      sseConnectionState.value = 'disconnected'
      stopSSEStatsRefresh()
    }
    startAutoRefresh()
  } else {
    stopAutoRefresh()
  }
}

// Stats-only refresh for SSE mode (summary tables)
const refreshStatsOnly = async () => {
  try {
    const { from, to } = getDateRange()
    const [statsRes, activeSessionsRes] = await Promise.all([
      api.getRequestLogStats({ from, to }),
      api.getActiveSessions().catch(() => [] as ActiveSession[])
    ])

    // Detect updated groups in stats
    const newUpdatedModels = detectUpdatedGroups(stats.value?.byModel, statsRes?.byModel)
    const newUpdatedProviders = detectUpdatedGroups(stats.value?.byProvider, statsRes?.byProvider)
    const newUpdatedClients = detectUpdatedGroups(stats.value?.byClient, statsRes?.byClient)
    const newUpdatedSessions = detectUpdatedGroups(stats.value?.bySession, statsRes?.bySession)
    const newUpdatedAPIKeys = detectUpdatedGroups(stats.value?.byApiKey, statsRes?.byApiKey)

    // Detect active session changes before updating
    const newUpdatedActive = detectActiveSessionChanges(activeSessions.value, activeSessionsRes)

    stats.value = statsRes
    activeSessions.value = activeSessionsRes

    // Flash updated summary groups
    if (newUpdatedModels.size > 0) {
      updatedModels.value = newUpdatedModels
      setTimeout(() => { updatedModels.value = new Set() }, 1000)
    }
    if (newUpdatedProviders.size > 0) {
      updatedProviders.value = newUpdatedProviders
      setTimeout(() => { updatedProviders.value = new Set() }, 1000)
    }
    if (newUpdatedClients.size > 0) {
      updatedClients.value = newUpdatedClients
      setTimeout(() => { updatedClients.value = new Set() }, 1000)
    }
    if (newUpdatedSessions.size > 0) {
      updatedSessions.value = newUpdatedSessions
      setTimeout(() => { updatedSessions.value = new Set() }, 1000)
    }
    if (newUpdatedAPIKeys.size > 0) {
      updatedAPIKeys.value = newUpdatedAPIKeys
      setTimeout(() => { updatedAPIKeys.value = new Set() }, 1000)
    }
    // Flash updated active sessions
    if (newUpdatedActive.size > 0) {
      updatedActiveSessions.value = newUpdatedActive
      setTimeout(() => { updatedActiveSessions.value = new Set() }, 1000)
    }
  } catch (e) {
    console.warn('Failed to refresh stats:', e)
  }
}

const startSSEStatsRefresh = () => {
  if (sseStatsRefreshTimer) {
    clearInterval(sseStatsRefreshTimer)
  }
  sseStatsRefreshTimer = setInterval(refreshStatsOnly, SSE_STATS_REFRESH_INTERVAL)
}

const stopSSEStatsRefresh = () => {
  if (sseStatsRefreshTimer) {
    clearInterval(sseStatsRefreshTimer)
    sseStatsRefreshTimer = null
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

    // Fetch active sessions separately to avoid breaking main data on error
    let activeSessionsRes: ActiveSession[] = []
    try {
      activeSessionsRes = await api.getActiveSessions()
    } catch (e) {
      console.warn('Failed to fetch active sessions:', e)
    }

    // When SSE is connected, skip flash animations.
    // SSE event handlers already flash rows/groups for real changes.
    // The silentRefresh here only syncs authoritative data from the server
    // (e.g. after reconnect), so flashing would be misleading.
    const skipFlash = isSSEConnected.value

    if (!skipFlash) {
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
      const newUpdatedClients = detectUpdatedGroups(stats.value?.byClient, statsRes?.byClient)
      const newUpdatedSessions = detectUpdatedGroups(stats.value?.bySession, statsRes?.bySession)
      const newUpdatedAPIKeys = detectUpdatedGroups(stats.value?.byApiKey, statsRes?.byApiKey)

      if (newUpdatedIds.size > 0) {
        updatedIds.value = newUpdatedIds
        setTimeout(() => { updatedIds.value = new Set() }, 1000)
      }
      if (newUpdatedModels.size > 0) {
        updatedModels.value = newUpdatedModels
        setTimeout(() => { updatedModels.value = new Set() }, 1000)
      }
      if (newUpdatedProviders.size > 0) {
        updatedProviders.value = newUpdatedProviders
        setTimeout(() => { updatedProviders.value = new Set() }, 1000)
      }
      if (newUpdatedClients.size > 0) {
        updatedClients.value = newUpdatedClients
        setTimeout(() => { updatedClients.value = new Set() }, 1000)
      }
      if (newUpdatedSessions.size > 0) {
        updatedSessions.value = newUpdatedSessions
        setTimeout(() => { updatedSessions.value = new Set() }, 1000)
      }
      if (newUpdatedAPIKeys.size > 0) {
        updatedAPIKeys.value = newUpdatedAPIKeys
        setTimeout(() => { updatedAPIKeys.value = new Set() }, 1000)
      }

      // Detect updated active sessions
      const newUpdatedActive = detectActiveSessionChanges(activeSessions.value, activeSessionsRes)
      if (newUpdatedActive.size > 0) {
        updatedActiveSessions.value = newUpdatedActive
        setTimeout(() => { updatedActiveSessions.value = new Set() }, 1000)
      }
    }

    // Always update the data (authoritative sync from server)
    logs.value = logsRes.requests || []
    total.value = logsRes.total
    hasMore.value = logsRes.hasMore
    stats.value = statsRes
    activeSessions.value = activeSessionsRes || []
  } catch (error) {
    console.error('Failed to refresh logs:', error)
  }
}
</script>

<style scoped>
.request-log-container {
  padding: 0;
}

/* Modal card structure */
.modal-card {
  display: flex;
  flex-direction: column;
  max-height: 85vh;
}

.modal-header {
  flex-shrink: 0;
  border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.12);
}

.modal-content {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
  padding: 16px;
}

/* iOS-style action buttons */
.modal-action-btn {
  width: 36px;
  height: 36px;
}

/* Live indicator pulse animation */
.live-indicator {
  font-weight: 600;
}

.clickable-chip {
  cursor: pointer;
  transition: opacity 0.2s;
}

.clickable-chip:hover {
  opacity: 0.8;
}

.pulse-icon {
  animation: pulse 1.5s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

/* Top panels container with flex layout */
.top-panels-container {
  display: flex;
  align-items: stretch;
  gap: 0;
  width: 100%;
}

.panel-wrapper {
  flex-shrink: 1;
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

.panel-splitter-spacer {
  width: 12px;
  flex-shrink: 0;
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

/* Summary header with group by select - moved to end of file for theme styling */

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

/* Sortable header styles */
.sortable-header {
  cursor: pointer;
  user-select: none;
}

.sortable-header:hover {
  background: rgba(var(--v-theme-primary), 0.08);
}

.sortable-header .header-content {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

/* Hit rate value with tooltip indicator */
.hit-rate-value {
  cursor: help;
  border-bottom: 1px dotted currentColor;
}

.sortable-header .sort-icon {
  opacity: 0.7;
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
  position: relative;
}

/* Date filter panel collapse/expand */
.date-filter-panel {
  transition: width 0.2s ease-out;
}

.date-filter-panel.collapsed {
  flex-shrink: 0;
  min-width: 40px;
}

.date-filter-collapsed {
  flex: 1;
  height: 100%;
}

.date-filter-collapse-btn {
  position: absolute;
  top: 2px;
  right: 2px;
  opacity: 0.5;
  z-index: 1;
}

.date-filter-collapse-btn:hover {
  opacity: 1;
}

.collapse-toggle-btn {
  opacity: 0.6;
}

.collapse-toggle-btn:hover {
  opacity: 1;
  background: rgba(var(--v-theme-primary), 0.1);
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

/* Override Vuetify table cell padding for tighter layout */
.log-table :deep(.v-table__wrapper > table > thead > tr > th),
.log-table :deep(.v-table__wrapper > table > tbody > tr > td) {
  padding: 0 4px;
  vertical-align: middle !important;
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

/* Clickable row style */
.log-table :deep(tr.clickable-row) {
  cursor: pointer;
  transition: background-color 0.15s ease;
}

.log-table :deep(tr.clickable-row:hover) {
  background-color: rgba(var(--v-theme-primary), 0.08) !important;
}

/* Summary table row flash animation */
.summary-table tr.summary-row-flash,
.summary-table-custom tr.summary-row-flash {
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
  display: block;
  width: 100%;
}

.resize-handle {
  position: absolute;
  top: -8px;
  right: -4px;
  bottom: -8px;
  width: 12px;
  cursor: col-resize;
  user-select: none;
  z-index: 200;
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
  width: 16px;
  right: -6px;
}

.resize-handle:hover::before {
  height: 24px;
}

.resizable-table :deep(thead th) {
  position: relative !important;
  overflow: visible !important;
}

/* Ensure resize handles are above adjacent cells */
.resizable-table :deep(thead th:hover) {
  z-index: 20 !important;
}

.resizable-table :deep(thead) {
  position: relative;
  z-index: 10;
}

/* Token column styles */
.token-text {
  font-family: 'Courier New', monospace;
  font-weight: 500;
  font-size: 0.85rem;
  white-space: pre;
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

/* Stacked tokens display - using inline-grid for alignment */
.tokens-stacked {
  display: inline-grid;
  grid-template-columns: auto auto;
  gap: 2px 6px;
  line-height: 1.3;
}

.token-cell {
  display: inline-flex;
  min-width: 58px;
  font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', Consolas, 'Courier New', monospace;
  font-size: 0.75rem;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
}

.token-num {
  flex: 1;
  text-align: right;
  letter-spacing: -0.02em;
}

.token-sym {
  width: 12px;
  text-align: center;
  flex-shrink: 0;
}

.cache-cell {
  font-size: 0.7rem;
  opacity: 0.85;
}

/* Tokens tooltip styles */
.tokens-tooltip {
  font-size: 0.85rem;
  line-height: 1.5;
}

.tokens-tooltip .tooltip-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.tokens-tooltip .tooltip-label {
  color: rgba(255, 255, 255, 0.7);
}

/* Error tooltip styles */
.error-tooltip {
  font-size: 0.85rem;
  line-height: 1.4;
  color: #fff;
}

.failover-info-line {
  margin-bottom: 8px;
  color: #fff;
}

.failover-info-text {
  font-family: 'Courier New', monospace;
  font-size: 0.85rem;
  color: #90caf9;
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
  font-weight: 600;
  text-transform: uppercase;
}

.reasoning-effort-tooltip .effort-value.effort-low {
  color: #4CAF50;
}

.reasoning-effort-tooltip .effort-value.effort-medium {
  color: #64B5F6;
}

.reasoning-effort-tooltip .effort-value.effort-high {
  color: #FFB74D;
}

.reasoning-effort-tooltip .effort-value.effort-xhigh {
  color: #EF5350;
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

/* =========================================
   Global Tooltip Theme Styles
   Note: Tooltip styles are in src/assets/style.css
   because tooltips are teleported to body.
   ========================================= */

/* Summary header alignment - ensure consistent height across all panels */
.summary-header {
  height: 36px;
  min-height: 36px;
  max-height: 36px;
  border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.1);
  box-sizing: border-box;
}

/* Group by toggle styling */
.group-by-toggle {
  height: 28px;
}

.group-by-toggle .v-btn {
  height: 28px !important;
  min-width: 36px !important;
}

/* Duration column - right alignment and color coding */
.duration-text {
  font-family: 'Courier New', monospace;
  font-weight: 500;
  font-size: 0.85rem;
}

.duration-success {
  color: rgb(var(--v-theme-success));
}

.duration-warning {
  color: rgb(var(--v-theme-warning));
}

.duration-error {
  color: rgb(var(--v-theme-error));
}

/* Clickable ID style */
.clickable-id {
  cursor: pointer;
  transition: all 0.15s ease;
}

.clickable-id:hover {
  color: rgb(var(--v-theme-primary));
  text-decoration: underline;
}

/* Alias tooltip labels */
.alias-label,
.id-label {
  color: rgba(255, 255, 255, 0.7);
  margin-right: 4px;
}

.alias-tooltip-row {
  margin-bottom: 4px;
}

/* User ID display in dialog */
.user-id-display {
  background: rgba(var(--v-theme-on-surface), 0.05);
  padding: 8px 12px;
  border-radius: 4px;
  word-break: break-all;
  border: 1px solid rgba(var(--v-theme-on-surface), 0.1);
}

/* Alias list in settings */
.alias-list {
  max-height: 200px;
  overflow-y: auto;
  border: 1px solid rgba(var(--v-theme-on-surface), 0.1);
  border-radius: 4px;
}

.alias-item {
  padding: 8px 12px;
  border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.1);
}

.alias-item:last-child {
  border-bottom: none;
}

.alias-name {
  min-width: 80px;
  color: rgb(var(--v-theme-primary));
}

.alias-userid {
  font-size: 0.8rem;
  color: rgba(var(--v-theme-on-surface), 0.7);
}

/* Column visibility grid */
.column-visibility-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 0;
}

.column-visibility-checkbox {
  margin: 0;
}

.column-visibility-checkbox :deep(.v-label) {
  font-size: 0.875rem;
}

/* =========================================
   Stacked Column Styles
   ========================================= */

/* Stacked column cells */
.stacked-cell {
  display: flex;
  flex-direction: column;
  gap: 1px;
  line-height: 1.3;
  padding: 2px 0;
}

.stacked-secondary {
  font-size: 0.7rem;
  opacity: 0.7;
  line-height: 1.2;
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Stacked tooltip */
.stacked-tooltip {
  font-size: 0.85rem;
  line-height: 1.5;
}

.stacked-tooltip div {
  margin-bottom: 2px;
}

.stacked-tooltip strong {
  color: rgba(255, 255, 255, 0.7);
  margin-right: 4px;
}

/* Settings stacking rows */
.stacking-options {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.stacking-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px;
  border-radius: 6px;
  transition: background-color 200ms;
}

.stacking-row:hover {
  background-color: rgba(var(--v-theme-on-surface), 0.04);
}

.stacking-label {
  font-weight: 500;
  min-width: 140px;
}

.stacking-toggle {
  height: 28px;
}

.stacking-toggle .v-btn {
  font-size: 0.7rem !important;
  padding: 0 8px !important;
}

</style>
