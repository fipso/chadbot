<script setup lang="ts">
import { ref, nextTick, watch, computed } from 'vue'
import { useChatStore } from '../stores/chat'
import ChatMessage from './ChatMessage.vue'
import VoiceButton from './VoiceButton.vue'

const chatStore = useChatStore()
const messageInput = ref('')
const messagesContainer = ref<HTMLElement | null>(null)

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
      <div v-if="chatStore.isLoading" class="loading">
        <span class="dot"></span>
        <span class="dot"></span>
        <span class="dot"></span>
      </div>
    </div>

    <div class="input-area">
      <div class="input-wrapper">
        <textarea
          v-model="messageInput"
          placeholder="Type a message..."
          rows="1"
          @keydown="handleKeydown"
          :disabled="chatStore.isLoading"
        ></textarea>
        <VoiceButton @result="handleVoiceResult" />
        <button
          class="send-btn"
          @click="handleSend"
          :disabled="!messageInput.trim() || chatStore.isLoading"
        >
          Send
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chat-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.chat-header {
  padding: 1rem;
  border-bottom: 1px solid var(--bg-tertiary);
}

.chat-header h2 {
  font-size: 1.125rem;
  font-weight: 500;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 1rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.loading {
  display: flex;
  gap: 0.25rem;
  padding: 1rem;
  align-self: flex-start;
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--text-secondary);
  animation: bounce 1.4s infinite ease-in-out both;
}

.dot:nth-child(1) { animation-delay: -0.32s; }
.dot:nth-child(2) { animation-delay: -0.16s; }

@keyframes bounce {
  0%, 80%, 100% { transform: scale(0); }
  40% { transform: scale(1); }
}

.input-area {
  padding: 1rem;
  border-top: 1px solid var(--bg-tertiary);
}

.input-wrapper {
  display: flex;
  gap: 0.5rem;
  align-items: flex-end;
}

.input-wrapper textarea {
  flex: 1;
  resize: none;
  min-height: 44px;
  max-height: 120px;
}

.send-btn {
  min-width: 80px;
}
</style>
