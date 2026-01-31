<script setup lang="ts">
import { useChatStore } from '../stores/chat'
import ChatSidebar from '../components/ChatSidebar.vue'
import ChatView from '../components/ChatView.vue'
import { ChatLineRound, Plus } from '@element-plus/icons-vue'

const chatStore = useChatStore()
</script>

<template>
  <div class="chat-page">
    <ChatSidebar />
    <main class="main">
      <ChatView v-if="chatStore.activeChat" />
      <div v-else class="empty-state">
        <el-empty description=" ">
          <template #image>
            <div class="empty-icon">
              <el-icon :size="64"><ChatLineRound /></el-icon>
            </div>
          </template>
          <template #default>
            <h2 class="empty-title">Welcome to Chadbot</h2>
            <p class="empty-description">Select a chat or create a new one to get started.</p>
            <el-button type="primary" size="large" @click="chatStore.createChat()">
              <el-icon><Plus /></el-icon>
              New Chat
            </el-button>
          </template>
        </el-empty>
      </div>
    </main>
  </div>
</template>

<style scoped>
.chat-page {
  display: flex;
  height: 100vh;
  width: 100%;
  overflow: hidden;
}

.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--el-bg-color-page);
  min-width: 0;
  overflow: hidden;
}

.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.empty-icon {
  width: 120px;
  height: 120px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--el-color-primary-light-5) 0%, var(--el-color-primary) 100%);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 24px;
}

.empty-icon .el-icon {
  color: white;
}

.empty-title {
  font-size: 24px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 0 0 8px 0;
}

.empty-description {
  font-size: 14px;
  color: var(--el-text-color-secondary);
  margin: 0 0 24px 0;
}
</style>
