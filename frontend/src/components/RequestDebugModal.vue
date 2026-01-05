<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="900">
    <v-card class="debug-modal-card">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-information-outline</v-icon>
        {{ t('debugModal.title') }}
        <v-spacer />
        <v-btn icon variant="text" size="small" @click="$emit('update:modelValue', false)">
          <v-icon>mdi-close</v-icon>
        </v-btn>
      </v-card-title>

      <v-card-text class="pa-0">
        <!-- Tabs: Metadata is always available, Request/Response only when debug data exists -->
        <v-tabs v-model="activeTab" bg-color="transparent" density="compact">
          <v-tab value="metadata">
            <v-icon start size="small">mdi-tag-text-outline</v-icon>
            {{ t('debugModal.metadata') }}
          </v-tab>
          <v-tab value="request" :disabled="!debugData && !loading">
            <v-icon start size="small">mdi-upload</v-icon>
            {{ t('debugModal.request') }}
            <v-chip v-if="!debugData && !loading" size="x-small" class="ml-1" color="grey">
              {{ t('debugModal.off') }}
            </v-chip>
          </v-tab>
          <v-tab value="response" :disabled="!debugData && !loading">
            <v-icon start size="small">mdi-download</v-icon>
            {{ t('debugModal.response') }}
            <v-chip v-if="!debugData && !loading" size="x-small" class="ml-1" color="grey">
              {{ t('debugModal.off') }}
            </v-chip>
          </v-tab>
        </v-tabs>

        <v-divider />

        <v-tabs-window v-model="activeTab">
          <!-- Metadata Tab (always available) -->
          <v-tabs-window-item value="metadata">
            <div class="debug-section pa-4">
              <div v-if="logItem" class="metadata-grid">
                <!-- Request Info -->
                <div class="metadata-group">
                  <div class="group-title">
                    <v-icon size="small" class="mr-1">mdi-information-outline</v-icon>
                    {{ t('debugModal.requestInfo') }}
                  </div>
                  <table class="metadata-table">
                    <tbody>
                      <tr>
                        <td class="meta-key">{{ t('debugModal.requestId') }}</td>
                        <td class="meta-value mono-text">{{ logItem.id }}</td>
                      </tr>
                      <tr>
                        <td class="meta-key">{{ t('debugModal.endpoint') }}</td>
                        <td class="meta-value">
                          <v-chip size="x-small" variant="tonal" :color="logItem.endpoint === '/v1/messages' ? 'deep-purple' : 'teal'">
                            {{ logItem.endpoint }}
                          </v-chip>
                        </td>
                      </tr>
                      <tr>
                        <td class="meta-key">{{ t('debugModal.stream') }}</td>
                        <td class="meta-value">
                          <v-icon v-if="logItem.stream" size="small" color="success">mdi-check-circle</v-icon>
                          <v-icon v-else size="small" color="grey">mdi-close-circle</v-icon>
                          {{ logItem.stream ? t('common.yes') : t('common.no') }}
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>

                <!-- Timing -->
                <div class="metadata-group">
                  <div class="group-title">
                    <v-icon size="small" class="mr-1">mdi-clock-outline</v-icon>
                    {{ t('debugModal.timing') }}
                  </div>
                  <table class="metadata-table">
                    <tbody>
                      <tr>
                        <td class="meta-key">{{ t('debugModal.startTime') }}</td>
                        <td class="meta-value">{{ formatDateTime(logItem.initialTime) }}</td>
                      </tr>
                      <tr>
                        <td class="meta-key">{{ t('debugModal.endTime') }}</td>
                        <td class="meta-value">{{ formatDateTime(logItem.completeTime) }}</td>
                      </tr>
                      <tr>
                        <td class="meta-key">{{ t('debugModal.duration') }}</td>
                        <td class="meta-value">
                          <span :class="'duration-' + getDurationColor(logItem.durationMs)">
                            {{ logItem.durationMs }} ms
                          </span>
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>

                <!-- Status -->
                <div class="metadata-group">
                  <div class="group-title">
                    <v-icon size="small" class="mr-1">mdi-check-circle-outline</v-icon>
                    {{ t('debugModal.statusInfo') }}
                  </div>
                  <table class="metadata-table">
                    <tbody>
                      <tr>
                        <td class="meta-key">{{ t('debugModal.status') }}</td>
                        <td class="meta-value">
                          <v-chip size="x-small" :color="getStatusColor(logItem.status)" variant="flat">
                            {{ logItem.status }}
                          </v-chip>
                        </td>
                      </tr>
                      <tr>
                        <td class="meta-key">{{ t('debugModal.httpStatus') }}</td>
                        <td class="meta-value">
                          <v-chip size="x-small" :color="getHttpStatusColor(logItem.httpStatus)" variant="tonal">
                            {{ logItem.httpStatus || '-' }}
                          </v-chip>
                        </td>
                      </tr>
                      <tr v-if="logItem.error">
                        <td class="meta-key">{{ t('debugModal.error') }}</td>
                        <td class="meta-value error-text">{{ logItem.error }}</td>
                      </tr>
                      <tr v-if="logItem.failoverInfo">
                        <td class="meta-key">{{ t('debugModal.failoverInfo') }}</td>
                        <td class="meta-value failover-info-text">{{ logItem.failoverInfo }}</td>
                      </tr>
                      <tr v-if="logItem.upstreamError">
                        <td class="meta-key">{{ t('debugModal.upstreamError') }}</td>
                        <td class="meta-value">
                          <pre class="upstream-error-content">{{ logItem.upstreamError }}</pre>
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
              <div v-else class="text-center pa-8 text-medium-emphasis">
                {{ t('debugModal.noMetadata') }}
              </div>
            </div>
          </v-tabs-window-item>

          <!-- Request Tab -->
          <v-tabs-window-item value="request">
            <div class="debug-section pa-4">
              <div v-if="loading" class="text-center pa-8">
                <v-progress-circular indeterminate size="48" />
                <div class="mt-4 text-medium-emphasis">{{ t('common.loading') }}</div>
              </div>
              <div v-else-if="debugData">
                <!-- Request Headers -->
                <div class="section-title mb-2">
                  <v-icon size="small" class="mr-1">mdi-format-list-bulleted</v-icon>
                  {{ t('debugModal.headers') }}
                  <v-chip size="x-small" class="ml-2">{{ Object.keys(debugData.requestHeaders || {}).length }}</v-chip>
                </div>
                <v-card variant="outlined" class="mb-4">
                  <div class="headers-container">
                    <table class="headers-table">
                      <tbody>
                        <tr v-for="(value, key) in debugData.requestHeaders" :key="key">
                          <td class="header-key">{{ key }}</td>
                          <td class="header-value">{{ value }}</td>
                        </tr>
                      </tbody>
                    </table>
                    <div v-if="!debugData.requestHeaders || Object.keys(debugData.requestHeaders).length === 0" class="pa-4 text-center text-medium-emphasis">
                      {{ t('debugModal.noHeaders') }}
                    </div>
                  </div>
                </v-card>

                <!-- Request Body -->
                <div class="section-title mb-2">
                  <v-icon size="small" class="mr-1">mdi-code-json</v-icon>
                  {{ t('debugModal.body') }}
                  <v-chip size="x-small" class="ml-2">{{ formatBytes(debugData.requestBody?.length || 0) }}</v-chip>
                  <v-btn
                    v-if="debugData.requestBody"
                    icon
                    variant="text"
                    size="x-small"
                    class="ml-2"
                    @click="copyToClipboard(debugData.requestBody)"
                    :title="t('common.copy')"
                  >
                    <v-icon size="small">mdi-content-copy</v-icon>
                  </v-btn>
                </div>
                <v-card variant="outlined">
                  <pre class="body-content">{{ formatJson(debugData.requestBody) }}</pre>
                </v-card>
              </div>
              <div v-else class="text-center pa-8">
                <v-icon size="48" color="grey">mdi-bug-outline</v-icon>
                <div class="mt-4 text-medium-emphasis">{{ t('debugModal.debugDisabled') }}</div>
              </div>
            </div>
          </v-tabs-window-item>

          <!-- Response Tab -->
          <v-tabs-window-item value="response">
            <div class="debug-section pa-4">
              <div v-if="loading" class="text-center pa-8">
                <v-progress-circular indeterminate size="48" />
                <div class="mt-4 text-medium-emphasis">{{ t('common.loading') }}</div>
              </div>
              <div v-else-if="debugData">
                <!-- Response Headers -->
                <div class="section-title mb-2">
                  <v-icon size="small" class="mr-1">mdi-format-list-bulleted</v-icon>
                  {{ t('debugModal.headers') }}
                  <v-chip size="x-small" class="ml-2">{{ Object.keys(debugData.responseHeaders || {}).length }}</v-chip>
                </div>
                <v-card variant="outlined" class="mb-4">
                  <div class="headers-container">
                    <table class="headers-table">
                      <tbody>
                        <tr v-for="(value, key) in debugData.responseHeaders" :key="key">
                          <td class="header-key">{{ key }}</td>
                          <td class="header-value">{{ value }}</td>
                        </tr>
                      </tbody>
                    </table>
                    <div v-if="!debugData.responseHeaders || Object.keys(debugData.responseHeaders).length === 0" class="pa-4 text-center text-medium-emphasis">
                      {{ t('debugModal.noHeaders') }}
                    </div>
                  </div>
                </v-card>

                <!-- Response Body -->
                <div class="section-title mb-2">
                  <v-icon size="small" class="mr-1">mdi-code-json</v-icon>
                  {{ t('debugModal.body') }}
                  <v-chip size="x-small" class="ml-2">{{ formatBytes(debugData.responseBody?.length || 0) }}</v-chip>
                  <v-btn
                    v-if="debugData.responseBody"
                    icon
                    variant="text"
                    size="x-small"
                    class="ml-2"
                    @click="copyToClipboard(debugData.responseBody)"
                    :title="t('common.copy')"
                  >
                    <v-icon size="small">mdi-content-copy</v-icon>
                  </v-btn>
                </div>
                <v-card variant="outlined">
                  <pre class="body-content">{{ formatJson(debugData.responseBody) }}</pre>
                </v-card>
              </div>
              <div v-else class="text-center pa-8">
                <v-icon size="48" color="grey">mdi-bug-outline</v-icon>
                <div class="mt-4 text-medium-emphasis">{{ t('debugModal.debugDisabled') }}</div>
              </div>
            </div>
          </v-tabs-window-item>
        </v-tabs-window>
      </v-card-text>
    </v-card>
  </v-dialog>

  <!-- Snackbar for copy notification -->
  <v-snackbar v-model="showCopySnackbar" :timeout="2000" color="success">
    {{ t('common.copied') }}
  </v-snackbar>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type DebugLogEntry, type RequestLog } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
  requestId: string | null
  logItem: RequestLog | null
}>()

defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const loading = ref(false)
const debugData = ref<DebugLogEntry | null>(null)
const activeTab = ref('metadata')
const showCopySnackbar = ref(false)

// Load debug data when dialog opens
watch(() => props.modelValue, (newVal) => {
  if (newVal && props.requestId) {
    loadDebugData(props.requestId)
  } else if (!newVal) {
    // Reset state when dialog closes
    debugData.value = null
    activeTab.value = 'metadata'
  }
})

const loadDebugData = async (requestId: string) => {
  loading.value = true
  try {
    debugData.value = await api.getDebugLog(requestId)
  } catch (err) {
    // Debug data not available (debug mode was off) - this is expected
    debugData.value = null
  } finally {
    loading.value = false
  }
}

const formatJson = (str: string | undefined): string => {
  if (!str) return ''
  try {
    const parsed = JSON.parse(str)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return str
  }
}

const formatBytes = (bytes: number): string => {
  if (bytes === 0) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

const formatDateTime = (dateStr: string | undefined): string => {
  if (!dateStr) return '-'
  const d = new Date(dateStr)
  return d.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

const getDurationColor = (ms: number): string => {
  if (ms <= 5000) return 'success'
  if (ms <= 15000) return 'warning'
  return 'error'
}

const getStatusColor = (status: string): string => {
  const colors: Record<string, string> = {
    pending: 'warning',
    completed: 'success',
    error: 'error',
    timeout: 'grey',
    failover: 'orange'
  }
  return colors[status] || 'grey'
}

const getHttpStatusColor = (status: number): string => {
  if (!status) return 'grey'
  if (status >= 200 && status < 300) return 'success'
  if (status >= 300 && status < 400) return 'info'
  if (status >= 400 && status < 500) return 'warning'
  return 'error'
}

const copyToClipboard = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text)
    showCopySnackbar.value = true
  } catch (err) {
    console.error('Failed to copy:', err)
  }
}
</script>

<style scoped>
.debug-modal-card {
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 6px 6px 0 0 rgb(var(--v-theme-on-surface));
  border-radius: 0 !important;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
}

.v-theme--dark .debug-modal-card {
  border-color: rgba(255, 255, 255, 0.7);
  box-shadow: 6px 6px 0 0 rgba(255, 255, 255, 0.7);
}

.debug-section {
  max-height: 60vh;
  overflow-y: auto;
}

/* Metadata Grid Layout */
.metadata-grid {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.metadata-group {
  border: 1px solid rgba(var(--v-theme-on-surface), 0.1);
  border-radius: 4px;
  padding: 12px;
}

.group-title {
  font-weight: 600;
  font-size: 0.875rem;
  display: flex;
  align-items: center;
  color: rgb(var(--v-theme-primary));
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.1);
}

.metadata-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.8125rem;
}

.metadata-table tr {
  border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.05);
}

.metadata-table tr:last-child {
  border-bottom: none;
}

.meta-key {
  padding: 6px 12px 6px 0;
  font-weight: 500;
  color: rgba(var(--v-theme-on-surface), 0.7);
  white-space: nowrap;
  width: 140px;
  vertical-align: top;
}

.meta-value {
  padding: 6px 0;
  color: rgba(var(--v-theme-on-surface), 0.9);
  word-break: break-all;
}

.mono-text {
  font-family: 'Courier New', Consolas, monospace;
  font-size: 0.75rem;
}

.error-text {
  color: rgb(var(--v-theme-error));
}

.failover-info-text {
  font-family: 'Courier New', Consolas, monospace;
  font-size: 0.85rem;
  color: rgb(var(--v-theme-info));
}

.upstream-error-content {
  margin: 0;
  padding: 8px;
  font-size: 0.75rem;
  font-family: 'Courier New', Consolas, monospace;
  max-height: 120px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  background: rgba(var(--v-theme-error), 0.1);
  border-radius: 4px;
  color: rgb(var(--v-theme-error));
}

/* Duration colors */
.duration-success {
  color: rgb(var(--v-theme-success));
  font-weight: 600;
}

.duration-warning {
  color: rgb(var(--v-theme-warning));
  font-weight: 600;
}

.duration-error {
  color: rgb(var(--v-theme-error));
  font-weight: 600;
}

/* Existing styles for debug data */
.section-title {
  font-weight: 600;
  font-size: 0.875rem;
  display: flex;
  align-items: center;
  color: rgb(var(--v-theme-primary));
}

.headers-container {
  max-height: 200px;
  overflow-y: auto;
}

.headers-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.8125rem;
  font-family: 'Courier New', Consolas, monospace;
}

.headers-table td {
  padding: 6px 12px;
  border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.1);
  vertical-align: top;
}

.header-key {
  font-weight: 600;
  color: rgb(var(--v-theme-primary));
  white-space: nowrap;
  width: 200px;
}

.header-value {
  word-break: break-all;
  color: rgba(var(--v-theme-on-surface), 0.8);
}

.body-content {
  margin: 0;
  padding: 12px;
  font-size: 0.75rem;
  font-family: 'Courier New', Consolas, monospace;
  max-height: 400px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  background: rgba(var(--v-theme-on-surface), 0.02);
}

.v-theme--dark .body-content {
  background: rgba(255, 255, 255, 0.02);
}
</style>
