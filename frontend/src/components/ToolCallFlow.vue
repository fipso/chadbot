<script setup lang="ts">
import { ref, computed } from 'vue'
import { ArrowRight, Check, Close, Loading } from '@element-plus/icons-vue'
import type { ToolCallRecord, ToolCallEvent } from '../stores/chat'

const props = defineProps<{
  toolCalls?: ToolCallRecord[]
  pendingCalls?: ToolCallEvent[]
}>()

const isExpanded = ref(false)

const allCalls = computed(() => {
  const completed = props.toolCalls || []
  const pending = props.pendingCalls || []
  return [...completed, ...pending.filter(p => !completed.some(c => c.id === p.tool_id))]
})

const hasToolCalls = computed(() => allCalls.value.length > 0)

const totalDuration = computed(() => {
  if (!props.toolCalls) return 0
  return props.toolCalls.reduce((sum, tc) => sum + tc.duration_ms, 0)
})

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function formatArgs(args: Record<string, string>): string {
  const entries = Object.entries(args)
  if (entries.length === 0) return '(no args)'
  return entries.map(([key, val]) => `${key}: ${val}`).join('\n')
}

function isPending(call: ToolCallRecord | ToolCallEvent): boolean {
  return 'type' in call && call.type === 'start'
}

function isError(call: ToolCallRecord | ToolCallEvent): boolean {
  return ('error' in call && !!call.error)
}

function getCallId(call: ToolCallRecord | ToolCallEvent): string {
  if ('id' in call) return call.id
  if ('tool_id' in call) return call.tool_id
  return ''
}

function getCallName(call: ToolCallRecord | ToolCallEvent): string {
  if ('name' in call) return call.name
  if ('tool_name' in call) return call.tool_name
  return 'unknown'
}

function getCallArgs(call: ToolCallRecord | ToolCallEvent): Record<string, string> {
  return call.arguments || {}
}

function getCallResult(call: ToolCallRecord | ToolCallEvent): string {
  if ('result' in call) return call.result || ''
  return ''
}

function getCallError(call: ToolCallRecord | ToolCallEvent): string {
  if ('error' in call) return call.error || ''
  return ''
}

function getCallDuration(call: ToolCallRecord | ToolCallEvent): number {
  if ('duration_ms' in call && call.duration_ms !== undefined) return call.duration_ms
  return 0
}
</script>

<template>
  <div v-if="hasToolCalls" class="tool-call-flow">
    <!-- Collapsed summary -->
    <div class="flow-header" @click="isExpanded = !isExpanded">
      <div class="flow-toggle">
        <el-icon :class="{ rotated: isExpanded }">
          <ArrowRight />
        </el-icon>
      </div>
      <div class="flow-summary">
        <span class="tool-count">{{ allCalls.length }} tool call{{ allCalls.length !== 1 ? 's' : '' }}</span>
        <span v-if="totalDuration > 0" class="total-duration">{{ formatDuration(totalDuration) }}</span>
      </div>
      <!-- Mini flow diagram when collapsed -->
      <div v-if="!isExpanded" class="mini-flow">
        <span v-for="(call, idx) in allCalls.slice(0, 3)" :key="getCallId(call) || idx" class="mini-node">
          <span :class="['mini-dot', { pending: isPending(call), error: isError(call) }]"></span>
          {{ getCallName(call) }}
        </span>
        <span v-if="allCalls.length > 3" class="mini-more">+{{ allCalls.length - 3 }}</span>
      </div>
    </div>

    <!-- Expanded flow diagram -->
    <div v-if="isExpanded" class="flow-diagram">
      <div v-for="(call, idx) in allCalls" :key="getCallId(call) || idx" class="flow-node">
        <!-- Node header -->
        <div class="node-header">
          <div class="node-status">
            <el-icon v-if="isPending(call)" class="status-pending"><Loading /></el-icon>
            <el-icon v-else-if="isError(call)" class="status-error"><Close /></el-icon>
            <el-icon v-else class="status-success"><Check /></el-icon>
          </div>
          <span class="node-name">{{ getCallName(call) }}</span>
          <span v-if="getCallDuration(call) > 0" class="node-duration">
            {{ formatDuration(getCallDuration(call)) }}
          </span>
        </div>

        <!-- Input/Output -->
        <div class="node-io">
          <div class="io-section io-input">
            <span class="io-label">Input</span>
            <code class="io-content">{{ formatArgs(getCallArgs(call)) }}</code>
          </div>
          <div class="io-arrow">
            <el-icon><ArrowRight /></el-icon>
          </div>
          <div class="io-section io-output">
            <span class="io-label">Output</span>
            <code v-if="getCallError(call)" class="io-content io-error">
              {{ getCallError(call) }}
            </code>
            <code v-else-if="getCallResult(call)" class="io-content">
              {{ getCallResult(call) }}
            </code>
            <span v-else class="io-pending">Running...</span>
          </div>
        </div>

        <!-- Connector line to next node -->
        <div v-if="idx < allCalls.length - 1" class="node-connector"></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tool-call-flow {
  margin: 8px 0;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);
  overflow: hidden;
  max-width: 100%;
}

.flow-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  cursor: pointer;
  user-select: none;
}

.flow-header:hover {
  background: var(--el-fill-color);
}

.flow-toggle {
  color: var(--el-text-color-secondary);
  transition: transform 0.2s;
}

.flow-toggle .rotated {
  transform: rotate(90deg);
}

.flow-summary {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tool-count {
  font-size: 12px;
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.total-duration {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  padding: 2px 6px;
  background: var(--el-fill-color);
  border-radius: 4px;
}

.mini-flow {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}

.mini-node {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.mini-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-color-success);
}

.mini-dot.pending {
  background: var(--el-color-warning);
  animation: pulse 1s infinite;
}

.mini-dot.error {
  background: var(--el-color-danger);
}

.mini-more {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

.flow-diagram {
  padding: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
  overflow: hidden;
}

.flow-node {
  position: relative;
}

.node-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.node-status {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 50%;
}

.status-success {
  color: var(--el-color-success);
}

.status-error {
  color: var(--el-color-danger);
}

.status-pending {
  color: var(--el-color-warning);
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.node-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  font-family: monospace;
}

.node-duration {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-left: auto;
}

.node-io {
  display: flex;
  align-items: stretch;
  gap: 8px;
  margin-left: 28px;
  margin-bottom: 8px;
}

.io-section {
  flex: 1;
  min-width: 0;
  max-width: calc(50% - 20px);
  padding: 8px;
  background: var(--el-bg-color);
  border-radius: 6px;
  border: 1px solid var(--el-border-color-lighter);
  overflow: hidden;
}

.io-label {
  display: block;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
}

.io-content {
  display: block;
  font-size: 11px;
  color: var(--el-text-color-primary);
  word-break: break-word;
  overflow-wrap: break-word;
  white-space: pre-wrap;
}

.io-content.io-error {
  color: var(--el-color-danger);
}

.io-pending {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  font-style: italic;
}

.io-arrow {
  display: flex;
  align-items: center;
  color: var(--el-text-color-placeholder);
}

.node-connector {
  position: absolute;
  left: 9px;
  bottom: 0;
  width: 2px;
  height: 16px;
  background: var(--el-border-color);
  margin-bottom: -8px;
}

.flow-node:last-child .node-connector {
  display: none;
}
</style>
