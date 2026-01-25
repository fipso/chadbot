const API_BASE = `http://${window.location.hostname}:8080`

export interface Chat {
  id: string
  user_id: string
  name: string
  created_at: string
  updated_at: string
  messages: Message[]
}

export interface Message {
  id: string
  chat_id: string
  role: 'user' | 'assistant'
  content: string
  created_at: string
}

export async function fetchChats(): Promise<Chat[]> {
  const res = await fetch(`${API_BASE}/api/chats`)
  if (!res.ok) throw new Error('Failed to fetch chats')
  return res.json()
}

export async function createChat(name: string = 'New Chat'): Promise<Chat> {
  const res = await fetch(`${API_BASE}/api/chats`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name })
  })
  if (!res.ok) throw new Error('Failed to create chat')
  return res.json()
}

export async function deleteChat(chatId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/chats/${chatId}`, {
    method: 'DELETE'
  })
  if (!res.ok) throw new Error('Failed to delete chat')
}

export async function fetchChat(chatId: string): Promise<Chat> {
  const res = await fetch(`${API_BASE}/api/chats/${chatId}`)
  if (!res.ok) throw new Error('Failed to fetch chat')
  return res.json()
}

export async function renameChat(chatId: string, name: string): Promise<Chat> {
  const res = await fetch(`${API_BASE}/api/chats/${chatId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name })
  })
  if (!res.ok) throw new Error('Failed to rename chat')
  return res.json()
}

export interface SkillParam {
  name: string
  type: string
  description: string
  required: boolean
}

export interface Skill {
  name: string
  description: string
  plugin_id: string
  plugin_name: string
  parameters: SkillParam[]
}

export interface ConfigField {
  key: string
  label: string
  description: string
  type: 'bool' | 'string' | 'number'
  default_value: string
}

export interface PluginConfig {
  schema: ConfigField[]
  values: Record<string, string>
}

export interface Plugin {
  id: string
  name: string
  version: string
  description: string
  subscribed: string[]
  config?: PluginConfig
}

export interface StatusResponse {
  plugins: Plugin[]
  skills: Skill[]
}

export async function fetchStatus(): Promise<StatusResponse> {
  const res = await fetch(`${API_BASE}/api/status`)
  if (!res.ok) throw new Error('Failed to fetch status')
  return res.json()
}

export async function setPluginConfig(pluginName: string, key: string, value: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/plugins/${pluginName}/config`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ key, value })
  })
  if (!res.ok) throw new Error('Failed to set plugin config')
}

export interface Provider {
  name: string
  is_default: boolean
}

export async function fetchProviders(): Promise<Provider[]> {
  const res = await fetch(`${API_BASE}/api/providers`)
  if (!res.ok) throw new Error('Failed to fetch providers')
  return res.json()
}
