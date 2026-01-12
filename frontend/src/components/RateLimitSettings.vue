<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="700">
    <v-card class="settings-card modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4">
        <v-icon class="mr-2">mdi-speedometer</v-icon>
        {{ t('rateLimit.title') }}
        <v-spacer />
        <v-btn variant="text" size="small" color="warning" @click="confirmReset" :loading="resetting" class="mr-2">
          {{ t('rateLimit.resetDefault') }}
        </v-btn>
        <v-btn icon variant="text" size="small" @click="$emit('update:modelValue', false)" class="modal-action-btn">
          <v-icon>mdi-close</v-icon>
        </v-btn>
        <v-btn icon variant="flat" size="small" color="primary" @click="saveConfig" :loading="saving" class="modal-action-btn">
          <v-icon>mdi-check</v-icon>
        </v-btn>
      </v-card-title>
      <v-card-text class="modal-content">
        <div v-if="loading" class="text-center pa-4">
          <v-progress-circular indeterminate size="24" />
        </div>
        <div v-else-if="config">
          <!-- API Rate Limit -->
          <div class="section-title mb-2">{{ t('rateLimit.apiSection') }}</div>
          <v-card variant="outlined" class="mb-4 pa-3">
            <v-row align="center">
              <v-col cols="6">
                <v-switch
                  v-model="config.api.enabled"
                  :label="t('rateLimit.enabled')"
                  color="primary"
                  density="compact"
                  hide-details
                />
              </v-col>
              <v-col cols="6">
                <v-text-field
                  v-model.number="config.api.requestsPerMinute"
                  :label="t('rateLimit.requestsPerMinute')"
                  type="number"
                  min="1"
                  density="compact"
                  hide-details
                  :disabled="!config.api.enabled"
                />
              </v-col>
            </v-row>
            <div class="text-caption text-grey mt-2">{{ t('rateLimit.apiDescription') }}</div>
          </v-card>

          <!-- Portal Rate Limit -->
          <div class="section-title mb-2">{{ t('rateLimit.portalSection') }}</div>
          <v-card variant="outlined" class="mb-4 pa-3">
            <v-row align="center">
              <v-col cols="6">
                <v-switch
                  v-model="config.portal.enabled"
                  :label="t('rateLimit.enabled')"
                  color="primary"
                  density="compact"
                  hide-details
                />
              </v-col>
              <v-col cols="6">
                <v-text-field
                  v-model.number="config.portal.requestsPerMinute"
                  :label="t('rateLimit.requestsPerMinute')"
                  type="number"
                  min="1"
                  density="compact"
                  hide-details
                  :disabled="!config.portal.enabled"
                />
              </v-col>
            </v-row>
            <div class="text-caption text-grey mt-2">{{ t('rateLimit.portalDescription') }}</div>
          </v-card>

          <!-- Auth Failure Protection -->
          <div class="section-title mb-2">{{ t('rateLimit.authFailureSection') }}</div>
          <v-card variant="outlined" class="pa-3">
            <v-switch
              v-model="config.authFailure.enabled"
              :label="t('rateLimit.enabled')"
              color="primary"
              density="compact"
              hide-details
              class="mb-3"
            />
            <div class="text-caption text-grey mb-3">{{ t('rateLimit.authFailureDescription') }}</div>

            <v-table density="compact" class="threshold-table" v-if="config.authFailure.enabled">
              <thead>
                <tr>
                  <th>{{ t('rateLimit.failures') }}</th>
                  <th>{{ t('rateLimit.blockMinutes') }}</th>
                  <th class="text-center" style="width: 60px;">{{ t('common.operation') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(threshold, index) in config.authFailure.thresholds" :key="index">
                  <td>
                    <v-text-field
                      v-model.number="threshold.failures"
                      type="number"
                      min="1"
                      density="compact"
                      hide-details
                      variant="plain"
                      class="threshold-input"
                    />
                  </td>
                  <td>
                    <v-text-field
                      v-model.number="threshold.blockMinutes"
                      type="number"
                      min="1"
                      density="compact"
                      hide-details
                      variant="plain"
                      class="threshold-input"
                    />
                  </td>
                  <td class="text-center">
                    <v-btn
                      icon
                      size="x-small"
                      variant="text"
                      color="error"
                      @click="removeThreshold(index)"
                      :disabled="config.authFailure.thresholds.length <= 1"
                    >
                      <v-icon size="16">mdi-delete</v-icon>
                    </v-btn>
                  </td>
                </tr>
              </tbody>
            </v-table>
            <v-btn
              v-if="config.authFailure.enabled"
              size="small"
              variant="text"
              color="primary"
              @click="addThreshold"
              class="mt-2"
            >
              <v-icon class="mr-1">mdi-plus</v-icon>
              {{ t('rateLimit.addThreshold') }}
            </v-btn>
          </v-card>
        </div>
        <div v-else class="text-center pa-4 text-grey">
          {{ t('rateLimit.loadFailed') }}
        </div>
      </v-card-text>
    </v-card>
  </v-dialog>

  <!-- Reset Confirmation Dialog -->
  <v-dialog v-model="showResetDialog" max-width="400">
    <v-card class="modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4 text-warning">
        {{ t('rateLimit.confirmReset') }}
        <v-spacer />
        <v-btn icon variant="text" size="small" @click="showResetDialog = false" class="modal-action-btn">
          <v-icon>mdi-close</v-icon>
        </v-btn>
        <v-btn icon variant="flat" size="small" color="warning" @click="resetConfig" :loading="resetting" class="modal-action-btn">
          <v-icon>mdi-check</v-icon>
        </v-btn>
      </v-card-title>
      <v-card-text class="modal-content">{{ t('rateLimit.confirmResetDesc') }}</v-card-text>
    </v-card>
  </v-dialog>

  <!-- Snackbar for notifications -->
  <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
    {{ snackbar.text }}
  </v-snackbar>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type RateLimitConfig } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const config = ref<RateLimitConfig | null>(null)
const loading = ref(false)
const saving = ref(false)
const resetting = ref(false)
const showResetDialog = ref(false)

// Snackbar
const snackbar = ref({
  show: false,
  text: '',
  color: 'error'
})

const showSnackbar = (text: string, color: string = 'error') => {
  snackbar.value = { show: true, text, color }
}

// Load config when dialog opens
watch(() => props.modelValue, (newVal) => {
  if (newVal && !config.value) {
    loadConfig()
  }
})

const loadConfig = async () => {
  loading.value = true
  try {
    config.value = await api.getRateLimitConfig()
  } catch (error) {
    console.error('Failed to load rate limit config:', error)
    showSnackbar(t('rateLimit.loadFailed'), 'error')
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  if (!config.value) return

  // Validate thresholds are in ascending order
  const thresholds = config.value.authFailure.thresholds
  for (let i = 1; i < thresholds.length; i++) {
    if (thresholds[i].failures <= thresholds[i - 1].failures) {
      showSnackbar(t('rateLimit.thresholdOrderError'), 'error')
      return
    }
  }

  saving.value = true
  try {
    await api.updateRateLimitConfig(config.value)
    showSnackbar(t('common.success'), 'success')
    emit('update:modelValue', false)
  } catch (error) {
    console.error('Failed to save rate limit config:', error)
    showSnackbar(t('rateLimit.saveFailed'), 'error')
  } finally {
    saving.value = false
  }
}

const confirmReset = () => {
  showResetDialog.value = true
}

const resetConfig = async () => {
  resetting.value = true
  try {
    const result = await api.resetRateLimitConfig()
    config.value = result.config
    showResetDialog.value = false
    showSnackbar(t('common.success'), 'success')
  } catch (error) {
    console.error('Failed to reset rate limit config:', error)
    showSnackbar(t('rateLimit.resetFailed'), 'error')
  } finally {
    resetting.value = false
  }
}

const addThreshold = () => {
  if (!config.value) return
  const thresholds = config.value.authFailure.thresholds
  const lastThreshold = thresholds[thresholds.length - 1]
  thresholds.push({
    failures: lastThreshold.failures + 10,
    blockMinutes: lastThreshold.blockMinutes * 2
  })
}

const removeThreshold = (index: number) => {
  if (!config.value) return
  config.value.authFailure.thresholds.splice(index, 1)
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

.threshold-table {
  border: 1px solid rgba(var(--v-theme-on-surface), 0.1);
  border-radius: 0 !important;
}

.threshold-table th {
  background: rgba(var(--v-theme-on-surface), 0.05) !important;
  font-weight: 600 !important;
  font-size: 0.8rem !important;
  padding: 8px 12px !important;
}

.threshold-table td {
  padding: 4px 8px !important;
}

.threshold-input {
  max-width: 100px;
}

.threshold-input :deep(input) {
  text-align: center;
}
</style>
