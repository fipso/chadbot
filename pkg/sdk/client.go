package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/fipso/chadbot/gen/chadbot"
)

// SkillHandler is a function that handles skill invocations
type SkillHandler func(ctx context.Context, args map[string]string) (string, error)

// SkillInvocation contains the full context of a skill invocation
type SkillInvocation struct {
	Args      map[string]string
	ChatID    string // The chat ID where this skill was invoked (may be empty)
	UserID    string // The user ID who invoked the skill (may be empty)
	RequestID string // Unique request ID for this invocation
}

// SkillHandlerWithContext is an extended handler that receives invocation context
type SkillHandlerWithContext func(ctx context.Context, inv *SkillInvocation) (string, error)

// EventHandler is a function that handles events
type EventHandler func(event *pb.Event)

// ConfigChangedHandler is called when a config value changes
type ConfigChangedHandler func(key, value string, allValues map[string]string)

// Client is the SDK client for building plugins
type Client struct {
	name          string
	version       string
	description   string
	documentation string // PLUGIN.md content
	socket        string
	pluginID      string

	conn   *grpc.ClientConn
	client pb.PluginServiceClient
	stream pb.PluginService_ConnectClient

	mu                        sync.RWMutex
	skills                    map[string]*pb.Skill
	skillHandlers             map[string]SkillHandler
	skillHandlersWithContext  map[string]SkillHandlerWithContext
	eventHandlers             []EventHandler

	// Chat service handlers
	chatLLMHandler    ChatLLMResponseHandler
	pendingChatReqs   map[string]chan *pb.ChatGetOrCreateResponse
	pendingAddMsgReqs map[string]chan *pb.ChatAddMessageResponse
	pendingLLMReqs    map[string]chan *pb.ChatLLMResponse

	// Storage handlers
	pendingStorageReqs map[string]chan *pb.StorageResponse

	// Config
	configSchema         *pb.ConfigSchema
	configValues         map[string]string
	configChangedHandler ConfigChangedHandler
	pendingConfigReqs    map[string]chan *pb.ConfigGetResponse

	// Run loop
	ctx       context.Context
	cancel    context.CancelFunc
	runErr    error
	runErrMu  sync.Mutex
	runDone   chan struct{}

	// Queued messages received during config wait
	queuedMessages []*pb.BackendMessage
	queuedMu       sync.Mutex
}

// NewClient creates a new plugin client
func NewClient(name, version, description string) *Client {
	return &Client{
		name:                     name,
		version:                  version,
		description:              description,
		socket:                   "/tmp/chadbot.sock",
		skills:                   make(map[string]*pb.Skill),
		skillHandlers:            make(map[string]SkillHandler),
		skillHandlersWithContext: make(map[string]SkillHandlerWithContext),
		pendingChatReqs:          make(map[string]chan *pb.ChatGetOrCreateResponse),
		pendingAddMsgReqs:        make(map[string]chan *pb.ChatAddMessageResponse),
		pendingLLMReqs:           make(map[string]chan *pb.ChatLLMResponse),
		pendingStorageReqs:       make(map[string]chan *pb.StorageResponse),
		configValues:             make(map[string]string),
		pendingConfigReqs:        make(map[string]chan *pb.ConfigGetResponse),
		runDone:                  make(chan struct{}),
	}
}

// WithSocket sets a custom socket path
func (c *Client) WithSocket(socket string) *Client {
	c.socket = socket
	return c
}

// SetDocumentation sets the plugin documentation (PLUGIN.md content)
// This should be called before Connect()
func (c *Client) SetDocumentation(markdown string) *Client {
	c.documentation = markdown
	return c
}

// RegisterSkill registers a skill with the backend
func (c *Client) RegisterSkill(skill *pb.Skill, handler SkillHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.skills[skill.Name] = skill
	c.skillHandlers[skill.Name] = handler
}

// RegisterSkillWithContext registers a skill with access to invocation context (chat_id, user_id, etc.)
func (c *Client) RegisterSkillWithContext(skill *pb.Skill, handler SkillHandlerWithContext) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.skills[skill.Name] = skill
	c.skillHandlersWithContext[skill.Name] = handler
}

// OnEvent registers an event handler
func (c *Client) OnEvent(handler EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eventHandlers = append(c.eventHandlers, handler)
}

// Connect connects to the chadbot backend and starts the message processing loop
func (c *Client) Connect(ctx context.Context) error {
	// Use unix:// scheme for Unix socket connections
	target := "unix://" + c.socket

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	c.conn = conn
	c.client = pb.NewPluginServiceClient(conn)

	// Create cancellable context for the stream
	c.ctx, c.cancel = context.WithCancel(ctx)

	stream, err := c.client.Connect(c.ctx)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	c.stream = stream

	// Register plugin
	if err := c.register(); err != nil {
		return err
	}

	// Send documentation if set
	if err := c.sendDocumentation(); err != nil {
		return err
	}

	// Register skills
	if err := c.registerSkills(); err != nil {
		return err
	}

	// Note: processMessages() is started in Run(), not here
	// This allows RegisterConfig() to read from the stream without races

	return nil
}

// sendDocumentation sends the plugin documentation to the backend
func (c *Client) sendDocumentation() error {
	if c.documentation == "" {
		return nil
	}

	if err := c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_Documentation{
			Documentation: &pb.PluginDocumentation{
				Content: c.documentation,
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to send documentation: %w", err)
	}

	log.Printf("[SDK] Documentation sent")
	return nil
}

func (c *Client) register() error {
	if err := c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_Register{
			Register: &pb.RegisterRequest{
				Name:        c.name,
				Version:     c.version,
				Description: c.description,
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to send register: %w", err)
	}

	msg, err := c.stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive register response: %w", err)
	}

	resp := msg.GetRegisterResponse()
	if resp == nil || !resp.Success {
		return fmt.Errorf("registration failed")
	}

	c.pluginID = resp.PluginId
	log.Printf("[SDK] Registered as %s (id: %s)", c.name, c.pluginID)
	return nil
}

func (c *Client) registerSkills() error {
	c.mu.RLock()
	skills := make([]*pb.Skill, 0, len(c.skills))
	for _, skill := range c.skills {
		skills = append(skills, skill)
	}
	c.mu.RUnlock()

	if len(skills) == 0 {
		return nil
	}

	if err := c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_SkillRegister{
			SkillRegister: &pb.SkillRegister{
				Skills: skills,
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to register skills: %w", err)
	}

	log.Printf("[SDK] Registered %d skills", len(skills))
	return nil
}

// Subscribe subscribes to event patterns
func (c *Client) Subscribe(patterns []string) error {
	return c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_EventSubscribe{
			EventSubscribe: &pb.EventSubscribe{
				EventTypes: patterns,
			},
		},
	})
}

// Emit emits an event
func (c *Client) Emit(event *pb.Event) error {
	return c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_EventEmit{
			EventEmit: &pb.EventEmit{
				Event: event,
			},
		},
	})
}

// Run blocks until the client is stopped or encounters an error
func (c *Client) Run(ctx context.Context) error {
	// Start message processing loop now (after Connect and RegisterConfig are done)
	go c.processMessages()

	select {
	case <-ctx.Done():
		c.cancel()
		<-c.runDone
		return ctx.Err()
	case <-c.runDone:
		c.runErrMu.Lock()
		err := c.runErr
		c.runErrMu.Unlock()
		return err
	}
}

// processMessages is the internal message processing loop
func (c *Client) processMessages() {
	defer close(c.runDone)

	// First, process any messages that were queued during config wait
	c.queuedMu.Lock()
	queued := c.queuedMessages
	c.queuedMessages = nil
	c.queuedMu.Unlock()

	for _, msg := range queued {
		log.Printf("[SDK] Processing queued message: %T", msg.Payload)
		c.handleMessage(msg)
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			msg, err := c.stream.Recv()
			if err != nil {
				c.runErrMu.Lock()
				c.runErr = fmt.Errorf("stream error: %w", err)
				c.runErrMu.Unlock()
				return
			}

			c.handleMessage(msg)
		}
	}
}

// handleMessage processes a single backend message
func (c *Client) handleMessage(msg *pb.BackendMessage) {
	switch payload := msg.Payload.(type) {
	case *pb.BackendMessage_SkillInvoke:
		go c.handleSkillInvoke(c.ctx, payload.SkillInvoke)
	case *pb.BackendMessage_EventDispatch:
		go c.handleEventDispatch(payload.EventDispatch)
	case *pb.BackendMessage_Error:
		log.Printf("[SDK] Error from backend: %s", payload.Error.Message)

	// Chat service responses
	case *pb.BackendMessage_ChatGetOrCreateResponse:
		c.mu.Lock()
		if ch, ok := c.pendingChatReqs[payload.ChatGetOrCreateResponse.RequestId]; ok {
			ch <- payload.ChatGetOrCreateResponse
			delete(c.pendingChatReqs, payload.ChatGetOrCreateResponse.RequestId)
		}
		c.mu.Unlock()

	case *pb.BackendMessage_ChatAddMessageResponse:
		c.mu.Lock()
		if ch, ok := c.pendingAddMsgReqs[payload.ChatAddMessageResponse.RequestId]; ok {
			ch <- payload.ChatAddMessageResponse
			delete(c.pendingAddMsgReqs, payload.ChatAddMessageResponse.RequestId)
		}
		c.mu.Unlock()

	case *pb.BackendMessage_ChatLlmResponse:
		resp := payload.ChatLlmResponse
		// Check for pending sync requests first
		c.mu.Lock()
		if ch, ok := c.pendingLLMReqs[resp.RequestId]; ok {
			ch <- resp
			delete(c.pendingLLMReqs, resp.RequestId)
			c.mu.Unlock()
			return
		}
		handler := c.chatLLMHandler
		c.mu.Unlock()
		if handler != nil {
			go handler(resp)
		}

	// Storage responses
	case *pb.BackendMessage_StorageResponse:
		c.mu.Lock()
		if ch, ok := c.pendingStorageReqs[payload.StorageResponse.RequestId]; ok {
			ch <- payload.StorageResponse
			delete(c.pendingStorageReqs, payload.StorageResponse.RequestId)
		}
		c.mu.Unlock()

	// Config responses
	case *pb.BackendMessage_ConfigGetResponse:
		resp := payload.ConfigGetResponse
		if resp.Success && resp.Config != nil {
			c.mu.Lock()
			for k, v := range resp.Config.Values {
				c.configValues[k] = v
			}
			c.mu.Unlock()
		}
		// Also handle pending requests
		c.mu.Lock()
		if ch, ok := c.pendingConfigReqs[resp.RequestId]; ok {
			ch <- resp
			delete(c.pendingConfigReqs, resp.RequestId)
		}
		c.mu.Unlock()

	case *pb.BackendMessage_ConfigChanged:
		changed := payload.ConfigChanged
		c.mu.Lock()
		c.configValues[changed.Key] = changed.Value
		if changed.AllValues != nil {
			for k, v := range changed.AllValues.Values {
				c.configValues[k] = v
			}
		}
		handler := c.configChangedHandler
		c.mu.Unlock()
		if handler != nil {
			go handler(changed.Key, changed.Value, c.configValues)
		}
	}
}

func (c *Client) handleSkillInvoke(ctx context.Context, invoke *pb.SkillInvoke) {
	c.mu.RLock()
	handler, hasHandler := c.skillHandlers[invoke.SkillName]
	handlerWithCtx, hasHandlerWithCtx := c.skillHandlersWithContext[invoke.SkillName]
	c.mu.RUnlock()

	var result string
	var errMsg string
	success := true

	if !hasHandler && !hasHandlerWithCtx {
		success = false
		errMsg = fmt.Sprintf("skill %s not found", invoke.SkillName)
	} else {
		var err error
		if hasHandlerWithCtx {
			// Use context-aware handler
			inv := &SkillInvocation{
				Args:      invoke.Arguments,
				RequestID: invoke.RequestId,
			}
			if invoke.Context != nil {
				inv.ChatID = invoke.Context.ChatId
				inv.UserID = invoke.Context.UserId
			}
			result, err = handlerWithCtx(ctx, inv)
		} else {
			// Use simple handler
			result, err = handler(ctx, invoke.Arguments)
		}
		if err != nil {
			success = false
			errMsg = err.Error()
		}
	}

	c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_SkillResponse{
			SkillResponse: &pb.SkillResponse{
				RequestId: invoke.RequestId,
				Success:   success,
				Result:    result,
				Error:     errMsg,
			},
		},
	})
}

func (c *Client) handleEventDispatch(dispatch *pb.EventDispatch) {
	c.mu.RLock()
	handlers := make([]EventHandler, len(c.eventHandlers))
	copy(handlers, c.eventHandlers)
	c.mu.RUnlock()

	for _, handler := range handlers {
		handler(dispatch.Event)
	}
}

// Close closes the client connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// PluginID returns the assigned plugin ID
func (c *Client) PluginID() string {
	return c.pluginID
}

// ChatLLMResponseHandler handles LLM responses from chat service
type ChatLLMResponseHandler func(resp *pb.ChatLLMResponse)

// OnChatLLMResponse registers a handler for chat LLM responses
func (c *Client) OnChatLLMResponse(handler ChatLLMResponseHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chatLLMHandler = handler
}

// ChatGetOrCreate gets or creates a chat linked to a messenger
func (c *Client) ChatGetOrCreate(platform, linkedID, name string) (*pb.ChatGetOrCreateResponse, error) {
	reqID := fmt.Sprintf("chat_goc_%d", time.Now().UnixNano())
	if err := c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_ChatGetOrCreate{
			ChatGetOrCreate: &pb.ChatGetOrCreateRequest{
				RequestId: reqID,
				Platform:  platform,
				LinkedId:  linkedID,
				Name:      name,
			},
		},
	}); err != nil {
		return nil, err
	}

	// Wait for response (blocking for now - could be improved with channels)
	c.mu.Lock()
	c.pendingChatReqs[reqID] = make(chan *pb.ChatGetOrCreateResponse, 1)
	ch := c.pendingChatReqs[reqID]
	c.mu.Unlock()

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("timeout waiting for chat response")
	}
}

// ChatAddMessageOptions contains optional parameters for ChatAddMessage
type ChatAddMessageOptions struct {
	DisplayOnly bool              // If true, message is shown in UI but not sent to LLM
	Attachments []*pb.Attachment  // Optional attachments (images, files, etc.)
}

// ChatAddMessage adds a message to a chat
func (c *Client) ChatAddMessage(chatID, role, content string, opts ...ChatAddMessageOptions) (*pb.ChatAddMessageResponse, error) {
	req := &pb.ChatAddMessageRequest{
		RequestId: fmt.Sprintf("chat_add_%d", time.Now().UnixNano()),
		ChatId:    chatID,
		Role:      role,
		Content:   content,
	}

	// Apply options if provided
	if len(opts) > 0 {
		req.DisplayOnly = opts[0].DisplayOnly
		req.Attachments = opts[0].Attachments
	}

	if err := c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_ChatAddMessage{
			ChatAddMessage: req,
		},
	}); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.pendingAddMsgReqs[req.RequestId] = make(chan *pb.ChatAddMessageResponse, 1)
	ch := c.pendingAddMsgReqs[req.RequestId]
	c.mu.Unlock()

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("timeout waiting for add message response")
	}
}

// ChatLLMRequest requests an LLM response for a chat (async, use OnChatLLMResponse to handle)
func (c *Client) ChatLLMRequest(chatID, provider string) error {
	reqID := fmt.Sprintf("chat_llm_%d", time.Now().UnixNano())
	return c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_ChatLlmRequest{
			ChatLlmRequest: &pb.ChatLLMRequest{
				RequestId: reqID,
				ChatId:    chatID,
				Provider:  provider,
			},
		},
	})
}

// ChatLLMRequestSync requests an LLM response and waits for it synchronously
func (c *Client) ChatLLMRequestSync(chatID, provider string, timeout time.Duration) (*pb.ChatLLMResponse, error) {
	reqID := fmt.Sprintf("chat_llm_%d", time.Now().UnixNano())

	c.mu.Lock()
	ch := make(chan *pb.ChatLLMResponse, 1)
	c.pendingLLMReqs[reqID] = ch
	c.mu.Unlock()

	if err := c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_ChatLlmRequest{
			ChatLlmRequest: &pb.ChatLLMRequest{
				RequestId: reqID,
				ChatId:    chatID,
				Provider:  provider,
			},
		},
	}); err != nil {
		c.mu.Lock()
		delete(c.pendingLLMReqs, reqID)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if !resp.Success {
			return nil, fmt.Errorf("LLM request failed: %s", resp.Error)
		}
		return resp, nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pendingLLMReqs, reqID)
		c.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for LLM response")
	}
}

// Storage provides access to plugin-namespaced storage
func (c *Client) Storage() *StorageClient {
	return &StorageClient{client: c}
}

// StorageClient provides storage operations for plugins
type StorageClient struct {
	client     *Client
	requestSeq int64
	mu         sync.Mutex
}

func (s *StorageClient) nextRequestID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestSeq++
	return fmt.Sprintf("storage_%d", s.requestSeq)
}

func (s *StorageClient) waitForResponse(reqID string) (*pb.StorageResponse, error) {
	s.client.mu.Lock()
	ch := make(chan *pb.StorageResponse, 1)
	s.client.pendingStorageReqs[reqID] = ch
	s.client.mu.Unlock()

	select {
	case resp := <-ch:
		if !resp.Success {
			return nil, fmt.Errorf("storage error: %s", resp.Error)
		}
		return resp, nil
	case <-time.After(10 * time.Second):
		s.client.mu.Lock()
		delete(s.client.pendingStorageReqs, reqID)
		s.client.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for storage response")
	}
}

// CreateTable creates a new table in plugin namespace
func (s *StorageClient) CreateTable(name string, columns []*pb.ColumnDef, ifNotExists bool) error {
	reqID := s.nextRequestID()

	s.client.mu.Lock()
	ch := make(chan *pb.StorageResponse, 1)
	s.client.pendingStorageReqs[reqID] = ch
	s.client.mu.Unlock()

	if err := s.client.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_StorageRequest{
			StorageRequest: &pb.StorageRequest{
				RequestId: reqID,
				Operation: &pb.StorageRequest_CreateTable{
					CreateTable: &pb.CreateTableRequest{
						TableName:   name,
						Columns:     columns,
						IfNotExists: ifNotExists,
					},
				},
			},
		},
	}); err != nil {
		return err
	}

	_, err := s.waitForResponseCh(ch, reqID)
	return err
}

func (s *StorageClient) waitForResponseCh(ch chan *pb.StorageResponse, reqID string) (*pb.StorageResponse, error) {
	select {
	case resp := <-ch:
		if !resp.Success {
			return nil, fmt.Errorf("storage error: %s", resp.Error)
		}
		return resp, nil
	case <-time.After(10 * time.Second):
		s.client.mu.Lock()
		delete(s.client.pendingStorageReqs, reqID)
		s.client.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for storage response")
	}
}

// Insert inserts a row into a table
func (s *StorageClient) Insert(table string, values map[string]string) error {
	reqID := s.nextRequestID()

	s.client.mu.Lock()
	ch := make(chan *pb.StorageResponse, 1)
	s.client.pendingStorageReqs[reqID] = ch
	s.client.mu.Unlock()

	if err := s.client.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_StorageRequest{
			StorageRequest: &pb.StorageRequest{
				RequestId: reqID,
				Operation: &pb.StorageRequest_Insert{
					Insert: &pb.InsertRequest{
						TableName: table,
						Values:    values,
					},
				},
			},
		},
	}); err != nil {
		return err
	}

	_, err := s.waitForResponseCh(ch, reqID)
	return err
}

// Update updates rows in a table
func (s *StorageClient) Update(table string, values map[string]string, where string, whereArgs ...string) error {
	reqID := s.nextRequestID()

	s.client.mu.Lock()
	ch := make(chan *pb.StorageResponse, 1)
	s.client.pendingStorageReqs[reqID] = ch
	s.client.mu.Unlock()

	if err := s.client.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_StorageRequest{
			StorageRequest: &pb.StorageRequest{
				RequestId: reqID,
				Operation: &pb.StorageRequest_Update{
					Update: &pb.UpdateRequest{
						TableName:   table,
						Values:      values,
						WhereClause: where,
						WhereArgs:   whereArgs,
					},
				},
			},
		},
	}); err != nil {
		return err
	}

	_, err := s.waitForResponseCh(ch, reqID)
	return err
}

// Delete deletes rows from a table
func (s *StorageClient) Delete(table string, where string, whereArgs ...string) error {
	reqID := s.nextRequestID()

	s.client.mu.Lock()
	ch := make(chan *pb.StorageResponse, 1)
	s.client.pendingStorageReqs[reqID] = ch
	s.client.mu.Unlock()

	if err := s.client.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_StorageRequest{
			StorageRequest: &pb.StorageRequest{
				RequestId: reqID,
				Operation: &pb.StorageRequest_Delete{
					Delete: &pb.DeleteRequest{
						TableName:   table,
						WhereClause: where,
						WhereArgs:   whereArgs,
					},
				},
			},
		},
	}); err != nil {
		return err
	}

	_, err := s.waitForResponseCh(ch, reqID)
	return err
}

// Query queries a table and returns the rows
func (s *StorageClient) Query(table string, columns []string, where string, whereArgs []string, orderBy string, limit, offset int32) ([]*pb.Row, error) {
	reqID := s.nextRequestID()

	s.client.mu.Lock()
	ch := make(chan *pb.StorageResponse, 1)
	s.client.pendingStorageReqs[reqID] = ch
	s.client.mu.Unlock()

	if err := s.client.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_StorageRequest{
			StorageRequest: &pb.StorageRequest{
				RequestId: reqID,
				Operation: &pb.StorageRequest_Query{
					Query: &pb.QueryRequest{
						TableName:   table,
						Columns:     columns,
						WhereClause: where,
						WhereArgs:   whereArgs,
						OrderBy:     orderBy,
						Limit:       limit,
						Offset:      offset,
					},
				},
			},
		},
	}); err != nil {
		return nil, err
	}

	resp, err := s.waitForResponseCh(ch, reqID)
	if err != nil {
		return nil, err
	}
	return resp.Rows, nil
}

// ConfigField defines a config field for the plugin
type ConfigField struct {
	Key          string
	Label        string
	Description  string
	Type         pb.ConfigFieldType
	DefaultValue string
}

// RegisterConfig registers the plugin's config schema and waits for initial config values
func (c *Client) RegisterConfig(fields []ConfigField) error {
	schema := &pb.ConfigSchema{
		Fields: make([]*pb.ConfigField, len(fields)),
	}
	for i, f := range fields {
		schema.Fields[i] = &pb.ConfigField{
			Key:          f.Key,
			Label:        f.Label,
			Description:  f.Description,
			Type:         f.Type,
			DefaultValue: f.DefaultValue,
		}
	}
	c.configSchema = schema

	if err := c.stream.Send(&pb.PluginMessage{
		Payload: &pb.PluginMessage_ConfigSchema{ConfigSchema: schema},
	}); err != nil {
		return err
	}

	// Wait for config response from backend
	// The backend sends ConfigGetResponse immediately after receiving ConfigSchema
	// Since processMessages() hasn't started yet, we can read directly from the stream
	msg, err := c.stream.Recv()
	if err != nil {
		return fmt.Errorf("error receiving config response: %w", err)
	}

	if resp, ok := msg.Payload.(*pb.BackendMessage_ConfigGetResponse); ok {
		if resp.ConfigGetResponse.Success && resp.ConfigGetResponse.Config != nil {
			c.mu.Lock()
			for k, v := range resp.ConfigGetResponse.Config.Values {
				c.configValues[k] = v
			}
			c.mu.Unlock()
			log.Printf("[SDK] Received initial config: %d values", len(resp.ConfigGetResponse.Config.Values))
		}
	} else {
		// Unexpected message - queue it for later processing
		log.Printf("[SDK] Unexpected message during config wait: %T (queued)", msg.Payload)
		c.queuedMu.Lock()
		c.queuedMessages = append(c.queuedMessages, msg)
		c.queuedMu.Unlock()
	}

	return nil
}

// OnConfigChanged registers a handler for config changes
func (c *Client) OnConfigChanged(handler ConfigChangedHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configChangedHandler = handler
}

// GetConfig returns a config value
func (c *Client) GetConfig(key string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.configValues[key]
}

// GetConfigBool returns a config value as bool
func (c *Client) GetConfigBool(key string) bool {
	return c.GetConfig(key) == "true"
}

// GetAllConfig returns all config values
func (c *Client) GetAllConfig() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range c.configValues {
		result[k] = v
	}
	return result
}

// GetConfigStringArray returns a config value parsed as a string array
// The value is expected to be stored as a JSON array string
func (c *Client) GetConfigStringArray(key string) []string {
	value := c.GetConfig(key)
	if value == "" {
		return nil
	}

	var arr []string
	if err := json.Unmarshal([]byte(value), &arr); err != nil {
		return nil
	}
	return arr
}
