package storage

import (
	"time"

	"gorm.io/gorm"
)

// Chat represents a conversation
type Chat struct {
	ID        string         `gorm:"primaryKey" json:"id"`
	UserID    string         `gorm:"index" json:"user_id"`
	Name      string         `json:"name"`
	Platform  string         `gorm:"index" json:"platform"`  // "web", "whatsapp", "telegram", etc.
	LinkedID  string         `gorm:"index" json:"linked_id"` // External chat ID (e.g., WhatsApp JID)
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Messages  []Message      `gorm:"foreignKey:ChatID" json:"messages"`
}

// Message represents a single message in a chat
type Message struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	ChatID    string    `gorm:"index" json:"chat_id"`
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// PluginConfig stores plugin configuration values
type PluginConfig struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	PluginName string   `gorm:"index:idx_plugin_key,unique" json:"plugin_name"`
	Key       string    `gorm:"index:idx_plugin_key,unique" json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}
