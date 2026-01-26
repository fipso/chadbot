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

// Coding Plan endpoint (subscription-based) - use /api/paas/v4 for pay-as-you-go
const zaiEndpoint = "https://api.z.ai/api/coding/paas/v4/chat/completions"

// ZAIProvider implements the Provider interface for z.ai (GLM models)
type ZAIProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewZAIProvider creates a new z.ai provider
func NewZAIProvider(apiKey, model string) *ZAIProvider {
	if apiKey == "" {
		apiKey = os.Getenv("ZAI_API_KEY")
	}
	if model == "" {
		model = "glm-4.7"
	}
	return &ZAIProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

func (p *ZAIProvider) Name() string {
	return "zai"
}

// Chat sends a chat completion request to z.ai GLM
func (p *ZAIProvider) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
	reqBody := map[string]interface{}{
		"model":      p.model,
		"messages":   p.convertMessages(messages),
		"max_tokens": 4096,
	}

	if len(tools) > 0 {
		reqBody["tools"] = p.convertTools(tools)
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", zaiEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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
		return nil, fmt.Errorf("z.ai API error: %s", string(respBody))
	}

	var result zaiResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no response from z.ai")
	}

	choice := result.Choices[0]
	response := &Response{
		Content: choice.Message.Content,
		Done:    choice.FinishReason == "stop",
	}

	// Parse tool calls
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]string
		json.Unmarshal([]byte(tc.Function.Arguments), &args)

		response.ToolCalls = append(response.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return response, nil
}

func (p *ZAIProvider) convertMessages(messages []Message) []map[string]interface{} {
	result := make([]map[string]interface{}, len(messages))
	for i, m := range messages {
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			toolCalls := make([]map[string]interface{}, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				args, _ := json.Marshal(tc.Arguments)
				toolCalls[j] = map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Name,
						"arguments": string(args),
					},
				}
			}
			msg["tool_calls"] = toolCalls
		}
		result[i] = msg
	}
	return result
}

func (p *ZAIProvider) convertTools(tools []Tool) []map[string]interface{} {
	result := make([]map[string]interface{}, len(tools))
	for i, t := range tools {
		result[i] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		}
	}
	return result
}

type zaiResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}
