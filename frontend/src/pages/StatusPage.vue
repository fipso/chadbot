<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import * as api from '../services/api'

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
    // Update local state
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

function onInputChange(pluginName: string, key: string, event: Event) {
  const target = event.target as HTMLInputElement
  updateConfig(pluginName, key, target.value)
}

onMounted(() => {
  loadStatus()
})
</script>

<template>
  <div class="status-page">
    <header class="status-header">
      <button class="back-btn" @click="router.push('/')">
        &larr; Back to Chat
      </button>
      <h1>System Status</h1>
      <button class="refresh-btn" @click="loadStatus" :disabled="loading">
        Refresh
      </button>
    </header>

    <div v-if="loading" class="loading">
      Loading...
    </div>

    <div v-else-if="error" class="error">
      {{ error }}
    </div>

    <div v-else-if="status" class="status-content">
      <!-- Plugins Section -->
      <section class="section">
        <h2>Connected Plugins ({{ status.plugins.length }})</h2>
        <div v-if="status.plugins.length === 0" class="empty">
          No plugins connected
        </div>
        <div v-else class="cards">
          <div v-for="plugin in status.plugins" :key="plugin.id" class="card plugin-card">
            <div class="card-header">
              <h3>{{ plugin.name }}</h3>
              <span class="version">v{{ plugin.version }}</span>
            </div>
            <p class="description">{{ plugin.description }}</p>
            <div class="meta">
              <span class="label">ID:</span>
              <code>{{ plugin.id.slice(0, 8) }}...</code>
            </div>
            <div v-if="plugin.subscribed && plugin.subscribed.length > 0" class="subscriptions">
              <span class="label">Subscribed Events:</span>
              <div class="tags">
                <span v-for="sub in plugin.subscribed" :key="sub" class="tag">
                  {{ sub }}
                </span>
              </div>
            </div>

            <!-- Plugin Configuration -->
            <div v-if="plugin.config && plugin.config.schema && plugin.config.schema.length > 0" class="config-section">
              <span class="label">Configuration:</span>
              <div class="config-fields">
                <div v-for="field in plugin.config.schema" :key="field.key" class="config-field">
                  <div class="config-field-header">
                    <label :for="`${plugin.name}-${field.key}`">{{ field.label }}</label>
                    <span v-if="savingConfig === `${plugin.name}:${field.key}`" class="saving">Saving...</span>
                  </div>
                  <p class="config-description">{{ field.description }}</p>

                  <!-- Boolean toggle -->
                  <div v-if="field.type === 'bool'" class="toggle-wrapper">
                    <button
                      :id="`${plugin.name}-${field.key}`"
                      class="toggle-btn"
                      :class="{ active: plugin.config.values[field.key] === 'true' }"
                      @click="onToggle(plugin.name, field.key, plugin.config.values[field.key] || 'false')"
                      :disabled="savingConfig === `${plugin.name}:${field.key}`"
                    >
                      <span class="toggle-indicator"></span>
                    </button>
                    <span class="toggle-label">{{ plugin.config.values[field.key] === 'true' ? 'Enabled' : 'Disabled' }}</span>
                  </div>

                  <!-- String input -->
                  <input
                    v-else-if="field.type === 'string'"
                    :id="`${plugin.name}-${field.key}`"
                    type="text"
                    class="config-input"
                    :value="plugin.config.values[field.key] || field.default_value"
                    @change="onInputChange(plugin.name, field.key, $event)"
                    :disabled="savingConfig === `${plugin.name}:${field.key}`"
                  />

                  <!-- Number input -->
                  <input
                    v-else-if="field.type === 'number'"
                    :id="`${plugin.name}-${field.key}`"
                    type="number"
                    class="config-input"
                    :value="plugin.config.values[field.key] || field.default_value"
                    @change="onInputChange(plugin.name, field.key, $event)"
                    :disabled="savingConfig === `${plugin.name}:${field.key}`"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- Skills Section -->
      <section class="section">
        <h2>Available Skills ({{ status.skills.length }})</h2>
        <div v-if="status.skills.length === 0" class="empty">
          No skills registered
        </div>
        <div v-else class="cards">
          <div v-for="skill in status.skills" :key="skill.name" class="card skill-card">
            <div class="card-header">
              <h3>{{ skill.name }}</h3>
              <span class="plugin-badge">{{ skill.plugin_name }}</span>
            </div>
            <p class="description">{{ skill.description }}</p>
            <div v-if="skill.parameters && skill.parameters.length > 0" class="parameters">
              <span class="label">Parameters:</span>
              <table class="params-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Type</th>
                    <th>Required</th>
                    <th>Description</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="param in skill.parameters" :key="param.name">
                    <td><code>{{ param.name }}</code></td>
                    <td><code>{{ param.type }}</code></td>
                    <td>{{ param.required ? 'Yes' : 'No' }}</td>
                    <td>{{ param.description }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.status-page {
  height: 100vh;
  overflow-y: auto;
  background: var(--bg-primary);
  padding: 2rem;
}

.status-header {
  display: flex;
  align-items: center;
  gap: 1rem;
  margin-bottom: 2rem;
}

.status-header h1 {
  flex: 1;
  font-size: 1.5rem;
  font-weight: 600;
}

.back-btn, .refresh-btn {
  padding: 0.5rem 1rem;
}

.loading, .error {
  text-align: center;
  padding: 2rem;
  color: var(--text-secondary);
}

.error {
  color: #ff6b6b;
}

.section {
  margin-bottom: 2rem;
}

.section h2 {
  font-size: 1.25rem;
  font-weight: 500;
  margin-bottom: 1rem;
  color: var(--text-primary);
}

.empty {
  color: var(--text-secondary);
  padding: 1rem;
  text-align: center;
  background: var(--bg-secondary);
  border-radius: 8px;
}

.cards {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(350px, 1fr));
}

.card {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 1rem;
  border: 1px solid var(--bg-tertiary);
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 0.5rem;
}

.card-header h3 {
  font-size: 1rem;
  font-weight: 600;
  color: var(--text-primary);
}

.version {
  font-size: 0.75rem;
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
}

.plugin-badge {
  font-size: 0.75rem;
  color: var(--accent);
  background: rgba(79, 70, 229, 0.1);
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
}

.description {
  color: var(--text-secondary);
  font-size: 0.875rem;
  margin-bottom: 0.75rem;
}

.meta {
  font-size: 0.75rem;
  color: var(--text-secondary);
  margin-bottom: 0.5rem;
}

.meta code {
  background: var(--bg-tertiary);
  padding: 0.125rem 0.25rem;
  border-radius: 2px;
}

.label {
  font-weight: 500;
  margin-right: 0.5rem;
}

.subscriptions {
  margin-top: 0.75rem;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
  margin-top: 0.25rem;
}

.tag {
  font-size: 0.75rem;
  background: var(--bg-tertiary);
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  font-family: monospace;
}

.parameters {
  margin-top: 0.75rem;
}

.params-table {
  width: 100%;
  font-size: 0.75rem;
  margin-top: 0.5rem;
  border-collapse: collapse;
}

.params-table th,
.params-table td {
  text-align: left;
  padding: 0.5rem;
  border-bottom: 1px solid var(--bg-tertiary);
}

.params-table th {
  color: var(--text-secondary);
  font-weight: 500;
}

.params-table code {
  background: var(--bg-tertiary);
  padding: 0.125rem 0.25rem;
  border-radius: 2px;
}

/* Plugin Config Styles */
.config-section {
  margin-top: 1rem;
  padding-top: 1rem;
  border-top: 1px solid var(--bg-tertiary);
}

.config-fields {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  margin-top: 0.5rem;
}

.config-field {
  background: var(--bg-tertiary);
  padding: 0.75rem;
  border-radius: 6px;
}

.config-field-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 0.25rem;
}

.config-field-header label {
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--text-primary);
}

.saving {
  font-size: 0.75rem;
  color: var(--accent);
}

.config-description {
  font-size: 0.75rem;
  color: var(--text-secondary);
  margin-bottom: 0.5rem;
}

/* Toggle button styles */
.toggle-wrapper {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.toggle-btn {
  position: relative;
  width: 44px;
  height: 24px;
  background: var(--bg-secondary);
  border: 1px solid var(--bg-tertiary);
  border-radius: 12px;
  cursor: pointer;
  transition: background-color 0.2s, border-color 0.2s;
  padding: 0;
}

.toggle-btn:hover {
  border-color: var(--accent);
}

.toggle-btn.active {
  background: var(--accent);
  border-color: var(--accent);
}

.toggle-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.toggle-indicator {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 18px;
  height: 18px;
  background: white;
  border-radius: 50%;
  transition: transform 0.2s;
}

.toggle-btn.active .toggle-indicator {
  transform: translateX(20px);
}

.toggle-label {
  font-size: 0.875rem;
  color: var(--text-secondary);
}

/* Input styles */
.config-input {
  width: 100%;
  padding: 0.5rem;
  font-size: 0.875rem;
  background: var(--bg-secondary);
  border: 1px solid var(--bg-tertiary);
  border-radius: 4px;
  color: var(--text-primary);
}

.config-input:focus {
  outline: none;
  border-color: var(--accent);
}

.config-input:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
