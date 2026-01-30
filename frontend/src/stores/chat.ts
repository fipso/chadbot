import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { wsService, type WSMessage, type ChatMessagePayload, type Attachment } from '../services/websocket'
import * as api from '../services/api'
import type { Provider, Soul } from '../services/api'

export interface ChatMessage {
  id: string
  chat_id: string
  content: string
  role: 'user' | 'assistant' | 'plugin'
  created_at: string
  display_only?: boolean
  attachments?: Attachment[]
  soul?: string
  provider?: string
}

export interface Chat {
  id: string
  name: string
  messages: ChatMessage[]
  created_at: string
  updated_at: string
}

export const useChatStore = defineStore('chat', () => {
  const chats = ref<Map<string, Chat>>(new Map())
  const activeChatId = ref<string | null>(null)
  const isConnected = ref(false)
  const isLoading = ref(false)
  const providers = ref<Provider[]>([])
  const selectedProvider = ref<string>('')
  const souls = ref<Soul[]>([])
  const selectedSoul = ref<string>('default')

  const activeChat = computed(() => {
    if (!activeChatId.value) return null
    return chats.value.get(activeChatId.value) || null
  })

  const chatList = computed(() => {
    return Array.from(chats.value.values()).sort(
      (a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
    )
  })

  // Parse attachments JSON string from API into array
  function parseAttachments(attachmentsJson?: string): Attachment[] | undefined {
    if (!attachmentsJson) return undefined
    try {
      return JSON.parse(attachmentsJson)
    } catch {
      return undefined
    }
  }

  async function loadChats() {
    try {
      const serverChats = await api.fetchChats()
      chats.value.clear()
      for (const chat of serverChats) {
        const messages: ChatMessage[] = (chat.messages || []).map(m => ({
          id: m.id,
          chat_id: m.chat_id,
          content: m.content,
          role: m.role,
          created_at: m.created_at,
          display_only: m.display_only,
          attachments: parseAttachments(m.attachments),
          soul: m.soul,
          provider: m.provider
        }))
        chats.value.set(chat.id, {
          id: chat.id,
          name: chat.name,
          messages,
          created_at: chat.created_at,
          updated_at: chat.updated_at
        })
      }
      // Set active chat to most recent if not set
      if (!activeChatId.value && serverChats.length > 0) {
        activeChatId.value = serverChats[0].id
      }
    } catch (error) {
      console.error('[Chat] Failed to load chats:', error)
    }
  }

  async function createChat(name: string = 'New Chat'): Promise<Chat | null> {
    try {
      const chat = await api.createChat(name)
      const newChat: Chat = {
        id: chat.id,
        name: chat.name,
        messages: [],
        created_at: chat.created_at,
        updated_at: chat.updated_at
      }
      chats.value.set(chat.id, newChat)
      activeChatId.value = chat.id
      return newChat
    } catch (error) {
      console.error('[Chat] Failed to create chat:', error)
      return null
    }
  }

  function setActiveChat(chatId: string) {
    if (chats.value.has(chatId)) {
      activeChatId.value = chatId
    }
  }

  async function deleteChat(chatId: string) {
    try {
      await api.deleteChat(chatId)
      chats.value.delete(chatId)
      if (activeChatId.value === chatId) {
        const remaining = Array.from(chats.value.keys())
        activeChatId.value = remaining.length > 0 ? remaining[0] : null
      }
    } catch (error) {
      console.error('[Chat] Failed to delete chat:', error)
    }
  }

  async function renameChat(chatId: string, name: string) {
    try {
      const updated = await api.renameChat(chatId, name)
      const chat = chats.value.get(chatId)
      if (chat) {
        chat.name = updated.name
        chat.updated_at = updated.updated_at
      }
    } catch (error) {
      console.error('[Chat] Failed to rename chat:', error)
    }
  }

  function addMessage(chatId: string, message: ChatMessage) {
    const chat = chats.value.get(chatId)
    if (!chat) return
    chat.messages.push(message)
    chat.updated_at = message.created_at
  }

  async function loadProviders() {
    try {
      providers.value = await api.fetchProviders()
      // Set default provider
      const defaultProvider = providers.value.find(p => p.is_default)
      if (defaultProvider && !selectedProvider.value) {
        selectedProvider.value = defaultProvider.name
      }
    } catch (error) {
      console.error('[Chat] Failed to load providers:', error)
    }
  }

  async function loadSouls() {
    try {
      souls.value = await api.fetchSouls()
      // Set default soul if not set
      if (!selectedSoul.value || !souls.value.find(s => s.name === selectedSoul.value)) {
        const defaultSoul = souls.value.find(s => s.name === 'default')
        if (defaultSoul) {
          selectedSoul.value = 'default'
        } else if (souls.value.length > 0) {
          selectedSoul.value = souls.value[0].name
        }
      }
    } catch (error) {
      console.error('[Chat] Failed to load souls:', error)
    }
  }

  function setProvider(provider: string) {
    selectedProvider.value = provider
  }

  function setSoul(soul: string) {
    selectedSoul.value = soul
  }

  async function sendMessage(content: string) {
    if (!activeChatId.value || !content.trim()) return

    const chat = chats.value.get(activeChatId.value)
    if (!chat) return

    // Add user message optimistically
    const userMessage: ChatMessage = {
      id: crypto.randomUUID(),
      chat_id: activeChatId.value,
      content,
      role: 'user',
      created_at: new Date().toISOString()
    }
    addMessage(activeChatId.value, userMessage)

    isLoading.value = true

    // Send via WebSocket with selected provider and soul
    wsService.sendChatMessage(activeChatId.value, content, selectedProvider.value, selectedSoul.value)
  }

  async function connect() {
    try {
      await wsService.connect()
      isConnected.value = true

      // Handle incoming messages
      wsService.on('chat.message', (msg: WSMessage) => {
        const payload = msg.payload as ChatMessagePayload
        if (payload.role === 'assistant' || payload.role === 'plugin') {
          addMessage(payload.chat_id, {
            id: payload.id,
            chat_id: payload.chat_id,
            content: payload.content,
            role: payload.role,
            created_at: payload.created_at,
            display_only: payload.display_only,
            attachments: payload.attachments,
            soul: payload.soul,
            provider: payload.provider
          })
          if (payload.role === 'assistant') {
            isLoading.value = false
          }
        }
      })

      wsService.on('chat.error', (msg: WSMessage) => {
        console.error('[Chat] Error:', msg.payload)
        isLoading.value = false
      })

      // Load chats, providers, and souls from server
      await Promise.all([loadChats(), loadProviders(), loadSouls()])

      // Create initial chat if none exists
      if (chats.value.size === 0) {
        await createChat()
      }
    } catch (error) {
      console.error('[Chat] Connection failed:', error)
      isConnected.value = false
    }
  }

  function disconnect() {
    wsService.disconnect()
    isConnected.value = false
  }

  return {
    chats,
    activeChatId,
    activeChat,
    chatList,
    isConnected,
    isLoading,
    providers,
    selectedProvider,
    souls,
    selectedSoul,
    loadChats,
    loadProviders,
    loadSouls,
    createChat,
    setActiveChat,
    deleteChat,
    renameChat,
    addMessage,
    sendMessage,
    setProvider,
    setSoul,
    connect,
    disconnect
  }
})
