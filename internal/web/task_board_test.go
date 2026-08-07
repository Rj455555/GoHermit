package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/employee"
)

func TestTaskBoardProjectsQueuedTaskAndNoteWithoutCreatingExecution(t *testing.T) {
	server, workspace, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	createEmployeeForTaskAPI(t, handler, workspace, "employee-a")
	response := requestJSON(t, handler, http.MethodPost, "/api/employees/employee-a/tasks", taskAPIInput(), "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create Task status=%d body=%s", response.Code, response.Body.String())
	}
	var task controlplane.EmployeeTaskView
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, handler, http.MethodGet, "/api/task-board", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("get board status=%d body=%s", response.Code, response.Body.String())
	}
	var board controlplane.TaskBoardView
	if err := json.Unmarshal(response.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	if len(board.Cards) != 1 || board.Cards[0].TaskID != task.ID || board.Cards[0].ColumnID != "todo" || board.Cards[0].ProjectionReason != "queued_task" {
		t.Fatalf("queued board projection = %#v", board.Cards)
	}
	if board.Cards[0].SessionID != "" || board.Cards[0].RunID != "" || board.Cards[0].State != string(employee.TaskQueued) {
		t.Fatalf("board fabricated execution identities = %#v", board.Cards[0])
	}

	response = requestJSON(t, handler, http.MethodPut, "/api/task-board/cards/"+task.ID, map[string]any{
		"column_id": "in_progress", "rank": 10, "labels": []string{"focus"}, "priority": 2,
		"due_at": nil, "pinned": true, "blocked": false, "blocker_reason": "", "depends_on": []string{}, "source_url": "", "loop_id": "",
	}, "")
	if response.Code != http.StatusOK {
		t.Fatalf("update board card status=%d body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	if board.Cards[0].ColumnID != "todo" || board.Cards[0].State != string(employee.TaskQueued) {
		t.Fatalf("queued task bypassed explicit Start through metadata = %#v", board.Cards[0])
	}

	response = requestJSON(t, handler, http.MethodPost, "/api/task-board/notes", map[string]any{
		"title": "Research idea", "body": "Keep this outside the execution runtime.", "column_id": "backlog",
		"rank": 1, "labels": []string{"research"}, "priority": 1, "due_at": nil, "pinned": false,
		"source_url": "https://example.com/reference", "blocker_reason": "",
	}, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create Note status=%d body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	if len(board.Cards) != 2 {
		t.Fatalf("board cards after Note = %#v", board.Cards)
	}
	var foundNote bool
	for _, card := range board.Cards {
		if card.Kind == "note" {
			foundNote = true
			if card.SessionID != "" || card.RunID != "" || card.State != "note" {
				t.Fatalf("Note entered execution projection = %#v", card)
			}
		}
	}
	if !foundNote {
		t.Fatal("created Note missing from Board projection")
	}
}

func TestTaskBoardMovesNoteCardWithoutExecution(t *testing.T) {
	server, _, _ := newEmployeeTestServer(t)
	handler := server.Handler()
	response := requestJSON(t, handler, http.MethodPost, "/api/task-board/notes", map[string]any{
		"title": "Research idea", "body": "Keep this outside the execution runtime.", "column_id": "backlog",
		"rank": 1, "labels": []string{"research"}, "priority": 1, "due_at": nil, "pinned": false,
		"source_url": "", "blocker_reason": "",
	}, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("create Note status=%d body=%s", response.Code, response.Body.String())
	}
	var board controlplane.TaskBoardView
	if err := json.Unmarshal(response.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	noteID := ""
	for _, card := range board.Cards {
		if card.Kind == "note" {
			noteID = card.ID
		}
	}
	if noteID == "" {
		t.Fatal("created Note missing from Board projection")
	}

	move := func(cardID string) int {
		return requestJSON(t, handler, http.MethodPut, "/api/task-board/cards/"+cardID, map[string]any{
			"column_id": "review", "rank": 42, "labels": []string{"research"}, "priority": 1,
			"due_at": nil, "pinned": false, "blocked": false, "blocker_reason": "", "depends_on": []string{},
			"source_url": "", "loop_id": "",
		}, "").Code
	}
	if code := move("note-missing"); code != http.StatusNotFound {
		t.Fatalf("move unknown card status=%d", code)
	}
	if code := move(noteID); code != http.StatusOK {
		t.Fatalf("move Note status=%d", code)
	}

	response = requestJSON(t, handler, http.MethodGet, "/api/task-board", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("get board status=%d body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	for _, card := range board.Cards {
		if card.ID != noteID {
			continue
		}
		if card.Kind != "note" || card.ColumnID != "review" || card.Rank != 42 {
			t.Fatalf("moved Note projection = %#v", card)
		}
		if card.SessionID != "" || card.RunID != "" || card.State != "note" {
			t.Fatalf("Note move entered execution projection = %#v", card)
		}
		return
	}
	t.Fatal("moved Note missing from Board projection")
}
