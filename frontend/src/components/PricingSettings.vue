<template>
  <v-dialog :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)" max-width="800">
    <v-card class="settings-card">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-currency-usd</v-icon>
        模型定价配置
      </v-card-title>
      <v-card-text>
        <div class="d-flex align-center justify-space-between mb-3">
          <div class="text-caption text-grey">设置各模型的 token 价格（单位：$/1M tokens）</div>
          <div class="d-flex ga-2">
            <v-btn
              size="small"
              variant="tonal"
              color="primary"
              @click="showAddModelDialog = true"
            >
              <v-icon class="mr-1">mdi-plus</v-icon>
              添加模型
            </v-btn>
            <v-btn
              size="small"
              variant="tonal"
              color="warning"
              @click="confirmResetPricing"
              :loading="resettingPricing"
            >
              <v-icon class="mr-1">mdi-refresh</v-icon>
              重置默认
            </v-btn>
          </div>
        </div>

        <v-table density="compact" class="pricing-table" v-if="pricingConfig">
          <thead>
            <tr>
              <th>模型</th>
              <th class="text-end">输入价格</th>
              <th class="text-end">输出价格</th>
              <th class="text-end">缓存创建</th>
              <th class="text-end">缓存读取</th>
              <th class="text-center">操作</th>
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
                <v-btn icon size="x-small" variant="text" @click="editModel(String(model))">
                  <v-icon size="16">mdi-pencil</v-icon>
                </v-btn>
                <v-btn icon size="x-small" variant="text" color="error" @click="confirmDeleteModel(String(model))">
                  <v-icon size="16">mdi-delete</v-icon>
                </v-btn>
              </td>
            </tr>
            <tr v-if="!pricingConfig || Object.keys(pricingConfig.models).length === 0">
              <td colspan="6" class="text-center text-caption text-grey">暂无定价配置</td>
            </tr>
          </tbody>
        </v-table>
        <div v-else class="text-center pa-4 text-grey">
          <v-progress-circular v-if="loadingPricing" indeterminate size="24" />
          <span v-else>加载定价配置失败</span>
        </div>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="$emit('update:modelValue', false)">关闭</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- 添加/编辑模型定价对话框 -->
  <v-dialog v-model="showAddModelDialog" max-width="450">
    <v-card>
      <v-card-title>{{ editingModel ? '编辑模型定价' : '添加模型定价' }}</v-card-title>
      <v-card-text>
        <v-text-field
          v-model="modelForm.name"
          label="模型名称"
          placeholder="例如: claude-3-5-sonnet"
          :disabled="!!editingModel"
          density="compact"
          class="mb-2"
        />
        <v-text-field
          v-model="modelForm.description"
          label="描述 (可选)"
          placeholder="模型描述"
          density="compact"
          class="mb-2"
        />
        <v-row>
          <v-col cols="6">
            <v-text-field
              v-model.number="modelForm.inputPrice"
              label="输入价格 ($/1M)"
              type="number"
              step="0.01"
              density="compact"
            />
          </v-col>
          <v-col cols="6">
            <v-text-field
              v-model.number="modelForm.outputPrice"
              label="输出价格 ($/1M)"
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
              label="缓存创建价格 ($/1M)"
              type="number"
              step="0.01"
              density="compact"
              hint="默认为输入价格×1.25"
              persistent-hint
            />
          </v-col>
          <v-col cols="6">
            <v-text-field
              v-model.number="modelForm.cacheReadPrice"
              label="缓存读取价格 ($/1M)"
              type="number"
              step="0.01"
              density="compact"
              hint="默认为输入价格×0.1"
              persistent-hint
            />
          </v-col>
        </v-row>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="cancelModelEdit">取消</v-btn>
        <v-btn color="primary" variant="flat" @click="saveModelPricing" :loading="savingPricing">保存</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- 删除模型确认对话框 -->
  <v-dialog v-model="showDeleteModelDialog" max-width="400">
    <v-card>
      <v-card-title class="text-error">确认删除</v-card-title>
      <v-card-text>确定要删除模型 "{{ deletingModel }}" 的定价配置吗？</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="showDeleteModelDialog = false">取消</v-btn>
        <v-btn color="error" variant="flat" @click="deleteModelPricing" :loading="deletingPricingModel">确认删除</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <!-- 重置定价确认对话框 -->
  <v-dialog v-model="showResetPricingDialog" max-width="400">
    <v-card>
      <v-card-title class="text-warning">确认重置</v-card-title>
      <v-card-text>确定要将所有定价配置重置为默认值吗？自定义的定价将会丢失。</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="showResetPricingDialog = false">取消</v-btn>
        <v-btn color="warning" variant="flat" @click="resetPricing" :loading="resettingPricing">确认重置</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, type PricingConfig, type ModelPricing } from '../services/api'

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
      cacheCreationPrice: modelForm.value.cacheCreationPrice || undefined,
      cacheReadPrice: modelForm.value.cacheReadPrice || undefined,
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
