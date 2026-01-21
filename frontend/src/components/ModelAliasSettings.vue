<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="700">
    <v-card class="settings-card modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4">
        <v-icon class="mr-2">mdi-swap-horizontal</v-icon>
        {{ t('modelAliases.title') }}
        <v-spacer />
        <v-btn icon variant="text" size="small" @click="$emit('update:modelValue', false)" class="modal-action-btn">
          <v-icon>mdi-close</v-icon>
        </v-btn>
      </v-card-title>
      <v-card-text class="modal-content">
        <div class="d-flex align-center justify-space-between mb-3">
          <div class="text-caption text-grey">{{ t('modelAliases.description') }}</div>
          <v-btn
            size="small"
            variant="tonal"
            color="warning"
            @click="confirmResetAliases"
            :loading="resettingAliases"
          >
            <v-icon class="mr-1">mdi-refresh</v-icon>
            {{ t('modelAliases.resetDefault') }}
          </v-btn>
        </div>

        <div v-if="aliasesConfig">
          <!-- Messages API Models -->
          <div class="mb-4">
            <div class="d-flex align-center justify-space-between mb-2">
              <div class="text-subtitle-2 font-weight-bold">{{ t('modelAliases.messagesModels') }}</div>
              <v-btn size="x-small" variant="tonal" color="primary" @click="openAddDialog('messages')">
                <v-icon size="16" class="mr-1">mdi-plus</v-icon>
                {{ t('modelAliases.addModel') }}
              </v-btn>
            </div>
            <v-table density="compact" class="aliases-table">
              <thead>
                <tr>
                  <th>{{ t('modelAliases.modelValue') }}</th>
                  <th>{{ t('modelAliases.modelDescription') }}</th>
                  <th class="text-center" style="width: 100px;">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(alias, index) in aliasesConfig.messagesModels" :key="'msg-' + index">
                  <td class="text-caption font-weight-medium">{{ alias.value }}</td>
                  <td class="text-caption text-grey">{{ alias.description || '-' }}</td>
                  <td class="text-center">
                    <v-btn icon size="x-small" variant="text" @click="editAlias('messages', index)" :title="t('common.edit')">
                      <v-icon size="16">mdi-pencil</v-icon>
                    </v-btn>
                    <v-btn icon size="x-small" variant="text" color="error" @click="confirmDeleteAlias('messages', index)" :title="t('common.delete')">
                      <v-icon size="16">mdi-delete</v-icon>
                    </v-btn>
                  </td>
                </tr>
                <tr v-if="aliasesConfig.messagesModels.length === 0">
                  <td colspan="3" class="text-center text-caption text-grey">{{ t('modelAliases.noModels') }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>

          <!-- Gemini API Models -->
          <div class="mb-4">
            <div class="d-flex align-center justify-space-between mb-2">
              <div class="text-subtitle-2 font-weight-bold">{{ t('modelAliases.geminiModels') }}</div>
              <v-btn size="x-small" variant="tonal" color="primary" @click="openAddDialog('gemini')">
                <v-icon size="16" class="mr-1">mdi-plus</v-icon>
                {{ t('modelAliases.addModel') }}
              </v-btn>
            </div>
            <v-table density="compact" class="aliases-table">
              <thead>
                <tr>
                  <th>{{ t('modelAliases.modelValue') }}</th>
                  <th>{{ t('modelAliases.modelDescription') }}</th>
                  <th class="text-center" style="width: 100px;">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(alias, index) in aliasesConfig.geminiModels" :key="'gem-' + index">
                  <td class="text-caption font-weight-medium">{{ alias.value }}</td>
                  <td class="text-caption text-grey">{{ alias.description || '-' }}</td>
                  <td class="text-center">
                    <v-btn icon size="x-small" variant="text" @click="editAlias('gemini', index)" :title="t('common.edit')">
                      <v-icon size="16">mdi-pencil</v-icon>
                    </v-btn>
                    <v-btn icon size="x-small" variant="text" color="error" @click="confirmDeleteAlias('gemini', index)" :title="t('common.delete')">
                      <v-icon size="16">mdi-delete</v-icon>
                    </v-btn>
                  </td>
                </tr>
                <tr v-if="aliasesConfig.geminiModels.length === 0">
                  <td colspan="3" class="text-center text-caption text-grey">{{ t('modelAliases.noModels') }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>

          <!-- Responses API Models -->
          <div>
            <div class="d-flex align-center justify-space-between mb-2">
              <div class="text-subtitle-2 font-weight-bold">{{ t('modelAliases.responsesModels') }}</div>
              <v-btn size="x-small" variant="tonal" color="primary" @click="openAddDialog('responses')">
                <v-icon size="16" class="mr-1">mdi-plus</v-icon>
                {{ t('modelAliases.addModel') }}
              </v-btn>
            </div>
            <v-table density="compact" class="aliases-table">
              <thead>
                <tr>
                  <th>{{ t('modelAliases.modelValue') }}</th>
                  <th>{{ t('modelAliases.modelDescription') }}</th>
                  <th class="text-center" style="width: 100px;">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(alias, index) in aliasesConfig.responsesModels" :key="'resp-' + index">
                  <td class="text-caption font-weight-medium">{{ alias.value }}</td>
                  <td class="text-caption text-grey">{{ alias.description || '-' }}</td>
                  <td class="text-center">
                    <v-btn icon size="x-small" variant="text" @click="editAlias('responses', index)" :title="t('common.edit')">
                      <v-icon size="16">mdi-pencil</v-icon>
                    </v-btn>
                    <v-btn icon size="x-small" variant="text" color="error" @click="confirmDeleteAlias('responses', index)" :title="t('common.delete')">
                      <v-icon size="16">mdi-delete</v-icon>
                    </v-btn>
                  </td>
                </tr>
                <tr v-if="aliasesConfig.responsesModels.length === 0">
                  <td colspan="3" class="text-center text-caption text-grey">{{ t('modelAliases.noModels') }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>
        </div>
        <div v-else class="text-center pa-4 text-grey">
          <v-progress-circular v-if="loadingAliases" indeterminate size="24" />
          <span v-else>{{ t('modelAliases.loadFailed') }}</span>
        </div>
      </v-card-text>
    </v-card>
  </v-dialog>

  <!-- Add/Edit Model Dialog -->
  <v-dialog v-model="showAddDialog" max-width="400">
    <v-card class="modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4">
        {{ editingIndex !== null ? t('modelAliases.editModel') : t('modelAliases.addModel') }}
        <v-spacer />
        <v-btn icon variant="text" size="small" @click="cancelEdit" class="modal-action-btn">
          <v-icon>mdi-close</v-icon>
        </v-btn>
        <v-btn icon variant="flat" size="small" color="primary" @click="saveAlias" :loading="savingAlias" class="modal-action-btn">
          <v-icon>mdi-check</v-icon>
        </v-btn>
      </v-card-title>
      <v-card-text class="modal-content">
        <v-text-field
          v-model="aliasForm.value"
          :label="t('modelAliases.modelValue')"
          :placeholder="t('modelAliases.modelValuePlaceholder')"
          density="compact"
          class="mb-2"
        />
        <v-text-field
          v-model="aliasForm.description"
          :label="t('modelAliases.modelDescriptionOptional')"
          :placeholder="t('modelAliases.modelDescriptionPlaceholder')"
          density="compact"
        />
      </v-card-text>
    </v-card>
  </v-dialog>

  <!-- Delete Confirmation Dialog -->
  <v-dialog v-model="showDeleteDialog" max-width="400">
    <v-card class="modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4 text-error">
        {{ t('common.confirm') }}
        <v-spacer />
        <v-btn icon variant="text" size="small" @click="showDeleteDialog = false" class="modal-action-btn">
          <v-icon>mdi-close</v-icon>
        </v-btn>
        <v-btn icon variant="flat" size="small" color="error" @click="deleteAlias" :loading="deletingAlias" class="modal-action-btn">
          <v-icon>mdi-check</v-icon>
        </v-btn>
      </v-card-title>
      <v-card-text class="modal-content">{{ t('modelAliases.confirmDelete', { model: deletingValue }) }}</v-card-text>
    </v-card>
  </v-dialog>

  <!-- Reset Confirmation Dialog -->
  <v-dialog v-model="showResetDialog" max-width="400">
    <v-card class="modal-card">
      <v-card-title class="d-flex align-center modal-header pa-4 text-warning">
        {{ t('modelAliases.confirmReset') }}
        <v-spacer />
        <v-btn icon variant="text" size="small" @click="showResetDialog = false" class="modal-action-btn">
          <v-icon>mdi-close</v-icon>
        </v-btn>
        <v-btn icon variant="flat" size="small" color="warning" @click="resetAliases" :loading="resettingAliases" class="modal-action-btn">
          <v-icon>mdi-check</v-icon>
        </v-btn>
      </v-card-title>
      <v-card-text class="modal-content">{{ t('modelAliases.confirmResetDesc') }}</v-card-text>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type AliasesConfig, type ModelAlias } from '../services/api'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

// State
const aliasesConfig = ref<AliasesConfig | null>(null)
const loadingAliases = ref(false)
const showAddDialog = ref(false)
const showDeleteDialog = ref(false)
const showResetDialog = ref(false)
const savingAlias = ref(false)
const deletingAlias = ref(false)
const resettingAliases = ref(false)

// Edit state
const editingType = ref<'messages' | 'responses' | 'gemini'>('messages')
const editingIndex = ref<number | null>(null)
const deletingType = ref<'messages' | 'responses' | 'gemini'>('messages')
const deletingIndex = ref<number | null>(null)
const deletingValue = ref('')

const aliasForm = ref({
  value: '',
  description: ''
})

// Load aliases when dialog opens
watch(() => props.modelValue, (newVal) => {
  if (newVal && !aliasesConfig.value) {
    loadAliases()
  }
})

const loadAliases = async () => {
  loadingAliases.value = true
  try {
    aliasesConfig.value = await api.getModelAliases()
  } catch (error) {
    console.error('Failed to load aliases:', error)
  } finally {
    loadingAliases.value = false
  }
}

const openAddDialog = (type: 'messages' | 'responses' | 'gemini') => {
  editingType.value = type
  editingIndex.value = null
  aliasForm.value = { value: '', description: '' }
  showAddDialog.value = true
}

const editAlias = (type: 'messages' | 'responses' | 'gemini', index: number) => {
  editingType.value = type
  editingIndex.value = index
  const models =
    type === 'messages'
      ? aliasesConfig.value?.messagesModels
      : type === 'responses'
        ? aliasesConfig.value?.responsesModels
        : aliasesConfig.value?.geminiModels
  const alias = models?.[index]
  if (alias) {
    aliasForm.value = {
      value: alias.value,
      description: alias.description || ''
    }
    showAddDialog.value = true
  }
}

const cancelEdit = () => {
  showAddDialog.value = false
  editingIndex.value = null
  aliasForm.value = { value: '', description: '' }
}

const saveAlias = async () => {
  if (!aliasForm.value.value || !aliasesConfig.value) return

  savingAlias.value = true
  try {
    const newAlias: ModelAlias = {
      value: aliasForm.value.value,
      description: aliasForm.value.description || undefined
    }

    const config = { ...aliasesConfig.value }
    const models =
      editingType.value === 'messages'
        ? [...config.messagesModels]
        : editingType.value === 'responses'
          ? [...config.responsesModels]
          : [...config.geminiModels]

    if (editingIndex.value !== null) {
      models[editingIndex.value] = newAlias
    } else {
      models.push(newAlias)
    }

    if (editingType.value === 'messages') {
      config.messagesModels = models
    } else if (editingType.value === 'responses') {
      config.responsesModels = models
    } else {
      config.geminiModels = models
    }

    await api.updateModelAliases(config)
    aliasesConfig.value = config
    cancelEdit()
  } catch (error) {
    console.error('Failed to save alias:', error)
  } finally {
    savingAlias.value = false
  }
}

const confirmDeleteAlias = (type: 'messages' | 'responses' | 'gemini', index: number) => {
  deletingType.value = type
  deletingIndex.value = index
  const models =
    type === 'messages'
      ? aliasesConfig.value?.messagesModels
      : type === 'responses'
        ? aliasesConfig.value?.responsesModels
        : aliasesConfig.value?.geminiModels
  deletingValue.value = models?.[index]?.value || ''
  showDeleteDialog.value = true
}

const deleteAlias = async () => {
  if (deletingIndex.value === null || !aliasesConfig.value) return

  deletingAlias.value = true
  try {
    const config = { ...aliasesConfig.value }
    if (deletingType.value === 'messages') {
      config.messagesModels = config.messagesModels.filter((_, i) => i !== deletingIndex.value)
    } else if (deletingType.value === 'responses') {
      config.responsesModels = config.responsesModels.filter((_, i) => i !== deletingIndex.value)
    } else {
      config.geminiModels = config.geminiModels.filter((_, i) => i !== deletingIndex.value)
    }

    await api.updateModelAliases(config)
    aliasesConfig.value = config
    showDeleteDialog.value = false
    deletingIndex.value = null
    deletingValue.value = ''
  } catch (error) {
    console.error('Failed to delete alias:', error)
  } finally {
    deletingAlias.value = false
  }
}

const confirmResetAliases = () => {
  showResetDialog.value = true
}

const resetAliases = async () => {
  resettingAliases.value = true
  try {
    const result = await api.resetModelAliasesToDefault()
    aliasesConfig.value = result.config
    showResetDialog.value = false
  } catch (error) {
    console.error('Failed to reset aliases:', error)
  } finally {
    resettingAliases.value = false
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

.aliases-table {
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 4px;
}
</style>
