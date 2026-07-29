package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/approval"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/session"
)

func TestHiddenSessionControlPlaneAccessFailsClosedWithoutSideEffects(t *testing.T) {
	const sentinel = "PRIVATE_EMPLOYEE_MEMORY_PHASE9_SENTINEL"
	svc := newTestService(t)
	if err := svc.credentials.SetAPIKey("deepseek", "test-secret"); err != nil {
		t.Fatal(err)
	}
	parent, err := session.New("parent mission", svc.Workspace, "digest")
	if err != nil {
		t.Fatal(err)
	}
	parent.ID = "parent-hidden-access"
	if err = svc.store.Save(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	hidden, err := session.New("hidden worker", svc.Workspace, "digest")
	if err != nil {
		t.Fatal(err)
	}
	hidden.ID = "worker-hidden-access"
	hidden.Hidden = true
	hidden.ParentSessionID = parent.ID
	hidden.ParentRunID = "parent-run"
	hidden.WorkItemID = "explore"
	hidden.Selection = session.Selection{
		Company: "deepseek", Access: "deepseek", Model: "deepseek-chat", Agent: "explorer",
	}
	run, err := hidden.NewRun("inspect private context")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Status = session.RunCompleted
	run.FinalMessage = sentinel
	run.CompletedAt = &now
	run.UpdatedAt = now
	hidden.ActiveRunID = ""
	hidden.RecentMessages = []model.Message{{Role: model.RoleAssistant, Content: sentinel}}
	request, err := approval.Create(approval.CreateSpec{
		RequestID: "approval-hidden", SessionID: hidden.ID, RunID: run.ID,
		Tool: "write_file", ResourcePaths: []string{"private.txt"},
		ArgsSummary: sentinel, ArgsPayload: `{"path":"private.txt"}`,
		PolicyFingerprint: "policy", PlanRevision: 1,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	hidden.ApprovalRequests = append(hidden.ApprovalRequests, request)
	if err = svc.store.Save(context.Background(), hidden); err != nil {
		t.Fatal(err)
	}
	if err = svc.store.AppendMessage(hidden.ID, session.MessageRecord{
		RunID: run.ID, Role: model.RoleAssistant, Content: sentinel, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	completed := event.New(event.TaskCompleted, hidden.ID)
	completed.RunID = run.ID
	completed.Message = sentinel
	if _, err = svc.store.CommitDetachedEvent(context.Background(), hidden.ID, completed); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(svc.Workspace, "workspace-marker.txt")
	if err = os.WriteFile(markerPath, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}

	beforeHidden, beforeMessages, beforeEvents, beforeParent := publicBoundaryState(t, svc, hidden.ID, parent.ID)
	var runtimeBuilds atomic.Int32
	svc.build = func(
		context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption,
	) (*app.Runtime, error) {
		runtimeBuilds.Add(1)
		return nil, errors.New("runtime build must not be reached")
	}
	assertNotFound := func(name string, err error) {
		t.Helper()
		var serviceErr *Error
		if !errors.As(err, &serviceErr) || serviceErr.Kind != KindNotFound {
			t.Fatalf("%s error = %v, want KindNotFound", name, err)
		}
		if strings.Contains(serviceErr.Error(), sentinel) ||
			strings.Contains(strings.ToLower(serviceErr.Error()), "hidden") {
			t.Fatalf("%s leaked private Session state: %v", name, serviceErr)
		}
	}

	_, _, err = svc.GetSession(context.Background(), hidden.ID)
	assertNotFound("GetSession", err)
	_, err = svc.LoadSession(context.Background(), hidden.ID)
	assertNotFound("LoadSession", err)
	_, err = svc.SessionEvents(context.Background(), hidden.ID, 0)
	assertNotFound("SessionEvents", err)
	_, err = svc.StartRun(context.Background(), hidden.ID, "second run")
	assertNotFound("StartRun", err)
	_, err = svc.StartRun(context.Background(), hidden.ID, "")
	assertNotFound("StartRun invalid message", err)
	_, err = svc.ResumeRun(context.Background(), hidden.ID, run.ID)
	assertNotFound("ResumeRun", err)
	_, err = svc.ApprovePlan(context.Background(), hidden.ID, run.ID)
	assertNotFound("ApprovePlan", err)
	_, err = svc.CancelRun(context.Background(), hidden.ID, run.ID)
	assertNotFound("CancelRun", err)
	_, err = svc.ListApprovals(context.Background(), hidden.ID, string(approval.Pending))
	assertNotFound("ListApprovals", err)
	_, _, err = svc.DecideApproval(context.Background(), hidden.ID, request.RequestID, true)
	assertNotFound("DecideApproval", err)
	_, _, err = svc.DecideApproval(context.Background(), hidden.ID, "", true)
	assertNotFound("DecideApproval invalid request", err)

	afterHidden, afterMessages, afterEvents, afterParent := publicBoundaryState(t, svc, hidden.ID, parent.ID)
	if !reflect.DeepEqual(afterHidden, beforeHidden) ||
		!reflect.DeepEqual(afterMessages, beforeMessages) ||
		!reflect.DeepEqual(afterEvents, beforeEvents) ||
		!reflect.DeepEqual(afterParent, beforeParent) {
		t.Fatal("rejected public access mutated hidden or parent Session state")
	}
	if runtimeBuilds.Load() != 0 {
		t.Fatalf("rejected public access attempted %d runtime/provider builds", runtimeBuilds.Load())
	}
	if marker, readErr := os.ReadFile(markerPath); readErr != nil || string(marker) != "unchanged" {
		t.Fatalf("workspace marker changed: %q err=%v", marker, readErr)
	}
}

func publicBoundaryState(
	t *testing.T,
	svc *Service,
	hiddenID, parentID string,
) ([]byte, []session.MessageRecord, []event.Event, []byte) {
	t.Helper()
	hidden, err := svc.store.Load(context.Background(), hiddenID)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := svc.store.Load(context.Background(), parentID)
	if err != nil {
		t.Fatal(err)
	}
	hiddenJSON, err := json.Marshal(hidden)
	if err != nil {
		t.Fatal(err)
	}
	parentJSON, err := json.Marshal(parent)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := svc.store.Messages(hiddenID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := svc.store.Events(hiddenID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return hiddenJSON, messages, events, parentJSON
}
