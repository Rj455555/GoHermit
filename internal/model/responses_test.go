package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func responsesProviderFor(t *testing.T, h http.HandlerFunc) *ResponsesProvider {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	provider, err := NewResponsesProvider(ResponsesConfig{BaseURL: server.URL, APIKey: "secret", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestResponsesGenerateAndPreserveOutputItems(t *testing.T) {
	provider := responsesProviderFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("request path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["store"] != false {
			t.Fatalf("store=%v", body["store"])
		}
		include, _ := body["include"].([]any)
		if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
			t.Fatalf("include=%v", body["include"])
		}
		fmt.Fprint(w, `{"status":"completed","output":[{"id":"rs_1","type":"reasoning","status":"completed","summary":[{"type":"summary_text","text":"do not persist"}],"encrypted_content":"ciphertext"},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"filesystem.read","arguments":"{\"path\":\"README.md\"}"}],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}`)
	})
	response, err := provider.Generate(context.Background(), GenerateRequest{Model: "gpt-5.6", Messages: []Message{{Role: RoleUser, Content: "inspect"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].ID != "call_1" || len(response.Message.ProviderData) == 0 || response.Usage.TotalTokens != 14 {
		t.Fatalf("response=%+v", response)
	}
	if strings.Contains(string(response.Message.ProviderData), "do not persist") || !strings.Contains(string(response.Message.ProviderData), "ciphertext") {
		t.Fatalf("unsafe provider data=%s", response.Message.ProviderData)
	}
	request, err := makeResponsesRequest(GenerateRequest{Model: "gpt-5.6", Messages: []Message{response.Message, {Role: RoleTool, ToolCallID: "call_1", Content: "ok"}}})
	if err != nil {
		t.Fatal(err)
	}
	joined := string(bytesJoin(request.Input))
	if !strings.Contains(joined, `"type":"reasoning"`) || !strings.Contains(joined, `"type":"function_call_output"`) {
		t.Fatalf("input=%s", joined)
	}
	if strings.Contains(joined, `"id":"rs_1"`) {
		t.Fatalf("store=false replay must not include response item ids: %s", joined)
	}
	if !strings.Contains(joined, `"summary"`) {
		t.Fatalf("reasoning replay must retain summary: %s", joined)
	}
}

func TestResponsesStreaming(t *testing.T) {
	provider := responsesProviderFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}]}}\n\n")
	})
	var streamed strings.Builder
	response, err := provider.Generate(context.Background(), GenerateRequest{Model: "gpt-5.6", Stream: true, OnStream: func(e StreamEvent) { streamed.WriteString(e.Delta) }})
	if err != nil {
		t.Fatal(err)
	}
	if streamed.String() != "hello" || response.Message.Content != "hello" {
		t.Fatalf("stream=%q response=%+v", streamed.String(), response)
	}
}

func TestResponsesStreamingBuildsToolCallFromOutputItemDone(t *testing.T) {
	provider := responsesProviderFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"tool_0\",\"arguments\":\"{}\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
	})
	response, err := provider.Generate(t.Context(), GenerateRequest{
		Model: "gpt", Stream: true,
		Tools: []ToolDefinition{{Name: "filesystem.read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != "filesystem.read" {
		t.Fatalf("tool calls=%+v", response.Message.ToolCalls)
	}
}

func TestResponsesMapsToolNamesBidirectionally(t *testing.T) {
	provider := responsesProviderFor(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []responsesTool `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Tools) != 1 || body.Tools[0].Name != "tool_0" {
			t.Fatalf("tools=%+v", body.Tools)
		}
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"call_1","name":"tool_0","arguments":"{}"}]}`)
	})
	response, err := provider.Generate(t.Context(), GenerateRequest{
		Model: "gpt", Tools: []ToolDefinition{{Name: "filesystem.read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != "filesystem.read" {
		t.Fatalf("tool calls=%+v", response.Message.ToolCalls)
	}
}

func TestResponsesProviderAddsCodexAccountHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("originator") != "codex_cli_rs" || r.Header.Get("ChatGPT-Account-ID") != "acct_test" {
			t.Fatalf("headers=%v", r.Header)
		}
		fmt.Fprint(w, `{"status":"completed","output":[]}`)
	}))
	defer server.Close()
	provider, err := NewResponsesProvider(ResponsesConfig{BaseURL: server.URL, APIKey: "secret", Timeout: time.Second, Headers: map[string]string{"originator": "codex_cli_rs", "ChatGPT-Account-ID": "acct_test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Generate(context.Background(), GenerateRequest{Model: "gpt-5.3-codex"}); err != nil {
		t.Fatal(err)
	}
}

func bytesJoin(values []json.RawMessage) []byte {
	var out []byte
	for _, value := range values {
		out = append(out, value...)
	}
	return out
}
