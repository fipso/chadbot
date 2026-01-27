<script setup lang="ts">
import { ref, nextTick, watch, computed } from 'vue'
import { useChatStore } from '../stores/chat'
import ChatMessage from './ChatMessage.vue'
import VoiceButton from './VoiceButton.vue'
import { Promotion } from '@element-plus/icons-vue'

const chatStore = useChatStore()
const messageInput = ref('')
const messagesContainer = ref<HTMLElement | null>(null)
const inputRef = ref<InstanceType<typeof import('element-plus')['ElInput']> | null>(null)

const messages = computed(() => chatStore.activeChat?.messages || [])

watch(messages, async () => {
  await nextTick()
  scrollToBottom()
}, { deep: true })

function scrollToBottom() {
  if (messagesContainer.value) {
    messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
  }
}

function handleSend() {
  if (messageInput.value.trim() && !chatStore.isLoading) {
    chatStore.sendMessage(messageInput.value)
    messageInput.value = ''
    nextTick(() => inputRef.value?.focus())
  }
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    handleSend()
  }
}

function handleVoiceResult(text: string) {
  if (text) {
    messageInput.value = text
  }
}
</script>

<template>
  <div class="chat-view">
    <header class="chat-header">
      <h2>{{ chatStore.activeChat?.name }}</h2>
    </header>

    <div ref="messagesContainer" class="messages">
      <ChatMessage
        v-for="message in messages"
        :key="message.id"
        :message="message"
      />
      <div v-if="chatStore.isLoading" class="loading-indicator">
        <div class="typing-dots">
          <span></span>
          <span></span>
          <span></span>
        </div>
      </div>
    </div>

    <div class="input-area">
      <el-card shadow="never" class="input-card">
        <div class="input-wrapper">
          <el-input
            ref="inputRef"
            v-model="messageInput"
            type="textarea"
            :autosize="{ minRows: 1, maxRows: 4 }"
            placeholder="Type a message..."
            :disabled="chatStore.isLoading"
            @keydown="handleKeydown"
            resize="none"
          />
          <div class="input-actions">
            <VoiceButton @result="handleVoiceResult" />
            <el-button
              type="primary"
              :icon="Promotion"
              circle
              :disabled="!messageInput.trim() || chatStore.isLoading"
              @click="handleSend"
            />
          </div>
        </div>
      </el-card>
    </div>
  </div>
</template>

<style scoped>
.chat-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--el-bg-color-page);
}

.chat-header {
  padding: 16px 24px;
  border-bottom: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
}

.chat-header h2 {
  font-size: 16px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  margin: 0;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.loading-indicator {
  padding: 16px;
  align-self: flex-start;
}

.typing-dots {
  display: flex;
  gap: 4px;
  padding: 12px 16px;
  background: var(--el-fill-color);
  border-radius: 16px;
}

.typing-dots span {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--el-text-color-secondary);
  animation: typing 1.4s infinite ease-in-out both;
}

.typing-dots span:nth-child(1) { animation-delay: -0.32s; }
.typing-dots span:nth-child(2) { animation-delay: -0.16s; }

@keyframes typing {
  0%, 80%, 100% { transform: scale(0.6); opacity: 0.5; }
  40% { transform: scale(1); opacity: 1; }
}

.input-area {
  padding: 16px 24px;
  background: var(--el-bg-color-page);
}

.input-card {
  background: var(--el-bg-color);
  border-radius: 16px;
}

.input-card :deep(.el-card__body) {
  padding: 12px 16px;
}

.input-wrapper {
  display: flex;
  align-items: flex-end;
  gap: 12px;
}

.input-wrapper :deep(.el-textarea__inner) {
  background: transparent;
  border: none;
  box-shadow: none;
  padding: 8px 0;
  font-size: 15px;
  line-height: 1.5;
}

.input-wrapper :deep(.el-textarea__inner:focus) {
  box-shadow: none;
}

.input-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
</style>
