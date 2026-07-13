package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/model"
)

type finalProvider struct{}

func (finalProvider) Capabilities() model.Capabilities {
	return model.Capabilities{Streaming: true, ToolCalls: true}
}
func (finalProvider) Generate(context.Context, model.GenerateRequest) (model.GenerateResponse, error) {
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
}

func TestCLIRunJSONAndStatus(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hermit.toml"), []byte("[model]\nmodel = \"test\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cli := CLI{Stdout: &stdout, Stderr: &stderr, NewProvider: func(config.Config) (model.Provider, error) { return finalProvider{}, nil }}
	if code := cli.Run(context.Background(), []string{"run", "--workspace", root, "--output", "json", "task"}); code != ExitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var id string
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		if event["type"] == "task_started" {
			id, _ = event["session_id"].(string)
		}
	}
	if id == "" {
		t.Fatalf("session ID missing from %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"status", "--workspace", root, "--output", "json", id}); code != ExitOK {
		t.Fatalf("status code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status":"completed"`) {
		t.Fatalf("status=%s", stdout.String())
	}
}
func TestCLIUsageAndConfigValidation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cli := CLI{Stdout: &stdout, Stderr: &stderr}
	if code := cli.Run(context.Background(), nil); code != ExitUsage {
		t.Fatalf("usage code=%d", code)
	}
	path, err := filepath.Abs(filepath.Join("..", "..", "hermit.example.toml"))
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"config", "validate", "--config", path, "--output", "json"}); code != ExitOK {
		t.Fatalf("validate code=%d stderr=%s", code, stderr.String())
	}
}
