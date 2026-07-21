package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func providerFor(t *testing.T, h http.HandlerFunc, retries int) *OpenAIProvider {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	p, err := NewOpenAIProvider(OpenAIConfig{BaseURL: s.URL, APIKey: "secret", Timeout: time.Second, MaxRetries: retries})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func sanitizingProviderFor(t *testing.T, h http.HandlerFunc) *OpenAIProvider {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	p, err := NewOpenAIProvider(OpenAIConfig{BaseURL: s.URL, APIKey: "secret", Timeout: time.Second, SanitizeToolNames: true})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSanitizeToolNamesRewritesDefinitionsAndHistory(t *testing.T) {
	p := sanitizingProviderFor(t, func(w http.ResponseWriter, r *http.Request) {
		var request openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Tools) != 3 {
			t.Fatalf("tools=%+v", request.Tools)
		}
		got := []string{request.Tools[0].Function.Name, request.Tools[1].Function.Name, request.Tools[2].Function.Name}
		want := []string{"file_read", "plugin_echo_ping", "git_status"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("tool names=%v, want %v", got, want)
			}
		}
		if len(request.Messages) != 2 || request.Messages[0].ToolCalls[0].Function.Name != "patch_apply" {
			t.Fatalf("history tool call not sanitized: %+v", request.Messages)
		}
		if request.Messages[1].Role != RoleTool || request.Messages[1].ToolCallID != "c0" {
			t.Fatalf("tool result message altered: %+v", request.Messages[1])
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"file_read","arguments":"{}"}},{"id":"c2","type":"function","function":{"name":"provider_custom","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	})
	history := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c0", Name: "patch.apply", Arguments: json.RawMessage(`{}`)}}},
		{Role: RoleTool, ToolCallID: "c0", Content: "patched"},
	}
	tools := []ToolDefinition{
		{Name: "file.read", Description: "read", Parameters: json.RawMessage(`{}`)},
		{Name: "plugin.echo.ping", Description: "ping", Parameters: json.RawMessage(`{}`)},
		{Name: "git_status", Description: "status", Parameters: json.RawMessage(`{}`)},
	}
	r, err := p.Generate(context.Background(), GenerateRequest{Model: "test", Messages: history, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Message.ToolCalls) != 2 {
		t.Fatalf("tool calls=%+v", r.Message.ToolCalls)
	}
	if r.Message.ToolCalls[0].Name != "file.read" {
		t.Fatalf("mapped name=%q", r.Message.ToolCalls[0].Name)
	}
	if r.Message.ToolCalls[1].Name != "provider_custom" {
		t.Fatalf("unmapped name=%q", r.Message.ToolCalls[1].Name)
	}
}

func TestSanitizeToolNamesStreamingRestoresNames(t *testing.T) {
	p := sanitizingProviderFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"shell_exe\",\"arguments\":\"{}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"cute\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	tools := []ToolDefinition{{Name: "shell.execute", Description: "run", Parameters: json.RawMessage(`{}`)}}
	r, err := p.Generate(context.Background(), GenerateRequest{Model: "test", Stream: true, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Message.ToolCalls) != 1 || r.Message.ToolCalls[0].Name != "shell.execute" {
		t.Fatalf("tool calls=%+v", r.Message.ToolCalls)
	}
}

func TestToolNamesPassThroughWhenSanitizeDisabled(t *testing.T) {
	p := providerFor(t, func(w http.ResponseWriter, r *http.Request) {
		var request openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Tools) != 1 || request.Tools[0].Function.Name != "file.read" {
			t.Fatalf("tools=%+v", request.Tools)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"file.read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	}, 0)
	tools := []ToolDefinition{{Name: "file.read", Description: "read", Parameters: json.RawMessage(`{}`)}}
	r, err := p.Generate(context.Background(), GenerateRequest{Model: "test", Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Message.ToolCalls) != 1 || r.Message.ToolCalls[0].Name != "file.read" {
		t.Fatalf("tool calls=%+v", r.Message.ToolCalls)
	}
}

func TestToolNameMapperCollisionsAndValidity(t *testing.T) {
	m := newToolNameMapper()
	if got := m.wire("file.read"); got != "file_read" {
		t.Fatalf("wire=%q", got)
	}
	if got := m.wire("file_read"); got != "file_read_2" {
		t.Fatalf("collision wire=%q", got)
	}
	if got := m.wire("file.read"); got != "file_read" {
		t.Fatalf("memoized wire=%q", got)
	}
	if got := m.restore("file_read_2"); got != "file_read" {
		t.Fatalf("restore=%q", got)
	}
	if got := m.restore("unknown"); got != "unknown" {
		t.Fatalf("restore fallback=%q", got)
	}
	if validToolName("1file") || validToolName("") || validToolName("file.read") {
		t.Fatal("validToolName accepted invalid names")
	}
	if !validToolName("file_read-2") {
		t.Fatal("validToolName rejected valid name")
	}
}
func TestGenerateNormalAndAuthorization(t *testing.T) {
	p := providerFor(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization=%q", got)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"total_tokens":3}}`)
	}, 0)
	r, err := p.Generate(context.Background(), GenerateRequest{Model: "test", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Message.Content != "done" || r.Usage.TotalTokens != 3 {
		t.Fatalf("response=%+v", r)
	}
}
func TestGenerateRetriesServerError(t *testing.T) {
	var calls atomic.Int32
	p := providerFor(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", 503)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}, 1)
	if _, err := p.Generate(context.Background(), GenerateRequest{Model: "test"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
}
func TestGenerateStreamingToolCall(t *testing.T) {
	p := providerFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\",\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"filesystem.\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"read\",\"arguments\":\"\\\"README.md\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}, 0)
	var delta strings.Builder
	r, err := p.Generate(context.Background(), GenerateRequest{Model: "test", Stream: true, OnStream: func(e StreamEvent) { delta.WriteString(e.Delta) }})
	if err != nil {
		t.Fatal(err)
	}
	if delta.String() != "hello" || len(r.Message.ToolCalls) != 1 || r.Message.ToolCalls[0].Name != "filesystem.read" {
		t.Fatalf("response=%+v delta=%q", r, delta.String())
	}
}
func TestGenerateCancellation(t *testing.T) {
	p := providerFor(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := p.Generate(ctx, GenerateRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected cancellation")
	}
}

func TestReasoningContentIsEncryptedAtRestAndReplayed(t *testing.T) {
	var calls atomic.Int32
	p := providerFor(t, func(w http.ResponseWriter, r *http.Request) {
		var request openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"private chain","tool_calls":[{"id":"c1","type":"function","function":{"name":"filesystem.read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		if len(request.Messages) != 1 || request.Messages[0].ReasoningContent != "private chain" {
			t.Fatalf("reasoning was not replayed: %+v", request.Messages)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}, 0)
	first, err := p.Generate(context.Background(), GenerateRequest{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := json.Marshal(first.Message)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted), "private chain") || len(first.Message.ProviderData) == 0 {
		t.Fatalf("reasoning was stored in plaintext: %s", persisted)
	}
	if _, err = p.Generate(context.Background(), GenerateRequest{Model: "test", Messages: []Message{first.Message}}); err != nil {
		t.Fatal(err)
	}
}
