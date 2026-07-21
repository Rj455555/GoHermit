package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/tool"
)

type teamProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *teamProvider) Generate(context.Context, model.GenerateRequest) (model.GenerateResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return model.GenerateResponse{Message: model.Message{Role: model.RoleAssistant, Content: `{"summary":"inspected","evidence":["workspace"]}`}, FinishReason: "stop", Usage: model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}, nil
}

func (*teamProvider) Capabilities() model.Capabilities { return model.Capabilities{} }

func TestTeamWorkerReusesCompletedExecutionSession(t *testing.T) {
	root := t.TempDir()
	store, err := session.NewStore(root, ".gohermit")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := session.New("parent goal", root, "digest")
	if err != nil {
		t.Fatal(err)
	}
	parent.ID = "parent"
	if err = store.Save(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	provider := &teamProvider{}
	build := func(context.Context, string, string, RuntimeOptions) (*Runtime, error) {
		manager, managerErr := contextmgr.New(contextmgr.Config{MaxTokens: 4096, CompressionThreshold: .8, HardLimitThreshold: .92, ReserveOutputTokens: 512})
		if managerErr != nil {
			return nil, managerErr
		}
		return &Runtime{Workspace: root, Store: store, Runner: &agent.Runner{Provider: provider, Executor: tool.Executor{Registry: tool.NewRegistry(), DefaultTimeout: time.Second}, Context: manager, Store: store, Config: agent.Config{MaxTurns: 2, Timeout: time.Minute, Model: "test"}}}, nil
	}
	worker := TeamWorker{Workspace: root, ParentSessionID: "parent", ParentRunID: "run", ParentStore: store, Build: build}
	assignment := team.Assignment{MissionID: "mission", Goal: "inspect", WorkItem: team.WorkItem{ID: "explore", Role: team.RoleExplorer, Title: "Explore", Goal: "inspect", ExecutionSessionID: "worker-mission-explore"}, MaxTokens: 1000, MaxDuration: time.Minute}
	first, err := worker.Execute(context.Background(), assignment)
	if err != nil || first.Handoff.Summary != "inspected" || first.Tokens != 30 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := worker.Execute(context.Background(), assignment)
	if err != nil || second.Handoff.Summary != "inspected" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls=%d, completed worker was replayed", provider.calls)
	}
}
