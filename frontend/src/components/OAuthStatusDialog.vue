<template>
  <v-dialog v-model="dialogVisible" max-width="600" persistent>
    <v-card>
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2" color="primary">mdi-shield-account</v-icon>
        {{ t('oauth.status') }}
        <v-spacer />
        <v-btn icon variant="text" @click="close">
          <v-icon>mdi-close</v-icon>
        </v-btn>
      </v-card-title>

      <v-divider />

      <v-card-text class="pa-4">
        <!-- Loading state -->
        <div v-if="loading" class="d-flex justify-center align-center py-8">
          <v-progress-circular indeterminate color="primary" />
        </div>

        <!-- Error state -->
        <v-alert v-else-if="error" type="error" variant="tonal" class="mb-0">
          {{ error }}
        </v-alert>

        <!-- Not configured state -->
        <v-alert v-else-if="!oauthStatus?.configured" type="warning" variant="tonal" class="mb-0">
          <div class="d-flex align-center">
            <v-icon class="mr-2">mdi-alert-circle</v-icon>
            {{ t('oauth.notConfigured') }}
          </div>
        </v-alert>

        <!-- OAuth status display -->
        <div v-else-if="oauthStatus?.status" class="oauth-status-content">
          <!-- Token status indicator -->
          <div class="d-flex align-center mb-4">
            <v-chip
              :color="tokenStatusColor"
              variant="tonal"
              size="small"
              class="mr-2"
            >
              <v-icon start size="small">{{ tokenStatusIcon }}</v-icon>
              {{ t(`oauth.tokenStatus.${oauthStatus.tokenStatus || 'valid'}`) }}
            </v-chip>
            <span v-if="oauthStatus.tokenExpiresIn" class="text-caption text-medium-emphasis">
              {{ t('oauth.expiresIn', { seconds: oauthStatus.tokenExpiresIn }) }}
            </span>
          </div>

          <!-- Quota exceeded warning -->
          <v-alert
            v-if="oauthStatus.quota?.is_exceeded"
            type="error"
            variant="tonal"
            class="mb-4"
            density="compact"
          >
            <div class="d-flex align-center">
              <v-icon class="mr-2">mdi-alert-octagon</v-icon>
              <div>
                <div class="font-weight-medium">{{ t('oauth.quotaExceeded') }}</div>
                <div v-if="oauthStatus.quota.recover_at" class="text-caption">
                  {{ t('oauth.recoversAt', { time: formatDate(oauthStatus.quota.recover_at) }) }}
                </div>
              </div>
            </div>
          </v-alert>

          <!-- Codex Quota (Primary/Secondary Windows) -->
          <div v-if="oauthStatus.quota?.codex_quota" class="mb-4">
            <div class="text-subtitle-2 mb-2 d-flex align-center">
              <v-icon size="small" class="mr-1">mdi-speedometer</v-icon>
              {{ t('oauth.usageQuota') }}
              <v-chip v-if="oauthStatus.quota.codex_quota.plan_type" size="x-small" color="primary" variant="tonal" class="ml-2">
                {{ oauthStatus.quota.codex_quota.plan_type }}
              </v-chip>
            </div>

            <!-- Primary Window (Short-term) -->
            <div class="mb-3">
              <div class="d-flex justify-space-between text-caption mb-1">
                <span>{{ t('oauth.primaryWindow') }} ({{ formatWindowDuration(oauthStatus.quota.codex_quota.primary_window_minutes) }})</span>
                <span>{{ t('oauth.availablePercent', { percent: 100 - oauthStatus.quota.codex_quota.primary_used_percent }) }}</span>
              </div>
              <v-progress-linear
                :model-value="100 - oauthStatus.quota.codex_quota.primary_used_percent"
                :color="getRemainingColor(oauthStatus.quota.codex_quota.primary_used_percent)"
                height="8"
                rounded
              />
              <div v-if="oauthStatus.quota.codex_quota.primary_reset_at" class="text-caption text-medium-emphasis mt-1">
                {{ t('oauth.resetsAt', { time: formatDate(oauthStatus.quota.codex_quota.primary_reset_at) }) }}
              </div>
            </div>

            <!-- Secondary Window (Long-term) -->
            <div class="mb-3">
              <div class="d-flex justify-space-between text-caption mb-1">
                <span>{{ t('oauth.secondaryWindow') }} ({{ formatWindowDuration(oauthStatus.quota.codex_quota.secondary_window_minutes) }})</span>
                <span>{{ t('oauth.availablePercent', { percent: 100 - oauthStatus.quota.codex_quota.secondary_used_percent }) }}</span>
              </div>
              <v-progress-linear
                :model-value="100 - oauthStatus.quota.codex_quota.secondary_used_percent"
                :color="getRemainingColor(oauthStatus.quota.codex_quota.secondary_used_percent)"
                height="8"
                rounded
              />
              <div v-if="oauthStatus.quota.codex_quota.secondary_reset_at" class="text-caption text-medium-emphasis mt-1">
                {{ t('oauth.resetsAt', { time: formatDate(oauthStatus.quota.codex_quota.secondary_reset_at) }) }}
              </div>
            </div>

            <!-- Credits info -->
            <div v-if="oauthStatus.quota.codex_quota.credits_has_credits || oauthStatus.quota.codex_quota.credits_unlimited" class="text-caption text-medium-emphasis">
              <v-icon size="x-small" class="mr-1">mdi-credit-card</v-icon>
              <span v-if="oauthStatus.quota.codex_quota.credits_unlimited">{{ t('oauth.creditsUnlimited') }}</span>
              <span v-else-if="oauthStatus.quota.codex_quota.credits_balance">{{ t('oauth.creditsBalance', { balance: oauthStatus.quota.codex_quota.credits_balance }) }}</span>
              <span v-else>{{ t('oauth.creditsAvailable') }}</span>
            </div>

            <!-- Last Updated -->
            <div v-if="oauthStatus.quota.codex_quota.updated_at" class="text-caption text-medium-emphasis mt-2">
              {{ t('oauth.lastUpdated', { time: formatDate(oauthStatus.quota.codex_quota.updated_at) }) }}
            </div>
          </div>

          <!-- Standard Rate Limit Progress Bars (fallback for non-Codex) -->
          <div v-else-if="oauthStatus.quota?.rate_limit" class="mb-4">
            <div class="text-subtitle-2 mb-2 d-flex align-center">
              <v-icon size="small" class="mr-1">mdi-speedometer</v-icon>
              {{ t('oauth.rateLimits') }}
            </div>

            <!-- Request Limit -->
            <div v-if="oauthStatus.quota.rate_limit.limit_requests" class="mb-3">
              <div class="d-flex justify-space-between text-caption mb-1">
                <span>{{ t('oauth.requestLimit') }}</span>
                <span>
                  {{ oauthStatus.quota.rate_limit.remaining_requests ?? 0 }} / {{ oauthStatus.quota.rate_limit.limit_requests }}
                </span>
              </div>
              <v-progress-linear
                :model-value="getRequestUsagePercent"
                :color="getUsageColor(getRequestUsagePercent)"
                height="8"
                rounded
              />
              <div v-if="oauthStatus.quota.rate_limit.reset_requests" class="text-caption text-medium-emphasis mt-1">
                {{ t('oauth.resetsAt', { time: formatDate(oauthStatus.quota.rate_limit.reset_requests) }) }}
              </div>
            </div>

            <!-- Token Limit -->
            <div v-if="oauthStatus.quota.rate_limit.limit_tokens" class="mb-3">
              <div class="d-flex justify-space-between text-caption mb-1">
                <span>{{ t('oauth.tokenLimit') }}</span>
                <span>
                  {{ formatNumber(oauthStatus.quota.rate_limit.remaining_tokens ?? 0) }} / {{ formatNumber(oauthStatus.quota.rate_limit.limit_tokens) }}
                </span>
              </div>
              <v-progress-linear
                :model-value="getTokenUsagePercent"
                :color="getUsageColor(getTokenUsagePercent)"
                height="8"
                rounded
              />
              <div v-if="oauthStatus.quota.rate_limit.reset_tokens" class="text-caption text-medium-emphasis mt-1">
                {{ t('oauth.resetsAt', { time: formatDate(oauthStatus.quota.rate_limit.reset_tokens) }) }}
              </div>
            </div>

            <!-- Last Updated -->
            <div v-if="oauthStatus.quota.rate_limit.updated_at" class="text-caption text-medium-emphasis">
              {{ t('oauth.lastUpdated', { time: formatDate(oauthStatus.quota.rate_limit.updated_at) }) }}
            </div>
          </div>

          <!-- No quota data message -->
          <v-alert
            v-else
            type="info"
            variant="tonal"
            class="mb-4"
            density="compact"
          >
            <div class="d-flex align-center">
              <v-icon class="mr-2">mdi-information</v-icon>
              {{ t('oauth.noQuotaData') }}
            </div>
          </v-alert>

          <v-divider class="my-3" />

          <!-- Status details -->
          <div class="text-subtitle-2 mb-2 d-flex align-center">
            <v-icon size="small" class="mr-1">mdi-account-details</v-icon>
            {{ t('oauth.accountDetails') }}
          </div>

          <v-list density="compact" class="oauth-details-list">
            <!-- Email -->
            <v-list-item v-if="oauthStatus.status.email">
              <template #prepend>
                <v-icon size="small" color="primary">mdi-email</v-icon>
              </template>
              <v-list-item-title class="text-caption text-medium-emphasis">{{ t('oauth.email') }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium">{{ oauthStatus.status.email }}</v-list-item-subtitle>
            </v-list-item>

            <!-- Plan Type -->
            <v-list-item v-if="oauthStatus.status.plan_type">
              <template #prepend>
                <v-icon size="small" color="primary">mdi-card-account-details</v-icon>
              </template>
              <v-list-item-title class="text-caption text-medium-emphasis">{{ t('oauth.planType') }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium">
                <v-chip size="x-small" color="success" variant="tonal">
                  {{ oauthStatus.status.plan_type }}
                </v-chip>
              </v-list-item-subtitle>
            </v-list-item>

            <!-- Account ID (masked) -->
            <v-list-item v-if="oauthStatus.status.masked_account_id">
              <template #prepend>
                <v-icon size="small" color="primary">mdi-account</v-icon>
              </template>
              <v-list-item-title class="text-caption text-medium-emphasis">{{ t('oauth.accountId') }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium text-mono">{{ oauthStatus.status.masked_account_id }}</v-list-item-subtitle>
            </v-list-item>

            <!-- Subscription Active Until -->
            <v-list-item v-if="oauthStatus.status.subscription_active_until">
              <template #prepend>
                <v-icon size="small" color="primary">mdi-calendar-check</v-icon>
              </template>
              <v-list-item-title class="text-caption text-medium-emphasis">{{ t('oauth.subscriptionActiveUntil') }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium">{{ formatDate(oauthStatus.status.subscription_active_until) }}</v-list-item-subtitle>
            </v-list-item>

            <!-- Token Expiry -->
            <v-list-item v-if="oauthStatus.status.token_expires_at">
              <template #prepend>
                <v-icon size="small" color="primary">mdi-clock-outline</v-icon>
              </template>
              <v-list-item-title class="text-caption text-medium-emphasis">{{ t('oauth.tokenExpiry') }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium">{{ formatDate(oauthStatus.status.token_expires_at) }}</v-list-item-subtitle>
            </v-list-item>

            <!-- Last Refresh -->
            <v-list-item v-if="oauthStatus.status.last_refresh">
              <template #prepend>
                <v-icon size="small" color="primary">mdi-refresh</v-icon>
              </template>
              <v-list-item-title class="text-caption text-medium-emphasis">{{ t('oauth.lastRefresh') }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium">{{ formatDate(oauthStatus.status.last_refresh) }}</v-list-item-subtitle>
            </v-list-item>
          </v-list>
        </div>
      </v-card-text>

      <v-divider />

      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="close">{{ t('common.close') }}</v-btn>
        <v-btn color="primary" variant="tonal" @click="refresh" :loading="loading">
          <v-icon start>mdi-refresh</v-icon>
          {{ t('common.refresh') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import api, { type OAuthStatusResponse } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
  channelId: number | null
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const dialogVisible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const loading = ref(false)
const error = ref<string | null>(null)
const oauthStatus = ref<OAuthStatusResponse | null>(null)

const tokenStatusColor = computed(() => {
  switch (oauthStatus.value?.tokenStatus) {
    case 'valid': return 'success'
    case 'expiring_soon': return 'warning'
    case 'expired': return 'error'
    default: return 'grey'
  }
})

const tokenStatusIcon = computed(() => {
  switch (oauthStatus.value?.tokenStatus) {
    case 'valid': return 'mdi-check-circle'
    case 'expiring_soon': return 'mdi-alert'
    case 'expired': return 'mdi-close-circle'
    default: return 'mdi-help-circle'
  }
})

// Calculate request usage percentage (used / limit)
const getRequestUsagePercent = computed(() => {
  const rl = oauthStatus.value?.quota?.rate_limit
  if (!rl?.limit_requests) return 0
  const used = rl.limit_requests - (rl.remaining_requests ?? 0)
  return Math.min(100, Math.max(0, (used / rl.limit_requests) * 100))
})

// Calculate token usage percentage (used / limit)
const getTokenUsagePercent = computed(() => {
  const rl = oauthStatus.value?.quota?.rate_limit
  if (!rl?.limit_tokens) return 0
  const used = rl.limit_tokens - (rl.remaining_tokens ?? 0)
  return Math.min(100, Math.max(0, (used / rl.limit_tokens) * 100))
})

// Get color based on usage percentage (for "used" display)
const getUsageColor = (percent: number): string => {
  if (percent >= 90) return 'error'
  if (percent >= 70) return 'warning'
  return 'success'
}

// Get color based on used percentage (for "remaining" display - inverted logic)
const getRemainingColor = (usedPercent: number): string => {
  if (usedPercent >= 90) return 'error'
  if (usedPercent >= 70) return 'warning'
  return 'success'
}

const formatDate = (dateStr: string | undefined) => {
  if (!dateStr) return '-'
  try {
    const date = new Date(dateStr)
    return date.toLocaleString()
  } catch {
    return dateStr
  }
}

const formatNumber = (num: number): string => {
  if (num >= 1000000) {
    return (num / 1000000).toFixed(1) + 'M'
  }
  if (num >= 1000) {
    return (num / 1000).toFixed(1) + 'K'
  }
  return num.toString()
}

// Format window duration from minutes to human-readable string
const formatWindowDuration = (minutes: number | undefined): string => {
  if (!minutes) return ''
  if (minutes >= 1440) {
    const days = Math.round(minutes / 1440)
    return `${days}d`
  }
  if (minutes >= 60) {
    const hours = Math.round(minutes / 60)
    return `${hours}h`
  }
  return `${minutes}m`
}

const loadStatus = async () => {
  if (props.channelId === null) return

  loading.value = true
  error.value = null

  try {
    oauthStatus.value = await api.getResponsesChannelOAuthStatus(props.channelId)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('oauth.loadError')
  } finally {
    loading.value = false
  }
}

const refresh = () => {
  loadStatus()
}

const close = () => {
  dialogVisible.value = false
}

watch(() => props.modelValue, (newVal) => {
  if (newVal && props.channelId !== null) {
    loadStatus()
  }
})

watch(() => props.channelId, () => {
  if (props.modelValue && props.channelId !== null) {
    loadStatus()
  }
})
</script>

<style scoped>
.oauth-details-list {
  background: transparent;
}

.oauth-details-list :deep(.v-list-item) {
  padding-left: 0;
  padding-right: 0;
  min-height: 48px;
}

.text-mono {
  font-family: monospace;
}
</style>
