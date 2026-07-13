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

func TestRuntimeSelectionAppliesReadOnlyReviewAgent(t *testing.T) {
	root := t.TempDir()
	selection := config.RuntimeSelection{Company: "openai", Access: "openai-api", Model: "gpt-5.3-codex", Agent: "review"}
	runtime, err := BuildRuntimeWithOptions(context.Background(), root, "", RuntimeOptions{Selection: &selection}, func(config.Config) (model.Provider, error) {
		return finalProvider{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if runtime.Config.Model.Provider != "openai-api" || runtime.Config.Agent.Profile != "review" {
		t.Fatalf("config=%+v", runtime.Config)
	}
	definitions := runtime.Runner.Executor.Registry.Definitions()
	if len(definitions) == 0 {
		t.Fatal("review agent has no tools")
	}
	for _, definition := range definitions {
		if definition.Permission != "read" || definition.MutatesWorkspace {
			t.Fatalf("review tool is not read-only: %+v", definition)
		}
	}
}
