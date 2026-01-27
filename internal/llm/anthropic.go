package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const anthropicEndpoint = "https://api.anthropic.com/v1/messages"

// AnthropicProvider implements the Provider interface for Anthropic Claude
type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// Chat sends a messages request to Claude
func (p *AnthropicProvider) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
	reqBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": 4096,
		"messages":   p.convertMessages(messages),
	}

	if len(tools) > 0 {
		reqBody["tools"] = p.convertTools(tools)
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Anthropic API error: %s", string(respBody))
	}

	var result anthropicResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	response := &Response{
		Done: result.StopReason == "end_turn",
	}

	// Parse content blocks
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			response.Content += block.Text
		case "tool_use":
			args := make(map[string]string)
			if block.Input != nil {
				for k, v := range block.Input {
					args[k] = fmt.Sprintf("%v", v)
				}
			}
			response.ToolCalls = append(response.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}

	return response, nil
}

func (p *AnthropicProvider) convertMessages(messages []Message) []map[string]interface{} {
	var result []map[string]interface{}

	for _, m := range messages {
		switch m.Role {
		case "system":
			// System messages are handled separately in Anthropic API
			continue
		case "tool":
			// Tool results in Anthropic format
			result = append(result, map[string]interface{}{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type":        "tool_result",
						"tool_use_id": m.ToolCallID,
						"content":     m.Content,
					},
				},
			})
		case "assistant":
			if len(m.ToolCalls) > 0 {
				content := []map[string]interface{}{}
				if m.Content != "" {
					content = append(content, map[string]interface{}{
						"type": "text",
						"text": m.Content,
					})
				}
				for _, tc := range m.ToolCalls {
					content = append(content, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": tc.Arguments,
					})
				}
				result = append(result, map[string]interface{}{
					"role":    "assistant",
					"content": content,
				})
			} else {
				result = append(result, map[string]interface{}{
					"role":    "assistant",
					"content": m.Content,
				})
			}
		default:
			result = append(result, map[string]interface{}{
				"role":    m.Role,
				"content": m.Content,
			})
		}
	}

	return result
}

func (p *AnthropicProvider) convertTools(tools []Tool) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tools))
	for i, t := range tools {
		result[i] = map[string]interface{}{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.Parameters,
		}
	}
	return result
}

type anthropicResponse struct {
	Content []struct {
		Type  string                 `json:"type"`
		Text  string                 `json:"text,omitempty"`
		ID    string                 `json:"id,omitempty"`
		Name  string                 `json:"name,omitempty"`
		Input map[string]interface{} `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
}
