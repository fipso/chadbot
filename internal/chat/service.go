package chat

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/internal/storage"
)

// LLMProvider is the interface for LLM chat functionality
type LLMProvider interface {
	Chat(ctx context.Context, messages []Message, providerName string) (*Response, error)
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
	llm LLMProvider
}

// NewService creates a new chat service
func NewService(llm LLMProvider) *Service {
	return &Service{llm: llm}
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

	msg := &storage.Message{
		ID:        uuid.New().String(),
		ChatID:    req.ChatId,
		Role:      req.Role,
		Content:   req.Content,
		CreatedAt: time.Now(),
	}

	if err := storage.AddMessage(msg); err != nil {
		resp.Error = err.Error()
		return resp
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

	// Convert to LLM messages
	messages := make([]Message, len(dbMessages))
	for i, m := range dbMessages {
		messages[i] = Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	// Call LLM
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	llmResp, err := s.llm.Chat(ctx, messages, req.Provider)
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
		resp.Messages[i] = &pb.ChatMessage{
			Id:        m.ID,
			ChatId:    m.ChatID,
			Role:      m.Role,
			Content:   m.Content,
			CreatedAt: m.CreatedAt.Format(time.RFC3339),
		}
	}
	return resp
}
