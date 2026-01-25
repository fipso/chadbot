package llm

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/internal/plugin"
)

// Provider represents an LLM provider
type Provider interface {
	Name() string
	Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error)
}

// Message represents a chat message
type Message struct {
	Role       string      `json:"role"` // "user", "assistant", "system", "tool"
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// Tool represents a function/skill that the LLM can call
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToolCall represents an LLM's request to call a tool
type ToolCall struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments"`
}

// Response represents an LLM response
type Response struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Done      bool       `json:"done"`
}

// Router routes requests to LLM providers and handles skill invocation
type Router struct {
	providers map[string]Provider
	manager   *plugin.Manager
	registry  *plugin.Registry
	defaultProvider string
}

// NewRouter creates a new LLM router
func NewRouter(manager *plugin.Manager, registry *plugin.Registry) *Router {
	return &Router{
		providers: make(map[string]Provider),
		manager:   manager,
		registry:  registry,
	}
}

// RegisterProvider adds an LLM provider
func (r *Router) RegisterProvider(provider Provider) {
	r.providers[provider.Name()] = provider
	if r.defaultProvider == "" {
		r.defaultProvider = provider.Name()
	}
	log.Printf("[LLM Router] Registered provider: %s", provider.Name())
}

// SetDefaultProvider sets the default provider
func (r *Router) SetDefaultProvider(name string) error {
	if _, ok := r.providers[name]; !ok {
		return fmt.Errorf("provider %s not found", name)
	}
	r.defaultProvider = name
	return nil
}

// ProviderInfo contains information about an LLM provider
type ProviderInfo struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// ListProviders returns all registered providers
func (r *Router) ListProviders() []ProviderInfo {
	result := make([]ProviderInfo, 0, len(r.providers))
	for name := range r.providers {
		result = append(result, ProviderInfo{
			Name:      name,
			IsDefault: name == r.defaultProvider,
		})
	}
	return result
}

// DefaultSystemPrompt is prepended to all conversations
const DefaultSystemPrompt = `You are a helpful AI assistant. You have access to tools/skills provided by plugins.
IMPORTANT: Only use the tools that are explicitly provided to you. Do not make up or hallucinate tools that don't exist.
If asked what tools you have, list ONLY the ones provided in the current conversation - nothing else.`

// Chat processes a chat request with tool calling loop
func (r *Router) Chat(ctx context.Context, messages []Message, providerName string) (*Response, error) {
	provider, ok := r.providers[providerName]
	if !ok {
		provider = r.providers[r.defaultProvider]
	}
	if provider == nil {
		return nil, fmt.Errorf("no LLM provider available")
	}

	// Prepend system prompt
	messages = append([]Message{{Role: "system", Content: DefaultSystemPrompt}}, messages...)

	// Convert skills to tools
	tools := r.getTools()

	// Main conversation loop with tool calls
	for {
		resp, err := provider.Chat(ctx, messages, tools)
		if err != nil {
			return nil, err
		}

		// If no tool calls, return the response
		if len(resp.ToolCalls) == 0 {
			return resp, nil
		}

		// Add assistant message with tool calls
		messages = append(messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute tool calls
		for _, tc := range resp.ToolCalls {
			result, err := r.invokeSkill(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error: %s", err.Error())
			}

			messages = append(messages, Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}
}

// getTools converts registered skills to LLM tools
func (r *Router) getTools() []Tool {
	skills := r.registry.GetSkillsForLLM()
	tools := make([]Tool, len(skills))

	for i, skill := range skills {
		params := make(map[string]interface{})
		properties := make(map[string]interface{})
		required := []string{}

		for _, p := range skill.Parameters {
			properties[p.Name] = map[string]interface{}{
				"type":        p.Type,
				"description": p.Description,
			}
			if p.Required {
				required = append(required, p.Name)
			}
		}

		params["type"] = "object"
		params["properties"] = properties
		params["required"] = required

		tools[i] = Tool{
			Name:        skill.Name,
			Description: skill.Description,
			Parameters:  params,
		}
	}

	return tools
}

// invokeSkill invokes a skill on a plugin
func (r *Router) invokeSkill(ctx context.Context, skillName string, args map[string]string) (string, error) {
	skill, ok := r.registry.GetSkill(skillName)
	if !ok {
		return "", fmt.Errorf("skill %s not found", skillName)
	}

	plugin, ok := r.manager.Get(skill.PluginID)
	if !ok {
		return "", fmt.Errorf("plugin %s not found", skill.PluginID)
	}

	requestID := uuid.New().String()
	respChan := r.manager.RegisterPendingRequest(requestID)

	// Send skill invocation to plugin
	err := plugin.Stream.Send(&pb.BackendMessage{
		Payload: &pb.BackendMessage_SkillInvoke{
			SkillInvoke: &pb.SkillInvoke{
				RequestId: requestID,
				SkillName: skillName,
				Arguments: args,
			},
		},
	})
	if err != nil {
		r.manager.CancelPendingRequest(requestID)
		return "", fmt.Errorf("failed to invoke skill: %w", err)
	}

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		if !resp.Success {
			return "", fmt.Errorf("skill error: %s", resp.Error)
		}
		return resp.Result, nil
	case <-time.After(30 * time.Second):
		r.manager.CancelPendingRequest(requestID)
		return "", fmt.Errorf("skill invocation timed out")
	case <-ctx.Done():
		r.manager.CancelPendingRequest(requestID)
		return "", ctx.Err()
	}
}
