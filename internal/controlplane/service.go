// Package controlplane holds GoHermit's application services: session and
// run lifecycle, team execution, approval coordination, and durable event
// commit/publish. Transports — the web server today, a CLI command or the
// Loop Invocation dispatcher tomorrow — call Service methods directly; the
// package never imports net/http or any other transport concern. The
// dependency direction is web/cli → controlplane → domain packages
// (app/agent/session/team/...); domain packages never import web or
// controlplane.
package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/approval"
	modelauth "github.com/Rj455555/GoHermit/internal/auth"
	"github.com/Rj455555/GoHermit/internal/boardstore"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/employee"
	"github.com/Rj455555/GoHermit/internal/employeestore"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/knowledge"
	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/loopstore"
	"github.com/Rj455555/GoHermit/internal/notify"
	"github.com/Rj455555/GoHermit/internal/owner"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/skill"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
)

// Publisher is the port through which the service makes committed events
// visible. The web server implements it with its SSE subscriber fan-out; a
// CLI can implement it with terminal rendering. The service always commits
// durably BEFORE invoking the publisher.
type Publisher func(event.Event)

// Kind classifies a service failure so transports can map it to their own
// status model (HTTP codes, CLI exit codes) without parsing messages.
type Kind int

const (
	// KindInvalid is a caller input failure (HTTP 400).
	KindInvalid Kind = iota
	// KindNotFound is a missing session, run, or approval request (HTTP 404).
	KindNotFound
	// KindConflict is a state conflict such as an active run, an expired or
	// already-decided approval, or a non-resumable run (HTTP 409).
	KindConflict
	// KindInternal is a persistence or runtime failure (HTTP 500).
	KindInternal
	// KindBadGateway is an upstream identity-provider failure (HTTP 502).
	KindBadGateway
)

// Error is a classified service failure. Message carries the exact
// user-facing text; Request carries the affected approval request when the
// failure concerns one, so transports can echo it like the pre-refactor API
// did.
type Error struct {
	Kind    Kind
	Message string
	Request *approval.Request
}

func (e *Error) Error() string { return e.Message }

func classified(kind Kind, err error) *Error {
	return &Error{Kind: kind, Message: err.Error()}
}

// Service is the control-plane application service. It owns session/run
// state transitions, team execution, approval coordination, and the durable
// event journal; transports wrap it with request parsing and response
// writing.
type Service struct {
	Workspace             string
	ConfigPath            string
	active                atomic.Bool
	store                 *session.Store
	runMu                 sync.Mutex
	prepareMu             sync.Mutex
	employeeTaskMu        sync.Mutex
	loopScheduleMu        sync.Mutex
	activeSession         string
	activeRun             string
	cancelRun             context.CancelFunc
	publish               Publisher
	credentials           *modelauth.Store
	owner                 *owner.Store
	logins                *modelauth.LoginManager
	build                 func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error)
	buildEmployee         func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption, employee.EffectivePolicy) (*app.Runtime, error)
	codexModelsMu         sync.Mutex
	codexModels           []config.ModelOption
	codexModelsAt         time.Time
	teamWorker            team.Worker
	teamTemplates         *teamtemplate.Store
	loopStore             *loopstore.Store
	employees             *employeestore.Store
	board                 *boardstore.Store
	skills                *skill.Catalog
	knowledge             *knowledge.Catalog
	prepareStageHook      func(string) error
	employeeTaskStageHook func(string) error
	// approvals is the single in-process rendezvous between parked runners
	// and DecideApproval for the whole service lifetime (ADR 0011, C3).
	approvals *approvalBroker
	// teamTemplatesErr defers store-resolution failure to request time so a
	// team session fails closed instead of the service failing to start.
	teamTemplatesErr error
	// loopStoreErr defers loop store resolution failure the same way.
	loopStoreErr           error
	notificationConfig     notify.Config
	notificationMu         sync.Mutex
	notificationDeliveryMu sync.Mutex
	notificationLastError  string
	notificationLastSent   *time.Time
}

// New builds the service over the workspace, recovering every persisted
// session, and wires the approval broker into runtime construction exactly
// like the pre-refactor web server did. publish may be nil for embedders
// that only read the journal.
func New(workspace, configPath string, publish Publisher) (*Service, error) {
	credentials, err := modelauth.NewStore("")
	if err != nil {
		return nil, err
	}
	ownerStore, err := owner.NewStore("")
	if err != nil {
		return nil, err
	}
	conf, err := app.LoadConfig(workspace, configPath)
	if err != nil {
		return nil, err
	}
	teamTemplates, teamTemplatesErr := teamtemplate.NewStore("")
	loopStore, loopStoreErr := loopstore.NewStore("")
	employeeStore, err := employeestore.NewStore("")
	if err != nil {
		return nil, err
	}
	boardRoot := filepath.Join(workspace, ".gohermit", "board")
	if configured := strings.TrimSpace(os.Getenv("GOHERMIT_BOARD_STORE")); configured != "" {
		boardRoot = configured
	}
	boardStore, err := boardstore.NewStore(boardRoot, "owner", workspace)
	if err != nil {
		return nil, err
	}
	skillCatalog, err := skill.NewCatalog("")
	if err != nil {
		return nil, err
	}
	knowledgeCatalog, err := knowledge.NewCatalog("")
	if err != nil {
		return nil, err
	}
	store, err := session.NewStore(workspace, conf.Storage.Directory)
	if err != nil {
		return nil, err
	}
	if ids, listErr := store.List(); listErr == nil {
		for _, id := range ids {
			_, _ = store.Recover(context.Background(), id)
		}
	}
	broker := newApprovalBroker()
	notificationConfig := notify.ConfigFromEnv()
	return &Service{
		Workspace: workspace, ConfigPath: configPath,
		publish:       publish,
		store:         store,
		credentials:   credentials,
		owner:         ownerStore,
		logins:        modelauth.NewLoginManager(credentials),
		teamTemplates: teamTemplates, teamTemplatesErr: teamTemplatesErr,
		loopStore: loopStore, loopStoreErr: loopStoreErr,
		notificationConfig: notificationConfig,
		employees:          employeeStore,
		board:              boardStore,
		skills:             skillCatalog,
		knowledge:          knowledgeCatalog,
		approvals:          broker,
		build: func(ctx context.Context, workspace, configPath string, selection config.RuntimeSelection, apiKey string, models []config.ModelOption) (*app.Runtime, error) {
			return app.BuildRuntimeWithOptions(ctx, workspace, configPath, app.RuntimeOptions{Selection: &selection, APIKey: apiKey, Models: models, Approvals: broker}, nil)
		},
		buildEmployee: func(ctx context.Context, workspace, configPath string, selection config.RuntimeSelection, apiKey string, models []config.ModelOption, policy employee.EffectivePolicy) (*app.Runtime, error) {
			return app.BuildRuntimeWithOptions(ctx, workspace, configPath, app.RuntimeOptions{
				Selection: &selection, APIKey: apiKey, Models: models, Approvals: broker,
				EffectivePolicy: &policy,
			}, nil)
		},
	}, nil
}

// EmailNotificationStatus is the safe Settings projection for completion
// notifications. It never contains the SMTP password or provider tokens.
type EmailNotificationStatus struct {
	Configured         bool       `json:"configured"`
	EmailConfigured    bool       `json:"email_configured"`
	OpenClawConfigured bool       `json:"openclaw_configured"`
	Recipient          string     `json:"recipient"`
	From               string     `json:"from,omitempty"`
	Host               string     `json:"host,omitempty"`
	OpenClawChannel    string     `json:"openclaw_channel,omitempty"`
	OpenClawTarget     string     `json:"openclaw_target,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
	LastSentAt         *time.Time `json:"last_sent_at,omitempty"`
}

func (s *Service) EmailNotificationStatus() EmailNotificationStatus {
	s.notificationMu.Lock()
	defer s.notificationMu.Unlock()
	return EmailNotificationStatus{
		Configured:         s.notificationConfig.AnyConfigured(),
		EmailConfigured:    s.notificationConfig.Configured(),
		OpenClawConfigured: s.notificationConfig.OpenClaw.Configured(),
		Recipient:          s.notificationConfig.To,
		From:               s.notificationConfig.From,
		Host:               s.notificationConfig.Host,
		OpenClawChannel:    s.notificationConfig.OpenClaw.Channel,
		OpenClawTarget:     s.notificationConfig.OpenClaw.Target,
		LastError:          s.notificationLastError,
		LastSentAt:         cloneTime(s.notificationLastSent),
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// notifyLoopCompletion delivers one bounded terminal outcome. The marker is
// persisted only after delivery succeeds, so a restart can retry a failed
// notification without creating a second execution state machine.
func (s *Service) notifyLoopCompletion(ctx context.Context, invocation loop.Invocation) {
	s.notifyTerminalCompletion(ctx, invocation.ID, invocation.ID, invocation.DefinitionSnapshot.Name, "loop", invocation.Status, invocation.FinishedAt, invocation.FailureCode, invocation.FailureSummary)
}

// notifyEmployeeTaskCompletion uses the same bounded delivery path as Loop
// Invocations. The prefixed key keeps Task markers separate from Invocation
// markers while preserving one idempotent notification ledger.
func (s *Service) notifyEmployeeTaskCompletion(ctx context.Context, task employee.EmployeeTask, status employee.TaskState, finishedAt *time.Time, summary string) {
	sum := sha256.Sum256([]byte("employee-task-notification\x00" + task.ID))
	key := "task-" + hex.EncodeToString(sum[:16])
	s.notifyTerminalCompletion(ctx, key, task.ID, task.EmployeeSnapshot.Employee.Name, "employee_task", loop.Status(status), finishedAt, "", summary)
}

func (s *Service) notifyTerminalCompletion(ctx context.Context, key, displayID, name, sourceType string, status loop.Status, finishedAt *time.Time, failureCode, summary string) {
	if !status.Terminal() || s.loopStore == nil {
		return
	}
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s.notificationDeliveryMu.Lock()
	defer s.notificationDeliveryMu.Unlock()
	summary = clipNotificationText(summary)
	err := s.loopStore.SaveReport(loopstore.ReportRecord{
		ID: key, SourceType: sourceType, SourceID: displayID, Title: clipNotificationText(name),
		Status: status, FailureCode: clipNotificationText(failureCode), Summary: summary,
		FinishedAt: finishedAt, DeliveryStatus: "pending",
	})
	if err != nil {
		s.notificationMu.Lock()
		s.notificationLastError = "保存汇报记录失败"
		s.notificationMu.Unlock()
		return
	}
	report, err := s.loopStore.GetReport(key)
	if err == nil && report.DeliveryStatus == "sent" {
		return
	}
	sent, err := s.loopStore.NotificationSent(key, status)
	if err != nil {
		s.notificationMu.Lock()
		s.notificationLastError = "读取通知状态失败"
		s.notificationMu.Unlock()
		return
	}
	if sent {
		now := time.Now().UTC()
		report.DeliveryStatus, report.DeliveryChannel, report.DeliveredAt, report.LastError = "sent", "legacy", &now, ""
		_ = s.loopStore.SaveReport(report)
		return
	}
	subject := fmt.Sprintf("GoHermit 任务状态：%s", name)
	body := fmt.Sprintf("Target: %s\nID: %s\nStatus: %s\nFinished: %s\n",
		name, displayID, status, formatTime(finishedAt))
	if failureCode != "" {
		body += fmt.Sprintf("Failure: %s\n", failureCode)
	}
	if summary != "" {
		body += "Summary: " + summary + "\n"
	}
	var channel string
	if s.notificationConfig.OpenClaw.Configured() {
		err = s.notificationConfig.OpenClaw.Send(deliveryCtx, key, name, body)
		channel = "openclaw-weixin"
	}
	if err != nil && s.notificationConfig.Configured() {
		err = s.notificationConfig.SendTLS(deliveryCtx, subject, body)
		channel = "email"
	}
	if !s.notificationConfig.AnyConfigured() {
		err = errors.New("汇报通道未配置")
	}
	if err != nil {
		report.DeliveryStatus, report.DeliveryChannel, report.LastError = "failed", channel, clipNotificationText(err.Error())
		_ = s.loopStore.SaveReport(report)
		s.notificationMu.Lock()
		s.notificationLastError = report.LastError
		s.notificationMu.Unlock()
		return
	}
	now := time.Now().UTC()
	if err = s.loopStore.MarkNotificationSent(key, status, now); err != nil {
		s.notificationMu.Lock()
		s.notificationLastError = "保存通知状态失败"
		s.notificationMu.Unlock()
		return
	}
	report.DeliveryStatus, report.DeliveryChannel, report.DeliveredAt, report.LastError = "sent", channel, &now, ""
	_ = s.loopStore.SaveReport(report)
	s.notificationMu.Lock()
	s.notificationLastError = ""
	s.notificationLastSent = &now
	s.notificationMu.Unlock()
}

// ListReports returns the bounded report-center projection.
func (s *Service) ListReports(_ context.Context, limit int) ([]loopstore.ReportRecord, error) {
	if s.loopStore == nil {
		return nil, classified(KindInternal, errors.New("loop store unavailable"))
	}
	reports, err := s.loopStore.ListReports(limit)
	if err != nil {
		return nil, classified(KindInternal, err)
	}
	return reports, nil
}

// RetryReport reuses the immutable report snapshot and never starts a new
// Loop/Task execution.
func (s *Service) RetryReport(ctx context.Context, id string) (loopstore.ReportRecord, error) {
	if s.loopStore == nil {
		return loopstore.ReportRecord{}, classified(KindInternal, errors.New("loop store unavailable"))
	}
	report, err := s.loopStore.GetReport(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return loopstore.ReportRecord{}, classified(KindNotFound, err)
		}
		return loopstore.ReportRecord{}, classified(KindInternal, err)
	}
	s.notifyTerminalCompletion(ctx, report.ID, report.SourceID, report.Title, report.SourceType, report.Status, report.FinishedAt, report.FailureCode, report.Summary)
	updated, err := s.loopStore.GetReport(id)
	if err != nil {
		return loopstore.ReportRecord{}, classified(KindInternal, err)
	}
	return updated, nil
}

func formatTime(value *time.Time) string {
	if value == nil {
		return "未结束"
	}
	return value.UTC().Format(time.RFC3339)
}

func clipNotificationText(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if len(value) > 512 {
		return value[:512] + "…"
	}
	return value
}

// Active reports whether a run currently occupies the workspace.
func (s *Service) Active() bool { return s.active.Load() }

// TryAcquireRun takes the workspace run gate; it is the primitive RunOnce
// and launchSessionRun build on. Transports that must probe or hold the
// gate (e.g. the legacy one-shot run, or a test simulating an occupied
// workspace) use it together with ReleaseRun.
func (s *Service) TryAcquireRun() bool { return s.active.CompareAndSwap(false, true) }

// ReleaseRun frees the workspace run gate taken by TryAcquireRun.
func (s *Service) ReleaseRun() { s.active.Store(false) }

// emit delivers a committed event through the Publisher port.
func (s *Service) emit(runtimeEvent event.Event) {
	if s.publish != nil {
		s.publish(runtimeEvent)
	}
}

// ListSessions returns the persisted session summaries, most recent first.
func (s *Service) ListSessions(ctx context.Context, limit int) ([]session.SessionSummary, error) {
	items, err := s.store.ListSummaries(ctx, limit)
	if err != nil {
		return nil, classified(KindInternal, err)
	}
	return items, nil
}

// GetSession loads one session and its visible message history.
func (s *Service) GetSession(ctx context.Context, id string) (*session.Session, []session.MessageRecord, error) {
	sess, err := s.loadPublicSession(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	messages, err := s.store.Messages(sess.ID)
	if err != nil {
		return nil, nil, classified(KindInternal, err)
	}
	return sess, messages, nil
}

// LoadSession loads a public Session, reporting the same KindNotFound response
// for absent and hidden Team Worker Sessions. It is the reusable authorization
// boundary for public transports; internal Team execution and recovery use the
// Session Store directly.
func (s *Service) LoadSession(ctx context.Context, id string) (*session.Session, error) {
	return s.loadPublicSession(ctx, id)
}

func (s *Service) loadPublicSession(ctx context.Context, id string) (*session.Session, error) {
	sess, err := s.store.Load(ctx, id)
	if err != nil {
		return nil, classified(KindNotFound, err)
	}
	if err = requirePublicSession(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Service) recoverPublicSession(ctx context.Context, id string) (*session.Session, error) {
	if _, err := s.loadPublicSession(ctx, id); err != nil {
		return nil, err
	}
	sess, err := s.store.Recover(ctx, id)
	if err != nil {
		return nil, classified(KindNotFound, err)
	}
	if err = requirePublicSession(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func requirePublicSession(sess *session.Session) error {
	if sess == nil || sess.Hidden {
		// Deliberately identical to an absent Session: no public caller may
		// learn whether a guessed hidden Worker Session ID exists.
		return &Error{Kind: KindNotFound, Message: "session not found"}
	}
	return nil
}

// SessionEvents returns the durable event journal of one session after the
// given sequence.
func (s *Service) SessionEvents(ctx context.Context, id string, after uint64) ([]event.Event, error) {
	if _, err := s.loadPublicSession(ctx, id); err != nil {
		return nil, err
	}
	events, err := s.store.Events(id, after)
	if err != nil {
		return nil, classified(KindNotFound, err)
	}
	return events, nil
}

// RunOnceInput is the legacy single-shot task input: no session is
// persisted, the run streams its events through the sink and is gone.
type RunOnceInput struct {
	Task    string
	Company string
	Access  string
	Model   string
	Agent   string
}

// RunOnce executes the legacy single-shot task run. The caller must hold
// the workspace run gate (TryAcquireRun) for the whole call and release it
// with ReleaseRun afterwards, mirroring the pre-refactor /api/run endpoint
// which took the gate before parsing the request. Failures before streaming
// starts are returned as classified errors; once the run is streaming, a
// failure is delivered as a task_failed event through the sink instead.
func (s *Service) RunOnce(ctx context.Context, in RunOnceInput, sink func(event.Event)) error {
	in.Task = strings.TrimSpace(in.Task)
	if in.Task == "" || len(in.Task) > 16<<10 {
		return &Error{Kind: KindInvalid, Message: "task must contain 1 to 16384 bytes"}
	}
	selection := config.RuntimeSelection{Company: in.Company, Access: in.Access, Model: in.Model, Agent: in.Agent}
	var liveModels []config.ModelOption
	if selection.Access == "openai-codex" {
		models, modelErr := s.codexCatalog(ctx)
		if modelErr != nil {
			return &Error{Kind: KindInvalid, Message: "无法读取 Codex 账户的可用模型，请重新登录后再试"}
		}
		liveModels = models
	}
	if _, _, err := config.ResolveSelectionWithModels(selection, liveModels); err != nil {
		return classified(KindInvalid, err)
	}
	if selection.Agent == "team" {
		return &Error{Kind: KindInvalid, Message: "the team profile requires the Session/Run API"}
	}
	apiKey, err := s.resolveCredential(ctx, selection)
	if err != nil {
		return classified(KindInvalid, err)
	}
	runtime, err := s.build(ctx, s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
	if err != nil {
		return classified(KindInvalid, err)
	}
	s.applyOwner(runtime)
	defer runtime.Close()
	sess, err := session.New(in.Task, runtime.Workspace, session.ConfigDigest(runtime.Config))
	if err != nil {
		return classified(KindInternal, err)
	}
	sess.GitState = session.GitState(ctx, runtime.Workspace)
	runtime.Runner.Sink = sink
	err = runtime.Runner.Run(ctx, sess)
	s.approvals.Release(sess.ID)
	if err != nil && !errors.Is(err, context.Canceled) {
		sink(event.Event{Type: event.TaskFailed, Time: time.Now().UTC(), SessionID: sess.ID, Error: err.Error()})
	}
	return nil
}

// codexCatalog caches the live Codex model list for five minutes.
func (s *Service) codexCatalog(ctx context.Context) ([]config.ModelOption, error) {
	s.codexModelsMu.Lock()
	defer s.codexModelsMu.Unlock()
	if len(s.codexModels) > 0 && time.Since(s.codexModelsAt) < 5*time.Minute {
		return append([]config.ModelOption(nil), s.codexModels...), nil
	}
	credentials, err := modelauth.ResolveCodexWithStore(ctx, s.credentials)
	if err != nil {
		return nil, err
	}
	discovered, err := modelauth.DiscoverCodexModels(ctx, credentials.Token)
	if err != nil {
		return nil, err
	}
	models := make([]config.ModelOption, 0, len(discovered))
	for _, model := range discovered {
		models = append(models, config.ModelOption{ID: model.ID, Label: model.ID, Provider: "openai-codex"})
	}
	s.codexModels = models
	s.codexModelsAt = time.Now()
	return append([]config.ModelOption(nil), models...), nil
}

// CodexCatalog returns the live model catalog of the logged-in Codex
// account, cached for five minutes.
func (s *Service) CodexCatalog(ctx context.Context) ([]config.ModelOption, error) {
	return s.codexCatalog(ctx)
}

// AccessStatus reports whether one access preset has usable credentials,
// where they come from, and the owner-facing detail text.
func (s *Service) AccessStatus(ctx context.Context, access config.AccessPreset) (bool, string, string) {
	if access.AuthType == "oauth_external" || access.ID == "openai-codex" {
		configured, detail := modelauth.CodexStatus(ctx, s.credentials)
		if configured {
			if strings.Contains(detail, "auth.json") {
				detail = "Codex CLI"
			}
			return true, detail, "登录有效，可以运行。"
		}
		return false, "", "登录不存在或已失效，请重新登录。"
	}
	if key, ok := s.credentials.APIKey(access.ID); ok && key != "" {
		return true, "GoHermit 设置", "API Key 已安全保存。"
	}
	if access.APIKeyEnv != "" && strings.TrimSpace(os.Getenv(access.APIKeyEnv)) != "" {
		return true, "环境变量 " + access.APIKeyEnv, "由服务端环境提供。"
	}
	return false, "", "尚未设置 API Key。"
}

func (s *Service) accessStatus(ctx context.Context, access config.AccessPreset) (bool, string, string) {
	return s.AccessStatus(ctx, access)
}

func (s *Service) validateSelection(ctx context.Context, selection config.RuntimeSelection) ([]config.ModelOption, error) {
	var liveModels []config.ModelOption
	if selection.Access == "openai-codex" {
		models, err := s.codexCatalog(ctx)
		if err != nil {
			return nil, errors.New("无法读取 Codex 账户的可用模型，请重新登录后再试")
		}
		liveModels = models
	}
	if _, _, err := config.ResolveSelectionWithModels(selection, liveModels); err != nil {
		return nil, err
	}
	return liveModels, nil
}

func (s *Service) resolveCredential(ctx context.Context, selection config.RuntimeSelection) (string, error) {
	access, ok := config.AccessProfile(selection.Company, selection.Access)
	if !ok {
		return "", errors.New("未知的接入方式")
	}
	if access.AuthType == "oauth_external" {
		credentials, err := modelauth.ResolveCodexWithStore(ctx, s.credentials)
		if err != nil {
			return "", errors.New("Codex 登录不存在或已失效，请先到设置中登录")
		}
		return credentials.Token, nil
	}
	if key, ok := s.credentials.APIKey(access.ID); ok {
		return key, nil
	}
	if access.APIKeyEnv != "" {
		if key := strings.TrimSpace(os.Getenv(access.APIKeyEnv)); key != "" {
			return key, nil
		}
	}
	return "", fmt.Errorf("%s 尚未设置 API Key，请先到设置中配置", access.Label)
}

func (s *Service) applyOwner(runtime *app.Runtime) {
	if runtime == nil || runtime.Runner == nil || runtime.Runner.Context == nil {
		return
	}
	profile, err := s.owner.Load()
	if err == nil {
		runtime.Runner.Context.SetOwnerProfile(owner.Markdown(profile))
	}
}

func compactTitle(message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	if len(message) > 80 {
		return message[:80] + "…"
	}
	return message
}

// SaveAPIKey stores an API key for a provider in the credential store.
func (s *Service) SaveAPIKey(provider, key string) error {
	if err := s.credentials.SetAPIKey(provider, key); err != nil {
		return classified(KindInternal, err)
	}
	return nil
}

// DeleteCredentials removes every stored credential of a provider.
func (s *Service) DeleteCredentials(provider string) error {
	if err := s.credentials.Delete(provider); err != nil {
		return classified(KindInternal, err)
	}
	return nil
}

// OwnerProfile loads the owner profile.
func (s *Service) OwnerProfile() (owner.Profile, error) {
	profile, err := s.owner.Load()
	if err != nil {
		return owner.Profile{}, classified(KindInternal, err)
	}
	return profile, nil
}

// SaveOwnerProfile persists the owner profile and returns the reloaded
// state, like the pre-refactor endpoint did.
func (s *Service) SaveOwnerProfile(profile owner.Profile) (owner.Profile, error) {
	if err := s.owner.Save(profile); err != nil {
		return owner.Profile{}, classified(KindInvalid, err)
	}
	profile, _ = s.owner.Load()
	return profile, nil
}

// UpsertOwnerFact adds or replaces one owner fact and returns the profile.
func (s *Service) UpsertOwnerFact(fact owner.Fact) (owner.Profile, error) {
	profile, err := s.owner.UpsertFact(fact)
	if err != nil {
		return owner.Profile{}, classified(KindInvalid, err)
	}
	return profile, nil
}

// ForgetOwnerFact deletes one owner fact and returns the profile.
func (s *Service) ForgetOwnerFact(id string) (owner.Profile, error) {
	profile, err := s.owner.ForgetFact(id)
	if err != nil {
		return owner.Profile{}, classified(KindNotFound, err)
	}
	return profile, nil
}

// TeamTemplate loads the stored team template.
func (s *Service) TeamTemplate() (teamtemplate.Template, error) {
	if s.teamTemplatesErr != nil || s.teamTemplates == nil {
		return teamtemplate.Template{}, &Error{Kind: KindInternal, Message: "team template store unavailable"}
	}
	template, err := s.teamTemplates.Load()
	if err != nil {
		return teamtemplate.Template{}, classified(KindInternal, err)
	}
	return template, nil
}

// SaveTeamTemplate replaces the stored team template. Callers are expected
// to screen the template through teamtemplate.Import first.
func (s *Service) SaveTeamTemplate(template teamtemplate.Template) error {
	if s.teamTemplatesErr != nil || s.teamTemplates == nil {
		return &Error{Kind: KindInternal, Message: "team template store unavailable"}
	}
	if err := s.teamTemplates.Save(template); err != nil {
		return classified(KindInternal, err)
	}
	return nil
}

// StartLogin begins a Codex device login flow.
func (s *Service) StartLogin(ctx context.Context) (modelauth.LoginSession, error) {
	login, err := s.logins.Start(ctx)
	if err != nil {
		return modelauth.LoginSession{}, classified(KindBadGateway, err)
	}
	return login, nil
}

// LoginStatus reports a device login flow's state. An approved login
// invalidates the cached Codex model catalog so the next lookup reflects
// the new account.
func (s *Service) LoginStatus(id string) (modelauth.LoginSession, bool) {
	login, ok := s.logins.Status(id)
	if !ok {
		return modelauth.LoginSession{}, false
	}
	if login.Status == "approved" {
		s.codexModelsMu.Lock()
		s.codexModels = nil
		s.codexModelsAt = time.Time{}
		s.codexModelsMu.Unlock()
	}
	return login, true
}
