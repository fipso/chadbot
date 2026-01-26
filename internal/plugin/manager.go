package plugin

import (
	"log"
	"sync"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/internal/event"
)

// Plugin represents a connected plugin
type Plugin struct {
	ID            string
	Name          string
	Version       string
	Description   string
	Stream        pb.PluginService_ConnectServer
	Subscribed    []string // Event patterns subscribed to
	ConfigSchema  *pb.ConfigSchema
	Documentation string // PLUGIN.md content
}

// Manager handles plugin lifecycle and communication
type Manager struct {
	mu       sync.RWMutex
	plugins  map[string]*Plugin
	registry *Registry
	eventBus *event.Bus

	// Pending skill invocations (request_id -> response channel)
	pendingMu   sync.RWMutex
	pendingReqs map[string]chan *pb.SkillResponse
}

// NewManager creates a new plugin manager
func NewManager(registry *Registry, eventBus *event.Bus) *Manager {
	return &Manager{
		plugins:     make(map[string]*Plugin),
		registry:    registry,
		eventBus:    eventBus,
		pendingReqs: make(map[string]chan *pb.SkillResponse),
	}
}

// Register adds a new plugin
func (m *Manager) Register(id, name, version, description string, stream pb.PluginService_ConnectServer) *Plugin {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin := &Plugin{
		ID:          id,
		Name:        name,
		Version:     version,
		Description: description,
		Stream:      stream,
	}

	m.plugins[id] = plugin
	log.Printf("[Manager] Plugin registered: %s (id: %s, version: %s)", name, id, version)
	return plugin
}

// Unregister removes a plugin
func (m *Manager) Unregister(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if plugin, ok := m.plugins[id]; ok {
		log.Printf("[Manager] Plugin unregistered: %s (id: %s)", plugin.Name, id)
		delete(m.plugins, id)
	}

	// Cleanup registry
	m.registry.UnregisterPluginSkills(id)
}

// Get returns a plugin by ID
func (m *Manager) Get(id string) (*Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, ok := m.plugins[id]
	return plugin, ok
}

// GetByName returns a plugin by name
func (m *Manager) GetByName(name string) (*Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.plugins {
		if p.Name == name {
			return p, true
		}
	}
	return nil, false
}

// List returns all registered plugins
func (m *Manager) List() []*Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugins := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		plugins = append(plugins, p)
	}
	return plugins
}

// RegisterPendingRequest creates a channel for skill response
func (m *Manager) RegisterPendingRequest(requestID string) chan *pb.SkillResponse {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	ch := make(chan *pb.SkillResponse, 1)
	m.pendingReqs[requestID] = ch
	return ch
}

// ResolvePendingRequest sends a response to a pending request
func (m *Manager) ResolvePendingRequest(requestID string, response *pb.SkillResponse) bool {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	if ch, ok := m.pendingReqs[requestID]; ok {
		ch <- response
		close(ch)
		delete(m.pendingReqs, requestID)
		return true
	}
	return false
}

// CancelPendingRequest removes a pending request
func (m *Manager) CancelPendingRequest(requestID string) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	if ch, ok := m.pendingReqs[requestID]; ok {
		close(ch)
		delete(m.pendingReqs, requestID)
	}
}

// SubscribePlugin subscribes a plugin to event patterns
func (m *Manager) SubscribePlugin(pluginID string, patterns []string) {
	m.mu.Lock()
	plugin, ok := m.plugins[pluginID]
	if !ok {
		m.mu.Unlock()
		return
	}
	plugin.Subscribed = append(plugin.Subscribed, patterns...)
	stream := plugin.Stream
	m.mu.Unlock()

	// Subscribe to event bus
	m.eventBus.Subscribe(patterns, func(evt *pb.Event) {
		// Send event to plugin
		err := stream.Send(&pb.BackendMessage{
			Payload: &pb.BackendMessage_EventDispatch{
				EventDispatch: &pb.EventDispatch{
					Event: evt,
				},
			},
		})
		if err != nil {
			log.Printf("[Manager] Failed to send event to plugin %s: %v", pluginID, err)
		}
	})
}

// Registry returns the skill registry
func (m *Manager) Registry() *Registry {
	return m.registry
}

// EventBus returns the event bus
func (m *Manager) EventBus() *event.Bus {
	return m.eventBus
}

// SetConfigSchema sets the config schema for a plugin
func (m *Manager) SetConfigSchema(pluginID string, schema *pb.ConfigSchema) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if plugin, ok := m.plugins[pluginID]; ok {
		plugin.ConfigSchema = schema
		log.Printf("[Manager] Config schema set for plugin %s: %d fields", plugin.Name, len(schema.Fields))
	}
}

// GetConfigSchema returns the config schema for a plugin
func (m *Manager) GetConfigSchema(pluginID string) *pb.ConfigSchema {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if plugin, ok := m.plugins[pluginID]; ok {
		return plugin.ConfigSchema
	}
	return nil
}

// NotifyConfigChanged sends a config change to a plugin
func (m *Manager) NotifyConfigChanged(pluginID, key, value string, allValues map[string]string) error {
	m.mu.RLock()
	plugin, ok := m.plugins[pluginID]
	m.mu.RUnlock()

	if !ok {
		return nil
	}

	values := &pb.ConfigValues{Values: allValues}
	return plugin.Stream.Send(&pb.BackendMessage{
		Payload: &pb.BackendMessage_ConfigChanged{
			ConfigChanged: &pb.ConfigChanged{
				Key:       key,
				Value:     value,
				AllValues: values,
			},
		},
	})
}

// SetDocumentation sets the documentation for a plugin
func (m *Manager) SetDocumentation(pluginID, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if plugin, ok := m.plugins[pluginID]; ok {
		plugin.Documentation = content
		log.Printf("[Manager] Documentation set for plugin %s", plugin.Name)
	}
}

// GetDocumentation returns the documentation for a plugin by ID
func (m *Manager) GetDocumentation(pluginID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if plugin, ok := m.plugins[pluginID]; ok {
		return plugin.Documentation
	}
	return ""
}

// GetDocumentationByName returns the documentation for a plugin by name
func (m *Manager) GetDocumentationByName(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.plugins {
		if p.Name == name {
			return p.Documentation
		}
	}
	return ""
}
