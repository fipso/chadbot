<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useChatStore } from './stores/chat'

const chatStore = useChatStore()

onMounted(() => {
  // Apply dark class to html element
  document.documentElement.classList.add('dark')
  chatStore.connect()
})

onUnmounted(() => {
  chatStore.disconnect()
})
</script>

<template>
  <el-config-provider namespace="el">
    <router-view />
  </el-config-provider>
</template>

<style scoped>
</style>
