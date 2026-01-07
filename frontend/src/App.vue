<template>
  <v-app>
    <!-- 自动认证加载提示 - 只在真正进行自动认证时显示 -->
    <v-overlay
      :model-value="isAutoAuthenticating && !isInitialized"
      persistent
      class="align-center justify-center"
      scrim="black"
    >
      <v-card class="pa-6 text-center" max-width="400" rounded="lg">
        <v-progress-circular indeterminate :size="64" :width="6" color="primary" class="mb-4" />
        <div class="text-h6 mb-2">{{ t('app.verifyingAccess') }}</div>
        <div class="text-body-2 text-medium-emphasis">{{ t('app.authenticatingWithSavedKey') }}</div>
      </v-card>
    </v-overlay>

    <!-- 认证界面 -->
    <v-dialog v-model="showAuthDialog" persistent max-width="500">
      <v-card class="pa-4">
        <v-card-title class="text-h5 text-center mb-4"> 🔐 {{ t('app.title') }} </v-card-title>

        <v-card-text>
          <v-alert v-if="authError" type="error" variant="tonal" class="mb-4">
            {{ authError }}
          </v-alert>

          <v-form @submit.prevent="handleAuthSubmit">
            <v-text-field
              v-model="authKeyInput"
              :label="t('auth.accessKey')"
              type="password"
              variant="outlined"
              prepend-inner-icon="mdi-key"
              :rules="[v => !!v || t('auth.enterAccessKey')]"
              required
              autofocus
              @keyup.enter="handleAuthSubmit"
            />

            <v-btn type="submit" color="primary" block size="large" class="mt-4" :loading="authLoading">
              {{ t('auth.accessManagement') }}
            </v-btn>
          </v-form>

          <v-divider class="my-4" />

          <v-alert type="info" variant="tonal" density="compact" class="mb-0">
            <div class="text-body-2">
              <p class="mb-2"><strong>🔒 {{ t('auth.securityTips') }}</strong></p>
              <ul class="ml-4 mb-0">
                <li>{{ t('auth.tip1') }}</li>
                <li>{{ t('auth.tip2') }}</li>
                <li>{{ t('auth.tip3') }}</li>
                <li>{{ t('auth.tip4') }}</li>
                <li>{{ t('auth.tip5', { count: MAX_AUTH_ATTEMPTS }) }}</li>
              </ul>
            </div>
          </v-alert>
        </v-card-text>
      </v-card>
    </v-dialog>

    <!-- 应用栏 - 毛玻璃效果 -->
    <v-app-bar elevation="0" :height="$vuetify.display.mobile ? 56 : 72" class="app-header">
      <template #prepend>
        <v-menu>
          <template #activator="{ props }">
            <div class="app-logo" v-bind="props" style="cursor: pointer;" :title="t('common.settings')">
              <v-icon :size="$vuetify.display.mobile ? 22 : 32" color="white"> mdi-cog </v-icon>
            </div>
          </template>
          <v-list density="compact">
            <v-list-item @click="showPricingSettings = true">
              <template #prepend>
                <v-icon size="small">mdi-currency-usd</v-icon>
              </template>
              <v-list-item-title>{{ t('app.pricingSettings') }}</v-list-item-title>
            </v-list-item>
            <v-list-item @click="showRateLimitSettings = true">
              <template #prepend>
                <v-icon size="small">mdi-speedometer</v-icon>
              </template>
              <v-list-item-title>{{ t('app.rateLimitSettings') }}</v-list-item-title>
            </v-list-item>
            <v-list-item @click="showDebugLogSettings = true">
              <template #prepend>
                <v-icon size="small">mdi-bug</v-icon>
              </template>
              <v-list-item-title>{{ t('app.debugLogSettings') }}</v-list-item-title>
            </v-list-item>
            <v-list-item @click="showFailoverSettings = true">
              <template #prepend>
                <v-icon size="small">mdi-swap-horizontal</v-icon>
              </template>
              <v-list-item-title>{{ t('app.failoverSettings') }}</v-list-item-title>
            </v-list-item>
            <v-list-item @click="openBackupRestore">
              <template #prepend>
                <v-icon size="small">mdi-backup-restore</v-icon>
              </template>
              <v-list-item-title>{{ t('backup.title') }}</v-list-item-title>
            </v-list-item>
          </v-list>
        </v-menu>
      </template>

      <!-- 自定义标题容器 - 替代 v-app-bar-title -->
      <div class="header-title">
        <v-btn-toggle v-model="activeTab" mandatory class="nav-toggle">
          <v-btn value="messages" class="nav-btn" :size="$vuetify.display.mobile ? 'small' : 'default'">
            <v-icon start :size="$vuetify.display.mobile ? 16 : 20" icon="custom:claude" />
            <span>Claude</span>
          </v-btn>
          <v-btn value="responses" class="nav-btn" :size="$vuetify.display.mobile ? 'small' : 'default'">
            <v-icon start :size="$vuetify.display.mobile ? 16 : 20" icon="custom:codex" />
            <span>Codex</span>
          </v-btn>
          <v-btn value="apikeys" class="nav-btn" :size="$vuetify.display.mobile ? 'small' : 'default'">
            <v-icon start :size="$vuetify.display.mobile ? 16 : 20">mdi-key-variant</v-icon>
            <span>{{ t('apiKeys.tabTitle') }}</span>
          </v-btn>
          <v-btn value="logs" class="nav-btn" :size="$vuetify.display.mobile ? 'small' : 'default'">
            <v-icon start :size="$vuetify.display.mobile ? 16 : 20">mdi-format-list-bulleted</v-icon>
            <span>Logs</span>
          </v-btn>
        </v-btn-toggle>
      </div>

      <v-spacer></v-spacer>

      <!-- 版本号 -->
      <span v-if="appVersion" class="version-badge mr-2">{{ appVersion }}</span>

      <!-- 语言切换 -->
      <v-btn icon variant="text" size="small" class="header-btn" @click="toggleLocale" :title="currentLocale === 'zh-CN' ? 'English' : '中文'">
        <span style="font-size: 14px; font-weight: 600;">{{ currentLocale === 'zh-CN' ? 'EN' : '中' }}</span>
      </v-btn>

      <!-- 暗色模式切换 -->
      <v-btn icon variant="text" size="small" class="header-btn" @click="toggleDarkMode">
        <v-icon size="20">{{
          theme.global.current.value.dark ? 'mdi-weather-night' : 'mdi-white-balance-sunny'
        }}</v-icon>
      </v-btn>

      <!-- 注销按钮 -->
      <v-btn
        icon
        variant="text"
        size="small"
        class="header-btn"
        @click="handleLogout"
        v-if="isAuthenticated"
        :title="t('app.logout')"
      >
        <v-icon size="20">mdi-logout</v-icon>
      </v-btn>
    </v-app-bar>

    <!-- 主要内容 -->
    <v-main>
      <v-container fluid class="pa-4 pa-md-6">
        <!-- Logs 视图 -->
        <template v-if="activeTab === 'logs'">
          <!-- 全局统计图表 - 可折叠 -->
          <v-card v-if="showGlobalStatsChart" elevation="0" class="global-chart-card mb-6">
            <GlobalStatsChart ref="globalStatsChartRef" :from="logsDateRange?.from" :to="logsDateRange?.to" />
            <div class="chart-collapse-btn">
              <v-btn
                icon
                size="x-small"
                variant="text"
                @click="showGlobalStatsChart = false"
                :title="t('common.close')"
              >
                <v-icon size="small">mdi-chevron-up</v-icon>
              </v-btn>
            </div>
          </v-card>
          <div v-else class="chart-expand-bar mb-4">
            <v-btn
              variant="text"
              size="small"
              @click="showGlobalStatsChart = true"
              prepend-icon="mdi-chart-line"
              class="expand-chart-btn"
            >
              {{ t('chart.view.traffic') }}
            </v-btn>
          </div>

          <RequestLogTable @date-range-change="onLogsDateRangeChange" />
        </template>

        <!-- API Keys 视图 -->
        <APIKeyManagement v-if="activeTab === 'apikeys'" />

        <!-- 渠道管理视图 -->
        <template v-if="activeTab !== 'logs' && activeTab !== 'apikeys'">
        <!-- 统计卡片 - 玻璃拟态风格 -->
        <v-row class="mb-6 stat-cards-row">
          <v-col cols="6" sm="4">
            <div class="stat-card stat-card-info">
              <div class="stat-card-icon">
                <v-icon size="28">mdi-server-network</v-icon>
              </div>
              <div class="stat-card-content">
                <div class="stat-card-value">{{ currentChannelsData.channels?.length || 0 }}</div>
                <div class="stat-card-label">{{ t('stats.totalChannels') }}</div>
                <div class="stat-card-desc">{{ t('stats.configuredChannels') }}</div>
              </div>
              <div class="stat-card-glow"></div>
            </div>
          </v-col>

          <v-col cols="6" sm="4">
            <div class="stat-card stat-card-success">
              <div class="stat-card-icon">
                <v-icon size="28">mdi-check-circle</v-icon>
              </div>
              <div class="stat-card-content">
                <div class="stat-card-value">
                  {{ activeChannelCount }}<span class="stat-card-total">/{{ failoverChannelCount }}</span>
                </div>
                <div class="stat-card-label">{{ t('stats.activeChannels') }}</div>
                <div class="stat-card-desc">{{ t('stats.failoverScheduling') }}</div>
              </div>
              <div class="stat-card-glow"></div>
            </div>
          </v-col>

          <v-col cols="6" sm="4">
            <div class="stat-card stat-card-emerald">
              <div class="stat-card-icon">
                <v-icon size="28">mdi-chart-line</v-icon>
              </div>
              <div class="stat-card-content">
                <div class="stat-card-value">--</div>
                <div class="stat-card-label">--</div>
                <div class="stat-card-desc">--</div>
              </div>
              <div class="stat-card-glow"></div>
            </div>
          </v-col>
        </v-row>

        <!-- 操作按钮区域 - 现代化设计 -->
        <div class="action-bar mb-6">
          <div class="action-bar-left">
            <v-btn
              color="primary"
              size="large"
              @click="openAddChannelModal"
              prepend-icon="mdi-plus"
              class="action-btn action-btn-primary"
            >
              {{ t('actions.addChannel') }}
            </v-btn>

            <v-btn
              color="info"
              size="large"
              @click="pingAllChannels"
              prepend-icon="mdi-speedometer"
              variant="tonal"
              :loading="isPingingAll"
              class="action-btn"
            >
              {{ t('actions.testLatency') }}
            </v-btn>

          </div>

          <div class="action-bar-right">
            <!-- 负载均衡选择 -->
            <v-menu>
              <template v-slot:activator="{ props }">
                <v-btn
                  v-bind="props"
                  variant="tonal"
                  size="large"
                  append-icon="mdi-chevron-down"
                  class="action-btn load-balance-btn"
                >
                  <v-icon start size="20">mdi-tune</v-icon>
                  {{ currentChannelsData.loadBalance }}
                </v-btn>
              </template>
              <v-list class="load-balance-menu" rounded="lg" elevation="8">
                <v-list-subheader>{{ t('loadBalance.title') }}</v-list-subheader>
                <v-list-item
                  @click="updateLoadBalance('round-robin')"
                  :active="currentChannelsData.loadBalance === 'round-robin'"
                  rounded="lg"
                >
                  <template v-slot:prepend>
                    <v-avatar color="info" size="36" variant="tonal">
                      <v-icon size="20">mdi-rotate-right</v-icon>
                    </v-avatar>
                  </template>
                  <v-list-item-title class="font-weight-medium">{{ t('loadBalance.roundRobin') }}</v-list-item-title>
                  <v-list-item-subtitle>{{ t('loadBalance.roundRobinDesc') }}</v-list-item-subtitle>
                </v-list-item>
                <v-list-item
                  @click="updateLoadBalance('random')"
                  :active="currentChannelsData.loadBalance === 'random'"
                  rounded="lg"
                >
                  <template v-slot:prepend>
                    <v-avatar color="secondary" size="36" variant="tonal">
                      <v-icon size="20">mdi-dice-6</v-icon>
                    </v-avatar>
                  </template>
                  <v-list-item-title class="font-weight-medium">{{ t('loadBalance.random') }}</v-list-item-title>
                  <v-list-item-subtitle>{{ t('loadBalance.randomDesc') }}</v-list-item-subtitle>
                </v-list-item>
                <v-list-item
                  @click="updateLoadBalance('failover')"
                  :active="currentChannelsData.loadBalance === 'failover'"
                  rounded="lg"
                >
                  <template v-slot:prepend>
                    <v-avatar color="warning" size="36" variant="tonal">
                      <v-icon size="20">mdi-backup-restore</v-icon>
                    </v-avatar>
                  </template>
                  <v-list-item-title class="font-weight-medium">{{ t('loadBalance.failover') }}</v-list-item-title>
                  <v-list-item-subtitle>{{ t('loadBalance.failoverDesc') }}</v-list-item-subtitle>
                </v-list-item>
              </v-list>
            </v-menu>
          </div>
        </div>

        <!-- 渠道编排（高密度列表模式） -->
        <ChannelOrchestration
          v-if="currentChannelsData.channels?.length"
          ref="channelOrchestrationRef"
          :channels="currentChannelsData.channels"
          :current-channel-index="currentChannelsData.current"
          :channel-type="channelTypeForComponents"
          @edit="editChannel"
          @delete="deleteChannel"
          @ping="pingChannel"
          @refresh="refreshChannels"
          @error="showErrorToast"
          @success="showSuccessToast"
          class="mb-6"
        />

        <!-- 空状态 -->
        <v-card v-if="!currentChannelsData.channels?.length" elevation="2" class="text-center pa-12" rounded="lg">
          <v-avatar size="120" color="primary" class="mb-6">
            <v-icon size="60" color="white">mdi-rocket-launch</v-icon>
          </v-avatar>
          <div class="text-h4 mb-4 font-weight-bold">{{ t('channel.noChannels') }}</div>
          <div class="text-subtitle-1 text-medium-emphasis mb-8">
            {{ t('channel.noChannelsDesc') }}
          </div>
          <v-btn color="primary" size="x-large" @click="openAddChannelModal" prepend-icon="mdi-plus" variant="elevated">
            {{ t('actions.addFirstChannel') }}
          </v-btn>
        </v-card>
        </template>
      </v-container>
    </v-main>

    <!-- 添加渠道模态框 -->
    <AddChannelModal
      v-model:show="showAddChannelModal"
      :channel="editingChannel"
      :channel-type="channelTypeForComponents"
      :all-channels="channels"
      @save="saveChannel"
    />

    <!-- 添加API密钥对话框 -->
    <v-dialog v-model="showAddKeyModalRef" max-width="500">
      <v-card rounded="lg">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-3">mdi-key-plus</v-icon>
          {{ t('channel.addApiKey') }}
        </v-card-title>
        <v-card-text>
          <v-text-field
            v-model="newApiKey"
            :label="t('channel.apiKeyLabel')"
            type="password"
            variant="outlined"
            density="comfortable"
            @keyup.enter="addApiKey"
            :placeholder="t('channel.enterApiKey')"
          ></v-text-field>
        </v-card-text>
        <v-card-actions>
          <v-spacer></v-spacer>
          <v-btn @click="showAddKeyModalRef = false" variant="text">{{ t('common.cancel') }}</v-btn>
          <v-btn @click="addApiKey" :disabled="!newApiKey.trim()" color="primary" variant="elevated">{{ t('common.add') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 删除渠道确认对话框 -->
    <v-dialog v-model="showDeleteChannelConfirm" max-width="400">
      <v-card>
        <v-card-title class="text-error d-flex align-center">
          <v-icon class="mr-2" color="error">mdi-alert-circle</v-icon>
          {{ t('confirm.deleteChannel') }}
        </v-card-title>
        <v-card-text>{{ t('channel.deleteConfirm') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showDeleteChannelConfirm = false" :disabled="isDeleting">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" @click="confirmDeleteChannel" :loading="isDeleting">{{ t('common.delete') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 删除API密钥确认对话框 -->
    <v-dialog v-model="showDeleteApiKeyConfirm" max-width="400">
      <v-card>
        <v-card-title class="text-warning d-flex align-center">
          <v-icon class="mr-2" color="warning">mdi-key-remove</v-icon>
          {{ t('confirm.deleteApiKey') }}
        </v-card-title>
        <v-card-text>{{ t('channel.apiKeyDeleteConfirm') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showDeleteApiKeyConfirm = false" :disabled="isDeleting">{{ t('common.cancel') }}</v-btn>
          <v-btn color="warning" variant="flat" @click="confirmDeleteApiKey" :loading="isDeleting">{{ t('common.delete') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 定价设置对话框 -->
    <PricingSettings v-model="showPricingSettings" />

    <!-- 速率限制设置对话框 -->
    <RateLimitSettings v-model="showRateLimitSettings" />

    <!-- 调试日志设置对话框 -->
    <DebugLogSettings v-model="showDebugLogSettings" />

    <!-- 故障转移设置对话框 -->
    <FailoverSettings v-model="showFailoverSettings" />

    <!-- 备份恢复对话框 -->
    <v-dialog v-model="showBackupRestore" max-width="600">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-3">mdi-backup-restore</v-icon>
          {{ t('backup.title') }}
        </v-card-title>
        <v-card-text>
          <!-- 创建备份按钮 -->
          <v-btn
            color="primary"
            variant="elevated"
            block
            size="large"
            class="mb-4"
            :loading="isCreatingBackup"
            @click="createBackup"
            prepend-icon="mdi-content-save"
          >
            {{ t('backup.createBackup') }}
          </v-btn>

          <v-divider class="mb-4" />

          <!-- 备份列表 -->
          <div class="text-subtitle-1 font-weight-medium mb-2">{{ t('backup.backupList') }}</div>

          <v-progress-linear v-if="isLoadingBackups" indeterminate color="primary" class="mb-4" />

          <v-alert v-else-if="backupList.length === 0" type="info" variant="tonal" density="compact" class="mb-0">
            {{ t('backup.noBackups') }}
          </v-alert>

          <v-list v-else density="compact" class="backup-list">
            <v-list-item
              v-for="backup in backupList"
              :key="backup.filename"
              class="backup-item mb-2"
            >
              <template #prepend>
                <v-icon color="primary">mdi-file-document</v-icon>
              </template>
              <v-list-item-title class="text-body-2 font-weight-medium">
                {{ formatBackupDate(backup.createdAt) }}
              </v-list-item-title>
              <v-list-item-subtitle class="text-caption">
                {{ formatFileSize(backup.size) }}
              </v-list-item-subtitle>
              <template #append>
                <v-btn
                  icon
                  variant="text"
                  size="small"
                  color="success"
                  :title="t('backup.restore')"
                  @click="confirmRestore(backup.filename)"
                >
                  <v-icon>mdi-restore</v-icon>
                </v-btn>
                <v-btn
                  icon
                  variant="text"
                  size="small"
                  color="error"
                  :title="t('common.delete')"
                  @click="deleteBackup(backup.filename)"
                >
                  <v-icon>mdi-delete</v-icon>
                </v-btn>
              </template>
            </v-list-item>
          </v-list>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showBackupRestore = false">{{ t('common.close') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 恢复备份确认对话框 -->
    <v-dialog v-model="showRestoreConfirm" max-width="400">
      <v-card>
        <v-card-title class="text-warning d-flex align-center">
          <v-icon class="mr-2" color="warning">mdi-alert-circle</v-icon>
          {{ t('backup.confirmRestore') }}
        </v-card-title>
        <v-card-text>{{ t('backup.confirmRestoreDesc') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showRestoreConfirm = false" :disabled="isRestoringBackup">{{ t('common.cancel') }}</v-btn>
          <v-btn color="warning" variant="flat" @click="executeRestore" :loading="isRestoringBackup">{{ t('backup.restore') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Toast通知 -->
    <v-snackbar
      v-for="toast in toasts"
      :key="toast.id"
      v-model="toast.show"
      :color="getToastColor(toast.type)"
      :timeout="3000"
      location="top right"
      variant="elevated"
    >
      <div class="d-flex align-center">
        <v-icon class="mr-3">{{ getToastIcon(toast.type) }}</v-icon>
        {{ toast.message }}
      </div>
    </v-snackbar>
  </v-app>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { useTheme } from 'vuetify'
import { useI18n } from 'vue-i18n'
import { api, type Channel, type ChannelsResponse } from './services/api'
import AddChannelModal from './components/AddChannelModal.vue'
import ChannelOrchestration from './components/ChannelOrchestration.vue'
import RequestLogTable from './components/RequestLogTable.vue'
import APIKeyManagement from './components/APIKeyManagement.vue'
import PricingSettings from './components/PricingSettings.vue'
import RateLimitSettings from './components/RateLimitSettings.vue'
import DebugLogSettings from './components/DebugLogSettings.vue'
import FailoverSettings from './components/FailoverSettings.vue'
import GlobalStatsChart from './components/GlobalStatsChart.vue'
import { useAppTheme } from './composables/useTheme'
import { useLocale } from './composables/useLocale'

// i18n
const { t } = useI18n()

// Locale management
const { currentLocale, toggleLocale, init: initLocale } = useLocale()

// Vuetify主题
const theme = useTheme()

// 应用主题系统
const { init: initTheme } = useAppTheme()

// 渠道编排组件引用
const channelOrchestrationRef = ref<InstanceType<typeof ChannelOrchestration> | null>(null)

// 自动刷新定时器
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null
const AUTO_REFRESH_INTERVAL = 2000 // 2秒

// 响应式数据
const activeTab = ref<'messages' | 'responses' | 'logs' | 'apikeys'>('messages') // Tab 切换状态
const channelsData = ref<ChannelsResponse>({ channels: [], current: -1, loadBalance: 'round-robin' })
const responsesChannelsData = ref<ChannelsResponse>({ channels: [], current: -1, loadBalance: 'round-robin' }) // Responses渠道数据
const showAddChannelModal = ref(false)
const showAddKeyModalRef = ref(false)
const editingChannel = ref<Channel | null>(null)
const selectedChannelForKey = ref<number>(-1)
const newApiKey = ref('')
const isPingingAll = ref(false)
const darkModePreference = ref<'light' | 'dark' | 'auto'>('auto')
const appVersion = ref('') // 应用版本号
const showPricingSettings = ref(false) // 定价设置对话框
const showRateLimitSettings = ref(false) // 速率限制设置对话框
const showDebugLogSettings = ref(false) // 调试日志设置对话框
const showFailoverSettings = ref(false) // 故障转移设置对话框
const showBackupRestore = ref(false) // 备份恢复对话框
const showGlobalStatsChart = ref(false) // 全局统计图表显示状态

// GlobalStatsChart 组件引用
const globalStatsChartRef = ref<InstanceType<typeof GlobalStatsChart> | null>(null)

type LogDateRange = { from: string; to: string }
const logsDateRange = ref<LogDateRange | null>(null)
const onLogsDateRangeChange = (range: LogDateRange) => {
  logsDateRange.value = range
}

// 备份恢复相关状态
const backupList = ref<Array<{ filename: string; createdAt: string; size: number }>>([])
const isLoadingBackups = ref(false)
const isCreatingBackup = ref(false)
const isRestoringBackup = ref(false)
const showRestoreConfirm = ref(false)
const pendingRestoreFilename = ref<string | null>(null)

// 确认对话框状态
const showDeleteChannelConfirm = ref(false)
const showDeleteApiKeyConfirm = ref(false)
const pendingDeleteChannelId = ref<number | null>(null)
const pendingDeleteApiKey = ref<{ channelId: number; apiKey: string } | null>(null)
const isDeleting = ref(false)

// 用于传递给子组件的 channelType (排除 'logs' 和 'apikeys')
const channelTypeForComponents = computed((): 'messages' | 'responses' => {
  if (activeTab.value === 'logs' || activeTab.value === 'apikeys') {
    return 'messages'
  }
  return activeTab.value
})

// All channels for composite channel editor (uses messages channels for composite targets)
const channels = computed(() => channelsData.value.channels)

// Toast通知系统
interface Toast {
  id: number
  message: string
  type: 'success' | 'error' | 'warning' | 'info'
  show?: boolean
}
const toasts = ref<Toast[]>([])
let toastId = 0

// 计算属性 - 根据当前Tab动态返回数据
const currentChannelsData = computed(() => {
  return activeTab.value === 'messages' ? channelsData.value : responsesChannelsData.value
})

// 计算属性：活跃渠道数（仅 active 状态）
const activeChannelCount = computed(() => {
  const data = currentChannelsData.value
  if (!data.channels) return 0
  return data.channels.filter(ch => ch.status === 'active').length
})

// 计算属性：参与故障转移的渠道数（active + suspended）
const failoverChannelCount = computed(() => {
  const data = currentChannelsData.value
  if (!data.channels) return 0
  return data.channels.filter(ch => ch.status !== 'disabled').length
})

// Toast工具函数
const getToastColor = (type: string) => {
  const colorMap: Record<string, string> = {
    success: 'success',
    error: 'error',
    warning: 'warning',
    info: 'info'
  }
  return colorMap[type] || 'info'
}

const getToastIcon = (type: string) => {
  const iconMap: Record<string, string> = {
    success: 'mdi-check-circle',
    error: 'mdi-alert-circle',
    warning: 'mdi-alert',
    info: 'mdi-information'
  }
  return iconMap[type] || 'mdi-information'
}

// 工具函数
const showToast = (message: string, type: 'success' | 'error' | 'warning' | 'info' = 'info') => {
  const toast: Toast = { id: ++toastId, message, type, show: true }
  toasts.value.push(toast)
  setTimeout(() => {
    const index = toasts.value.findIndex(t => t.id === toast.id)
    if (index > -1) toasts.value.splice(index, 1)
  }, 3000)
}

const handleError = (error: unknown, defaultMessage: string) => {
  const message = error instanceof Error ? error.message : defaultMessage
  showToast(message, 'error')
  console.error(error)
}

// 直接显示错误消息（供子组件事件使用）
const showErrorToast = (message: string) => {
  showToast(message, 'error')
}

// 直接显示成功消息（供子组件事件使用）
const showSuccessToast = (message: string) => {
  showToast(message, 'info')
}

// 主要功能函数
const refreshChannels = async () => {
  try {
    if (activeTab.value === 'messages') {
      channelsData.value = await api.getChannels()
    } else {
      responsesChannelsData.value = await api.getResponsesChannels()
    }
  } catch (error) {
    handleAuthError(error)
  }
}

const saveChannel = async (channel: Omit<Channel, 'index' | 'latency' | 'status'>, options?: { isQuickAdd?: boolean }) => {
  try {
    const isResponses = activeTab.value === 'responses'
    if (editingChannel.value) {
      const { apiKeys, ...channelUpdate } = channel

      if (isResponses) {
        await api.updateResponsesChannel(editingChannel.value.index, channelUpdate)
      } else {
        await api.updateChannel(editingChannel.value.index, channelUpdate)
      }

      const keysToAdd = (apiKeys || []).map(k => k.trim()).filter(Boolean)
      const keyAddErrors: string[] = []
      for (const key of keysToAdd) {
        try {
          if (isResponses) {
            await api.addResponsesApiKey(editingChannel.value.index, key)
          } else {
            await api.addApiKey(editingChannel.value.index, key)
          }
        } catch (err) {
          const message = err instanceof Error ? err.message : String(err)
          if (message.includes('认证失败')) {
            throw err
          }
          keyAddErrors.push(message)
        }
      }

      showToast(t('channel.updateSuccess'), 'success')
      if (keyAddErrors.length > 0) {
        showToast(keyAddErrors[0], 'warning')
      }
      // Refresh channels to get updated data from server
      await refreshChannels()
    } else {
      if (isResponses) {
        await api.addResponsesChannel(channel)
      } else {
        await api.addChannel(channel)
      }
      showToast(t('channel.addSuccess'), 'success')

      // 快速添加模式：将新渠道设为第一优先级并设置5分钟促销期
      if (options?.isQuickAdd) {
        await refreshChannels() // 先刷新获取新渠道的 index
        const data = isResponses ? responsesChannelsData.value : channelsData.value

        // 找到新添加的渠道（应该是列表中 index 最大的 active 状态渠道）
        const activeChannels = data.channels?.filter(ch => ch.status !== 'disabled') || []
        if (activeChannels.length > 0) {
          // 新添加的渠道会分配到最大的 index
          const newChannel = activeChannels.reduce((max, ch) => ch.index > max.index ? ch : max, activeChannels[0])

          try {
            // 1. 重新排序：将新渠道放到第一位
            const otherIndexes = activeChannels
              .filter(ch => ch.index !== newChannel.index)
              .sort((a, b) => (a.priority ?? a.index) - (b.priority ?? b.index))
              .map(ch => ch.index)
            const newOrder = [newChannel.index, ...otherIndexes]

            if (isResponses) {
              await api.reorderResponsesChannels(newOrder)
            } else {
              await api.reorderChannels(newOrder)
            }

            // 2. 设置5分钟促销期（300秒）
            if (isResponses) {
              await api.setResponsesChannelPromotion(newChannel.index, 300)
            } else {
              await api.setChannelPromotion(newChannel.index, 300)
            }

            showToast(t('channel.prioritySet', { name: channel.name }), 'info')
          } catch (err) {
            console.warn('设置快速添加优先级失败:', err)
            // 不影响主流程，只是提示
          }
        }
      }
    }
    showAddChannelModal.value = false
    editingChannel.value = null
    await refreshChannels()
  } catch (error) {
    handleAuthError(error)
  }
}

const editChannel = (channel: Channel) => {
  editingChannel.value = channel
  showAddChannelModal.value = true
}

const deleteChannel = (channelId: number) => {
  pendingDeleteChannelId.value = channelId
  showDeleteChannelConfirm.value = true
}

const confirmDeleteChannel = async () => {
  if (pendingDeleteChannelId.value === null) return

  isDeleting.value = true
  try {
    if (activeTab.value === 'responses') {
      await api.deleteResponsesChannel(pendingDeleteChannelId.value)
    } else {
      await api.deleteChannel(pendingDeleteChannelId.value)
    }
    showToast(t('channel.deleteSuccess'), 'success')
    await refreshChannels()
  } catch (error) {
    handleAuthError(error)
  } finally {
    isDeleting.value = false
    showDeleteChannelConfirm.value = false
    pendingDeleteChannelId.value = null
  }
}

const openAddChannelModal = () => {
  editingChannel.value = null
  showAddChannelModal.value = true
}

const openAddKeyModal = (channelId: number) => {
  selectedChannelForKey.value = channelId
  newApiKey.value = ''
  showAddKeyModalRef.value = true
}

const addApiKey = async () => {
  if (!newApiKey.value.trim()) return

  try {
    if (activeTab.value === 'responses') {
      await api.addResponsesApiKey(selectedChannelForKey.value, newApiKey.value.trim())
    } else {
      await api.addApiKey(selectedChannelForKey.value, newApiKey.value.trim())
    }
    showToast(t('channel.apiKeyAddSuccess'), 'success')
    showAddKeyModalRef.value = false
    newApiKey.value = ''
    await refreshChannels()
  } catch (error) {
    showToast(t('channel.apiKeyAddFailed', { error: error instanceof Error ? error.message : 'Unknown error' }), 'error')
  }
}

const removeApiKey = (channelId: number, apiKey: string) => {
  pendingDeleteApiKey.value = { channelId, apiKey }
  showDeleteApiKeyConfirm.value = true
}

const confirmDeleteApiKey = async () => {
  if (!pendingDeleteApiKey.value) return

  isDeleting.value = true
  try {
    const { channelId, apiKey } = pendingDeleteApiKey.value
    if (activeTab.value === 'responses') {
      await api.removeResponsesApiKey(channelId, apiKey)
    } else {
      await api.removeApiKey(channelId, apiKey)
    }
    showToast(t('channel.apiKeyDeleteSuccess'), 'success')
    await refreshChannels()
  } catch (error) {
    showToast(t('channel.apiKeyDeleteFailed', { error: error instanceof Error ? error.message : 'Unknown error' }), 'error')
  } finally {
    isDeleting.value = false
    showDeleteApiKeyConfirm.value = false
    pendingDeleteApiKey.value = null
  }
}

const pingChannel = async (channelId: number) => {
  try {
    const result = await api.pingChannel(channelId)
    const data = activeTab.value === 'messages' ? channelsData.value : responsesChannelsData.value
    const channel = data.channels?.find(c => c.index === channelId)
    if (channel) {
      channel.latency = result.latency
      channel.status = result.success ? 'healthy' : 'error'
    }
    showToast(t('channel.latencyTestComplete', { latency: result.latency }), result.success ? 'success' : 'warning')
  } catch (error) {
    showToast(t('channel.latencyTestFailed', { error: error instanceof Error ? error.message : 'Unknown error' }), 'error')
  }
}

const pingAllChannels = async () => {
  if (isPingingAll.value) return

  isPingingAll.value = true
  try {
    const results = await api.pingAllChannels()
    const data = activeTab.value === 'messages' ? channelsData.value : responsesChannelsData.value
    results.forEach(result => {
      const channel = data.channels?.find(c => c.index === result.id)
      if (channel) {
        channel.latency = result.latency
        channel.status = result.status as 'healthy' | 'error'
      }
    })
    showToast(t('channel.allLatencyTestComplete'), 'success')
  } catch (error) {
    showToast(t('channel.batchLatencyTestFailed', { error: error instanceof Error ? error.message : 'Unknown error' }), 'error')
  } finally {
    isPingingAll.value = false
  }
}

const updateLoadBalance = async (strategy: string) => {
  try {
    if (activeTab.value === 'messages') {
      await api.updateLoadBalance(strategy)
      channelsData.value.loadBalance = strategy
    } else {
      await api.updateResponsesLoadBalance(strategy)
      responsesChannelsData.value.loadBalance = strategy
    }
    showToast(t('loadBalance.updated', { strategy }), 'success')
  } catch (error) {
    showToast(t('loadBalance.updateFailed', { error: error instanceof Error ? error.message : 'Unknown error' }), 'error')
  }
}

// 主题管理
const toggleDarkMode = () => {
  const newMode = darkModePreference.value === 'dark' ? 'light' : 'dark'
  setDarkMode(newMode)
}

const setDarkMode = (themeName: 'light' | 'dark' | 'auto') => {
  darkModePreference.value = themeName
  const apply = (isDark: boolean) => {
    theme.global.name.value = isDark ? 'dark' : 'light'
  }

  if (themeName === 'auto') {
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
    apply(prefersDark)
  } else {
    apply(themeName === 'dark')
  }

  localStorage.setItem('theme', themeName)
}

// 认证状态管理
const isAuthenticated = ref(false)
const authError = ref('')
const authKeyInput = ref('')
const authLoading = ref(false)
const isAutoAuthenticating = ref(true) // 初始化为true，防止登录框闪现
const isInitialized = ref(false) // 添加初始化完成标志

// 认证尝试限制
const authAttempts = ref(0)
const MAX_AUTH_ATTEMPTS = 5
const authLockoutTime = ref<Date | null>(null)

// 控制认证对话框显示
const showAuthDialog = computed({
  get: () => {
    // 只有在初始化完成后，且未认证，且不在自动认证中时，才显示对话框
    return isInitialized.value && !isAuthenticated.value && !isAutoAuthenticating.value
  },
  set: () => {} // 防止外部修改，认证状态只能通过内部逻辑控制
})

// 初始化认证 - 只负责从存储获取密钥
const initializeAuth = () => {
  const key = api.initializeAuth()
  return key
}

// 自动验证保存的密钥
const autoAuthenticate = async () => {
  const savedKey = initializeAuth()
  if (!savedKey) {
    // 没有保存的密钥，显示登录对话框
    authError.value = t('auth.enterKeyToContinue')
    isAutoAuthenticating.value = false
    isInitialized.value = true
    return false
  }

  // 有保存的密钥，尝试自动认证
  try {
    // 尝试调用API验证密钥是否有效
    await api.getChannels()

    // 密钥有效，设置认证状态
    isAuthenticated.value = true
    authError.value = ''

    return true
  } catch (error: any) {
    // 密钥无效或过期
    console.warn('Auto auth failed:', error.message)

    // 清除无效的密钥
    api.clearAuth()

    // 显示登录对话框，提示用户重新输入
    isAuthenticated.value = false
    authError.value = t('auth.savedKeyInvalid')

    return false
  } finally {
    isAutoAuthenticating.value = false
    isInitialized.value = true
  }
}

// 手动设置密钥（用于重新认证）
const setAuthKey = (key: string) => {
  api.setApiKey(key)
  sessionStorage.setItem('proxyAccessKey', key)
  isAuthenticated.value = true
  authError.value = ''
  // 重新加载数据
  refreshChannels()
}

// 处理认证提交
const handleAuthSubmit = async () => {
  if (!authKeyInput.value.trim()) {
    authError.value = t('auth.enterAccessKey')
    return
  }

  // 检查是否被锁定
  if (authLockoutTime.value && new Date() < authLockoutTime.value) {
    const remainingSeconds = Math.ceil((authLockoutTime.value.getTime() - Date.now()) / 1000)
    authError.value = t('auth.waitAndRetry', { seconds: remainingSeconds })
    return
  }

  authLoading.value = true
  authError.value = ''

  try {
    // 设置密钥
    setAuthKey(authKeyInput.value.trim())

    // 测试API调用以验证密钥
    await api.getChannels()

    // 认证成功，重置计数器
    authAttempts.value = 0
    authLockoutTime.value = null

    // 如果成功，加载数据
    await refreshChannels()

    authKeyInput.value = ''

    // 记录认证成功(前端日志)
    console.info('✅ Auth success - time:', new Date().toISOString())
  } catch (error: any) {
    // 认证失败
    authAttempts.value++

    // 记录认证失败(前端日志)
    console.warn('🔒 Auth failed - attempts:', authAttempts.value, 'time:', new Date().toISOString())

    // 如果尝试次数过多，锁定5分钟
    if (authAttempts.value >= MAX_AUTH_ATTEMPTS) {
      authLockoutTime.value = new Date(Date.now() + 5 * 60 * 1000)
      authError.value = t('auth.tooManyAttempts')
    } else {
      authError.value = t('auth.authFailed', { remaining: MAX_AUTH_ATTEMPTS - authAttempts.value })
    }

    isAuthenticated.value = false
    api.clearAuth()
  } finally {
    authLoading.value = false
  }
}

// 处理注销
const handleLogout = () => {
  api.clearAuth()
  isAuthenticated.value = false
  authError.value = t('auth.enterKeyToContinue')
  channelsData.value = { channels: [], current: 0, loadBalance: 'failover' }
  showToast(t('app.loggedOut'), 'info')
}

// 处理认证失败
const handleAuthError = (error: any) => {
  if (error.message && error.message.includes('认证失败')) {
    isAuthenticated.value = false
    authError.value = t('auth.savedKeyInvalid')
  } else {
    showToast(`${t('common.error')}: ${error instanceof Error ? error.message : t('common.unknown')}`, 'error')
  }
}

// ============== 备份恢复功能 ==============

// 打开备份恢复对话框
const openBackupRestore = async () => {
  showBackupRestore.value = true
  await loadBackupList()
}

// 加载备份列表
const loadBackupList = async () => {
  isLoadingBackups.value = true
  try {
    const response = await api.listBackups()
    backupList.value = response.backups || []
  } catch (error) {
    showToast(t('backup.loadFailed'), 'error')
    console.error('Failed to load backups:', error)
  } finally {
    isLoadingBackups.value = false
  }
}

// 创建备份
const createBackup = async () => {
  isCreatingBackup.value = true
  try {
    const result = await api.createBackup()
    showToast(t('backup.createSuccess'), 'success')
    // 刷新列表
    await loadBackupList()
  } catch (error) {
    showToast(t('backup.createFailed'), 'error')
    console.error('Failed to create backup:', error)
  } finally {
    isCreatingBackup.value = false
  }
}

// 确认恢复备份
const confirmRestore = (filename: string) => {
  pendingRestoreFilename.value = filename
  showRestoreConfirm.value = true
}

// 执行恢复备份
const executeRestore = async () => {
  if (!pendingRestoreFilename.value) return

  isRestoringBackup.value = true
  try {
    await api.restoreBackup(pendingRestoreFilename.value)
    showToast(t('backup.restoreSuccess'), 'success')
    showRestoreConfirm.value = false
    showBackupRestore.value = false
    // 刷新渠道数据
    await refreshChannels()
  } catch (error) {
    showToast(t('backup.restoreFailed'), 'error')
    console.error('Failed to restore backup:', error)
  } finally {
    isRestoringBackup.value = false
    pendingRestoreFilename.value = null
  }
}

// 删除备份
const deleteBackup = async (filename: string) => {
  try {
    await api.deleteBackup(filename)
    showToast(t('backup.deleteSuccess'), 'success')
    await loadBackupList()
  } catch (error) {
    showToast(t('backup.deleteFailed'), 'error')
    console.error('Failed to delete backup:', error)
  }
}

// 格式化文件大小
const formatFileSize = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

// 格式化日期
const formatBackupDate = (dateString: string): string => {
  const date = new Date(dateString)
  return date.toLocaleString()
}

// 键盘快捷键处理
const handleKeydown = (event: KeyboardEvent) => {
  // 如果正在输入框中，忽略快捷键
  const target = event.target as HTMLElement
  if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) {
    return
  }

  // Esc 关闭所有对话框
  if (event.key === 'Escape') {
    showAddChannelModal.value = false
    showAddKeyModalRef.value = false
    showDeleteChannelConfirm.value = false
    showDeleteApiKeyConfirm.value = false
    showPricingSettings.value = false
    showRateLimitSettings.value = false
    showDebugLogSettings.value = false
    showFailoverSettings.value = false
    showBackupRestore.value = false
    showRestoreConfirm.value = false
    return
  }

  // 数字键切换 Tab（仅在已认证时）
  if (isAuthenticated.value) {
    if (event.key === '1') {
      activeTab.value = 'messages'
    } else if (event.key === '2') {
      activeTab.value = 'responses'
    } else if (event.key === '3') {
      activeTab.value = 'apikeys'
    } else if (event.key === '4') {
      activeTab.value = 'logs'
    }
  }
}

// 初始化
onMounted(async () => {
  // 注册键盘快捷键
  window.addEventListener('keydown', handleKeydown)

  // 初始化复古像素主题
  document.documentElement.dataset.theme = 'retro'
  initTheme()
  initLocale()

  // 加载保存的暗色模式偏好
  const savedMode = (localStorage.getItem('theme') as 'light' | 'dark' | 'auto') || 'auto'
  setDarkMode(savedMode)

  // 监听系统主题变化
  const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
  const handlePref = () => {
    if (darkModePreference.value === 'auto') setDarkMode('auto')
  }
  mediaQuery.addEventListener('change', handlePref)

  // 检查是否有保存的密钥
  const savedKey = initializeAuth()

  if (savedKey) {
    // 有保存的密钥，开始自动认证
    isAutoAuthenticating.value = true
    isInitialized.value = false
  } else {
    // 没有保存的密钥，直接显示登录对话框
    isAutoAuthenticating.value = false
    isInitialized.value = true
  }

  // 尝试自动认证
  const authenticated = await autoAuthenticate()

  if (authenticated) {
    // 加载渠道数据
    await refreshChannels()
    // 启动自动刷新
    startAutoRefresh()
    // 获取版本信息
    try {
      const versionInfo = await api.getVersion()
      appVersion.value = versionInfo.version
    } catch (e) {
      console.warn('Failed to fetch version:', e)
    }
  }
})

// 启动自动刷新定时器
const startAutoRefresh = () => {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
  }
  autoRefreshTimer = setInterval(async () => {
    if (isAuthenticated.value) {
      try {
        // 静默刷新渠道数据
        if (activeTab.value === 'messages') {
          channelsData.value = await api.getChannels()
        } else {
          responsesChannelsData.value = await api.getResponsesChannels()
        }
        // 同时刷新渠道指标
        channelOrchestrationRef.value?.refreshMetrics()
      } catch (error) {
        // 静默处理错误，避免刷新失败时干扰用户
        console.warn('自动刷新失败:', error)
      }
    }
  }, AUTO_REFRESH_INTERVAL)
}

// 停止自动刷新定时器
const stopAutoRefresh = () => {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}

// 监听 Tab 切换，刷新对应数据
watch(activeTab, async () => {
  if (isAuthenticated.value) {
    await refreshChannels()
    // 切换 Tab 时立即刷新指标
    channelOrchestrationRef.value?.refreshMetrics()
  }
})

// 监听认证状态变化
watch(isAuthenticated, newValue => {
  if (newValue) {
    startAutoRefresh()
  } else {
    stopAutoRefresh()
  }
})

// 在组件卸载时清除定时器和事件监听
onUnmounted(() => {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
  window.removeEventListener('keydown', handleKeydown)
})
</script>

<style scoped>
/* =====================================================
   🎮 复古像素 (Retro Pixel) 主题样式系统
   Neo-Brutalism: 直角、粗黑边框、硬阴影、等宽字体
   ===================================================== */

/* ----- 应用栏 - 复古像素风格 ----- */
.app-header {
  background: rgb(var(--v-theme-surface)) !important;
  border-bottom: 2px solid rgb(var(--v-theme-on-surface));
  transition: none;
  padding: 0 16px !important;
}

.v-theme--dark .app-header {
  background: rgb(var(--v-theme-surface)) !important;
  border-bottom: 2px solid rgba(255, 255, 255, 0.8);
}

/* 修复 Header 布局 */
.app-header :deep(.v-toolbar__prepend) {
  margin-inline-end: 4px !important;
}

.app-header .v-toolbar-title {
  overflow: hidden !important;
  min-width: 0 !important;
  flex: 1 !important;
}

.app-header :deep(.v-toolbar__content) {
  overflow: visible !important;
}

.app-header :deep(.v-toolbar__content > .v-toolbar-title) {
  min-width: 0 !important;
  margin-inline-start: 0 !important;
  margin-inline-end: auto !important;
}

.app-header :deep(.v-toolbar-title__placeholder) {
  width: 100%;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.app-logo {
  width: 42px;
  height: 42px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(var(--v-theme-primary));
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 3px 3px 0 0 rgb(var(--v-theme-on-surface));
  margin-right: 8px;
  transition: all 0.1s ease;
}

.app-logo:hover {
  transform: translate(-1px, -1px);
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface));
}

.app-logo:active {
  transform: translate(2px, 2px);
  box-shadow: none;
}

.v-theme--dark .app-logo {
  border-color: rgba(255, 255, 255, 0.8);
  box-shadow: 3px 3px 0 0 rgba(255, 255, 255, 0.8);
}

.v-theme--dark .app-logo:hover {
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.8);
}

.v-theme--dark .app-logo:active {
  box-shadow: none;
}

/* 自定义标题容器 */
.header-title {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

/* 导航按钮组 - 复古像素风格 */
.nav-toggle {
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 3px 3px 0 0 rgb(var(--v-theme-on-surface)) !important;
  border-radius: 0 !important;
  background: rgb(var(--v-theme-surface)) !important;
}

.v-theme--dark .nav-toggle {
  border-color: rgba(255, 255, 255, 0.7) !important;
  box-shadow: 3px 3px 0 0 rgba(255, 255, 255, 0.7) !important;
}

.nav-btn {
  border-radius: 0 !important;
  text-transform: none !important;
  font-weight: 600 !important;
  letter-spacing: 0 !important;
  border: none !important;
  min-width: 80px !important;
}

.nav-btn:not(:last-child) {
  border-right: 2px solid rgb(var(--v-theme-on-surface)) !important;
}

.v-theme--dark .nav-btn:not(:last-child) {
  border-right-color: rgba(255, 255, 255, 0.5) !important;
}

.nav-toggle .nav-btn.v-btn--active {
  background: rgb(var(--v-theme-primary)) !important;
  color: white !important;
}

.nav-toggle .nav-btn:not(.v-btn--active):hover {
  background: rgba(var(--v-theme-primary), 0.1) !important;
}

.version-badge {
  font-size: 12px;
  font-weight: 600;
  color: rgba(var(--v-theme-on-surface), 0.6);
  background: rgba(var(--v-theme-on-surface), 0.08);
  padding: 2px 8px;
  border: 1px solid rgba(var(--v-theme-on-surface), 0.2);
  font-family: 'Courier New', monospace;
}

.header-btn {
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 2px 2px 0 0 rgb(var(--v-theme-on-surface)) !important;
  margin-left: 4px;
  transition: all 0.1s ease !important;
}

.v-theme--dark .header-btn {
  border-color: rgba(255, 255, 255, 0.6) !important;
  box-shadow: 2px 2px 0 0 rgba(255, 255, 255, 0.6) !important;
}

.header-btn:hover {
  background: rgba(var(--v-theme-primary), 0.1);
  transform: translate(-1px, -1px);
  box-shadow: 3px 3px 0 0 rgb(var(--v-theme-on-surface)) !important;
}

.header-btn:active {
  transform: translate(2px, 2px) !important;
  box-shadow: none !important;
}

/* ----- 统计卡片 - 复古像素风格 ----- */
.stat-cards-row {
  margin-top: -8px;
}

/* ----- 全局统计图表卡片 ----- */
.global-chart-card {
  position: relative;
  background: rgb(var(--v-theme-surface));
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 6px 6px 0 0 rgb(var(--v-theme-on-surface));
}

.v-theme--dark .global-chart-card {
  border-color: rgba(255, 255, 255, 0.8);
  box-shadow: 6px 6px 0 0 rgba(255, 255, 255, 0.8);
}

.chart-collapse-btn {
  position: absolute;
  top: 4px;
  right: 4px;
  z-index: 2;
}

.chart-expand-bar {
  display: flex;
  justify-content: center;
  padding: 4px;
  background: rgba(var(--v-theme-surface-variant), 0.3);
  border: 2px dashed rgb(var(--v-theme-on-surface));
  border-radius: 0;
  opacity: 0.7;
  transition: all 0.2s ease;
}

.chart-expand-bar:hover {
  opacity: 1;
  background: rgba(var(--v-theme-surface-variant), 0.5);
}

.v-theme--dark .chart-expand-bar {
  border-color: rgba(255, 255, 255, 0.5);
}

.expand-chart-btn {
  text-transform: none !important;
  font-weight: 500 !important;
  letter-spacing: 0 !important;
}

.stat-card {
  position: relative;
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 20px;
  margin: 2px;
  background: rgb(var(--v-theme-surface));
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 6px 6px 0 0 rgb(var(--v-theme-on-surface));
  transition: all 0.1s ease;
  overflow: hidden;
  min-height: 100px;
}
.stat-card:hover {
  transform: translate(-2px, -2px);
  box-shadow: 8px 8px 0 0 rgb(var(--v-theme-on-surface));
  border: 2px solid rgb(var(--v-theme-on-surface));
}

.stat-card:active {
  transform: translate(2px, 2px);
  box-shadow: 2px 2px 0 0 rgb(var(--v-theme-on-surface));
}

.v-theme--dark .stat-card {
  background: rgb(var(--v-theme-surface));
  border-color: rgba(255, 255, 255, 0.8);
  box-shadow: 6px 6px 0 0 rgba(255, 255, 255, 0.8);
}
.v-theme--dark .stat-card:hover {
  box-shadow: 8px 8px 0 0 rgba(255, 255, 255, 0.8);
  border-color: rgba(255, 255, 255, 0.8);
}

.v-theme--dark .stat-card:active {
  box-shadow: 2px 2px 0 0 rgba(255, 255, 255, 0.8);
}

.stat-card-icon {
  width: 56px;
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  border: 2px solid rgb(var(--v-theme-on-surface));
  background: rgba(var(--v-theme-primary), 0.15);
  transition: transform 0.1s ease;
}

.v-theme--dark .stat-card-icon {
  border-color: rgba(255, 255, 255, 0.6);
}

.stat-card:hover .stat-card-icon {
  transform: scale(1.05);
}

.stat-card-content {
  flex: 1;
  min-width: 0;
}

.stat-card-value {
  font-size: 1.75rem;
  font-weight: 700;
  line-height: 1.2;
  letter-spacing: -0.5px;
}

.stat-card-total {
  font-size: 1rem;
  font-weight: 500;
  opacity: 0.6;
}

.stat-card-label {
  font-size: 0.875rem;
  font-weight: 600;
  margin-top: 2px;
  opacity: 0.85;
  text-transform: uppercase;
}

.stat-card-desc {
  font-size: 0.75rem;
  opacity: 0.6;
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 隐藏光晕效果 */
.stat-card-glow {
  display: none;
}

/* 统计卡片颜色变体 */
.stat-card-info .stat-card-icon {
  background: #3b82f6;
  color: white;
}
.stat-card-info .stat-card-value {
  color: #3b82f6;
}
.v-theme--dark .stat-card-info .stat-card-value {
  color: #60a5fa;
}

.stat-card-success .stat-card-icon {
  background: #10b981;
  color: white;
}
.stat-card-success .stat-card-value {
  color: #10b981;
}
.v-theme--dark .stat-card-success .stat-card-value {
  color: #34d399;
}

.stat-card-primary .stat-card-icon {
  background: #6366f1;
  color: white;
}
.stat-card-primary .stat-card-value {
  color: #6366f1;
}
.v-theme--dark .stat-card-primary .stat-card-value {
  color: #818cf8;
}

.stat-card-emerald .stat-card-icon {
  background: #059669;
  color: white;
}
.stat-card-emerald .stat-card-value {
  color: #059669;
}
.v-theme--dark .stat-card-emerald .stat-card-value {
  color: #34d399;
}

/* =========================================
   复古像素主题 - 全局样式覆盖
   ========================================= */

/* 全局背景 */
.v-application {
  background-color: #fffbeb !important;
  font-family: 'Courier New', Consolas, monospace !important;
}

.v-theme--dark .v-application,
.v-theme--dark.v-application {
  background-color: rgb(var(--v-theme-background)) !important;
}

.v-main {
  background-color: #fffbeb !important;
}

.v-theme--dark .v-main {
  background-color: rgb(var(--v-theme-background)) !important;
}

/* 统计卡片图标配色 */
.stat-card-icon .v-icon {
  color: white !important;
}

.stat-card-emerald .stat-card-icon .v-icon {
  color: white !important;
}

/* 主按钮 - 复古像素风格 */
.action-btn-primary {
  background: rgb(var(--v-theme-primary)) !important;
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface)) !important;
  color: white !important;
}

.action-btn-primary:hover {
  transform: translate(-1px, -1px);
  box-shadow: 5px 5px 0 0 rgb(var(--v-theme-on-surface)) !important;
}

.action-btn-primary:active {
  transform: translate(2px, 2px) !important;
  box-shadow: none !important;
}

.v-theme--dark .action-btn-primary {
  border-color: rgba(255, 255, 255, 0.8) !important;
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.8) !important;
}

/* 渠道编排容器 */
.channel-orchestration {
  background: transparent !important;
  box-shadow: none !important;
  border: none !important;
}

/* 渠道列表卡片样式 */
.channel-list .channel-row {
  background: rgb(var(--v-theme-surface)) !important;
  margin-bottom: 0;
  padding: 14px 12px 14px 28px !important;
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface)) !important;
  min-height: 48px !important;
  position: relative;
}

.v-theme--dark .channel-list .channel-row {
  border-color: rgba(255, 255, 255, 0.7) !important;
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7) !important;
}

.channel-list .channel-row:active {
  transform: translate(2px, 2px);
  box-shadow: none !important;
  transition: transform 0.1s;
}

/* 序号角标 */
.channel-row .priority-number {
  position: absolute !important;
  top: -1px !important;
  left: -1px !important;
  background: rgb(var(--v-theme-surface)) !important;
  color: rgb(var(--v-theme-on-surface)) !important;
  font-size: 10px !important;
  font-weight: 700 !important;
  padding: 2px 8px !important;
  border: 1px solid rgb(var(--v-theme-on-surface)) !important;
  border-top: none !important;
  border-left: none !important;
  width: auto !important;
  height: auto !important;
  margin: 0 !important;
  box-shadow: none !important;
  text-transform: uppercase;
}

.v-theme--dark .channel-row .priority-number {
  border-color: rgba(255, 255, 255, 0.5) !important;
}

/* 拖拽手柄 */
.drag-handle {
  opacity: 0.3;
  padding: 8px;
  margin-left: -8px;
}

/* 渠道名称 */
.channel-name {
  font-size: 14px !important;
  font-weight: 700 !important;
  color: rgb(var(--v-theme-on-surface));
}

.channel-name .text-caption.text-medium-emphasis {
  background: rgb(var(--v-theme-surface-variant));
  padding: 2px 6px;
  font-size: 10px !important;
  font-weight: 600;
  color: rgb(var(--v-theme-on-surface)) !important;
  border: 1px solid rgb(var(--v-theme-on-surface));
  text-transform: uppercase;
}

.v-theme--dark .channel-name .text-caption.text-medium-emphasis {
  border-color: rgba(255, 255, 255, 0.5);
}

/* 隐藏描述文字 */
.channel-name .text-disabled {
  display: none !important;
}

/* 隐藏指标和密钥数 */
.channel-metrics,
.channel-keys {
  display: none !important;
}

/* --- 备用资源池 --- */
.inactive-pool {
  background: rgb(var(--v-theme-surface)) !important;
  border: 2px dashed rgb(var(--v-theme-on-surface)) !important;
  padding: 8px !important;
  margin-top: 12px;
}

.v-theme--dark .inactive-pool {
  border-color: rgba(255, 255, 255, 0.5) !important;
}

.inactive-channel-row {
  background: rgb(var(--v-theme-surface)) !important;
  margin: 6px !important;
  padding: 12px !important;
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 3px 3px 0 0 rgb(var(--v-theme-on-surface)) !important;
}

.v-theme--dark .inactive-channel-row {
  border-color: rgba(255, 255, 255, 0.6) !important;
  box-shadow: 3px 3px 0 0 rgba(255, 255, 255, 0.6) !important;
}

.inactive-channel-row .channel-info-main {
  color: rgb(var(--v-theme-on-surface)) !important;
  font-weight: 600;
}

/* ----- 操作按钮区域 ----- */
.action-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 20px;
  background: rgb(var(--v-theme-surface));
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 6px 6px 0 0 rgb(var(--v-theme-on-surface));
}

.v-theme--dark .action-bar {
  background: rgb(var(--v-theme-surface));
  border-color: rgba(255, 255, 255, 0.8);
  box-shadow: 6px 6px 0 0 rgba(255, 255, 255, 0.8);
}

.action-bar-left {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}

.action-bar-right {
  display: flex;
  align-items: center;
}

.action-btn {
  font-weight: 600;
  letter-spacing: 0.3px;
  text-transform: uppercase;
  transition: all 0.1s ease;
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface)) !important;
}

.v-theme--dark .action-btn {
  border-color: rgba(255, 255, 255, 0.7) !important;
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7) !important;
}

.action-btn:hover {
  transform: translate(-1px, -1px);
  box-shadow: 5px 5px 0 0 rgb(var(--v-theme-on-surface)) !important;
}

.action-btn:active {
  transform: translate(2px, 2px) !important;
  box-shadow: none !important;
}

.load-balance-btn {
  text-transform: uppercase;
}

.load-balance-menu {
  min-width: 300px;
  padding: 8px;
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface)) !important;
}

.v-theme--dark .load-balance-menu {
  border-color: rgba(255, 255, 255, 0.7) !important;
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7) !important;
}

.load-balance-menu .v-list-item {
  margin-bottom: 4px;
  padding: 12px 16px;
}

.load-balance-menu .v-list-item:last-child {
  margin-bottom: 0;
}

/* =========================================
   手机端专属样式 (≤600px)
   ========================================= */
@media (max-width: 600px) {
  /* --- 顶部导航栏 --- */
  .app-header {
    padding: 0 12px !important;
    background: rgb(var(--v-theme-surface)) !important;
    border-bottom: 2px solid rgb(var(--v-theme-on-surface)) !important;
    box-shadow: none !important;
  }

  .v-theme--dark .app-header {
    border-bottom-color: rgba(255, 255, 255, 0.7) !important;
  }

  .app-logo {
    width: 32px;
    height: 32px;
    margin-right: 8px;
    box-shadow: 2px 2px 0 0 rgb(var(--v-theme-on-surface));
  }

  .v-theme--dark .app-logo {
    box-shadow: 2px 2px 0 0 rgba(255, 255, 255, 0.7);
  }

  /* 移动端导航按钮调整 */
  .nav-toggle {
    box-shadow: 2px 2px 0 0 rgb(var(--v-theme-on-surface)) !important;
  }

  .v-theme--dark .nav-toggle {
    box-shadow: 2px 2px 0 0 rgba(255, 255, 255, 0.7) !important;
  }

  .nav-btn {
    min-width: 70px !important;
    padding: 0 8px !important;
    min-height: 44px !important; /* Touch-friendly */
  }

  /* Touch-friendly header buttons */
  .header-btn {
    min-width: 44px !important;
    min-height: 44px !important;
  }

  /* --- 统计卡片优化 --- */
  .stat-card {
    padding: 14px 12px;
    gap: 10px;
    min-height: auto;
    background: rgb(var(--v-theme-surface)) !important;
    box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface)) !important;
    border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  }

  .v-theme--dark .stat-card {
    box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7) !important;
    border-color: rgba(255, 255, 255, 0.7) !important;
  }

  .stat-card-icon {
    width: 36px;
    height: 36px;
  }

  .stat-card-icon .v-icon {
    font-size: 18px !important;
  }

  .stat-card-value {
    font-size: 1.35rem;
    font-weight: 800 !important;
    line-height: 1.2;
    color: rgb(var(--v-theme-on-surface));
    letter-spacing: -0.5px;
  }

  .stat-card-label {
    font-size: 0.7rem;
    color: rgba(var(--v-theme-on-surface), 0.6);
    font-weight: 500;
    text-transform: uppercase;
  }

  .stat-card-desc {
    display: none;
  }

  .stat-cards-row {
    margin-bottom: 12px !important;
  }

  .stat-cards-row .v-col {
    padding: 4px !important;
  }

  /* --- 操作按钮区域 --- */
  .action-bar {
    flex-direction: column;
    gap: 10px;
    padding: 12px !important;
    box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface)) !important;
  }

  .v-theme--dark .action-bar {
    box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7) !important;
  }

  .action-bar-left {
    width: 100%;
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }

  .action-bar-left .action-btn {
    width: 100%;
    justify-content: center;
    min-height: 44px !important; /* Touch-friendly */
  }

  /* 刷新按钮独占一行 */
  .action-bar-left .action-btn:nth-child(3) {
    grid-column: 1 / -1;
  }

  .action-bar-right {
    width: 100%;
  }

  .action-bar-right .load-balance-btn {
    width: 100%;
    justify-content: center;
  }

  /* --- 渠道编排容器 --- */
  .channel-orchestration .v-card-title {
    display: none !important;
  }

  .channel-orchestration > .v-divider {
    display: none !important;
  }

  /* 隐藏"故障转移序列"标题区域 */
  .channel-orchestration .px-4.pt-3.pb-2 > .d-flex.mb-2 {
    display: none !important;
  }

  /* --- 渠道列表卡片化 --- */
  .channel-list .channel-row:active {
    transform: translate(2px, 2px);
    box-shadow: none !important;
    transition: transform 0.1s;
  }

  /* --- 通用优化 --- */
  .v-chip {
    font-weight: 600;
    border: 1px solid rgb(var(--v-theme-on-surface));
    text-transform: uppercase;
  }

  .v-theme--dark .v-chip {
    border-color: rgba(255, 255, 255, 0.5);
  }

  /* 隐藏分割线 */
  .channel-orchestration .v-divider {
    display: none !important;
  }
}

/* 心跳动画 - 简化为简单闪烁 */
.pulse-animation {
  animation: pixel-blink 1s step-end infinite;
}

@keyframes pixel-blink {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.7;
  }
}

/* ----- 响应式调整 ----- */
@media (min-width: 768px) {
  .app-header {
    padding: 0 24px !important;
  }
}

@media (min-width: 1024px) {
  .app-header {
    padding: 0 32px !important;
  }
}

/* ----- 渠道列表动画 ----- */
.d-contents {
  display: contents;
}

.channel-col {
  transition: all 0.2s ease;
  max-width: 640px;
}

.channel-list-enter-active,
.channel-list-leave-active {
  transition: all 0.2s ease;
}

.channel-list-enter-from {
  opacity: 0;
  transform: translateY(10px);
}

.channel-list-leave-to {
  opacity: 0;
  transform: translateY(-10px);
}

.channel-list-move {
  transition: transform 0.2s ease;
}
</style>

<!-- 全局样式 - 复古像素主题 -->
<style>
/* 复古像素主题 - 全局样式 */
.v-application {
  font-family: 'Courier New', Consolas, 'Liberation Mono', monospace !important;
}

/* 所有按钮复古像素风格 */
.v-btn:not(.v-btn--icon) {
  border-radius: 0 !important;
  text-transform: uppercase !important;
  font-weight: 600 !important;
}

/* 所有卡片复古像素风格 */
.v-card {
  border-radius: 0 !important;
}

/* 所有 Chip 复古像素风格 */
.v-chip {
  border-radius: 0 !important;
  font-weight: 600;
  text-transform: uppercase;
}

/* 输入框复古像素风格 */
.v-text-field .v-field {
  border-radius: 0 !important;
}

/* 对话框复古像素风格 */
.v-dialog .v-card {
  border: 2px solid currentColor !important;
  box-shadow: 6px 6px 0 0 currentColor !important;
}

/* 菜单复古像素风格 */
.v-menu > .v-overlay__content > .v-list {
  border-radius: 0 !important;
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface)) !important;
}

.v-theme--dark .v-menu > .v-overlay__content > .v-list {
  border-color: rgba(255, 255, 255, 0.7) !important;
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7) !important;
}

/* Snackbar 复古像素风格 */
.v-snackbar__wrapper {
  border-radius: 0 !important;
  border: 2px solid currentColor !important;
  box-shadow: 4px 4px 0 0 currentColor !important;
}

/* 状态徽章复古像素风格 */
.status-badge .badge-content {
  border-radius: 0 !important;
  border: 1px solid rgb(var(--v-theme-on-surface));
}

.v-theme--dark .status-badge .badge-content {
  border-color: rgba(255, 255, 255, 0.6);
}

/* 备份列表复古像素风格 */
.v-dialog .backup-list {
  background: transparent !important;
}

.v-dialog .backup-list .v-list-item {
  border: 2px solid rgb(var(--v-theme-on-surface)) !important;
  box-shadow: 3px 3px 0 0 rgb(var(--v-theme-on-surface)) !important;
  border-radius: 0 !important;
  margin-bottom: 8px !important;
  font-family: 'Courier New', Consolas, monospace !important;
}

.v-theme--dark .v-dialog .backup-list .v-list-item {
  border-color: rgba(255, 255, 255, 0.7) !important;
  box-shadow: 3px 3px 0 0 rgba(255, 255, 255, 0.7) !important;
}

.v-dialog .backup-list .v-list-item:hover {
  transform: translate(-1px, -1px);
  box-shadow: 4px 4px 0 0 rgb(var(--v-theme-on-surface)) !important;
}

.v-theme--dark .v-dialog .backup-list .v-list-item:hover {
  box-shadow: 4px 4px 0 0 rgba(255, 255, 255, 0.7) !important;
}

.v-dialog .backup-list .v-list-item-title,
.v-dialog .backup-list .v-list-item-subtitle {
  font-family: 'Courier New', Consolas, monospace !important;
}

/* 设置菜单复古像素风格 */
.v-menu > .v-overlay__content > .v-list .v-list-item-title {
  font-family: 'Courier New', Consolas, monospace !important;
  font-weight: 600 !important;
  text-transform: uppercase !important;
  font-size: 0.85rem !important;
}

/* 对话框标题复古像素风格 */
.v-dialog .v-card-title {
  font-family: 'Courier New', Consolas, monospace !important;
  font-weight: 700 !important;
  text-transform: uppercase !important;
  letter-spacing: 0.5px !important;
}
</style>
