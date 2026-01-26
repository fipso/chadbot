<script setup lang="ts">
import { ref, onUnmounted } from 'vue'
import { voiceService } from '../services/voice'
import { Microphone } from '@element-plus/icons-vue'

const emit = defineEmits<{
  result: [text: string]
}>()

const isRecording = ref(false)
const audioLevel = ref(0)
let levelInterval: number | null = null

async function toggleRecording() {
  if (isRecording.value) {
    await stopRecording()
  } else {
    await startRecording()
  }
}

async function startRecording() {
  try {
    await voiceService.startRecording()
    isRecording.value = true

    levelInterval = window.setInterval(() => {
      audioLevel.value = voiceService.getAudioLevel()
    }, 50)
  } catch (error) {
    console.error('[Voice] Failed to start:', error)
  }
}

async function stopRecording() {
  if (levelInterval) {
    clearInterval(levelInterval)
    levelInterval = null
  }

  try {
    const audioBlob = await voiceService.stopRecording()
    isRecording.value = false
    audioLevel.value = 0

    console.log('[Voice] Recorded:', audioBlob.size, 'bytes')
    emit('result', '')
  } catch (error) {
    console.error('[Voice] Failed to stop:', error)
  }
}

onUnmounted(() => {
  if (levelInterval) {
    clearInterval(levelInterval)
  }
  voiceService.cancelRecording()
})
</script>

<template>
  <div class="voice-button-wrapper">
    <el-button
      :type="isRecording ? 'danger' : 'default'"
      circle
      :class="{ recording: isRecording }"
      @click="toggleRecording"
    >
      <el-icon :size="18"><Microphone /></el-icon>
    </el-button>
    <span
      v-if="isRecording"
      class="level-ring"
      :style="{ transform: `scale(${1 + audioLevel * 0.5})` }"
    ></span>
  </div>
</template>

<style scoped>
.voice-button-wrapper {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
}

.voice-button-wrapper .el-button {
  position: relative;
  z-index: 1;
}

.voice-button-wrapper .el-button.recording {
  animation: pulse 1.5s infinite;
}

.level-ring {
  position: absolute;
  inset: -6px;
  border: 2px solid var(--el-color-danger);
  border-radius: 50%;
  pointer-events: none;
  transition: transform 0.1s ease-out;
  opacity: 0.6;
}

@keyframes pulse {
  0%, 100% {
    box-shadow: 0 0 0 0 rgba(var(--el-color-danger-rgb), 0.4);
  }
  50% {
    box-shadow: 0 0 0 8px rgba(var(--el-color-danger-rgb), 0);
  }
}
</style>
