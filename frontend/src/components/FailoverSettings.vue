<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="900">
    <v-card class="settings-card modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4">
        <v-icon class="mr-2">mdi-swap-horizontal</v-icon>
        {{ t('failover.title') }}
        <v-spacer />
        <v-btn variant="text" size="small" color="warning" @click="resetConfig" :loading="resetting" :disabled="!config" class="mr-2">
          {{ t('failover.reset') }}
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
            <div class="text-caption text-grey mb-3">{{ t('failover.rulesDescriptionNew') }}</div>

            <!-- Rules List with Expandable Rows -->
            <div class="rules-list">
              <div v-for="(rule, ruleIndex) in config.rules" :key="ruleIndex" class="rule-item mb-2">
                <!-- Collapsed View Header -->
                <v-card variant="outlined" class="rule-card">
                  <div class="rule-header d-flex align-center pa-2" @click="toggleRule(ruleIndex)">
                    <v-icon size="small" class="mr-2">
                      {{ expandedRules[ruleIndex] ? 'mdi-chevron-down' : 'mdi-chevron-right' }}
                    </v-icon>
                    <div class="rule-summary flex-grow-1">
                      <code class="error-code">{{ rule.errorCodes || t('failover.noErrorCode') }}</code>
                      <span class="text-grey mx-2">→</span>
                      <span class="action-chain-summary">{{ getActionChainSummary(rule) }}</span>
                    </div>
                    <v-btn
                      icon
                      size="x-small"
                      variant="text"
                      color="error"
                      @click.stop="removeRule(ruleIndex)"
                      :disabled="!config.enabled || config.rules.length <= 1"
                    >
                      <v-icon size="16">mdi-delete</v-icon>
                    </v-btn>
                  </div>

                  <!-- Expanded View -->
                  <v-expand-transition>
                    <div v-if="expandedRules[ruleIndex]" class="rule-details pa-3 pt-0">
                      <v-divider class="mb-3" />

                      <!-- Error Pattern -->
                      <div class="mb-3">
                        <div class="text-caption text-grey mb-1">{{ t('failover.errorPattern') }}</div>
                        <v-text-field
                          v-model="rule.errorCodes"
                          density="compact"
                          variant="outlined"
                          hide-details
                          :placeholder="t('failover.codesPlaceholder')"
                          :disabled="!config.enabled"
                        />
                      </div>

                      <!-- Action Chain Editor -->
                      <div class="text-caption text-grey mb-2">{{ t('failover.actionChain') }}</div>
                      <div class="action-chain-editor">
                        <div v-for="(step, stepIndex) in rule.actionChain" :key="stepIndex" class="action-step mb-2">
                          <div class="d-flex align-center">
                            <div class="step-number text-caption text-grey mr-2">{{ stepIndex + 1 }}.</div>

                            <!-- Action Type -->
                            <v-select
                              v-model="step.action"
                              :items="actionTypeOptions"
                              item-title="label"
                              item-value="value"
                              density="compact"
                              variant="outlined"
                              hide-details
                              :disabled="!config.enabled"
                              style="max-width: 140px"
                              class="mr-2"
                            />

                            <!-- Wait Seconds (only for retry) -->
                            <template v-if="step.action === 'retry'">
                              <div class="text-caption text-grey mx-2">{{ t('failover.wait') }}:</div>
                              <v-text-field
                                v-model.number="step.waitSeconds"
                                type="number"
                                min="0"
                                max="300"
                                density="compact"
                                variant="outlined"
                                hide-details
                                :disabled="!config.enabled"
                                :placeholder="t('failover.autoDetect')"
                                style="max-width: 80px"
                                class="mr-2"
                              />
                              <div class="text-caption text-grey mr-2">s</div>

                              <!-- Max Attempts -->
                              <div class="text-caption text-grey mx-2">{{ t('failover.max') }}:</div>
                              <v-text-field
                                v-model.number="step.maxAttempts"
                                type="number"
                                min="1"
                                max="99"
                                density="compact"
                                variant="outlined"
                                hide-details
                                :disabled="!config.enabled"
                                style="max-width: 70px"
                                class="mr-2"
                              />
                              <div class="text-caption text-grey">{{ t('failover.attempts') }}</div>
                            </template>

                            <v-spacer />

                            <!-- Remove Step Button -->
                            <v-btn
                              icon
                              size="x-small"
                              variant="text"
                              color="error"
                              @click="removeStep(ruleIndex, stepIndex)"
                              :disabled="!config.enabled || (rule.actionChain?.length || 0) <= 1"
                            >
                              <v-icon size="14">mdi-close</v-icon>
                            </v-btn>
                          </div>

                          <!-- Arrow between steps -->
                          <div v-if="stepIndex < (rule.actionChain?.length || 0) - 1" class="step-arrow text-center text-grey">
                            <v-icon size="16">mdi-arrow-down</v-icon>
                          </div>
                        </div>

                        <!-- Add Step Button -->
                        <v-btn
                          size="small"
                          variant="text"
                          @click="addStep(ruleIndex)"
                          :disabled="!config.enabled || (rule.actionChain?.length || 0) >= 5"
                          class="mt-1"
                        >
                          <v-icon start size="16">mdi-plus</v-icon>
                          {{ t('failover.addStep') }}
                        </v-btn>
                      </div>
                    </div>
                  </v-expand-transition>
                </v-card>
              </div>
            </div>

            <v-btn
              size="small"
              variant="outlined"
              @click="addRule"
              :disabled="!config.enabled"
              class="mt-2"
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
                    <v-chip size="x-small" color="info" class="mr-2">{{ t('failover.actionRetry') }}</v-chip>
                    {{ t('failover.actionRetryDesc') }}
                  </div>
                  <div class="mb-1">
                    <v-chip size="x-small" color="warning" class="mr-2">{{ t('failover.actionFailover') }}</v-chip>
                    {{ t('failover.actionFailoverDesc') }}
                  </div>
                  <div>
                    <v-chip size="x-small" color="purple" class="mr-2">{{ t('failover.actionSuspend') }}</v-chip>
                    {{ t('failover.actionSuspendDesc') }}
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
                <li>{{ t('failover.howItWorksItem1New') }}</li>
                <li>{{ t('failover.howItWorksItem2New') }}</li>
                <li>{{ t('failover.howItWorksItem3New') }}</li>
                <li>{{ t('failover.howItWorksItem4New') }}</li>
              </ul>
            </div>
          </v-alert>
        </div>
        <div v-else class="text-center pa-4 text-grey">
          {{ t('failover.loadFailed') }}
        </div>
      </v-card-text>
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
import { api, type FailoverConfig, type FailoverRule, type ActionStep, type FailoverActionType } from '../services/api'

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
const expandedRules = ref<Record<number, boolean>>({})

// Action type options for dropdown
const actionTypeOptions = computed(() => [
  { value: 'retry', label: t('failover.actionRetry') },
  { value: 'failover', label: t('failover.actionFailover') },
  { value: 'suspend', label: t('failover.actionSuspend') }
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

// Toggle rule expansion
const toggleRule = (index: number) => {
  expandedRules.value[index] = !expandedRules.value[index]
}

// Get action chain summary for collapsed view
const getActionChainSummary = (rule: FailoverRule): string => {
  const chain = rule.actionChain ?? []
  if (chain.length === 0) {
    return t('failover.noActions')
  }
  return chain.map(step => {
    switch (step.action) {
      case 'retry': {
        const waitSeconds = typeof step.waitSeconds === 'number' ? step.waitSeconds : 0
        const wait = waitSeconds === 0 ? t('failover.autoDetect') : `${waitSeconds}s`
        const maxAttempts = typeof step.maxAttempts === 'number' ? step.maxAttempts : 1
        const max = maxAttempts === 99 ? '∞' : `${maxAttempts}`
        return `${t('failover.actionRetry')}(${wait}, ${max}x)`
      }
      case 'failover':
        return t('failover.actionFailover')
      case 'suspend':
        return t('failover.actionSuspend')
      default:
        return String(step.action)
    }
  }).join(' → ')
}

// Load config when dialog opens
watch(() => props.modelValue, (newVal) => {
  if (newVal) {
    loadConfig()
  } else {
    expandedRules.value = {}
  }
})

// Migrate legacy rule format to new actionChain format (client-side)
const migrateRule = (rule: FailoverRule): FailoverRule => {
  if (rule.actionChain && rule.actionChain.length > 0) {
    return rule // Already in new format
  }

  // Migrate from legacy format
  const actionChain: ActionStep[] = []
  const legacyAction = rule.action

  switch (legacyAction) {
    case 'suspend_channel':
      actionChain.push({ action: 'suspend' })
      break
    case 'failover_immediate':
      actionChain.push({ action: 'failover' })
      break
    case 'failover_threshold': {
      const threshold = typeof rule.threshold === 'number' ? rule.threshold : 1
      actionChain.push({ action: 'retry', waitSeconds: 0, maxAttempts: threshold > 0 ? threshold : 1 })
      actionChain.push({ action: 'failover' })
      break
    }
    case 'retry_wait': {
      const waitSeconds = typeof rule.waitSeconds === 'number' ? rule.waitSeconds : 0
      actionChain.push({ action: 'retry', waitSeconds, maxAttempts: 99 })
      actionChain.push({ action: 'failover' })
      break
    }
    default:
      // Default to single failover action
      actionChain.push({ action: 'failover' })
  }

  return {
    errorCodes: rule.errorCodes,
    actionChain
  }
}

const loadConfig = async () => {
  loading.value = true
  try {
    const data = await api.getFailoverConfig()
    // Migrate rules if needed
    if (data?.rules) {
      data.rules = data.rules.map(migrateRule)
    }
    config.value = data
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
    if (!rule.actionChain || rule.actionChain.length === 0) {
      showSnackbar(t('failover.emptyActionChainError'), 'error')
      return
    }
    for (const step of rule.actionChain) {
      if (step.action === 'retry' && (!step.maxAttempts || step.maxAttempts < 1)) {
        showSnackbar(t('failover.invalidMaxAttemptsError'), 'error')
        return
      }
    }
  }

  saving.value = true
  try {
    // Clean up legacy fields before saving
    const cleanConfig: FailoverConfig = {
      enabled: config.value.enabled,
      rules: config.value.rules.map(rule => ({
        errorCodes: rule.errorCodes,
        actionChain: rule.actionChain
      }))
    }
    await api.updateFailoverConfig(cleanConfig)
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
    const data = await api.resetFailoverConfig()
    // Migrate rules if needed
    if (data?.rules) {
      data.rules = data.rules.map(migrateRule)
    }
    config.value = data
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
  const newIndex = config.value.rules.length
  config.value.rules.push({
    errorCodes: '',
    actionChain: [{ action: 'failover' }]
  })
  expandedRules.value[newIndex] = true
}

const removeRule = (index: number) => {
  if (!config.value || config.value.rules.length <= 1) return
  config.value.rules.splice(index, 1)
  // Update expanded indices after removal
  const nextExpanded: Record<number, boolean> = {}
  for (const [key, isExpanded] of Object.entries(expandedRules.value)) {
    if (!isExpanded) continue
    const idx = Number(key)
    if (Number.isNaN(idx)) continue
    if (idx < index) {
      nextExpanded[idx] = true
    } else if (idx > index) {
      nextExpanded[idx - 1] = true
    }
  }
  expandedRules.value = nextExpanded
}

const addStep = (ruleIndex: number) => {
  if (!config.value) return
  const rule = config.value.rules[ruleIndex]
  if (!rule.actionChain) {
    rule.actionChain = []
  }
  if (rule.actionChain.length >= 5) return
  rule.actionChain.push({ action: 'failover' })
}

const removeStep = (ruleIndex: number, stepIndex: number) => {
  if (!config.value) return
  const rule = config.value.rules[ruleIndex]
  if (!rule.actionChain || rule.actionChain.length <= 1) return
  rule.actionChain.splice(stepIndex, 1)
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

.rule-card {
  transition: all 0.2s ease;
}

.rule-header {
  cursor: pointer;
  user-select: none;
}

.rule-header:hover {
  background: rgba(var(--v-theme-on-surface), 0.05);
}

.error-code {
  background: rgba(var(--v-theme-primary), 0.1);
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 0.8rem;
}

.action-chain-summary {
  font-size: 0.85rem;
  color: rgba(var(--v-theme-on-surface), 0.7);
}

.action-step {
  background: rgba(var(--v-theme-on-surface), 0.03);
  border-radius: 4px;
  padding: 8px;
}

.step-arrow {
  padding: 4px 0;
}

.step-number {
  min-width: 20px;
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
