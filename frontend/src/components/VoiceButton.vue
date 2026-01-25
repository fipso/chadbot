<script setup lang="ts">
import { ref, onUnmounted } from 'vue'
import { voiceService } from '../services/voice'

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

    // Update audio level for visualization
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

    // Here you would send to backend for transcription
    // For now, we just log the size
    console.log('[Voice] Recorded:', audioBlob.size, 'bytes')

    // Placeholder: In production, send blob to backend
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
  <button
    class="voice-btn"
    :class="{ recording: isRecording }"
    @click="toggleRecording"
    :title="isRecording ? 'Stop recording' : 'Start voice input'"
  >
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
    >
      <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z" />
      <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
      <line x1="12" y1="19" x2="12" y2="23" />
      <line x1="8" y1="23" x2="16" y2="23" />
    </svg>
    <span
      v-if="isRecording"
      class="level-indicator"
      :style="{ transform: `scale(${1 + audioLevel})` }"
    ></span>
  </button>
</template>

<style scoped>
.voice-btn {
  position: relative;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  padding: 0;
}

.voice-btn:hover {
  background: var(--accent);
}

.voice-btn.recording {
  background: var(--accent);
  animation: pulse 1.5s infinite;
}

.level-indicator {
  position: absolute;
  inset: -4px;
  border: 2px solid var(--accent);
  border-radius: 50%;
  pointer-events: none;
  transition: transform 0.1s;
}

@keyframes pulse {
  0%, 100% { box-shadow: 0 0 0 0 rgba(233, 69, 96, 0.4); }
  50% { box-shadow: 0 0 0 8px rgba(233, 69, 96, 0); }
}
</style>
