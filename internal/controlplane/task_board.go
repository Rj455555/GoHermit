package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/boardstore"
	"github.com/Rj455555/GoHermit/internal/employeestore"
)

type TaskBoardCard struct {
	ID                     string     `json:"id"`
	TaskID                 string     `json:"task_id,omitempty"`
	Kind                   string     `json:"kind"`
	Title                  string     `json:"title"`
	Body                   string     `json:"body,omitempty"`
	ColumnID               string     `json:"column_id"`
	Rank                   int64      `json:"rank"`
	Labels                 []string   `json:"labels"`
	Priority               int        `json:"priority"`
	DueAt                  *time.Time `json:"due_at,omitempty"`
	Pinned                 bool       `json:"pinned"`
	Blocked                bool       `json:"blocked"`
	BlockerReason          string     `json:"blocker_reason,omitempty"`
	DependsOn              []string   `json:"depends_on"`
	SourceURL              string     `json:"source_url,omitempty"`
	LoopID                 string     `json:"loop_id,omitempty"`
	EmployeeID             string     `json:"employee_id,omitempty"`
	EmployeeName           string     `json:"employee_name,omitempty"`
	Provider               string     `json:"provider,omitempty"`
	Model                  string     `json:"model,omitempty"`
	State                  string     `json:"state,omitempty"`
	StateSource            string     `json:"state_source,omitempty"`
	ProjectionReason       string     `json:"projection_reason"`
	AuthoritativeUpdatedAt time.Time  `json:"authoritative_updated_at"`
	SessionID              string     `json:"session_id,omitempty"`
	RunID                  string     `json:"run_id,omitempty"`
	SessionEventSequence   int64      `json:"session_event_sequence,omitempty"`
	SessionCount           int        `json:"session_count"`
	ApprovalStatus         string     `json:"approval_status"`
	VerificationStatus     string     `json:"verification_status"`
	Stale                  bool       `json:"stale"`
}

type TaskBoardView struct {
	SchemaVersion         int                        `json:"schema_version"`
	Definition            boardstore.Definition      `json:"definition"`
	Cards                 []TaskBoardCard            `json:"cards"`
	View                  boardstore.ViewPreferences `json:"view"`
	Filters               boardstore.Filters         `json:"filters"`
	UpdatedAt             time.Time                  `json:"updated_at"`
	ProjectionGeneratedAt time.Time                  `json:"projection_generated_at"`
}

type taskBoardEmployeeInfo struct {
	summary  employeestore.Summary
	provider string
	model    string
}

type TaskBoardSettingsInput struct {
	Definition boardstore.Definition      `json:"definition"`
	View       boardstore.ViewPreferences `json:"view"`
	Filters    boardstore.Filters         `json:"filters"`
}

type TaskBoardCardInput struct {
	ColumnID      string     `json:"column_id"`
	Rank          int64      `json:"rank"`
	Labels        []string   `json:"labels"`
	Priority      int        `json:"priority"`
	DueAt         *time.Time `json:"due_at"`
	Pinned        bool       `json:"pinned"`
	Blocked       bool       `json:"blocked"`
	BlockerReason string     `json:"blocker_reason"`
	DependsOn     []string   `json:"depends_on"`
	SourceURL     string     `json:"source_url"`
	LoopID        string     `json:"loop_id"`
}

type TaskBoardNoteInput struct {
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	ColumnID      string     `json:"column_id"`
	Rank          int64      `json:"rank"`
	Labels        []string   `json:"labels"`
	Priority      int        `json:"priority"`
	DueAt         *time.Time `json:"due_at"`
	Pinned        bool       `json:"pinned"`
	SourceURL     string     `json:"source_url"`
	BlockerReason string     `json:"blocker_reason"`
}

func (s *Service) GetTaskBoard(ctx context.Context) (TaskBoardView, error) {
	document, err := s.board.Load()
	if err != nil {
		return TaskBoardView{}, classified(KindInternal, err)
	}
	metadata := make(map[string]boardstore.CardMetadata, len(document.Cards))
	for _, card := range document.Cards {
		metadata[card.ID] = card
		if card.TaskID != "" {
			metadata[card.TaskID] = card
		}
	}

	tasks := make([]EmployeeTaskView, 0)
	employeeInfo := make(map[string]taskBoardEmployeeInfo)
	var cursor string
	for {
		page, listErr := s.employees.List(employeestore.ListOptions{Limit: employeestore.MaxPageSize, Cursor: cursor})
		if listErr != nil {
			return TaskBoardView{}, classifyEmployeeStore(listErr)
		}
		for _, summary := range page.Employees {
			record, recordErr := s.employees.Get(summary.ID)
			if recordErr != nil {
				return TaskBoardView{}, classifyEmployeeStore(recordErr)
			}
			employeeInfo[summary.ID] = taskBoardEmployeeInfo{
				summary: summary, provider: record.Employee.DefaultSelection.Company,
				model: record.Employee.DefaultSelection.Model,
			}
			var taskCursor string
			for {
				taskPage, taskErr := s.ListEmployeeTasks(ctx, summary.ID, employeestore.TaskListOptions{Limit: employeestore.MaxTaskPageSize, Cursor: taskCursor})
				if taskErr != nil {
					return TaskBoardView{}, taskErr
				}
				tasks = append(tasks, taskPage.Tasks...)
				if taskPage.NextCursor == "" {
					break
				}
				taskCursor = taskPage.NextCursor
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	cards := make([]TaskBoardCard, 0, len(tasks)+len(document.Cards))
	seenTasks := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		seenTasks[task.ID] = struct{}{}
		card, cardErr := s.projectTaskBoardCard(ctx, task, metadata[task.ID], employeeInfo[task.EmployeeID])
		if cardErr != nil {
			return TaskBoardView{}, cardErr
		}
		cards = append(cards, card)
	}
	for _, card := range document.Cards {
		if card.Kind != boardstore.CardNote || card.TaskID != "" {
			continue
		}
		if _, exists := seenTasks[card.ID]; exists {
			continue
		}
		cards = append(cards, TaskBoardCard{
			ID: card.ID, Kind: string(boardstore.CardNote), Title: card.Title, Body: card.Body,
			ColumnID: card.ColumnID, Rank: card.Rank, Labels: append([]string{}, card.Labels...),
			Priority: card.Priority, DueAt: cloneBoardTime(card.DueAt), Pinned: card.Pinned,
			Blocked: card.Blocked, BlockerReason: card.BlockerReason,
			DependsOn: append([]string{}, card.DependsOn...), SourceURL: card.SourceURL,
			LoopID: card.LoopID, State: "note", StateSource: "board_metadata",
			ProjectionReason: "note_metadata", AuthoritativeUpdatedAt: card.UpdatedAt,
			ApprovalStatus: "none", VerificationStatus: "none",
		})
	}
	return TaskBoardView{
		SchemaVersion: document.SchemaVersion, Definition: document.Definition, Cards: cards,
		View: document.View, Filters: document.Filters, UpdatedAt: document.UpdatedAt,
		ProjectionGeneratedAt: time.Now().UTC(),
	}, nil
}

func (s *Service) UpdateTaskBoardSettings(ctx context.Context, input TaskBoardSettingsInput) (TaskBoardView, error) {
	document, err := s.board.Load()
	if err != nil {
		return TaskBoardView{}, classified(KindInternal, err)
	}
	document.Definition, document.View, document.Filters = input.Definition, input.View, input.Filters
	document.UpdatedAt = time.Now().UTC()
	if err := s.board.Save(document); err != nil {
		return TaskBoardView{}, classified(KindInvalid, err)
	}
	return s.GetTaskBoard(ctx)
}

func (s *Service) UpdateTaskBoardCard(ctx context.Context, taskID string, input TaskBoardCardInput) (TaskBoardView, error) {
	if err := validateBoardID(taskID); err != nil {
		return TaskBoardView{}, classified(KindInvalid, err)
	}
	if _, err := s.employees.GetTask(taskID); err != nil {
		if !errors.Is(err, employeestore.ErrNotFound) {
			return TaskBoardView{}, classifyEmployeeStore(err)
		}
		return s.moveTaskBoardNoteCard(ctx, taskID, input)
	}
	document, err := s.board.Load()
	if err != nil {
		return TaskBoardView{}, classified(KindInternal, err)
	}
	position := -1
	for index := range document.Cards {
		if document.Cards[index].ID == taskID || document.Cards[index].TaskID == taskID {
			position = index
			break
		}
	}
	if position < 0 {
		document.Cards = append(document.Cards, boardstore.CardMetadata{ID: taskID, TaskID: taskID, Kind: boardstore.CardTask})
		position = len(document.Cards) - 1
	}
	card := &document.Cards[position]
	if card.Kind != boardstore.CardTask || card.TaskID != taskID {
		return TaskBoardView{}, classified(KindConflict, errors.New("task board card is not a task reference"))
	}
	card.ColumnID, card.Rank = input.ColumnID, input.Rank
	card.Labels, card.Priority, card.DueAt = append([]string{}, input.Labels...), input.Priority, cloneBoardTime(input.DueAt)
	card.Pinned, card.Blocked, card.BlockerReason = input.Pinned, input.Blocked, input.BlockerReason
	card.DependsOn, card.SourceURL, card.LoopID = append([]string{}, input.DependsOn...), input.SourceURL, input.LoopID
	card.UpdatedAt = time.Now().UTC()
	document.UpdatedAt = card.UpdatedAt
	if err := s.board.Save(document); err != nil {
		return TaskBoardView{}, classified(KindInvalid, err)
	}
	return s.GetTaskBoard(ctx)
}

// moveTaskBoardNoteCard moves a Note card between columns. Notes never carry
// execution semantics, so only the column and rank metadata may change.
func (s *Service) moveTaskBoardNoteCard(ctx context.Context, cardID string, input TaskBoardCardInput) (TaskBoardView, error) {
	document, err := s.board.Load()
	if err != nil {
		return TaskBoardView{}, classified(KindInternal, err)
	}
	for index := range document.Cards {
		card := &document.Cards[index]
		if card.ID != cardID || card.TaskID != "" {
			continue
		}
		if card.Kind != boardstore.CardNote {
			return TaskBoardView{}, classified(KindConflict, errors.New("task board card is not a note"))
		}
		card.ColumnID, card.Rank = input.ColumnID, input.Rank
		card.UpdatedAt = time.Now().UTC()
		document.UpdatedAt = card.UpdatedAt
		if err := s.board.Save(document); err != nil {
			return TaskBoardView{}, classified(KindInvalid, err)
		}
		return s.GetTaskBoard(ctx)
	}
	return TaskBoardView{}, classified(KindNotFound, errors.New("task board card not found"))
}

func (s *Service) CreateTaskBoardNote(ctx context.Context, input TaskBoardNoteInput) (TaskBoardView, error) {
	now := time.Now().UTC()
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return TaskBoardView{}, classified(KindInternal, err)
	}
	document, err := s.board.Load()
	if err != nil {
		return TaskBoardView{}, classified(KindInternal, err)
	}
	document.Cards = append(document.Cards, boardstore.CardMetadata{
		ID: "note-" + hex.EncodeToString(random[:]), Kind: boardstore.CardNote, Title: input.Title,
		Body: input.Body, ColumnID: input.ColumnID, Rank: input.Rank, Labels: append([]string{}, input.Labels...),
		Priority: input.Priority, DueAt: cloneBoardTime(input.DueAt), Pinned: input.Pinned,
		SourceURL: input.SourceURL, BlockerReason: input.BlockerReason, UpdatedAt: now,
	})
	document.UpdatedAt = now
	if err := s.board.Save(document); err != nil {
		return TaskBoardView{}, classified(KindInvalid, err)
	}
	return s.GetTaskBoard(ctx)
}

func (s *Service) projectTaskBoardCard(ctx context.Context, task EmployeeTaskView, metadata boardstore.CardMetadata, info taskBoardEmployeeInfo) (TaskBoardCard, error) {
	columnID := metadata.ColumnID
	if columnID == "" {
		columnID = "todo"
	}
	card := TaskBoardCard{
		ID: task.ID, TaskID: task.ID, Kind: string(boardstore.CardTask), Title: task.Prompt,
		ColumnID: columnID, Rank: metadata.Rank, Labels: append([]string{}, metadata.Labels...),
		Priority: metadata.Priority, DueAt: cloneBoardTime(metadata.DueAt), Pinned: metadata.Pinned,
		Blocked: metadata.Blocked, BlockerReason: metadata.BlockerReason,
		DependsOn: append([]string{}, metadata.DependsOn...), SourceURL: metadata.SourceURL, LoopID: metadata.LoopID,
		EmployeeID: task.EmployeeID, EmployeeName: info.summary.Name, Provider: info.provider, Model: info.model, State: string(task.State),
		SessionID: task.SessionID, RunID: task.RunID, SessionCount: 0,
		StateSource: "employee_task", ProjectionReason: "queued_task",
		AuthoritativeUpdatedAt: task.UpdatedAt, ApprovalStatus: "none", VerificationStatus: "none",
	}
	if task.SessionID == "" {
		card.ColumnID = "todo"
		if task.State == EmployeeTaskStatePrepared {
			card.ProjectionReason, card.StateSource = "prepared_task", "dispatch_session"
		}
		return card, nil
	}
	card.SessionCount = 1
	card.StateSource = "session_run"
	sess, err := s.store.Load(ctx, task.SessionID)
	if err != nil {
		return TaskBoardCard{}, classified(KindInternal, err)
	}
	card.SessionEventSequence = int64(sess.NextEventSequence)
	if sess.UpdatedAt.After(card.AuthoritativeUpdatedAt) {
		card.AuthoritativeUpdatedAt = sess.UpdatedAt
	}
	for _, request := range sess.ApprovalRequests {
		if request.RunID != task.RunID {
			continue
		}
		if request.Status == "pending" {
			card.ApprovalStatus = "pending"
		} else if card.ApprovalStatus == "none" {
			card.ApprovalStatus = string(request.Status)
		}
	}
	switch task.State {
	case EmployeeTaskStateRunning:
		card.ColumnID, card.ProjectionReason = "in_progress", "run_running"
	case EmployeeTaskStateVerifying, EmployeeTaskStateWaitingOwner:
		card.ColumnID, card.ProjectionReason = "review", "approval_or_verification"
		if task.State == EmployeeTaskStateVerifying {
			card.VerificationStatus = "pending"
		}
	case EmployeeTaskStateCompleted:
		card.ColumnID, card.ProjectionReason, card.VerificationStatus = "done", "verified_completed", "passed"
	case EmployeeTaskStateFailed:
		card.ProjectionReason, card.VerificationStatus = "run_failed", "failed"
	case EmployeeTaskStateInterrupted:
		card.ProjectionReason = "run_interrupted"
	case EmployeeTaskStateCancelled:
		card.ProjectionReason = "run_cancelled"
	default:
		card.ColumnID, card.ProjectionReason = "todo", "prepared_task"
	}
	return card, nil
}

func validateBoardID(value string) error {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\\x00\r\n") {
		return errors.New("task board id is invalid")
	}
	return nil
}

func cloneBoardTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}
