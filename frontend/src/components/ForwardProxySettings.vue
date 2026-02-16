<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="600">
    <v-card class="settings-card modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4">
        <v-icon class="mr-2">mdi-shield-lock-outline</v-icon>
        {{ t('forwardProxy.title') }}
        <v-spacer />
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
          <!-- Server status -->
          <v-alert v-if="!config.running" type="warning" variant="tonal" density="compact" class="mb-4">
            {{ t('forwardProxy.notRunning') }}
          </v-alert>

          <!-- Enable/Disable Toggle -->
          <v-card variant="outlined" class="mb-4 pa-3">
            <v-switch
              v-model="config.enabled"
              :label="config.enabled ? t('forwardProxy.interceptEnabled') : t('forwardProxy.interceptDisabled')"
              color="primary"
              density="compact"
              hide-details
              :disabled="!config.running"
            />
            <div class="text-caption text-grey mt-2">{{ t('forwardProxy.enableDescription') }}</div>
          </v-card>

          <!-- Intercept Domains -->
          <div class="section-title mb-2">{{ t('forwardProxy.interceptDomains') }}</div>
          <v-card variant="outlined" class="mb-4 pa-3">
            <div v-for="(domain, index) in config.interceptDomains" :key="index" class="d-flex align-center mb-2">
              <v-text-field
                v-model="config.interceptDomains[index]"
                density="compact"
                variant="outlined"
                hide-details
                :placeholder="t('forwardProxy.domainPlaceholder')"
                :disabled="!config.running"
              />
              <v-btn
                icon
                variant="text"
                size="small"
                color="error"
                class="ml-1"
                @click="removeDomain(index)"
                :disabled="!config.running"
              >
                <v-icon size="small">mdi-close</v-icon>
              </v-btn>
            </div>
            <v-btn
              size="small"
              variant="tonal"
              color="primary"
              prepend-icon="mdi-plus"
              @click="addDomain"
              :disabled="!config.running"
            >
              {{ t('forwardProxy.addDomain') }}
            </v-btn>
            <div class="text-caption text-grey mt-2">{{ t('forwardProxy.domainsDescription') }}</div>
          </v-card>

          <!-- CA Certificate Download -->
          <div class="section-title mb-2">{{ t('forwardProxy.caCertSection') }}</div>
          <v-card variant="outlined" class="pa-3">
            <div class="text-caption text-grey mb-3">{{ t('forwardProxy.caCertDescription') }}</div>
            <v-btn
              color="primary"
              variant="elevated"
              size="small"
              prepend-icon="mdi-certificate"
              @click="downloadCACert"
              :loading="downloading"
              :disabled="!config.running"
            >
              {{ t('forwardProxy.downloadCACert') }}
            </v-btn>
          </v-card>

          <!-- Usage instructions -->
          <v-card variant="outlined" class="mt-4 pa-3">
            <div class="section-title mb-2">{{ t('forwardProxy.usageTitle') }}</div>
            <div class="text-caption text-grey">
              <code class="d-block mb-1" style="white-space: pre-wrap; font-size: 0.8rem;">HTTPS_PROXY=http://localhost:{{ proxyPort }} claude</code>
              <div class="mt-2">{{ t('forwardProxy.usageNote') }}</div>
            </div>
          </v-card>
        </div>
        <div v-else class="text-center pa-4 text-grey">
          {{ t('forwardProxy.loadFailed') }}
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
import { api, type ForwardProxyConfig } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const config = ref<ForwardProxyConfig | null>(null)
const loading = ref(false)
const saving = ref(false)
const downloading = ref(false)
const proxyPort = computed(() => config.value?.port ?? 3001)

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
    config.value = await api.getForwardProxyConfig()
  } catch (error) {
    console.error('Failed to load forward proxy config:', error)
    showSnackbar(t('forwardProxy.loadFailed'), 'error')
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  if (!config.value) return

  // Filter out empty domains
  config.value.interceptDomains = config.value.interceptDomains.filter(d => d.trim() !== '')

  saving.value = true
  try {
    await api.updateForwardProxyConfig({
      enabled: config.value.enabled,
      interceptDomains: config.value.interceptDomains
    })
    showSnackbar(t('common.success'), 'success')
    emit('update:modelValue', false)
  } catch (error) {
    console.error('Failed to save forward proxy config:', error)
    showSnackbar(t('forwardProxy.saveFailed'), 'error')
  } finally {
    saving.value = false
  }
}

const addDomain = () => {
  if (config.value) {
    config.value.interceptDomains.push('')
  }
}

const removeDomain = (index: number) => {
  if (config.value) {
    config.value.interceptDomains.splice(index, 1)
  }
}

const downloadCACert = async () => {
  downloading.value = true
  try {
    const blob = await api.downloadForwardProxyCACert()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'ccbridge-ca.pem'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    showSnackbar(t('forwardProxy.certDownloaded'), 'success')
  } catch (error) {
    console.error('Failed to download CA cert:', error)
    showSnackbar(t('forwardProxy.certDownloadFailed'), 'error')
  } finally {
    downloading.value = false
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
