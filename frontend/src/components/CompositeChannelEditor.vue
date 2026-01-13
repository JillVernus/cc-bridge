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
        <div
          v-for="pattern in requiredPatterns"
          :key="pattern"
          class="model-column"
        >
          <!-- Model Header -->
          <div class="column-header">
            <span class="model-name">{{ pattern }}</span>
            <v-icon
              v-if="isModelValid(pattern)"
              size="x-small"
              color="success"
            >mdi-check-circle</v-icon>
            <v-tooltip v-else location="top">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="x-small" color="error">mdi-alert-circle</v-icon>
              </template>
              {{ getModelError(pattern) }}
            </v-tooltip>
          </div>

          <!-- Vertical Channel List -->
          <div class="channel-list">
            <template v-for="(channelId, index) in modelChains[pattern]" :key="`${pattern}-${index}-${channelId}`">
              <!-- Channel Select -->
              <div class="channel-item">
                <v-select
                  :modelValue="channelId"
                  @update:modelValue="(val) => updateChannelAt(pattern, index, val)"
                  :items="getAvailableChannelsFor(pattern, index)"
                  item-title="name"
                  item-value="id"
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
                        <v-chip size="x-small" variant="outlined">{{ item.raw.serviceType }}</v-chip>
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
            </template>

            <!-- Add Button -->
            <v-btn
              icon
              size="x-small"
              variant="tonal"
              color="primary"
              class="add-btn"
              @click="addChannelSlot(pattern)"
              :disabled="modelChains[pattern].length >= availableClaudeChannels.length"
            >
              <v-icon size="x-small">mdi-plus</v-icon>
            </v-btn>
          </div>
        </div>
      </div>

      <!-- Validation Errors -->
      <v-alert
        v-if="validationError"
        type="error"
        variant="tonal"
        class="mt-3"
        rounded="lg"
        density="compact"
      >
        {{ validationError }}
      </v-alert>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed, watch, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Channel, CompositeMapping } from '../services/api'

const { t } = useI18n()

// Props
interface Props {
  modelValue: CompositeMapping[]
  allChannels: Channel[]
}

const props = defineProps<Props>()

// Emits
const emit = defineEmits<{
  'update:modelValue': [value: CompositeMapping[]]
}>()

// Required patterns (no wildcard)
const requiredPatterns = ['haiku', 'sonnet', 'opus'] as const

// Claude-compatible service types
const claudeCompatibleTypes = ['claude', 'openai_chat', 'openai', 'gemini', 'openaiold']

// Local state: per-model channel chains (array of channel IDs)
const modelChains = reactive<Record<string, string[]>>({
  haiku: [''],
  sonnet: [''],
  opus: ['']
})

// Sync prop -> local state
watch(
  () => props.modelValue,
  (newMappings) => {
    for (const pattern of requiredPatterns) {
      const mapping = newMappings.find(m => m.pattern === pattern)
      if (mapping) {
        const chain: string[] = []
        if (mapping.targetChannelId) {
          chain.push(mapping.targetChannelId)
        }
        if (mapping.failoverChain && mapping.failoverChain.length > 0) {
          chain.push(...mapping.failoverChain)
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
    const chain = modelChains[pattern].filter(id => id !== '')
    return {
      pattern,
      targetChannelId: chain[0] || '',
      failoverChain: chain.slice(1)
    }
  })
  emit('update:modelValue', mappings)
}

// No watcher needed - we emit directly from mutation functions

// Available Claude-compatible channels
const availableClaudeChannels = computed(() => {
  return props.allChannels
    .filter(ch => !!ch.id && claudeCompatibleTypes.includes(ch.serviceType))
    .map(ch => ({
      id: ch.id as string,
      name: ch.name,
      status: ch.status || 'active',
      serviceType: ch.serviceType,
      index: ch.index
    }))
    .sort((a, b) => a.name.localeCompare(b.name))
})

// Get available channels for a specific position (exclude already used in this pattern)
const getAvailableChannelsFor = (model: string, position: number) => {
  const chain = modelChains[model] || []
  const usedIds = new Set(chain.filter((id, idx) => id !== '' && idx !== position))
  return availableClaudeChannels.value.filter(ch => !usedIds.has(ch.id))
}

// Update channel at specific position
const updateChannelAt = (model: string, index: number, channelId: string | null) => {
  if (!modelChains[model]) return
  modelChains[model][index] = channelId || ''
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

// Validation
const isModelValid = (model: string): boolean => {
  const chain = modelChains[model]?.filter(id => id !== '') || []
  // Must have at least 2 channels (1 primary + 1 failover)
  return chain.length >= 2
}

const getModelError = (model: string): string => {
  const chain = modelChains[model]?.filter(id => id !== '') || []
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

    // Validate each channel exists and is Claude-compatible
    const chain = modelChains[pattern]?.filter(id => id !== '') || []
    for (const channelId of chain) {
      const channel = props.allChannels.find(ch => ch.id === channelId)
      if (!channel) {
        return t('addChannel.compositeMissingTargetChannel')
      }
      if (!claudeCompatibleTypes.includes(channel.serviceType)) {
        return t('addChannel.compositeInvalidServiceType', { channel: channel.name })
      }
    }
  }
  return ''
})

// Helper functions
const getChannelStatusColor = (channel: { status?: string }): string => {
  switch (channel.status) {
    case 'active': return 'success'
    case 'suspended': return 'warning'
    default: return 'grey'
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

.channel-item {
  display: flex;
  align-items: center;
  gap: 4px;
  width: 100%;
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
