<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useChatStore } from '../stores/chat'
import { ChatLineRound, Plus, Setting, Delete, Edit } from '@element-plus/icons-vue'

const router = useRouter()
const chatStore = useChatStore()
const editingChatId = ref<string | null>(null)
const editingName = ref('')
const editInput = ref<HTMLInputElement | null>(null)

function startEditing(chatId: string, currentName: string) {
  editingChatId.value = chatId
  editingName.value = currentName
  nextTick(() => {
    editInput.value?.focus()
    editInput.value?.select()
  })
}

async function finishEditing() {
  if (editingChatId.value && editingName.value.trim()) {
    await chatStore.renameChat(editingChatId.value, editingName.value.trim())
  }
  editingChatId.value = null
  editingName.value = ''
}

function cancelEditing() {
  editingChatId.value = null
  editingName.value = ''
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    finishEditing()
  } else if (e.key === 'Escape') {
    cancelEditing()
  }
}
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar-header">
      <div class="logo">
        <el-icon :size="24"><ChatLineRound /></el-icon>
        <span class="logo-text">Chadbot</span>
      </div>
      <el-tag
        :type="chatStore.isConnected ? 'success' : 'danger'"
        size="small"
        effect="dark"
      >
        {{ chatStore.isConnected ? 'Connected' : 'Offline' }}
      </el-tag>
    </div>

    <div class="sidebar-actions">
      <el-button type="primary" class="new-chat-btn" @click="chatStore.createChat()">
        <el-icon><Plus /></el-icon>
        New Chat
      </el-button>

      <el-button class="status-btn" @click="router.push('/status')">
        <el-icon><Setting /></el-icon>
        System Status
      </el-button>
    </div>

    <div v-if="chatStore.providers.length > 0" class="model-selector">
      <el-select
        :model-value="chatStore.selectedProvider"
        @change="(val: string) => chatStore.setProvider(val)"
        placeholder="Select Model"
        size="default"
      >
        <el-option
          v-for="provider in chatStore.providers"
          :key="provider.name"
          :label="`${provider.name}${provider.is_default ? ' (default)' : ''}`"
          :value="provider.name"
        />
      </el-select>
    </div>

    <el-divider />

    <nav class="chat-list">
      <el-scrollbar>
        <div
          v-for="chat in chatStore.chatList"
          :key="chat.id"
          class="chat-item"
          :class="{ active: chat.id === chatStore.activeChatId }"
          @click="chatStore.setActiveChat(chat.id)"
        >
          <el-input
            v-if="editingChatId === chat.id"
            ref="editInput"
            v-model="editingName"
            size="small"
            @blur="finishEditing"
            @keydown="handleKeydown"
            @click.stop
          />
          <span
            v-else
            class="chat-name"
            @dblclick.stop="startEditing(chat.id, chat.name)"
          >
            <el-icon class="chat-icon"><ChatLineRound /></el-icon>
            {{ chat.name }}
          </span>
          <div class="chat-actions">
            <el-button
              text
              size="small"
              class="action-btn"
              @click.stop="startEditing(chat.id, chat.name)"
            >
              <el-icon><Edit /></el-icon>
            </el-button>
            <el-popconfirm
              title="Delete this chat?"
              confirm-button-text="Delete"
              cancel-button-text="Cancel"
              @confirm="chatStore.deleteChat(chat.id)"
            >
              <template #reference>
                <el-button
                  text
                  size="small"
                  class="action-btn delete"
                  @click.stop
                >
                  <el-icon><Delete /></el-icon>
                </el-button>
              </template>
            </el-popconfirm>
          </div>
        </div>
      </el-scrollbar>
    </nav>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 280px;
  background: var(--el-bg-color);
  border-right: 1px solid var(--el-border-color);
  display: flex;
  flex-direction: column;
  padding: 16px;
}

.sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.logo {
  display: flex;
  align-items: center;
  gap: 8px;
}

.logo-text {
  font-size: 18px;
  font-weight: 600;
  background: linear-gradient(135deg, var(--el-color-primary), var(--el-color-primary-light-3));
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.sidebar-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 16px;
}

.new-chat-btn,
.status-btn {
  width: 100%;
  justify-content: flex-start;
  margin-left: 0;
}

.model-selector {
  margin-bottom: 8px;
}

.model-selector .el-select {
  width: 100%;
}

.el-divider {
  margin: 12px 0;
}

.chat-list {
  flex: 1;
  overflow: hidden;
  margin: 0 -16px;
  padding: 0 16px;
}

.chat-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  border-radius: 8px;
  cursor: pointer;
  margin-bottom: 4px;
  transition: all 0.2s;
}

.chat-item:hover {
  background: var(--el-fill-color-light);
}

.chat-item.active {
  background: var(--el-fill-color);
}

.chat-name {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--el-text-color-regular);
  font-size: 14px;
}

.chat-item.active .chat-name {
  color: var(--el-text-color-primary);
}

.chat-icon {
  flex-shrink: 0;
  color: var(--el-text-color-secondary);
}

.chat-actions {
  display: flex;
  gap: 2px;
  opacity: 0;
  transition: opacity 0.2s;
}

.chat-item:hover .chat-actions {
  opacity: 1;
}

.action-btn {
  padding: 4px;
  color: var(--el-text-color-secondary);
}

.action-btn:hover {
  color: var(--el-color-primary);
}

.action-btn.delete:hover {
  color: var(--el-color-danger);
}
</style>
