package model

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OpenAIConfig struct {
	BaseURL, APIKey string
	Timeout         time.Duration
	MaxRetries      int
	// SanitizeToolNames rewrites tool names that strict function-calling APIs
	// reject (for example names containing dots) to safe wire names and maps
	// response tool calls back to the registry names.
	SanitizeToolNames bool
}
type OpenAIProvider struct {
	baseURL, apiKey   string
	client            *http.Client
	maxRetries        int
	sanitizeToolNames bool
}

func NewOpenAIProvider(c OpenAIConfig) (*OpenAIProvider, error) {
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
	return &OpenAIProvider{baseURL: strings.TrimRight(c.BaseURL, "/"), apiKey: c.APIKey, client: &http.Client{Timeout: c.Timeout}, maxRetries: c.MaxRetries, sanitizeToolNames: c.SanitizeToolNames}, nil
}

func (p *OpenAIProvider) Capabilities() Capabilities {
	return Capabilities{Streaming: true, ToolCalls: true}
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Tools    []openAITool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream,omitempty"`
}
type openAIMessage struct {
	Role             Role             `json:"role"`
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}
type openAITool struct {
	Type     string         `json:"type"`
	Function ToolDefinition `json:"function"`
}
type openAIToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}
type openAIResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		Delta        openAIMessage `json:"delta"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage                                 `json:"usage"`
	Error *struct{ Message, Type, Code string } `json:"error,omitempty"`
}

func (p *OpenAIProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	body := openAIRequest{Model: req.Model, Stream: req.Stream}
	var names *toolNameMapper
	if p.sanitizeToolNames {
		names = newToolNameMapper()
	}
	for _, m := range req.Messages {
		reasoning, err := p.reasoningForMessage(m)
		if err != nil {
			return GenerateResponse{}, err
		}
		om := openAIMessage{Role: m.Role, Content: m.Content, ReasoningContent: reasoning, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			var c openAIToolCall
			c.ID = tc.ID
			c.Type = "function"
			c.Function.Name = tc.Name
			if names != nil {
				c.Function.Name = names.wire(tc.Name)
			}
			c.Function.Arguments = string(tc.Arguments)
			om.ToolCalls = append(om.ToolCalls, c)
		}
		body.Messages = append(body.Messages, om)
	}
	for _, t := range req.Tools {
		if names != nil {
			t.Name = names.wire(t.Name)
		}
		body.Tools = append(body.Tools, openAITool{Type: "function", Function: t})
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
		resp, err := p.do(ctx, payload)
		if err == nil {
			var result GenerateResponse
			if req.Stream {
				result, err = p.readStream(resp.Body, req.OnStream, names.restore)
			} else {
				result, err = p.readJSON(resp.Body, names.restore)
			}
			if err == nil {
				err = p.protectReasoning(&result.Message)
			}
			return result, err
		}
		last = err
		var pe *ProviderError
		if !errors.As(err, &pe) || !pe.Retryable {
			return GenerateResponse{}, err
		}
	}
	return GenerateResponse{}, last
}

// toolNameMapper translates registry tool names to provider-safe wire names
// and back within a single request. Names that are already valid are sent
// unchanged; mappings are remembered so response tool calls restore exactly.
type toolNameMapper struct {
	forward map[string]string
	reverse map[string]string
}

func newToolNameMapper() *toolNameMapper {
	return &toolNameMapper{forward: map[string]string{}, reverse: map[string]string{}}
}

// wire returns the provider-safe name for a registry tool name.
func (m *toolNameMapper) wire(name string) string {
	if m == nil {
		return name
	}
	if safe, ok := m.forward[name]; ok {
		return safe
	}
	safe := name
	if !validToolName(name) {
		safe = sanitizeToolName(name)
	}
	base := safe
	for i := 2; ; i++ {
		original, taken := m.reverse[safe]
		if !taken || original == name {
			break
		}
		safe = fmt.Sprintf("%s_%d", base, i)
	}
	m.forward[name] = safe
	m.reverse[safe] = name
	return safe
}

// restore maps a wire tool call name back to the registry name; unknown names
// pass through unchanged so providers may still answer with their own names.
func (m *toolNameMapper) restore(name string) string {
	if m == nil {
		return name
	}
	if original, ok := m.reverse[name]; ok {
		return original
	}
	return name
}

// validToolName reports whether name matches the strict function-calling
// shape: starts with a letter, then letters, digits, underscores, or dashes.
func validToolName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' || c == '_' || c == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func sanitizeToolName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			b.WriteByte(c)
		case c >= '0' && c <= '9' || c == '_' || c == '-':
			if i == 0 {
				b.WriteString("t_")
			}
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

type protectedReasoning struct {
	Type       string `json:"type"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func (p *OpenAIProvider) protectReasoning(message *Message) error {
	if message.ReasoningContent == "" || len(message.ToolCalls) == 0 {
		return nil
	}
	key := sha256.Sum256([]byte("gohermit/provider-reasoning/v1\x00" + p.apiKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(message.ReasoningContent), nil)
	message.ProviderData, err = json.Marshal(protectedReasoning{Type: "gohermit.encrypted_reasoning.v1", Nonce: base64.RawStdEncoding.EncodeToString(nonce), Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext)})
	return err
}

func (p *OpenAIProvider) reasoningForMessage(message Message) (string, error) {
	if message.ReasoningContent != "" || len(message.ProviderData) == 0 {
		return message.ReasoningContent, nil
	}
	var protected protectedReasoning
	if err := json.Unmarshal(message.ProviderData, &protected); err != nil || protected.Type != "gohermit.encrypted_reasoning.v1" {
		return "", nil
	}
	nonce, err := base64.RawStdEncoding.DecodeString(protected.Nonce)
	if err != nil {
		return "", &ProviderError{Kind: ErrorProtocol, Message: "decode protected reasoning nonce", Cause: err}
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(protected.Ciphertext)
	if err != nil {
		return "", &ProviderError{Kind: ErrorProtocol, Message: "decode protected reasoning content", Cause: err}
	}
	key := sha256.Sum256([]byte("gohermit/provider-reasoning/v1\x00" + p.apiKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", &ProviderError{Kind: ErrorAuthentication, Message: "decrypt protected reasoning; the API key may have changed", Cause: err}
	}
	return string(plaintext), nil
}

func (p *OpenAIProvider) do(ctx context.Context, payload []byte) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+p.apiKey)
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
	msg := strings.TrimSpace(string(limited))
	var er openAIResponse
	if json.Unmarshal(limited, &er) == nil && er.Error != nil {
		msg = er.Error.Message
	}
	kind, retry := ErrorInvalidRequest, false
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		kind = ErrorAuthentication
	case resp.StatusCode == 429:
		kind, retry = ErrorRateLimit, true
	case resp.StatusCode >= 500:
		kind, retry = ErrorUnavailable, true
	}
	return nil, &ProviderError{Kind: kind, Status: resp.StatusCode, Retryable: retry, Message: msg}
}

func (p *OpenAIProvider) readJSON(body io.ReadCloser, restore func(string) string) (GenerateResponse, error) {
	defer body.Close()
	var r openAIResponse
	if err := json.NewDecoder(io.LimitReader(body, 16<<20)).Decode(&r); err != nil {
		return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "decode response", Cause: err}
	}
	if len(r.Choices) == 0 {
		return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "response has no choices"}
	}
	return convertChoice(r.Choices[0].Message, r.Choices[0].FinishReason, r.Usage, restore)
}

func convertChoice(m openAIMessage, finish string, usage Usage, restore func(string) string) (GenerateResponse, error) {
	out := Message{Role: m.Role, Content: m.Content, ReasoningContent: m.ReasoningContent, ToolCallID: m.ToolCallID}
	for _, c := range m.ToolCalls {
		args := json.RawMessage(c.Function.Arguments)
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		if !json.Valid(args) {
			return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "invalid tool call arguments"}
		}
		name := c.Function.Name
		if restore != nil {
			name = restore(name)
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: c.ID, Name: name, Arguments: args})
	}
	return GenerateResponse{Message: out, FinishReason: finish, Usage: usage}, nil
}

func (p *OpenAIProvider) readStream(body io.ReadCloser, on func(StreamEvent), restore func(string) string) (GenerateResponse, error) {
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	msg := openAIMessage{Role: RoleAssistant}
	calls := map[int]*openAIToolCall{}
	finish := ""
	usage := Usage{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk openAIResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "invalid stream event", Cause: err}
		}
		if len(chunk.Choices) == 0 {
			if chunk.Usage.TotalTokens > 0 {
				usage = chunk.Usage
			}
			continue
		}
		choice := chunk.Choices[0]
		msg.Content += choice.Delta.Content
		msg.ReasoningContent += choice.Delta.ReasoningContent
		if choice.Delta.Content != "" && on != nil {
			on(StreamEvent{Delta: choice.Delta.Content})
		}
		for _, d := range choice.Delta.ToolCalls {
			c := calls[d.Index]
			if c == nil {
				c = &openAIToolCall{Index: d.Index}
				calls[d.Index] = c
			}
			if d.ID != "" {
				c.ID = d.ID
			}
			if d.Function.Name != "" {
				c.Function.Name += d.Function.Name
			}
			c.Function.Arguments += d.Function.Arguments
		}
		if choice.FinishReason != "" {
			finish = choice.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		return GenerateResponse{}, &ProviderError{Kind: ErrorProtocol, Message: "read stream", Cause: err}
	}
	for i := 0; i < len(calls); i++ {
		if c := calls[i]; c != nil {
			msg.ToolCalls = append(msg.ToolCalls, *c)
		}
	}
	out, err := convertChoice(msg, finish, usage, restore)
	if err == nil && on != nil {
		on(StreamEvent{Done: true})
	}
	return out, err
}

func RetryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	return 0
}
