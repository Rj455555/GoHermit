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
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/approval"
	modelauth "github.com/Rj455555/GoHermit/internal/auth"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/owner"
	"github.com/Rj455555/GoHermit/internal/session"
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
	Workspace     string
	ConfigPath    string
	active        atomic.Bool
	store         *session.Store
	runMu         sync.Mutex
	activeSession string
	activeRun     string
	cancelRun     context.CancelFunc
	publish       Publisher
	credentials   *modelauth.Store
	owner         *owner.Store
	logins        *modelauth.LoginManager
	build         func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error)
	codexModelsMu sync.Mutex
	codexModels   []config.ModelOption
	codexModelsAt time.Time
	teamWorker    team.Worker
	teamTemplates *teamtemplate.Store
	// approvals is the single in-process rendezvous between parked runners
	// and DecideApproval for the whole service lifetime (ADR 0011, C3).
	approvals *approvalBroker
	// teamTemplatesErr defers store-resolution failure to request time so a
	// team session fails closed instead of the service failing to start.
	teamTemplatesErr error
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
	return &Service{
		Workspace: workspace, ConfigPath: configPath,
		publish:       publish,
		store:         store,
		credentials:   credentials,
		owner:         ownerStore,
		logins:        modelauth.NewLoginManager(credentials),
		teamTemplates: teamTemplates, teamTemplatesErr: teamTemplatesErr,
		approvals: broker,
		build: func(ctx context.Context, workspace, configPath string, selection config.RuntimeSelection, apiKey string, models []config.ModelOption) (*app.Runtime, error) {
			return app.BuildRuntimeWithOptions(ctx, workspace, configPath, app.RuntimeOptions{Selection: &selection, APIKey: apiKey, Models: models, Approvals: broker}, nil)
		},
	}, nil
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
	sess, err := s.store.Load(ctx, id)
	if err != nil {
		return nil, nil, classified(KindNotFound, err)
	}
	messages, err := s.store.Messages(sess.ID)
	if err != nil {
		return nil, nil, classified(KindInternal, err)
	}
	return sess, messages, nil
}

// LoadSession loads one session, reporting KindNotFound when it does not
// exist. It backs existence checks before event streaming.
func (s *Service) LoadSession(ctx context.Context, id string) (*session.Session, error) {
	sess, err := s.store.Load(ctx, id)
	if err != nil {
		return nil, classified(KindNotFound, err)
	}
	return sess, nil
}

// SessionEvents returns the durable event journal of one session after the
// given sequence.
func (s *Service) SessionEvents(ctx context.Context, id string, after uint64) ([]event.Event, error) {
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
