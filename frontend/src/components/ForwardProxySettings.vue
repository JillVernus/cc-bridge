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
            <div
              v-for="(row, index) in interceptDomainRows"
              :key="`intercept-domain-${index}`"
              class="d-flex align-center ga-2 mb-2 flex-wrap"
            >
              <v-text-field
                v-model="row.domain"
                density="compact"
                variant="outlined"
                hide-details
                :placeholder="t('forwardProxy.domainPlaceholder')"
                :disabled="!config.running"
              />
              <v-text-field
                v-model="row.alias"
                density="compact"
                variant="outlined"
                hide-details
                :placeholder="t('forwardProxy.domainAliasPlaceholder')"
                :disabled="!config.running"
              />
              <v-btn
                icon
                variant="text"
                size="small"
                color="error"
                @click="removeInterceptDomainRow(index)"
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
              @click="addInterceptDomainRow"
              :disabled="!config.running"
            >
              {{ t('forwardProxy.addDomain') }}
            </v-btn>
            <div class="text-caption text-grey mt-2">{{ t('forwardProxy.domainsDescription') }}</div>
          </v-card>

          <!-- X-Initiator Override -->
          <div class="section-title mb-2">{{ t('forwardProxy.xInitiatorOverrideSection') }}</div>
          <v-card variant="outlined" class="mb-4 pa-3">
            <v-switch
              v-model="config.xInitiatorOverride.enabled"
              :label="
                config.xInitiatorOverride.enabled
                  ? t('forwardProxy.xInitiatorOverrideEnabled')
                  : t('forwardProxy.xInitiatorOverrideDisabled')
              "
              color="primary"
              density="compact"
              hide-details
              :disabled="!config.running"
            />
            <div class="text-caption text-grey mt-2 mb-3">{{ t('forwardProxy.xInitiatorOverrideDescription') }}</div>

            <v-select
              v-model="config.xInitiatorOverride.mode"
              :items="xInitiatorModeOptions"
              item-title="title"
              item-value="value"
              density="compact"
              variant="outlined"
              :label="t('forwardProxy.xInitiatorOverrideMode')"
              hide-details
              class="mb-3"
              :disabled="!config.running || !config.xInitiatorOverride.enabled"
            />

            <v-text-field
              v-model.number="config.xInitiatorOverride.durationSeconds"
              type="number"
              min="1"
              density="compact"
              variant="outlined"
              :label="t('forwardProxy.xInitiatorOverrideDuration')"
              hide-details
              class="mb-2"
              :disabled="!config.running || !config.xInitiatorOverride.enabled"
            />
            <v-text-field
              v-if="showOverrideTimesField"
              v-model.number="config.xInitiatorOverride.overrideTimes"
              type="number"
              min="1"
              density="compact"
              variant="outlined"
              :label="t('forwardProxy.xInitiatorOverrideOverrideTimes')"
              hide-details
              class="mb-2"
              :disabled="!config.running || !config.xInitiatorOverride.enabled"
            />
            <v-text-field
              v-if="showTotalCostField"
              v-model.number="config.xInitiatorOverride.totalCost"
              type="number"
              min="0.01"
              step="0.01"
              density="compact"
              variant="outlined"
              :label="t('forwardProxy.xInitiatorOverrideTotalCost')"
              hide-details
              class="mb-2"
              :disabled="!config.running || !config.xInitiatorOverride.enabled"
            />
            <div class="text-caption text-grey">{{ xInitiatorOverrideModeHint }}</div>
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
              <div class="mb-1">{{ t('forwardProxy.usageBasicLabel') }}</div>
              <code class="d-block mb-2" style="white-space: pre-wrap; font-size: 0.8rem"
                >HTTPS_PROXY=http://localhost:{{ proxyPort }} claude</code
              >
              <div class="mb-1">{{ t('forwardProxy.usageRecommendedLabel') }}</div>
              <code class="d-block mb-2" style="white-space: pre-wrap; font-size: 0.8rem"
                >HTTPS_PROXY=http://localhost:{{ proxyPort }} NO_PROXY=127.0.0.1,localhost,::1,.local claude</code
              >
              <div class="mb-1">{{ t('forwardProxy.usageExportLabel') }}</div>
              <code class="d-block mb-2" style="white-space: pre-wrap; font-size: 0.8rem"
                >export HTTPS_PROXY=http://localhost:{{ proxyPort }} export NO_PROXY=127.0.0.1,localhost,::1,.local
                claude</code
              >
              <div>{{ t('forwardProxy.usageNote') }}</div>
              <div class="mt-2">{{ t('forwardProxy.noProxyNote') }}</div>
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
const interceptDomainRows = ref<Array<{ domain: string; alias: string }>>([])
const loading = ref(false)
const saving = ref(false)
const downloading = ref(false)
const proxyPort = computed(() => config.value?.port ?? 3001)
const xInitiatorModeOptions = computed(() => [
  { title: t('forwardProxy.xInitiatorOverrideModeFixedWindow'), value: 'fixed_window' },
  { title: t('forwardProxy.xInitiatorOverrideModeRelativeCountdown'), value: 'relative_countdown' },
  { title: t('forwardProxy.xInitiatorOverrideModeWindowedQuota'), value: 'windowed_quota' },
  { title: t('forwardProxy.xInitiatorOverrideModeWindowedCost'), value: 'windowed_cost' }
])
const showOverrideTimesField = computed(
  () => config.value?.xInitiatorOverride.enabled && config.value.xInitiatorOverride.mode === 'windowed_quota'
)
const showTotalCostField = computed(
  () => config.value?.xInitiatorOverride.enabled && config.value.xInitiatorOverride.mode === 'windowed_cost'
)
const xInitiatorOverrideModeHint = computed(() => {
  const mode = config.value?.xInitiatorOverride.mode
  if (mode === 'relative_countdown') {
    return t('forwardProxy.xInitiatorOverrideHintRelativeCountdown')
  }
  if (mode === 'windowed_quota') {
    return t('forwardProxy.xInitiatorOverrideHintWindowedQuota')
  }
  if (mode === 'windowed_cost') {
    return t('forwardProxy.xInitiatorOverrideHintWindowedCost')
  }
  return t('forwardProxy.xInitiatorOverrideHintFixedWindow')
})

// Snackbar
const snackbar = ref({
  show: false,
  text: '',
  color: 'error'
})

const showSnackbar = (text: string, color: string = 'error') => {
  snackbar.value = { show: true, text, color }
}

const clampPositiveInt = (value: unknown, fallback: number) => Math.max(1, Math.trunc(Number(value) || fallback))
const clampPositiveNumber = (value: unknown, fallback: number) => {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
}

const defaultForwardProxyConfig = (): ForwardProxyConfig => ({
  enabled: false,
  interceptDomains: [],
  domainAliases: {},
  xInitiatorOverride: {
    enabled: false,
    mode: 'fixed_window',
    durationSeconds: 300,
    overrideTimes: 1,
    totalCost: 1
  },
  xInitiatorOverrideRuntime: {
    enabled: false,
    mode: 'fixed_window',
    activeDomains: 0,
    nearestRemainingSeconds: 0,
    domains: []
  },
  running: false,
  port: 3001
})

const syncInterceptDomainRows = (cfg: ForwardProxyConfig) => {
  const rows = new Map<string, string>()

  for (const domain of cfg.interceptDomains ?? []) {
    const normalizedDomain = domain.trim().toLowerCase()
    if (!normalizedDomain) continue
    rows.set(normalizedDomain, cfg.domainAliases?.[normalizedDomain] || '')
  }

  for (const [domain, alias] of Object.entries(cfg.domainAliases ?? {})) {
    const normalizedDomain = domain.trim().toLowerCase()
    if (!normalizedDomain) continue
    rows.set(normalizedDomain, alias)
  }

  interceptDomainRows.value = Array.from(rows.entries())
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([domain, alias]) => ({ domain, alias }))
}

// Load config when dialog opens
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
    const loaded = await api.getForwardProxyConfig()
    config.value = {
      ...defaultForwardProxyConfig(),
      ...loaded,
      domainAliases: {
        ...defaultForwardProxyConfig().domainAliases,
        ...(loaded.domainAliases ?? {})
      },
      xInitiatorOverride: {
        ...defaultForwardProxyConfig().xInitiatorOverride,
        ...loaded.xInitiatorOverride
      },
      xInitiatorOverrideRuntime: {
        ...defaultForwardProxyConfig().xInitiatorOverrideRuntime,
        ...loaded.xInitiatorOverrideRuntime,
        domains: loaded.xInitiatorOverrideRuntime?.domains ?? []
      }
    }
    syncInterceptDomainRows(config.value)
  } catch (error) {
    console.error('Failed to load forward proxy config:', error)
    showSnackbar(t('forwardProxy.loadFailed'), 'error')
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  if (!config.value) return

  const interceptDomains: string[] = []
  const cleanedDomainAliases: Record<string, string> = {}
  for (const row of interceptDomainRows.value) {
    const domain = row.domain.trim().toLowerCase()
    const alias = row.alias.trim()
    if (!domain) continue
    interceptDomains.push(domain)
    if (alias) {
      cleanedDomainAliases[domain] = alias
    }
  }
  config.value.interceptDomains = [...new Set(interceptDomains)].sort((a, b) => a.localeCompare(b))
  config.value.domainAliases = cleanedDomainAliases

  const durationSeconds = clampPositiveInt(config.value.xInitiatorOverride.durationSeconds, 300)
  const overrideTimes = clampPositiveInt(config.value.xInitiatorOverride.overrideTimes, 1)
  const totalCost = clampPositiveNumber(config.value.xInitiatorOverride.totalCost, 1)

  config.value.xInitiatorOverride.durationSeconds = durationSeconds
  config.value.xInitiatorOverride.overrideTimes = overrideTimes
  config.value.xInitiatorOverride.totalCost = totalCost

  saving.value = true
  try {
    await api.updateForwardProxyConfig({
      enabled: config.value.enabled,
      interceptDomains: config.value.interceptDomains,
      domainAliases: cleanedDomainAliases,
      xInitiatorOverride: {
        ...config.value.xInitiatorOverride,
        durationSeconds,
        overrideTimes,
        totalCost
      }
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

const addInterceptDomainRow = () => {
  interceptDomainRows.value.push({ domain: '', alias: '' })
}

const removeInterceptDomainRow = (index: number) => {
  interceptDomainRows.value.splice(index, 1)
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
