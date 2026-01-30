package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/internal/plugin"
	"github.com/fipso/chadbot/internal/souls"
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
	Content            string              `json:"content"`
	ToolCalls          []ToolCall          `json:"tool_calls,omitempty"`
	Done               bool                `json:"done"`
	DeferredAttachments []DeferredAttachment `json:"-"` // Attachments to add after response
}

// DeferredAttachment represents an attachment to add after the LLM response
type DeferredAttachment struct {
	ChatID      string
	Role        string
	Content     string
	DisplayOnly bool
	Attachments []*pb.Attachment
}

// Router routes requests to LLM providers and handles skill invocation
type Router struct {
	providers       map[string]Provider
	manager         *plugin.Manager
	registry        *plugin.Registry
	souls           *souls.Manager
	defaultProvider string
}

// NewRouter creates a new LLM router
func NewRouter(manager *plugin.Manager, registry *plugin.Registry, soulsManager *souls.Manager) *Router {
	return &Router{
		providers: make(map[string]Provider),
		manager:   manager,
		registry:  registry,
		souls:     soulsManager,
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

// DefaultSystemPrompt is prepended to all conversations (fallback if no soul is active)
const DefaultSystemPrompt = `You are a helpful AI assistant. You have access to tools/skills provided by plugins.
IMPORTANT: Only use the tools that are explicitly provided to you. Do not make up or hallucinate tools that don't exist.
If asked what tools you have, list ONLY the ones provided in the current conversation - nothing else.

When using tools to extract or gather data, be efficient with token usage:
- Extract only the specific information needed, not entire pages or datasets
- Summarize large results before requesting more data
- Avoid redundant tool calls - plan your approach before executing
- If a tool returns a large response, focus on the relevant parts in your answer`

// buildSystemPrompt creates the base system prompt (without plugin docs)
func (r *Router) buildSystemPrompt(soulName string) string {
	if r.souls != nil {
		return r.souls.GetSystemPrompt(soulName)
	}
	return DefaultSystemPrompt
}

// getPluginDocumentation returns formatted documentation for a plugin
func (r *Router) getPluginDocumentation(pluginName string) string {
	doc := r.manager.GetDocumentationByName(pluginName)
	if doc == "" {
		return ""
	}
	return fmt.Sprintf("\n\n---\n## Plugin: %s\n\n%s", pluginName, doc)
}

// ChatContext contains context information for the chat session
type ChatContext struct {
	ChatID string
	UserID string
	Soul   string // Soul name for system prompt
}

// Chat processes a chat request with tool calling loop
func (r *Router) Chat(ctx context.Context, messages []Message, providerName string, chatCtx *ChatContext) (*Response, error) {
	provider, ok := r.providers[providerName]
	if !ok {
		provider = r.providers[r.defaultProvider]
	}
	if provider == nil {
		return nil, fmt.Errorf("no LLM provider available")
	}

	// Prepend system prompt (without plugin docs - those are added on-demand)
	soulName := ""
	if chatCtx != nil {
		soulName = chatCtx.Soul
	}
	systemPrompt := r.buildSystemPrompt(soulName)
	messages = append([]Message{{Role: "system", Content: systemPrompt}}, messages...)

	// Convert skills to tools
	tools := r.getTools()

	// Track deferred attachments from skill results
	var deferredAttachments []DeferredAttachment

	// Track which plugins have had their documentation injected
	loadedPluginDocs := make(map[string]bool)

	// Main conversation loop with tool calls
	for {
		resp, err := provider.Chat(ctx, messages, tools)
		if err != nil {
			return nil, err
		}

		// If no tool calls, return the response with deferred attachments
		if len(resp.ToolCalls) == 0 {
			resp.DeferredAttachments = deferredAttachments
			return resp, nil
		}

		// Check if any tool calls need plugin documentation injected first
		needsDocInjection := false
		var docsToInject []string
		for _, tc := range resp.ToolCalls {
			skill, ok := r.registry.GetSkill(tc.Name)
			if !ok {
				continue
			}
			plugin, ok := r.manager.Get(skill.PluginID)
			if !ok {
				continue
			}
			pluginName := plugin.Name
			if !loadedPluginDocs[pluginName] {
				doc := r.getPluginDocumentation(pluginName)
				if doc != "" {
					docsToInject = append(docsToInject, doc)
					loadedPluginDocs[pluginName] = true
					needsDocInjection = true
					log.Printf("[LLM Router] Injecting documentation for plugin: %s", pluginName)
				}
			}
		}

		// If we need to inject docs, add them to the system message and re-query
		if needsDocInjection {
			// Append plugin docs to system message
			for i, m := range messages {
				if m.Role == "system" {
					for _, doc := range docsToInject {
						messages[i].Content += doc
					}
					break
				}
			}
			// Re-query the LLM with the documentation - don't add the tool calls yet
			log.Printf("[LLM Router] Re-querying LLM with plugin documentation")
			continue
		}

		// Add assistant message with tool calls
		messages = append(messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute tool calls
		for _, tc := range resp.ToolCalls {
			log.Printf("[LLM Router] Tool call: %s with args: %+v", tc.Name, tc.Arguments)
			result, err := r.invokeSkill(ctx, tc.Name, tc.Arguments, chatCtx)
			if err != nil {
				result = fmt.Sprintf("Error: %s", err.Error())
			}

			// Check for deferred attachments in the result
			textResult, deferred := r.extractDeferredAttachment(result, chatCtx)
			if deferred != nil {
				deferredAttachments = append(deferredAttachments, *deferred)
				result = textResult
			}

			// Truncate very large responses to avoid token limits
			const maxResultSize = 16000
			if len(result) > maxResultSize {
				result = result[:maxResultSize] + "\n\n[... truncated, response was " + fmt.Sprintf("%d", len(result)) + " bytes]"
				log.Printf("[LLM Router] Tool %s result truncated from %d to %d bytes", tc.Name, len(result), maxResultSize)
			}

			log.Printf("[LLM Router] Tool %s result (%d bytes): %.200s...", tc.Name, len(result), result)

			messages = append(messages, Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
		// Prune old tool exchanges to keep context manageable
		// Keep: system prompt (1) + user messages + last N tool exchanges
		const maxToolExchanges = 10 // Each exchange = assistant + tool messages
		messages = pruneToolHistory(messages, maxToolExchanges)

		log.Printf("[LLM Router] Continuing with %d messages", len(messages))
	}
}

// extractDeferredAttachment checks if a skill result contains a deferred attachment marker
// Returns the text portion and the deferred attachment (if any)
func (r *Router) extractDeferredAttachment(result string, chatCtx *ChatContext) (string, *DeferredAttachment) {
	// Try to parse as JSON with __deferred_attachment__ key
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return result, nil
	}

	deferredData, ok := parsed["__deferred_attachment__"].(map[string]interface{})
	if !ok {
		return result, nil
	}

	textResult, _ := parsed["text"].(string)

	// Extract attachment data
	attachmentData, ok := deferredData["attachment"].(map[string]interface{})
	if !ok {
		return textResult, nil
	}

	// Decode base64 data
	dataStr, _ := attachmentData["data"].(string)
	data, err := base64.StdEncoding.DecodeString(dataStr)
	if err != nil {
		log.Printf("[LLM Router] Failed to decode deferred attachment data: %v", err)
		return textResult, nil
	}

	chatID := ""
	if chatCtx != nil {
		chatID = chatCtx.ChatID
	}

	role, _ := deferredData["role"].(string)
	content, _ := deferredData["content"].(string)
	displayOnly, _ := deferredData["display_only"].(bool)
	mimeType, _ := attachmentData["mime_type"].(string)
	attachType, _ := attachmentData["type"].(string)

	deferred := &DeferredAttachment{
		ChatID:      chatID,
		Role:        role,
		Content:     content,
		DisplayOnly: displayOnly,
		Attachments: []*pb.Attachment{
			{
				Type:     attachType,
				MimeType: mimeType,
				Data:     data,
			},
		},
	}

	log.Printf("[LLM Router] Extracted deferred attachment: type=%s, mime=%s, size=%d bytes", attachType, mimeType, len(data))
	return textResult, deferred
}

// pruneToolHistory keeps system prompt, user messages, and last N tool exchanges
func pruneToolHistory(messages []Message, maxExchanges int) []Message {
	if len(messages) <= 10 {
		return messages
	}

	// Find where tool exchanges start (after system + initial user messages)
	toolStart := 0
	for i, m := range messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			toolStart = i
			break
		}
	}

	if toolStart == 0 {
		return messages
	}

	// Count tool exchanges (assistant with tool_calls + tool responses)
	toolMessages := messages[toolStart:]
	exchangeCount := 0
	for _, m := range toolMessages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			exchangeCount++
		}
	}

	if exchangeCount <= maxExchanges {
		return messages
	}

	// Keep only the last maxExchanges
	keepExchanges := maxExchanges
	keptMessages := messages[:toolStart] // System + user messages

	// Walk backwards to find where to start keeping
	currentExchange := 0
	startKeepIdx := len(toolMessages)
	for i := len(toolMessages) - 1; i >= 0; i-- {
		if toolMessages[i].Role == "assistant" && len(toolMessages[i].ToolCalls) > 0 {
			currentExchange++
			if currentExchange == keepExchanges {
				startKeepIdx = i
				break
			}
		}
	}

	keptMessages = append(keptMessages, toolMessages[startKeepIdx:]...)
	log.Printf("[LLM Router] Pruned history: %d -> %d messages (kept %d tool exchanges)",
		len(messages), len(keptMessages), keepExchanges)

	return keptMessages
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
func (r *Router) invokeSkill(ctx context.Context, skillName string, args map[string]string, chatCtx *ChatContext) (string, error) {
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

	// Build invocation context
	var invCtx *pb.InvocationContext
	if chatCtx != nil {
		invCtx = &pb.InvocationContext{
			ChatId: chatCtx.ChatID,
			UserId: chatCtx.UserID,
		}
	}

	// Send skill invocation to plugin
	err := plugin.Stream.Send(&pb.BackendMessage{
		Payload: &pb.BackendMessage_SkillInvoke{
			SkillInvoke: &pb.SkillInvoke{
				RequestId: requestID,
				SkillName: skillName,
				Arguments: args,
				Context:   invCtx,
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
