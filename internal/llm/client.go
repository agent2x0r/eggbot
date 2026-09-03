package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL         string
	APIKey          string
	Model           string
	SearchModel     string // live /responses lookup; empty = Model
	SearchReasoning string // Responses reasoning.effort; empty = omit
	HTTP            *http.Client
	Ctx             context.Context
	StoreProvider   bool
}

func NewClient(baseURL, apiKey, model string, timeout time.Duration) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ToolSpec struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

func FuncTool(name, desc string, params any) ToolSpec {
	raw, _ := json.Marshal(params)
	var t ToolSpec
	t.Type = "function"
	t.Function.Name = name
	t.Function.Description = desc
	t.Function.Parameters = raw
	return t
}

type chatRequest struct {
	Model     string     `json:"model"`
	Messages  []Message  `json:"messages"`
	Tools     []ToolSpec `json:"tools,omitempty"`
	MaxTokens int        `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
}

func (c *Client) Chat(messages []Message, tools []ToolSpec) (Message, error) {
	return c.chat(c.Model, messages, tools, 0)
}

func (c *Client) chat(model string, messages []Message, tools []ToolSpec, maxTokens int) (Message, error) {
	if c.APIKey == "" {
		return Message{}, fmt.Errorf("no API key configured (set llm.api_key or llm.api_key_env)")
	}
	if model == "" {
		model = c.Model
	}
	body, err := json.Marshal(chatRequest{Model: model, Messages: messages, Tools: tools, MaxTokens: maxTokens})
	if err != nil {
		return Message{}, err
	}
	url := c.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(c.reqCtx(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Message{}, err
	}
	if err := providerStatus(resp.StatusCode, raw); err != nil {
		return Message{}, err
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Message{}, fmt.Errorf("llm decode: %w (%s)", err, trim(raw, 200))
	}
	if out.Error != nil && out.Error.Message != "" {
		return Message{}, fmt.Errorf("llm: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return Message{}, fmt.Errorf("llm: empty response")
	}
	return out.Choices[0].Message, nil
}

func (c *Client) reqCtx() context.Context {
	if c != nil && c.Ctx != nil {
		return c.Ctx
	}
	return context.Background()
}

func trim(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
