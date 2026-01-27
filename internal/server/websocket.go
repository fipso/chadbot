package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/internal/event"
	"github.com/fipso/chadbot/internal/llm"
	"github.com/fipso/chadbot/internal/plugin"
	"github.com/fipso/chadbot/internal/storage"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// WSClient represents a connected WebSocket client
type WSClient struct {
	ID     string
	UserID string // For now, same as client ID
	Conn   *websocket.Conn
	Send   chan []byte
	Server *WebSocketServer
}

// WSMessage is a message sent over WebSocket
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// IncomingChatMessage from PWA client
type IncomingChatMessage struct {
	ChatID   string `json:"chat_id"`
	Content  string `json:"content"`
	Provider string `json:"provider,omitempty"`
}

// CreateChatRequest for creating a new chat
type CreateChatRequest struct {
	Name string `json:"name"`
}

// WebSocketServer handles PWA WebSocket connections
type WebSocketServer struct {
	mu            sync.RWMutex
	clients       map[string]*WSClient
	eventBus      *event.Bus
	llmRouter     *llm.Router
	pluginManager *plugin.Manager
	pluginHandler *plugin.Handler
	addr          string
	server        *http.Server
}

// NewWebSocketServer creates a new WebSocket server
func NewWebSocketServer(addr string, eventBus *event.Bus, llmRouter *llm.Router, pluginManager *plugin.Manager, pluginHandler *plugin.Handler) *WebSocketServer {
	ws := &WebSocketServer{
		clients:       make(map[string]*WSClient),
		eventBus:      eventBus,
		llmRouter:     llmRouter,
		pluginManager: pluginManager,
		pluginHandler: pluginHandler,
		addr:          addr,
	}

	// Subscribe to chat events to forward to PWA
	eventBus.Subscribe([]string{"chat.message.*"}, func(evt *pb.Event) {
		ws.broadcastEvent(evt)
	})

	return ws
}

// Start starts the WebSocket server
func (s *WebSocketServer) Start() error {
	mux := http.NewServeMux()

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	// REST API endpoints
	mux.HandleFunc("/api/chats", s.handleChats)
	mux.HandleFunc("/api/chats/", s.handleChatByID)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/providers", s.handleProviders)
	mux.HandleFunc("/api/plugins/", s.handlePluginConfig)

	// Health check
	mux.HandleFunc("/health", s.handleHealth)

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	s.server = &http.Server{
		Addr:    s.addr,
		Handler: handler,
	}

	log.Printf("[WebSocket] Server listening on %s", s.addr)
	return s.server.ListenAndServe()
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Stop stops the WebSocket server
func (s *WebSocketServer) Stop() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
	}
}

func (s *WebSocketServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *WebSocketServer) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	providers := s.llmRouter.ListProviders()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(providers)
}

// StatusResponse is the response for /api/status
type StatusResponse struct {
	Plugins []PluginInfo `json:"plugins"`
	Skills  []SkillInfo  `json:"skills"`
}

// PluginInfo represents a connected plugin
type PluginInfo struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description"`
	Subscribed  []string            `json:"subscribed"`
	Config      *PluginConfigInfo   `json:"config,omitempty"`
}

// PluginConfigInfo represents a plugin's configuration
type PluginConfigInfo struct {
	Schema []ConfigFieldInfo `json:"schema"`
	Values map[string]string `json:"values"`
}

// ConfigFieldInfo represents a config field for the web UI
type ConfigFieldInfo struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Type         string `json:"type"` // "bool", "string", "number"
	DefaultValue string `json:"default_value"`
}

// SkillInfo represents a registered skill
type SkillInfo struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	PluginID    string           `json:"plugin_id"`
	PluginName  string           `json:"plugin_name"`
	Parameters  []SkillParamInfo `json:"parameters"`
}

// SkillParamInfo represents a skill parameter
type SkillParamInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func (s *WebSocketServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get plugins
	plugins := s.pluginManager.List()
	pluginInfos := make([]PluginInfo, len(plugins))
	pluginNames := make(map[string]string) // id -> name

	for i, p := range plugins {
		pluginInfos[i] = PluginInfo{
			ID:          p.ID,
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
			Subscribed:  p.Subscribed,
		}
		pluginNames[p.ID] = p.Name

		// Add config info if schema is available
		if p.ConfigSchema != nil && len(p.ConfigSchema.Fields) > 0 {
			schemaFields := make([]ConfigFieldInfo, len(p.ConfigSchema.Fields))
			for j, f := range p.ConfigSchema.Fields {
				fieldType := "string"
				switch f.Type {
				case pb.ConfigFieldType_CONFIG_FIELD_TYPE_BOOL:
					fieldType = "bool"
				case pb.ConfigFieldType_CONFIG_FIELD_TYPE_NUMBER:
					fieldType = "number"
				case pb.ConfigFieldType_CONFIG_FIELD_TYPE_STRING:
					fieldType = "string"
				}
				schemaFields[j] = ConfigFieldInfo{
					Key:          f.Key,
					Label:        f.Label,
					Description:  f.Description,
					Type:         fieldType,
					DefaultValue: f.DefaultValue,
				}
			}

			// Get current config values from storage
			values, _ := storage.GetPluginConfigs(p.Name)
			if values == nil {
				values = make(map[string]string)
			}

			pluginInfos[i].Config = &PluginConfigInfo{
				Schema: schemaFields,
				Values: values,
			}
		}
	}

	// Get skills from registry
	registry := s.pluginManager.Registry()
	skills := registry.ListSkills()
	skillInfos := make([]SkillInfo, len(skills))

	for i, rs := range skills {
		params := make([]SkillParamInfo, len(rs.Skill.Parameters))
		for j, p := range rs.Skill.Parameters {
			params[j] = SkillParamInfo{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
			}
		}

		skillInfos[i] = SkillInfo{
			Name:        rs.Skill.Name,
			Description: rs.Skill.Description,
			PluginID:    rs.PluginID,
			PluginName:  pluginNames[rs.PluginID],
			Parameters:  params,
		}
	}

	resp := StatusResponse{
		Plugins: pluginInfos,
		Skills:  skillInfos,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleChats handles GET /api/chats and POST /api/chats
func (s *WebSocketServer) handleChats(w http.ResponseWriter, r *http.Request) {
	// For now, use a default user ID (in production, extract from auth)
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "default"
	}

	switch r.Method {
	case http.MethodGet:
		chats, err := storage.GetUserChatsWithMessages(userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chats)

	case http.MethodPost:
		var req CreateChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Name == "" {
			req.Name = "New Chat"
		}

		chat := &storage.Chat{
			ID:        uuid.New().String(),
			UserID:    userID,
			Name:      req.Name,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := storage.CreateChat(chat); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(chat)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// UpdateChatRequest for updating a chat
type UpdateChatRequest struct {
	Name string `json:"name"`
}

// handleChatByID handles GET/PUT/DELETE /api/chats/{id}
func (s *WebSocketServer) handleChatByID(w http.ResponseWriter, r *http.Request) {
	// Extract chat ID from URL
	path := strings.TrimPrefix(r.URL.Path, "/api/chats/")
	chatID := strings.TrimSuffix(path, "/")

	if chatID == "" {
		http.Error(w, "Chat ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		chat, err := storage.GetChat(chatID)
		if err != nil {
			http.Error(w, "Chat not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chat)

	case http.MethodPut:
		var req UpdateChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		chat, err := storage.GetChat(chatID)
		if err != nil {
			http.Error(w, "Chat not found", http.StatusNotFound)
			return
		}

		chat.Name = req.Name
		if err := storage.UpdateChat(chat); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chat)

	case http.MethodDelete:
		if err := storage.DeleteChat(chatID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *WebSocketServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebSocket] Upgrade error: %v", err)
		return
	}

	clientID := uuid.New().String()
	// For now, use query param or default user ID
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "default"
	}

	client := &WSClient{
		ID:     clientID,
		UserID: userID,
		Conn:   conn,
		Send:   make(chan []byte, 256),
		Server: s,
	}

	s.mu.Lock()
	s.clients[client.ID] = client
	s.mu.Unlock()

	log.Printf("[WebSocket] Client connected: %s (user: %s)", client.ID, userID)

	go client.writePump()
	go client.readPump()
}

func (c *WSClient) readPump() {
	defer func() {
		c.Server.mu.Lock()
		delete(c.Server.clients, c.ID)
		c.Server.mu.Unlock()
		c.Conn.Close()
		log.Printf("[WebSocket] Client disconnected: %s", c.ID)
	}()

	c.Conn.SetReadLimit(512 * 1024) // 512KB
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WebSocket] Read error: %v", err)
			}
			break
		}

		c.handleMessage(message)
	}
}

func (c *WSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *WSClient) handleMessage(data []byte) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[WebSocket] Invalid message: %v", err)
		return
	}

	switch msg.Type {
	case "chat.message":
		var chatMsg IncomingChatMessage
		if err := json.Unmarshal(msg.Payload, &chatMsg); err != nil {
			log.Printf("[WebSocket] Invalid chat message: %v", err)
			return
		}
		c.handleChatMessage(chatMsg)

	case "ping":
		c.send("pong", nil)

	default:
		log.Printf("[WebSocket] Unknown message type: %s", msg.Type)
	}
}

func (c *WSClient) handleChatMessage(msg IncomingChatMessage) {
	// Save user message to database
	userMsg := &storage.Message{
		ID:        uuid.New().String(),
		ChatID:    msg.ChatID,
		Role:      "user",
		Content:   msg.Content,
		CreatedAt: time.Now(),
	}
	if err := storage.AddMessage(userMsg); err != nil {
		log.Printf("[WebSocket] Failed to save user message: %v", err)
	}

	// Emit event
	event := &pb.Event{
		EventType: "chat.message.received",
		Timestamp: timestamppb.Now(),
		Data: &pb.Event_ChatMessage{
			ChatMessage: &pb.ChatMessageEvent{
				Platform:    "pwa",
				ChatId:      msg.ChatID,
				MessageId:   userMsg.ID,
				SenderId:    c.UserID,
				Content:     msg.Content,
				ContentType: "text",
			},
		},
	}
	c.Server.eventBus.Publish(event)

	// Process with LLM if available
	if c.Server.llmRouter != nil {
		go c.processWithLLM(msg)
	}
}

func (c *WSClient) processWithLLM(msg IncomingChatMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Load chat history from database
	dbMessages, err := storage.GetChatMessages(msg.ChatID)
	if err != nil {
		log.Printf("[WebSocket] Failed to load chat history: %v", err)
		c.send("chat.error", map[string]string{"error": "Failed to load chat history"})
		return
	}

	// Convert to LLM messages, filtering out display-only and plugin messages
	messages := make([]llm.Message, 0, len(dbMessages))
	for _, m := range dbMessages {
		// Skip display-only messages and plugin messages (not valid LLM roles)
		if m.DisplayOnly || m.Role == "plugin" {
			continue
		}
		messages = append(messages, llm.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// If no messages, something went wrong
	if len(messages) == 0 {
		messages = []llm.Message{
			{Role: "user", Content: msg.Content},
		}
	}

	chatCtx := &llm.ChatContext{ChatID: msg.ChatID, UserID: c.UserID}
	resp, err := c.Server.llmRouter.Chat(ctx, messages, msg.Provider, chatCtx)
	if err != nil {
		log.Printf("[WebSocket] LLM error: %v", err)
		c.send("chat.error", map[string]string{"error": err.Error()})
		return
	}

	// Save assistant message to database
	assistantMsg := &storage.Message{
		ID:        uuid.New().String(),
		ChatID:    msg.ChatID,
		Role:      "assistant",
		Content:   resp.Content,
		CreatedAt: time.Now(),
	}
	if err := storage.AddMessage(assistantMsg); err != nil {
		log.Printf("[WebSocket] Failed to save assistant message: %v", err)
	}

	// Send response back
	c.send("chat.message", map[string]interface{}{
		"id":         assistantMsg.ID,
		"chat_id":    msg.ChatID,
		"content":    resp.Content,
		"role":       "assistant",
		"created_at": assistantMsg.CreatedAt,
	})

	// Process deferred attachments (images, etc.) after assistant response
	for _, da := range resp.DeferredAttachments {
		if da.ChatID == "" {
			da.ChatID = msg.ChatID
		}

		// Save to database
		deferredMsg := &storage.Message{
			ID:          uuid.New().String(),
			ChatID:      da.ChatID,
			Role:        da.Role,
			Content:     da.Content,
			DisplayOnly: da.DisplayOnly,
			CreatedAt:   time.Now(),
		}

		// Serialize attachments to JSON
		if len(da.Attachments) > 0 {
			attachmentsJSON, err := json.Marshal(da.Attachments)
			if err == nil {
				deferredMsg.Attachments = string(attachmentsJSON)
			}
		}

		if err := storage.AddMessage(deferredMsg); err != nil {
			log.Printf("[WebSocket] Failed to save deferred message: %v", err)
			continue
		}

		// Broadcast to connected clients
		c.Server.BroadcastMessage(da.ChatID, deferredMsg, da.Attachments)
		log.Printf("[WebSocket] Added deferred attachment message: %s", deferredMsg.ID)
	}

	// Emit as event
	event := &pb.Event{
		EventType: "chat.message.sent",
		Timestamp: timestamppb.Now(),
		Data: &pb.Event_ChatMessage{
			ChatMessage: &pb.ChatMessageEvent{
				Platform:    "pwa",
				ChatId:      msg.ChatID,
				MessageId:   assistantMsg.ID,
				SenderId:    "assistant",
				Content:     resp.Content,
				ContentType: "text",
			},
		},
	}
	c.Server.eventBus.Publish(event)
}

func (c *WSClient) send(msgType string, payload any) {
	var payloadBytes json.RawMessage
	if payload != nil {
		payloadBytes, _ = json.Marshal(payload)
	}

	msg := WSMessage{
		Type:    msgType,
		Payload: payloadBytes,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	select {
	case c.Send <- data:
	default:
		log.Printf("[WebSocket] Client %s send buffer full", c.ID)
	}
}

// SetConfigRequest is the request body for setting plugin config
type SetConfigRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// handlePluginConfig handles GET/PUT /api/plugins/{name}/config
func (s *WebSocketServer) handlePluginConfig(w http.ResponseWriter, r *http.Request) {
	// Extract plugin name from URL: /api/plugins/{name}/config
	path := strings.TrimPrefix(r.URL.Path, "/api/plugins/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "config" {
		http.Error(w, "Invalid URL format. Use /api/plugins/{name}/config", http.StatusBadRequest)
		return
	}
	pluginName := parts[0]

	switch r.Method {
	case http.MethodGet:
		// Get plugin config values
		values, err := storage.GetPluginConfigs(pluginName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if values == nil {
			values = make(map[string]string)
		}

		// Also try to get schema from connected plugin
		var schema []ConfigFieldInfo
		plugin, ok := s.pluginManager.GetByName(pluginName)
		if ok && plugin.ConfigSchema != nil {
			schema = make([]ConfigFieldInfo, len(plugin.ConfigSchema.Fields))
			for i, f := range plugin.ConfigSchema.Fields {
				fieldType := "string"
				switch f.Type {
				case pb.ConfigFieldType_CONFIG_FIELD_TYPE_BOOL:
					fieldType = "bool"
				case pb.ConfigFieldType_CONFIG_FIELD_TYPE_NUMBER:
					fieldType = "number"
				case pb.ConfigFieldType_CONFIG_FIELD_TYPE_STRING:
					fieldType = "string"
				}
				schema[i] = ConfigFieldInfo{
					Key:          f.Key,
					Label:        f.Label,
					Description:  f.Description,
					Type:         fieldType,
					DefaultValue: f.DefaultValue,
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PluginConfigInfo{
			Schema: schema,
			Values: values,
		})

	case http.MethodPut:
		var req SetConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Key == "" {
			http.Error(w, "Key is required", http.StatusBadRequest)
			return
		}

		// Use the handler to set config (saves to DB and notifies plugin)
		if err := s.pluginHandler.SetPluginConfig(pluginName, req.Key, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"key":     req.Key,
			"value":   req.Value,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *WebSocketServer) broadcastEvent(event *pb.Event) {
	data, err := json.Marshal(map[string]any{
		"type": "event",
		"payload": map[string]any{
			"event_type": event.EventType,
			"source":     event.SourcePlugin,
			"data":       event.GetChatMessage(),
		},
	})
	if err != nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, client := range s.clients {
		select {
		case client.Send <- data:
		default:
		}
	}
}

// Broadcast sends a message to all connected clients
func (s *WebSocketServer) Broadcast(msgType string, payload any) {
	var payloadBytes json.RawMessage
	if payload != nil {
		payloadBytes, _ = json.Marshal(payload)
	}

	msg := WSMessage{
		Type:    msgType,
		Payload: payloadBytes,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, client := range s.clients {
		select {
		case client.Send <- data:
		default:
		}
	}
}

// BroadcastMessage broadcasts a chat message to connected WebSocket clients
// This implements chat.MessageBroadcaster interface
func (s *WebSocketServer) BroadcastMessage(chatID string, msg *storage.Message, attachments []*pb.Attachment) {
	// Convert attachments to JSON-friendly format
	var jsonAttachments []map[string]interface{}
	for _, a := range attachments {
		att := map[string]interface{}{
			"type":      a.Type,
			"mime_type": a.MimeType,
		}
		if len(a.Data) > 0 {
			// Encode binary data as base64
			att["data"] = base64.StdEncoding.EncodeToString(a.Data)
		}
		if a.Url != "" {
			att["url"] = a.Url
		}
		if a.Filename != "" {
			att["name"] = a.Filename
		}
		jsonAttachments = append(jsonAttachments, att)
	}

	payload := map[string]interface{}{
		"id":          msg.ID,
		"chat_id":     chatID,
		"content":     msg.Content,
		"role":        msg.Role,
		"created_at":  msg.CreatedAt,
		"display_only": msg.DisplayOnly,
	}
	if len(jsonAttachments) > 0 {
		payload["attachments"] = jsonAttachments
	}

	s.Broadcast("chat.message", payload)
}

