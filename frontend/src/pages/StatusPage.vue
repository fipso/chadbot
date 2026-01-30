<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import * as api from '../services/api'
import { ArrowLeft, Refresh, Connection, Tools, Setting, Download, Upload, Plus, Check } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

interface SkillParam {
  name: string
  type: string
  description: string
  required: boolean
}

interface Skill {
  name: string
  description: string
  plugin_id: string
  plugin_name: string
  parameters: SkillParam[]
}

interface ConfigField {
  key: string
  label: string
  description: string
  type: 'bool' | 'string' | 'number' | 'string_array'
  default_value: string
}

interface PluginConfig {
  schema: ConfigField[]
  values: Record<string, string>
}

interface Plugin {
  id: string
  name: string
  version: string
  description: string
  subscribed: string[]
  config?: PluginConfig
}

interface StatusData {
  plugins: Plugin[]
  skills: Skill[]
}

const router = useRouter()
const status = ref<StatusData | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const savingConfig = ref<string | null>(null)
const savedConfig = ref<string | null>(null)
const newArrayItem = ref<Record<string, string>>({})
const fileInputRef = ref<HTMLInputElement | null>(null)

// Debounce timers
// Track local input values, pending values and debounce timers (non-reactive to avoid re-renders)
const localInputValues: Record<string, string> = {}
const debounceTimers: Record<string, ReturnType<typeof setTimeout>> = {}

async function loadStatus() {
  loading.value = true
  error.value = null
  try {
    status.value = await api.fetchStatus()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load status'
  } finally {
    loading.value = false
  }
}

async function saveConfig(pluginName: string, key: string, value: string) {
  const configKey = `${pluginName}:${key}`
  try {
    await api.setPluginConfig(pluginName, key, value)
    // Update local state after successful save
    if (status.value) {
      const plugin = status.value.plugins.find(p => p.name === pluginName)
      if (plugin?.config) {
        plugin.config.values[key] = value
      }
    }
    savedConfig.value = configKey
    setTimeout(() => {
      if (savedConfig.value === configKey) {
        savedConfig.value = null
      }
    }, 2000)
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : 'Failed to update config')
  }
}

function updateConfigDebounced(pluginName: string, key: string, value: string, delay = 500) {
  const configKey = `${pluginName}:${key}`

  // Clear existing timer
  if (debounceTimers[configKey]) {
    clearTimeout(debounceTimers[configKey])
  }

  // Store local value (non-reactive, won't cause re-render)
  localInputValues[configKey] = value

  // Debounce the API call
  debounceTimers[configKey] = setTimeout(() => {
    saveConfig(pluginName, key, localInputValues[configKey])
    delete debounceTimers[configKey]
    delete localInputValues[configKey]
  }, delay)
}

function updateConfigImmediate(pluginName: string, key: string, value: string) {
  // Update local state
  if (status.value) {
    const plugin = status.value.plugins.find(p => p.name === pluginName)
    if (plugin?.config) {
      plugin.config.values[key] = value
    }
  }
  // Save immediately
  saveConfig(pluginName, key, value)
}

function onToggle(pluginName: string, key: string, currentValue: string) {
  const newValue = currentValue === 'true' ? 'false' : 'true'
  updateConfigImmediate(pluginName, key, newValue)
}

// Cleanup timers on unmount
onUnmounted(() => {
  Object.values(debounceTimers.value).forEach(timer => clearTimeout(timer))
})

function parseArray(value: string | undefined): string[] {
  if (!value) return []
  try {
    const arr = JSON.parse(value)
    return Array.isArray(arr) ? arr : []
  } catch {
    return []
  }
}

function addArrayItem(pluginName: string, key: string) {
  const itemKey = `${pluginName}:${key}`
  const newItem = newArrayItem.value[itemKey]?.trim()
  if (!newItem) return

  const plugin = status.value?.plugins.find(p => p.name === pluginName)
  if (!plugin?.config) return

  const currentArray = parseArray(plugin.config.values[key])
  if (currentArray.includes(newItem)) {
    ElMessage.warning('Item already exists')
    return
  }

  currentArray.push(newItem)
  const newValue = JSON.stringify(currentArray)
  updateConfigImmediate(pluginName, key, newValue)
  newArrayItem.value[itemKey] = ''
}

function removeArrayItem(pluginName: string, key: string, index: number) {
  const plugin = status.value?.plugins.find(p => p.name === pluginName)
  if (!plugin?.config) return

  const currentArray = parseArray(plugin.config.values[key])
  currentArray.splice(index, 1)
  const newValue = JSON.stringify(currentArray)
  updateConfigImmediate(pluginName, key, newValue)
}

async function handleExport() {
  try {
    const blob = await api.exportConfig()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'chadbot-config.toml'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    ElMessage.success('Config exported successfully')
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : 'Failed to export config')
  }
}

function triggerImport() {
  fileInputRef.value?.click()
}

async function handleImport(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  try {
    await api.importConfig(file)
    ElMessage.success('Config imported successfully')
    await loadStatus()
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : 'Failed to import config')
  } finally {
    input.value = ''
  }
}

onMounted(() => {
  loadStatus()
})
</script>

<template>
  <div class="status-page">
    <header class="status-header">
      <el-button @click="router.push('/')">
        <el-icon><ArrowLeft /></el-icon>
        Back to Chat
      </el-button>
      <h1>System Status</h1>
      <div class="header-actions">
        <el-button @click="handleExport">
          <el-icon><Download /></el-icon>
          Export Config
        </el-button>
        <el-button @click="triggerImport">
          <el-icon><Upload /></el-icon>
          Import Config
        </el-button>
        <input
          ref="fileInputRef"
          type="file"
          accept=".toml"
          style="display: none"
          @change="handleImport"
        />
        <el-button type="primary" :loading="loading" @click="loadStatus">
          <el-icon><Refresh /></el-icon>
          Refresh
        </el-button>
      </div>
    </header>

    <div v-if="loading" class="loading-container">
      <el-skeleton :rows="5" animated />
    </div>

    <el-alert
      v-else-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
      class="error-alert"
    />

    <div v-else-if="status" class="status-content">
      <!-- Plugins Section -->
      <section class="section">
        <div class="section-header">
          <el-icon :size="20"><Connection /></el-icon>
          <h2>Connected Plugins</h2>
          <el-tag type="info" size="small">{{ status.plugins.length }}</el-tag>
        </div>

        <el-empty
          v-if="status.plugins.length === 0"
          description="No plugins connected"
          :image-size="80"
        />

        <div v-else class="cards-grid">
          <el-card
            v-for="plugin in status.plugins"
            :key="plugin.id"
            class="plugin-card"
            shadow="hover"
          >
            <template #header>
              <div class="card-header">
                <div class="card-title">
                  <h3>{{ plugin.name }}</h3>
                  <el-tag size="small" type="info">v{{ plugin.version }}</el-tag>
                </div>
              </div>
            </template>

            <p class="description">{{ plugin.description }}</p>

            <div class="meta-item">
              <span class="label">ID:</span>
              <el-text type="info" size="small" truncated>
                <code>{{ plugin.id.slice(0, 8) }}...</code>
              </el-text>
            </div>

            <div v-if="plugin.subscribed && plugin.subscribed.length > 0" class="subscriptions">
              <span class="label">Subscribed Events:</span>
              <div class="tags">
                <el-tag
                  v-for="sub in plugin.subscribed"
                  :key="sub"
                  size="small"
                  type="warning"
                  effect="plain"
                >
                  {{ sub }}
                </el-tag>
              </div>
            </div>

            <!-- Plugin Configuration -->
            <div
              v-if="plugin.config && plugin.config.schema && plugin.config.schema.length > 0"
              class="config-section"
            >
              <el-divider content-position="left">
                <el-icon><Setting /></el-icon>
                Configuration
              </el-divider>

              <div class="config-fields">
                <div
                  v-for="field in plugin.config.schema"
                  :key="field.key"
                  class="config-field"
                >
                  <div class="config-field-header">
                    <label>{{ field.label }}</label>
                    <span class="config-status-wrapper">
                      <el-text v-show="savingConfig === `${plugin.name}:${field.key}`" type="primary" size="small" class="config-status">
                        Saving...
                      </el-text>
                      <el-text v-show="savedConfig === `${plugin.name}:${field.key}` && savingConfig !== `${plugin.name}:${field.key}`" type="success" size="small" class="config-status">
                        <el-icon><Check /></el-icon>
                        Saved
                      </el-text>
                    </span>
                  </div>
                  <el-text type="info" size="small" class="config-description">
                    {{ field.description }}
                  </el-text>

                  <!-- Boolean toggle -->
                  <div v-if="field.type === 'bool'" class="toggle-wrapper">
                    <el-switch
                      :model-value="plugin.config.values[field.key] === 'true'"
                      :disabled="savingConfig === `${plugin.name}:${field.key}`"
                      @change="onToggle(plugin.name, field.key, plugin.config!.values[field.key] || 'false')"
                    />
                    <el-text :type="plugin.config.values[field.key] === 'true' ? 'success' : 'info'" size="small">
                      {{ plugin.config.values[field.key] === 'true' ? 'Enabled' : 'Disabled' }}
                    </el-text>
                  </div>

                  <!-- String input - uses native input to avoid focus loss on re-render -->
                  <input
                    v-else-if="field.type === 'string'"
                    type="text"
                    class="config-input"
                    :ref="(el: any) => el && !el._initialized && (el.value = plugin.config?.values[field.key] ?? field.default_value, el._initialized = true)"
                    @input="(e: Event) => updateConfigDebounced(plugin.name, field.key, (e.target as HTMLInputElement).value)"
                  />

                  <!-- Number input -->
                  <el-input-number
                    v-else-if="field.type === 'number'"
                    :model-value="Number(plugin.config.values[field.key] || field.default_value || 0)"
                    :disabled="savingConfig === `${plugin.name}:${field.key}`"
                    @change="(val: number | undefined) => updateConfigImmediate(plugin.name, field.key, String(val ?? 0))"
                    controls-position="right"
                  />

                  <!-- String Array input -->
                  <div v-else-if="field.type === 'string_array'" class="array-input">
                    <div class="array-tags">
                      <el-tag
                        v-for="(item, index) in parseArray(plugin.config.values[field.key])"
                        :key="index"
                        closable
                        :disable-transitions="false"
                        @close="removeArrayItem(plugin.name, field.key, index)"
                      >
                        {{ item }}
                      </el-tag>
                    </div>
                    <div class="array-add">
                      <el-input
                        v-model="newArrayItem[`${plugin.name}:${field.key}`]"
                        placeholder="Add item..."
                        size="small"
                        :disabled="savingConfig === `${plugin.name}:${field.key}`"
                        @keyup.enter="addArrayItem(plugin.name, field.key)"
                      />
                      <el-button
                        size="small"
                        type="primary"
                        :disabled="savingConfig === `${plugin.name}:${field.key}`"
                        @click="addArrayItem(plugin.name, field.key)"
                      >
                        <el-icon><Plus /></el-icon>
                        Add
                      </el-button>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </el-card>
        </div>
      </section>

      <!-- Skills Section -->
      <section class="section">
        <div class="section-header">
          <el-icon :size="20"><Tools /></el-icon>
          <h2>Available Skills</h2>
          <el-tag type="info" size="small">{{ status.skills.length }}</el-tag>
        </div>

        <el-empty
          v-if="status.skills.length === 0"
          description="No skills registered"
          :image-size="80"
        />

        <div v-else class="cards-grid">
          <el-card
            v-for="skill in status.skills"
            :key="skill.name"
            class="skill-card"
            shadow="hover"
          >
            <template #header>
              <div class="card-header">
                <div class="card-title">
                  <h3>{{ skill.name }}</h3>
                  <el-tag size="small" type="success">{{ skill.plugin_name }}</el-tag>
                </div>
              </div>
            </template>

            <p class="description">{{ skill.description }}</p>

            <div v-if="skill.parameters && skill.parameters.length > 0" class="parameters">
              <el-divider content-position="left">Parameters</el-divider>
              <el-table
                :data="skill.parameters"
                size="small"
                stripe
                :show-header="true"
              >
                <el-table-column prop="name" label="Name" width="120">
                  <template #default="{ row }">
                    <code>{{ row.name }}</code>
                  </template>
                </el-table-column>
                <el-table-column prop="type" label="Type" width="100">
                  <template #default="{ row }">
                    <el-tag size="small" type="info">{{ row.type }}</el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="required" label="Required" width="90">
                  <template #default="{ row }">
                    <el-tag :type="row.required ? 'danger' : 'info'" size="small">
                      {{ row.required ? 'Yes' : 'No' }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="description" label="Description" />
              </el-table>
            </div>
          </el-card>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.status-page {
  height: 100vh;
  overflow-y: auto;
  background: var(--el-bg-color-page);
  padding: 24px;
}

.status-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 24px;
}

.status-header h1 {
  flex: 1;
  font-size: 24px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0;
}

.header-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}

.loading-container {
  padding: 24px;
}

.error-alert {
  margin-bottom: 24px;
}

.section {
  margin-bottom: 32px;
}

.section-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.section-header h2 {
  font-size: 18px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0;
}

.cards-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(auto-fill, minmax(400px, 1fr));
}

.plugin-card,
.skill-card {
  background: var(--el-bg-color);
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.card-title {
  display: flex;
  align-items: center;
  gap: 12px;
}

.card-title h3 {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0;
}

.description {
  color: var(--el-text-color-secondary);
  font-size: 14px;
  margin: 0 0 12px 0;
  line-height: 1.5;
}

.meta-item {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.meta-item code {
  background: var(--el-fill-color);
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 12px;
}

.label {
  font-weight: 500;
  color: var(--el-text-color-regular);
}

.subscriptions {
  margin-top: 12px;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.config-section {
  margin-top: 16px;
}

.config-fields {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.config-field {
  background: var(--el-fill-color-lighter);
  padding: 12px;
  border-radius: 8px;
}

.config-field-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
}

.config-field-header label {
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.config-description {
  display: block;
  margin-bottom: 8px;
}

.toggle-wrapper {
  display: flex;
  align-items: center;
  gap: 12px;
}

.array-input {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.array-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-height: 24px;
}

.array-add {
  display: flex;
  gap: 8px;
  align-items: center;
}

.array-add .el-input {
  flex: 1;
}

.parameters {
  margin-top: 16px;
}

.parameters :deep(.el-table) {
  --el-table-bg-color: transparent;
  --el-table-tr-bg-color: transparent;
}

.parameters :deep(.el-table th.el-table__cell) {
  background: var(--el-fill-color-light);
}

.parameters :deep(code) {
  background: var(--el-fill-color);
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 12px;
}

.config-status-wrapper {
  min-width: 60px;
  text-align: right;
}

.config-status {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.config-input {
  width: 100%;
  padding: 8px 12px;
  font-size: 14px;
  line-height: 1.5;
  color: var(--el-text-color-regular);
  background-color: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  outline: none;
  transition: border-color 0.2s;
}

.config-input:focus {
  border-color: var(--el-color-primary);
}

.config-input:hover {
  border-color: var(--el-border-color-hover);
}

.config-input:disabled {
  background-color: var(--el-fill-color-light);
  cursor: not-allowed;
}
</style>
