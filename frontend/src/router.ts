import { createRouter, createWebHistory } from 'vue-router'
import ChatPage from './pages/ChatPage.vue'
import StatusPage from './pages/StatusPage.vue'

const routes = [
  { path: '/', name: 'chat', component: ChatPage },
  { path: '/status', name: 'status', component: StatusPage }
]

export const router = createRouter({
  history: createWebHistory(),
  routes
})
