<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="900">
    <v-card class="settings-card">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-swap-horizontal</v-icon>
        {{ t('failover.title') }}
      </v-card-title>
      <v-card-text>
        <div v-if="loading" class="text-center pa-4">
          <v-progress-circular indeterminate size="24" />
        </div>
        <div v-else-if="config">
          <!-- Scope Note -->
          <v-alert type="info" variant="tonal" density="compact" class="mb-4">
            {{ t('failover.quotaChannelsOnly') }}
          </v-alert>

          <!-- Enable/Disable Toggle -->
          <v-card variant="outlined" class="mb-4 pa-3">
            <v-switch
              v-model="config.enabled"
              :label="config.enabled ? t('failover.enabled') : t('failover.disabled')"
              color="primary"
              density="compact"
              hide-details
            />
            <div class="text-caption text-grey mt-2">{{ t('failover.enableDescription') }}</div>
          </v-card>

          <!-- Rules Section -->
          <div class="section-title mb-2">{{ t('failover.rulesSection') }}</div>
          <v-card variant="outlined" class="mb-4 pa-3">
            <div class="text-caption text-grey mb-3">{{ t('failover.rulesDescription') }}</div>

            <!-- Rules List -->
            <v-table density="compact" class="mb-3 rules-table">
              <thead>
                <tr>
                  <th style="width: 200px">{{ t('failover.errorCodes') }}</th>
                  <th style="width: 180px">{{ t('failover.action') }}</th>
                  <th style="width: 100px">{{ t('failover.threshold') }}</th>
                  <th style="width: 100px">{{ t('failover.waitSeconds') }}</th>
                  <th style="width: 50px"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(rule, index) in config.rules" :key="index">
                  <td>
                    <v-text-field
                      v-model="rule.errorCodes"
                      density="compact"
                      variant="outlined"
                      hide-details
                      :placeholder="t('failover.codesPlaceholder')"
                      :disabled="!config.enabled"
                    />
                  </td>
                  <td>
                    <v-select
                      v-model="rule.action"
                      :items="actionOptions"
                      item-title="label"
                      item-value="value"
                      density="compact"
                      variant="outlined"
                      hide-details
                      :disabled="!config.enabled"
                    />
                  </td>
                  <td>
                    <v-text-field
                      v-if="rule.action === 'failover_threshold'"
                      v-model.number="rule.threshold"
                      type="number"
                      min="1"
                      max="100"
                      density="compact"
                      variant="outlined"
                      hide-details
                      :disabled="!config.enabled"
                    />
                    <span v-else class="text-grey text-caption">-</span>
                  </td>
                  <td>
                    <v-text-field
                      v-if="rule.action === 'retry_wait'"
                      v-model.number="rule.waitSeconds"
                      type="number"
                      min="0"
                      max="300"
                      density="compact"
                      variant="outlined"
                      hide-details
                      :disabled="!config.enabled"
                      :placeholder="t('failover.autoDetect')"
                    />
                    <span v-else class="text-grey text-caption">-</span>
                  </td>
                  <td class="text-center">
                    <v-btn
                      icon
                      size="small"
                      variant="text"
                      color="error"
                      @click="removeRule(index)"
                      :disabled="!config.enabled || config.rules.length <= 1"
                    >
                      <v-icon size="18">mdi-delete</v-icon>
                    </v-btn>
                  </td>
                </tr>
              </tbody>
            </v-table>

            <v-btn
              size="small"
              variant="outlined"
              @click="addRule"
              :disabled="!config.enabled"
            >
              <v-icon start>mdi-plus</v-icon>
              {{ t('failover.addRule') }}
            </v-btn>
          </v-card>

          <!-- Action Legend & Error Codes Reference (side by side) -->
          <v-row class="mb-4">
            <!-- Action Legend (Left) -->
            <v-col cols="12" md="6">
              <v-card variant="outlined" class="pa-3 h-100">
                <div class="section-title mb-2">{{ t('failover.actionLegend') }}</div>
                <div class="text-caption">
                  <div class="mb-1">
                    <v-chip size="x-small" color="error" class="mr-2">{{ t('failover.actionFailoverImmediate') }}</v-chip>
                    {{ t('failover.actionFailoverImmediateDesc') }}
                  </div>
                  <div class="mb-1">
                    <v-chip size="x-small" color="warning" class="mr-2">{{ t('failover.actionFailoverThreshold') }}</v-chip>
                    {{ t('failover.actionFailoverThresholdDesc') }}
                  </div>
                  <div class="mb-1">
                    <v-chip size="x-small" color="info" class="mr-2">{{ t('failover.actionRetryWait') }}</v-chip>
                    {{ t('failover.actionRetryWaitDesc') }}
                  </div>
                  <div>
                    <v-chip size="x-small" color="purple" class="mr-2">{{ t('failover.actionSuspendChannel') }}</v-chip>
                    {{ t('failover.actionSuspendChannelDesc') }}
                  </div>
                </div>
              </v-card>
            </v-col>

            <!-- Error Code Patterns (Right) -->
            <v-col cols="12" md="6">
              <v-card variant="outlined" class="pa-3 h-100">
                <div class="section-title mb-2">{{ t('failover.errorCodeReference') }}</div>
                <v-table density="compact" class="error-codes-table">
                  <tbody>
                    <tr><td class="code-cell">401</td><td class="desc-cell">{{ t('failover.errorCode401') }}</td></tr>
                    <tr><td class="code-cell">403</td><td class="desc-cell">{{ t('failover.errorCode403') }}</td></tr>
                    <tr><td class="code-cell">429</td><td class="desc-cell">{{ t('failover.errorCode429') }}</td></tr>
                    <tr><td class="code-cell">429:QUOTA_EXHAUSTED</td><td class="desc-cell">{{ t('failover.errorCode429QuotaExhausted') }}</td></tr>
                    <tr><td class="code-cell">429:model_cooldown</td><td class="desc-cell">{{ t('failover.errorCode429ModelCooldown') }}</td></tr>
                    <tr><td class="code-cell">429:RESOURCE_EXHAUSTED</td><td class="desc-cell">{{ t('failover.errorCode429ResourceExhausted') }}</td></tr>
                    <tr><td class="code-cell">403:CREDIT_EXHAUSTED</td><td class="desc-cell">{{ t('failover.errorCode403CreditExhausted') }}</td></tr>
                    <tr><td class="code-cell">500,502,503,504</td><td class="desc-cell">{{ t('failover.errorCode5xx') }}</td></tr>
                    <tr><td class="code-cell">others</td><td class="desc-cell">{{ t('failover.errorCodeOthers') }}</td></tr>
                  </tbody>
                </v-table>
              </v-card>
            </v-col>
          </v-row>

          <!-- Info Box -->
          <v-alert type="info" variant="tonal" density="compact">
            <div class="text-caption">
              <strong>{{ t('failover.howItWorks') }}</strong>
              <ul class="mt-1 mb-0 pl-4">
                <li>{{ t('failover.howItWorksItem1') }}</li>
                <li>{{ t('failover.howItWorksItem2') }}</li>
                <li>{{ t('failover.howItWorksItem3') }}</li>
                <li>{{ t('failover.howItWorksItem4') }}</li>
              </ul>
            </div>
          </v-alert>
        </div>
        <div v-else class="text-center pa-4 text-grey">
          {{ t('failover.loadFailed') }}
        </div>
      </v-card-text>
      <v-card-actions>
        <v-btn variant="text" color="warning" @click="resetConfig" :loading="resetting" :disabled="!config">
          {{ t('failover.reset') }}
        </v-btn>
        <v-spacer />
        <v-btn variant="text" @click="$emit('update:modelValue', false)">{{ t('common.cancel') }}</v-btn>
        <v-btn color="primary" variant="flat" @click="saveConfig" :loading="saving">{{ t('common.save') }}</v-btn>
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
import { api, type FailoverConfig, type FailoverRule, type FailoverAction } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const config = ref<FailoverConfig | null>(null)
const loading = ref(false)
const saving = ref(false)
const resetting = ref(false)

// Action options for dropdown
const actionOptions = computed(() => [
  { value: 'failover_immediate', label: t('failover.actionFailoverImmediate') },
  { value: 'failover_threshold', label: t('failover.actionFailoverThreshold') },
  { value: 'retry_wait', label: t('failover.actionRetryWait') },
  { value: 'suspend_channel', label: t('failover.actionSuspendChannel') }
])

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
  if (newVal) {
    loadConfig()
  }
})

const loadConfig = async () => {
  loading.value = true
  try {
    config.value = await api.getFailoverConfig()
    // Ensure all rules have action field (for backwards compatibility)
    if (config.value?.rules) {
      config.value.rules = config.value.rules.map(rule => ({
        ...rule,
        action: rule.action || 'failover_threshold',
        threshold: rule.threshold ?? 1,
        waitSeconds: rule.waitSeconds ?? 0
      }))
    }
  } catch (error) {
    console.error('Failed to load failover config:', error)
    showSnackbar(t('failover.loadFailed'), 'error')
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  if (!config.value) return

  // Validate rules
  for (const rule of config.value.rules) {
    if (!rule.errorCodes.trim()) {
      showSnackbar(t('failover.emptyCodesError'), 'error')
      return
    }
    if (rule.action === 'failover_threshold' && (!rule.threshold || rule.threshold < 1)) {
      showSnackbar(t('failover.invalidThresholdError'), 'error')
      return
    }
  }

  saving.value = true
  try {
    await api.updateFailoverConfig(config.value)
    showSnackbar(t('common.success'), 'success')
    emit('update:modelValue', false)
  } catch (error) {
    console.error('Failed to save failover config:', error)
    showSnackbar(t('failover.saveFailed'), 'error')
  } finally {
    saving.value = false
  }
}

const resetConfig = async () => {
  resetting.value = true
  try {
    config.value = await api.resetFailoverConfig()
    // Ensure all rules have action field
    if (config.value?.rules) {
      config.value.rules = config.value.rules.map(rule => ({
        ...rule,
        action: rule.action || 'failover_threshold',
        threshold: rule.threshold ?? 1,
        waitSeconds: rule.waitSeconds ?? 0
      }))
    }
    showSnackbar(t('failover.resetSuccess'), 'success')
  } catch (error) {
    console.error('Failed to reset failover config:', error)
    showSnackbar(t('failover.resetFailed'), 'error')
  } finally {
    resetting.value = false
  }
}

const addRule = () => {
  if (!config.value) return
  config.value.rules.push({
    errorCodes: '',
    action: 'failover_threshold' as FailoverAction,
    threshold: 1,
    waitSeconds: 0
  })
}

const removeRule = (index: number) => {
  if (!config.value || config.value.rules.length <= 1) return
  config.value.rules.splice(index, 1)
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

.rules-table :deep(th),
.rules-table :deep(td) {
  padding: 8px 4px !important;
}

.rules-table :deep(.v-field__input) {
  min-height: 32px !important;
  padding-top: 4px !important;
  padding-bottom: 4px !important;
}

.error-codes-table {
  background: transparent !important;
}

.error-codes-table :deep(tr) {
  background: transparent !important;
}

.error-codes-table :deep(td) {
  padding: 2px 8px !important;
  border: none !important;
  font-size: 0.75rem !important;
}

.error-codes-table .code-cell {
  font-family: monospace;
  font-weight: 600;
  white-space: nowrap;
  color: rgb(var(--v-theme-primary));
  user-select: all;
  cursor: text;
}

.error-codes-table .desc-cell {
  color: rgba(var(--v-theme-on-surface), 0.7);
}
</style>
