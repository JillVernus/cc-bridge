<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="550">
    <v-card class="settings-card">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-bug</v-icon>
        {{ t('debugLog.title') }}
      </v-card-title>
      <v-card-text>
        <div v-if="loading" class="text-center pa-4">
          <v-progress-circular indeterminate size="24" />
        </div>
        <div v-else-if="config">
          <!-- Enable/Disable Toggle -->
          <v-card variant="outlined" class="mb-4 pa-3">
            <v-switch
              v-model="config.enabled"
              :label="config.enabled ? t('debugLog.enabled') : t('debugLog.disabled')"
              color="primary"
              density="compact"
              hide-details
            />
            <div class="text-caption text-grey mt-2">{{ t('debugLog.enableDescription') }}</div>
          </v-card>

          <!-- Retention Hours -->
          <div class="section-title mb-2">{{ t('debugLog.retentionSection') }}</div>
          <v-card variant="outlined" class="mb-4 pa-3">
            <v-slider
              v-model="config.retentionHours"
              :min="1"
              :max="168"
              :step="1"
              :label="t('debugLog.retentionHours')"
              thumb-label
              hide-details
              :disabled="!config.enabled"
            >
              <template #append>
                <span class="text-body-2" style="min-width: 60px">
                  {{ formatRetention(config.retentionHours) }}
                </span>
              </template>
            </v-slider>
            <div class="text-caption text-grey mt-2">{{ t('debugLog.retentionDescription') }}</div>
          </v-card>

          <!-- Max Body Size -->
          <div class="section-title mb-2">{{ t('debugLog.maxSizeSection') }}</div>
          <v-card variant="outlined" class="pa-3">
            <v-slider
              v-model="maxBodySizeKB"
              :min="64"
              :max="2048"
              :step="64"
              :label="t('debugLog.maxBodySize')"
              thumb-label
              hide-details
              :disabled="!config.enabled"
            >
              <template #append>
                <span class="text-body-2" style="min-width: 60px">
                  {{ formatSize(config.maxBodySize) }}
                </span>
              </template>
            </v-slider>
            <div class="text-caption text-grey mt-2">{{ t('debugLog.maxSizeDescription') }}</div>
          </v-card>

          <!-- Stats -->
          <div v-if="stats" class="mt-4 pa-3 bg-surface-variant rounded">
            <div class="d-flex align-center justify-space-between">
              <span class="text-caption">
                <v-icon size="16" class="mr-1">mdi-database</v-icon>
                {{ t('debugLog.storedLogs', { count: stats.count }) }}
              </span>
              <v-btn
                v-if="stats.count > 0"
                size="x-small"
                variant="text"
                color="error"
                @click="confirmPurge"
                :loading="purging"
              >
                {{ t('debugLog.purgeAll') }}
              </v-btn>
            </div>
          </div>
        </div>
        <div v-else class="text-center pa-4 text-grey">
          {{ t('debugLog.loadFailed') }}
        </div>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="$emit('update:modelValue', false)">{{ t('common.cancel') }}</v-btn>
        <v-btn color="primary" variant="flat" @click="saveConfig" :loading="saving">{{ t('common.save') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- Purge Confirmation Dialog -->
  <v-dialog v-model="showPurgeDialog" max-width="400">
    <v-card>
      <v-card-title class="text-error">{{ t('debugLog.confirmPurge') }}</v-card-title>
      <v-card-text>{{ t('debugLog.confirmPurgeDesc') }}</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="showPurgeDialog = false">{{ t('common.cancel') }}</v-btn>
        <v-btn color="error" variant="flat" @click="purgeAll" :loading="purging">{{ t('common.confirm') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- Snackbar for notifications -->
  <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
    {{ snackbar.text }}
  </v-snackbar>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type DebugLogConfig } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const config = ref<DebugLogConfig | null>(null)
const stats = ref<{ count: number } | null>(null)
const loading = ref(false)
const saving = ref(false)
const purging = ref(false)
const showPurgeDialog = ref(false)

// Snackbar
const snackbar = ref({
  show: false,
  text: '',
  color: 'error'
})

const showSnackbar = (text: string, color: string = 'error') => {
  snackbar.value = { show: true, text, color }
}

// Convert bytes to KB for slider
const maxBodySizeKB = computed({
  get: () => config.value ? Math.round(config.value.maxBodySize / 1024) : 512,
  set: (kb: number) => {
    if (config.value) {
      config.value.maxBodySize = kb * 1024
    }
  }
})

const formatRetention = (hours: number): string => {
  if (hours >= 24) {
    const days = Math.floor(hours / 24)
    const remainingHours = hours % 24
    if (remainingHours === 0) {
      return `${days}d`
    }
    return `${days}d ${remainingHours}h`
  }
  return `${hours}h`
}

const formatSize = (bytes: number): string => {
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  }
  return `${Math.round(bytes / 1024)} KB`
}

// Load config when dialog opens
watch(() => props.modelValue, (newVal) => {
  if (newVal) {
    loadConfig()
    loadStats()
  }
})

const loadConfig = async () => {
  loading.value = true
  try {
    config.value = await api.getDebugLogConfig()
  } catch (error) {
    console.error('Failed to load debug log config:', error)
    showSnackbar(t('debugLog.loadFailed'), 'error')
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    stats.value = await api.getDebugLogStats()
  } catch (error) {
    console.error('Failed to load debug log stats:', error)
  }
}

const saveConfig = async () => {
  if (!config.value) return

  saving.value = true
  try {
    await api.updateDebugLogConfig(config.value)
    showSnackbar(t('common.success'), 'success')
    emit('update:modelValue', false)
  } catch (error) {
    console.error('Failed to save debug log config:', error)
    showSnackbar(t('debugLog.saveFailed'), 'error')
  } finally {
    saving.value = false
  }
}

const confirmPurge = () => {
  showPurgeDialog.value = true
}

const purgeAll = async () => {
  purging.value = true
  try {
    const result = await api.purgeDebugLogs()
    showPurgeDialog.value = false
    showSnackbar(t('debugLog.purged', { count: result.deleted }), 'success')
    await loadStats()
  } catch (error) {
    console.error('Failed to purge debug logs:', error)
    showSnackbar(t('debugLog.purgeFailed'), 'error')
  } finally {
    purging.value = false
  }
}
</script>

<style scoped>
.settings-card {
  border: 2px solid rgb(var(--v-theme-on-surface));
  box-shadow: 6px 6px 0 0 rgb(var(--v-theme-on-surface));
  border-radius: 0 !important;
}

.v-theme--dark .settings-card {
  border-color: rgba(255, 255, 255, 0.7);
  box-shadow: 6px 6px 0 0 rgba(255, 255, 255, 0.7);
}

.section-title {
  font-weight: 600;
  font-size: 0.9rem;
  color: rgb(var(--v-theme-primary));
}
</style>
