<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="700">
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
          <!-- Enable/Disable Toggle -->
          <v-card variant="outlined" class="mb-4 pa-3">
            <div class="d-flex align-center">
              <v-switch
                v-model="config.enabled"
                :label="config.enabled ? t('failover.enabled') : t('failover.disabled')"
                color="primary"
                density="compact"
                hide-details
              />
              <v-tooltip location="bottom" max-width="400">
                <template #activator="{ props }">
                  <v-icon v-bind="props" size="small" class="ml-2 text-grey" style="cursor: help;">mdi-information-outline</v-icon>
                </template>
                <div class="pa-2">
                  <div class="font-weight-bold mb-2">{{ t('failover.errorCodeReference') }}</div>
                  <div class="text-caption">
                    <div>• <strong>401</strong> - {{ t('failover.errorCode401') }}</div>
                    <div>• <strong>403</strong> - {{ t('failover.errorCode403') }}</div>
                    <div>• <strong>429</strong> - {{ t('failover.errorCode429') }}</div>
                    <div>• <strong>500</strong> - {{ t('failover.errorCode500') }}</div>
                    <div>• <strong>502</strong> - {{ t('failover.errorCode502') }}</div>
                    <div>• <strong>503</strong> - {{ t('failover.errorCode503') }}</div>
                    <div>• <strong>504</strong> - {{ t('failover.errorCode504') }}</div>
                    <div>• <strong>others</strong> - {{ t('failover.errorCodeOthers') }}</div>
                  </div>
                </div>
              </v-tooltip>
            </div>
            <div class="text-caption text-grey mt-2">{{ t('failover.enableDescription') }}</div>
          </v-card>

          <!-- Rules Section -->
          <div class="section-title mb-2">{{ t('failover.rulesSection') }}</div>
          <v-card variant="outlined" class="mb-4 pa-3">
            <div class="text-caption text-grey mb-3">{{ t('failover.rulesDescription') }}</div>

            <!-- Rules List -->
            <v-table density="compact" class="mb-3">
              <thead>
                <tr>
                  <th>{{ t('failover.errorCodes') }}</th>
                  <th style="width: 120px">{{ t('failover.threshold') }}</th>
                  <th style="width: 60px"></th>
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
                    <v-text-field
                      v-model.number="rule.threshold"
                      type="number"
                      min="1"
                      max="100"
                      density="compact"
                      variant="outlined"
                      hide-details
                      :disabled="!config.enabled"
                    />
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

          <!-- Info Box -->
          <v-alert type="info" variant="tonal" density="compact">
            <div class="text-caption">
              <strong>{{ t('failover.howItWorks') }}</strong>
              <ul class="mt-1 mb-0 pl-4">
                <li>{{ t('failover.howItWorksItem1') }}</li>
                <li>{{ t('failover.howItWorksItem2') }}</li>
                <li>{{ t('failover.howItWorksItem3') }}</li>
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
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type FailoverConfig, type FailoverRule } from '../services/api'

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
    if (rule.threshold < 1) {
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
  config.value.rules.push({ errorCodes: '', threshold: 1 })
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
</style>
