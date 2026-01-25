<script setup lang="ts">
import { useChatStore } from '../stores/chat'
import ChatSidebar from '../components/ChatSidebar.vue'
import ChatView from '../components/ChatView.vue'

const chatStore = useChatStore()
</script>

<template>
  <div class="chat-page">
    <ChatSidebar />
    <main class="main">
      <ChatView v-if="chatStore.activeChat" />
      <div v-else class="empty-state">
        <h2>Welcome to Chadbot</h2>
        <p>Select a chat or create a new one to get started.</p>
        <button @click="chatStore.createChat()">New Chat</button>
      </div>
    </main>
  </div>
</template>

<style scoped>
.chat-page {
  display: flex;
  height: 100vh;
  width: 100%;
}

.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
}

.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1rem;
  color: var(--text-secondary);
}

.empty-state h2 {
  color: var(--text-primary);
  font-weight: 500;
}
</style>
