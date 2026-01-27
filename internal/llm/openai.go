package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

const openAIEndpoint = "https://api.openai.com/v1/chat/completions"

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey, model string) *OpenAIProvider {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAIProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Chat sends a chat completion request
func (p *OpenAIProvider) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
	reqBody := map[string]interface{}{
		"model":    p.model,
		"messages": p.convertMessages(messages),
	}

	if len(tools) > 0 {
		reqBody["tools"] = p.convertTools(tools)
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openAIEndpoint, bytes.NewReader(body))
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
		return nil, fmt.Errorf("OpenAI API error: %s", string(respBody))
	}

	var result openAIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	choice := result.Choices[0]
	response := &Response{
		Content: choice.Message.Content,
		Done:    choice.FinishReason == "stop",
	}

	// Parse tool calls
	for _, tc := range choice.Message.ToolCalls {
		log.Printf("[OpenAI] Raw tool call: %s, args JSON: %s", tc.Function.Name, tc.Function.Arguments)

		// Parse into interface{} first to handle both strings and numbers
		var rawArgs map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &rawArgs)

		// Convert all values to strings
		args := make(map[string]string)
		for k, v := range rawArgs {
			args[k] = fmt.Sprintf("%v", v)
		}

		response.ToolCalls = append(response.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return response, nil
}

func (p *OpenAIProvider) convertMessages(messages []Message) []map[string]interface{} {
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

func (p *OpenAIProvider) convertTools(tools []Tool) []map[string]interface{} {
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

type openAIResponse struct {
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
