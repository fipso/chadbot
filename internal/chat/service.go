package chat

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/internal/storage"
)

// LLMProvider is the interface for LLM chat functionality
type LLMProvider interface {
	Chat(ctx context.Context, messages []Message, providerName string, chatID string) (*Response, error)
}

// MessageBroadcaster broadcasts new messages to connected clients
type MessageBroadcaster interface {
	BroadcastMessage(chatID string, msg *storage.Message, attachments []*pb.Attachment)
}

// Message for LLM
type Message struct {
	Role    string
	Content string
}

// Response from LLM
type Response struct {
	Content string
}

// Service handles chat operations for plugins (same logic as web UI)
type Service struct {
	llm         LLMProvider
	broadcaster MessageBroadcaster
}

// NewService creates a new chat service
func NewService(llm LLMProvider) *Service {
	return &Service{llm: llm}
}

// SetBroadcaster sets the message broadcaster for real-time updates
func (s *Service) SetBroadcaster(b MessageBroadcaster) {
	s.broadcaster = b
}

// HandleGetOrCreate handles ChatGetOrCreateRequest
func (s *Service) HandleGetOrCreate(req *pb.ChatGetOrCreateRequest) *pb.ChatGetOrCreateResponse {
	resp := &pb.ChatGetOrCreateResponse{RequestId: req.RequestId}

	userID := req.UserId
	if userID == "" {
		userID = "default"
	}

	chat, created, err := storage.GetOrCreateLinkedChat(req.Platform, req.LinkedId, req.Name, userID)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	resp.Success = true
	resp.ChatId = chat.ID
	resp.Name = chat.Name
	resp.Created = created
	return resp
}

// HandleAddMessage handles ChatAddMessageRequest
func (s *Service) HandleAddMessage(req *pb.ChatAddMessageRequest) *pb.ChatAddMessageResponse {
	resp := &pb.ChatAddMessageResponse{RequestId: req.RequestId}

	// Serialize attachments to JSON if present
	var attachmentsJSON string
	if len(req.Attachments) > 0 {
		data, err := json.Marshal(req.Attachments)
		if err != nil {
			resp.Error = "Failed to serialize attachments: " + err.Error()
			return resp
		}
		attachmentsJSON = string(data)
	}

	msg := &storage.Message{
		ID:          uuid.New().String(),
		ChatID:      req.ChatId,
		Role:        req.Role,
		Content:     req.Content,
		DisplayOnly: req.DisplayOnly,
		Attachments: attachmentsJSON,
		CreatedAt:   time.Now(),
	}

	if err := storage.AddMessage(msg); err != nil {
		resp.Error = err.Error()
		return resp
	}

	// Broadcast to connected WebSocket clients
	if s.broadcaster != nil {
		s.broadcaster.BroadcastMessage(req.ChatId, msg, req.Attachments)
	}

	resp.Success = true
	resp.MessageId = msg.ID
	return resp
}

// HandleLLMRequest handles ChatLLMRequest - gets LLM response for chat
func (s *Service) HandleLLMRequest(req *pb.ChatLLMRequest) *pb.ChatLLMResponse {
	resp := &pb.ChatLLMResponse{RequestId: req.RequestId}

	// Load chat history
	dbMessages, err := storage.GetChatMessages(req.ChatId)
	if err != nil {
		resp.Error = "Failed to load chat history: " + err.Error()
		return resp
	}

	// Convert to LLM messages, filtering out display-only and plugin messages
	messages := make([]Message, 0, len(dbMessages))
	for _, m := range dbMessages {
		// Skip display-only messages and plugin messages (not valid LLM roles)
		if m.DisplayOnly || m.Role == "plugin" {
			continue
		}
		messages = append(messages, Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// Call LLM
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	llmResp, err := s.llm.Chat(ctx, messages, req.Provider, req.ChatId)
	if err != nil {
		resp.Error = "LLM error: " + err.Error()
		return resp
	}

	// Save assistant response
	assistantMsg := &storage.Message{
		ID:        uuid.New().String(),
		ChatID:    req.ChatId,
		Role:      "assistant",
		Content:   llmResp.Content,
		CreatedAt: time.Now(),
	}
	if err := storage.AddMessage(assistantMsg); err != nil {
		log.Printf("[Chat] Failed to save assistant message: %v", err)
	}

	resp.Success = true
	resp.Content = llmResp.Content
	resp.MessageId = assistantMsg.ID
	return resp
}

// HandleGetMessages handles ChatGetMessagesRequest
func (s *Service) HandleGetMessages(req *pb.ChatGetMessagesRequest) *pb.ChatGetMessagesResponse {
	resp := &pb.ChatGetMessagesResponse{RequestId: req.RequestId}

	messages, err := storage.GetChatMessages(req.ChatId)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	resp.Success = true
	resp.Messages = make([]*pb.ChatMessage, len(messages))
	for i, m := range messages {
		msg := &pb.ChatMessage{
			Id:          m.ID,
			ChatId:      m.ChatID,
			Role:        m.Role,
			Content:     m.Content,
			CreatedAt:   m.CreatedAt.Format(time.RFC3339),
			DisplayOnly: m.DisplayOnly,
		}

		// Deserialize attachments from JSON if present
		if m.Attachments != "" {
			var attachments []*pb.Attachment
			if err := json.Unmarshal([]byte(m.Attachments), &attachments); err == nil {
				msg.Attachments = attachments
			}
		}

		resp.Messages[i] = msg
	}
	return resp
}
