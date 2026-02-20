<template>
  <v-card elevation="2" rounded="lg" class="channel-orchestration">
    <!-- 调度器统计信息 -->
    <v-card-title class="d-flex align-center justify-space-between py-3 px-0">
      <div class="d-flex align-center">
        <v-icon class="mr-2" color="primary">mdi-swap-vertical-bold</v-icon>
        <span class="text-h6">{{ t('orchestration.title') }}</span>
        <v-chip v-if="isMultiChannelMode" size="small" color="success" variant="tonal" class="ml-3">
          {{ t('orchestration.multiChannelMode') }}
        </v-chip>
        <v-chip v-else size="small" color="warning" variant="tonal" class="ml-3"> {{ t('orchestration.singleChannelMode') }} </v-chip>
      </div>
      <div class="d-flex align-center ga-2">
        <v-progress-circular v-if="isLoadingMetrics" indeterminate size="16" width="2" color="primary" />
      </div>
    </v-card-title>

    <v-divider />

    <!-- 故障转移序列 (active + suspended) -->
    <div class="pt-3 pb-2">
      <div class="d-flex align-center justify-space-between mb-2">
        <div class="text-subtitle-2 text-medium-emphasis d-flex align-center">
          <v-icon size="small" class="mr-1" color="success">mdi-play-circle</v-icon>
          {{ t('orchestration.failoverSequence') }}
          <v-chip size="x-small" class="ml-2">{{ activeChannels.length }}</v-chip>
        </div>
        <div class="d-flex align-center ga-2">
          <span class="text-caption text-medium-emphasis">{{ t('orchestration.dragToReorder') }}</span>
          <v-progress-circular v-if="isSavingOrder" indeterminate size="16" width="2" color="primary" />
        </div>
      </div>

      <!-- 拖拽列表 -->
      <draggable
        v-model="activeChannels"
        item-key="index"
        handle=".drag-handle"
        ghost-class="ghost"
        @change="onDragChange"
        class="channel-list"
      >
        <template #item="{ element, index }">
          <div class="channel-item-wrapper">
            <div class="channel-row" :class="{ 'is-suspended': element.status === 'suspended', 'has-quota-column': true }">
            <!-- 拖拽手柄 -->
            <div class="drag-handle">
              <v-icon size="small" color="grey">mdi-drag-vertical</v-icon>
            </div>

            <!-- 优先级序号 -->
            <div class="priority-number">
              <span class="text-caption font-weight-bold">{{ index + 1 }}</span>
            </div>

            <!-- 状态指示器 -->
            <ChannelStatusBadge :status="element.status || 'active'" :metrics="getChannelMetrics(element.index)" />

            <!-- 渠道名称和描述 -->
            <div class="channel-name">
              <span class="font-weight-medium">{{ element.name }}</span>
              <!-- 官网链接按钮 -->
              <v-btn
                :href="getWebsiteUrl(element)"
                target="_blank"
                rel="noopener"
                icon
                size="x-small"
                variant="text"
                color="primary"
                class="ml-1"
                :title="t('orchestration.openWebsite')"
              >
                <v-icon size="14">mdi-open-in-new</v-icon>
              </v-btn>
              <v-icon v-if="element.serviceType === 'claude' || element.serviceType === 'composite'" size="14" icon="custom:claude" class="ml-2" />
              <v-icon v-else-if="element.serviceType === 'gemini'" size="14" icon="custom:gemini" class="ml-2" />
              <v-icon v-else size="14" icon="custom:codex" class="ml-2" />
              <span class="text-caption text-medium-emphasis ml-1">{{ element.serviceType }}</span>
              <span v-if="element.description" class="text-caption text-disabled ml-3">{{ element.description }}</span>
            </div>

            <!-- 指标显示 -->
            <div class="channel-metrics">
              <div class="recent-calls-display">
                <div class="recent-calls-blocks">
                  <template v-for="(call, callIndex) in getRecentCalls(element.index)" :key="`${element.index}-${callIndex}`">
                    <v-tooltip location="top" :open-delay="120">
                      <template #activator="{ props: tooltipProps }">
                        <span
                          v-bind="tooltipProps"
                          class="recent-call-block"
                          :class="{
                            'is-success': call.state === 'success',
                            'is-failure': call.state === 'failure',
                            'is-unused': call.state === 'unused'
                          }"
                        />
                      </template>
                      <div class="text-caption recent-call-tooltip">
                        <div
                          v-for="(line, lineIndex) in getRecentCallTooltipLines(call, element.serviceType === 'composite')"
                          :key="`${element.index}-${callIndex}-${lineIndex}`"
                        >
                          {{ line }}
                        </div>
                      </div>
                    </v-tooltip>
                  </template>
                </div>
                <span class="recent-calls-rate">{{ getRecentSuccessRate(element.index) }}</span>
              </div>
            </div>

            <!-- Inline Quota Bar (usage quota or OAuth quota) -->
            <div class="channel-quota">
              <!-- User-configured usage quota (requests/credit) -->
              <template v-if="hasUsageQuota(element)">
                <v-menu location="top" :close-on-content-click="false">
                  <template #activator="{ props: menuProps }">
                    <v-tooltip location="top" :open-delay="300">
                      <template #activator="{ props: tooltipProps }">
                        <div v-bind="{ ...menuProps, ...tooltipProps }" class="quota-bar-container">
                          <template v-if="getUsageQuota(element.index)">
                            <div class="quota-bar-wrapper">
                              <div
                                class="quota-bar"
                                :style="{
                                  width: `${getUsageQuota(element.index)!.remainingPercent}%`,
                                  backgroundColor: getUsageQuotaBarColor(getUsageQuota(element.index)!.remainingPercent)
                                }"
                              />
                            </div>
                            <span class="quota-text">{{ getUsageQuota(element.index)!.remainingPercent.toFixed(0) }}%</span>
                          </template>
                          <span v-else class="text-caption text-medium-emphasis">--</span>
                        </div>
                      </template>
                      <!-- Hover tooltip (no reset button) -->
                      <div class="quota-tooltip">
                        <template v-if="getUsageQuota(element.index)">
                          <div class="text-caption font-weight-bold mb-1">
                            {{ element.quotaType === 'credit' ? t('quota.creditQuota') : t('quota.requestQuota') }}
                          </div>
                          <div class="quota-tooltip-row">
                            <span>{{ t('quota.used') }}:</span>
                            <span>{{ formatQuotaValue(getUsageQuota(element.index)!.used, element.quotaType || '') }}</span>
                          </div>
                          <div class="quota-tooltip-row">
                            <span>{{ t('quota.remaining') }}:</span>
                            <span>{{ formatQuotaValue(getUsageQuota(element.index)!.remaining, element.quotaType || '') }} ({{ getUsageQuota(element.index)!.remainingPercent.toFixed(0) }}%)</span>
                          </div>
                          <div class="quota-tooltip-row">
                            <span>{{ t('quota.limit') }}:</span>
                            <span>{{ formatQuotaValue(getUsageQuota(element.index)!.limit, element.quotaType || '') }}</span>
                          </div>
                          <div v-if="getUsageQuota(element.index)!.nextResetAt" class="text-caption text-medium-emphasis mt-1">
                            {{ t('quota.nextReset') }}: {{ new Date(getUsageQuota(element.index)!.nextResetAt!).toLocaleString() }}
                          </div>
                          <div v-if="element.quotaModels && element.quotaModels.length > 0" class="text-caption text-medium-emphasis mt-1">
                            {{ t('quota.quotaModelsApplied') }}: {{ element.quotaModels.join(', ') }}
                          </div>
                        </template>
                      </div>
                    </v-tooltip>
                  </template>
                  <!-- Click menu (with reset button) -->
                  <v-card min-width="200" class="pa-3">
                    <template v-if="getUsageQuota(element.index)">
                      <div class="text-caption font-weight-bold mb-1">
                        {{ element.quotaType === 'credit' ? t('quota.creditQuota') : t('quota.requestQuota') }}
                      </div>
                      <div class="quota-tooltip-row">
                        <span>{{ t('quota.used') }}:</span>
                        <span>{{ formatQuotaValue(getUsageQuota(element.index)!.used, element.quotaType || '') }}</span>
                      </div>
                      <div class="quota-tooltip-row">
                        <span>{{ t('quota.remaining') }}:</span>
                        <span>{{ formatQuotaValue(getUsageQuota(element.index)!.remaining, element.quotaType || '') }} ({{ getUsageQuota(element.index)!.remainingPercent.toFixed(0) }}%)</span>
                      </div>
                      <div class="quota-tooltip-row">
                        <span>{{ t('quota.limit') }}:</span>
                        <span>{{ formatQuotaValue(getUsageQuota(element.index)!.limit, element.quotaType || '') }}</span>
                      </div>
                      <div v-if="getUsageQuota(element.index)!.nextResetAt" class="text-caption text-medium-emphasis mt-1">
                        {{ t('quota.nextReset') }}: {{ new Date(getUsageQuota(element.index)!.nextResetAt!).toLocaleString() }}
                      </div>
                      <div v-if="element.quotaModels && element.quotaModels.length > 0" class="text-caption text-medium-emphasis mt-1">
                        {{ t('quota.quotaModelsApplied') }}: {{ element.quotaModels.join(', ') }}
                      </div>
                      <v-btn
                        size="x-small"
                        variant="tonal"
                        color="warning"
                        class="mt-2"
                        @click="resetUsageQuota(element.index)"
                      >
                        <v-icon start size="small">mdi-refresh</v-icon>
                        {{ t('quota.manualReset') }}
                      </v-btn>
                      <!-- Activate/Suspend button -->
                      <v-btn
                        v-if="element.status === 'suspended'"
                        size="x-small"
                        variant="tonal"
                        color="success"
                        class="mt-2 ml-2"
                        @click="resumeChannel(element.index)"
                      >
                        <v-icon start size="small">mdi-play-circle</v-icon>
                        {{ t('quota.activate') }}
                      </v-btn>
                      <v-btn
                        v-else
                        size="x-small"
                        variant="tonal"
                        color="warning"
                        class="mt-2 ml-2"
                        @click="setChannelStatus(element.index, 'suspended')"
                      >
                        <v-icon start size="small">mdi-pause-circle</v-icon>
                        {{ t('quota.suspend') }}
                      </v-btn>
                    </template>
                    <template v-else>
                      <div class="text-caption">{{ t('quota.noData') }}</div>
                    </template>
                  </v-card>
                </v-menu>
              </template>
              <!-- OAuth quota for openai-oauth channels in responses tab -->
              <template v-else-if="element.serviceType === 'openai-oauth' && channelType === 'responses'">
                <v-tooltip location="top" :open-delay="200">
                  <template #activator="{ props: tooltipProps }">
                    <div v-bind="tooltipProps" class="oauth-quota-dual-bar" @click="openOAuthStatus(element)">
                      <template v-if="getChannelQuota(element)?.codex_quota">
                        <!-- 5h quota bar -->
                        <div class="oauth-quota-row">
                          <span class="oauth-quota-label">5h</span>
                          <div class="quota-bar-wrapper">
                            <div
                              class="quota-bar"
                              :style="{
                                width: `${getOAuthWindowRemainingPercent(element, 'primary')}%`,
                                backgroundColor: getQuotaBarColor(getOAuthWindowUsedPercent(element, 'primary'))
                              }"
                            />
                          </div>
                          <span class="quota-text">{{ getOAuthWindowRemainingPercent(element, 'primary') }}%</span>
                        </div>
                        <!-- 7d quota bar -->
                        <div class="oauth-quota-row">
                          <span class="oauth-quota-label">7d</span>
                          <div class="quota-bar-wrapper">
                            <div
                              class="quota-bar"
                              :style="{
                                width: `${getOAuthWindowRemainingPercent(element, 'secondary')}%`,
                                backgroundColor: getQuotaBarColor(getOAuthWindowUsedPercent(element, 'secondary'))
                              }"
                            />
                          </div>
                          <span class="quota-text">{{ getOAuthWindowRemainingPercent(element, 'secondary') }}%</span>
                        </div>
                      </template>
                      <span v-else class="text-caption text-medium-emphasis">--</span>
                    </div>
                  </template>
                  <div class="quota-tooltip">
                    <template v-if="getChannelQuota(element)?.codex_quota">
                      <div class="text-caption font-weight-bold mb-1">{{ t('oauth.usageQuota') }}</div>
                      <div class="quota-tooltip-row">
                        <span>{{ t('oauth.primaryWindow') }}:</span>
                        <span>{{ getOAuthWindowRemainingPercent(element, 'primary') }}% {{ t('orchestration.quotaRemaining') }}</span>
                      </div>
                      <div class="quota-tooltip-row">
                        <span>{{ t('oauth.secondaryWindow') }}:</span>
                        <span>{{ getOAuthWindowRemainingPercent(element, 'secondary') }}% {{ t('orchestration.quotaRemaining') }}</span>
                      </div>
                      <div class="text-caption text-medium-emphasis mt-1">{{ t('orchestration.clickForDetails') }}</div>
                    </template>
                    <template v-else>
                      <div class="text-caption">{{ t('oauth.noQuotaData') }}</div>
                    </template>
                  </div>
                </v-tooltip>
              </template>
              <!-- No quota configured -->
              <span v-else class="text-caption text-medium-emphasis">--</span>
            </div>

            <!-- API密钥数量 -->
            <div class="channel-keys">
              <v-chip size="x-small" variant="outlined" class="keys-chip" @click="$emit('edit', element)">
                <v-icon start size="x-small">mdi-key</v-icon>
                {{ element.apiKeyCount ?? element.apiKeys?.length ?? 0 }}
              </v-chip>
            </div>

            <!-- 操作按钮 -->
            <div class="channel-actions">
              <!-- 图表展开按钮 -->
              <v-btn
                icon
                size="small"
                variant="text"
                :color="expandedChartChannelId === element.index ? 'primary' : 'default'"
                @click="toggleChannelChart(element.index)"
                :title="t('chart.view.traffic')"
                class="chart-toggle-btn"
              >
                <v-icon size="18">{{ expandedChartChannelId === element.index ? 'mdi-chart-line' : 'mdi-chart-line-variant' }}</v-icon>
              </v-btn>

              <!-- 内联操作按钮（宽屏显示） -->
              <div class="inline-actions">
                <!-- 编辑 -->
                <v-btn
                  icon
                  size="small"
                  variant="text"
                  color="primary"
                  @click="$emit('edit', element)"
                  :title="t('common.edit')"
                >
                  <v-icon size="18">mdi-pencil</v-icon>
                </v-btn>

                <!-- 暂停/恢复 -->
                <v-btn
                  v-if="element.status === 'suspended'"
                  icon
                  size="small"
                  variant="text"
                  color="success"
                  @click="resumeChannel(element.index)"
                  :title="t('orchestration.resume')"
                >
                  <v-icon size="18">mdi-play-circle</v-icon>
                </v-btn>
                <v-btn
                  v-else
                  icon
                  size="small"
                  variant="text"
                  color="warning"
                  @click="setChannelStatus(element.index, 'suspended')"
                  :title="t('orchestration.pause')"
                >
                  <v-icon size="18">mdi-pause-circle</v-icon>
                </v-btn>

                <!-- 移至备用池 -->
                <v-btn
                  icon
                  size="small"
                  variant="text"
                  color="error"
                  @click="setChannelStatus(element.index, 'disabled')"
                  :title="t('orchestration.moveToPool')"
                >
                  <v-icon size="18">mdi-stop-circle</v-icon>
                </v-btn>
              </div>

              <!-- 更多操作菜单（宽屏：只有抢优先级和删除；窄屏：所有操作） -->
              <v-menu>
                <template #activator="{ props }">
                  <v-btn icon size="small" variant="text" v-bind="props">
                    <v-icon size="18">mdi-dots-vertical</v-icon>
                  </v-btn>
                </template>
                <v-list density="compact">
                  <!-- 窄屏时显示的额外选项 -->
                  <v-list-item class="menu-item-narrow" @click="$emit('edit', element)">
                    <template #prepend>
                      <v-icon size="small">mdi-pencil</v-icon>
                    </template>
                    <v-list-item-title>{{ t('common.edit') }}</v-list-item-title>
                  </v-list-item>
                  <!-- OAuth Status (only for openai-oauth channels in responses mode) -->
                  <v-list-item
                    v-if="element.serviceType === 'openai-oauth' && channelType === 'responses'"
                    @click="openOAuthStatus(element)"
                  >
                    <template #prepend>
                      <v-icon size="small" color="info">mdi-shield-account</v-icon>
                    </template>
                    <v-list-item-title>{{ t('oauth.viewStatus') }}</v-list-item-title>
                  </v-list-item>
                  <v-list-item
                    v-if="element.status === 'suspended'"
                    class="menu-item-narrow"
                    @click="resumeChannel(element.index)"
                  >
                    <template #prepend>
                      <v-icon size="small" color="success">mdi-play-circle</v-icon>
                    </template>
                    <v-list-item-title>{{ t('orchestration.resumeReset') }}</v-list-item-title>
                  </v-list-item>
                  <v-list-item
                    v-if="element.status !== 'suspended'"
                    class="menu-item-narrow"
                    @click="setChannelStatus(element.index, 'suspended')"
                  >
                    <template #prepend>
                      <v-icon size="small" color="warning">mdi-pause-circle</v-icon>
                    </template>
                    <v-list-item-title>{{ t('orchestration.pause') }}</v-list-item-title>
                  </v-list-item>
                  <v-list-item class="menu-item-narrow" @click="setChannelStatus(element.index, 'disabled')">
                    <template #prepend>
                      <v-icon size="small" color="error">mdi-stop-circle</v-icon>
                    </template>
                    <v-list-item-title>{{ t('orchestration.moveToPool') }}</v-list-item-title>
                  </v-list-item>
                  <v-divider class="menu-item-narrow" />
                  <!-- 始终显示的选项 -->
                  <v-list-item @click="setPromotion(element)">
                    <template #prepend>
                      <v-icon size="small" color="info">mdi-rocket-launch</v-icon>
                    </template>
                    <v-list-item-title>{{ t('orchestration.boostPriority') }}</v-list-item-title>
                  </v-list-item>
                  <v-divider />
                  <v-list-item @click="handleDeleteChannel(element)" :disabled="!canDeleteChannel(element)">
                    <template #prepend>
                      <v-icon size="small" :color="canDeleteChannel(element) ? 'error' : 'grey'">mdi-delete</v-icon>
                    </template>
                    <v-list-item-title>
                      {{ t('common.delete') }}
                      <span v-if="!canDeleteChannel(element)" class="text-caption text-disabled ml-1">
                        {{ t('orchestration.keepAtLeastOne') }}
                      </span>
                    </v-list-item-title>
                  </v-list-item>
                </v-list>
              </v-menu>
            </div>
          </div>
          <!-- 渠道统计图表 - 展开时显示 -->
          <ChannelStatsChart
            v-if="expandedChartChannelId === element.index"
            :channel-id="element.index"
            :channel-type="channelType"
            @close="expandedChartChannelId = null"
          />
          </div>
        </template>
      </draggable>

      <!-- 空状态 -->
      <div v-if="activeChannels.length === 0" class="text-center py-6 text-medium-emphasis">
        <v-icon size="48" color="grey-lighten-1">mdi-playlist-remove</v-icon>
        <div class="mt-2">{{ t('orchestration.noActiveChannels') }}</div>
        <div class="text-caption">{{ t('orchestration.enableFromPool') }}</div>
      </div>
    </div>

    <v-divider class="my-2" />

    <!-- 组合渠道池 (disabled composite channels) -->
    <div v-if="disabledCompositeChannels.length > 0" class="pt-2 pb-3">
      <div class="inactive-pool-header">
        <div class="text-subtitle-2 text-medium-emphasis d-flex align-center">
          <v-icon size="small" class="mr-1" color="purple">mdi-layers-triple</v-icon>
          {{ t('orchestration.compositePool') }}
          <v-chip size="x-small" class="ml-2" color="purple" variant="tonal">{{ disabledCompositeChannels.length }}</v-chip>
        </div>
        <span class="text-caption text-medium-emphasis">{{ t('orchestration.compositePoolDesc') }}</span>
      </div>

      <div class="inactive-pool composite-pool">
        <div v-for="channel in disabledCompositeChannels" :key="channel.index" class="inactive-channel-row composite-channel-row">
          <!-- 渠道信息 -->
          <div class="channel-info">
            <div class="channel-info-main">
              <span class="font-weight-medium">{{ channel.name }}</span>
            </div>
            <div v-if="channel.description" class="channel-info-desc text-caption text-disabled">
              {{ channel.description }}
            </div>
          </div>

          <!-- 操作按钮 -->
          <div class="channel-actions">
            <v-btn size="small" color="success" variant="tonal" @click="enableChannel(channel.index)">
              <v-icon start size="18">mdi-play-circle</v-icon>
              {{ t('common.enabled') }}
            </v-btn>

            <v-menu>
              <template #activator="{ props }">
                <v-btn icon size="small" variant="text" v-bind="props">
                  <v-icon size="18">mdi-dots-vertical</v-icon>
                </v-btn>
              </template>
              <v-list density="compact">
                <v-list-item @click="$emit('edit', channel)">
                  <template #prepend>
                    <v-icon size="small">mdi-pencil</v-icon>
                  </template>
                  <v-list-item-title>{{ t('common.edit') }}</v-list-item-title>
                </v-list-item>
                <v-divider />
                <v-list-item @click="enableChannel(channel.index)">
                  <template #prepend>
                    <v-icon size="small" color="success">mdi-play-circle</v-icon>
                  </template>
                  <v-list-item-title>{{ t('common.enabled') }}</v-list-item-title>
                </v-list-item>
                <v-list-item @click="$emit('delete', channel.index)">
                  <template #prepend>
                    <v-icon size="small" color="error">mdi-delete</v-icon>
                  </template>
                  <v-list-item-title>{{ t('common.delete') }}</v-list-item-title>
                </v-list-item>
              </v-list>
            </v-menu>
          </div>
        </div>
      </div>
    </div>

    <!-- 备用资源池 (disabled only) -->
    <div class="pt-2 pb-3">
      <div class="inactive-pool-header">
        <div class="text-subtitle-2 text-medium-emphasis d-flex align-center">
          <v-icon size="small" class="mr-1" color="grey">mdi-archive-outline</v-icon>
          {{ t('orchestration.reservePool') }}
          <v-chip size="x-small" class="ml-2">{{ inactiveChannels.length }}</v-chip>
        </div>
        <span class="text-caption text-medium-emphasis">{{ t('orchestration.enableToAppend') }}</span>
      </div>

      <div v-if="inactiveChannels.length > 0" class="inactive-pool">
        <div v-for="channel in inactiveChannels" :key="channel.index" class="inactive-channel-row">
          <!-- 渠道信息 -->
          <div class="channel-info">
            <div class="channel-info-main">
              <span class="font-weight-medium">{{ channel.name }}</span>
            </div>
            <div v-if="channel.description" class="channel-info-desc text-caption text-disabled">
              {{ channel.description }}
            </div>
          </div>

          <!-- API密钥数量 -->
          <div class="channel-keys">
            <v-chip size="x-small" variant="outlined" color="grey" class="keys-chip" @click="$emit('edit', channel)">
              <v-icon start size="x-small">mdi-key</v-icon>
              {{ channel.apiKeyCount ?? channel.apiKeys?.length ?? 0 }}
            </v-chip>
          </div>

          <!-- 操作按钮 -->
          <div class="channel-actions">
            <v-btn size="small" color="success" variant="tonal" @click="enableChannel(channel.index)">
              <v-icon start size="18">mdi-play-circle</v-icon>
              {{ t('common.enabled') }}
            </v-btn>

            <v-menu>
              <template #activator="{ props }">
                <v-btn icon size="small" variant="text" v-bind="props">
                  <v-icon size="18">mdi-dots-vertical</v-icon>
                </v-btn>
              </template>
              <v-list density="compact">
                <v-list-item @click="$emit('edit', channel)">
                  <template #prepend>
                    <v-icon size="small">mdi-pencil</v-icon>
                  </template>
                  <v-list-item-title>{{ t('common.edit') }}</v-list-item-title>
                </v-list-item>
                <v-divider />
                <v-list-item @click="enableChannel(channel.index)">
                  <template #prepend>
                    <v-icon size="small" color="success">mdi-play-circle</v-icon>
                  </template>
                  <v-list-item-title>{{ t('common.enabled') }}</v-list-item-title>
                </v-list-item>
                <v-list-item @click="$emit('delete', channel.index)">
                  <template #prepend>
                    <v-icon size="small" color="error">mdi-delete</v-icon>
                  </template>
                  <v-list-item-title>{{ t('common.delete') }}</v-list-item-title>
                </v-list-item>
              </v-list>
            </v-menu>
          </div>
        </div>
      </div>

      <div v-else class="text-center py-4 text-medium-emphasis text-caption">{{ t('orchestration.allActive') }}</div>
    </div>

    <!-- OAuth Status Dialog -->
    <OAuthStatusDialog
      v-model="showOAuthStatusDialog"
      :channel-id="oauthStatusChannelId"
    />
  </v-card>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import draggable from 'vuedraggable'
import { api, type Channel, type ChannelMetrics, type ChannelStatus, type RecentCallStat, type QuotaInfo, type ChannelUsageStatus, type CodexQuotaInfo } from '../services/api'
import ChannelStatusBadge from './ChannelStatusBadge.vue'
import ChannelStatsChart from './ChannelStatsChart.vue'
import OAuthStatusDialog from './OAuthStatusDialog.vue'

// i18n
const { t } = useI18n()

const props = defineProps<{
  channels: Channel[]
  currentChannelIndex: number
  channelType: 'messages' | 'responses' | 'gemini'
}>()

const emit = defineEmits<{
  (e: 'edit', channel: Channel): void
  (e: 'delete', channelId: number): void
  (e: 'refresh'): void
  (e: 'error', message: string): void
  (e: 'success', message: string): void
}>()

// 状态
const metrics = ref<ChannelMetrics[]>([])
const schedulerStats = ref<{
  multiChannelMode: boolean
  activeChannelCount: number
  traceAffinityCount: number
  traceAffinityTTL: string
  failureThreshold: number
  windowSize: number
} | null>(null)
const isLoadingMetrics = ref(false)
const showGreyPlaceholder = ref(true)
const metricsRefreshQueued = ref(false)
const isSavingOrder = ref(false)
const expandedChartChannelId = ref<number | null>(null) // 展开图表的渠道ID

// OAuth status dialog state
const showOAuthStatusDialog = ref(false)
const oauthStatusChannelId = ref<number | null>(null)

// Quota data for OAuth channels (keyed by stable channel id when available)
const channelQuotas = ref<Record<string, QuotaInfo>>({})
const oauthQuotaClock = ref(Date.now())
let oauthQuotaResetTimer: ReturnType<typeof setTimeout> | null = null

const clearOAuthQuotaResetTimer = () => {
  if (oauthQuotaResetTimer) {
    clearTimeout(oauthQuotaResetTimer)
    oauthQuotaResetTimer = null
  }
}

const parseResetTimestamp = (resetAt?: string): number | null => {
  if (!resetAt) return null
  const timestamp = Date.parse(resetAt)
  if (!Number.isFinite(timestamp)) return null
  return timestamp
}

const clampPercent = (percent: number): number => {
  return Math.min(100, Math.max(0, percent))
}

const getEffectiveUsedPercent = (usedPercent: number, resetAt?: string): number => {
  const resetTimestamp = parseResetTimestamp(resetAt)
  if (resetTimestamp !== null && oauthQuotaClock.value >= resetTimestamp) {
    return 0
  }
  return clampPercent(usedPercent)
}

const getOAuthWindowUsedPercent = (channel: Channel, window: 'primary' | 'secondary'): number => {
  const codexQuota = getChannelQuota(channel)?.codex_quota
  if (!codexQuota) return 0

  if (window === 'primary') {
    return getEffectiveUsedPercent(codexQuota.primary_used_percent, codexQuota.primary_reset_at)
  }
  return getEffectiveUsedPercent(codexQuota.secondary_used_percent, codexQuota.secondary_reset_at)
}

const getOAuthWindowRemainingPercent = (channel: Channel, window: 'primary' | 'secondary'): number => {
  return 100 - getOAuthWindowUsedPercent(channel, window)
}

const getNextOAuthResetTimestamp = (codexQuota?: CodexQuotaInfo): number | null => {
  if (!codexQuota) return null

  const now = Date.now()
  const candidates = [
    parseResetTimestamp(codexQuota.primary_reset_at),
    parseResetTimestamp(codexQuota.secondary_reset_at)
  ].filter((timestamp): timestamp is number => timestamp !== null && timestamp > now)

  if (candidates.length === 0) return null
  return Math.min(...candidates)
}

const scheduleOAuthQuotaAutoReset = () => {
  clearOAuthQuotaResetTimer()

  if (props.channelType !== 'responses') return

  let nearestReset: number | null = null
  for (const quotaInfo of Object.values(channelQuotas.value)) {
    const nextReset = getNextOAuthResetTimestamp(quotaInfo.codex_quota)
    if (nextReset === null) continue
    if (nearestReset === null || nextReset < nearestReset) {
      nearestReset = nextReset
    }
  }

  if (nearestReset === null) return

  const delay = Math.max(0, nearestReset - Date.now()) + 50
  oauthQuotaResetTimer = setTimeout(() => {
    oauthQuotaClock.value = Date.now()
    scheduleOAuthQuotaAutoReset()
  }, delay)
}

const getQuotaKey = (channel: Channel): string => {
  if (channel.id && channel.id.trim().length > 0) {
    return `id:${channel.id}`
  }
  return `idx:${channel.index}`
}

// Get quota for a channel
const getChannelQuota = (channel: Channel): QuotaInfo | undefined => {
  return channelQuotas.value[getQuotaKey(channel)]
}

// Fetch quota for OAuth channels
const fetchOAuthQuotas = async () => {
  // Only fetch for responses channels with openai-oauth service type
  if (props.channelType !== 'responses') {
    channelQuotas.value = {}
    oauthQuotaClock.value = Date.now()
    scheduleOAuthQuotaAutoReset()
    return
  }

  const oauthChannels = props.channels.filter(ch => ch.serviceType === 'openai-oauth')
  if (oauthChannels.length === 0) {
    channelQuotas.value = {}
    oauthQuotaClock.value = Date.now()
    scheduleOAuthQuotaAutoReset()
    return
  }

  // Fetch quota for each OAuth channel in parallel
  const results = await Promise.allSettled(
    oauthChannels.map(async (ch) => {
      try {
        const status = await api.getResponsesChannelOAuthStatus(ch.index)
        if (status.quota) {
          return { key: getQuotaKey(ch), quota: status.quota }
        }
        return null
      } catch {
        return null
      }
    })
  )

  // Update quotas
  const newQuotas: Record<string, QuotaInfo> = {}
  for (const result of results) {
    if (result.status === 'fulfilled' && result.value) {
      newQuotas[result.value.key] = result.value.quota
    }
  }
  channelQuotas.value = newQuotas
  oauthQuotaClock.value = Date.now()
  scheduleOAuthQuotaAutoReset()
}

// User-configured usage quotas (keyed by channel index)
const usageQuotas = ref<Record<number, ChannelUsageStatus>>({})

// Get usage quota for a channel
const getUsageQuota = (channelIndex: number): ChannelUsageStatus | undefined => {
  return usageQuotas.value[channelIndex]
}

// Check if a channel has usage quota configured
const hasUsageQuota = (channel: Channel): boolean => {
  return channel.quotaType === 'requests' || channel.quotaType === 'credit'
}

// Get usage quota bar color based on remaining percent
const getUsageQuotaBarColor = (remainingPercent: number): string => {
  if (remainingPercent >= 50) return 'rgb(76, 175, 80)'   // success - green
  if (remainingPercent >= 20) return 'rgb(255, 193, 7)'   // warning - yellow
  return 'rgb(244, 67, 54)'                                // error - red
}

// Format usage quota value for display
const formatQuotaValue = (value: number, quotaType: string): string => {
  if (quotaType === 'credit') {
    return `$${value.toFixed(2)}`
  }
  return Math.round(value).toString()
}

// Fetch usage quotas for all channels
const fetchUsageQuotas = async () => {
  try {
    if (props.channelType === 'messages') {
      usageQuotas.value = await api.getAllChannelUsageQuotas()
    } else if (props.channelType === 'responses') {
      usageQuotas.value = await api.getAllResponsesChannelUsageQuotas()
    } else {
      usageQuotas.value = await api.getAllGeminiChannelUsageQuotas()
    }
  } catch (error) {
    console.warn('Failed to fetch usage quotas:', error)
  }
}

// Reset usage quota for a channel
const resetUsageQuota = async (channelIndex: number) => {
  try {
    if (props.channelType === 'messages') {
      await api.resetChannelUsageQuota(channelIndex)
    } else if (props.channelType === 'responses') {
      await api.resetResponsesChannelUsageQuota(channelIndex)
    } else {
      await api.resetGeminiChannelUsageQuota(channelIndex)
    }
    await fetchUsageQuotas()
  } catch (error) {
    console.error('Failed to reset usage quota:', error)
  }
}

// Open OAuth status dialog for a channel
const openOAuthStatus = (channel: Channel) => {
  oauthStatusChannelId.value = channel.index
  showOAuthStatusDialog.value = true
}

// 切换渠道图表展开状态
const toggleChannelChart = (channelId: number) => {
  if (expandedChartChannelId.value === channelId) {
    expandedChartChannelId.value = null
  } else {
    expandedChartChannelId.value = channelId
  }
}

// 活跃渠道（可拖拽排序）- 包含 active 和 suspended 状态
const activeChannels = ref<Channel[]>([])

// 计算属性：禁用的组合渠道 - 单独分组（按名称排序）
const disabledCompositeChannels = computed(() => {
  return props.channels
    .filter(ch => ch.status === 'disabled' && ch.serviceType === 'composite')
    .sort((a, b) => a.name.localeCompare(b.name) || a.index - b.index)
})

// 计算属性：非活跃渠道 - 仅 disabled 状态，排除组合渠道（按名称排序）
const inactiveChannels = computed(() => {
  return props.channels
    .filter(ch => ch.status === 'disabled' && ch.serviceType !== 'composite')
    .sort((a, b) => a.name.localeCompare(b.name) || a.index - b.index)
})

// 计算属性：是否为多渠道模式
// 多渠道模式判断逻辑：
// 1. 只有一个启用的渠道 → 单渠道模式
// 2. 有一个 active + 几个 suspended → 单渠道模式
// 3. 有多个 active 渠道 → 多渠道模式
const isMultiChannelMode = computed(() => {
  const activeCount = props.channels.filter(
    ch => ch.status === 'active' || ch.status === undefined
  ).length
  return activeCount > 1
})

// 初始化活跃渠道列表 - active + suspended 都参与故障转移序列
const initActiveChannels = () => {
  const active = props.channels
    .filter(ch => ch.status !== 'disabled')
    .sort((a, b) => (a.priority ?? a.index) - (b.priority ?? b.index))
  activeChannels.value = [...active]
}

// 监听 channels 变化
watch(() => props.channels, initActiveChannels, { immediate: true, deep: true })

// 获取渠道指标
const getChannelMetrics = (channelIndex: number): ChannelMetrics | undefined => {
  return metrics.value.find(m => m.channelIndex === channelIndex)
}

const recentCallsLimit = 20

type RecentCallSlot = {
  state: 'unused' | 'success' | 'failure'
  statusCode?: number
  timestamp?: string
  model?: string
  channelName?: string
  routedChannelName?: string
}

const getRecentCalls = (channelIndex: number): RecentCallSlot[] => {
  if (showGreyPlaceholder.value) {
    return Array.from({ length: recentCallsLimit }, () => ({
      state: 'unused' as const
    }))
  }

  const recentCalls = getChannelMetrics(channelIndex)?.recentCalls ?? []
  const normalizedCalls: RecentCallSlot[] = recentCalls.slice(-recentCallsLimit).map((call: RecentCallStat) => ({
    state: call.success ? 'success' : 'failure',
    statusCode: call.statusCode,
    timestamp: call.timestamp,
    model: call.model,
    channelName: call.channelName,
    routedChannelName: call.routedChannelName
  }))

  if (normalizedCalls.length >= recentCallsLimit) {
    return normalizedCalls
  }

  const leadingUnused = Array.from({ length: recentCallsLimit - normalizedCalls.length }, () => ({
    state: 'unused' as const
  }))
  return [...leadingUnused, ...normalizedCalls]
}

const getRecentSuccessRate = (channelIndex: number): string => {
  const recentCalls = getChannelMetrics(channelIndex)?.recentCalls?.slice(-recentCallsLimit) ?? []
  if (recentCalls.length === 0) return '0%'

  const successCount = recentCalls.filter(call => call.success).length
  return `${Math.round((successCount / recentCalls.length) * 100)}%`
}

const formatRecentCallTime = (timestamp?: string): string => {
  if (!timestamp) return 'Time unavailable'
  const parsed = new Date(timestamp)
  if (Number.isNaN(parsed.getTime())) return 'Time unavailable'
  return parsed.toLocaleString()
}

const getRecentCallStatusText = (call: RecentCallSlot): string => {
  if (call.state === 'unused') {
    return 'Not used yet'
  }

  if (call.state === 'failure') {
    if (call.statusCode && call.statusCode > 0) {
      return `Failed (HTTP ${call.statusCode})`
    }
    return 'Failed (HTTP error)'
  }

  return 'Success'
}

const getRecentCallTooltipLines = (call: RecentCallSlot, isCompositeChannel: boolean): string[] => {
  const lines = [
    `Status: ${getRecentCallStatusText(call)}`,
    `Time: ${formatRecentCallTime(call.timestamp)}`,
    `Model: ${call.model?.trim() ? call.model : 'N/A'}`
  ]

  if (isCompositeChannel) {
    const compositeTarget = call.routedChannelName?.trim() || call.channelName?.trim() || 'N/A'
    lines.push(`Channel: ${compositeTarget}`)
  }

  return lines
}

// 获取配额条颜色 (based on used percent)
const getQuotaBarColor = (usedPercent: number): string => {
  const remaining = 100 - usedPercent
  if (remaining >= 50) return 'rgb(var(--v-theme-success))'
  if (remaining >= 20) return 'rgb(var(--v-theme-warning))'
  return 'rgb(var(--v-theme-error))'
}

// 获取官网 URL（优先使用 website，否则从 baseUrl 提取域名）
const getWebsiteUrl = (channel: Channel): string => {
  if (channel.website) return channel.website
  try {
    const url = new URL(channel.baseUrl)
    return `${url.protocol}//${url.host}`
  } catch {
    return channel.baseUrl
  }
}

// 刷新指标
const refreshMetrics = async () => {
  if (isLoadingMetrics.value) {
    metricsRefreshQueued.value = true
    return
  }
  isLoadingMetrics.value = true
  const requestedType = props.channelType
  try {
    let metricsPromise
    if (requestedType === 'messages') {
      metricsPromise = api.getChannelMetrics()
    } else if (requestedType === 'responses') {
      metricsPromise = api.getResponsesChannelMetrics()
    } else {
      metricsPromise = api.getGeminiChannelMetrics()
    }
    const [metricsData, statsData] = await Promise.all([
      metricsPromise,
      api.getSchedulerStats(requestedType)
    ])
    if (requestedType !== props.channelType) {
      return
    }
    metrics.value = metricsData
    schedulerStats.value = statsData
    showGreyPlaceholder.value = false
  } catch (error) {
    console.error('Failed to load metrics:', error)
  } finally {
    isLoadingMetrics.value = false
    if (metricsRefreshQueued.value) {
      metricsRefreshQueued.value = false
      void refreshMetrics()
    }
  }
}

// 拖拽变更事件 - 自动保存顺序
const onDragChange = () => {
  // 拖拽后自动保存顺序到后端
  saveOrder()
}

// 保存顺序
const saveOrder = async () => {
  isSavingOrder.value = true
  try {
    const order = activeChannels.value.map(ch => ch.index)
    if (props.channelType === 'messages') {
      await api.reorderChannels(order)
    } else if (props.channelType === 'responses') {
      await api.reorderResponsesChannels(order)
    } else {
      await api.reorderGeminiChannels(order)
    }
    // 不调用 emit('refresh')，避免触发父组件刷新导致列表闪烁
  } catch (error) {
    console.error('Failed to save order:', error)
    const errorMessage = error instanceof Error ? error.message : t('common.unknown')
    emit('error', t('orchestration.saveOrderFailed', { error: errorMessage }))
    // 保存失败时重新初始化列表，恢复原始顺序
    initActiveChannels()
  } finally {
    isSavingOrder.value = false
  }
}

// 设置渠道状态
const setChannelStatus = async (channelId: number, status: ChannelStatus) => {
  try {
    if (props.channelType === 'messages') {
      await api.setChannelStatus(channelId, status)
    } else if (props.channelType === 'responses') {
      await api.setResponsesChannelStatus(channelId, status)
    } else {
      await api.setGeminiChannelStatus(channelId, status)
    }
    emit('refresh')
  } catch (error) {
    console.error('Failed to set channel status:', error)
    const errorMessage = error instanceof Error ? error.message : t('common.unknown')
    emit('error', t('orchestration.setStatusFailed', { error: errorMessage }))
  }
}

// 启用渠道（从备用池移到活跃序列）
const enableChannel = async (channelId: number) => {
  await setChannelStatus(channelId, 'active')
}

// 恢复渠道（重置指标并设为 active）
const resumeChannel = async (channelId: number) => {
  try {
    if (props.channelType === 'messages') {
      await api.resumeChannel(channelId)
    } else if (props.channelType === 'responses') {
      await api.resumeResponsesChannel(channelId)
    }
    // Gemini doesn't have resumeChannel, just set status directly
    await setChannelStatus(channelId, 'active')
  } catch (error) {
    console.error('Failed to resume channel:', error)
  }
}

// 设置渠道促销期（抢优先级）- Gemini不支持促销期
const setPromotion = async (channel: Channel) => {
  if (props.channelType === 'gemini') {
    // Gemini doesn't support promotion period
    emit('error', 'Promotion not supported for Gemini channels')
    return
  }
  try {
    const PROMOTION_DURATION = 300 // 5分钟
    if (props.channelType === 'messages') {
      await api.setChannelPromotion(channel.index, PROMOTION_DURATION)
    } else {
      await api.setResponsesChannelPromotion(channel.index, PROMOTION_DURATION)
    }
    emit('refresh')
    // 通知用户
    emit('success', t('channel.prioritySet', { name: channel.name }))
  } catch (error) {
    console.error('Failed to set promotion:', error)
    const errorMessage = error instanceof Error ? error.message : t('common.unknown')
    emit('error', t('orchestration.setPriorityFailed', { error: errorMessage }))
  }
}

// 判断渠道是否可以删除
// 规则：故障转移序列中至少要保留一个 active 状态的渠道
const canDeleteChannel = (channel: Channel): boolean => {
  // 统计当前 active 状态的渠道数量
  const activeCount = activeChannels.value.filter(
    ch => ch.status === 'active' || ch.status === undefined
  ).length

  // 如果要删除的是 active 渠道，且只剩一个 active，则不允许删除
  const isActive = channel.status === 'active' || channel.status === undefined
  if (isActive && activeCount <= 1) {
    return false
  }

  return true
}

// 处理删除渠道
const handleDeleteChannel = (channel: Channel) => {
  if (!canDeleteChannel(channel)) {
    emit('error', t('orchestration.cannotDelete'))
    return
  }
  emit('delete', channel.index)
}

// 组件挂载时加载指标
onMounted(() => {
  refreshMetrics()
  fetchOAuthQuotas()
  fetchUsageQuotas()
})

onUnmounted(() => {
  clearOAuthQuotaResetTimer()
})

// Re-fetch quotas when tab changes
watch(() => props.channelType, () => {
  // Prevent briefly showing previous tab's metrics while new data is loading.
  showGreyPlaceholder.value = true
  metrics.value = []
  schedulerStats.value = null
  void refreshMetrics()
  fetchOAuthQuotas()
  fetchUsageQuotas()
})

const channelQuotaCacheKey = computed(() => props.channels
  .map(ch => `${ch.id ?? ''}:${ch.index}:${ch.priority ?? ''}:${ch.status ?? ''}:${ch.serviceType}`)
  .join('|'))

// Channel indices can change after DB polling / reorder operations.
// Re-fetch quota snapshots whenever the channel mapping changes.
watch(channelQuotaCacheKey, () => {
  fetchOAuthQuotas()
  fetchUsageQuotas()
})

// 暴露方法给父组件
defineExpose({
  refreshMetrics,
  fetchOAuthQuotas,
  fetchUsageQuotas
})
</script>

<style scoped>
/* =====================================================
   🎮 渠道编排 - 复古像素主题样式
   Neo-Brutalism: 直角、粗黑边框、硬阴影
   ===================================================== */

.channel-orchestration {
  overflow: hidden;
  background: transparent;
  border: none;
}

.channel-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.channel-item-wrapper {
  display: flex;
  flex-direction: column;
}

.channel-row {
  display: grid;
  grid-template-columns: 36px 36px 110px 1fr 230px 90px 140px;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  margin: 2px;
  background: rgb(var(--v-theme-surface));
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface));
  min-height: 56px;
  transition: all 0.1s ease;
}

/* Responses tab has an extra quota column */
.channel-row.has-quota-column {
  grid-template-columns: 36px 36px 110px 1fr 230px 100px 90px 140px;
}

.channel-row:hover {
  background: rgba(var(--v-theme-primary), 0.08);
  transform: translate(-2px, -2px);
  box-shadow: 6px 6px 0 0 rgb(var(--v-theme-on-surface));
  border: 2px solid rgb(var(--v-theme-on-surface));
}

.channel-row:active {
  transform: translate(2px, 2px);
  box-shadow: none;
}

.v-theme--dark .channel-row {
  background: rgb(var(--v-theme-surface));
  border-color: rgba(255, 255, 255, 0.7);
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7);
}
.v-theme--dark .channel-row:hover {
  background: rgba(var(--v-theme-primary), 0.12);
  box-shadow: 6px 6px 0 0 rgba(255, 255, 255, 0.7);
  border-color: rgba(255, 255, 255, 0.7);
}

/* suspended 状态的视觉区分 */
.channel-row.is-suspended {
  background: rgba(var(--v-theme-warning), 0.1);
  border-color: rgb(var(--v-theme-warning));
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface));
}
.channel-row.is-suspended:hover {
  background: rgba(var(--v-theme-warning), 0.15);
  box-shadow: 6px 6px 0 0 rgb(var(--v-theme-on-surface));
}

.v-theme--dark .channel-row.is-suspended {
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7);
}

.v-theme--dark .channel-row.is-suspended:hover {
  box-shadow: 6px 6px 0 0 rgba(255, 255, 255, 0.7);
}

.channel-row.ghost {
  opacity: 0.6;
  background: rgba(var(--v-theme-primary), 0.15);
  border: 2px dashed rgb(var(--v-theme-primary));
  box-shadow: none;
}

.drag-handle {
  cursor: grab;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  transition: all 0.1s ease;
}

.drag-handle:hover {
  background: rgba(var(--v-theme-on-surface), 0.1);
}

.drag-handle:active {
  cursor: grabbing;
  background: rgba(var(--v-theme-primary), 0.2);
}

.priority-number {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  background: rgb(var(--v-theme-primary));
  color: white;
  font-size: 12px;
  font-weight: 700;
  border: 2px solid rgb(var(--v-theme-on-surface));
  text-transform: uppercase;
}

.v-theme--dark .priority-number {
  border-color: rgba(255, 255, 255, 0.6);
}

.channel-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.channel-name .font-weight-medium {
  font-size: 0.95rem;
}

.channel-metrics {
  display: flex;
  align-items: center;
}

.recent-calls-display {
  display: flex;
  align-items: center;
  gap: 8px;
}

.recent-calls-blocks {
  display: flex;
  align-items: center;
  gap: 2px;
}

.recent-call-block {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 1px;
  background: rgba(var(--v-theme-on-surface), 0.22);
  cursor: help;
}

.recent-call-block.is-unused {
  background: rgba(var(--v-theme-on-surface), 0.2);
}

.recent-call-block.is-success {
  background: rgb(var(--v-theme-success));
}

.recent-call-block.is-failure {
  background: rgb(var(--v-theme-error));
}

.recent-calls-rate {
  font-size: 12px;
  font-weight: 600;
  min-width: 36px;
  text-align: right;
  color: rgba(var(--v-theme-on-surface), 0.8);
}

.recent-call-tooltip {
  display: flex;
  flex-direction: column;
  gap: 2px;
  white-space: nowrap;
}

/* Inline Quota Bar */
.channel-quota {
  display: flex;
  align-items: center;
  min-width: 70px;
}

.quota-bar-container {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  padding: 2px 4px;
  border-radius: 4px;
  transition: background-color 0.15s ease;
}

.quota-bar-container:hover {
  background: rgba(var(--v-theme-primary), 0.1);
}

/* OAuth dual quota bars (5h and 7d) */
.oauth-quota-dual-bar {
  display: flex;
  flex-direction: column;
  gap: 3px;
  cursor: pointer;
  padding: 2px 4px;
  border-radius: 4px;
  transition: background-color 0.15s ease;
}

.oauth-quota-dual-bar:hover {
  background: rgba(var(--v-theme-primary), 0.1);
}

.oauth-quota-row {
  display: flex;
  align-items: center;
  gap: 4px;
}

.oauth-quota-label {
  font-size: 9px;
  font-weight: 600;
  color: rgba(var(--v-theme-on-surface), 0.6);
  min-width: 14px;
}

.quota-bar-wrapper {
  width: 40px;
  height: 6px;
  background: rgba(var(--v-theme-on-surface), 0.15);
  border-radius: 3px;
  overflow: hidden;
}

.quota-bar {
  height: 100%;
  border-radius: 3px;
  transition: width 0.3s ease, background-color 0.3s ease;
}

.quota-text {
  font-size: 11px;
  font-weight: 600;
  min-width: 28px;
  text-align: right;
}

/* Quota tooltip */
.quota-tooltip {
  font-size: 12px;
  line-height: 1.5;
}

.quota-tooltip-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 2px 0;
}

.quota-tooltip-row span:first-child {
  color: rgba(var(--v-theme-on-surface), 0.7);
}

.quota-tooltip-row span:last-child {
  font-weight: 500;
}

.channel-keys {
  display: flex;
  align-items: center;
}

.channel-keys .keys-chip {
  cursor: pointer;
  transition: all 0.1s ease;
}

.channel-keys .keys-chip:hover {
  background: rgba(var(--v-theme-primary), 0.1);
  border-color: rgb(var(--v-theme-primary));
  color: rgb(var(--v-theme-primary));
}

.channel-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  justify-content: flex-end;
}

/* 图表展开按钮样式 */
.chart-toggle-btn {
  transition: all 0.2s ease;
}

.chart-toggle-btn:hover {
  background-color: rgba(var(--v-theme-primary), 0.15) !important;
}

/* 内联操作按钮容器 */
.inline-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}

/* 内联操作按钮 hover 效果增强 */
.inline-actions :deep(.v-btn:hover) {
  background-color: rgba(var(--v-theme-on-surface), 0.15) !important;
}

.inline-actions :deep(.v-btn[color="primary"]:hover) {
  background-color: rgba(var(--v-theme-primary), 0.2) !important;
}

.inline-actions :deep(.v-btn[color="info"]:hover) {
  background-color: rgba(var(--v-theme-info), 0.2) !important;
}

.inline-actions :deep(.v-btn[color="warning"]:hover) {
  background-color: rgba(var(--v-theme-warning), 0.2) !important;
}

.inline-actions :deep(.v-btn[color="success"]:hover) {
  background-color: rgba(var(--v-theme-success), 0.2) !important;
}

.inline-actions :deep(.v-btn[color="error"]:hover) {
  background-color: rgba(var(--v-theme-error), 0.2) !important;
}

/* 窄屏时隐藏的菜单项（默认隐藏） */
.menu-item-narrow {
  display: none !important;
}

/* 备用资源池样式 */
.inactive-pool-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.inactive-pool {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(336px, 1fr));
  gap: 10px;
  background: rgb(var(--v-theme-surface));
  padding: 16px;
  border: 2px dashed rgb(var(--v-theme-on-surface));
}

.v-theme--dark .inactive-pool {
  background: rgb(var(--v-theme-surface));
  border-color: rgba(255, 255, 255, 0.5);
}

/* 组合渠道池样式 - 紫色主题 */
.composite-pool {
  border-color: rgba(156, 39, 176, 0.5);
}

.v-theme--dark .composite-pool {
  border-color: rgba(186, 104, 200, 0.6);
}

.composite-channel-row {
  border-color: rgba(156, 39, 176, 0.6);
  box-shadow: 3px 3px 0 0 rgba(156, 39, 176, 0.4);
}

.composite-channel-row:hover {
  background: rgba(156, 39, 176, 0.08);
  box-shadow: 4px 4px 0 0 rgba(156, 39, 176, 0.5);
}

.v-theme--dark .composite-channel-row {
  border-color: rgba(186, 104, 200, 0.7);
  box-shadow: 3px 3px 0 0 rgba(186, 104, 200, 0.5);
}

.v-theme--dark .composite-channel-row:hover {
  background: rgba(186, 104, 200, 0.12);
  box-shadow: 4px 4px 0 0 rgba(186, 104, 200, 0.6);
}

.inactive-channel-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  background: rgb(var(--v-theme-surface));
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 3px 3px 0 0 rgb(var(--v-theme-on-surface));
  transition: all 0.1s ease;
}

.inactive-channel-row:hover {
  background: rgba(var(--v-theme-primary), 0.08);
  transform: translate(-1px, -1px);
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface));
}

.inactive-channel-row:active {
  transform: translate(2px, 2px);
  box-shadow: none;
}

.v-theme--dark .inactive-channel-row {
  background: rgb(var(--v-theme-surface));
  border-color: rgba(255, 255, 255, 0.6);
  box-shadow: 3px 3px 0 0 rgba(255, 255, 255, 0.6);
}

.v-theme--dark .inactive-channel-row:hover {
  background: rgba(var(--v-theme-primary), 0.12);
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.6);
}

.inactive-channel-row .channel-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.inactive-channel-row .channel-info-main {
  font-size: 0.7rem;
  font-weight: 600;
  line-height: 1.3;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  text-overflow: ellipsis;
  word-break: break-word;
}

.inactive-channel-row .channel-info-desc {
  display: none;
}

.inactive-channel-row .channel-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

/* 响应式调整 */
@media (max-width: 960px) {
  .channel-row {
    grid-template-columns: 32px 32px 90px 1fr 70px;
    padding: 10px 12px;
  }

  .channel-metrics,
  .channel-keys,
  .channel-quota {
    display: none;
  }

  /* 窄屏时隐藏内联按钮，显示菜单中的选项 */
  .inline-actions {
    display: none;
  }

  .menu-item-narrow {
    display: flex !important;
  }
}

@media (max-width: 600px) {
  .channel-row {
    grid-template-columns: 28px 1fr 60px;
    padding: 10px;
    gap: 8px;
    box-shadow: 3px 3px 0 0 rgb(var(--v-theme-on-surface));
  }

  .v-theme--dark .channel-row {
    box-shadow: 3px 3px 0 0 rgba(255, 255, 255, 0.6);
  }

  .priority-number,
  .drag-handle {
    display: none;
  }

  .inactive-pool {
    grid-template-columns: 1fr;
    padding: 12px;
  }

  .inactive-pool-header {
    flex-wrap: wrap;
    gap: 4px;
  }

  .inactive-channel-row {
    flex-wrap: wrap;
    padding: 10px;
    box-shadow: 2px 2px 0 0 rgb(var(--v-theme-on-surface));
  }

  .v-theme--dark .inactive-channel-row {
    box-shadow: 2px 2px 0 0 rgba(255, 255, 255, 0.5);
  }

  .inactive-channel-row .channel-info {
    flex: 1 1 100%;
    margin-bottom: 8px;
  }

  .inactive-channel-row .channel-keys {
    display: none;
  }

  .inactive-channel-row .channel-actions {
    flex: 1;
    justify-content: flex-end;
  }
}

/* =========================================
   Minimal Dark Theme Overrides
   ========================================= */
[data-theme="minimal"] .channel-row {
  border: none !important;
  box-shadow: none !important;
  border-radius: 12px !important;
  transition: all 0.2s ease !important;
}

[data-theme="minimal"] .channel-row:hover {
  background: rgba(255, 255, 255, 0.12) !important;
  transform: none !important;
  box-shadow: none !important;
}

[data-theme="minimal"] .channel-row:active {
  transform: none !important;
  box-shadow: none !important;
  background: rgba(255, 255, 255, 0.15) !important;
}

[data-theme="minimal"] .channel-row.is-suspended {
  border: none !important;
  box-shadow: none !important;
}

[data-theme="minimal"] .channel-row.is-suspended:hover {
  box-shadow: none !important;
}

[data-theme="minimal"] .channel-row.ghost {
  border: 2px dashed rgb(var(--v-theme-primary)) !important;
  box-shadow: none !important;
  border-radius: 12px !important;
}

[data-theme="minimal"] .inactive-pool {
  border: 1px dashed rgba(255, 255, 255, 0.15) !important;
  border-radius: 12px !important;
  background: rgba(255, 255, 255, 0.02) !important;
}

[data-theme="minimal"] .inactive-channel-row {
  border: none !important;
  box-shadow: none !important;
  border-radius: 10px !important;
  transition: all 0.2s ease !important;
}

[data-theme="minimal"] .inactive-channel-row:hover {
  background: rgba(255, 255, 255, 0.12) !important;
  transform: none !important;
  box-shadow: none !important;
}

[data-theme="minimal"] .inactive-channel-row:active {
  transform: none !important;
  box-shadow: none !important;
}
</style>
