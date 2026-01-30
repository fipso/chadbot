import { createRouter, createWebHistory } from 'vue-router'
import ChatPage from './pages/ChatPage.vue'
import StatusPage from './pages/StatusPage.vue'
import SoulsPage from './pages/SoulsPage.vue'

const routes = [
  { path: '/', name: 'chat', component: ChatPage },
  { path: '/status', name: 'status', component: StatusPage },
  { path: '/souls', name: 'souls', component: SoulsPage }
]

export const router = createRouter({
  history: createWebHistory(),
  routes
})
