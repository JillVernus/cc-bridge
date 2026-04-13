<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="640">
    <v-card class="settings-card modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4">
        <v-icon class="mr-2">mdi-filter-remove-outline</v-icon>
        {{ t('outboundHeaders.title') }}
        <v-spacer />
        <v-btn icon variant="text" size="small" @click="$emit('update:modelValue', false)" class="modal-action-btn">
          <v-icon>mdi-close</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="flat"
          size="small"
          color="primary"
          @click="saveConfig"
          :loading="saving"
          class="modal-action-btn"
        >
          <v-icon>mdi-check</v-icon>
        </v-btn>
      </v-card-title>
      <v-card-text class="modal-content">
        <div v-if="loading" class="text-center pa-4">
          <v-progress-circular indeterminate size="24" />
        </div>
        <div v-else-if="config">
          <v-card variant="outlined" class="mb-4 pa-3">
            <v-switch
              v-model="config.enabled"
              :label="config.enabled ? t('outboundHeaders.enabled') : t('outboundHeaders.disabled')"
              color="primary"
              density="compact"
              hide-details
            />
            <div class="text-caption text-grey mt-2">{{ t('outboundHeaders.description') }}</div>
          </v-card>

          <div class="section-title mb-2">{{ t('outboundHeaders.rulesSection') }}</div>
          <v-card variant="outlined" class="pa-3">
            <v-combobox
              v-model="config.stripRules"
              :label="t('outboundHeaders.rulesLabel')"
              :placeholder="t('outboundHeaders.rulesPlaceholder')"
              :hint="t('outboundHeaders.rulesHint')"
              chips
              closable-chips
              clearable
              multiple
              persistent-hint
              variant="outlined"
              :disabled="!config.enabled"
            />

            <div class="d-flex align-center mt-3">
              <div class="text-caption text-grey">
                {{ t('outboundHeaders.currentRules', { count: config.stripRules.length }) }}
              </div>
            </div>
            <div class="d-flex flex-wrap ga-2 mt-2">
              <v-chip
                v-for="rule in config.stripRules"
                :key="rule"
                size="small"
                variant="outlined"
                closable
                :disabled="!config.enabled"
                @click:close="removeRule(rule)"
              >
                {{ rule }}
              </v-chip>
            </div>

            <div class="text-caption text-grey mt-3">
              {{ t('outboundHeaders.examples') }}
            </div>
            <div class="d-flex flex-wrap ga-2 mt-2">
              <v-btn
                v-for="example in exampleRules"
                :key="example"
                size="small"
                variant="tonal"
                color="secondary"
                class="quick-add-btn"
                :disabled="!config.enabled"
                @click="addExampleRule(example)"
              >
                {{ example }}
              </v-btn>
            </div>
          </v-card>
        </div>
        <div v-else class="text-center pa-4 text-grey">
          {{ t('outboundHeaders.loadFailed') }}
        </div>
      </v-card-text>
    </v-card>
  </v-dialog>

  <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
    {{ snackbar.text }}
  </v-snackbar>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type OutboundHeaderPolicyConfig } from '../services/api'
import { appendUniqueHeaderRule } from '../utils/outboundHeaderRules'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const loading = ref(false)
const saving = ref(false)
const config = ref<OutboundHeaderPolicyConfig | null>(null)

const exampleRules = ['Cf-*', 'X-Forwarded-*', 'True-Client-IP', 'X-Real-IP', 'Forwarded']

const snackbar = ref({
  show: false,
  text: '',
  color: 'error'
})

const showSnackbar = (text: string, color: string = 'error') => {
  snackbar.value = { show: true, text, color }
}

watch(
  () => props.modelValue,
  newVal => {
    if (newVal) {
      loadConfig()
    }
  }
)

const loadConfig = async () => {
  loading.value = true
  try {
    config.value = await api.getOutboundHeaderPolicyConfig()
  } catch (error) {
    console.error('Failed to load outbound header policy config:', error)
    showSnackbar(t('outboundHeaders.loadFailed'), 'error')
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  if (!config.value) return

  saving.value = true
  try {
    config.value.stripRules = config.value.stripRules
      .map(rule => rule.trim())
      .filter(
        (rule, index, rules) =>
          rule !== '' && rules.findIndex(item => item.toLowerCase() === rule.toLowerCase()) === index
      )

    await api.updateOutboundHeaderPolicyConfig(config.value)
    showSnackbar(t('common.success'), 'success')
    emit('update:modelValue', false)
  } catch (error) {
    console.error('Failed to save outbound header policy config:', error)
    showSnackbar(t('outboundHeaders.saveFailed'), 'error')
  } finally {
    saving.value = false
  }
}

const addExampleRule = (rule: string) => {
  if (!config.value) return
  config.value.stripRules = appendUniqueHeaderRule(config.value.stripRules, rule)
}

const removeRule = (rule: string) => {
  if (!config.value) return
  config.value.stripRules = config.value.stripRules.filter(existing => existing !== rule)
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

.quick-add-btn {
  text-transform: none;
}
</style>
