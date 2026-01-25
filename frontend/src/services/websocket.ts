export interface WSMessage {
  type: string
  payload: unknown
}

export interface ChatMessagePayload {
  id: string
  chat_id: string
  content: string
  role: 'user' | 'assistant'
  created_at: string
}

type MessageHandler = (message: WSMessage) => void

class WebSocketService {
  private ws: WebSocket | null = null
  private handlers: Map<string, Set<MessageHandler>> = new Map()
  private reconnectAttempts = 0
  private maxReconnectAttempts = 5
  private reconnectDelay = 1000

  connect(url: string = `ws://${window.location.hostname}:8080/ws`): Promise<void> {
    return new Promise((resolve, reject) => {
      try {
        this.ws = new WebSocket(url)

        this.ws.onopen = () => {
          console.log('[WS] Connected')
          this.reconnectAttempts = 0
          resolve()
        }

        this.ws.onclose = () => {
          console.log('[WS] Disconnected')
          this.scheduleReconnect(url)
        }

        this.ws.onerror = (error) => {
          console.error('[WS] Error:', error)
          reject(error)
        }

        this.ws.onmessage = (event) => {
          try {
            const message: WSMessage = JSON.parse(event.data)
            this.dispatch(message)
          } catch (e) {
            console.error('[WS] Parse error:', e)
          }
        }
      } catch (e) {
        reject(e)
      }
    })
  }

  private scheduleReconnect(url: string) {
    if (this.reconnectAttempts < this.maxReconnectAttempts) {
      this.reconnectAttempts++
      const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1)
      console.log(`[WS] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`)
      setTimeout(() => this.connect(url), delay)
    }
  }

  disconnect() {
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
  }

  send(type: string, payload: unknown) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      console.error('[WS] Not connected')
      return
    }

    this.ws.send(JSON.stringify({ type, payload }))
  }

  sendChatMessage(chatId: string, content: string, provider?: string) {
    this.send('chat.message', { chat_id: chatId, content, provider })
  }

  on(type: string, handler: MessageHandler) {
    if (!this.handlers.has(type)) {
      this.handlers.set(type, new Set())
    }
    this.handlers.get(type)!.add(handler)
  }

  off(type: string, handler: MessageHandler) {
    this.handlers.get(type)?.delete(handler)
  }

  private dispatch(message: WSMessage) {
    // Dispatch to specific type handlers
    this.handlers.get(message.type)?.forEach(handler => handler(message))
    // Dispatch to wildcard handlers
    this.handlers.get('*')?.forEach(handler => handler(message))
  }

  get isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }
}

export const wsService = new WebSocketService()
