<script setup lang="ts">
import { computed } from 'vue'
import { marked } from 'marked'
import type { ChatMessage } from '../stores/chat'

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
    // Don't render markdown for user messages
    return props.message.content
  }
  return marked.parse(props.message.content) as string
})
</script>

<template>
  <div class="message" :class="message.role">
    <div
      v-if="message.role === 'assistant'"
      class="message-content markdown"
      v-html="renderedContent"
    ></div>
    <div v-else class="message-content">
      {{ message.content }}
    </div>
    <div class="message-meta">
      {{ new Date(message.created_at).toLocaleTimeString() }}
    </div>
  </div>
</template>

<style scoped>
.message {
  max-width: 75%;
  padding: 0.75rem 1rem;
  border-radius: 12px;
  word-wrap: break-word;
}

.message.user {
  align-self: flex-end;
  background: var(--message-user);
  border-bottom-right-radius: 4px;
}

.message.assistant {
  align-self: flex-start;
  background: var(--message-assistant);
  border-bottom-left-radius: 4px;
}

.message-content {
  line-height: 1.6;
}

.message-meta {
  font-size: 0.75rem;
  color: var(--text-secondary);
  margin-top: 0.25rem;
}

.message.user .message-meta {
  text-align: right;
}

/* Markdown styles */
.markdown :deep(p) {
  margin: 0 0 0.75rem 0;
}

.markdown :deep(p:last-child) {
  margin-bottom: 0;
}

.markdown :deep(h1),
.markdown :deep(h2),
.markdown :deep(h3),
.markdown :deep(h4) {
  margin: 1rem 0 0.5rem 0;
  font-weight: 600;
}

.markdown :deep(h1:first-child),
.markdown :deep(h2:first-child),
.markdown :deep(h3:first-child) {
  margin-top: 0;
}

.markdown :deep(code) {
  background: rgba(0, 0, 0, 0.3);
  padding: 0.15rem 0.4rem;
  border-radius: 4px;
  font-family: 'SF Mono', 'Fira Code', monospace;
  font-size: 0.9em;
}

.markdown :deep(pre) {
  background: rgba(0, 0, 0, 0.3);
  padding: 1rem;
  border-radius: 8px;
  overflow-x: auto;
  margin: 0.75rem 0;
}

.markdown :deep(pre code) {
  background: none;
  padding: 0;
  font-size: 0.875rem;
}

.markdown :deep(ul),
.markdown :deep(ol) {
  margin: 0.5rem 0;
  padding-left: 1.5rem;
}

.markdown :deep(li) {
  margin: 0.25rem 0;
}

.markdown :deep(blockquote) {
  border-left: 3px solid var(--accent);
  margin: 0.75rem 0;
  padding-left: 1rem;
  color: var(--text-secondary);
}

.markdown :deep(a) {
  color: var(--accent);
  text-decoration: none;
}

.markdown :deep(a:hover) {
  text-decoration: underline;
}

.markdown :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 0.75rem 0;
}

.markdown :deep(th),
.markdown :deep(td) {
  border: 1px solid var(--bg-tertiary);
  padding: 0.5rem;
  text-align: left;
}

.markdown :deep(th) {
  background: rgba(0, 0, 0, 0.2);
}

.markdown :deep(hr) {
  border: none;
  border-top: 1px solid var(--bg-tertiary);
  margin: 1rem 0;
}

.markdown :deep(img) {
  max-width: 100%;
  border-radius: 8px;
}
</style>
