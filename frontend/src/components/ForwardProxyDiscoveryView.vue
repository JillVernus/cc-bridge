<template>
  <div class="d-flex flex-column ga-4">
    <v-card variant="outlined" class="pa-4">
      <div class="d-flex align-center justify-space-between flex-wrap ga-3">
        <div>
          <div class="text-h6">{{ t('forwardProxy.discoveryTitle') }}</div>
          <div class="text-body-2 text-medium-emphasis">{{ t('forwardProxy.discoveryDescription') }}</div>
        </div>
        <div class="d-flex ga-2 flex-wrap">
          <v-btn variant="tonal" color="primary" prepend-icon="mdi-refresh" @click="loadData" :loading="loading">
            {{ t('common.refresh') }}
          </v-btn>
          <v-btn
            variant="tonal"
            color="error"
            prepend-icon="mdi-delete-sweep-outline"
            @click="clearEntries"
            :loading="clearing"
            :disabled="entries.length === 0"
          >
            {{ t('common.clear') }}
          </v-btn>
        </div>
      </div>
    </v-card>

    <v-alert v-if="!config.running" type="warning" variant="tonal">
      {{ t('forwardProxy.notRunning') }}
    </v-alert>

    <v-alert v-if="error" type="error" variant="tonal">
      {{ error }}
    </v-alert>

    <v-card variant="outlined">
      <v-data-table
        :headers="headers"
        :items="entries"
        :loading="loading"
        :items-per-page="25"
        item-value="host"
        density="comfortable"
      >
        <template #item.host="{ item }">
          <div class="font-weight-medium">{{ item.host }}</div>
          <div v-if="getDomainAlias(item.host)" class="text-caption text-medium-emphasis">
            {{ getDomainAlias(item.host) }}
          </div>
          <div class="text-caption text-medium-emphasis">:{{ item.port }}</div>
        </template>

        <template #item.transport="{ item }">
          <v-chip size="small" variant="tonal" :color="item.intercepted ? 'primary' : 'default'">
            {{ item.transport }}
          </v-chip>
        </template>

        <template #item.intercepted="{ item }">
          <v-chip size="small" :color="item.intercepted ? 'success' : 'default'" variant="tonal">
            {{ item.intercepted ? t('common.yes') : t('common.no') }}
          </v-chip>
        </template>

        <template #item.lastRequest="{ item }">
          <div v-if="item.lastMethod || item.lastPath" class="text-body-2">
            <span v-if="item.lastMethod" class="font-weight-medium">{{ item.lastMethod }}</span>
            <span v-if="item.lastMethod && item.lastPath"> </span>
            <span v-if="item.lastPath" class="text-medium-emphasis">{{ item.lastPath }}</span>
          </div>
          <div v-if="headerEntries(item).length > 0" class="mt-1">
            <v-tooltip location="top" max-width="420">
              <template #activator="{ props }">
                <v-chip v-bind="props" size="x-small" variant="outlined" color="secondary">
                  {{ t('forwardProxy.discoveryHeadersChip', { count: headerEntries(item).length }) }}
                </v-chip>
              </template>
              <div class="text-caption">
                <div class="d-flex align-center justify-space-between mb-2 ga-2">
                  <strong>{{ t('forwardProxy.discoveryHeadersTitle') }}</strong>
                  <v-btn
                    v-if="hasSensitiveHeaders(item)"
                    size="x-small"
                    variant="text"
                    density="comfortable"
                    :icon="isHeadersRevealed(item) ? 'mdi-eye-off' : 'mdi-eye'"
                    :title="
                      isHeadersRevealed(item)
                        ? t('forwardProxy.discoveryHideSensitiveHeaders')
                        : t('forwardProxy.discoveryShowSensitiveHeaders')
                    "
                    @click.stop="toggleHeaderReveal(item)"
                  />
                </div>
                <div v-for="[key, value] in headerEntries(item)" :key="key" class="mb-1">
                  <strong>{{ key }}:</strong> {{ value }}
                </div>
              </div>
            </v-tooltip>
          </div>
          <span v-else class="text-medium-emphasis">-</span>
        </template>

        <template #item.firstSeenAt="{ item }">
          {{ formatDateTime(item.firstSeenAt) }}
        </template>

        <template #item.lastSeenAt="{ item }">
          {{ formatDateTime(item.lastSeenAt) }}
        </template>

        <template #item.actions="{ item }">
          <v-btn
            size="small"
            color="primary"
            variant="tonal"
            prepend-icon="mdi-plus"
            @click="addToInterceptDomains(item.host)"
            :disabled="isDomainIntercepted(item.host)"
          >
            {{
              isDomainIntercepted(item.host)
                ? t('forwardProxy.alreadyIntercepted')
                : t('forwardProxy.addToInterceptDomains')
            }}
          </v-btn>
        </template>

        <template #no-data>
          <div class="pa-6 text-medium-emphasis text-center">
            {{ t('forwardProxy.discoveryEmpty') }}
          </div>
        </template>
      </v-data-table>
    </v-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type ForwardProxyConfig, type ForwardProxyDiscoveryEntry } from '../services/api'

const { t } = useI18n()

const loading = ref(false)
const clearing = ref(false)
const error = ref('')
const config = ref<ForwardProxyConfig>({
  enabled: false,
  interceptDomains: [],
  domainAliases: {},
  xInitiatorOverride: {
    enabled: false,
    mode: 'fixed_window',
    durationSeconds: 300
  },
  xInitiatorOverrideRuntime: {
    enabled: false,
    mode: 'fixed_window',
    activeDomains: 0,
    nearestRemainingSeconds: 0
  },
  running: false,
  port: 3001
})
const entries = ref<ForwardProxyDiscoveryEntry[]>([])
const revealedHeadersByEntry = ref<Record<string, boolean>>({})

const headers = computed(() => [
  { title: t('forwardProxy.discoveryHost'), key: 'host', sortable: true },
  { title: t('forwardProxy.discoveryTransport'), key: 'transport', sortable: true },
  { title: t('forwardProxy.discoveryIntercepted'), key: 'intercepted', sortable: true },
  { title: t('forwardProxy.discoverySeenCount'), key: 'seenCount', sortable: true },
  { title: t('forwardProxy.discoveryLastRequest'), key: 'lastRequest', sortable: false },
  { title: t('forwardProxy.discoveryFirstSeen'), key: 'firstSeenAt', sortable: true },
  { title: t('forwardProxy.discoveryLastSeen'), key: 'lastSeenAt', sortable: true },
  { title: t('common.actions'), key: 'actions', sortable: false, align: 'end' as const }
])

const formatDateTime = (value?: string) => {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

const discoveryEntryKey = (item: ForwardProxyDiscoveryEntry) => `${item.host}:${item.port}`

const isSensitiveHeader = (key: string) => {
  const normalized = key.trim().toLowerCase()
  return (
    normalized === 'authorization' ||
    normalized === 'x-api-key' ||
    normalized === 'cookie' ||
    normalized === 'set-cookie' ||
    normalized === 'proxy-authorization'
  )
}

const isHeadersRevealed = (item: ForwardProxyDiscoveryEntry) =>
  revealedHeadersByEntry.value[discoveryEntryKey(item)] === true

const hasSensitiveHeaders = (item: ForwardProxyDiscoveryEntry) =>
  Object.keys(item.lastRequestHeadersRaw ?? item.lastRequestHeaders ?? {}).some(isSensitiveHeader)

const toggleHeaderReveal = (item: ForwardProxyDiscoveryEntry) => {
  const key = discoveryEntryKey(item)
  revealedHeadersByEntry.value[key] = !revealedHeadersByEntry.value[key]
}

const headerEntries = (item: ForwardProxyDiscoveryEntry) => {
  const source =
    isHeadersRevealed(item) && item.lastRequestHeadersRaw ? item.lastRequestHeadersRaw : item.lastRequestHeaders
  return Object.entries(source ?? {}).sort((a, b) => a[0].localeCompare(b[0]))
}

const isDomainIntercepted = (host: string) => {
  const normalizedHost = host.trim().toLowerCase()
  return config.value.interceptDomains.some(domain => domain.trim().toLowerCase() === normalizedHost)
}

const getDomainAlias = (host: string) => {
  const normalizedHost = host.trim().toLowerCase()
  return config.value.domainAliases?.[normalizedHost] || ''
}

const loadData = async () => {
  loading.value = true
  error.value = ''
  try {
    const [proxyConfig, discovery] = await Promise.all([api.getForwardProxyConfig(), api.getForwardProxyDiscovery()])
    config.value = proxyConfig
    entries.value = discovery.entries
    const nextRevealState: Record<string, boolean> = {}
    for (const item of discovery.entries) {
      const key = discoveryEntryKey(item)
      if (revealedHeadersByEntry.value[key]) {
        nextRevealState[key] = true
      }
    }
    revealedHeadersByEntry.value = nextRevealState
  } catch (err) {
    console.error('Failed to load forward proxy discovery:', err)
    error.value = t('forwardProxy.discoveryLoadFailed')
  } finally {
    loading.value = false
  }
}

const clearEntries = async () => {
  clearing.value = true
  error.value = ''
  try {
    const result = await api.clearForwardProxyDiscovery()
    entries.value = result.entries
    revealedHeadersByEntry.value = {}
  } catch (err) {
    console.error('Failed to clear forward proxy discovery:', err)
    error.value = t('forwardProxy.discoveryClearFailed')
  } finally {
    clearing.value = false
  }
}

const addToInterceptDomains = async (host: string) => {
  if (isDomainIntercepted(host)) return
  error.value = ''
  try {
    const interceptDomains = [...config.value.interceptDomains, host].sort((a, b) => a.localeCompare(b))
    config.value = await api.updateForwardProxyConfig({
      enabled: config.value.enabled,
      interceptDomains
    })
  } catch (err) {
    console.error('Failed to update intercept domains from discovery:', err)
    error.value = t('forwardProxy.discoveryPromoteFailed')
  }
}

onMounted(() => {
  loadData()
})
</script>
