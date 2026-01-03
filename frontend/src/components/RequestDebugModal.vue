<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="900">
    <v-card class="debug-modal-card">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-bug</v-icon>
        {{ t('debugModal.title') }}
        <v-spacer />
        <v-btn icon variant="text" size="small" @click="$emit('update:modelValue', false)">
          <v-icon>mdi-close</v-icon>
        </v-btn>
      </v-card-title>

      <v-card-text v-if="loading" class="text-center pa-8">
        <v-progress-circular indeterminate size="48" />
        <div class="mt-4 text-medium-emphasis">{{ t('common.loading') }}</div>
      </v-card-text>

      <v-card-text v-else-if="error" class="text-center pa-8">
        <v-icon size="48" color="error">mdi-alert-circle</v-icon>
        <div class="mt-4 text-error">{{ error }}</div>
      </v-card-text>

      <v-card-text v-else-if="debugData" class="pa-0">
        <v-tabs v-model="activeTab" bg-color="transparent" density="compact">
          <v-tab value="request">
            <v-icon start size="small">mdi-upload</v-icon>
            {{ t('debugModal.request') }}
          </v-tab>
          <v-tab value="response">
            <v-icon start size="small">mdi-download</v-icon>
            {{ t('debugModal.response') }}
          </v-tab>
        </v-tabs>

        <v-divider />

        <v-tabs-window v-model="activeTab">
          <!-- Request Tab -->
          <v-tabs-window-item value="request">
            <div class="debug-section pa-4">
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
          </v-tabs-window-item>

          <!-- Response Tab -->
          <v-tabs-window-item value="response">
            <div class="debug-section pa-4">
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
          </v-tabs-window-item>
        </v-tabs-window>
      </v-card-text>

      <v-card-text v-else class="text-center pa-8">
        <v-icon size="48" color="grey">mdi-file-document-outline</v-icon>
        <div class="mt-4 text-medium-emphasis">{{ t('debugModal.noData') }}</div>
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
import { api, type DebugLogEntry } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
  requestId: string | null
}>()

defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const loading = ref(false)
const error = ref<string | null>(null)
const debugData = ref<DebugLogEntry | null>(null)
const activeTab = ref('request')
const showCopySnackbar = ref(false)

// Load debug data when dialog opens
watch(() => props.modelValue, (newVal) => {
  if (newVal && props.requestId) {
    loadDebugData(props.requestId)
  } else if (!newVal) {
    // Reset state when dialog closes
    debugData.value = null
    error.value = null
    activeTab.value = 'request'
  }
})

const loadDebugData = async (requestId: string) => {
  loading.value = true
  error.value = null
  try {
    debugData.value = await api.getDebugLog(requestId)
  } catch (err) {
    console.error('Failed to load debug data:', err)
    error.value = err instanceof Error ? err.message : t('debugModal.loadFailed')
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
