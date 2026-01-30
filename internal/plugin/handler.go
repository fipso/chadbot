package plugin

import (
	"fmt"
	"io"
	"log"

	"github.com/google/uuid"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/internal/chat"
	"github.com/fipso/chadbot/internal/config"
	"github.com/fipso/chadbot/internal/storage"
)

// Handler handles gRPC stream connections from plugins
type Handler struct {
	manager        *Manager
	chatService    *chat.Service
	pluginStorages map[string]*storage.PluginStorage
	pluginConfig   *config.PluginConfigManager
}

// NewHandler creates a new plugin handler
func NewHandler(manager *Manager, chatService *chat.Service, pluginConfig *config.PluginConfigManager) *Handler {
	return &Handler{
		manager:        manager,
		chatService:    chatService,
		pluginStorages: make(map[string]*storage.PluginStorage),
		pluginConfig:   pluginConfig,
	}
}

// HandleConnection processes a plugin's bidirectional stream
func (h *Handler) HandleConnection(stream pb.PluginService_ConnectServer) error {
	var plugin *Plugin
	var pluginID string

	defer func() {
		if pluginID != "" {
			h.manager.Unregister(pluginID)
		}
	}()

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			log.Printf("[Handler] Plugin stream closed (id: %s)", pluginID)
			return nil
		}
		if err != nil {
			log.Printf("[Handler] Error receiving message: %v", err)
			return err
		}

		switch payload := msg.Payload.(type) {
		case *pb.PluginMessage_Register:
			pluginID, plugin, err = h.handleRegister(payload.Register, stream)
			if err != nil {
				return err
			}

		case *pb.PluginMessage_SkillRegister:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before registering skills", "")
				continue
			}
			h.handleSkillRegister(pluginID, payload.SkillRegister)

		case *pb.PluginMessage_EventSubscribe:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before subscribing to events", "")
				continue
			}
			h.handleEventSubscribe(pluginID, payload.EventSubscribe)

		case *pb.PluginMessage_EventEmit:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before emitting events", "")
				continue
			}
			h.handleEventEmit(pluginID, payload.EventEmit)

		case *pb.PluginMessage_SkillResponse:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before responding to skills", "")
				continue
			}
			h.handleSkillResponse(payload.SkillResponse)

		case *pb.PluginMessage_StorageRequest:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before using storage", "")
				continue
			}
			h.handleStorageRequest(pluginID, payload.StorageRequest, stream)

		case *pb.PluginMessage_ChatGetOrCreate:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before using chat service", "")
				continue
			}
			resp := h.chatService.HandleGetOrCreate(payload.ChatGetOrCreate)
			stream.Send(&pb.BackendMessage{
				Payload: &pb.BackendMessage_ChatGetOrCreateResponse{ChatGetOrCreateResponse: resp},
			})

		case *pb.PluginMessage_ChatAddMessage:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before using chat service", "")
				continue
			}
			resp := h.chatService.HandleAddMessage(payload.ChatAddMessage)
			stream.Send(&pb.BackendMessage{
				Payload: &pb.BackendMessage_ChatAddMessageResponse{ChatAddMessageResponse: resp},
			})

		case *pb.PluginMessage_ChatLlmRequest:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before using chat service", "")
				continue
			}
			// Run LLM request in goroutine to not block stream
			go func(req *pb.ChatLLMRequest) {
				resp := h.chatService.HandleLLMRequest(req)
				stream.Send(&pb.BackendMessage{
					Payload: &pb.BackendMessage_ChatLlmResponse{ChatLlmResponse: resp},
				})
			}(payload.ChatLlmRequest)

		case *pb.PluginMessage_ChatGetMessages:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before using chat service", "")
				continue
			}
			resp := h.chatService.HandleGetMessages(payload.ChatGetMessages)
			stream.Send(&pb.BackendMessage{
				Payload: &pb.BackendMessage_ChatGetMessagesResponse{ChatGetMessagesResponse: resp},
			})

		case *pb.PluginMessage_ConfigSchema:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before setting config schema", "")
				continue
			}
			h.handleConfigSchema(pluginID, plugin.Name, payload.ConfigSchema, stream)

		case *pb.PluginMessage_ConfigGet:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before getting config", "")
				continue
			}
			h.handleConfigGet(plugin.Name, payload.ConfigGet, stream)

		case *pb.PluginMessage_Documentation:
			if plugin == nil {
				h.sendError(stream, 1, "Must register before setting documentation", "")
				continue
			}
			h.manager.SetDocumentation(pluginID, payload.Documentation.Content)

		default:
			log.Printf("[Handler] Unknown message type from plugin %s", pluginID)
		}
	}
}

func (h *Handler) handleRegister(req *pb.RegisterRequest, stream pb.PluginService_ConnectServer) (string, *Plugin, error) {
	pluginID := uuid.New().String()
	plugin := h.manager.Register(pluginID, req.Name, req.Version, req.Description, stream)

	err := stream.Send(&pb.BackendMessage{
		Payload: &pb.BackendMessage_RegisterResponse{
			RegisterResponse: &pb.RegisterResponse{
				PluginId: pluginID,
				Success:  true,
				Message:  fmt.Sprintf("Plugin %s registered successfully", req.Name),
			},
		},
	})
	if err != nil {
		return "", nil, err
	}

	return pluginID, plugin, nil
}

func (h *Handler) handleSkillRegister(pluginID string, req *pb.SkillRegister) {
	registry := h.manager.Registry()
	plugin, ok := h.manager.Get(pluginID)
	if !ok {
		log.Printf("[Handler] Plugin %s not found for skill registration", pluginID)
		return
	}
	for _, skill := range req.Skills {
		if err := registry.RegisterSkill(pluginID, plugin.Name, skill); err != nil {
			log.Printf("[Handler] Failed to register skill %s: %v", skill.Name, err)
		}
	}
}

func (h *Handler) handleEventSubscribe(pluginID string, req *pb.EventSubscribe) {
	h.manager.SubscribePlugin(pluginID, req.EventTypes)
	log.Printf("[Handler] Plugin %s subscribed to: %v", pluginID, req.EventTypes)
}

func (h *Handler) handleEventEmit(pluginID string, req *pb.EventEmit) {
	event := req.Event
	event.SourcePlugin = pluginID
	h.manager.EventBus().Publish(event)
	log.Printf("[Handler] Plugin %s emitted event: %s", pluginID, event.EventType)
}

func (h *Handler) handleSkillResponse(resp *pb.SkillResponse) {
	if h.manager.ResolvePendingRequest(resp.RequestId, resp) {
		log.Printf("[Handler] Skill response received for request: %s", resp.RequestId)
	} else {
		log.Printf("[Handler] No pending request for: %s", resp.RequestId)
	}
}

func (h *Handler) handleStorageRequest(pluginID string, req *pb.StorageRequest, stream pb.PluginService_ConnectServer) {
	// Get plugin name for storage namespacing (persists across restarts)
	plugin, ok := h.manager.Get(pluginID)
	if !ok {
		stream.Send(&pb.BackendMessage{
			Payload: &pb.BackendMessage_StorageResponse{
				StorageResponse: &pb.StorageResponse{
					RequestId: req.RequestId,
					Success:   false,
					Error:     "plugin not found",
				},
			},
		})
		return
	}

	// Get or create plugin storage handler (keyed by name for persistence)
	ps, ok := h.pluginStorages[plugin.Name]
	if !ok {
		ps = storage.NewPluginStorage(plugin.Name)
		h.pluginStorages[plugin.Name] = ps
	}

	// Process the request
	resp := ps.HandleRequest(req)

	// Send response
	stream.Send(&pb.BackendMessage{
		Payload: &pb.BackendMessage_StorageResponse{
			StorageResponse: resp,
		},
	})
}

func (h *Handler) sendError(stream pb.PluginService_ConnectServer, code int32, message, requestID string) {
	stream.Send(&pb.BackendMessage{
		Payload: &pb.BackendMessage_Error{
			Error: &pb.Error{
				Code:      code,
				Message:   message,
				RequestId: requestID,
			},
		},
	})
}

func (h *Handler) handleConfigSchema(pluginID, pluginName string, schema *pb.ConfigSchema, stream pb.PluginService_ConnectServer) {
	// Store schema in manager
	h.manager.SetConfigSchema(pluginID, schema)

	// Initialize all config fields from schema if they don't exist
	// This ensures the config.toml file is created with all available options
	existingConfigs := h.pluginConfig.GetPluginConfigs(pluginName)
	newConfigs := make(map[string]string)

	for _, field := range schema.Fields {
		if _, exists := existingConfigs[field.Key]; !exists {
			// Add default value (even if empty) so user can see the field in config.toml
			newConfigs[field.Key] = field.DefaultValue
		}
	}

	// Batch set new configs if any
	if len(newConfigs) > 0 {
		h.pluginConfig.SetPluginConfigs(pluginName, newConfigs)
	}

	// Send current config values back to plugin
	values := h.pluginConfig.GetPluginConfigs(pluginName)
	stream.Send(&pb.BackendMessage{
		Payload: &pb.BackendMessage_ConfigGetResponse{
			ConfigGetResponse: &pb.ConfigGetResponse{
				Success: true,
				Config:  &pb.ConfigValues{Values: values},
			},
		},
	})
}

func (h *Handler) handleConfigGet(pluginName string, req *pb.ConfigGetRequest, stream pb.PluginService_ConnectServer) {
	values := h.pluginConfig.GetPluginConfigs(pluginName)
	resp := &pb.ConfigGetResponse{
		RequestId: req.RequestId,
		Success:   true,
		Config:    &pb.ConfigValues{Values: values},
	}

	stream.Send(&pb.BackendMessage{
		Payload: &pb.BackendMessage_ConfigGetResponse{ConfigGetResponse: resp},
	})
}

// SetPluginConfig sets a config value and notifies the plugin
func (h *Handler) SetPluginConfig(pluginName, key, value string) error {
	// Save to config file
	if err := h.pluginConfig.SetPluginConfig(pluginName, key, value); err != nil {
		return err
	}

	// Find plugin by name and notify if connected
	plugin, ok := h.manager.GetByName(pluginName)
	if !ok {
		return nil // Plugin not connected, just saved to file
	}

	// Notify plugin
	allValues := h.pluginConfig.GetPluginConfigs(pluginName)
	return h.manager.NotifyConfigChanged(plugin.ID, key, value, allValues)
}

// Manager returns the plugin manager
func (h *Handler) Manager() *Manager {
	return h.manager
}
