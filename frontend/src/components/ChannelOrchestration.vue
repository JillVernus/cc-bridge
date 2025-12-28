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
            <div class="channel-row" :class="{ 'is-suspended': element.status === 'suspended' }">
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
              <v-icon v-if="element.serviceType === 'claude'" size="14" icon="custom:claude" class="ml-2" />
              <v-icon v-else size="14" icon="custom:codex" class="ml-2" />
              <span class="text-caption text-medium-emphasis ml-1">{{ element.serviceType }}</span>
              <span v-if="element.description" class="text-caption text-disabled ml-3">{{ element.description }}</span>
            </div>

            <!-- 指标显示 -->
            <div class="channel-metrics">
              <template v-if="getChannelMetrics(element.index)">
                <v-tooltip location="top" :open-delay="200">
                  <template #activator="{ props: tooltipProps }">
                    <div v-bind="tooltipProps" class="d-flex align-center metrics-display">
                      <!-- 15分钟有请求时显示成功率，否则显示 -- -->
                      <template v-if="get15mStats(element.index)?.requestCount">
                        <v-chip
                          size="x-small"
                          :color="getSuccessRateColor(get15mStats(element.index)?.successRate)"
                          variant="tonal"
                        >
                          {{ get15mStats(element.index)?.successRate?.toFixed(0) }}%
                        </v-chip>
                        <span class="text-caption text-medium-emphasis ml-2">
                          {{ get15mStats(element.index)?.requestCount }} {{ t('orchestration.requests') }}
                        </span>
                      </template>
                      <span v-else class="text-caption text-medium-emphasis">--</span>
                    </div>
                  </template>
                  <div class="metrics-tooltip">
                    <div class="text-caption font-weight-bold mb-1">{{ t('orchestration.requestStats') }}</div>
                    <div class="metrics-tooltip-row">
                      <span>{{ t('orchestration.minutes15') }}</span>
                      <span>{{ formatStats(get15mStats(element.index)) }}</span>
                    </div>
                    <div class="metrics-tooltip-row">
                      <span>{{ t('orchestration.hour1') }}</span>
                      <span>{{ formatStats(get1hStats(element.index)) }}</span>
                    </div>
                    <div class="metrics-tooltip-row">
                      <span>{{ t('orchestration.hours6') }}</span>
                      <span>{{ formatStats(get6hStats(element.index)) }}</span>
                    </div>
                    <div class="metrics-tooltip-row">
                      <span>{{ t('orchestration.hours24') }}</span>
                      <span>{{ formatStats(get24hStats(element.index)) }}</span>
                    </div>
                  </div>
                </v-tooltip>
              </template>
              <span v-else class="text-caption text-medium-emphasis">--</span>
            </div>

            <!-- API密钥数量 -->
            <div class="channel-keys">
              <v-chip size="x-small" variant="outlined" class="keys-chip" @click="$emit('edit', element)">
                <v-icon start size="x-small">mdi-key</v-icon>
                {{ element.apiKeys?.length || 0 }}
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
                  <v-list-item @click="$emit('ping', element.index)">
                    <template #prepend>
                      <v-icon size="small">mdi-speedometer</v-icon>
                    </template>
                    <v-list-item-title>{{ t('actions.testLatency') }}</v-list-item-title>
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
            :is-responses="channelType === 'responses'"
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
              {{ channel.apiKeys?.length || 0 }}
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
  </v-card>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import draggable from 'vuedraggable'
import { api, type Channel, type ChannelMetrics, type ChannelStatus, type TimeWindowStats } from '../services/api'
import ChannelStatusBadge from './ChannelStatusBadge.vue'
import ChannelStatsChart from './ChannelStatsChart.vue'

// i18n
const { t } = useI18n()

const props = defineProps<{
  channels: Channel[]
  currentChannelIndex: number
  channelType: 'messages' | 'responses'
}>()

const emit = defineEmits<{
  (e: 'edit', channel: Channel): void
  (e: 'delete', channelId: number): void
  (e: 'ping', channelId: number): void
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
const isSavingOrder = ref(false)
const expandedChartChannelId = ref<number | null>(null) // 展开图表的渠道ID

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

// 计算属性：非活跃渠道 - 仅 disabled 状态
const inactiveChannels = computed(() => {
  return props.channels.filter(ch => ch.status === 'disabled')
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

// 获取分时段统计的辅助方法
const get15mStats = (channelIndex: number) => {
  return getChannelMetrics(channelIndex)?.timeWindows?.['15m']
}

const get1hStats = (channelIndex: number) => {
  return getChannelMetrics(channelIndex)?.timeWindows?.['1h']
}

const get6hStats = (channelIndex: number) => {
  return getChannelMetrics(channelIndex)?.timeWindows?.['6h']
}

const get24hStats = (channelIndex: number) => {
  return getChannelMetrics(channelIndex)?.timeWindows?.['24h']
}

// 获取成功率颜色
const getSuccessRateColor = (rate?: number): string => {
  if (rate === undefined) return 'grey'
  if (rate >= 90) return 'success'
  if (rate >= 70) return 'warning'
  return 'error'
}

// 格式化统计数据：有请求显示"N 请求 (X%)"，无请求显示"--"
const formatStats = (stats?: TimeWindowStats): string => {
  if (!stats || !stats.requestCount) return '--'
  return `${stats.requestCount} ${t('orchestration.requests')} (${stats.successRate?.toFixed(0)}%)`
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
  isLoadingMetrics.value = true
  try {
    const [metricsData, statsData] = await Promise.all([
      props.channelType === 'messages' ? api.getChannelMetrics() : api.getResponsesChannelMetrics(),
      api.getSchedulerStats(props.channelType)
    ])
    metrics.value = metricsData
    schedulerStats.value = statsData
  } catch (error) {
    console.error('Failed to load metrics:', error)
  } finally {
    isLoadingMetrics.value = false
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
    } else {
      await api.reorderResponsesChannels(order)
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
    } else {
      await api.setResponsesChannelStatus(channelId, status)
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
    } else {
      await api.resumeResponsesChannel(channelId)
    }
    await setChannelStatus(channelId, 'active')
  } catch (error) {
    console.error('Failed to resume channel:', error)
  }
}

// 设置渠道促销期（抢优先级）
const setPromotion = async (channel: Channel) => {
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
})

// 暴露方法给父组件
defineExpose({
  refreshMetrics
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
  grid-template-columns: 36px 36px 110px 1fr 130px 90px 140px;
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
  gap: 6px;
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
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
  background: rgb(var(--v-theme-surface));
  padding: 16px;
  border: 2px dashed rgb(var(--v-theme-on-surface));
}

.v-theme--dark .inactive-pool {
  background: rgb(var(--v-theme-surface));
  border-color: rgba(255, 255, 255, 0.5);
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
  .channel-keys {
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

/* 指标显示样式 */
.metrics-display {
  cursor: help;
}

/* 指标 tooltip 样式 */
.metrics-tooltip {
  font-size: 12px;
  line-height: 1.5;
  color: rgb(var(--v-theme-on-surface));
}

.metrics-tooltip-row {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 2px 0;
}

.metrics-tooltip-row span:first-child {
  color: rgba(var(--v-theme-on-surface), 0.7);
}

.metrics-tooltip-row span:last-child {
  font-weight: 500;
  color: rgb(var(--v-theme-on-surface));
}
</style>
