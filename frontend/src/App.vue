<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useChatStore } from './stores/chat'

const chatStore = useChatStore()

onMounted(() => {
  chatStore.connect()
})

onUnmounted(() => {
  chatStore.disconnect()
})
</script>

<template>
  <div class="app">
    <router-view />
  </div>
</template>

<style scoped>
.app {
  display: flex;
  height: 100vh;
}
</style>
