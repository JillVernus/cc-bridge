<template>
  <v-dialog v-model="dialogVisible" max-width="600" persistent>
    <v-card class="modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4">
        <v-icon class="mr-2" color="primary">mdi-shield-account</v-icon>
        {{ t('oauth.status') }}
        <v-spacer />
        <v-btn icon variant="text" size="small" @click="close" class="modal-action-btn">
          <v-icon>mdi-close</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="flat"
          size="small"
          color="primary"
          @click="refresh"
          :loading="loading"
          class="modal-action-btn"
        >
          <v-icon>mdi-refresh</v-icon>
        </v-btn>
      </v-card-title>

      <v-card-text class="modal-content pa-4">
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
            <v-chip :color="tokenStatusColor" variant="tonal" size="small" class="mr-2">
              <v-icon start size="small">{{ tokenStatusIcon }}</v-icon>
              {{ t(`oauth.tokenStatus.${oauthStatus.tokenStatus || 'valid'}`) }}
            </v-chip>
            <span v-if="oauthStatus.tokenExpiresIn" class="text-caption text-medium-emphasis">
              {{ t('oauth.expiresIn', { seconds: oauthStatus.tokenExpiresIn }) }}
            </span>
          </div>

          <!-- Quota exceeded warning -->
          <v-alert v-if="oauthStatus.quota?.is_exceeded" type="error" variant="tonal" class="mb-4" density="compact">
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
              <v-chip
                v-if="oauthStatus.quota.codex_quota.plan_type"
                size="x-small"
                color="primary"
                variant="tonal"
                class="ml-2"
              >
                {{ oauthStatus.quota.codex_quota.plan_type }}
              </v-chip>
              <v-chip
                v-if="oauthStatus.quota.codex_quota.active_limit"
                size="x-small"
                color="secondary"
                variant="tonal"
                class="ml-2"
              >
                {{ t('oauth.activeLimit', { limit: oauthStatus.quota.codex_quota.active_limit }) }}
              </v-chip>
            </div>

            <!-- Primary Window (Short-term) -->
            <div class="mb-3">
              <div class="d-flex justify-space-between text-caption mb-1">
                <span
                  >{{ t('oauth.primaryWindow') }} ({{
                    formatWindowDuration(oauthStatus.quota.codex_quota.primary_window_minutes)
                  }})</span
                >
                <span>{{ t('oauth.availablePercent', { percent: formatPercentValue(primaryRemainingPercent) }) }}</span>
              </div>
              <v-progress-linear
                :model-value="primaryRemainingPercent"
                :color="getRemainingColor(primaryUsedPercent)"
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
                <span
                  >{{ t('oauth.secondaryWindow') }} ({{
                    formatWindowDuration(oauthStatus.quota.codex_quota.secondary_window_minutes)
                  }})</span
                >
                <span>{{
                  t('oauth.availablePercent', { percent: formatPercentValue(secondaryRemainingPercent) })
                }}</span>
              </div>
              <v-progress-linear
                :model-value="secondaryRemainingPercent"
                :color="getRemainingColor(secondaryUsedPercent)"
                height="8"
                rounded
              />
              <div
                v-if="oauthStatus.quota.codex_quota.secondary_reset_at"
                class="text-caption text-medium-emphasis mt-1"
              >
                {{ t('oauth.resetsAt', { time: formatDate(oauthStatus.quota.codex_quota.secondary_reset_at) }) }}
              </div>
            </div>

            <!-- Named metered limits -->
            <div v-if="detailedQuotaLimits.length > 0" class="mb-3">
              <div class="text-caption font-weight-medium mb-2">{{ t('oauth.detailedLimits') }}</div>
              <div v-for="limit in detailedQuotaLimits" :key="limit.limit_id" class="detailed-limit mb-3">
                <div class="d-flex align-center justify-space-between text-caption mb-1">
                  <span class="font-weight-medium">{{ getLimitDisplayName(limit) }}</span>
                </div>
                <div class="mb-2">
                  <div class="d-flex justify-space-between text-caption mb-1">
                    <span
                      >{{ t('oauth.primaryWindow') }} ({{ formatWindowDuration(limit.primary_window_minutes) }})</span
                    >
                    <span>{{
                      t('oauth.availablePercent', {
                        percent: formatPercentValue(getLimitRemainingPercent(limit, 'primary'))
                      })
                    }}</span>
                  </div>
                  <v-progress-linear
                    :model-value="getLimitRemainingPercent(limit, 'primary')"
                    :color="getRemainingColor(getLimitUsedPercent(limit, 'primary'))"
                    height="6"
                    rounded
                  />
                  <div v-if="limit.primary_reset_at" class="text-caption text-medium-emphasis mt-1">
                    {{ t('oauth.resetsAt', { time: formatDate(limit.primary_reset_at) }) }}
                  </div>
                </div>
                <div>
                  <div class="d-flex justify-space-between text-caption mb-1">
                    <span
                      >{{ t('oauth.secondaryWindow') }} ({{
                        formatWindowDuration(limit.secondary_window_minutes)
                      }})</span
                    >
                    <span>{{
                      t('oauth.availablePercent', {
                        percent: formatPercentValue(getLimitRemainingPercent(limit, 'secondary'))
                      })
                    }}</span>
                  </div>
                  <v-progress-linear
                    :model-value="getLimitRemainingPercent(limit, 'secondary')"
                    :color="getRemainingColor(getLimitUsedPercent(limit, 'secondary'))"
                    height="6"
                    rounded
                  />
                  <div v-if="limit.secondary_reset_at" class="text-caption text-medium-emphasis mt-1">
                    {{ t('oauth.resetsAt', { time: formatDate(limit.secondary_reset_at) }) }}
                  </div>
                </div>
              </div>
            </div>

            <!-- User-driven reset credits -->
            <v-alert v-if="resetCredits" type="info" variant="tonal" class="mb-3" density="compact">
              <div class="d-flex align-center justify-space-between reset-credit-row">
                <div>
                  <div class="text-caption font-weight-medium">{{ t('oauth.resetCredits') }}</div>
                  <div class="text-caption">
                    {{ resetCreditsRemainingText }}
                  </div>
                  <div v-if="resetCreditItems.length === 0 && resetCreditCreatedAt" class="text-caption text-medium-emphasis">
                    {{ t('oauth.resetCreditCreatedAt', { time: formatDate(resetCreditCreatedAt) }) }}
                  </div>
                  <div v-if="resetCreditItems.length === 0 && resetCreditExpiresAt" class="text-caption text-medium-emphasis">
                    {{ t('oauth.resetCreditExpiresAt', { time: formatDate(resetCreditExpiresAt) }) }}
                  </div>
                  <div v-if="resetCreditItems.length > 0" class="reset-credit-list mt-2">
                    <div
                      v-for="(credit, creditIndex) in resetCreditItems"
                      :key="credit.id || `${creditIndex}-${credit.expires_at || credit.granted_at || ''}`"
                      class="reset-credit-item"
                    >
                      <div class="text-caption font-weight-medium">
                        {{ credit.title || t('oauth.resetCreditItem', { index: creditIndex + 1 }) }}
                      </div>
                      <div v-if="credit.expires_at" class="text-caption text-medium-emphasis">
                        {{ t('oauth.resetCreditExpiresAt', { time: formatDate(credit.expires_at) }) }}
                      </div>
                      <div v-if="credit.granted_at || credit.created_at" class="text-caption text-medium-emphasis">
                        {{ t('oauth.resetCreditCreatedAt', { time: formatDate(credit.granted_at || credit.created_at) }) }}
                      </div>
                    </div>
                  </div>
                </div>
                <v-btn
                  size="small"
                  color="primary"
                  variant="flat"
                  prepend-icon="mdi-backup-restore"
                  :loading="resettingResetCredit"
                  :disabled="!canConsumeResetCredit || resettingResetCredit"
                  @click="openResetCreditConfirm"
                >
                  {{ t('oauth.resetNow') }}
                </v-btn>
              </div>
            </v-alert>

            <!-- Credits info -->
            <div
              v-if="
                oauthStatus.quota.codex_quota.credits_has_credits || oauthStatus.quota.codex_quota.credits_unlimited
              "
              class="text-caption text-medium-emphasis"
            >
              <v-icon size="x-small" class="mr-1">mdi-credit-card</v-icon>
              <span v-if="oauthStatus.quota.codex_quota.credits_unlimited">{{ t('oauth.creditsUnlimited') }}</span>
              <span v-else-if="oauthStatus.quota.codex_quota.credits_balance">{{
                t('oauth.creditsBalance', { balance: oauthStatus.quota.codex_quota.credits_balance })
              }}</span>
              <span v-else>{{ t('oauth.creditsAvailable') }}</span>
            </div>

            <!-- Last Updated -->
            <div v-if="oauthStatus.quota.codex_quota.updated_at" class="text-caption text-medium-emphasis mt-2">
              {{ t('oauth.lastUpdated', { time: formatDate(oauthStatus.quota.codex_quota.updated_at) }) }}
            </div>
          </div>

          <!-- Standard Rate Limit Progress Bars -->
          <div v-if="oauthStatus.quota?.rate_limit" class="mb-4">
            <div class="text-subtitle-2 mb-2 d-flex align-center">
              <v-icon size="small" class="mr-1">mdi-speedometer</v-icon>
              {{ t('oauth.rateLimits') }}
            </div>

            <!-- Request Limit -->
            <div v-if="oauthStatus.quota.rate_limit.limit_requests" class="mb-3">
              <div class="d-flex justify-space-between text-caption mb-1">
                <span>{{ t('oauth.requestLimit') }}</span>
                <span>
                  {{ oauthStatus.quota.rate_limit.remaining_requests ?? 0 }} /
                  {{ oauthStatus.quota.rate_limit.limit_requests }}
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
                  {{ formatNumber(oauthStatus.quota.rate_limit.remaining_tokens ?? 0) }} /
                  {{ formatNumber(oauthStatus.quota.rate_limit.limit_tokens) }}
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
            v-if="!oauthStatus.quota?.codex_quota && !oauthStatus.quota?.rate_limit"
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
              <v-list-item-title class="text-caption text-medium-emphasis">{{
                t('oauth.accountId')
              }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium text-mono">{{
                oauthStatus.status.masked_account_id
              }}</v-list-item-subtitle>
            </v-list-item>

            <!-- Subscription Active Until -->
            <v-list-item v-if="oauthStatus.status.subscription_active_until">
              <template #prepend>
                <v-icon size="small" color="primary">mdi-calendar-check</v-icon>
              </template>
              <v-list-item-title class="text-caption text-medium-emphasis">{{
                t('oauth.subscriptionActiveUntil')
              }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium">{{
                formatDate(oauthStatus.status.subscription_active_until)
              }}</v-list-item-subtitle>
            </v-list-item>

            <!-- Token Expiry -->
            <v-list-item v-if="oauthStatus.status.token_expires_at">
              <template #prepend>
                <v-icon size="small" color="primary">mdi-clock-outline</v-icon>
              </template>
              <v-list-item-title class="text-caption text-medium-emphasis">{{
                t('oauth.tokenExpiry')
              }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium">{{
                formatDate(oauthStatus.status.token_expires_at)
              }}</v-list-item-subtitle>
            </v-list-item>

            <!-- Last Refresh -->
            <v-list-item v-if="oauthStatus.status.last_refresh">
              <template #prepend>
                <v-icon size="small" color="primary">mdi-refresh</v-icon>
              </template>
              <v-list-item-title class="text-caption text-medium-emphasis">{{
                t('oauth.lastRefresh')
              }}</v-list-item-title>
              <v-list-item-subtitle class="font-weight-medium">{{
                formatDate(oauthStatus.status.last_refresh)
              }}</v-list-item-subtitle>
            </v-list-item>
          </v-list>
        </div>
      </v-card-text>
    </v-card>

    <v-dialog v-model="showResetCreditConfirmDialog" max-width="420">
      <v-card class="modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4">
          {{ t('oauth.confirmResetCredit') }}
          <v-spacer />
          <v-btn icon variant="text" size="small" class="modal-action-btn" @click="showResetCreditConfirmDialog = false">
            <v-icon>mdi-close</v-icon>
          </v-btn>
          <v-btn
            icon
            variant="flat"
            size="small"
            color="warning"
            class="modal-action-btn"
            :loading="resettingResetCredit"
            :disabled="!canConsumeResetCredit"
            @click="consumeResetCredit"
          >
            <v-icon>mdi-check</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">
          <v-alert type="warning" variant="tonal" density="compact" class="mb-3">
            {{ t('oauth.resetCreditConfirmWarning') }}
          </v-alert>
          <p class="mb-0">
            {{ t('oauth.resetCreditConfirmDesc', { count: resetCreditCount }) }}
          </p>
        </v-card-text>
      </v-card>
    </v-dialog>
  </v-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import api, { type OAuthStatusResponse, type CodexQuotaInfo, type CodexQuotaLimitInfo } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
  channelId: string | null
  channelIndex: number | null
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const dialogVisible = computed({
  get: () => props.modelValue,
  set: value => emit('update:modelValue', value)
})

const loading = ref(false)
const resettingResetCredit = ref(false)
const showResetCreditConfirmDialog = ref(false)
const error = ref<string | null>(null)
const oauthStatus = ref<OAuthStatusResponse | null>(null)
const quotaClock = ref(Date.now())
let quotaResetTimer: ReturnType<typeof setTimeout> | null = null
let loadStatusRequestSequence = 0

const clearQuotaResetTimer = () => {
  if (quotaResetTimer) {
    clearTimeout(quotaResetTimer)
    quotaResetTimer = null
  }
}

const parseResetTimestamp = (resetAt?: string): number | null => {
  if (!resetAt) return null
  const timestamp = Date.parse(resetAt)
  if (!Number.isFinite(timestamp)) return null
  return timestamp
}

const clampPercent = (percent: number): number => {
  return Math.min(100, Math.max(0, percent))
}

const formatPercentValue = (percent: number): string => {
  const clamped = clampPercent(percent)
  if (Number.isInteger(clamped)) {
    return `${clamped}`
  }
  return clamped.toFixed(2)
}

const getEffectiveUsedPercent = (usedPercent: number, resetAt?: string): number => {
  const resetTimestamp = parseResetTimestamp(resetAt)
  if (resetTimestamp !== null && quotaClock.value >= resetTimestamp) {
    return 0
  }
  return clampPercent(usedPercent)
}

const codexQuota = computed(() => oauthStatus.value?.quota?.codex_quota)

const detailedQuotaLimits = computed(() => codexQuota.value?.detailed_limits ?? [])

const resetCredits = computed(() => codexQuota.value?.rate_limit_reset_credits)

const resetCreditItems = computed(() => resetCredits.value?.credits ?? [])

const resetCreditCreatedAt = computed(() => {
  const credits = resetCredits.value
  return (
    credits?.granted_at ||
    credits?.created_at ||
    credits?.credits?.find(credit => credit.granted_at || credit.created_at)?.granted_at ||
    credits?.credits?.find(credit => credit.granted_at || credit.created_at)?.created_at
  )
})

const resetCreditExpiresAt = computed(() => {
  const credits = resetCredits.value
  return credits?.expires_at || credits?.credits?.find(credit => credit.expires_at)?.expires_at
})

const canConsumeResetCredit = computed(() => (resetCredits.value?.available_count ?? 0) > 0)

const resetCreditCount = computed(() => resetCredits.value?.available_count ?? 0)

const resetCreditTotalCount = computed(() => resetCredits.value?.total_earned_count ?? resetCredits.value?.credits?.length ?? 0)

const resetCreditsRemainingText = computed(() => {
  if (resetCreditTotalCount.value > 0) {
    return t('oauth.resetCreditsRemainingTotal', {
      count: resetCreditCount.value,
      total: resetCreditTotalCount.value
    })
  }
  return t('oauth.resetCreditsRemaining', { count: resetCreditCount.value })
})

const primaryUsedPercent = computed(() => {
  const quota = codexQuota.value
  if (!quota) return 0
  return getEffectiveUsedPercent(quota.primary_used_percent, quota.primary_reset_at)
})

const primaryRemainingPercent = computed(() => {
  return 100 - primaryUsedPercent.value
})

const secondaryUsedPercent = computed(() => {
  const quota = codexQuota.value
  if (!quota) return 0
  return getEffectiveUsedPercent(quota.secondary_used_percent, quota.secondary_reset_at)
})

const secondaryRemainingPercent = computed(() => {
  return 100 - secondaryUsedPercent.value
})

const getLimitDisplayName = (limit: CodexQuotaLimitInfo): string => {
  return limit.limit_name || limit.limit_id
}

const getLimitUsedPercent = (limit: CodexQuotaLimitInfo, window: 'primary' | 'secondary'): number => {
  if (window === 'primary') {
    return getEffectiveUsedPercent(limit.primary_used_percent, limit.primary_reset_at)
  }
  return getEffectiveUsedPercent(limit.secondary_used_percent, limit.secondary_reset_at)
}

const getLimitRemainingPercent = (limit: CodexQuotaLimitInfo, window: 'primary' | 'secondary'): number => {
  return 100 - getLimitUsedPercent(limit, window)
}

const getNextQuotaResetTimestamp = (quota?: CodexQuotaInfo): number | null => {
  if (!quota) return null

  const now = Date.now()
  const candidates = [
    parseResetTimestamp(quota.primary_reset_at),
    parseResetTimestamp(quota.secondary_reset_at),
    ...(quota.detailed_limits ?? []).flatMap(limit => [
      parseResetTimestamp(limit.primary_reset_at),
      parseResetTimestamp(limit.secondary_reset_at)
    ])
  ].filter((timestamp): timestamp is number => timestamp !== null && timestamp > now)

  if (candidates.length === 0) return null
  return Math.min(...candidates)
}

const scheduleQuotaAutoReset = () => {
  clearQuotaResetTimer()

  if (!props.modelValue) return
  const nextReset = getNextQuotaResetTimestamp(codexQuota.value)
  if (nextReset === null) return

  const delay = Math.max(0, nextReset - Date.now()) + 50
  quotaResetTimer = setTimeout(() => {
    quotaClock.value = Date.now()
    scheduleQuotaAutoReset()
  }, delay)
}

const tokenStatusColor = computed(() => {
  switch (oauthStatus.value?.tokenStatus) {
    case 'valid':
      return 'success'
    case 'expiring_soon':
      return 'warning'
    case 'expired':
      return 'error'
    default:
      return 'grey'
  }
})

const tokenStatusIcon = computed(() => {
  switch (oauthStatus.value?.tokenStatus) {
    case 'valid':
      return 'mdi-check-circle'
    case 'expiring_soon':
      return 'mdi-alert'
    case 'expired':
      return 'mdi-close-circle'
    default:
      return 'mdi-help-circle'
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
  if (props.channelId === null && props.channelIndex === null) return

  const requestSequence = ++loadStatusRequestSequence

  loading.value = true
  error.value = null

  try {
    const status = props.channelId
      ? await api.getResponsesChannelOAuthStatusByStableId(props.channelId)
      : await api.getResponsesChannelOAuthStatus(props.channelIndex as number)

    if (requestSequence !== loadStatusRequestSequence) {
      return
    }

    oauthStatus.value = status
    quotaClock.value = Date.now()
    scheduleQuotaAutoReset()
  } catch (err) {
    if (requestSequence !== loadStatusRequestSequence) {
      return
    }
    error.value = err instanceof Error ? err.message : t('oauth.loadError')
    clearQuotaResetTimer()
  } finally {
    if (requestSequence === loadStatusRequestSequence) {
      loading.value = false
    }
  }
}

const refresh = () => {
  loadStatus()
}

const openResetCreditConfirm = () => {
  if (!canConsumeResetCredit.value || resettingResetCredit.value) return
  showResetCreditConfirmDialog.value = true
}

const consumeResetCredit = async () => {
  if ((props.channelId === null && props.channelIndex === null) || !canConsumeResetCredit.value) return

  resettingResetCredit.value = true
  error.value = null
  try {
    const status = props.channelId
      ? await api.resetResponsesChannelOAuthQuotaCreditByStableId(props.channelId)
      : await api.resetResponsesChannelOAuthQuotaCredit(props.channelIndex as number)

    oauthStatus.value = status
    quotaClock.value = Date.now()
    scheduleQuotaAutoReset()
    showResetCreditConfirmDialog.value = false
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('oauth.resetCreditFailed')
  } finally {
    resettingResetCredit.value = false
  }
}

const close = () => {
  dialogVisible.value = false
}

watch(
  () => props.modelValue,
  newVal => {
    if (newVal && (props.channelId !== null || props.channelIndex !== null)) {
      loadStatus()
      return
    }
    clearQuotaResetTimer()
  }
)

watch(
  () => [props.channelId, props.channelIndex],
  () => {
    if (props.modelValue && (props.channelId !== null || props.channelIndex !== null)) {
      loadStatus()
    }
  }
)

onUnmounted(() => {
  clearQuotaResetTimer()
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

.reset-credit-row {
  gap: 12px;
  flex-wrap: wrap;
}

.reset-credit-list {
  display: grid;
  gap: 6px;
}

.reset-credit-item {
  border-left: 2px solid rgba(var(--v-theme-info), 0.4);
  padding-left: 8px;
}
</style>
