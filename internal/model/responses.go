package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ResponsesConfig struct {
	BaseURL, APIKey string
	Timeout         time.Duration
	MaxRetries      int
	Headers         map[string]string
}

type ResponsesProvider struct {
	baseURL, apiKey string
	client          *http.Client
	maxRetries      int
	headers         map[string]string
}

func NewResponsesProvider(c ResponsesConfig) (*ResponsesProvider, error) {
	u, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid model base URL")
	}
	if c.APIKey == "" {
		return nil, errors.New("model API key is empty")
	}
	if c.Timeout <= 0 {
		c.Timeout = 120 * time.Second
	}
	return &ResponsesProvider{baseURL: strings.TrimRight(c.BaseURL, "/"), apiKey: c.APIKey, client: &http.Client{Timeout: c.Timeout}, maxRetries: c.MaxRetries, headers: c.Headers}, nil
}

func (p *ResponsesProvider) Capabilities() Capabilities {
	return Capabilities{Streaming: true, ToolCalls: true}
}

type responsesRequest struct {
	Model         string            `json:"model"`
	Instructions  string            `json:"instructions,omitempty"`
	Input         []json.RawMessage `json:"input"`
	Tools         []responsesTool   `json:"tools,omitempty"`
	Include       []string          `json:"include,omitempty"`
	Stream        bool              `json:"stream,omitempty"`
	Store         bool              `json:"store"`
	ToolChoice    string            `json:"tool_choice,omitempty"`
	ParallelTools bool              `json:"parallel_tool_calls,omitempty"`
	toolNames     map[string]string `json:"-"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type responseObject struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Output []json.RawMessage `json:"output"`
	Usage  struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

type responseOutputItem struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type"`
	Role      Role   `json:"role,omitempty"`
	Status    string `json:"status,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content,omitempty"`
}

type responseStreamEvent struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta,omitempty"`
	Response *responseObject `json:"response,omitempty"`
	Item     json.RawMessage `json:"item,omitempty"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *ResponsesProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	body, err := makeResponsesRequest(req)
	if err != nil {
		return GenerateResponse{}, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return GenerateResponse{}, err
	}
	var last error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(1<<min(attempt-1, 4)) * 200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return GenerateResponse{}, ctx.Err()
			case <-timer.C:
			}
		}
		resp, requestErr := p.do(ctx, payload)
		if requestErr == nil {
			var result GenerateResponse
			if req.Stream {
				result, err = p.readStream(resp.Body, req.OnStream, body.toolNames)
			} else {
				result, err = p.readJSON(resp.Body, body.toolNames)
			}
			if err == nil {
				result.Attempts = attempt + 1
			}
			return result, err
		}
		last = requestErr
		var pe *ProviderError
		if !errors.As(requestErr, &pe) || !pe.Retryable {
			return GenerateResponse{}, requestErr
		}
	}
	var pe *ProviderError
	if errors.As(last, &pe) {
		pe.Attempts = p.maxRetries + 1
	}
	return GenerateResponse{}, last
}

func makeResponsesRequest(req GenerateRequest) (responsesRequest, error) {
	body := responsesRequest{
		Model: req.Model, Stream: req.Stream, Store: false,
		Include:   []string{"reasoning.encrypted_content"},
		toolNames: make(map[string]string),
	}
	if len(req.Tools) > 0 {
		body.ToolChoice = "auto"
		body.ParallelTools = true
	}
	externalNames := make(map[string]string, len(req.Tools))
	for index, tool := range req.Tools {
		external := fmt.Sprintf("tool_%d", index)
		externalNames[tool.Name] = external
		body.toolNames[external] = tool.Name
	}
	for _, message := range req.Messages {
		if message.Role == RoleSystem {
			if body.Instructions != "" {
				body.Instructions += "\n\n"
			}
			body.Instructions += message.Content
			continue
		}
		if message.Role == RoleAssistant && len(message.ProviderData) > 0 {
			var items []json.RawMessage
			if err := json.Unmarshal(message.ProviderData, &items); err != nil {
				return body, &ProviderError{Kind: ErrorProtocol, Message: "invalid saved Responses output", Cause: err}
			}
			body.Input = append(body.Input, items...)
		}
		if message.Role == RoleTool {
			item, _ := json.Marshal(map[string]any{"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content})
			body.Input = append(body.Input, item)
			continue
		}
		if message.Role != RoleAssistant || message.Content != "" {
			item, _ := json.Marshal(map[string]any{"type": "message", "role": message.Role, "content": message.Content})
			body.Input = append(body.Input, item)
		}
		for _, call := range message.ToolCalls {
			name := call.Name
			if external := externalNames[name]; external != "" {
				name = external
			}
			toolItem, _ := json.Marshal(map[string]any{"type": "function_call", "call_id": call.ID, "name": name, "arguments": string(call.Arguments)})
			body.Input = append(body.Input, toolItem)
		}
	}
	for index, tool := range req.Tools {
		body.Tools = append(body.Tools, responsesTool{Type: "function", Name: fmt.Sprintf("tool_%d", index), Description: tool.Description, Parameters: tool.Parameters})
	}
	return body, nil
}

func (p *ResponsesProvider) do(ctx context.Context, payload []byte) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+p.apiKey)
	for name, value := range p.headers {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Content-Type") {
			continue
		}
		r.Header.Set(name, value)
	}
	resp, err := p.client.Do(r)
	if err != nil {
		kind := ErrorUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = ErrorTimeout
		}
		return nil, &ProviderError{Kind: kind, Retryable: kind != ErrorTimeout, Message: "request failed", Cause: err}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	limited, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	message := strings.TrimSpace(string(limited))
	var envelope struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(limited, &envelope) == nil && envelope.Error != nil {
		message = envelope.Error.Message
	}
	kind, retry := ErrorInvalidRequest, false
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		kind = ErrorAuthentication
	case resp.StatusCode == http.StatusTooManyRequests:
		kind, retry = ErrorRateLimit, true
	case resp.StatusCode >= 500:
		kind, retry = ErrorUnavailable, true
	}
	return nil, &ProviderError{Kind: kind, Status: resp.StatusCode, Retryable: retry, Message: message}
}

func (p *ResponsesProvider) readJSON(body io.ReadCloser, toolNames map[string]string) (GenerateResponse, error) {
	defer body.Close()
	var response responseObject
	if err := json.NewDecoder(io.LimitReader(body, 16<<20)).Decode(&response); err != nil {
		return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "decode response", Cause: err}
	}
	return convertResponse(response, toolNames)
}

func convertResponse(response responseObject, toolNames map[string]string) (GenerateResponse, error) {
	if response.Error != nil {
		return GenerateResponse{}, &ProviderError{Kind: ErrorInvalidRequest, Message: response.Error.Message}
	}
	message := Message{Role: RoleAssistant}
	var continuation []json.RawMessage
	for _, rawItem := range response.Output {
		var item responseOutputItem
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "invalid Responses output item", Cause: err}
		}
		switch item.Type {
		case "reasoning":
			var reasoning struct {
				ID               string `json:"id"`
				Status           string `json:"status,omitempty"`
				EncryptedContent string `json:"encrypted_content,omitempty"`
			}
			if err := json.Unmarshal(rawItem, &reasoning); err != nil {
				return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "invalid Responses reasoning item", Cause: err}
			}
			if reasoning.EncryptedContent != "" {
				safe := map[string]any{"type": "reasoning", "status": reasoning.Status, "encrypted_content": reasoning.EncryptedContent, "summary": []any{}}
				safeItem, _ := json.Marshal(safe)
				continuation = append(continuation, safeItem)
			}
		case "message":
			if item.Phase == "commentary" || item.Phase == "analysis" {
				continue
			}
			for _, content := range item.Content {
				if content.Type == "output_text" {
					message.Content += content.Text
				}
			}
		case "function_call":
			arguments := json.RawMessage(item.Arguments)
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			if !json.Valid(arguments) {
				return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "invalid tool call arguments"}
			}
			name := item.Name
			if internal := toolNames[name]; internal != "" {
				name = internal
			}
			message.ToolCalls = append(message.ToolCalls, ToolCall{ID: item.CallID, Name: name, Arguments: arguments})
		}
	}
	if len(message.ToolCalls) > 0 && len(continuation) > 0 {
		data, err := json.Marshal(continuation)
		if err != nil {
			return GenerateResponse{}, err
		}
		message.ProviderData = data
	}
	finish := response.Status
	if finish == "completed" {
		finish = "stop"
	}
	return GenerateResponse{Message: message, FinishReason: finish, Usage: Usage{PromptTokens: response.Usage.InputTokens, CompletionTokens: response.Usage.OutputTokens, TotalTokens: response.Usage.TotalTokens}}, nil
}

func (p *ResponsesProvider) readStream(body io.ReadCloser, on func(StreamEvent), toolNames map[string]string) (GenerateResponse, error) {
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	var final *responseObject
	var outputItems []json.RawMessage
	var streamedText strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event responseStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "invalid stream event", Cause: err}
		}
		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" && on != nil {
				on(StreamEvent{Delta: event.Delta})
			}
			streamedText.WriteString(event.Delta)
		case "response.output_item.done":
			if len(event.Item) > 0 && string(event.Item) != "null" {
				outputItems = append(outputItems, append(json.RawMessage(nil), event.Item...))
			}
		case "response.completed":
			final = event.Response
		case "error":
			message := "Responses stream failed"
			if event.Error != nil && event.Error.Message != "" {
				message = event.Error.Message
			}
			return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: message}
		}
	}
	if err := scanner.Err(); err != nil {
		return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "read stream", Cause: err}
	}
	if final == nil {
		return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "stream ended before response.completed"}
	}
	if len(outputItems) > 0 {
		final.Output = outputItems
	} else if len(final.Output) == 0 && streamedText.Len() > 0 {
		item, _ := json.Marshal(map[string]any{
			"type": "message", "role": "assistant", "status": "completed",
			"content": []map[string]string{{"type": "output_text", "text": streamedText.String()}},
		})
		final.Output = []json.RawMessage{item}
	}
	response, err := convertResponse(*final, toolNames)
	if err == nil && on != nil {
		on(StreamEvent{Done: true})
	}
	return response, err
}
