<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="1000">
    <v-card class="settings-card">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-currency-usd</v-icon>
        {{ t('pricing.title') }}
      </v-card-title>
      <v-card-text>
        <div class="d-flex align-center justify-space-between mb-3">
          <div class="text-caption text-grey">{{ t('pricing.priceUnit') }}</div>
          <div class="d-flex ga-2">
            <v-btn
              size="small"
              variant="tonal"
              color="primary"
              @click="showAddModelDialog = true"
            >
              <v-icon class="mr-1">mdi-plus</v-icon>
              {{ t('pricing.addModel') }}
            </v-btn>
            <v-btn
              size="small"
              variant="tonal"
              color="warning"
              @click="confirmResetPricing"
              :loading="resettingPricing"
            >
              <v-icon class="mr-1">mdi-refresh</v-icon>
              {{ t('pricing.resetDefault') }}
            </v-btn>
          </div>
        </div>

        <v-table density="compact" class="pricing-table" v-if="pricingConfig">
          <thead>
            <tr>
              <th>{{ t('pricing.modelName') }}</th>
              <th class="text-end">{{ t('pricing.inputPrice') }}</th>
              <th class="text-end">{{ t('pricing.outputPrice') }}</th>
              <th class="text-end">{{ t('pricing.cacheCreationPrice') }}</th>
              <th class="text-end">{{ t('pricing.cacheReadPrice') }}</th>
              <th class="text-center">{{ t('pricing.operation') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(pricing, model) in pricingConfig.models" :key="model">
              <td class="text-caption font-weight-medium">
                {{ model }}
                <span v-if="pricing.description" class="text-grey ml-1">({{ pricing.description }})</span>
              </td>
              <td class="text-end text-caption">${{ pricing.inputPrice }}</td>
              <td class="text-end text-caption">${{ pricing.outputPrice }}</td>
              <td class="text-end text-caption">{{ formatCachePrice(pricing.cacheCreationPrice) }}</td>
              <td class="text-end text-caption">{{ formatCachePrice(pricing.cacheReadPrice) }}</td>
              <td class="text-center">
                <v-btn icon size="x-small" variant="text" @click="editModel(String(model))" :title="t('common.edit')">
                  <v-icon size="16">mdi-pencil</v-icon>
                </v-btn>
                <v-btn icon size="x-small" variant="text" @click="duplicateModel(String(model))" :title="t('pricing.duplicate')">
                  <v-icon size="16">mdi-content-copy</v-icon>
                </v-btn>
                <v-btn icon size="x-small" variant="text" color="error" @click="confirmDeleteModel(String(model))" :title="t('common.delete')">
                  <v-icon size="16">mdi-delete</v-icon>
                </v-btn>
              </td>
            </tr>
            <tr v-if="!pricingConfig || Object.keys(pricingConfig.models).length === 0">
              <td colspan="6" class="text-center text-caption text-grey">{{ t('pricing.noPricingConfig') }}</td>
            </tr>
          </tbody>
        </v-table>
        <div v-else class="text-center pa-4 text-grey">
          <v-progress-circular v-if="loadingPricing" indeterminate size="24" />
          <span v-else>{{ t('pricing.loadPricingFailed') }}</span>
        </div>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="$emit('update:modelValue', false)">{{ t('common.close') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- 添加/编辑模型定价对话框 -->
  <v-dialog v-model="showAddModelDialog" max-width="450">
    <v-card>
      <v-card-title>{{ editingModel ? t('pricing.editModelPricing') : t('pricing.addModelPricing') }}</v-card-title>
      <v-card-text>
        <v-text-field
          v-model="modelForm.name"
          :label="t('pricing.modelName')"
          :placeholder="t('pricing.modelNamePlaceholder')"
          :disabled="!!editingModel"
          density="compact"
          class="mb-2"
        />
        <v-text-field
          v-model="modelForm.description"
          :label="t('pricing.descriptionOptional')"
          :placeholder="t('pricing.modelDescription')"
          density="compact"
          class="mb-2"
        />
        <v-row>
          <v-col cols="6">
            <v-text-field
              v-model.number="modelForm.inputPrice"
              :label="t('pricing.inputPriceLabel')"
              type="number"
              step="0.01"
              density="compact"
            />
          </v-col>
          <v-col cols="6">
            <v-text-field
              v-model.number="modelForm.outputPrice"
              :label="t('pricing.outputPriceLabel')"
              type="number"
              step="0.01"
              density="compact"
            />
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="6">
            <v-text-field
              v-model.number="modelForm.cacheCreationPrice"
              :label="t('pricing.cacheCreationLabel')"
              type="number"
              step="0.01"
              density="compact"
              :hint="t('pricing.cacheCreationHint')"
              persistent-hint
            />
          </v-col>
          <v-col cols="6">
            <v-text-field
              v-model.number="modelForm.cacheReadPrice"
              :label="t('pricing.cacheReadLabel')"
              type="number"
              step="0.01"
              density="compact"
              :hint="t('pricing.cacheReadHint')"
              persistent-hint
            />
          </v-col>
        </v-row>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="cancelModelEdit">{{ t('common.cancel') }}</v-btn>
        <v-btn color="primary" variant="flat" @click="saveModelPricing" :loading="savingPricing">{{ t('common.save') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- 删除模型确认对话框 -->
  <v-dialog v-model="showDeleteModelDialog" max-width="400">
    <v-card>
      <v-card-title class="text-error">{{ t('common.confirm') }}</v-card-title>
      <v-card-text>{{ t('pricing.confirmDeleteModel', { model: deletingModel }) }}</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="showDeleteModelDialog = false">{{ t('common.cancel') }}</v-btn>
        <v-btn color="error" variant="flat" @click="deleteModelPricing" :loading="deletingPricingModel">{{ t('common.confirm') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- 重置定价确认对话框 -->
  <v-dialog v-model="showResetPricingDialog" max-width="400">
    <v-card>
      <v-card-title class="text-warning">{{ t('pricing.confirmReset') }}</v-card-title>
      <v-card-text>{{ t('pricing.confirmResetDesc') }}</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="showResetPricingDialog = false">{{ t('common.cancel') }}</v-btn>
        <v-btn color="warning" variant="flat" @click="resetPricing" :loading="resettingPricing">{{ t('common.confirm') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type PricingConfig, type ModelPricing } from '../services/api'

// i18n
const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

// Pricing configuration
const pricingConfig = ref<PricingConfig | null>(null)
const loadingPricing = ref(false)
const showAddModelDialog = ref(false)
const showDeleteModelDialog = ref(false)
const showResetPricingDialog = ref(false)
const editingModel = ref<string | null>(null)
const deletingModel = ref<string | null>(null)
const savingPricing = ref(false)
const deletingPricingModel = ref(false)
const resettingPricing = ref(false)

const modelForm = ref({
  name: '',
  description: '',
  inputPrice: 0,
  outputPrice: 0,
  cacheCreationPrice: 0,
  cacheReadPrice: 0
})

// Load pricing when dialog opens
watch(() => props.modelValue, (newVal) => {
  if (newVal && !pricingConfig.value) {
    loadPricing()
  }
})

const loadPricing = async () => {
  loadingPricing.value = true
  try {
    pricingConfig.value = await api.getPricing()
  } catch (error) {
    console.error('Failed to load pricing:', error)
  } finally {
    loadingPricing.value = false
  }
}

const editModel = (model: string) => {
  editingModel.value = model
  const pricing = pricingConfig.value?.models[model]
  if (pricing) {
    modelForm.value = {
      name: model,
      description: pricing.description || '',
      inputPrice: pricing.inputPrice,
      outputPrice: pricing.outputPrice,
      cacheCreationPrice: pricing.cacheCreationPrice || 0,
      cacheReadPrice: pricing.cacheReadPrice || 0
    }
    showAddModelDialog.value = true
  }
}

const duplicateModel = (model: string) => {
  const pricing = pricingConfig.value?.models[model]
  if (pricing) {
    editingModel.value = null
    modelForm.value = {
      name: model + '-copy',
      description: pricing.description || '',
      inputPrice: pricing.inputPrice,
      outputPrice: pricing.outputPrice,
      cacheCreationPrice: pricing.cacheCreationPrice || 0,
      cacheReadPrice: pricing.cacheReadPrice || 0
    }
    showAddModelDialog.value = true
  }
}

const cancelModelEdit = () => {
  showAddModelDialog.value = false
  editingModel.value = null
  modelForm.value = {
    name: '',
    description: '',
    inputPrice: 0,
    outputPrice: 0,
    cacheCreationPrice: 0,
    cacheReadPrice: 0
  }
}

const saveModelPricing = async () => {
  if (!modelForm.value.name) return

  savingPricing.value = true
  try {
    const pricing: ModelPricing = {
      inputPrice: modelForm.value.inputPrice,
      outputPrice: modelForm.value.outputPrice,
      cacheCreationPrice: modelForm.value.cacheCreationPrice === 0 ? 0 : (modelForm.value.cacheCreationPrice || undefined),
      cacheReadPrice: modelForm.value.cacheReadPrice === 0 ? 0 : (modelForm.value.cacheReadPrice || undefined),
      description: modelForm.value.description || undefined
    }
    await api.setModelPricing(modelForm.value.name, pricing)
    await loadPricing()
    cancelModelEdit()
  } catch (error) {
    console.error('Failed to save model pricing:', error)
  } finally {
    savingPricing.value = false
  }
}

const confirmDeleteModel = (model: string) => {
  deletingModel.value = model
  showDeleteModelDialog.value = true
}

const deleteModelPricing = async () => {
  if (!deletingModel.value) return

  deletingPricingModel.value = true
  try {
    await api.deleteModelPricing(deletingModel.value)
    await loadPricing()
    showDeleteModelDialog.value = false
    deletingModel.value = null
  } catch (error) {
    console.error('Failed to delete model pricing:', error)
  } finally {
    deletingPricingModel.value = false
  }
}

const confirmResetPricing = () => {
  showResetPricingDialog.value = true
}

const resetPricing = async () => {
  resettingPricing.value = true
  try {
    const result = await api.resetPricingToDefault()
    pricingConfig.value = result.config
    showResetPricingDialog.value = false
  } catch (error) {
    console.error('Failed to reset pricing:', error)
  } finally {
    resettingPricing.value = false
  }
}

// 格式化缓存价格：null/undefined 显示 "-"（使用默认值），0 显示 "$0"（免费）
const formatCachePrice = (price: number | null | undefined): string => {
  if (price === null || price === undefined) {
    return '-'
  }
  return `$${price}`
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

/* Pricing table styles */
.pricing-table {
  border: 1px solid rgba(var(--v-theme-on-surface), 0.1);
  border-radius: 0 !important;
}

.pricing-table th {
  background: rgba(var(--v-theme-on-surface), 0.05) !important;
  font-weight: 600 !important;
  font-size: 0.8rem !important;
  padding: 8px 12px !important;
  white-space: nowrap;
}

.pricing-table td {
  padding: 6px 12px !important;
}

.pricing-table tbody tr:hover {
  background: rgba(var(--v-theme-primary), 0.05);
}
</style>
