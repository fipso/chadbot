<script setup lang="ts">
import { computed } from 'vue'
import { marked } from 'marked'
import type { ChatMessage } from '../stores/chat'
import { User, Monitor } from '@element-plus/icons-vue'

const props = defineProps<{
  message: ChatMessage
}>()

// Configure marked for security
marked.setOptions({
  breaks: true,
  gfm: true
})

const renderedContent = computed(() => {
  if (props.message.role === 'user') {
    return props.message.content
  }
  return marked.parse(props.message.content) as string
})

const formattedTime = computed(() => {
  return new Date(props.message.created_at).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit'
  })
})
</script>

<template>
  <div class="message" :class="message.role">
    <div class="avatar">
      <el-avatar :size="32" :class="message.role">
        <el-icon v-if="message.role === 'user'"><User /></el-icon>
        <el-icon v-else><Monitor /></el-icon>
      </el-avatar>
    </div>
    <div class="message-content-wrapper">
      <div class="message-header">
        <span class="role-label">{{ message.role === 'user' ? 'You' : 'Assistant' }}</span>
        <span class="timestamp">{{ formattedTime }}</span>
      </div>
      <div
        v-if="message.role === 'assistant'"
        class="message-content markdown-content"
        v-html="renderedContent"
      ></div>
      <div v-else class="message-content">
        {{ message.content }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.message {
  display: flex;
  gap: 12px;
  max-width: 85%;
}

.message.user {
  align-self: flex-end;
  flex-direction: row-reverse;
}

.message.assistant {
  align-self: flex-start;
}

.avatar .el-avatar {
  background: var(--el-fill-color);
  color: var(--el-text-color-secondary);
}

.avatar .el-avatar.user {
  background: var(--el-color-primary);
  color: white;
}

.avatar .el-avatar.assistant {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
}

.message-content-wrapper {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.message-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 4px;
}

.message.user .message-header {
  flex-direction: row-reverse;
}

.role-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
}

.timestamp {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

.message-content {
  padding: 12px 16px;
  border-radius: 16px;
  line-height: 1.6;
  font-size: 14px;
}

.message.user .message-content {
  background: var(--el-color-primary);
  color: white;
  border-bottom-right-radius: 4px;
}

.message.assistant .message-content {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
  border-bottom-left-radius: 4px;
}
</style>
