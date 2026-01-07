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
        {{ t('addChannel.compositeMappingsHint') }}
      </div>

      <v-row>
        <!-- Left Panel: Available Claude Channels -->
        <v-col cols="12" md="5">
          <div class="text-caption font-weight-medium mb-2">{{ t('addChannel.availableChannels') }}</div>
          <v-card variant="tonal" rounded="lg" class="channel-panel pa-2">
            <template v-if="availableClaudeChannels.length">
              <div v-for="channel in availableClaudeChannels" :key="channel.id" class="mb-3">
                <div class="d-flex align-center ga-2 mb-1">
                  <v-icon size="small" :color="getChannelStatusColor(channel)">mdi-server</v-icon>
                  <span class="text-body-2 font-weight-medium">{{ channel.name }}</span>
                  <v-chip v-if="channel.status !== 'active'" size="x-small" :color="getChannelStatusColor(channel)" variant="tonal">
                    {{ channel.status }}
                  </v-chip>
                </div>
                <div class="d-flex flex-wrap ga-1 ml-5">
                  <v-chip
                    v-for="model in commonModels"
                    :key="model"
                    size="small"
                    variant="outlined"
                    class="model-chip"
                    @click="addMappingFromChip(model, channel.id)"
                  >
                    <v-icon start size="x-small">mdi-plus</v-icon>
                    {{ model }}
                  </v-chip>
                </div>
              </div>
            </template>
            <v-alert v-else type="warning" variant="tonal" density="compact" rounded="lg">
              {{ t('addChannel.noClaudeChannels') }}
            </v-alert>
          </v-card>

          <!-- Custom Pattern Input -->
          <div class="mt-4">
            <div class="text-caption font-weight-medium mb-2">{{ t('addChannel.customPattern') }}</div>
            <div class="d-flex align-center ga-2">
              <v-text-field
                v-model="customPattern"
                :placeholder="t('addChannel.modelPatternPlaceholder')"
                variant="outlined"
                density="compact"
                hide-details
                class="flex-grow-1"
              />
              <v-select
                v-model="customTargetChannelId"
                :items="availableClaudeChannels"
                item-title="name"
                item-value="id"
                variant="outlined"
                density="compact"
                hide-details
                :placeholder="t('addChannel.targetChannel')"
                style="min-width: 150px;"
              />
              <v-btn
                color="primary"
                size="small"
                variant="elevated"
                :disabled="!customPattern.trim() || !customTargetChannelId"
                @click="addCustomMapping"
              >
                <v-icon size="small">mdi-plus</v-icon>
              </v-btn>
            </div>
            <!-- Quick Pattern Chips -->
            <div class="mt-2">
              <span class="text-caption text-medium-emphasis mr-2">{{ t('addChannel.quickPatterns') }}:</span>
              <v-chip
                v-for="pattern in ['haiku', 'sonnet', 'opus', '*']"
                :key="pattern"
                size="x-small"
                variant="outlined"
                class="mr-1"
                @click="customPattern = pattern"
              >
                {{ pattern }}
              </v-chip>
            </div>
          </div>
        </v-col>

        <!-- Right Panel: Mapping Slots -->
        <v-col cols="12" md="7">
          <div class="d-flex align-center justify-space-between mb-2">
            <div class="text-caption font-weight-medium">{{ t('addChannel.mappingOrder') }}</div>
            <v-chip v-if="mappings.length" size="x-small" color="primary" variant="tonal">
              {{ mappings.length }} {{ t('addChannel.mappingsCount') }}
            </v-chip>
          </div>

          <v-card variant="tonal" rounded="lg" class="mapping-panel pa-2" :class="{ 'empty-panel': !mappings.length }">
            <!-- Empty State -->
            <div v-if="!mappings.length" class="empty-state d-flex flex-column align-center justify-center">
              <v-icon size="48" color="grey-lighten-1" class="mb-2">mdi-tray-arrow-down</v-icon>
              <div class="text-body-2 text-medium-emphasis">{{ t('addChannel.noCompositeMappings') }}</div>
              <div class="text-caption text-medium-emphasis">{{ t('addChannel.clickModelToAdd') }}</div>
            </div>

            <!-- Draggable Mapping List -->
            <draggable
              v-else
              v-model="mappings"
              item-key="pattern"
              handle=".drag-handle"
              animation="200"
              ghost-class="ghost-mapping"
            >
              <template #item="{ element, index }">
                <v-card
                  class="mapping-item mb-2"
                  variant="flat"
                  rounded="lg"
                  :color="getMappingColor(element)"
                >
                  <div class="d-flex align-center pa-2">
                    <!-- Drag Handle -->
                    <v-icon class="drag-handle mr-2" size="small" color="grey">mdi-drag-vertical</v-icon>

                    <!-- Priority Number -->
                    <v-chip size="x-small" color="primary" variant="flat" class="mr-2">{{ index + 1 }}</v-chip>

                    <!-- Pattern -->
                    <code class="text-caption mr-2">{{ element.pattern }}</code>

                    <!-- Arrow -->
                    <v-icon size="small" color="primary" class="mr-2">mdi-arrow-right</v-icon>

                    <!-- Target Channel -->
                    <span class="text-caption font-weight-medium flex-grow-1">{{ getTargetChannelName(element.targetChannelId) }}</span>

                    <!-- Target Model (if specified) -->
                    <span v-if="element.targetModel" class="text-caption text-medium-emphasis mr-2">
                      (→ {{ element.targetModel }})
                    </span>

                    <!-- Status Warning -->
                    <v-tooltip v-if="getTargetChannelStatus(element.targetChannelId) !== 'active'" location="top">
                      <template #activator="{ props }">
                        <v-icon v-bind="props" size="small" color="warning" class="mr-1">mdi-alert</v-icon>
                      </template>
                      {{ t('addChannel.targetChannelNotActive') }}
                    </v-tooltip>

                    <!-- Remove Button -->
                    <v-btn size="x-small" color="error" icon variant="text" @click="removeMapping(index)">
                      <v-icon size="small">mdi-close</v-icon>
                    </v-btn>
                  </div>
                </v-card>
              </template>
            </draggable>
          </v-card>

          <!-- Validation Warnings -->
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

          <v-alert
            v-else-if="!hasWildcard && mappings.length > 0"
            type="warning"
            variant="tonal"
            class="mt-3"
            rounded="lg"
            density="compact"
          >
            <div class="d-flex align-center ga-2">
              <span>{{ t('addChannel.noWildcardWarning') }}</span>
              <v-btn size="x-small" variant="outlined" @click="addWildcardFallback">
                {{ t('addChannel.addWildcard') }}
              </v-btn>
            </div>
          </v-alert>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import draggable from 'vuedraggable'
import type { Channel } from '../services/api'

const { t } = useI18n()

// Types
type Mapping = { pattern: string; targetChannelId: string; targetModel?: string }

// Props
interface Props {
  modelValue: Mapping[]
  allChannels: Channel[]
}

const props = defineProps<Props>()

// Emits
const emit = defineEmits<{
  'update:modelValue': [value: Mapping[]]
}>()

// Common Claude model patterns
const commonModels = ['haiku', 'sonnet', 'opus']

// Custom pattern input
const customPattern = ref('')
const customTargetChannelId = ref('')

// Local mappings state - vuedraggable mutates this directly
const mappings = ref<Mapping[]>([])

// Helper: Normalize mappings (ensure wildcard is always last)
const normalizeMappings = (list: Mapping[]): Mapping[] => {
  const wildcardIndex = list.findIndex(m => m.pattern === '*')
  if (wildcardIndex !== -1 && wildcardIndex !== list.length - 1) {
    const result = [...list]
    const [wildcard] = result.splice(wildcardIndex, 1)
    result.push(wildcard)
    return result
  }
  return list
}

// Helper: Check if two mapping arrays are equal
const areMappingsEqual = (a: Mapping[], b: Mapping[]): boolean => {
  if (a.length !== b.length) return false
  return a.every((m, i) =>
    m.pattern === b[i].pattern &&
    m.targetChannelId === b[i].targetChannelId &&
    m.targetModel === b[i].targetModel
  )
}

// Sync prop -> local state (when parent changes)
watch(
  () => props.modelValue,
  (newValue) => {
    const normalized = normalizeMappings(newValue)
    if (!areMappingsEqual(normalized, mappings.value)) {
      mappings.value = [...normalized]
    }
  },
  { immediate: true, deep: true }
)

// Sync local state -> emit (when draggable or local mutations occur)
watch(
  mappings,
  (newValue) => {
    const normalized = normalizeMappings(newValue)
    // If normalization changed the order, update local state
    if (!areMappingsEqual(normalized, newValue)) {
      mappings.value = normalized
      return // The next watch trigger will emit
    }
    // Only emit if different from prop
    if (!areMappingsEqual(normalized, props.modelValue)) {
      emit('update:modelValue', [...normalized])
    }
  },
  { deep: true }
)

// Available Claude channels (only claude type, not disabled)
const availableClaudeChannels = computed(() => {
  return props.allChannels
    .filter(ch => !!ch.id && ch.serviceType === 'claude' && ch.status !== 'disabled')
    .map(ch => ({
      id: ch.id as string,
      name: ch.name,
      status: ch.status || 'active',
      index: ch.index
    }))
})

// Check if wildcard exists
const hasWildcard = computed(() => {
  return mappings.value.some(m => m.pattern === '*')
})

// Validation error
const validationError = computed(() => {
  const seen = new Set<string>()
  let wildcardIndex = -1

  for (let i = 0; i < mappings.value.length; i++) {
    const mapping = mappings.value[i]
    const pattern = mapping.pattern.trim()

    if (!pattern) return t('addChannel.compositeEmptyPattern')
    if (seen.has(pattern)) return t('addChannel.compositeDuplicatePattern', { pattern })
    seen.add(pattern)

    if (pattern === '*') {
      if (wildcardIndex !== -1) return t('addChannel.compositeWildcardMultiple')
      wildcardIndex = i
    }

    if (!mapping.targetChannelId) return t('addChannel.compositeTargetRequired')

    const target = props.allChannels.find(ch => ch.id === mapping.targetChannelId)
    if (!target) return t('addChannel.compositeMissingTargetChannel')
    if (target.serviceType !== 'claude') return t('addChannel.compositeInvalidTargetChannel')
  }

  // Wildcard must be last
  if (wildcardIndex !== -1 && wildcardIndex !== mappings.value.length - 1) {
    return t('addChannel.compositeWildcardLast')
  }

  return ''
})

// Helper functions
const getTargetChannelName = (channelId: string): string => {
  const channel = props.allChannels.find(ch => ch.id === channelId)
  return channel?.name || channelId
}

const getTargetChannelStatus = (channelId: string): string => {
  const channel = props.allChannels.find(ch => ch.id === channelId)
  return channel?.status || 'disabled'
}

const getChannelStatusColor = (channel: { status: string }): string => {
  switch (channel.status) {
    case 'active': return 'success'
    case 'suspended': return 'warning'
    default: return 'grey'
  }
}

const getMappingColor = (mapping: { pattern: string; targetChannelId: string }): string => {
  const status = getTargetChannelStatus(mapping.targetChannelId)
  if (status !== 'active') return 'warning-lighten-4'
  if (mapping.pattern === '*') return 'grey-lighten-3'
  return 'surface-variant'
}

// Add mapping from model chip click
const addMappingFromChip = (model: string, channelId: string) => {
  // Check if pattern already exists
  if (mappings.value.some(m => m.pattern === model)) {
    return
  }

  const newMapping = { pattern: model, targetChannelId: channelId }

  // If wildcard exists, insert before it
  const wildcardIndex = mappings.value.findIndex(m => m.pattern === '*')
  if (wildcardIndex !== -1) {
    const updated = [...mappings.value]
    updated.splice(wildcardIndex, 0, newMapping)
    mappings.value = updated
  } else {
    mappings.value = [...mappings.value, newMapping]
  }
}

// Add custom mapping
const addCustomMapping = () => {
  const pattern = customPattern.value.trim()
  const targetChannelId = customTargetChannelId.value

  if (!pattern || !targetChannelId) return
  if (mappings.value.some(m => m.pattern === pattern)) return

  const newMapping = { pattern, targetChannelId }

  // If wildcard exists and this isn't wildcard, insert before it
  const wildcardIndex = mappings.value.findIndex(m => m.pattern === '*')
  if (pattern !== '*' && wildcardIndex !== -1) {
    const updated = [...mappings.value]
    updated.splice(wildcardIndex, 0, newMapping)
    mappings.value = updated
  } else {
    mappings.value = [...mappings.value, newMapping]
  }

  // Reset inputs
  customPattern.value = ''
  customTargetChannelId.value = ''
}

// Remove mapping
const removeMapping = (index: number) => {
  const updated = [...mappings.value]
  updated.splice(index, 1)
  mappings.value = updated
}

// Add wildcard fallback
const addWildcardFallback = () => {
  if (hasWildcard.value) return
  if (!availableClaudeChannels.value.length) return

  // Use first available channel as default
  const defaultChannel = availableClaudeChannels.value[0]
  mappings.value = [...mappings.value, { pattern: '*', targetChannelId: defaultChannel.id }]
}
</script>

<style scoped>
.channel-panel {
  min-height: 200px;
  max-height: 350px;
  overflow-y: auto;
}

.mapping-panel {
  min-height: 200px;
  max-height: 350px;
  overflow-y: auto;
}

.empty-panel {
  background: transparent !important;
  border: 2px dashed rgba(var(--v-theme-on-surface), 0.12);
}

.empty-state {
  min-height: 180px;
}

.model-chip {
  cursor: pointer;
  transition: all 0.2s ease;
}

.model-chip:hover {
  background: rgba(var(--v-theme-primary), 0.1) !important;
  border-color: rgb(var(--v-theme-primary)) !important;
}

.mapping-item {
  cursor: default;
  transition: all 0.2s ease;
}

.drag-handle {
  cursor: grab;
}

.drag-handle:active {
  cursor: grabbing;
}

.ghost-mapping {
  opacity: 0.5;
  background: rgba(var(--v-theme-primary), 0.2) !important;
}
</style>
