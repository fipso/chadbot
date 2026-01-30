<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import * as api from '../services/api'
import { ArrowLeft, Plus, Delete, Edit } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'

const router = useRouter()
const souls = ref<api.Soul[]>([])
const loading = ref(true)
const error = ref<string | null>(null)
const saving = ref(false)

// Edit state
const editingSoul = ref<string | null>(null)
const editContent = ref('')

// New soul dialog
const showNewDialog = ref(false)
const newSoulName = ref('')
const newSoulContent = ref('')

async function loadSouls() {
  loading.value = true
  error.value = null
  try {
    souls.value = await api.fetchSouls()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load souls'
  } finally {
    loading.value = false
  }
}

function startEdit(soul: api.Soul) {
  editingSoul.value = soul.name
  editContent.value = soul.content
}

function cancelEdit() {
  editingSoul.value = null
  editContent.value = ''
}

async function saveEdit(name: string) {
  saving.value = true
  try {
    await api.updateSoul(name, editContent.value)
    ElMessage.success('Soul updated')
    editingSoul.value = null
    await loadSouls()
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : 'Failed to save')
  } finally {
    saving.value = false
  }
}

async function deleteSoul(name: string) {
  try {
    await ElMessageBox.confirm(
      `Are you sure you want to delete "${name}"?`,
      'Delete Soul',
      { confirmButtonText: 'Delete', cancelButtonText: 'Cancel', type: 'warning' }
    )
    await api.deleteSoul(name)
    ElMessage.success('Soul deleted')
    await loadSouls()
  } catch (e) {
    if (e !== 'cancel') {
      ElMessage.error(e instanceof Error ? e.message : 'Failed to delete')
    }
  }
}

async function createSoul() {
  if (!newSoulName.value.trim()) {
    ElMessage.warning('Name is required')
    return
  }
  saving.value = true
  try {
    await api.createSoul(newSoulName.value.trim(), newSoulContent.value)
    ElMessage.success('Soul created')
    showNewDialog.value = false
    newSoulName.value = ''
    newSoulContent.value = ''
    await loadSouls()
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : 'Failed to create')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  loadSouls()
})
</script>

<template>
  <div class="souls-page">
    <header class="souls-header">
      <el-button @click="router.push('/')">
        <el-icon><ArrowLeft /></el-icon>
        Back to Chat
      </el-button>
      <h1>Souls</h1>
      <el-button type="primary" @click="showNewDialog = true">
        <el-icon><Plus /></el-icon>
        New Soul
      </el-button>
    </header>

    <p class="description">
      Souls are system prompt profiles that define the AI's personality and behavior.
      Select a soul for each message using the selector in the chat input area.
    </p>

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

    <div v-else class="souls-list">
      <el-card
        v-for="soul in souls"
        :key="soul.name"
        class="soul-card"
        shadow="hover"
      >
        <template #header>
          <div class="card-header">
            <div class="card-title">
              <h3>{{ soul.name }}</h3>
              <el-tag v-if="soul.name === 'default'" type="info" size="small">Default</el-tag>
            </div>
            <div class="card-actions">
              <el-button
                v-if="editingSoul !== soul.name"
                size="small"
                @click="startEdit(soul)"
              >
                <el-icon><Edit /></el-icon>
                Edit
              </el-button>
              <el-button
                v-if="soul.name !== 'default'"
                type="danger"
                size="small"
                @click="deleteSoul(soul.name)"
              >
                <el-icon><Delete /></el-icon>
              </el-button>
            </div>
          </div>
        </template>

        <div v-if="editingSoul === soul.name" class="edit-mode">
          <el-input
            v-model="editContent"
            type="textarea"
            :rows="12"
            placeholder="Enter system prompt..."
          />
          <div class="edit-actions">
            <el-button @click="cancelEdit">Cancel</el-button>
            <el-button type="primary" :loading="saving" @click="saveEdit(soul.name)">
              Save
            </el-button>
          </div>
        </div>

        <pre v-else class="soul-content">{{ soul.content }}</pre>
      </el-card>

      <el-empty v-if="souls.length === 0" description="No souls configured" />
    </div>

    <!-- New Soul Dialog -->
    <el-dialog v-model="showNewDialog" title="Create New Soul" width="600px">
      <el-form label-position="top">
        <el-form-item label="Name">
          <el-input
            v-model="newSoulName"
            placeholder="e.g., friendly-assistant"
            :maxlength="50"
          />
          <el-text type="info" size="small">
            Only letters, numbers, dashes, and underscores
          </el-text>
        </el-form-item>
        <el-form-item label="System Prompt">
          <el-input
            v-model="newSoulContent"
            type="textarea"
            :rows="10"
            placeholder="Enter the system prompt that defines this soul's personality..."
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showNewDialog = false">Cancel</el-button>
        <el-button type="primary" :loading="saving" @click="createSoul">
          Create
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.souls-page {
  height: 100vh;
  overflow-y: auto;
  background: var(--el-bg-color-page);
  padding: 24px;
}

.souls-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 16px;
}

.souls-header h1 {
  flex: 1;
  font-size: 24px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0;
}

.description {
  color: var(--el-text-color-secondary);
  margin-bottom: 24px;
}

.loading-container {
  padding: 24px;
}

.error-alert {
  margin-bottom: 24px;
}

.souls-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.soul-card {
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

.card-actions {
  display: flex;
  gap: 8px;
}

.soul-content {
  background: var(--el-fill-color-lighter);
  padding: 16px;
  border-radius: 8px;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-wrap: break-word;
  max-height: 300px;
  overflow-y: auto;
  margin: 0;
  font-family: inherit;
}

.edit-mode {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.edit-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
