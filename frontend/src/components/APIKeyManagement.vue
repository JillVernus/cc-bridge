<template>
  <div class="api-key-management">
    <!-- Header with Add button -->
    <div class="d-flex justify-space-between align-center mb-4">
      <div class="text-h6">{{ t('apiKeys.title') }}</div>
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreateDialog">
        {{ t('apiKeys.create') }}
      </v-btn>
    </div>

    <!-- API Keys Table -->
    <v-card>
      <v-data-table
        :headers="headers"
        :items="keys"
        :loading="loading"
        :items-per-page="20"
        class="elevation-1"
      >
        <!-- Key Prefix -->
        <template v-slot:item.keyPrefix="{ item }">
          <code class="key-prefix">{{ item.keyPrefix }}</code>
        </template>

        <!-- Status -->
        <template v-slot:item.status="{ item }">
          <v-chip
            :color="getStatusColor(item.status)"
            size="small"
            variant="flat"
          >
            {{ t(`apiKeys.status.${item.status}`) }}
          </v-chip>
        </template>

        <!-- Admin Badge -->
        <template v-slot:item.isAdmin="{ item }">
          <v-chip v-if="item.isAdmin" color="warning" size="small" variant="flat">
            {{ t('apiKeys.admin') }}
          </v-chip>
          <span v-else class="text-grey">-</span>
        </template>

        <!-- Rate Limit -->
        <template v-slot:item.rateLimitRpm="{ item }">
          <span v-if="item.rateLimitRpm && item.rateLimitRpm > 0">
            {{ item.rateLimitRpm }} {{ t('apiKeys.rpm') }}
          </span>
          <span v-else class="text-grey">{{ t('apiKeys.useGlobal') }}</span>
        </template>

        <!-- Last Used -->
        <template v-slot:item.lastUsedAt="{ item }">
          <span v-if="item.lastUsedAt">{{ formatDate(item.lastUsedAt) }}</span>
          <span v-else class="text-grey">{{ t('apiKeys.neverUsed') }}</span>
        </template>

        <!-- Created At -->
        <template v-slot:item.createdAt="{ item }">
          {{ formatDate(item.createdAt) }}
        </template>

        <!-- Actions -->
        <template v-slot:item.actions="{ item }">
          <div class="d-flex gap-1">
            <v-tooltip :text="t('apiKeys.edit')" location="top">
              <template v-slot:activator="{ props }">
                <v-btn
                  v-bind="props"
                  icon="mdi-pencil"
                  size="small"
                  variant="text"
                  @click="openEditDialog(item)"
                />
              </template>
            </v-tooltip>

            <v-tooltip v-if="item.status === 'disabled'" :text="t('apiKeys.enable')" location="top">
              <template v-slot:activator="{ props }">
                <v-btn
                  v-bind="props"
                  icon="mdi-check-circle"
                  size="small"
                  variant="text"
                  color="success"
                  @click="enableKey(item)"
                />
              </template>
            </v-tooltip>

            <v-tooltip v-if="item.status === 'active'" :text="t('apiKeys.disable')" location="top">
              <template v-slot:activator="{ props }">
                <v-btn
                  v-bind="props"
                  icon="mdi-pause-circle"
                  size="small"
                  variant="text"
                  color="warning"
                  @click="disableKey(item)"
                />
              </template>
            </v-tooltip>

            <v-tooltip v-if="item.status !== 'revoked'" :text="t('apiKeys.revoke')" location="top">
              <template v-slot:activator="{ props }">
                <v-btn
                  v-bind="props"
                  icon="mdi-cancel"
                  size="small"
                  variant="text"
                  color="error"
                  @click="confirmRevoke(item)"
                />
              </template>
            </v-tooltip>

            <v-tooltip :text="t('apiKeys.delete')" location="top">
              <template v-slot:activator="{ props }">
                <v-btn
                  v-bind="props"
                  icon="mdi-delete"
                  size="small"
                  variant="text"
                  color="error"
                  @click="confirmDelete(item)"
                />
              </template>
            </v-tooltip>
          </div>
        </template>
      </v-data-table>
    </v-card>

    <!-- Create/Edit Dialog -->
    <v-dialog v-model="dialogOpen" max-width="700">
      <v-card class="modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4">
          {{ editingKey ? t('apiKeys.editTitle') : t('apiKeys.createTitle') }}
          <v-spacer />
          <v-btn icon variant="text" size="small" @click="dialogOpen = false" class="modal-action-btn">
            <v-icon>mdi-close</v-icon>
          </v-btn>
          <v-btn
            icon
            variant="flat"
            size="small"
            color="primary"
            :loading="saving"
            :disabled="!formValid"
            @click="saveKey"
            class="modal-action-btn"
          >
            <v-icon>mdi-check</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">
          <v-form ref="formRef" v-model="formValid">
            <v-text-field
              v-model="form.name"
              :label="t('apiKeys.name')"
              :rules="[v => !!v || t('apiKeys.nameRequired')]"
              required
            />
            <v-textarea
              v-model="form.description"
              :label="t('apiKeys.description')"
              rows="2"
            />
            <v-checkbox
              v-if="!editingKey"
              v-model="form.isAdmin"
              :label="t('apiKeys.isAdmin')"
              :hint="t('apiKeys.isAdminHint')"
              persistent-hint
            />
            <v-text-field
              v-model.number="form.rateLimitRpm"
              :label="t('apiKeys.rateLimitRpm')"
              :hint="t('apiKeys.rateLimitRpmHint')"
              persistent-hint
              type="number"
              min="0"
              class="mt-2"
            />

            <!-- Permissions Section -->
            <v-divider class="my-4" />
            <div class="text-subtitle-1 mb-2">{{ t('apiKeys.permissions') }}</div>
            <div class="text-caption text-grey mb-3">{{ t('apiKeys.permissionsHint') }}</div>

            <!-- Allowed Endpoints -->
            <v-select
              v-model="form.allowedEndpoints"
              :label="t('apiKeys.allowedEndpoints')"
              :hint="t('apiKeys.allowedEndpointsHint')"
              :items="[
                { title: t('apiKeys.endpointMessages'), value: 'messages' },
                { title: t('apiKeys.endpointResponses'), value: 'responses' }
              ]"
              multiple
              chips
              closable-chips
              clearable
              persistent-hint
              class="mt-2"
            />

            <!-- Allowed Models -->
            <v-textarea
              v-model="form.allowedModelsText"
              :label="t('apiKeys.allowedModels')"
              :hint="t('apiKeys.allowedModelsHint')"
              persistent-hint
              rows="3"
              class="mt-2"
            />

            <!-- Allowed Channels (Messages) -->
            <v-select
              v-model="form.allowedChannelsMsg"
              :label="t('apiKeys.allowedChannelsMsg')"
              :hint="t('apiKeys.allowedChannelsMsgHint')"
              :items="messagesChannels.map(c => ({ title: `[${c.index}] ${c.name}`, value: c.index }))"
              multiple
              chips
              closable-chips
              clearable
              persistent-hint
              class="mt-2"
            />

            <!-- Allowed Channels (Responses) -->
            <v-select
              v-model="form.allowedChannelsResp"
              :label="t('apiKeys.allowedChannelsResp')"
              :hint="t('apiKeys.allowedChannelsRespHint')"
              :items="responsesChannels.map(c => ({ title: `[${c.index}] ${c.name}`, value: c.index }))"
              multiple
              chips
              closable-chips
              clearable
              persistent-hint
              class="mt-2"
            />
          </v-form>
        </v-card-text>
      </v-card>
    </v-dialog>

    <!-- New Key Dialog (shows the key once) -->
    <v-dialog v-model="newKeyDialogOpen" max-width="600" persistent>
      <v-card class="modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4">
          <v-icon color="success" class="mr-2">mdi-check-circle</v-icon>
          {{ t('apiKeys.keyCreated') }}
          <v-spacer />
          <v-btn icon variant="flat" size="small" color="primary" @click="closeNewKeyDialog" class="modal-action-btn">
            <v-icon>mdi-check</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">
          <v-alert type="warning" variant="tonal" class="mb-4">
            {{ t('apiKeys.keyCreatedWarning') }}
          </v-alert>
          <div class="d-flex align-center gap-2">
            <v-text-field
              :model-value="newKey"
              readonly
              variant="outlined"
              density="compact"
              hide-details
              class="flex-grow-1"
            />
            <v-btn
              icon="mdi-content-copy"
              variant="tonal"
              @click="copyKey"
            />
          </div>
        </v-card-text>
      </v-card>
    </v-dialog>

    <!-- Confirm Revoke Dialog -->
    <v-dialog v-model="revokeDialogOpen" max-width="400">
      <v-card class="modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4">
          {{ t('apiKeys.confirmRevoke') }}
          <v-spacer />
          <v-btn icon variant="text" size="small" @click="revokeDialogOpen = false" class="modal-action-btn">
            <v-icon>mdi-close</v-icon>
          </v-btn>
          <v-btn icon variant="flat" size="small" color="error" :loading="revoking" @click="revokeKey" class="modal-action-btn">
            <v-icon>mdi-check</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">
          <v-alert type="error" variant="tonal" class="mb-2">
            {{ t('apiKeys.revokeWarning') }}
          </v-alert>
          <p>{{ t('apiKeys.revokeConfirmText', { name: keyToRevoke?.name }) }}</p>
        </v-card-text>
      </v-card>
    </v-dialog>

    <!-- Confirm Delete Dialog -->
    <v-dialog v-model="deleteDialogOpen" max-width="400">
      <v-card class="modal-card">
        <v-card-title class="d-flex align-center modal-header pa-4">
          {{ t('apiKeys.confirmDelete') }}
          <v-spacer />
          <v-btn icon variant="text" size="small" @click="deleteDialogOpen = false" class="modal-action-btn">
            <v-icon>mdi-close</v-icon>
          </v-btn>
          <v-btn icon variant="flat" size="small" color="error" :loading="deleting" @click="deleteKey" class="modal-action-btn">
            <v-icon>mdi-check</v-icon>
          </v-btn>
        </v-card-title>
        <v-card-text class="modal-content">
          {{ t('apiKeys.deleteConfirmText', { name: keyToDelete?.name }) }}
        </v-card-text>
      </v-card>
    </v-dialog>

    <!-- Snackbar for notifications -->
    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
      {{ snackbar.text }}
    </v-snackbar>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type APIKey, type CreateAPIKeyRequest, type Channel } from '../services/api'

const { t } = useI18n()

// State
const keys = ref<APIKey[]>([])
const loading = ref(false)
const saving = ref(false)
const revoking = ref(false)
const deleting = ref(false)

// Channels for permission selection
const messagesChannels = ref<Channel[]>([])
const responsesChannels = ref<Channel[]>([])

// Dialog state
const dialogOpen = ref(false)
const newKeyDialogOpen = ref(false)
const revokeDialogOpen = ref(false)
const deleteDialogOpen = ref(false)

const editingKey = ref<APIKey | null>(null)
const keyToRevoke = ref<APIKey | null>(null)
const keyToDelete = ref<APIKey | null>(null)
const newKey = ref('')

// Form state with permissions
interface FormState extends CreateAPIKeyRequest {
  allowedModelsText: string  // For textarea input
}

const formRef = ref()
const formValid = ref(false)
const form = ref<FormState>({
  name: '',
  description: '',
  isAdmin: false,
  rateLimitRpm: 0,
  allowedEndpoints: [],
  allowedChannelsMsg: [],
  allowedChannelsResp: [],
  allowedModels: [],
  allowedModelsText: ''
})

// Snackbar
const snackbar = ref({
  show: false,
  text: '',
  color: 'success'
})

// Table headers
const headers = computed(() => [
  { title: t('apiKeys.name'), key: 'name', sortable: true },
  { title: t('apiKeys.keyPrefix'), key: 'keyPrefix', sortable: false },
  { title: t('apiKeys.status.label'), key: 'status', sortable: true },
  { title: t('apiKeys.role'), key: 'isAdmin', sortable: true },
  { title: t('apiKeys.rateLimit'), key: 'rateLimitRpm', sortable: true },
  { title: t('apiKeys.lastUsed'), key: 'lastUsedAt', sortable: true },
  { title: t('apiKeys.createdAt'), key: 'createdAt', sortable: true },
  { title: t('apiKeys.actions'), key: 'actions', sortable: false, align: 'end' as const }
])

// Methods
const loadKeys = async () => {
  loading.value = true
  try {
    const response = await api.getAPIKeys()
    keys.value = response.keys || []
  } catch (error: any) {
    showSnackbar(error.message || t('apiKeys.loadError'), 'error')
  } finally {
    loading.value = false
  }
}

const loadChannels = async () => {
  try {
    const [msgResp, respResp] = await Promise.all([
      api.getChannels(),
      api.getResponsesChannels()
    ])
    messagesChannels.value = msgResp.channels || []
    responsesChannels.value = respResp.channels || []
  } catch (error) {
    console.error('Failed to load channels:', error)
  }
}

const openCreateDialog = () => {
  editingKey.value = null
  form.value = {
    name: '',
    description: '',
    isAdmin: false,
    rateLimitRpm: 0,
    allowedEndpoints: [],
    allowedChannelsMsg: [],
    allowedChannelsResp: [],
    allowedModels: [],
    allowedModelsText: ''
  }
  dialogOpen.value = true
}

const openEditDialog = (key: APIKey) => {
  editingKey.value = key
  form.value = {
    name: key.name,
    description: key.description || '',
    isAdmin: key.isAdmin,
    rateLimitRpm: key.rateLimitRpm || 0,
    allowedEndpoints: key.allowedEndpoints || [],
    allowedChannelsMsg: key.allowedChannelsMsg || [],
    allowedChannelsResp: key.allowedChannelsResp || [],
    allowedModels: key.allowedModels || [],
    allowedModelsText: (key.allowedModels || []).join('\n')
  }
  dialogOpen.value = true
}

const saveKey = async () => {
  if (!formValid.value) return

  // Parse models from textarea
  const allowedModels = form.value.allowedModelsText
    .split('\n')
    .map(s => s.trim())
    .filter(s => s.length > 0)

  saving.value = true
  try {
    if (editingKey.value) {
      await api.updateAPIKey(editingKey.value.id, {
        name: form.value.name,
        description: form.value.description,
        rateLimitRpm: form.value.rateLimitRpm || 0,
        allowedEndpoints: form.value.allowedEndpoints,
        allowedChannelsMsg: form.value.allowedChannelsMsg,
        allowedChannelsResp: form.value.allowedChannelsResp,
        allowedModels: allowedModels
      })
      showSnackbar(t('apiKeys.updateSuccess'), 'success')
    } else {
      const response = await api.createAPIKey({
        ...form.value,
        allowedModels: allowedModels
      })
      newKey.value = response.key
      newKeyDialogOpen.value = true
      showSnackbar(t('apiKeys.createSuccess'), 'success')
    }
    dialogOpen.value = false
    await loadKeys()
  } catch (error: any) {
    showSnackbar(error.message || t('apiKeys.saveError'), 'error')
  } finally {
    saving.value = false
  }
}

const enableKey = async (key: APIKey) => {
  try {
    await api.enableAPIKey(key.id)
    showSnackbar(t('apiKeys.enableSuccess'), 'success')
    await loadKeys()
  } catch (error: any) {
    showSnackbar(error.message || t('apiKeys.enableError'), 'error')
  }
}

const disableKey = async (key: APIKey) => {
  try {
    await api.disableAPIKey(key.id)
    showSnackbar(t('apiKeys.disableSuccess'), 'success')
    await loadKeys()
  } catch (error: any) {
    showSnackbar(error.message || t('apiKeys.disableError'), 'error')
  }
}

const confirmRevoke = (key: APIKey) => {
  keyToRevoke.value = key
  revokeDialogOpen.value = true
}

const revokeKey = async () => {
  if (!keyToRevoke.value) return

  revoking.value = true
  try {
    await api.revokeAPIKey(keyToRevoke.value.id)
    showSnackbar(t('apiKeys.revokeSuccess'), 'success')
    revokeDialogOpen.value = false
    await loadKeys()
  } catch (error: any) {
    showSnackbar(error.message || t('apiKeys.revokeError'), 'error')
  } finally {
    revoking.value = false
  }
}

const confirmDelete = (key: APIKey) => {
  keyToDelete.value = key
  deleteDialogOpen.value = true
}

const deleteKey = async () => {
  if (!keyToDelete.value) return

  deleting.value = true
  try {
    await api.deleteAPIKey(keyToDelete.value.id)
    showSnackbar(t('apiKeys.deleteSuccess'), 'success')
    deleteDialogOpen.value = false
    await loadKeys()
  } catch (error: any) {
    showSnackbar(error.message || t('apiKeys.deleteError'), 'error')
  } finally {
    deleting.value = false
  }
}

const copyKey = async () => {
  try {
    // Try modern clipboard API first
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(newKey.value)
    } else {
      // Fallback for non-secure contexts (HTTP)
      const textArea = document.createElement('textarea')
      textArea.value = newKey.value
      textArea.style.position = 'fixed'
      textArea.style.left = '-999999px'
      textArea.style.top = '-999999px'
      document.body.appendChild(textArea)
      textArea.focus()
      textArea.select()
      document.execCommand('copy')
      document.body.removeChild(textArea)
    }
    showSnackbar(t('apiKeys.keyCopied'), 'success')
  } catch {
    showSnackbar(t('apiKeys.copyError'), 'error')
  }
}

const closeNewKeyDialog = () => {
  newKey.value = ''
  newKeyDialogOpen.value = false
}

const getStatusColor = (status: string) => {
  switch (status) {
    case 'active': return 'success'
    case 'disabled': return 'warning'
    case 'revoked': return 'error'
    default: return 'grey'
  }
}

const formatDate = (dateStr: string) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return date.toLocaleString()
}

const showSnackbar = (text: string, color: string) => {
  snackbar.value = { show: true, text, color }
}

// Lifecycle
onMounted(() => {
  loadKeys()
  loadChannels()
})
</script>

<style scoped>
.api-key-management {
  padding: 16px;
}

/* Modal card structure */
.modal-card {
  display: flex;
  flex-direction: column;
  max-height: 85vh;
}

.modal-header {
  flex-shrink: 0;
  border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.12);
}

.modal-content {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
  padding: 16px;
}

/* iOS-style action buttons */
.modal-action-btn {
  width: 36px;
  height: 36px;
}

.key-prefix {
  font-family: monospace;
  background-color: rgba(var(--v-theme-surface-variant), 0.5);
  padding: 2px 6px;
  border-radius: 4px;
}

.gap-1 {
  gap: 4px;
}

.gap-2 {
  gap: 8px;
}
</style>
