package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Server-side xAI tool. Search runs on their side; we just get the final text
// (and any custom function_call items we have to execute).
func webSearchTool() map[string]any {
	return map[string]any{"type": "web_search"}
}

func xSearchTool() map[string]any {
	return map[string]any{"type": "x_search"}
}

func functionTool(spec ToolSpec) map[string]any {
	return map[string]any{
		"type":        "function",
		"name":        spec.Function.Name,
		"description": spec.Function.Description,
		"parameters":  json.RawMessage(spec.Function.Parameters),
	}
}

type respRequest struct {
	Model              string           `json:"model"`
	Input              []map[string]any `json:"input"`
	Tools              []map[string]any `json:"tools,omitempty"`
	Store              bool             `json:"store"`
	PreviousResponseID string           `json:"previous_response_id,omitempty"`
}

type respResponse struct {
	ID    string `json:"id"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Output []respItem `json:"output"`
}

type respItem struct {
	Type      string `json:"type"`
	Role      string `json:"role"`
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (c *Client) Respond(messages []Message, fnTools []ToolSpec, search bool, prevID string) (text string, calls []ToolCall, respID string, err error) {
	if c.APIKey == "" {
		return "", nil, "", fmt.Errorf("no API key configured (set llm.api_key or llm.api_key_env)")
	}
	var tools []map[string]any
	if search {
		tools = append(tools, webSearchTool(), xSearchTool())
	}
	for _, t := range fnTools {
		tools = append(tools, functionTool(t))
	}

	var input []map[string]any
	if prevID == "" {
		input = messagesToInput(messages)
	} else {
		// continuation: only new function outputs (and any extra user bits)
		input = messagesToInput(messages)
	}

	body, err := json.Marshal(respRequest{
		Model:              c.Model,
		Input:              input,
		Tools:              tools,
		Store:              c.StoreProvider,
		PreviousResponseID: prevID,
	})
	if err != nil {
		return "", nil, "", err
	}
	httpReq, err := http.NewRequestWithContext(c.reqCtx(), http.MethodPost, c.BaseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", nil, "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	hc := c.SearchHTTP
	if hc == nil {
		hc = c.HTTP
	}
	resp, err := hc.Do(httpReq)
	if err != nil {
		return "", nil, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", nil, "", err
	}
	if err := providerStatus(resp.StatusCode, raw); err != nil {
		return "", nil, "", err
	}
	var out respResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", nil, "", fmt.Errorf("llm responses decode: %w (%s)", err, trim(raw, 200))
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", nil, "", fmt.Errorf("llm: %s", out.Error.Message)
	}

	var b strings.Builder
	for _, it := range out.Output {
		switch it.Type {
		case "function_call":
			tc := ToolCall{ID: it.CallID, Type: "function"}
			tc.Function.Name = it.Name
			tc.Function.Arguments = it.Arguments
			calls = append(calls, tc)
		case "message":
			for _, c := range it.Content {
				if c.Type == "output_text" || c.Type == "text" {
					b.WriteString(c.Text)
				}
			}
		}
	}
	return strings.TrimSpace(b.String()), calls, out.ID, nil
}

func messagesToInput(msgs []Message) []map[string]any {
	var out []map[string]any
	for _, m := range msgs {
		switch m.Role {
		case "tool":
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": m.ToolCallID,
				"output":  m.Content,
			})
		default:
			item := map[string]any{"role": m.Role, "content": m.Content}
			out = append(out, item)
		}
	}
	return out
}
