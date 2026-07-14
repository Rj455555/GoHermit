package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	modelauth "github.com/Rj455555/GoHermit/internal/auth"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/session"
)

//go:embed assets/*
var assets embed.FS

type Server struct {
	Workspace     string
	ConfigPath    string
	active        atomic.Bool
	store         *session.Store
	runMu         sync.Mutex
	activeSession string
	activeRun     string
	cancelRun     context.CancelFunc
	subscribersMu sync.Mutex
	subscribers   map[string]map[chan event.Event]struct{}
	static        http.Handler
	credentials   *modelauth.Store
	logins        *modelauth.LoginManager
	build         func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error)
	codexModelsMu sync.Mutex
	codexModels   []config.ModelOption
	codexModelsAt time.Time
}

func New(workspace, configPath string) (*Server, error) {
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		return nil, err
	}
	credentials, err := modelauth.NewStore("")
	if err != nil {
		return nil, err
	}
	conf, err := app.LoadConfig(workspace, configPath)
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
	return &Server{
		Workspace: workspace, ConfigPath: configPath,
		static:      http.FileServer(http.FS(root)),
		store:       store,
		credentials: credentials,
		logins:      modelauth.NewLoginManager(credentials),
		subscribers: map[string]map[chan event.Event]struct{}{},
		build: func(ctx context.Context, workspace, configPath string, selection config.RuntimeSelection, apiKey string, models []config.ModelOption) (*app.Runtime, error) {
			return app.BuildRuntimeWithOptions(ctx, workspace, configPath, app.RuntimeOptions{Selection: &selection, APIKey: apiKey, Models: models}, nil)
		},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("POST /api/run", s.run)
	mux.HandleFunc("POST /api/sessions", s.createSession)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.getSession)
	mux.HandleFunc("POST /api/sessions/{id}/runs", s.startSessionRun)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.sessionEvents)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/cancel", s.cancelSessionRun)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/resume", s.resumeSessionRun)
	mux.HandleFunc("PUT /api/settings/providers/{provider}/api-key", s.saveAPIKey)
	mux.HandleFunc("DELETE /api/settings/providers/{provider}/credentials", s.deleteCredentials)
	mux.HandleFunc("POST /api/settings/providers/openai-codex/login", s.startCodexLogin)
	mux.HandleFunc("GET /api/settings/logins/{session}", s.loginStatus)
	mux.Handle("GET /", s.static)
	return securityHeaders(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": app.Version, "active": s.active.Load()})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	conf, err := app.LoadConfig(s.Workspace, s.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	authStatus := make(map[string]map[string]any)
	companies := config.CompanyPresets()
	availableCompanies := make([]config.CompanyPreset, 0, len(companies))
	for companyIndex, company := range companies {
		available := company
		available.Access = nil
		for accessIndex, access := range company.Access {
			configured, source, detail := s.accessStatus(r.Context(), access)
			if configured && access.ID == "openai-codex" {
				models, modelErr := s.codexCatalog(r.Context())
				if modelErr != nil {
					configured, source, detail = false, "", "登录有效，但无法读取该账户的可用模型。"
				} else {
					access.Models = models
					companies[companyIndex].Access[accessIndex].Models = models
				}
			}
			authStatus[access.ID] = map[string]any{"configured": configured, "source": source, "detail": detail}
			if configured && access.Supported {
				available.Access = append(available.Access, access)
			}
		}
		if len(available.Access) > 0 {
			availableCompanies = append(availableCompanies, available)
		}
	}
	_, keyErr := conf.APIKey()
	currentConfigured := keyErr == nil
	if conf.Model.Provider == "openai-codex" {
		currentConfigured, _, _ = s.accessStatus(r.Context(), config.AccessPreset{ID: "openai-codex", AuthType: "oauth_external"})
	}
	selection := normalizeSelection(conf.CurrentSelection(), availableCompanies)
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   app.Version,
		"workspace": s.Workspace,
		"model": map[string]any{
			"provider": conf.Model.Provider, "protocol": conf.Model.Protocol(), "base_url": conf.Model.BaseURL,
			"model": conf.Model.Name, "api_key_env": conf.Model.APIKeyEnv, "api_key_configured": currentConfigured,
		},
		"selection": selection, "companies": companies, "available_companies": availableCompanies, "agents": config.AgentPresets(),
		"auth_status": authStatus, "active": s.active.Load(),
	})
}

func normalizeSelection(selection config.RuntimeSelection, companies []config.CompanyPreset) config.RuntimeSelection {
	if selection.Agent == "" {
		selection.Agent = "coding"
	}
	for _, company := range companies {
		for _, access := range company.Access {
			if company.ID != selection.Company || access.ID != selection.Access {
				continue
			}
			for _, model := range access.Models {
				if model.ID == selection.Model {
					return selection
				}
			}
		}
	}
	if len(companies) == 0 || len(companies[0].Access) == 0 || len(companies[0].Access[0].Models) == 0 {
		return selection
	}
	company, access := companies[0], companies[0].Access[0]
	selection.Company, selection.Access = company.ID, access.ID
	selection.Model = access.Models[0].ID
	if access.ID == "openai-codex" {
		for _, model := range access.Models {
			if model.ID == "gpt-5.4-mini" {
				selection.Model = model.ID
				break
			}
		}
	}
	return selection
}

func (s *Server) codexCatalog(ctx context.Context) ([]config.ModelOption, error) {
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

func (s *Server) accessStatus(ctx context.Context, access config.AccessPreset) (bool, string, string) {
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

func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	if !s.active.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "another task is already running"})
		return
	}
	defer s.active.Store(false)

	var request struct {
		Task    string `json:"task"`
		Company string `json:"company"`
		Access  string `json:"access"`
		Model   string `json:"model"`
		Agent   string `json:"agent"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request: " + err.Error()})
		return
	}
	request.Task = strings.TrimSpace(request.Task)
	if request.Task == "" || len(request.Task) > 16<<10 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task must contain 1 to 16384 bytes"})
		return
	}
	selection := config.RuntimeSelection{Company: request.Company, Access: request.Access, Model: request.Model, Agent: request.Agent}
	var liveModels []config.ModelOption
	if selection.Access == "openai-codex" {
		models, modelErr := s.codexCatalog(r.Context())
		if modelErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "无法读取 Codex 账户的可用模型，请重新登录后再试"})
			return
		}
		liveModels = models
	}
	if _, _, err := config.ResolveSelectionWithModels(selection, liveModels); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	apiKey, err := s.resolveCredential(r.Context(), selection)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	runtime, err := s.build(r.Context(), s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	defer runtime.Close()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming is unavailable"})
		return
	}
	sess, err := session.New(request.Task, runtime.Workspace, session.ConfigDigest(runtime.Config))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	sess.GitState = session.GitState(r.Context(), runtime.Workspace)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(e event.Event) {
		payload, _ := json.Marshal(e)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, payload)
		flusher.Flush()
	}
	runtime.Runner.Sink = send
	if err := runtime.Runner.Run(r.Context(), sess); err != nil && !errors.Is(err, context.Canceled) {
		send(event.Event{Type: event.TaskFailed, Time: time.Now().UTC(), SessionID: sess.ID, Error: err.Error()})
	}
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	var request struct {
		Title   string `json:"title"`
		Company string `json:"company"`
		Access  string `json:"access"`
		Model   string `json:"model"`
		Agent   string `json:"agent"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request: " + err.Error()})
		return
	}
	selection := config.RuntimeSelection{Company: request.Company, Access: request.Access, Model: request.Model, Agent: request.Agent}
	liveModels, err := s.validateSelection(r.Context(), selection)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	apiKey, err := s.resolveCredential(r.Context(), selection)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	runtime, err := s.build(r.Context(), s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	digest := session.ConfigDigest(runtime.Config)
	runtime.Close()
	title := strings.TrimSpace(request.Title)
	if title == "" {
		title = "New conversation"
	}
	sess, err := session.NewConversation(title, runtime.Workspace, digest, session.Selection{Company: selection.Company, Access: selection.Access, Model: selection.Model, Agent: selection.Agent})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	sess.GitState = session.GitState(r.Context(), runtime.Workspace)
	if err := s.store.Save(r.Context(), sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.store.ListSummaries(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": items})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.Load(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	messages, err := s.store.Messages(sess.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess, "messages": messages})
}

func (s *Server) startSessionRun(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	var request struct {
		Message string `json:"message"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request: " + err.Error()})
		return
	}
	request.Message = strings.TrimSpace(request.Message)
	if request.Message == "" || len(request.Message) > 16<<10 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "message must contain 1 to 16384 bytes"})
		return
	}
	sess, err := s.store.Load(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	runID, err := s.launchSessionRun(sess, request.Message)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errRunActive) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"session_id": sess.ID, "run_id": runID})
}

func (s *Server) resumeSessionRun(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	sess, err := s.store.Recover(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	run := sess.ActiveRun()
	if run == nil || run.ID != r.PathValue("run") || run.Status != session.RunInterrupted {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "run is not interrupted or resumable"})
		return
	}
	runID, err := s.launchSessionRun(sess, "")
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errRunActive) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"session_id": sess.ID, "run_id": runID})
}

var errRunActive = errors.New("another run is already active in this workspace")

func (s *Server) launchSessionRun(sess *session.Session, message string) (string, error) {
	selection := config.RuntimeSelection{Company: sess.Selection.Company, Access: sess.Selection.Access, Model: sess.Selection.Model, Agent: sess.Selection.Agent}
	liveModels, err := s.validateSelection(context.Background(), selection)
	if err != nil {
		return "", err
	}
	apiKey, err := s.resolveCredential(context.Background(), selection)
	if err != nil {
		return "", err
	}
	s.runMu.Lock()
	if !s.active.CompareAndSwap(false, true) {
		s.runMu.Unlock()
		return "", errRunActive
	}
	if message != "" {
		run, runErr := sess.NewRun(message)
		if runErr != nil {
			s.active.Store(false)
			s.runMu.Unlock()
			return "", runErr
		}
		if sess.Title == "New conversation" {
			sess.Title = compactTitle(message)
		}
		if runErr = s.store.AppendMessage(sess.ID, session.MessageRecord{RunID: run.ID, Role: "user", Content: message}); runErr != nil {
			s.active.Store(false)
			s.runMu.Unlock()
			return "", runErr
		}
	}
	run := sess.ActiveRun()
	if run == nil {
		s.active.Store(false)
		s.runMu.Unlock()
		return "", errors.New("session has no active run")
	}
	if err = s.store.Save(context.Background(), sess); err != nil {
		s.active.Store(false)
		s.runMu.Unlock()
		return "", err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	s.activeSession, s.activeRun, s.cancelRun = sess.ID, run.ID, cancel
	runID := run.ID
	s.runMu.Unlock()
	go func() {
		defer func() {
			s.runMu.Lock()
			s.activeSession, s.activeRun, s.cancelRun = "", "", nil
			s.active.Store(false)
			s.runMu.Unlock()
		}()
		runtime, buildErr := s.build(runCtx, s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
		if buildErr != nil {
			s.failLaunchedRun(sess, runID, buildErr)
			return
		}
		defer runtime.Close()
		runtime.Runner.Sink = s.publish
		if runErr := runtime.Runner.Run(runCtx, sess); runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
			// Runner persists and emits its own terminal error.
			return
		}
	}()
	return runID, nil
}

func compactTitle(message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	if len(message) > 80 {
		return message[:80] + "…"
	}
	return message
}

func (s *Server) validateSelection(ctx context.Context, selection config.RuntimeSelection) ([]config.ModelOption, error) {
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

func (s *Server) failLaunchedRun(sess *session.Session, runID string, cause error) {
	if run := sess.ActiveRun(); run != nil && run.ID == runID {
		now := time.Now().UTC()
		run.Status = session.RunFailed
		run.Error = cause.Error()
		run.CompletedAt = &now
		run.UpdatedAt = now
		sess.ActiveRunID = ""
		sess.LastError = cause.Error()
		e := event.New(event.TaskFailed, sess.ID)
		e.RunID = runID
		e.Error = cause.Error()
		e = s.store.BufferEvent(sess.ID, e)
		_ = s.store.Save(context.Background(), sess)
		s.publish(e)
	}
}

func (s *Server) cancelSessionRun(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	s.runMu.Lock()
	if s.activeSession != r.PathValue("id") || s.activeRun != r.PathValue("run") || s.cancelRun == nil {
		s.runMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"error": "run is not active"})
		return
	}
	cancel := s.cancelRun
	s.runMu.Unlock()
	cancel()
	writeJSON(w, http.StatusAccepted, map[string]any{"cancelled": true})
}

func (s *Server) sessionEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Load(r.Context(), r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming is unavailable"})
		return
	}
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	ch := s.subscribe(r.PathValue("id"))
	defer s.unsubscribe(r.PathValue("id"), ch)
	history, err := s.store.Events(r.PathValue("id"), after)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	last := after
	for _, e := range history {
		sendSSE(w, flusher, e)
		if e.Sequence > last {
			last = e.Sequence
		}
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			if e.Sequence != 0 && e.Sequence <= last {
				continue
			}
			sendSSE(w, flusher, e)
			if e.Sequence > last {
				last = e.Sequence
			}
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, e event.Event) {
	payload, _ := json.Marshal(e)
	if e.Sequence > 0 {
		_, _ = fmt.Fprintf(w, "id: %d\n", e.Sequence)
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, payload)
	flusher.Flush()
}

func (s *Server) subscribe(sessionID string) chan event.Event {
	ch := make(chan event.Event, 256)
	s.subscribersMu.Lock()
	if s.subscribers[sessionID] == nil {
		s.subscribers[sessionID] = map[chan event.Event]struct{}{}
	}
	s.subscribers[sessionID][ch] = struct{}{}
	s.subscribersMu.Unlock()
	return ch
}

func (s *Server) unsubscribe(sessionID string, ch chan event.Event) {
	s.subscribersMu.Lock()
	delete(s.subscribers[sessionID], ch)
	if len(s.subscribers[sessionID]) == 0 {
		delete(s.subscribers, sessionID)
	}
	s.subscribersMu.Unlock()
}

func (s *Server) publish(e event.Event) {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()
	for ch := range s.subscribers[e.SessionID] {
		select {
		case ch <- e:
		default:
		}
	}
}

func (s *Server) resolveCredential(ctx context.Context, selection config.RuntimeSelection) (string, error) {
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

func (s *Server) saveAPIKey(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	provider := r.PathValue("provider")
	access, ok := accessByID(provider)
	if !ok || access.AuthType != "api_key" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "该接入方式不接受 API Key"})
		return
	}
	var request struct {
		APIKey string `json:"api_key"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.APIKey) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请输入有效的 API Key"})
		return
	}
	if err := s.credentials.SetAPIKey(provider, request.APIKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "provider": provider})
}

func (s *Server) deleteCredentials(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	provider := r.PathValue("provider")
	if _, ok := accessByID(provider); !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "未知的接入方式"})
		return
	}
	if err := s.credentials.Delete(provider); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": false, "provider": provider})
}

func (s *Server) startCodexLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	session, err := s.logins.Start(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, session)
}

func (s *Server) loginStatus(w http.ResponseWriter, r *http.Request) {
	session, ok := s.logins.Status(r.PathValue("session"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "登录会话不存在"})
		return
	}
	if session.Status == "approved" {
		s.codexModelsMu.Lock()
		s.codexModels = nil
		s.codexModelsAt = time.Time{}
		s.codexModelsMu.Unlock()
	}
	writeJSON(w, http.StatusOK, session)
}

func accessByID(id string) (config.AccessPreset, bool) {
	for _, company := range config.CompanyPresets() {
		for _, access := range company.Access {
			if access.ID == id {
				return access, true
			}
		}
	}
	return config.AccessPreset{}, false
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host) && (u.Scheme == "http" || u.Scheme == "https")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func ListenAndServe(ctx context.Context, address string, server *Server) error {
	httpServer := &http.Server{Addr: address, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
