<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import * as api from '../services/api'
import { ArrowLeft, Refresh, Connection, Tools, Setting } from '@element-plus/icons-vue'

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
  type: 'bool' | 'string' | 'number'
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

async function updateConfig(pluginName: string, key: string, value: string) {
  savingConfig.value = `${pluginName}:${key}`
  try {
    await api.setPluginConfig(pluginName, key, value)
    if (status.value) {
      const plugin = status.value.plugins.find(p => p.name === pluginName)
      if (plugin?.config) {
        plugin.config.values[key] = value
      }
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to update config'
  } finally {
    savingConfig.value = null
  }
}

function onToggle(pluginName: string, key: string, currentValue: string) {
  const newValue = currentValue === 'true' ? 'false' : 'true'
  updateConfig(pluginName, key, newValue)
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
      <el-button type="primary" :loading="loading" @click="loadStatus">
        <el-icon><Refresh /></el-icon>
        Refresh
      </el-button>
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
                    <el-text v-if="savingConfig === `${plugin.name}:${field.key}`" type="primary" size="small">
                      Saving...
                    </el-text>
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

                  <!-- String input -->
                  <el-input
                    v-else-if="field.type === 'string'"
                    :model-value="plugin.config.values[field.key] || field.default_value"
                    :disabled="savingConfig === `${plugin.name}:${field.key}`"
                    @change="(val: string) => updateConfig(plugin.name, field.key, val)"
                  />

                  <!-- Number input -->
                  <el-input-number
                    v-else-if="field.type === 'number'"
                    :model-value="Number(plugin.config.values[field.key] || field.default_value)"
                    :disabled="savingConfig === `${plugin.name}:${field.key}`"
                    @change="(val: number | undefined) => updateConfig(plugin.name, field.key, String(val ?? 0))"
                    controls-position="right"
                  />
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
</style>
