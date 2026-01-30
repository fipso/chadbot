<script setup lang="ts">
import { computed } from 'vue'
import { marked } from 'marked'
import type { ChatMessage } from '../stores/chat'
import { User, Monitor, Picture } from '@element-plus/icons-vue'

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

// Get image attachments with data URIs
const imageAttachments = computed(() => {
  if (!props.message.attachments) return []
  return props.message.attachments
    .filter(a => a.type === 'image' && a.data)
    .map(a => ({
      src: `data:${a.mime_type};base64,${a.data}`,
      name: a.name || 'Image'
    }))
})

const roleLabel = computed(() => {
  switch (props.message.role) {
    case 'user': return 'You'
    case 'assistant': return 'Assistant'
    case 'plugin': return 'Plugin'
    default: return props.message.role
  }
})

const modelInfo = computed(() => {
  if (props.message.role !== 'assistant') return null
  const parts: string[] = []
  if (props.message.soul) parts.push(props.message.soul)
  if (props.message.provider) parts.push(props.message.provider)
  return parts.length > 0 ? parts.join(' · ') : null
})

const roleIcon = computed(() => {
  switch (props.message.role) {
    case 'user': return User
    case 'plugin': return Picture
    default: return Monitor
  }
})
</script>

<template>
  <div class="message" :class="message.role">
    <div class="avatar">
      <el-avatar :size="32" :class="message.role">
        <el-icon><component :is="roleIcon" /></el-icon>
      </el-avatar>
    </div>
    <div class="message-content-wrapper">
      <div class="message-header">
        <span class="role-label">{{ roleLabel }}</span>
        <span v-if="modelInfo" class="model-info">{{ modelInfo }}</span>
        <span class="timestamp">{{ formattedTime }}</span>
      </div>
      <!-- Message content with attachments inside -->
      <div
        v-if="message.role === 'assistant' || message.role === 'plugin'"
        class="message-content markdown-content"
      >
        <div v-if="message.content" v-html="renderedContent"></div>
        <!-- Image attachments inside bubble -->
        <div v-if="imageAttachments.length > 0" class="attachments">
          <el-image
            v-for="(img, idx) in imageAttachments"
            :key="idx"
            :src="img.src"
            :alt="img.name"
            fit="contain"
            :preview-src-list="[img.src]"
            class="attachment-image"
          />
        </div>
      </div>
      <div v-else class="message-content">
        {{ message.content }}
        <!-- Image attachments inside bubble for user messages -->
        <div v-if="imageAttachments.length > 0" class="attachments">
          <el-image
            v-for="(img, idx) in imageAttachments"
            :key="idx"
            :src="img.src"
            :alt="img.name"
            fit="contain"
            :preview-src-list="[img.src]"
            class="attachment-image"
          />
        </div>
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

.model-info {
  font-size: 11px;
  color: var(--el-color-primary);
  padding: 1px 6px;
  background: var(--el-color-primary-light-9);
  border-radius: 4px;
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

.message.plugin {
  align-self: flex-start;
}

.avatar .el-avatar.plugin {
  background: linear-gradient(135deg, #10b981 0%, #059669 100%);
  color: white;
}

.message.plugin .message-content {
  background: var(--el-fill-color-light);
  color: var(--el-text-color-primary);
  border-bottom-left-radius: 4px;
}

.attachments {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}

.attachments:first-child {
  margin-top: 0;
}

.attachment-image {
  max-width: 100%;
  max-height: 400px;
  border-radius: 8px;
  cursor: pointer;
}

.attachment-image :deep(.el-image__inner) {
  border-radius: 8px;
}

/* For messages with only attachments (no text) */
.message-content:has(.attachments:first-child) {
  padding: 8px;
}
</style>
