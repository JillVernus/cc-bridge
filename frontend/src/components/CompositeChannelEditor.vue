<template>
  <v-card variant="outlined" rounded="lg">
    <v-card-title class="d-flex align-center justify-space-between pa-4 pb-2">
      <div class="d-flex align-center ga-2">
        <v-icon color="primary">mdi-source-branch</v-icon>
        <span class="text-body-1 font-weight-bold">{{ t('addChannel.compositeMappings') }}</span>
      </div>
      <v-chip size="small" color="info" variant="tonal">{{ t('addChannel.modelRouting') }}</v-chip>
    </v-card-title>

    <v-card-text class="pt-2">
      <div class="text-body-2 text-medium-emphasis mb-4">
        {{ t('addChannel.compositeFailoverHint') }}
      </div>

      <!-- 3-Column Model Routing -->
      <div class="model-columns">
        <div v-for="pattern in requiredPatterns" :key="pattern" class="model-column">
          <!-- Model Header -->
          <div class="column-header">
            <span class="model-name">{{ pattern }}</span>
            <v-icon v-if="isModelValid(pattern)" size="x-small" color="success">mdi-check-circle</v-icon>
            <v-tooltip v-else location="top">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="x-small" color="error">mdi-alert-circle</v-icon>
              </template>
              {{ getModelError(pattern) }}
            </v-tooltip>
          </div>

          <!-- Vertical Channel List -->
          <div class="channel-list">
            <draggable
              :modelValue="modelChains[pattern]"
              @update:modelValue="(val: string[]) => onDragEnd(pattern, val)"
              item-key="index"
              handle=".drag-handle"
              :animation="150"
              ghost-class="drag-ghost"
              class="draggable-container"
            >
              <template #item="{ element: channelId, index }">
                <div class="channel-row">
                  <!-- Channel Select -->
                  <div class="channel-item" :class="{ 'is-draggable': channelId !== '' }">
                    <!-- Drag Handle (only for populated slots) -->
                    <v-icon v-if="channelId !== ''" size="small" class="drag-handle" color="grey"
                      >mdi-drag-vertical</v-icon
                    >
                    <div v-else class="drag-handle-placeholder"></div>
                    <v-select
                      :modelValue="channelId"
                      @update:modelValue="val => updateChannelAt(pattern, index, val)"
                      :items="getAvailableTargetsFor(pattern, index)"
                      item-title="displayName"
                      item-value="key"
                      variant="outlined"
                      density="compact"
                      hide-details
                      :placeholder="index === 0 ? t('addChannel.primary') : t('addChannel.failoverN', { n: index })"
                      class="channel-select"
                      :class="{ 'primary-select': index === 0 }"
                    >
                      <template #item="{ item, props }">
                        <v-list-item v-bind="props">
                          <template #prepend>
                            <v-icon size="small" :color="getChannelStatusColor(item.raw)">mdi-server</v-icon>
                          </template>
                          <template #append>
                            <div class="d-flex align-center ga-1">
                              <v-chip size="x-small" variant="outlined" color="primary">{{
                                item.raw.poolLabel
                              }}</v-chip>
                              <v-chip size="x-small" variant="outlined">{{ item.raw.serviceType }}</v-chip>
                            </div>
                          </template>
                        </v-list-item>
                      </template>
                    </v-select>
                    <!-- Remove button -->
                    <v-btn
                      size="x-small"
                      icon
                      variant="text"
                      color="error"
                      class="remove-btn"
                      @click="removeChannelAt(pattern, index)"
                    >
                      <v-icon size="x-small">mdi-close</v-icon>
                    </v-btn>
                  </div>

                  <!-- Arrow (except for last) -->
                  <div v-if="index < modelChains[pattern].length - 1" class="arrow-down">
                    <v-icon size="small" color="grey">mdi-arrow-down</v-icon>
                  </div>
                </div>
              </template>
            </draggable>

            <!-- Add Button -->
            <v-btn
              icon
              size="x-small"
              variant="tonal"
              color="primary"
              class="add-btn"
              @click="addChannelSlot(pattern)"
              :disabled="modelChains[pattern].length >= availableCompositeTargets.length"
            >
              <v-icon size="x-small">mdi-plus</v-icon>
            </v-btn>
          </div>
        </div>
      </div>

      <!-- Validation Errors -->
      <v-alert v-if="validationError" type="error" variant="tonal" class="mt-3" rounded="lg" density="compact">
        {{ validationError }}
      </v-alert>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed, watch, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import draggable from 'vuedraggable'
import type { Channel, CompositeMapping, CompositeTargetPool, CompositeTargetRef } from '../services/api'

const { t } = useI18n()

// Props
interface Props {
  modelValue: CompositeMapping[]
  allChannels: Channel[] // messages pool channels
  responsesChannels?: Channel[] // responses pool channels
}

const props = withDefaults(defineProps<Props>(), {
  responsesChannels: () => []
})

// Emits
const emit = defineEmits<{
  'update:modelValue': [value: CompositeMapping[]]
}>()

// Required patterns (no wildcard)
const requiredPatterns = ['haiku', 'sonnet', 'opus'] as const

// messages pool service types that can receive /v1/messages traffic
const messagesCompatibleTypes = ['claude', 'openai_chat', 'openai', 'gemini', 'openaiold', 'responses']

// responses pool service types allowed for messages composite bridging
const responsesCompatibleTypes = ['responses', 'openai-oauth']

const normalizePool = (pool?: string): CompositeTargetPool => (pool === 'responses' ? 'responses' : 'messages')

const buildTargetKey = (pool: CompositeTargetPool, channelId: string): string => `${pool}|${channelId}`

const parseTargetKey = (key: string): CompositeTargetRef | null => {
  const [poolPart, channelIdPart] = String(key || '').split('|', 2)
  const channelId = String(channelIdPart || '').trim()
  if (!channelId) return null
  return {
    pool: normalizePool(poolPart),
    channelId
  }
}

const getFailoverTargets = (mapping: CompositeMapping): CompositeTargetRef[] => {
  const primaryPool = normalizePool(mapping.targetPool)
  if (Array.isArray(mapping.failoverTargets) && mapping.failoverTargets.length > 0) {
    return mapping.failoverTargets
      .map(target => ({
        pool: normalizePool(target.pool || primaryPool),
        channelId: String(target.channelId || '').trim()
      }))
      .filter(target => target.channelId !== '')
  }
  return (mapping.failoverChain || [])
    .map(channelId => ({
      pool: primaryPool,
      channelId: String(channelId || '').trim()
    }))
    .filter(target => target.channelId !== '')
}

type CompositeTargetOption = {
  key: string
  pool: CompositeTargetPool
  poolLabel: string
  channelId: string
  name: string
  displayName: string
  status: string
  serviceType: Channel['serviceType']
  index: number
}

// Local state: per-model channel chains (array of pool-aware keys)
const modelChains = reactive<Record<string, string[]>>({
  haiku: [''],
  sonnet: [''],
  opus: ['']
})

// Sync prop -> local state
watch(
  () => props.modelValue,
  newMappings => {
    for (const pattern of requiredPatterns) {
      const mapping = newMappings.find(m => m.pattern === pattern)
      if (mapping) {
        const chain: string[] = []
        const targetChannelId = String(mapping.targetChannelId || '').trim()
        const targetPool = normalizePool(mapping.targetPool)
        if (targetChannelId) {
          chain.push(buildTargetKey(targetPool, targetChannelId))
        }
        for (const target of getFailoverTargets(mapping)) {
          chain.push(buildTargetKey(normalizePool(target.pool || targetPool), target.channelId))
        }
        // Ensure at least one slot
        if (chain.length === 0) {
          chain.push('')
        }
        modelChains[pattern] = chain
      } else {
        modelChains[pattern] = ['']
      }
    }
  },
  { immediate: true, deep: true }
)

// Emit changes to parent
const emitChanges = () => {
  const mappings: CompositeMapping[] = requiredPatterns.map(pattern => {
    const chain = modelChains[pattern]
      .filter(key => key !== '')
      .map(parseTargetKey)
      .filter((target): target is CompositeTargetRef => !!target)
    const existing = props.modelValue.find(m => m.pattern === pattern)
    const primary = chain[0]
    const failovers = chain.slice(1)

    return {
      pattern,
      targetChannelId: primary?.channelId || '',
      targetPool: normalizePool(primary?.pool),
      failoverTargets: failovers.map(target => ({
        pool: normalizePool(target.pool),
        channelId: target.channelId
      })),
      failoverChain: failovers.map(target => target.channelId),
      targetModel: existing?.targetModel
    }
  })
  emit('update:modelValue', mappings)
}

// No watcher needed - we emit directly from mutation functions

const availableCompositeTargets = computed<CompositeTargetOption[]>(() => {
  const messagesTargets = props.allChannels
    .filter(ch => !!ch.id && messagesCompatibleTypes.includes(ch.serviceType))
    .map(ch => {
      const channelId = ch.id as string
      return {
        key: buildTargetKey('messages', channelId),
        pool: 'messages' as const,
        poolLabel: 'messages',
        channelId,
        name: ch.name,
        displayName: `${ch.name} · messages`,
        status: ch.status || 'active',
        serviceType: ch.serviceType,
        index: ch.index
      }
    })

  const responsesTargets = props.responsesChannels
    .filter(ch => !!ch.id && responsesCompatibleTypes.includes(ch.serviceType))
    .map(ch => {
      const channelId = ch.id as string
      return {
        key: buildTargetKey('responses', channelId),
        pool: 'responses' as const,
        poolLabel: 'responses',
        channelId,
        name: ch.name,
        displayName: `${ch.name} · responses`,
        status: ch.status || 'active',
        serviceType: ch.serviceType,
        index: ch.index
      }
    })

  return [...messagesTargets, ...responsesTargets].sort((a, b) => a.displayName.localeCompare(b.displayName))
})

const targetOptionsByKey = computed(() => {
  const map = new Map<string, CompositeTargetOption>()
  for (const option of availableCompositeTargets.value) {
    map.set(option.key, option)
  }
  return map
})

// Get available targets for a specific position (exclude already used in this pattern)
const getAvailableTargetsFor = (model: string, position: number) => {
  const chain = modelChains[model] || []
  const usedKeys = new Set(chain.filter((key, idx) => key !== '' && idx !== position))
  return availableCompositeTargets.value.filter(option => !usedKeys.has(option.key))
}

// Update channel at specific position
const updateChannelAt = (model: string, index: number, targetKey: string | null) => {
  if (!modelChains[model]) return
  modelChains[model][index] = targetKey || ''
  emitChanges()
}

// Remove channel at specific position
const removeChannelAt = (model: string, index: number) => {
  if (!modelChains[model]) return
  // Always keep at least one slot
  if (modelChains[model].length <= 1) {
    modelChains[model] = ['']
    emitChanges()
    return
  }
  modelChains[model].splice(index, 1)
  emitChanges()
}

// Add new channel slot (don't emit - empty slots are local-only until user selects a channel)
const addChannelSlot = (model: string) => {
  if (!modelChains[model]) return
  modelChains[model].push('')
  // Don't call emitChanges() here - empty slots shouldn't be persisted
}

// Handle drag reorder
const onDragEnd = (model: string, newOrder: string[]) => {
  if (!modelChains[model]) return
  modelChains[model] = newOrder
  emitChanges()
}

// Validation
const isModelValid = (model: string): boolean => {
  const chain = modelChains[model]?.filter(key => key !== '') || []
  // Must have at least 2 channels (1 primary + 1 failover)
  return chain.length >= 2
}

const getModelError = (model: string): string => {
  const chain = modelChains[model]?.filter(key => key !== '') || []
  if (chain.length === 0) {
    return t('addChannel.compositePatternRequired', { pattern: model })
  }
  if (chain.length < 2) {
    return t('addChannel.compositeMinFailover', { pattern: model })
  }
  return ''
}

const validationError = computed(() => {
  for (const pattern of requiredPatterns) {
    const error = getModelError(pattern)
    if (error) return error

    // Validate each target exists in allowed pool/type options
    const chain = modelChains[pattern]?.filter(key => key !== '') || []
    for (const targetKey of chain) {
      const target = targetOptionsByKey.value.get(targetKey)
      if (!target) {
        return t('addChannel.compositeMissingTargetChannel')
      }
    }
  }
  return ''
})

// Helper functions
const getChannelStatusColor = (channel: { status?: string }): string => {
  switch (channel.status) {
    case 'active':
      return 'success'
    case 'suspended':
      return 'warning'
    default:
      return 'grey'
  }
}
</script>

<style scoped>
.model-columns {
  display: flex;
  gap: 16px;
}

.model-column {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.column-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 12px;
}

.model-name {
  font-size: 0.875rem;
  font-weight: 600;
  color: rgb(var(--v-theme-on-surface));
}

.channel-list {
  display: flex;
  flex-direction: column;
  align-items: center;
  width: 100%;
}

.draggable-container {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.channel-row {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.channel-item {
  display: flex;
  align-items: center;
  gap: 4px;
  width: 100%;
}

.channel-item.is-draggable {
  cursor: default;
}

.drag-handle {
  cursor: grab;
  flex-shrink: 0;
  opacity: 0.5;
  transition: opacity 0.15s ease;
}

.drag-handle:hover {
  opacity: 1;
}

.drag-handle:active {
  cursor: grabbing;
}

.drag-handle-placeholder {
  width: 20px;
  flex-shrink: 0;
}

.drag-ghost {
  opacity: 0.5;
  background: rgba(var(--v-theme-primary), 0.1);
  border-radius: 4px;
}

.channel-select {
  flex: 1;
  min-width: 0;
}

.channel-select.primary-select :deep(.v-field) {
  border-color: rgb(var(--v-theme-primary));
}

.remove-btn {
  flex-shrink: 0;
  margin-left: -4px;
}

.arrow-down {
  display: flex;
  justify-content: center;
  padding: 2px 0;
}

.add-btn {
  margin-top: 4px;
}

/* Responsive: stack on small screens */
@media (max-width: 600px) {
  .model-columns {
    flex-direction: column;
    gap: 24px;
  }

  .model-column {
    padding-bottom: 16px;
    border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.1);
  }

  .model-column:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }
}
</style>
