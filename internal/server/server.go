package server

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/fipso/chadbot/internal/chat"
	"github.com/fipso/chadbot/internal/event"
	"github.com/fipso/chadbot/internal/llm"
	"github.com/fipso/chadbot/internal/plugin"
	"github.com/fipso/chadbot/internal/storage"
)

// llmAdapter adapts llm.Router to chat.LLMProvider interface
type llmAdapter struct {
	router *llm.Router
}

func (a *llmAdapter) Chat(ctx context.Context, messages []chat.Message, provider string, chatID string) (*chat.Response, error) {
	// Convert chat.Message to llm.Message
	llmMsgs := make([]llm.Message, len(messages))
	for i, m := range messages {
		llmMsgs[i] = llm.Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	// Build chat context
	var chatCtx *llm.ChatContext
	if chatID != "" {
		chatCtx = &llm.ChatContext{ChatID: chatID}
	}

	resp, err := a.router.Chat(ctx, llmMsgs, provider, chatCtx)
	if err != nil {
		return nil, err
	}

	return &chat.Response{Content: resp.Content}, nil
}

const DefaultSocket = "/var/run/chadbot.sock"

// Config holds server configuration
type Config struct {
	Socket       string
	HTTPAddr     string
	OpenAIKey    string
	AnthropicKey string
	ZAIKey       string
	DefaultLLM   string
	DBPath       string
}

// Server is the main chadbot server
type Server struct {
	config    *Config
	grpc      *GRPCServer
	ws        *WebSocketServer
	manager   *plugin.Manager
	registry  *plugin.Registry
	eventBus  *event.Bus
	llmRouter *llm.Router
	handler   *plugin.Handler
}

// New creates a new server instance
func New(config *Config) *Server {
	if config.Socket == "" {
		config.Socket = DefaultSocket
	}
	if config.HTTPAddr == "" {
		config.HTTPAddr = ":8080"
	}
	if config.DBPath == "" {
		config.DBPath = "chadbot.db"
	}

	// Initialize database
	if err := storage.Init(config.DBPath); err != nil {
		log.Fatalf("[Server] Failed to initialize database: %v", err)
	}

	// Initialize core components
	registry := plugin.NewRegistry()
	eventBus := event.NewBus()
	manager := plugin.NewManager(registry, eventBus)

	// Create LLM router
	llmRouter := llm.NewRouter(manager, registry)

	// Create chat service (reuses same logic as web UI)
	chatService := chat.NewService(&llmAdapter{router: llmRouter})

	// Create handler with chat service
	handler := plugin.NewHandler(manager, chatService)

	// Register LLM providers
	if config.OpenAIKey != "" || os.Getenv("OPENAI_API_KEY") != "" {
		llmRouter.RegisterProvider(llm.NewOpenAIProvider(config.OpenAIKey, ""))
	}
	if config.AnthropicKey != "" || os.Getenv("ANTHROPIC_API_KEY") != "" {
		llmRouter.RegisterProvider(llm.NewAnthropicProvider(config.AnthropicKey, ""))
	}
	if config.ZAIKey != "" || os.Getenv("ZAI_API_KEY") != "" {
		llmRouter.RegisterProvider(llm.NewZAIProvider(config.ZAIKey, ""))
	}
	if config.DefaultLLM != "" {
		llmRouter.SetDefaultProvider(config.DefaultLLM)
	}

	// Create servers
	grpc := NewGRPCServer(handler, config.Socket)
	ws := NewWebSocketServer(config.HTTPAddr, eventBus, llmRouter, manager, handler)

	// Wire up WebSocket as message broadcaster for real-time plugin message updates
	chatService.SetBroadcaster(ws)

	return &Server{
		config:    config,
		grpc:      grpc,
		ws:        ws,
		manager:   manager,
		registry:  registry,
		eventBus:  eventBus,
		llmRouter: llmRouter,
		handler:   handler,
	}
}

// Start starts all server components
func (s *Server) Start(ctx context.Context) error {
	log.Println("[Server] Starting chadbot...")

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error, 2)

	// Start gRPC server
	go func() {
		if err := s.grpc.Start(); err != nil {
			errChan <- err
		}
	}()

	// Start WebSocket server
	go func() {
		if err := s.ws.Start(); err != nil {
			errChan <- err
		}
	}()

	// Wait for shutdown signal or error
	select {
	case <-ctx.Done():
		log.Println("[Server] Context cancelled, shutting down...")
	case sig := <-sigChan:
		log.Printf("[Server] Received signal %s, shutting down...", sig)
	case err := <-errChan:
		log.Printf("[Server] Error: %v", err)
		return err
	}

	s.Stop()
	return nil
}

// Stop gracefully stops all server components
func (s *Server) Stop() {
	log.Println("[Server] Stopping...")
	s.grpc.Stop()
	s.ws.Stop()
	log.Println("[Server] Stopped")
}

// Manager returns the plugin manager
func (s *Server) Manager() *plugin.Manager {
	return s.manager
}

// Registry returns the skill registry
func (s *Server) Registry() *plugin.Registry {
	return s.registry
}

// EventBus returns the event bus
func (s *Server) EventBus() *event.Bus {
	return s.eventBus
}

// LLMRouter returns the LLM router
func (s *Server) LLMRouter() *llm.Router {
	return s.llmRouter
}
