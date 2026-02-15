<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="760">
    <v-card class="settings-card modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4">
        <v-icon class="mr-2">mdi-account-box-outline</v-icon>
        {{ t('userAgent.title') }}
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
          :disabled="!config || hasValidationError"
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
          <v-alert type="info" variant="tonal" density="compact" class="mb-4">
            {{ t('userAgent.description') }}
          </v-alert>

          <v-card variant="outlined" class="mb-4 pa-3">
            <div class="section-title mb-2">{{ t('userAgent.messagesTitle') }}</div>
            <v-text-field
              v-model="config.messages.latest"
              :label="t('userAgent.latestUserAgent')"
              variant="outlined"
              density="comfortable"
              :error="!isMessagesValid"
              :error-messages="!isMessagesValid ? t('userAgent.messagesInvalid') : ''"
              :placeholder="t('userAgent.messagesPlaceholder')"
            />
            <div class="text-caption text-grey">
              {{ t('userAgent.lastCapturedAt') }}: {{ formatDate(config.messages.lastCapturedAt) }}
            </div>
          </v-card>

          <v-card variant="outlined" class="pa-3">
            <div class="section-title mb-2">{{ t('userAgent.responsesTitle') }}</div>
            <v-text-field
              v-model="config.responses.latest"
              :label="t('userAgent.latestUserAgent')"
              variant="outlined"
              density="comfortable"
              :error="!isResponsesValid"
              :error-messages="!isResponsesValid ? t('userAgent.responsesInvalid') : ''"
              :placeholder="t('userAgent.responsesPlaceholder')"
            />
            <div class="text-caption text-grey">
              {{ t('userAgent.lastCapturedAt') }}: {{ formatDate(config.responses.lastCapturedAt) }}
            </div>
          </v-card>
        </div>
        <div v-else class="text-center pa-4 text-grey">
          {{ t('userAgent.loadFailed') }}
        </div>
      </v-card-text>
    </v-card>
  </v-dialog>

  <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
    {{ snackbar.text }}
  </v-snackbar>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type UserAgentConfig } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const config = ref<UserAgentConfig | null>(null)
const loading = ref(false)
const saving = ref(false)

const snackbar = ref({
  show: false,
  text: '',
  color: 'error'
})

const showSnackbar = (text: string, color: string = 'error') => {
  snackbar.value = { show: true, text, color }
}

const MESSAGES_UA_RE = /^claude-cli\/\d+(?:\.\d+)*/i
const RESPONSES_UA_RE = /^codex_cli_rs\/\d+(?:\.\d+)*/i

const isMessagesValid = computed(() => {
  if (!config.value) return false
  return MESSAGES_UA_RE.test(config.value.messages.latest.trim())
})

const isResponsesValid = computed(() => {
  if (!config.value) return false
  return RESPONSES_UA_RE.test(config.value.responses.latest.trim())
})

const hasValidationError = computed(() => !isMessagesValid.value || !isResponsesValid.value)

const formatDate = (iso?: string): string => {
  if (!iso) return t('userAgent.notCaptured')
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

const loadConfig = async () => {
  loading.value = true
  try {
    config.value = await api.getUserAgentConfig()
  } catch (error) {
    console.error('Failed to load user-agent config:', error)
    showSnackbar(t('userAgent.loadFailed'), 'error')
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  if (!config.value || hasValidationError.value) return

  saving.value = true
  try {
    config.value = await api.updateUserAgentConfig(config.value)
    showSnackbar(t('common.success'), 'success')
    emit('update:modelValue', false)
  } catch (error) {
    console.error('Failed to save user-agent config:', error)
    showSnackbar(t('userAgent.saveFailed'), 'error')
  } finally {
    saving.value = false
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) loadConfig()
  }
)
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
