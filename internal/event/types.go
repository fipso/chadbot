package event

import (
	"time"

	pb "github.com/fipso/chadbot/gen/chadbot"
)

// EventType constants
const (
	TypeChatMessageReceived = "chat.message.received"
	TypeChatMessageSent     = "chat.message.sent"
	TypeSkillInvoked        = "skill.invoked"
	TypeSkillCompleted      = "skill.completed"
)

// ChatMessage represents a unified chat message across platforms
type ChatMessage struct {
	Platform    string            `json:"platform"`
	ChatID      string            `json:"chat_id"`
	MessageID   string            `json:"message_id"`
	SenderID    string            `json:"sender_id"`
	SenderName  string            `json:"sender_name"`
	Content     string            `json:"content"`
	ContentType string            `json:"content_type"`
	Timestamp   time.Time         `json:"timestamp"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ToProto converts ChatMessage to protobuf
func (m *ChatMessage) ToProto() *pb.ChatMessageEvent {
	return &pb.ChatMessageEvent{
		Platform:    m.Platform,
		ChatId:      m.ChatID,
		MessageId:   m.MessageID,
		SenderId:    m.SenderID,
		SenderName:  m.SenderName,
		Content:     m.Content,
		ContentType: m.ContentType,
		ReplyTo:     m.ReplyTo,
		Metadata:    m.Metadata,
	}
}

// ChatMessageFromProto creates ChatMessage from protobuf
func ChatMessageFromProto(p *pb.ChatMessageEvent) *ChatMessage {
	return &ChatMessage{
		Platform:    p.Platform,
		ChatID:      p.ChatId,
		MessageID:   p.MessageId,
		SenderID:    p.SenderId,
		SenderName:  p.SenderName,
		Content:     p.Content,
		ContentType: p.ContentType,
		ReplyTo:     p.ReplyTo,
		Metadata:    p.Metadata,
	}
}
