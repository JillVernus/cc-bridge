<template>
  <v-dialog :model-value="show" @update:model-value="$emit('update:show', $event)" max-width="800" persistent>
    <v-card rounded="lg" class="modal-card">
      <v-card-title class="d-flex align-center ga-3 pa-4 modal-header" :class="headerClasses">
        <v-avatar :color="avatarColor" variant="flat" size="40">
          <v-icon :style="headerIconStyle" size="20">{{ isEditing ? 'mdi-pencil' : 'mdi-plus' }}</v-icon>
        </v-avatar>
        <div class="flex-grow-1">
          <div class="text-h6 font-weight-bold">
            {{ isEditing ? t('addChannel.editTitle') : t('addChannel.addTitle') }}
          </div>
          <div class="text-body-2" :class="subtitleClasses">
            {{ isEditing ? t('addChannel.editSubtitle') : isQuickMode ? t('addChannel.quickAddSubtitle') : t('addChannel.addSubtitle') }}
          </div>
        </div>
        <!-- 模式切换按钮（仅在添加模式显示） -->
        <v-btn v-if="!isEditing" variant="outlined" size="small" @click="toggleMode" class="mode-toggle-btn mr-2">
          <v-icon start size="16">{{ isQuickMode ? 'mdi-form-textbox' : 'mdi-lightning-bolt' }}</v-icon>
          {{ isQuickMode ? t('addChannel.detailedConfig') : t('addChannel.quickAdd') }}
        </v-btn>
        <!-- iOS-style action buttons -->
        <v-btn
          icon
          variant="text"
          size="small"
          @click="handleCancel"
          class="modal-action-btn"
        >
          <v-icon>mdi-close</v-icon>
        </v-btn>
        <v-btn
          icon
          variant="flat"
          size="small"
          color="primary"
          @click="isQuickMode && !isEditing ? handleQuickSubmit() : handleSubmit()"
          :disabled="isQuickMode && !isEditing ? !isQuickFormValid : !isFormValid"
          class="modal-action-btn"
        >
          <v-icon>mdi-check</v-icon>
        </v-btn>
      </v-card-title>

      <v-card-text class="pa-6 modal-content">
        <!-- 快速添加模式 -->
        <div v-if="!isEditing && isQuickMode">
          <v-textarea
            v-model="quickInput"
            :label="t('addChannel.inputContent')"
            :placeholder="t('addChannel.inputPlaceholder')"
            variant="outlined"
            rows="10"
            no-resize
            autofocus
            class="quick-input-textarea"
            @input="parseQuickInput"
          />

          <!-- 检测状态提示 -->
          <v-card variant="outlined" class="mt-4 detection-status-card" rounded="lg">
            <v-card-text class="pa-4">
              <div class="d-flex flex-column ga-3">
                <!-- Base URL 检测 -->
                <div class="d-flex align-center ga-3">
                  <v-icon :color="detectedBaseUrl ? 'success' : 'error'" size="20">
                    {{ detectedBaseUrl ? 'mdi-check-circle' : 'mdi-alert-circle' }}
                  </v-icon>
                  <div class="flex-grow-1">
                    <div class="text-body-2 font-weight-medium">Base URL</div>
                    <div class="text-caption" :class="detectedBaseUrl ? 'text-success' : 'text-error'">
                      {{ detectedBaseUrl || t('addChannel.baseUrlRequired') }}
                    </div>
                  </div>
                  <v-chip v-if="detectedBaseUrl" size="x-small" color="success" variant="tonal"> {{ t('addChannel.baseUrlDetected') }} </v-chip>
                </div>

                <!-- API Keys 检测 -->
                <div class="d-flex align-center ga-3">
                  <v-icon :color="detectedApiKeys.length > 0 ? 'success' : 'error'" size="20">
                    {{ detectedApiKeys.length > 0 ? 'mdi-check-circle' : 'mdi-alert-circle' }}
                  </v-icon>
                  <div class="flex-grow-1">
                    <div class="text-body-2 font-weight-medium">{{ t('addChannel.apiKeysLabel') }}</div>
                    <div class="text-caption" :class="detectedApiKeys.length > 0 ? 'text-success' : 'text-error'">
                      {{
                        detectedApiKeys.length > 0
                          ? t('addChannel.keysDetected', { count: detectedApiKeys.length })
                          : t('addChannel.atLeastOneKey')
                      }}
                    </div>
                  </div>
                  <v-chip v-if="detectedApiKeys.length > 0" size="x-small" color="success" variant="tonal">
                    {{ t('addChannel.keysCount', { count: detectedApiKeys.length }) }}
                  </v-chip>
                </div>

                <!-- 渠道名称预览 -->
                <div class="d-flex align-center ga-3">
                  <v-icon color="primary" size="20">mdi-tag</v-icon>
                  <div class="flex-grow-1">
                    <div class="text-body-2 font-weight-medium">{{ t('addChannel.channelName') }}</div>
                    <div class="text-caption text-primary font-weight-medium">
                      {{ generatedChannelName }}
                    </div>
                  </div>
                  <v-chip size="x-small" color="primary" variant="tonal"> {{ t('addChannel.autoGenerated') }} </v-chip>
                </div>

                <!-- 渠道类型提示 -->
                <div class="d-flex align-center ga-3">
                  <v-icon color="info" size="20">mdi-information</v-icon>
                  <div class="flex-grow-1">
                    <div class="text-body-2 font-weight-medium">{{ t('addChannel.channelType') }}</div>
                    <div class="text-caption text-medium-emphasis">
                      {{ props.channelType === 'responses' ? t('addChannel.responsesType') : t('addChannel.messagesType') }} -
                      {{ getDefaultServiceType() }}
                    </div>
                  </div>
                </div>
              </div>
            </v-card-text>
          </v-card>
        </div>

        <!-- 详细表单模式（原有表单） -->
        <v-form v-else ref="formRef" @submit.prevent="handleSubmit">
          <!-- Tabs for Configuration and Quota -->
          <v-tabs v-model="activeTab" color="primary" class="mb-4">
            <v-tab value="config">
              <v-icon start size="small">mdi-cog</v-icon>
              {{ t('addChannel.configTab') }}
            </v-tab>
            <v-tab value="quota" :disabled="!form.serviceType || isCompositeChannel">
              <v-icon start size="small">mdi-gauge</v-icon>
              {{ t('addChannel.quotaTab') }}
            </v-tab>
            <v-tab value="ratelimit" :disabled="!form.serviceType || isCompositeChannel">
              <v-icon start size="small">mdi-speedometer</v-icon>
              {{ t('addChannel.rateLimitTab') }}
            </v-tab>
          </v-tabs>

          <v-tabs-window v-model="activeTab">
            <!-- Configuration Tab -->
            <v-tabs-window-item value="config">
          <v-row>
            <!-- 基本信息 -->
            <v-col cols="12" md="6">
              <v-text-field
                v-model="form.name"
                :label="t('addChannel.channelNameLabel')"
                :placeholder="t('addChannel.channelNamePlaceholder')"
                prepend-inner-icon="mdi-tag"
                variant="outlined"
                density="comfortable"
                :rules="[rules.required]"
                required
                :error-messages="errors.name"
              />
            </v-col>

            <v-col cols="12" md="6">
              <v-select
                v-model="form.serviceType"
                :label="t('addChannel.serviceType')"
                :items="serviceTypeOptions"
                prepend-inner-icon="mdi-cog"
                variant="outlined"
                density="comfortable"
                :rules="[rules.required]"
                required
                :error-messages="errors.serviceType"
              />
            </v-col>

            <!-- 基础URL（非 OAuth 和 Composite 类型显示） -->
            <v-col cols="12" v-if="!isOAuthChannel && !isCompositeChannel">
              <v-text-field
                v-model="form.baseUrl"
                :label="t('addChannel.baseUrl')"
                :placeholder="t('addChannel.baseUrlPlaceholder')"
                prepend-inner-icon="mdi-web"
                variant="outlined"
                density="comfortable"
                type="url"
                :rules="[rules.required, rules.url]"
                required
                :error-messages="errors.baseUrl"
                :hint="getUrlHint()"
                persistent-hint
              />
            </v-col>

            <!-- OAuth 固定 URL 提示 -->
            <v-col cols="12" v-if="isOAuthChannel">
              <v-alert type="info" variant="tonal" density="compact" rounded="lg">
                <div class="d-flex align-center ga-2">
                  <v-icon size="small">mdi-lock</v-icon>
                  <span class="text-body-2">
                    {{ t('addChannel.oauthFixedUrl') }}: <code>https://chatgpt.com/backend-api/codex/responses</code>
                  </span>
                </div>
              </v-alert>
            </v-col>

            <!-- 官网/控制台（可选，非 Composite 类型） -->
            <v-col cols="12" v-if="!isCompositeChannel">
              <v-text-field
                v-model="form.website"
                :label="t('addChannel.websiteLabel')"
                :placeholder="t('addChannel.websitePlaceholder')"
                prepend-inner-icon="mdi-open-in-new"
                variant="outlined"
                density="comfortable"
                type="url"
                :rules="[rules.urlOptional]"
                :error-messages="errors.website"
              />
            </v-col>

            <!-- 跳过 TLS 证书验证（非 Composite 类型） -->
            <v-col cols="12" md="6" v-if="!isCompositeChannel">
              <div class="d-flex align-center justify-space-between">
                <div class="d-flex align-center ga-2">
                  <v-icon color="warning">mdi-shield-alert</v-icon>
                  <div>
                    <div class="text-body-1 font-weight-medium">{{ t('addChannel.skipTlsVerify') }}</div>
                    <div class="text-caption text-medium-emphasis">
                      {{ t('addChannel.skipTlsVerifyHint') }}
                    </div>
                  </div>
                </div>
                <v-switch inset color="warning" hide-details v-model="form.insecureSkipVerify" />
              </div>
            </v-col>

            <!-- 响应头超时（非 Composite 类型） -->
            <v-col cols="12" md="6" v-if="!isCompositeChannel">
              <v-text-field
                v-model.number="form.responseHeaderTimeout"
                :label="t('addChannel.responseTimeout')"
                placeholder="30"
                prepend-inner-icon="mdi-timer-outline"
                variant="outlined"
                density="comfortable"
                type="number"
                min="10"
                max="300"
                :hint="t('addChannel.responseTimeoutHint')"
                persistent-hint
              />
            </v-col>

            <!-- API密钥负载均衡策略（非 Composite 类型） -->
            <v-col cols="12" md="6" v-if="!isCompositeChannel">
              <v-select
                v-model="form.keyLoadBalance"
                :label="t('keyLoadBalance.title')"
                :items="keyLoadBalanceOptions"
                item-title="title"
                item-value="value"
                prepend-inner-icon="mdi-key-chain"
                variant="outlined"
                density="comfortable"
              >
                <template v-slot:item="{ props, item }">
                  <v-list-item v-bind="props">
                    <template v-slot:subtitle>
                      {{ item.raw.description }}
                    </template>
                  </v-list-item>
                </template>
              </v-select>
            </v-col>

            <!-- 描述 -->
            <v-col cols="12">
              <v-textarea
                v-model="form.description"
                :label="t('addChannel.description')"
                :hint="t('addChannel.descriptionHint')"
                persistent-hint
                prepend-inner-icon="mdi-text"
                variant="outlined"
                density="comfortable"
                rows="3"
                no-resize
              />
            </v-col>

            <!-- Composite 渠道模型映射配置 -->
            <v-col cols="12" v-if="isCompositeChannel">
              <CompositeChannelEditor
                :key="compositeEditorKey"
                v-model="form.compositeMappings"
                :all-channels="allChannels"
              />
            </v-col>

            <!-- 模型重定向配置（非 Composite 类型） -->
            <v-col cols="12" v-if="form.serviceType && !isCompositeChannel">
              <v-card variant="outlined" rounded="lg">
                <v-card-title class="d-flex align-center justify-space-between pa-4 pb-2">
                  <div class="d-flex align-center ga-2">
                    <v-icon color="primary">mdi-swap-horizontal</v-icon>
                    <span class="text-body-1 font-weight-bold">{{ t('addChannel.modelRedirect') }}</span>
                  </div>
                  <v-chip size="small" color="secondary" variant="tonal"> {{ t('addChannel.autoConvertModel') }} </v-chip>
                </v-card-title>

                <v-card-text class="pt-2">
                  <div class="text-body-2 text-medium-emphasis mb-4">
                    {{ modelMappingHint }}
                  </div>

                  <!-- 现有映射列表 -->
                  <div v-if="Object.keys(form.modelMapping).length" class="mb-4">
                    <v-list density="compact" class="bg-transparent">
                      <v-list-item
                        v-for="[source, target] in Object.entries(form.modelMapping)"
                        :key="source"
                        class="mb-2"
                        rounded="lg"
                        variant="tonal"
                        color="surface-variant"
                      >
                        <template v-slot:prepend>
                          <v-icon size="small" color="primary">mdi-arrow-right</v-icon>
                        </template>

                        <v-list-item-title>
                          <div class="d-flex align-center ga-2">
                            <code class="text-caption">{{ source }}</code>
                            <v-icon size="small" color="primary">mdi-arrow-right</v-icon>
                            <code class="text-caption">{{ target }}</code>
                          </div>
                        </v-list-item-title>

                        <template v-slot:append>
                          <v-btn size="small" color="error" icon variant="text" @click="removeModelMapping(source)">
                            <v-icon size="small" color="error">mdi-close</v-icon>
                          </v-btn>
                        </template>
                      </v-list-item>
                    </v-list>
                  </div>

                  <!-- 添加新映射 -->
                  <div class="d-flex align-center ga-2">
                    <v-select
                      v-model="newMapping.source"
                      :label="t('addChannel.sourceModel')"
                      :items="sourceModelOptions"
                      variant="outlined"
                      density="comfortable"
                      hide-details
                      class="flex-1-1"
                      :placeholder="t('addChannel.selectSourceModel')"
                    />
                    <v-icon color="primary">mdi-arrow-right</v-icon>
                    <v-text-field
                      v-model="newMapping.target"
                      :label="t('addChannel.targetModel')"
                      :placeholder="targetModelPlaceholder"
                      variant="outlined"
                      density="comfortable"
                      hide-details
                      class="flex-1-1"
                      @keyup.enter="addModelMapping"
                    />
                    <v-btn
                      color="secondary"
                      variant="elevated"
                      @click="addModelMapping"
                      :disabled="!newMapping.source.trim() || !newMapping.target.trim()"
                    >
                      {{ t('common.add') }}
                    </v-btn>
                  </div>
                </v-card-text>
              </v-card>
            </v-col>

            <!-- 价格乘数配置（折扣，非 Composite 类型） -->
            <v-col cols="12" v-if="form.serviceType && !isCompositeChannel">
              <v-card variant="outlined" rounded="lg">
                <v-card-title class="d-flex align-center justify-space-between pa-4 pb-2">
                  <div class="d-flex align-center ga-2">
                    <v-icon color="success">mdi-percent</v-icon>
                    <span class="text-body-1 font-weight-bold">{{ t('addChannel.priceMultiplier') }}</span>
                  </div>
                  <v-chip size="small" color="success" variant="tonal"> {{ t('addChannel.channelDiscount') }} </v-chip>
                </v-card-title>

                <v-card-text class="pt-2">
                  <div class="text-body-2 text-medium-emphasis mb-4">
                    {{ t('addChannel.priceMultiplierHint') }}
                  </div>

                  <!-- 现有乘数列表 -->
                  <div v-if="Object.keys(form.priceMultipliers).length" class="mb-4">
                    <v-list density="compact" class="bg-transparent">
                      <v-list-item
                        v-for="[modelKey, mult] in Object.entries(form.priceMultipliers)"
                        :key="modelKey"
                        class="mb-2"
                        rounded="lg"
                        variant="tonal"
                        color="surface-variant"
                      >
                        <template v-slot:prepend>
                          <v-icon size="small" color="success">mdi-tag-outline</v-icon>
                        </template>

                        <v-list-item-title>
                          <div class="d-flex align-center ga-2 flex-wrap">
                            <code class="text-caption">{{ modelKey === '_default' ? t('addChannel.defaultAllModels') : modelKey }}</code>
                            <span class="text-caption text-medium-emphasis mx-1">:</span>
                            <span class="text-caption" v-if="mult.inputMultiplier && mult.inputMultiplier !== 1">{{ t('addChannel.input') }} {{ mult.inputMultiplier }}x</span>
                            <span class="text-caption" v-if="mult.outputMultiplier && mult.outputMultiplier !== 1">{{ t('addChannel.output') }} {{ mult.outputMultiplier }}x</span>
                            <span class="text-caption" v-if="mult.cacheCreationMultiplier && mult.cacheCreationMultiplier !== 1">{{ t('addChannel.cacheCreation') }} {{ mult.cacheCreationMultiplier }}x</span>
                            <span class="text-caption" v-if="mult.cacheReadMultiplier && mult.cacheReadMultiplier !== 1">{{ t('addChannel.cacheRead') }} {{ mult.cacheReadMultiplier }}x</span>
                            <span class="text-caption text-medium-emphasis" v-if="!hasNonDefaultMultiplier(mult)">{{ t('addChannel.noDiscount') }}</span>
                          </div>
                        </v-list-item-title>

                        <template v-slot:append>
                          <v-btn size="small" color="error" icon variant="text" @click="removePriceMultiplier(modelKey)">
                            <v-icon size="small" color="error">mdi-close</v-icon>
                          </v-btn>
                        </template>
                      </v-list-item>
                    </v-list>
                  </div>

                  <!-- 添加新乘数 -->
                  <div class="price-multiplier-form">
                    <div class="d-flex align-center ga-2 mb-3">
                      <v-text-field
                        v-model="newMultiplier.modelKey"
                        :label="t('addChannel.modelNameDefault')"
                        :placeholder="t('addChannel.modelNamePlaceholder')"
                        variant="outlined"
                        density="comfortable"
                        hide-details
                        class="flex-grow-1"
                      />
                    </div>
                    <div class="d-flex align-center ga-2 mb-3 flex-wrap">
                      <v-text-field
                        v-model.number="newMultiplier.inputMultiplier"
                        :label="t('addChannel.inputMultiplier')"
                        placeholder="1.0"
                        type="number"
                        step="0.1"
                        min="0"
                        variant="outlined"
                        density="comfortable"
                        hide-details
                        style="min-width: 100px; max-width: 120px;"
                      />
                      <v-text-field
                        v-model.number="newMultiplier.outputMultiplier"
                        :label="t('addChannel.outputMultiplier')"
                        placeholder="1.0"
                        type="number"
                        step="0.1"
                        min="0"
                        variant="outlined"
                        density="comfortable"
                        hide-details
                        style="min-width: 100px; max-width: 120px;"
                      />
                      <v-text-field
                        v-model.number="newMultiplier.cacheCreationMultiplier"
                        :label="t('addChannel.cacheCreation')"
                        placeholder="1.0"
                        type="number"
                        step="0.1"
                        min="0"
                        variant="outlined"
                        density="comfortable"
                        hide-details
                        style="min-width: 100px; max-width: 120px;"
                      />
                      <v-text-field
                        v-model.number="newMultiplier.cacheReadMultiplier"
                        :label="t('addChannel.cacheRead')"
                        placeholder="1.0"
                        type="number"
                        step="0.1"
                        min="0"
                        variant="outlined"
                        density="comfortable"
                        hide-details
                        style="min-width: 100px; max-width: 120px;"
                      />
                      <v-btn
                        color="success"
                        variant="elevated"
                        @click="addPriceMultiplier"
                        :disabled="!newMultiplier.modelKey.trim()"
                      >
                        {{ t('common.add') }}
                      </v-btn>
                    </div>
                  </div>
                </v-card-text>
              </v-card>
            </v-col>
          </v-row>
            </v-tabs-window-item>

            <!-- Quota Tab -->
            <v-tabs-window-item value="quota">
              <v-card variant="outlined" rounded="lg" class="mb-4">
                <v-card-title class="d-flex align-center justify-space-between pa-4 pb-2">
                  <div class="d-flex align-center ga-2">
                    <v-icon color="info">mdi-gauge</v-icon>
                    <span class="text-body-1 font-weight-bold">{{ t('quota.title') }}</span>
                  </div>
                  <v-chip size="small" color="info" variant="tonal">{{ t('common.optional') }}</v-chip>
                </v-card-title>

                <v-card-text class="pt-2">
                  <div class="text-body-2 text-medium-emphasis mb-4">
                    {{ t('quota.description') }}
                  </div>

                  <!-- Quota Type -->
                  <div class="d-flex align-center ga-3 mb-4">
                    <v-select
                      v-model="form.quotaType"
                      :label="t('quota.quotaType')"
                      :items="[
                        { title: t('quota.quotaTypeNone'), value: '' },
                        { title: t('quota.quotaTypeRequests'), value: 'requests' },
                        { title: t('quota.quotaTypeCredit'), value: 'credit' }
                      ]"
                      variant="outlined"
                      density="comfortable"
                      hide-details
                      style="max-width: 200px;"
                    />

                    <v-text-field
                      v-if="form.quotaType"
                      v-model.number="form.quotaLimit"
                      :label="t('quota.quotaLimit')"
                      :placeholder="form.quotaType === 'credit' ? '100.00' : '1000'"
                      type="number"
                      :step="form.quotaType === 'credit' ? 0.01 : 1"
                      min="0"
                      variant="outlined"
                      density="comfortable"
                      hide-details
                      style="max-width: 180px;"
                    >
                      <template #append-inner>
                        <v-tooltip location="top">
                          <template #activator="{ props }">
                            <v-icon v-bind="props" size="small" color="grey">mdi-information-outline</v-icon>
                          </template>
                          {{ t('quota.quotaLimitHint') }}
                        </v-tooltip>
                      </template>
                    </v-text-field>
                  </div>

                  <!-- Reset Configuration (only if quota type selected) -->
                  <div v-if="form.quotaType" class="mt-4">
                    <div class="text-body-2 text-medium-emphasis mb-3">{{ t('quota.resetConfig') }}</div>
                    <div class="d-flex align-center ga-3 flex-wrap">
                      <!-- First Reset Time -->
                      <v-text-field
                        v-model="form.quotaResetAt"
                        :label="t('quota.firstResetAt')"
                        type="datetime-local"
                        variant="outlined"
                        density="comfortable"
                        hide-details
                        style="min-width: 280px; max-width: 320px;"
                      >
                        <template #append-inner>
                          <v-tooltip location="top">
                            <template #activator="{ props }">
                              <v-icon v-bind="props" size="small" color="grey">mdi-information-outline</v-icon>
                            </template>
                            {{ t('quota.firstResetAtHint') }}
                          </v-tooltip>
                        </template>
                      </v-text-field>

                      <!-- Reset Interval -->
                      <v-text-field
                        v-model.number="form.quotaResetInterval"
                        :label="t('quota.resetInterval')"
                        type="number"
                        min="1"
                        placeholder="1"
                        variant="outlined"
                        density="comfortable"
                        hide-details
                        style="min-width: 100px; max-width: 120px;"
                      >
                        <template #append-inner>
                          <v-tooltip location="top">
                            <template #activator="{ props }">
                              <v-icon v-bind="props" size="small" color="grey">mdi-information-outline</v-icon>
                            </template>
                            {{ t('quota.resetIntervalHint') }}
                          </v-tooltip>
                        </template>
                      </v-text-field>

                      <!-- Interval Unit -->
                      <v-select
                        v-model="form.quotaResetUnit"
                        :items="[
                          { title: t('quota.intervalUnit.hours'), value: 'hours' },
                          { title: t('quota.intervalUnit.days'), value: 'days' },
                          { title: t('quota.intervalUnit.weeks'), value: 'weeks' },
                          { title: t('quota.intervalUnit.months'), value: 'months' }
                        ]"
                        variant="outlined"
                        density="comfortable"
                        hide-details
                        style="min-width: 100px; max-width: 140px;"
                      />
                    </div>
                  </div>

                  <!-- Reset Mode (only if quota type selected) -->
                  <div v-if="form.quotaType" class="mt-4">
                    <div class="d-flex align-center mb-2">
                      <span class="text-body-2 text-medium-emphasis">{{ t('quota.resetMode') }}</span>
                      <v-tooltip location="top">
                        <template #activator="{ props }">
                          <v-icon v-bind="props" size="small" color="grey" class="ml-1">mdi-information-outline</v-icon>
                        </template>
                        {{ form.quotaResetMode === 'fixed' ? t('quota.resetModeFixedHint') : t('quota.resetModeRollingHint') }}
                      </v-tooltip>
                    </div>
                    <v-radio-group v-model="form.quotaResetMode" inline density="compact" hide-details>
                      <v-radio :label="t('quota.resetModeFixed')" value="fixed" />
                      <v-radio :label="t('quota.resetModeRolling')" value="rolling" />
                    </v-radio-group>
                  </div>

                  <!-- Model Filter (only if quota type selected) -->
                  <div v-if="form.quotaType" class="mt-4">
                    <div class="d-flex align-center mb-2">
                      <span class="text-body-2 text-medium-emphasis">{{ t('quota.quotaModels') }}</span>
                      <v-tooltip location="top">
                        <template #activator="{ props }">
                          <v-icon v-bind="props" size="small" color="grey" class="ml-1">mdi-information-outline</v-icon>
                        </template>
                        {{ t('quota.quotaModelsHint') }}
                      </v-tooltip>
                    </div>
                    <v-combobox
                      v-model="form.quotaModels"
                      :label="t('quota.quotaModelsLabel')"
                      :placeholder="t('quota.quotaModelsPlaceholder')"
                      variant="outlined"
                      density="comfortable"
                      chips
                      closable-chips
                      multiple
                      hide-details
                    />
                  </div>
                </v-card-text>
              </v-card>
            </v-tabs-window-item>

            <!-- Rate Limit Tab -->
            <v-tabs-window-item value="ratelimit">
              <v-card variant="outlined" rounded="lg" class="mb-4">
                <v-card-title class="d-flex align-center justify-space-between pa-4 pb-2">
                  <div class="d-flex align-center ga-2">
                    <v-icon color="primary">mdi-speedometer</v-icon>
                    <span class="text-body-1 font-weight-bold">{{ t('channelRateLimit.title') }}</span>
                  </div>
                  <v-chip size="small" color="info" variant="tonal">{{ t('channelRateLimit.upstreamProtection') }}</v-chip>
                </v-card-title>

                <v-card-text class="pt-2">
                  <div class="text-body-2 text-medium-emphasis mb-4">
                    {{ t('channelRateLimit.description') }}
                  </div>

                  <!-- Rate Limit RPM -->
                  <div class="d-flex align-center ga-3 flex-wrap mb-4">
                    <v-text-field
                      v-model.number="form.rateLimitRpm"
                      :label="t('channelRateLimit.rateLimitRpm')"
                      :placeholder="t('channelRateLimit.rateLimitRpmPlaceholder')"
                      type="number"
                      min="0"
                      step="1"
                      variant="outlined"
                      density="comfortable"
                      hide-details
                      style="max-width: 200px;"
                    />
                    <span class="text-body-2 text-medium-emphasis">{{ t('channelRateLimit.rpmHint') }}</span>
                  </div>

                  <!-- Queue Mode -->
                  <div v-if="form.rateLimitRpm && form.rateLimitRpm > 0" class="mt-4">
                    <div class="d-flex align-center mb-3">
                      <v-switch
                        v-model="form.queueEnabled"
                        :label="t('channelRateLimit.queueEnabled')"
                        color="primary"
                        density="compact"
                        hide-details
                      />
                      <v-tooltip location="top">
                        <template #activator="{ props }">
                          <v-icon v-bind="props" size="small" color="grey" class="ml-2">mdi-information-outline</v-icon>
                        </template>
                        {{ t('channelRateLimit.queueEnabledHint') }}
                      </v-tooltip>
                    </div>

                    <!-- Queue Timeout (only when queue enabled) -->
                    <div v-if="form.queueEnabled" class="d-flex align-center ga-3 flex-wrap">
                      <v-text-field
                        v-model.number="form.queueTimeout"
                        :label="t('channelRateLimit.queueTimeout')"
                        :placeholder="'60'"
                        type="number"
                        min="1"
                        max="300"
                        step="1"
                        variant="outlined"
                        density="comfortable"
                        hide-details
                        style="max-width: 150px;"
                      />
                      <span class="text-body-2 text-medium-emphasis">{{ t('channelRateLimit.queueTimeoutHint') }}</span>
                    </div>
                  </div>

                  <!-- Info Alert -->
                  <v-alert
                    v-if="form.rateLimitRpm && form.rateLimitRpm > 0"
                    type="info"
                    variant="tonal"
                    class="mt-4"
                    rounded="lg"
                  >
                    <div class="text-body-2">
                      <strong>{{ t('channelRateLimit.behaviorTitle') }}:</strong>
                      <span v-if="form.queueEnabled">
                        {{ t('channelRateLimit.behaviorQueue', { rpm: form.rateLimitRpm, timeout: form.queueTimeout || 60 }) }}
                      </span>
                      <span v-else>
                        {{ t('channelRateLimit.behaviorReject', { rpm: form.rateLimitRpm }) }}
                      </span>
                    </div>
                  </v-alert>
                </v-card-text>
              </v-card>
            </v-tabs-window-item>
          </v-tabs-window>

          <v-row>
            <!-- OAuth 认证配置（仅 openai-oauth 类型显示） -->
            <v-col cols="12" v-if="isOAuthChannel">
              <v-card variant="outlined" rounded="lg" :color="oauthCardColor">
                <v-card-title class="d-flex align-center justify-space-between pa-4 pb-2">
                  <div class="d-flex align-center ga-2">
                    <v-icon :color="oauthCardColor || 'primary'">mdi-shield-account</v-icon>
                    <span class="text-body-1 font-weight-bold">{{ t('addChannel.oauthConfig') }}</span>
                    <v-chip v-if="!form.oauthTokens && !isEditing" size="x-small" color="error" variant="tonal">
                      {{ t('addChannel.oauthRequired') }}
                    </v-chip>
                  </div>
                  <v-chip size="small" color="info" variant="tonal"> Codex / ChatGPT Plus </v-chip>
                </v-card-title>

                <v-card-text class="pt-2">
                  <div class="text-body-2 text-medium-emphasis mb-4">
                    {{ t('addChannel.oauthHint') }}
                  </div>

                  <!-- 已配置 OAuth tokens 的显示 -->
                  <v-alert v-if="parsedOAuthInfo" type="success" variant="tonal" class="mb-4" rounded="lg">
                    <div class="d-flex align-center ga-2">
                      <v-icon>mdi-check-circle</v-icon>
                      <div>
                        <div class="font-weight-medium">{{ t('addChannel.oauthConfigured') }}</div>
                        <div class="text-caption">
                          <span v-if="parsedOAuthInfo.email">{{ parsedOAuthInfo.email }} · </span>
                          Account: {{ parsedOAuthInfo.accountId }}
                        </div>
                      </div>
                    </div>
                  </v-alert>

                  <!-- auth.json 输入区域 -->
                  <v-textarea
                    v-model="oauthJsonInput"
                    :label="t('addChannel.oauthJsonLabel')"
                    :placeholder="t('addChannel.oauthJsonPlaceholder')"
                    variant="outlined"
                    rows="8"
                    no-resize
                    @input="parseOAuthJson"
                    :error="!!oauthParseError"
                    :error-messages="oauthParseError"
                    class="oauth-json-textarea"
                  />

                  <div class="text-caption text-medium-emphasis mt-2">
                    {{ t('addChannel.oauthJsonPath') }}: <code>~/.codex/auth.json</code>
                  </div>
                </v-card-text>
              </v-card>
            </v-col>

            <!-- API密钥管理（非 OAuth 和 Composite 类型显示） -->
            <v-col cols="12" v-if="!isOAuthChannel && !isCompositeChannel">
              <v-card variant="outlined" rounded="lg" :color="totalKeyCount === 0 ? 'error' : undefined">
                <v-card-title class="d-flex align-center justify-space-between pa-4 pb-2">
                  <div class="d-flex align-center ga-2">
                    <v-icon :color="totalKeyCount > 0 ? 'primary' : 'error'">mdi-key</v-icon>
                    <span class="text-body-1 font-weight-bold">{{ t('addChannel.apiKeyManagement') }}</span>
                    <v-chip v-if="totalKeyCount === 0" size="x-small" color="error" variant="tonal">
                      {{ t('addChannel.atLeastOneKeyRequired') }}
                    </v-chip>
                  </div>
                  <v-chip size="small" color="info" variant="tonal"> {{ t('addChannel.multiKeyLoadBalance') }} </v-chip>
                </v-card-title>

                <v-card-text class="pt-2">
                  <!-- 编辑模式：显示现有密钥列表（可删除/重排序） -->
                  <div v-if="isEditing && existingMaskedKeys.length > 0" class="mb-4">
                    <div class="text-caption text-medium-emphasis mb-2">
                      {{ t('addChannel.existingKeysLabel', { count: existingMaskedKeys.length }) }}
                    </div>
                    <v-list density="compact" class="bg-transparent">
                      <v-list-item
                        v-for="maskedKey in existingMaskedKeys"
                        :key="maskedKey.index"
                        class="mb-2"
                        rounded="lg"
                        variant="tonal"
                        color="surface-variant"
                      >
                        <template v-slot:prepend>
                          <v-icon size="small" color="primary">mdi-key</v-icon>
                        </template>

                        <v-list-item-title>
                          <code class="text-caption">{{ maskedKey.masked }}</code>
                        </v-list-item-title>

                        <template v-slot:append>
                          <div class="d-flex align-center ga-1">
                            <!-- Move to top (only show for last key when multiple keys exist) -->
                            <v-tooltip
                              v-if="maskedKey.index === existingMaskedKeys.length - 1 && existingMaskedKeys.length > 1"
                              :text="t('addChannel.moveToTop')"
                              location="top"
                              :open-delay="150"
                              content-class="key-tooltip"
                            >
                              <template #activator="{ props: tooltipProps }">
                                <v-btn
                                  v-bind="tooltipProps"
                                  size="small"
                                  color="warning"
                                  icon
                                  variant="text"
                                  rounded="md"
                                  :loading="existingKeyLoading === maskedKey.index"
                                  :disabled="existingKeyLoading !== null"
                                  @click="moveExistingKeyToTop(maskedKey.index)"
                                >
                                  <v-icon size="small">mdi-arrow-up-bold</v-icon>
                                </v-btn>
                              </template>
                            </v-tooltip>
                            <!-- Move to bottom (only show for first key when multiple keys exist) -->
                            <v-tooltip
                              v-if="maskedKey.index === 0 && existingMaskedKeys.length > 1"
                              :text="t('addChannel.moveToBottom')"
                              location="top"
                              :open-delay="150"
                              content-class="key-tooltip"
                            >
                              <template #activator="{ props: tooltipProps }">
                                <v-btn
                                  v-bind="tooltipProps"
                                  size="small"
                                  color="warning"
                                  icon
                                  variant="text"
                                  rounded="md"
                                  :loading="existingKeyLoading === maskedKey.index"
                                  :disabled="existingKeyLoading !== null"
                                  @click="moveExistingKeyToBottom(maskedKey.index)"
                                >
                                  <v-icon size="small">mdi-arrow-down-bold</v-icon>
                                </v-btn>
                              </template>
                            </v-tooltip>
                            <!-- Delete button -->
                            <v-tooltip
                              :text="t('addChannel.deleteKey')"
                              location="top"
                              :open-delay="150"
                              content-class="key-tooltip"
                            >
                              <template #activator="{ props: tooltipProps }">
                                <v-btn
                                  v-bind="tooltipProps"
                                  size="small"
                                  color="error"
                                  icon
                                  variant="text"
                                  :loading="existingKeyLoading === maskedKey.index"
                                  :disabled="existingKeyLoading !== null"
                                  @click="deleteExistingKey(maskedKey.index)"
                                >
                                  <v-icon size="small" color="error">mdi-close</v-icon>
                                </v-btn>
                              </template>
                            </v-tooltip>
                          </div>
                        </template>
                      </v-list-item>
                    </v-list>
                  </div>

                  <!-- 新添加的密钥列表（创建模式显示全部，编辑模式只显示新增的） -->
                  <div v-if="form.apiKeys.length" class="mb-4">
                    <div v-if="isEditing" class="text-caption text-medium-emphasis mb-2">
                      {{ t('addChannel.newKeysToAdd') }}
                    </div>
                    <v-list density="compact" class="bg-transparent">
                      <v-list-item
                        v-for="(key, index) in form.apiKeys"
                        :key="index"
                        class="mb-2"
                        rounded="lg"
                        variant="tonal"
                        :color="duplicateKeyIndex === index ? 'error' : 'surface-variant'"
                        :class="{ 'animate-pulse': duplicateKeyIndex === index }"
                      >
                        <template v-slot:prepend>
                          <v-icon size="small" :color="duplicateKeyIndex === index ? 'error' : 'primary'">
                            {{ duplicateKeyIndex === index ? 'mdi-alert' : 'mdi-key' }}
                          </v-icon>
                        </template>

                        <v-list-item-title>
                          <div class="d-flex align-center justify-space-between">
                            <code class="text-caption">{{ maskApiKey(key) }}</code>
                            <v-chip v-if="duplicateKeyIndex === index" size="x-small" color="error" variant="text">
                              {{ t('addChannel.duplicateKeyBadge') }}
                            </v-chip>
                          </div>
                        </v-list-item-title>

                        <template v-slot:append>
                          <div class="d-flex align-center ga-1">
                            <!-- 置顶/置底：仅首尾密钥显示 -->
                            <v-tooltip
                              v-if="index === form.apiKeys.length - 1 && form.apiKeys.length > 1"
                              :text="t('addChannel.moveToTop')"
                              location="top"
                              :open-delay="150"
                              content-class="key-tooltip"
                            >
                              <template #activator="{ props: tooltipProps }">
                                <v-btn
                                  v-bind="tooltipProps"
                                  size="small"
                                  color="warning"
                                  icon
                                  variant="text"
                                  rounded="md"
                                  @click="moveApiKeyToTop(index)"
                                >
                                  <v-icon size="small">mdi-arrow-up-bold</v-icon>
                                </v-btn>
                              </template>
                            </v-tooltip>
                            <v-tooltip
                              v-if="index === 0 && form.apiKeys.length > 1"
                              :text="t('addChannel.moveToBottom')"
                              location="top"
                              :open-delay="150"
                              content-class="key-tooltip"
                            >
                              <template #activator="{ props: tooltipProps }">
                                <v-btn
                                  v-bind="tooltipProps"
                                  size="small"
                                  color="warning"
                                  icon
                                  variant="text"
                                  rounded="md"
                                  @click="moveApiKeyToBottom(index)"
                                >
                                  <v-icon size="small">mdi-arrow-down-bold</v-icon>
                                </v-btn>
                              </template>
                            </v-tooltip>
                            <v-tooltip
                              :text="copiedKeyIndex === index ? t('common.copied') : t('addChannel.copyKey')"
                              location="top"
                              :open-delay="150"
                              content-class="key-tooltip"
                            >
                              <template #activator="{ props: tooltipProps }">
                                <v-btn
                                  v-bind="tooltipProps"
                                  size="small"
                                  :color="copiedKeyIndex === index ? 'success' : 'primary'"
                                  icon
                                  variant="text"
                                  @click="copyApiKey(key, index)"
                                >
                                  <v-icon size="small">{{
                                    copiedKeyIndex === index ? 'mdi-check' : 'mdi-content-copy'
                                  }}</v-icon>
                                </v-btn>
                              </template>
                            </v-tooltip>
                            <v-tooltip :text="t('addChannel.deleteKey')" location="top" :open-delay="150" content-class="key-tooltip">
                              <template #activator="{ props: tooltipProps }">
                                <v-btn
                                  v-bind="tooltipProps"
                                  size="small"
                                  color="error"
                                  icon
                                  variant="text"
                                  @click="removeApiKey(index)"
                                >
                                  <v-icon size="small" color="error">mdi-close</v-icon>
                                </v-btn>
                              </template>
                            </v-tooltip>
                          </div>
                        </template>
                      </v-list-item>
                    </v-list>
                  </div>

                  <!-- 添加新密钥 -->
                  <div class="d-flex align-start ga-3">
                    <v-text-field
                      v-model="newApiKey"
                      :label="t('addChannel.addNewApiKey')"
                      :placeholder="t('addChannel.enterFullApiKey')"
                      prepend-inner-icon="mdi-plus"
                      variant="outlined"
                      density="comfortable"
                      type="password"
                      @keyup.enter="addApiKey"
                      :error="!!apiKeyError"
                      :error-messages="apiKeyError"
                      @input="handleApiKeyInput"
                      class="flex-grow-1"
                    />
                    <v-btn
                      color="primary"
                      variant="elevated"
                      size="large"
                      height="40"
                      @click="addApiKey"
                      :disabled="!newApiKey.trim()"
                      class="mt-1"
                    >
                      {{ t('common.add') }}
                    </v-btn>
                  </div>
                </v-card-text>
              </v-card>
            </v-col>
          </v-row>
        </v-form>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted, onUnmounted } from 'vue'
import { useTheme } from 'vuetify'
import { useI18n } from 'vue-i18n'
import type { Channel, OAuthTokens, AliasesConfig } from '../services/api'
import { api } from '../services/api'
import CompositeChannelEditor from './CompositeChannelEditor.vue'

// i18n
const { t } = useI18n()

interface Props {
  show: boolean
  channel?: Channel | null
  channelType?: 'messages' | 'responses'
  allChannels?: Channel[]
}

const props = withDefaults(defineProps<Props>(), {
  channelType: 'messages',
  allChannels: () => []
})

const emit = defineEmits<{
  'update:show': [value: boolean]
  save: [channel: Omit<Channel, 'index' | 'latency' | 'status'>, options?: { isQuickAdd?: boolean }]
}>()

// 主题
const theme = useTheme()

// 表单引用
const formRef = ref()

// 模式切换: 快速添加 vs 详细表单
const isQuickMode = ref(true)

// Tab for detailed form mode
const activeTab = ref('config')

// 快速添加模式的数据
const quickInput = ref('')
const detectedBaseUrl = ref('')
const detectedApiKeys = ref<string[]>([])

// 模型别名配置（从后端获取）
const aliasesConfig = ref<AliasesConfig | null>(null)

// 切换模式时，将快速模式检测到的值同步到详细表单，但不清空快速模式输入
const toggleMode = () => {
  if (isQuickMode.value) {
    // 从快速模式切换到详细模式：始终用检测到的值覆盖表单
    if (detectedBaseUrl.value) {
      form.baseUrl = detectedBaseUrl.value
    }
    if (detectedApiKeys.value.length > 0) {
      form.apiKeys = [...detectedApiKeys.value]
    }
    if (generatedChannelName.value) {
      form.name = generatedChannelName.value
    }
    form.serviceType = getDefaultServiceTypeValue()
  }
  // 切换回快速模式时不做任何清理，保留 quickInput 原有内容
  isQuickMode.value = !isQuickMode.value
}

// 检测单个 token 是否为有效的 API Key
const isValidApiKey = (token: string): boolean => {
  // 常见 API Key 前缀格式
  if (/^(sk-|cr_|ms-|key-|api-|AIza)/i.test(token)) {
    return true
  }
  // 长度足够且只包含合法字符的也可能是 key
  if (token.length >= 32 && /^[a-zA-Z0-9_-]+$/.test(token)) {
    return true
  }
  return false
}

// 解析快速输入内容
const parseQuickInput = () => {
  // 统一按换行、空格、逗号、分号分割，然后 trim
  const tokens = quickInput.value
    .split(/[\n\s,;]+/)
    .map(t => t.trim())
    .filter(t => t.length > 0)

  // 重置检测结果
  detectedBaseUrl.value = ''
  detectedApiKeys.value = []

  for (const token of tokens) {
    // 检测 URL (http:// 或 https:// 开头)
    if (/^https?:\/\//i.test(token)) {
      // 只取第一个检测到的 URL
      if (!detectedBaseUrl.value) {
        detectedBaseUrl.value = token.replace(/\/$/, '') // 移除末尾斜杠
      }
      continue
    }

    // 检测 API Key
    if (isValidApiKey(token) && !detectedApiKeys.value.includes(token)) {
      detectedApiKeys.value.push(token)
    }
  }
}

// 获取默认服务类型
const getDefaultServiceType = (): string => {
  if (props.channelType === 'responses') {
    return t('addChannel.serviceTypeResponses')
  }
  return 'Claude'
}

// 获取默认服务类型值
const getDefaultServiceTypeValue = (): 'openai' | 'openai_chat' | 'openaiold' | 'gemini' | 'claude' | 'responses' => {
  if (props.channelType === 'responses') {
    return 'responses'
  }
  return 'claude'
}

// 获取默认 Base URL
const getDefaultBaseUrl = (): string => {
  if (props.channelType === 'responses') {
    return 'https://api.openai.com/v1'
  }
  return 'https://api.anthropic.com'
}

// 快速模式表单验证
const isQuickFormValid = computed(() => {
  return detectedBaseUrl.value.length > 0 && detectedApiKeys.value.length > 0
})

// 生成随机字符串
const generateRandomString = (length: number): string => {
  const chars = 'abcdefghijklmnopqrstuvwxyz0123456789'
  let result = ''
  for (let i = 0; i < length; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length))
  }
  return result
}

// 从 URL 提取二级域名
const extractDomain = (url: string): string => {
  try {
    const hostname = new URL(url).hostname
    // 移除 www. 前缀
    const cleanHost = hostname.replace(/^www\./, '')
    const parts = cleanHost.split('.')

    // 处理特殊情况
    if (parts.length <= 1) {
      // localhost 等单段域名
      return cleanHost
    } else if (parts.length === 2) {
      // example.com → example
      return parts[0]
    } else {
      // api.openai.com → openai (取倒数第二段)
      return parts[parts.length - 2]
    }
  } catch {
    return 'channel'
  }
}

// 随机后缀和生成的渠道名称
const randomSuffix = ref(generateRandomString(6))

const generatedChannelName = computed(() => {
  if (!detectedBaseUrl.value) {
    return `channel-${randomSuffix.value}`
  }
  const domain = extractDomain(detectedBaseUrl.value)
  return `${domain}-${randomSuffix.value}`
})

// 处理快速添加提交
const handleQuickSubmit = () => {
  if (!isQuickFormValid.value) return

  const channelData = {
    name: generatedChannelName.value,
    serviceType: getDefaultServiceTypeValue(),
    baseUrl: detectedBaseUrl.value,
    apiKeys: detectedApiKeys.value,
    modelMapping: {}
  }

  // 传递 isQuickAdd 标志，让 App.vue 知道需要进行后续处理
  emit('save', channelData, { isQuickAdd: true })
}

// 服务类型选项 - 根据渠道类型动态显示
const serviceTypeOptions = computed(() => {
  if (props.channelType === 'responses') {
    return [
      { title: t('addChannel.serviceTypeResponses'), value: 'responses' },
      { title: t('addChannel.serviceTypeOpenAIOAuth'), value: 'openai-oauth' },
      { title: t('addChannel.serviceTypeOpenAI'), value: 'openai' },
      { title: t('addChannel.serviceTypeOpenAIOld'), value: 'openaiold' },
      { title: 'Claude', value: 'claude' },
      { title: 'Gemini', value: 'gemini' }
    ]
  } else {
    return [
      { title: t('addChannel.serviceTypeOpenAI'), value: 'openai' },
      { title: t('addChannel.serviceTypeOpenAIChat'), value: 'openai_chat' },
      { title: t('addChannel.serviceTypeOpenAIOld'), value: 'openaiold' },
      { title: 'Claude', value: 'claude' },
      { title: 'Gemini', value: 'gemini' },
      { title: t('addChannel.serviceTypeResponses'), value: 'responses' },
      { title: t('addChannel.serviceTypeComposite'), value: 'composite' }
    ]
  }
})

// Per-channel API key load balance options
const keyLoadBalanceOptions = computed(() => [
  { title: t('keyLoadBalance.inherit'), value: '', description: t('keyLoadBalance.inheritDesc') },
  { title: t('keyLoadBalance.roundRobin'), value: 'round-robin', description: t('keyLoadBalance.roundRobinDesc') },
  { title: t('keyLoadBalance.random'), value: 'random', description: t('keyLoadBalance.randomDesc') },
  { title: t('keyLoadBalance.failover'), value: 'failover', description: t('keyLoadBalance.failoverDesc') }
])

// 全部源模型选项 - 从配置获取，回退到默认值
const allSourceModelOptions = computed(() => {
  if (props.channelType === 'responses') {
    // Responses API models from config
    if (aliasesConfig.value?.responsesModels?.length) {
      return aliasesConfig.value.responsesModels.map(m => ({
        title: m.description ? `${m.value} (${m.description})` : m.value,
        value: m.value
      }))
    }
    // Fallback defaults
    return [
      { title: 'codex', value: 'codex' },
      { title: 'gpt-5.1-codex-max', value: 'gpt-5.1-codex-max' },
      { title: 'gpt-5.1-codex', value: 'gpt-5.1-codex' },
      { title: 'gpt-5.1-codex-mini', value: 'gpt-5.1-codex-mini' },
      { title: 'gpt-5.1', value: 'gpt-5.1' }
    ]
  } else {
    // Messages API models from config
    if (aliasesConfig.value?.messagesModels?.length) {
      return aliasesConfig.value.messagesModels.map(m => ({
        title: m.description ? `${m.value} (${m.description})` : m.value,
        value: m.value
      }))
    }
    // Fallback defaults
    return [
      { title: 'opus', value: 'opus' },
      { title: 'sonnet', value: 'sonnet' },
      { title: 'haiku', value: 'haiku' }
    ]
  }
})

// 可选的源模型选项 - 过滤掉已配置的模型
const sourceModelOptions = computed(() => {
  const configuredModels = Object.keys(form.modelMapping)
  return allSourceModelOptions.value.filter(opt => !configuredModels.includes(opt.value))
})

// 模型重定向的示例文本 - 根据渠道类型动态显示
const modelMappingHint = computed(() => {
  if (props.channelType === 'responses') {
    return t('addChannel.modelMappingHintResponses')
  } else {
    return t('addChannel.modelMappingHintMessages')
  }
})

const targetModelPlaceholder = computed(() => {
  if (props.channelType === 'responses') {
    return t('addChannel.targetModelPlaceholderResponses')
  } else {
    return t('addChannel.targetModelPlaceholderMessages')
  }
})

// 表单数据
const form = reactive({
  name: '',
  serviceType: '' as 'openai' | 'openai_chat' | 'openaiold' | 'gemini' | 'claude' | 'responses' | 'openai-oauth' | 'composite' | '',
  baseUrl: '',
  website: '',
  insecureSkipVerify: false,
  responseHeaderTimeout: undefined as number | undefined,
  description: '',
  apiKeys: [] as string[],
  modelMapping: {} as Record<string, string>,
  priceMultipliers: {} as Record<string, { inputMultiplier?: number; outputMultiplier?: number; cacheCreationMultiplier?: number; cacheReadMultiplier?: number }>,
  oauthTokens: undefined as OAuthTokens | undefined,
  // Composite channel mappings
  compositeMappings: [] as Array<{ pattern: string; targetChannelId: string; failoverChain?: string[]; targetModel?: string }>,
  // Quota settings
  quotaType: '' as '' | 'requests' | 'credit',
  quotaLimit: undefined as number | undefined,
  quotaResetAt: undefined as string | undefined,
  quotaResetInterval: undefined as number | undefined,
  quotaResetUnit: 'days' as 'hours' | 'days' | 'weeks' | 'months',
  quotaModels: [] as string[],
  quotaResetMode: 'fixed' as 'fixed' | 'rolling',
  // Per-channel rate limiting
  rateLimitRpm: undefined as number | undefined,
  queueEnabled: false,
  queueTimeout: 60,
  // Per-channel API key load balance strategy
  keyLoadBalance: '' as '' | 'round-robin' | 'random' | 'failover'
})

// OAuth 相关状态
const oauthJsonInput = ref('')
const oauthParseError = ref('')
const parsedOAuthInfo = ref<{ email?: string; accountId?: string } | null>(null)

// 解析 OAuth auth.json 内容
const parseOAuthJson = () => {
  oauthParseError.value = ''

  // 如果输入为空，保留已有的 OAuth tokens（编辑模式下不清除）
  if (!oauthJsonInput.value.trim()) {
    // 不清除 form.oauthTokens 和 parsedOAuthInfo，保留编辑前的值
    return
  }

  // 只有当用户输入新内容时才清除并重新解析
  parsedOAuthInfo.value = null
  form.oauthTokens = undefined

  try {
    const parsed = JSON.parse(oauthJsonInput.value.trim())

    // 验证必需字段
    if (!parsed.tokens?.access_token) {
      oauthParseError.value = t('addChannel.oauthMissingAccessToken')
      return
    }
    if (!parsed.tokens?.account_id) {
      oauthParseError.value = t('addChannel.oauthMissingAccountId')
      return
    }
    if (!parsed.tokens?.refresh_token) {
      oauthParseError.value = t('addChannel.oauthMissingRefreshToken')
      return
    }

    // 尝试从 JWT 中提取 email（可选）
    let email: string | undefined
    try {
      const idToken = parsed.tokens.id_token
      if (idToken) {
        const parts = idToken.split('.')
        if (parts.length === 3) {
          const payload = JSON.parse(atob(parts[1]))
          email = payload.email
        }
      }
    } catch {
      // 忽略 JWT 解析错误
    }

    // 设置解析结果
    form.oauthTokens = {
      access_token: parsed.tokens.access_token,
      account_id: parsed.tokens.account_id,
      id_token: parsed.tokens.id_token,
      refresh_token: parsed.tokens.refresh_token,
      last_refresh: parsed.last_refresh
    }

    parsedOAuthInfo.value = {
      email,
      accountId: parsed.tokens.account_id.substring(0, 12) + '...'
    }
  } catch {
    oauthParseError.value = t('addChannel.oauthInvalidJson')
  }
}

// 检查是否为 OAuth 渠道类型
const isOAuthChannel = computed(() => form.serviceType === 'openai-oauth')

// 检查是否为 Composite 渠道类型
const isCompositeChannel = computed(() => form.serviceType === 'composite')

// Key for CompositeChannelEditor to force re-creation on channel change
// This ensures the component reinitializes when editing a different channel or after reopening
const compositeEditorReopenCount = ref(0)
const compositeEditorKey = computed(() => {
  const channelId = props.channel?.id || props.channel?.index || 'new'
  return `${channelId}-${compositeEditorReopenCount.value}`
})

// OAuth 卡片颜色：编辑模式下如果已有 tokens 则显示 success，新建模式下无 tokens 显示 error
const oauthCardColor = computed(() => {
  if (form.oauthTokens) {
    return 'success'
  }
  // 编辑模式下，如果没有 tokens（不应该发生），显示默认颜色而非 error
  if (isEditing.value) {
    return undefined
  }
  // 新建模式下，没有 tokens 显示 error
  return 'error'
})

// 原始密钥映射 (掩码密钥 -> 原始密钥)
const originalKeyMap = ref<Map<string, string>>(new Map())

// Existing key count (for edit mode - keys are write-only)
const existingKeyCount = ref(0)

// Existing masked keys (for edit mode - allows deletion/reordering)
const existingMaskedKeys = ref<Array<{ index: number; masked: string }>>([])

// Loading state for existing key operations
const existingKeyLoading = ref<number | null>(null)

// 新API密钥输入
const newApiKey = ref('')

// 密钥重复检测状态
const apiKeyError = ref('')
const duplicateKeyIndex = ref(-1)

// 处理 API 密钥输入事件
const handleApiKeyInput = () => {
  apiKeyError.value = ''
  duplicateKeyIndex.value = -1
}

// 复制功能相关状态
const copiedKeyIndex = ref<number | null>(null)

// 新模型映射输入
const newMapping = reactive({
  source: '',
  target: ''
})

// 新价格乘数输入
const newMultiplier = reactive({
  modelKey: '',
  inputMultiplier: undefined as number | undefined,
  outputMultiplier: undefined as number | undefined,
  cacheCreationMultiplier: undefined as number | undefined,
  cacheReadMultiplier: undefined as number | undefined
})

// Composite mapping input
const newCompositeMapping = reactive({
  pattern: '',
  targetChannelId: '',
  targetModel: ''
})

// Composite mapping error state
const compositeMappingError = ref('')

// Normalize composite mappings (trim, ensure wildcard last)
const normalizeCompositeMappings = (
  mappings: Array<{ pattern: string; targetChannelId: string; failoverChain?: string[]; targetModel?: string }>
) => {
  const normalized = mappings.map(m => ({
    pattern: m.pattern.trim(),
    targetChannelId: m.targetChannelId,
    failoverChain: m.failoverChain || [],
    targetModel: m.targetModel?.trim() || undefined
  }))

  // No longer need wildcard reordering - wildcard is not allowed
  return normalized
}

// Claude-compatible service types
const claudeCompatibleTypes = ['claude', 'openai_chat', 'openai', 'gemini', 'openaiold']

// Validate composite mappings
const validateCompositeMappings = (
  mappings: Array<{ pattern: string; targetChannelId: string; failoverChain?: string[]; targetModel?: string }>
): string => {
  // Required patterns - all 3 must be present
  const requiredPatterns = ['haiku', 'sonnet', 'opus']
  const seen = new Set<string>()

  for (let i = 0; i < mappings.length; i++) {
    const mapping = mappings[i]
    const pattern = mapping.pattern.trim()

    if (!pattern) return t('addChannel.compositeEmptyPattern')
    if (pattern === '*') return t('addChannel.compositeWildcardMultiple') // Wildcard not allowed
    if (!requiredPatterns.includes(pattern)) {
      return t('addChannel.compositeDuplicatePattern', { pattern }) // Invalid pattern
    }
    if (seen.has(pattern)) return t('addChannel.compositeDuplicatePattern', { pattern })
    seen.add(pattern)

    // Validate primary target
    const targetChannelId = mapping.targetChannelId
    if (!targetChannelId) return t('addChannel.compositeTargetRequired')

    const target = props.allChannels.find(ch => ch.id === targetChannelId)
    if (!target) return t('addChannel.compositeMissingTargetChannel')
    if (!claudeCompatibleTypes.includes(target.serviceType)) {
      return t('addChannel.compositeInvalidServiceType', { channel: target.name })
    }

    // Validate failover chain - at least 1 required
    const failoverChain = mapping.failoverChain || []
    if (failoverChain.length === 0) {
      return t('addChannel.compositeMinFailover', { pattern })
    }

    // Validate each failover channel
    for (const failoverId of failoverChain) {
      const failoverChannel = props.allChannels.find(ch => ch.id === failoverId)
      if (!failoverChannel) return t('addChannel.compositeMissingTargetChannel')
      if (!claudeCompatibleTypes.includes(failoverChannel.serviceType)) {
        return t('addChannel.compositeInvalidServiceType', { channel: failoverChannel.name })
      }
    }
  }

  // Check all required patterns are present
  for (const pattern of requiredPatterns) {
    if (!seen.has(pattern)) {
      return t('addChannel.compositePatternRequired', { pattern })
    }
  }

  return ''
}

// Computed validation error for real-time feedback
const compositeMappingsValidationError = computed(() => {
  if (form.serviceType !== 'composite') return ''
  return validateCompositeMappings(form.compositeMappings)
})

// Clear error when inputs change
watch(
  () => [newCompositeMapping.pattern, newCompositeMapping.targetChannelId, newCompositeMapping.targetModel],
  () => {
    compositeMappingError.value = ''
  }
)

// Available Claude channels for composite mapping
const availableClaudeChannels = computed(() => {
  return props.allChannels
    .filter(ch => !!ch.id && ch.serviceType === 'claude' && ch.status !== 'disabled')
    .map(ch => ({
      id: ch.id as string,
      name: ch.name,
      index: ch.index
    }))
})

// Get channel name by ID
const getTargetChannelName = (channelId: string): string => {
  const channel = props.allChannels.find(ch => ch.id === channelId)
  return channel?.name || channelId
}

// 表单验证错误
const errors = reactive({
  name: '',
  serviceType: '',
  baseUrl: '',
  website: ''
})

// 验证规则
const rules = {
  required: (value: string) => !!value || t('addChannel.requiredField'),
  url: (value: string) => {
    try {
      new URL(value)
      return true
    } catch {
      return t('addChannel.invalidUrl')
    }
  },
  urlOptional: (value: string) => {
    if (!value) return true
    try {
      new URL(value)
      return true
    } catch {
      return t('addChannel.invalidUrl')
    }
  }
}

// 计算属性
const isEditing = computed(() => !!props.channel)

// Total key count: existing keys (edit mode) + newly added keys
const totalKeyCount = computed(() => {
  return existingKeyCount.value + form.apiKeys.length
})

// 动态header样式
const headerClasses = computed(() => {
  const isDark = theme.global.current.value.dark
  // Dark: keep neutral surface header; Light: use brand primary header
  return isDark ? 'bg-surface text-high-emphasis' : 'bg-primary text-white'
})

const avatarColor = computed(() => 'primary')

// Use Vuetify theme "on-primary" token so icon isn't fixed white
const headerIconStyle = computed(() => ({
  color: 'rgb(var(--v-theme-on-primary))'
}))

const subtitleClasses = computed(() => {
  const isDark = theme.global.current.value.dark
  // Dark mode: use medium emphasis; Light mode: use white with opacity for primary bg
  return isDark ? 'text-medium-emphasis' : 'text-white-subtitle'
})

const isFormValid = computed(() => {
  // OAuth 渠道需要 OAuth tokens 而非 API keys，且不需要用户输入 base URL
  if (form.serviceType === 'openai-oauth') {
    // 编辑模式下，不需要重新输入 OAuth tokens（后端会保留现有 tokens）
    // 新建模式下，必须提供 OAuth tokens
    if (isEditing.value) {
      // 编辑模式：只需要名称和服务类型
      return form.name.trim() && form.serviceType
    }
    // 新建模式：必须提供 OAuth tokens
    return (
      form.name.trim() &&
      form.serviceType &&
      form.oauthTokens !== undefined
    )
  }
  // Composite 渠道需要至少一个映射
  if (form.serviceType === 'composite') {
    return (
      form.name.trim() &&
      form.serviceType &&
      form.compositeMappings.length > 0 &&
      !compositeMappingsValidationError.value
    )
  }
  // For regular channels: need at least one key (existing or new)
  return (
    form.name.trim() && form.serviceType && form.baseUrl.trim() && isValidUrl(form.baseUrl) && totalKeyCount.value > 0
  )
})

// 工具函数
const isValidUrl = (url: string): boolean => {
  try {
    new URL(url)
    return true
  } catch {
    return false
  }
}

const getUrlHint = (): string => {
  const hints: Record<string, string> = {
    responses: t('addChannel.urlHintOpenAI'),
    openai: t('addChannel.urlHintOpenAI'),
    openai_chat: t('addChannel.urlHintOpenAI'),
    openaiold: t('addChannel.urlHintOpenAI'),
    claude: t('addChannel.urlHintClaude'),
    gemini: t('addChannel.urlHintGemini')
  }
  return hints[form.serviceType] || t('addChannel.urlHintDefault')
}

const maskApiKey = (key: string): string => {
  if (key.length <= 10) return key.slice(0, 3) + '***' + key.slice(-2)
  return key.slice(0, 8) + '***' + key.slice(-5)
}

// 表单操作
const resetForm = () => {
  form.name = ''
  form.serviceType = ''
  form.baseUrl = ''
  form.website = ''
  form.insecureSkipVerify = false
  form.responseHeaderTimeout = undefined
  form.description = ''
  form.apiKeys = []
  form.modelMapping = {}
  form.priceMultipliers = {}
  form.oauthTokens = undefined
  form.compositeMappings = []
  // Reset quota settings
  form.quotaType = ''
  form.quotaLimit = undefined
  form.quotaResetAt = undefined
  form.quotaResetInterval = undefined
  form.quotaResetUnit = 'days'
  form.quotaModels = []
  form.quotaResetMode = 'fixed'
  // Reset rate limit settings
  form.rateLimitRpm = undefined
  form.queueEnabled = false
  form.queueTimeout = 60
  // Reset key load balance
  form.keyLoadBalance = ''
  newApiKey.value = ''
  newMapping.source = ''
  newMapping.target = ''
  newMultiplier.modelKey = ''
  newMultiplier.inputMultiplier = undefined
  newMultiplier.outputMultiplier = undefined
  newMultiplier.cacheCreationMultiplier = undefined
  newMultiplier.cacheReadMultiplier = undefined

  // 清空原始密钥映射
  originalKeyMap.value.clear()

  // Reset existing key count (for edit mode)
  existingKeyCount.value = 0

  // Reset existing masked keys
  existingMaskedKeys.value = []
  existingKeyLoading.value = null

  // 清空密钥错误状态
  apiKeyError.value = ''
  duplicateKeyIndex.value = -1

  // 清除错误信息
  errors.name = ''
  errors.serviceType = ''
  errors.baseUrl = ''

  // 重置快速添加模式数据
  quickInput.value = ''
  detectedBaseUrl.value = ''
  detectedApiKeys.value = []
  randomSuffix.value = generateRandomString(6)

  // 重置 OAuth 状态
  oauthJsonInput.value = ''
  oauthParseError.value = ''
  parsedOAuthInfo.value = null

  // Reset composite mapping input
  newCompositeMapping.pattern = ''
  newCompositeMapping.targetChannelId = ''
  newCompositeMapping.targetModel = ''
}

const loadChannelData = (channel: Channel) => {
  form.name = channel.name
  form.serviceType = channel.serviceType
  form.baseUrl = channel.baseUrl
  form.website = channel.website || ''
  form.insecureSkipVerify = !!channel.insecureSkipVerify
  form.responseHeaderTimeout = channel.responseHeaderTimeout
  form.description = channel.description || ''

  // In edit mode, we don't receive actual API keys from the server (security)
  // Only new keys added during this edit session will be stored here
  form.apiKeys = []

  // Store the existing key count for display
  existingKeyCount.value = channel.apiKeyCount || 0

  // Store masked keys for display and deletion
  existingMaskedKeys.value = channel.maskedKeys || []

  // Clear the original key map (not needed anymore)
  originalKeyMap.value.clear()

  form.modelMapping = { ...(channel.modelMapping || {}) }
  form.priceMultipliers = { ...(channel.priceMultipliers || {}) }

  // Load composite mappings
  form.compositeMappings = channel.compositeMappings?.map(m => ({
    pattern: m.pattern,
    targetChannelId: m.targetChannelId || '',
    failoverChain: m.failoverChain || [],
    targetModel: m.targetModel
  })) || []

  // Load quota settings
  form.quotaType = channel.quotaType || ''
  form.quotaLimit = channel.quotaLimit
  // Convert ISO datetime to datetime-local format (YYYY-MM-DDTHH:mm) in local timezone
  if (channel.quotaResetAt) {
    const date = new Date(channel.quotaResetAt)
    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')
    form.quotaResetAt = `${year}-${month}-${day}T${hours}:${minutes}`
  } else {
    form.quotaResetAt = undefined
  }
  form.quotaResetInterval = channel.quotaResetInterval
  form.quotaResetUnit = channel.quotaResetUnit || 'days'
  form.quotaModels = channel.quotaModels || []
  form.quotaResetMode = channel.quotaResetMode || 'fixed'

  // Load rate limit settings
  form.rateLimitRpm = typeof channel.rateLimitRpm === 'number' ? channel.rateLimitRpm : undefined
  form.queueEnabled = channel.queueEnabled || false
  form.queueTimeout = channel.queueTimeout || 60

  // Load key load balance setting
  form.keyLoadBalance = channel.keyLoadBalance || ''

  // 加载 OAuth tokens（如果存在）
  if (channel.oauthTokens) {
    form.oauthTokens = { ...channel.oauthTokens }
    // 尝试从 JWT 中提取 email
    let email: string | undefined
    try {
      const idToken = channel.oauthTokens.id_token
      if (idToken) {
        const parts = idToken.split('.')
        if (parts.length === 3) {
          const payload = JSON.parse(atob(parts[1]))
          email = payload.email
        }
      }
    } catch {
      // 忽略 JWT 解析错误
    }
    parsedOAuthInfo.value = {
      email,
      accountId: channel.oauthTokens.account_id.substring(0, 12) + '...'
    }
  } else {
    form.oauthTokens = undefined
    parsedOAuthInfo.value = null
  }
  oauthJsonInput.value = ''
  oauthParseError.value = ''
}

const addApiKey = () => {
  const key = newApiKey.value.trim()
  if (!key) return

  // 重置错误状态
  apiKeyError.value = ''
  duplicateKeyIndex.value = -1

  // 检查是否与现有密钥重复
  const duplicateIndex = findDuplicateKeyIndex(key)
  if (duplicateIndex !== -1) {
    apiKeyError.value = t('addChannel.duplicateKey')
    duplicateKeyIndex.value = duplicateIndex
    // 清除输入框，让用户重新输入
    newApiKey.value = ''
    return
  }

  // 直接存储原始密钥
  form.apiKeys.push(key)
  newApiKey.value = ''
}

// 检查密钥是否重复，返回重复密钥的索引，如果没有重复返回-1
const findDuplicateKeyIndex = (newKey: string): number => {
  return form.apiKeys.findIndex(existingKey => existingKey === newKey)
}

const removeApiKey = (index: number) => {
  form.apiKeys.splice(index, 1)

  // 如果删除的是当前高亮的重复密钥，清除高亮状态
  if (duplicateKeyIndex.value === index) {
    duplicateKeyIndex.value = -1
    apiKeyError.value = ''
  } else if (duplicateKeyIndex.value > index) {
    // 如果删除的密钥在高亮密钥之前，调整高亮索引
    duplicateKeyIndex.value--
  }
}

// 将指定密钥移到最上方
const moveApiKeyToTop = (index: number) => {
  if (index <= 0 || index >= form.apiKeys.length) return
  const [key] = form.apiKeys.splice(index, 1)
  form.apiKeys.unshift(key)
  duplicateKeyIndex.value = -1
  copiedKeyIndex.value = null
}

// 将指定密钥移到最下方
const moveApiKeyToBottom = (index: number) => {
  if (index < 0 || index >= form.apiKeys.length - 1) return
  const [key] = form.apiKeys.splice(index, 1)
  form.apiKeys.push(key)
  duplicateKeyIndex.value = -1
  copiedKeyIndex.value = null
}

// 复制API密钥到剪贴板
const copyApiKey = async (key: string, index: number) => {
  try {
    await navigator.clipboard.writeText(key)
    copiedKeyIndex.value = index

    // 2秒后重置复制状态
    setTimeout(() => {
      copiedKeyIndex.value = null
    }, 2000)
  } catch (err) {
    console.error('复制密钥失败:', err)
    // 降级方案：使用传统的复制方法
    const textArea = document.createElement('textarea')
    textArea.value = key
    textArea.style.position = 'fixed'
    textArea.style.left = '-999999px'
    textArea.style.top = '-999999px'
    document.body.appendChild(textArea)
    textArea.focus()
    textArea.select()

    try {
      document.execCommand('copy')
      copiedKeyIndex.value = index

      setTimeout(() => {
        copiedKeyIndex.value = null
      }, 2000)
    } catch (err) {
      console.error('降级复制方案也失败:', err)
    } finally {
      textArea.remove()
    }
  }
}

// ============== Existing Key Operations (Edit Mode) ==============

// Delete an existing key by index
const deleteExistingKey = async (keyIndex: number) => {
  if (!props.channel) return
  existingKeyLoading.value = keyIndex

  try {
    if (props.channelType === 'responses') {
      await api.removeResponsesApiKeyByIndex(props.channel.index, keyIndex)
    } else {
      await api.removeApiKeyByIndex(props.channel.index, keyIndex)
    }
    // Remove from local state
    existingMaskedKeys.value = existingMaskedKeys.value.filter(k => k.index !== keyIndex)
    // Re-index remaining keys
    existingMaskedKeys.value = existingMaskedKeys.value.map((k, i) => ({ ...k, index: i }))
    existingKeyCount.value = existingMaskedKeys.value.length
  } catch (err) {
    console.error('Failed to delete existing key:', err)
  } finally {
    existingKeyLoading.value = null
  }
}

// Move an existing key to top
const moveExistingKeyToTop = async (keyIndex: number) => {
  if (!props.channel || keyIndex === 0) return
  existingKeyLoading.value = keyIndex

  try {
    if (props.channelType === 'responses') {
      await api.moveResponsesApiKeyToTopByIndex(props.channel.index, keyIndex)
    } else {
      await api.moveApiKeyToTopByIndex(props.channel.index, keyIndex)
    }
    // Reorder local state
    const keyToMove = existingMaskedKeys.value.find(k => k.index === keyIndex)
    if (keyToMove) {
      existingMaskedKeys.value = existingMaskedKeys.value.filter(k => k.index !== keyIndex)
      existingMaskedKeys.value.unshift(keyToMove)
      // Re-index
      existingMaskedKeys.value = existingMaskedKeys.value.map((k, i) => ({ ...k, index: i }))
    }
  } catch (err) {
    console.error('Failed to move key to top:', err)
  } finally {
    existingKeyLoading.value = null
  }
}

// Move an existing key to bottom
const moveExistingKeyToBottom = async (keyIndex: number) => {
  if (!props.channel || keyIndex === existingMaskedKeys.value.length - 1) return
  existingKeyLoading.value = keyIndex

  try {
    if (props.channelType === 'responses') {
      await api.moveResponsesApiKeyToBottomByIndex(props.channel.index, keyIndex)
    } else {
      await api.moveApiKeyToBottomByIndex(props.channel.index, keyIndex)
    }
    // Reorder local state
    const keyToMove = existingMaskedKeys.value.find(k => k.index === keyIndex)
    if (keyToMove) {
      existingMaskedKeys.value = existingMaskedKeys.value.filter(k => k.index !== keyIndex)
      existingMaskedKeys.value.push(keyToMove)
      // Re-index
      existingMaskedKeys.value = existingMaskedKeys.value.map((k, i) => ({ ...k, index: i }))
    }
  } catch (err) {
    console.error('Failed to move key to bottom:', err)
  } finally {
    existingKeyLoading.value = null
  }
}

const addModelMapping = () => {
  const source = newMapping.source.trim()
  const target = newMapping.target.trim()
  if (source && target && !form.modelMapping[source]) {
    form.modelMapping[source] = target
    newMapping.source = ''
    newMapping.target = ''
  }
}

const removeModelMapping = (source: string) => {
  delete form.modelMapping[source]
}

// Price multiplier methods
const hasNonDefaultMultiplier = (mult: { inputMultiplier?: number; outputMultiplier?: number; cacheCreationMultiplier?: number; cacheReadMultiplier?: number }) => {
  return (mult.inputMultiplier !== undefined && mult.inputMultiplier !== 1) ||
         (mult.outputMultiplier !== undefined && mult.outputMultiplier !== 1) ||
         (mult.cacheCreationMultiplier !== undefined && mult.cacheCreationMultiplier !== 1) ||
         (mult.cacheReadMultiplier !== undefined && mult.cacheReadMultiplier !== 1)
}

const addPriceMultiplier = () => {
  const modelKey = newMultiplier.modelKey.trim()
  if (!modelKey || form.priceMultipliers[modelKey]) return

  const mult: { inputMultiplier?: number; outputMultiplier?: number; cacheCreationMultiplier?: number; cacheReadMultiplier?: number } = {}

  // Only include non-default values (undefined or 1.0 means default)
  if (newMultiplier.inputMultiplier !== undefined && newMultiplier.inputMultiplier !== 1) {
    mult.inputMultiplier = newMultiplier.inputMultiplier
  }
  if (newMultiplier.outputMultiplier !== undefined && newMultiplier.outputMultiplier !== 1) {
    mult.outputMultiplier = newMultiplier.outputMultiplier
  }
  if (newMultiplier.cacheCreationMultiplier !== undefined && newMultiplier.cacheCreationMultiplier !== 1) {
    mult.cacheCreationMultiplier = newMultiplier.cacheCreationMultiplier
  }
  if (newMultiplier.cacheReadMultiplier !== undefined && newMultiplier.cacheReadMultiplier !== 1) {
    mult.cacheReadMultiplier = newMultiplier.cacheReadMultiplier
  }

  form.priceMultipliers[modelKey] = mult

  // Reset form
  newMultiplier.modelKey = ''
  newMultiplier.inputMultiplier = undefined
  newMultiplier.outputMultiplier = undefined
  newMultiplier.cacheCreationMultiplier = undefined
  newMultiplier.cacheReadMultiplier = undefined
}

const removePriceMultiplier = (modelKey: string) => {
  delete form.priceMultipliers[modelKey]
}

// Composite mapping functions
const addCompositeMapping = () => {
  compositeMappingError.value = ''

  const pattern = newCompositeMapping.pattern.trim()
  const targetChannelId = newCompositeMapping.targetChannelId
  const targetModel = newCompositeMapping.targetModel.trim()

  if (!pattern || !targetChannelId) return

  const mappingToAdd = {
    pattern,
    targetChannelId,
    targetModel: targetModel || undefined
  }

  // If wildcard exists, keep it last by inserting before it
  const wildcardIndex = form.compositeMappings.findIndex(m => m.pattern.trim() === '*')
  const nextMappings = [...form.compositeMappings]
  if (pattern !== '*' && wildcardIndex !== -1) {
    nextMappings.splice(wildcardIndex, 0, mappingToAdd)
  } else {
    nextMappings.push(mappingToAdd)
  }

  const normalized = normalizeCompositeMappings(nextMappings)
  const validationError = validateCompositeMappings(normalized)
  if (validationError) {
    compositeMappingError.value = validationError
    return
  }

  form.compositeMappings.splice(0, form.compositeMappings.length, ...normalized)

  // Reset form
  newCompositeMapping.pattern = ''
  newCompositeMapping.targetChannelId = ''
  newCompositeMapping.targetModel = ''
}

const removeCompositeMapping = (index: number) => {
  compositeMappingError.value = ''
  form.compositeMappings.splice(index, 1)
}

const handleSubmit = async () => {
  if (!formRef.value) return

  const { valid } = await formRef.value.validate()
  if (!valid) return

  compositeMappingError.value = ''

  const serviceType = form.serviceType as Channel['serviceType']
  const isOAuth = serviceType === 'openai-oauth'
  const isComposite = serviceType === 'composite'

  // Composite/OAuth channels should not send user-entered API keys
  const processedApiKeys = isOAuth || isComposite ? [] : form.apiKeys.filter(key => key.trim())

  // OAuth uses fixed endpoint; composite must have empty baseUrl (backend invariant)
  const baseUrl = isOAuth
    ? 'https://chatgpt.com/backend-api/codex/responses'
    : isComposite
      ? ''
      : form.baseUrl.trim().replace(/\/$/, '') // 移除末尾斜杠

  // 类型断言，因为表单验证已经确保serviceType不为空
  const channelData: Record<string, unknown> = {
    name: form.name.trim(),
    serviceType,
    baseUrl,
    website: form.website.trim() || undefined,
    insecureSkipVerify: form.insecureSkipVerify || undefined,
    responseHeaderTimeout: form.responseHeaderTimeout || undefined,
    description: form.description.trim(),
    apiKeys: processedApiKeys,
    modelMapping: form.modelMapping,
    // 始终发送 priceMultipliers，即使为空对象（用于清除已有配置）
    priceMultipliers: form.priceMultipliers,
    // Quota settings - always send quotaType (empty string clears quota)
    // Note: Use validNumber helper to convert NaN to undefined (v-model.number returns NaN for empty inputs)
    quotaType: form.quotaType,
    quotaLimit: form.quotaType && typeof form.quotaLimit === 'number' && !Number.isNaN(form.quotaLimit) ? form.quotaLimit : undefined,
    quotaResetAt: form.quotaType && form.quotaResetAt ? new Date(form.quotaResetAt).toISOString() : undefined,
    quotaResetInterval: form.quotaType && typeof form.quotaResetInterval === 'number' && !Number.isNaN(form.quotaResetInterval) ? form.quotaResetInterval : undefined,
    quotaResetUnit: form.quotaType ? form.quotaResetUnit : undefined,
    quotaModels: form.quotaType && form.quotaModels.length > 0 ? form.quotaModels : undefined,
    quotaResetMode: form.quotaType ? form.quotaResetMode : undefined,
    // Per-channel rate limiting
    rateLimitRpm: typeof form.rateLimitRpm === 'number' && !Number.isNaN(form.rateLimitRpm) && form.rateLimitRpm >= 0 ? Math.floor(form.rateLimitRpm) : undefined,
    queueEnabled: typeof form.rateLimitRpm === 'number' && form.rateLimitRpm > 0 ? form.queueEnabled : undefined,
    queueTimeout: typeof form.rateLimitRpm === 'number' && form.rateLimitRpm > 0 && form.queueEnabled ? (typeof form.queueTimeout === 'number' && !Number.isNaN(form.queueTimeout) && form.queueTimeout > 0 ? Math.floor(form.queueTimeout) : 60) : undefined,
    // Per-channel API key load balance strategy
    keyLoadBalance: form.keyLoadBalance || undefined
  }

  // 对于 OAuth 渠道，添加 OAuth tokens
  if (isOAuth && form.oauthTokens) {
    channelData.oauthTokens = form.oauthTokens
  }

  // 对于 Composite 渠道，添加 compositeMappings
  if (isComposite) {
    const normalized = normalizeCompositeMappings(form.compositeMappings)
    const validationError = validateCompositeMappings(normalized)
    if (validationError) {
      compositeMappingError.value = validationError
      return
    }

    channelData.compositeMappings = normalized.map(m => ({
      pattern: m.pattern,
      targetChannelId: m.targetChannelId,
      failoverChain: m.failoverChain || [],
      targetModel: m.targetModel || undefined
    }))
  }

  emit('save', channelData as Omit<Channel, 'index' | 'latency' | 'status'>)
}

const handleCancel = () => {
  emit('update:show', false)
  resetForm()
}

// 监听props变化
watch(
  () => props.show,
  newShow => {
    if (newShow) {
      // Reset tab to config when dialog opens
      activeTab.value = 'config'
      // Increment key to force CompositeChannelEditor re-creation
      compositeEditorReopenCount.value++
      // Refresh model aliases config (only if authenticated)
      if (api.getApiKey()) {
        api.getModelAliases().then(config => {
          aliasesConfig.value = config
        }).catch(err => {
          console.warn('Failed to load model aliases config:', err)
        })
      }
      if (props.channel) {
        // 编辑模式：使用表单模式
        isQuickMode.value = false
        loadChannelData(props.channel)
      } else {
        // 添加模式：默认使用快速模式
        isQuickMode.value = true
        resetForm()
      }
    }
  }
)

watch(
  () => props.channel,
  newChannel => {
    if (newChannel && props.show) {
      loadChannelData(newChannel)
    }
  }
)

// ESC键监听
const handleKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && props.show) {
    handleCancel()
  }
}

onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown)
})
</script>

<style scoped>
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
}

/* iOS-style action buttons */
.modal-action-btn {
  width: 36px;
  height: 36px;
}

/* 浅色模式下副标题使用白色带透明度 */
.text-white-subtitle {
  color: rgba(255, 255, 255, 0.85) !important;
}

/* Action buttons on primary background */
.bg-primary .modal-action-btn {
  color: white !important;
}

.bg-primary .modal-action-btn:hover {
  background-color: rgba(255, 255, 255, 0.15) !important;
}

.animate-pulse {
  animation: pulse 1.5s ease-in-out infinite;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.7;
  }
}

:deep(.key-tooltip) {
  color: rgba(var(--v-theme-on-surface), 0.92);
  background-color: rgba(var(--v-theme-surface), 0.98);
  border: 1px solid rgba(var(--v-theme-primary), 0.45);
  font-weight: 600;
  letter-spacing: 0.2px;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.06);
}

/* 快速添加模式样式 */
.quick-input-textarea :deep(textarea) {
  font-family: 'SF Mono', Monaco, 'Cascadia Code', monospace;
  font-size: 13px;
  line-height: 1.6;
}

/* OAuth JSON 输入样式 */
.oauth-json-textarea :deep(textarea) {
  font-family: 'SF Mono', Monaco, 'Cascadia Code', monospace;
  font-size: 12px;
  line-height: 1.5;
}

.detection-status-card {
  background: rgba(var(--v-theme-surface-variant), 0.3);
}

.mode-toggle-btn {
  text-transform: none;
}

/* 亮色模式下按钮在 primary 背景上显示白色 */
.bg-primary .mode-toggle-btn {
  color: white !important;
  border-color: rgba(255, 255, 255, 0.7) !important;
}

.bg-primary .mode-toggle-btn:hover {
  background-color: rgba(255, 255, 255, 0.15) !important;
  border-color: white !important;
}
</style>
