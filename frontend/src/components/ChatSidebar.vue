<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useChatStore } from '../stores/chat'

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
      <h1>Chadbot</h1>
      <span class="status" :class="{ connected: chatStore.isConnected }">
        {{ chatStore.isConnected ? 'Connected' : 'Disconnected' }}
      </span>
    </div>

    <button class="new-chat-btn" @click="chatStore.createChat()">
      + New Chat
    </button>

    <button class="status-btn" @click="router.push('/status')">
      System Status
    </button>

    <div v-if="chatStore.providers.length > 0" class="model-selector">
      <label>Model</label>
      <select
        :value="chatStore.selectedProvider"
        @change="(e) => chatStore.setProvider((e.target as HTMLSelectElement).value)"
      >
        <option
          v-for="provider in chatStore.providers"
          :key="provider.name"
          :value="provider.name"
        >
          {{ provider.name }}{{ provider.is_default ? ' (default)' : '' }}
        </option>
      </select>
    </div>

    <nav class="chat-list">
      <div
        v-for="chat in chatStore.chatList"
        :key="chat.id"
        class="chat-item"
        :class="{ active: chat.id === chatStore.activeChatId }"
        @click="chatStore.setActiveChat(chat.id)"
      >
        <input
          v-if="editingChatId === chat.id"
          ref="editInput"
          v-model="editingName"
          class="chat-name-input"
          @blur="finishEditing"
          @keydown="handleKeydown"
          @click.stop
        />
        <span
          v-else
          class="chat-name"
          @dblclick.stop="startEditing(chat.id, chat.name)"
          title="Double-click to rename"
        >
          {{ chat.name }}
        </span>
        <button
          class="delete-btn"
          @click.stop="chatStore.deleteChat(chat.id)"
          title="Delete chat"
        >
          ×
        </button>
      </div>
    </nav>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 260px;
  background: var(--bg-secondary);
  border-right: 1px solid var(--bg-tertiary);
  display: flex;
  flex-direction: column;
  padding: 1rem;
}

.sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 1rem;
}

.sidebar-header h1 {
  font-size: 1.25rem;
  font-weight: 600;
}

.status {
  font-size: 0.75rem;
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  background: #ff4444;
  color: white;
}

.status.connected {
  background: #44bb44;
}

.new-chat-btn {
  width: 100%;
  margin-bottom: 0.5rem;
}

.status-btn {
  width: 100%;
  margin-bottom: 1rem;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.status-btn:hover {
  background: var(--bg-primary);
  color: var(--text-primary);
}

.model-selector {
  margin-bottom: 1rem;
}

.model-selector label {
  display: block;
  font-size: 0.75rem;
  color: var(--text-secondary);
  margin-bottom: 0.25rem;
}

.model-selector select {
  width: 100%;
  padding: 0.5rem;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  border: 1px solid var(--bg-tertiary);
  border-radius: 6px;
  font-size: 0.875rem;
  cursor: pointer;
}

.model-selector select:hover {
  border-color: var(--accent);
}

.model-selector select:focus {
  outline: none;
  border-color: var(--accent);
}

.chat-list {
  flex: 1;
  overflow-y: auto;
}

.chat-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.75rem;
  border-radius: 8px;
  cursor: pointer;
  margin-bottom: 0.25rem;
  transition: background 0.2s;
}

.chat-item:hover {
  background: var(--bg-tertiary);
}

.chat-item.active {
  background: var(--bg-tertiary);
}

.chat-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-name-input {
  flex: 1;
  background: var(--bg-primary);
  border: 1px solid var(--accent);
  color: var(--text-primary);
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  font-size: inherit;
  min-width: 0;
}

.chat-name-input:focus {
  outline: none;
}

.delete-btn {
  opacity: 0;
  background: transparent;
  color: var(--text-secondary);
  padding: 0.25rem 0.5rem;
  font-size: 1.25rem;
  line-height: 1;
}

.chat-item:hover .delete-btn {
  opacity: 1;
}

.delete-btn:hover {
  color: var(--accent);
  background: transparent;
}
</style>
