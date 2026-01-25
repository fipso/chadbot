package storage

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB is the global database instance
var DB *gorm.DB

// Init initializes the database connection
func Init(dbPath string) error {
	var err error
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return err
	}

	// Auto-migrate schemas
	if err := DB.AutoMigrate(&Chat{}, &Message{}, &PluginConfig{}); err != nil {
		return err
	}

	log.Printf("[Storage] Database initialized: %s", dbPath)
	return nil
}

// CreateChat creates a new chat
func CreateChat(chat *Chat) error {
	return DB.Create(chat).Error
}

// GetChat retrieves a chat by ID with messages
func GetChat(chatID string) (*Chat, error) {
	var chat Chat
	err := DB.Preload("Messages", func(db *gorm.DB) *gorm.DB {
		return db.Order("created_at ASC")
	}).First(&chat, "id = ?", chatID).Error
	if err != nil {
		return nil, err
	}
	return &chat, nil
}

// GetUserChats retrieves all chats for a user
func GetUserChats(userID string) ([]Chat, error) {
	var chats []Chat
	err := DB.Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&chats).Error
	return chats, err
}

// GetUserChatsWithMessages retrieves all chats for a user with their messages
func GetUserChatsWithMessages(userID string) ([]Chat, error) {
	var chats []Chat
	err := DB.Preload("Messages", func(db *gorm.DB) *gorm.DB {
		return db.Order("created_at ASC")
	}).Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&chats).Error
	return chats, err
}

// UpdateChat updates a chat
func UpdateChat(chat *Chat) error {
	return DB.Save(chat).Error
}

// DeleteChat soft-deletes a chat
func DeleteChat(chatID string) error {
	return DB.Delete(&Chat{}, "id = ?", chatID).Error
}

// AddMessage adds a message to a chat
func AddMessage(msg *Message) error {
	err := DB.Create(msg).Error
	if err != nil {
		return err
	}
	// Update chat's updated_at
	return DB.Model(&Chat{}).Where("id = ?", msg.ChatID).Update("updated_at", msg.CreatedAt).Error
}

// GetChatMessages retrieves all messages for a chat
func GetChatMessages(chatID string) ([]Message, error) {
	var messages []Message
	err := DB.Where("chat_id = ?", chatID).
		Order("created_at ASC").
		Find(&messages).Error
	return messages, err
}

// GetOrCreateLinkedChat finds or creates a chat linked to a messenger
// Returns the chat and a boolean indicating if it was newly created
func GetOrCreateLinkedChat(platform, linkedID, name, userID string) (*Chat, bool, error) {
	var chat Chat
	err := DB.Where("platform = ? AND linked_id = ?", platform, linkedID).First(&chat).Error
	if err == nil {
		return &chat, false, nil
	}

	// Create new linked chat
	chat = Chat{
		ID:       linkedID, // Use linked ID as primary key for simplicity
		UserID:   userID,
		Name:     name,
		Platform: platform,
		LinkedID: linkedID,
	}
	if err := DB.Create(&chat).Error; err != nil {
		return nil, false, err
	}
	return &chat, true, nil
}

// GetPluginConfig gets a single config value for a plugin
func GetPluginConfig(pluginName, key string) (string, error) {
	var config PluginConfig
	err := DB.Where("plugin_name = ? AND key = ?", pluginName, key).First(&config).Error
	if err != nil {
		return "", err
	}
	return config.Value, nil
}

// GetPluginConfigs gets all config values for a plugin
func GetPluginConfigs(pluginName string) (map[string]string, error) {
	var configs []PluginConfig
	err := DB.Where("plugin_name = ?", pluginName).Find(&configs).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, c := range configs {
		result[c.Key] = c.Value
	}
	return result, nil
}

// SetPluginConfig sets a config value for a plugin
func SetPluginConfig(pluginName, key, value string) error {
	var config PluginConfig
	err := DB.Where("plugin_name = ? AND key = ?", pluginName, key).First(&config).Error
	if err != nil {
		// Create new
		config = PluginConfig{
			PluginName: pluginName,
			Key:        key,
			Value:      value,
		}
		return DB.Create(&config).Error
	}
	// Update existing
	config.Value = value
	return DB.Save(&config).Error
}
