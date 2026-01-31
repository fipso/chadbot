const API_BASE = `http://${window.location.hostname}:8080`

export interface Chat {
  id: string
  user_id: string
  name: string
  created_at: string
  updated_at: string
  messages: Message[]
}

export interface Attachment {
  type: string
  mime_type: string
  data: string  // base64 encoded
  url?: string
  name?: string
}

export interface Message {
  id: string
  chat_id: string
  role: 'user' | 'assistant' | 'plugin'
  content: string
  created_at: string
  display_only?: boolean
  attachments?: string  // JSON string of Attachment[] from backend
  tool_calls?: string   // JSON string of ToolCallRecord[] from backend
  soul?: string
  provider?: string
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
  type: 'bool' | 'string' | 'number' | 'string_array'
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

export async function exportConfig(): Promise<Blob> {
  const res = await fetch(`${API_BASE}/api/config/export`)
  if (!res.ok) throw new Error('Failed to export config')
  return res.blob()
}

export async function importConfig(file: File): Promise<void> {
  const formData = new FormData()
  formData.append('file', file)
  const res = await fetch(`${API_BASE}/api/config/import`, {
    method: 'POST',
    body: formData
  })
  if (!res.ok) throw new Error('Failed to import config')
}

// Souls API
export interface Soul {
  name: string
  content: string
}

export async function fetchSouls(): Promise<Soul[]> {
  const res = await fetch(`${API_BASE}/api/souls`)
  if (!res.ok) throw new Error('Failed to fetch souls')
  return res.json()
}

export async function fetchSoul(name: string): Promise<Soul> {
  const res = await fetch(`${API_BASE}/api/souls/${encodeURIComponent(name)}`)
  if (!res.ok) throw new Error('Failed to fetch soul')
  return res.json()
}

export async function createSoul(name: string, content: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/souls`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, content })
  })
  if (!res.ok) throw new Error('Failed to create soul')
}

export async function updateSoul(name: string, content: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/souls/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content })
  })
  if (!res.ok) throw new Error('Failed to update soul')
}

export async function deleteSoul(name: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/souls/${encodeURIComponent(name)}`, {
    method: 'DELETE'
  })
  if (!res.ok) throw new Error('Failed to delete soul')
}
