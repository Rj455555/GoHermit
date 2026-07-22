package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	modelauth "github.com/Rj455555/GoHermit/internal/auth"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/owner"
	"github.com/Rj455555/GoHermit/internal/runcontrol"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/taskplan"
	"github.com/Rj455555/GoHermit/internal/team"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
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
	owner         *owner.Store
	logins        *modelauth.LoginManager
	build         func(context.Context, string, string, config.RuntimeSelection, string, []config.ModelOption) (*app.Runtime, error)
	codexModelsMu sync.Mutex
	codexModels   []config.ModelOption
	codexModelsAt time.Time
	teamWorker    team.Worker
	teamTemplates *teamtemplate.Store
	// teamTemplatesErr defers store-resolution failure to request time so a
	// team session fails closed instead of the server failing to start.
	teamTemplatesErr error
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
	return &Server{
		Workspace: workspace, ConfigPath: configPath,
		static:        http.FileServer(http.FS(root)),
		store:         store,
		credentials:   credentials,
		owner:         ownerStore,
		logins:        modelauth.NewLoginManager(credentials),
		subscribers:   map[string]map[chan event.Event]struct{}{},
		teamTemplates: teamTemplates, teamTemplatesErr: teamTemplatesErr,
		build: func(ctx context.Context, workspace, configPath string, selection config.RuntimeSelection, apiKey string, models []config.ModelOption) (*app.Runtime, error) {
			return app.BuildRuntimeWithOptions(ctx, workspace, configPath, app.RuntimeOptions{Selection: &selection, APIKey: apiKey, Models: models}, nil)
		},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/owner", s.getOwner)
	mux.HandleFunc("PUT /api/owner", s.saveOwner)
	mux.HandleFunc("PUT /api/owner/facts/{id}", s.saveOwnerFact)
	mux.HandleFunc("DELETE /api/owner/facts/{id}", s.deleteOwnerFact)
	mux.HandleFunc("GET /api/team-template/export", s.exportTeamTemplate)
	mux.HandleFunc("POST /api/team-template/import", s.importTeamTemplate)
	mux.HandleFunc("POST /api/run", s.run)
	mux.HandleFunc("POST /api/sessions", s.createSession)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.getSession)
	mux.HandleFunc("POST /api/sessions/{id}/runs", s.startSessionRun)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.sessionEvents)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/cancel", s.cancelSessionRun)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/resume", s.resumeSessionRun)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/approve", s.approveSessionRun)
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

func (s *Server) getOwner(w http.ResponseWriter, _ *http.Request) {
	profile, err := s.owner.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) saveOwner(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	var profile owner.Profile
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid owner profile: " + err.Error()})
		return
	}
	if err := s.owner.Save(profile); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	profile, _ = s.owner.Load()
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) saveOwnerFact(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	var request struct {
		Category  string `json:"category"`
		Value     string `json:"value"`
		Source    string `json:"source"`
		Confirmed bool   `json:"confirmed"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid owner fact: " + err.Error()})
		return
	}
	profile, err := s.owner.UpsertFact(owner.Fact{ID: r.PathValue("id"), Category: request.Category, Value: request.Value, Source: request.Source, Confirmed: request.Confirmed})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) deleteOwnerFact(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	profile, err := s.owner.ForgetFact(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

// exportTeamTemplate downloads the stored team template with credential
// redaction applied, so the exported file carries zero secret content even
// if the in-memory template was tampered with.
func (s *Server) exportTeamTemplate(w http.ResponseWriter, _ *http.Request) {
	if s.teamTemplatesErr != nil || s.teamTemplates == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "team template store unavailable"})
		return
	}
	template, err := s.teamTemplates.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	raw, err := teamtemplate.Export(template)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="team-template.json"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// importTeamTemplate replaces the stored team template with an uploaded
// export. teamtemplate.Import screens for credential markers and validates
// before anything is saved, so a poisoned file never overwrites the store.
func (s *Server) importTeamTemplate(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	if s.teamTemplatesErr != nil || s.teamTemplates == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "team template store unavailable"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid team template: " + err.Error()})
		return
	}
	template, err := teamtemplate.Import(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := s.teamTemplates.Save(template); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	roles := make([]string, 0, len(template.Roles))
	for role := range template.Roles {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	writeJSON(w, http.StatusOK, map[string]any{"name": template.Name, "roles": roles})
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
	ownerProfile, ownerErr := s.owner.Load()
	ownerStatus := map[string]any{"configured": false}
	if ownerErr == nil {
		ownerStatus = map[string]any{"configured": owner.Markdown(ownerProfile) != "", "display_name": ownerProfile.Identity.DisplayName}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   app.Version,
		"workspace": s.Workspace,
		"model": map[string]any{
			"provider": conf.Model.Provider, "protocol": conf.Model.Protocol(), "base_url": conf.Model.BaseURL,
			"model": conf.Model.Name, "api_key_env": conf.Model.APIKeyEnv, "api_key_configured": currentConfigured,
		},
		"selection": selection, "companies": companies, "available_companies": availableCompanies, "agents": config.AgentPresets(),
		"auth_status": authStatus, "active": s.active.Load(), "owner": ownerStatus,
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
	if selection.Agent == "team" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the team profile requires the Session/Run API"})
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
	s.applyOwner(runtime)
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
		Title    string `json:"title"`
		Company  string `json:"company"`
		Access   string `json:"access"`
		Model    string `json:"model"`
		Agent    string `json:"agent"`
		PlanMode string `json:"plan_mode"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request: " + err.Error()})
		return
	}
	selection := config.RuntimeSelection{Company: request.Company, Access: request.Access, Model: request.Model, Agent: request.Agent}
	planMode, err := session.NormalizePlanMode(request.PlanMode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
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
	if selection.Agent == "team" {
		if err := s.validateTeamSelections(r.Context(), selection); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
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
	sess.PlanMode = planMode
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

func (s *Server) approveSessionRun(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	sess, err := s.store.Load(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	run := sess.ActiveRun()
	if run == nil || run.ID != r.PathValue("run") || run.Status != session.RunQueued || run.PlanMode != session.PlanReview || run.Plan == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "run has no review plan awaiting approval"})
		return
	}
	if !run.PlanApproved {
		now := time.Now().UTC()
		run.PlanApproved, run.PlanApprovedAt, run.UpdatedAt = true, &now, now
		approved := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, "", "计划已批准，准备执行")
		if _, err = s.commitAndPublish(sess, approved); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
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
	if selection.Agent == "team" && (sess.Mission == nil || sess.Mission.RunID != run.ID) {
		mission, missionErr := team.AdaptiveMission("mission-"+run.ID, run.ID, run.Message, team.DefaultBudget())
		if missionErr != nil {
			s.active.Store(false)
			s.runMu.Unlock()
			return "", missionErr
		}
		sess.Mission = mission
	}
	var createdPlanEvent event.Event
	if run.Plan == nil {
		if selection.Agent == "team" {
			run.Plan, err = planForMission(run.ID, sess.Mission)
		} else {
			run.Plan, err = taskplan.ForGoal(run.ID, run.Message, selection.Agent)
		}
		if err != nil {
			s.active.Store(false)
			s.runMu.Unlock()
			return "", err
		}
		message := "执行计划已创建"
		if run.PlanMode == session.PlanReview && !run.PlanApproved {
			message = "执行计划已创建，等待确认"
		}
		createdPlanEvent = s.planRuntimeEvent(sess.ID, run, event.PlanCreated, "", message)
	}
	if createdPlanEvent.Type != "" {
		createdPlanEvent, err = s.store.CommitEvent(context.Background(), sess, createdPlanEvent)
	} else {
		err = s.store.Save(context.Background(), sess)
	}
	if err != nil {
		s.active.Store(false)
		s.runMu.Unlock()
		return "", err
	}
	if run.PlanMode == session.PlanReview && !run.PlanApproved {
		runID := run.ID
		s.active.Store(false)
		s.runMu.Unlock()
		if createdPlanEvent.Type != "" {
			s.publish(createdPlanEvent)
		}
		return runID, nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	s.activeSession, s.activeRun, s.cancelRun = sess.ID, run.ID, cancel
	runID := run.ID
	s.runMu.Unlock()
	if createdPlanEvent.Type != "" {
		s.publish(createdPlanEvent)
	}
	go func() {
		defer func() {
			s.runMu.Lock()
			s.activeSession, s.activeRun, s.cancelRun = "", "", nil
			s.active.Store(false)
			s.runMu.Unlock()
		}()
		if selection.Agent == "team" {
			s.runTeam(runCtx, sess, runID, selection, apiKey, liveModels)
			return
		}
		runtime, buildErr := s.build(runCtx, s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
		if buildErr != nil {
			s.failLaunchedRun(sess, runID, buildErr)
			return
		}
		s.applyOwner(runtime)
		defer runtime.Close()
		runtime.Runner.Sink = s.publish
		if runErr := runtime.Runner.Run(runCtx, sess); runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
			// Runner persists and emits its own terminal error.
			return
		}
	}()
	return runID, nil
}

func planForMission(runID string, mission *team.Mission) (*taskplan.Plan, error) {
	if mission == nil || len(mission.WorkItems) == 0 {
		return nil, errors.New("team mission is required before creating its plan")
	}
	specs := make([]taskplan.StepSpec, 0, len(mission.WorkItems))
	for _, item := range mission.WorkItems {
		specs = append(specs, taskplan.StepSpec{ID: item.ID, Title: item.Title})
	}
	return taskplan.NewParallel("plan-"+runID, specs)
}

func (s *Server) runTeam(ctx context.Context, sess *session.Session, runID string, selection config.RuntimeSelection, apiKey string, liveModels []config.ModelOption) {
	run := sess.ActiveRun()
	if run == nil || run.ID != runID || sess.Mission == nil {
		s.failLaunchedRun(sess, runID, errors.New("team mission is missing"))
		return
	}
	now := time.Now().UTC()
	run.Status, run.UpdatedAt = session.RunRunning, now
	started := event.New(event.TaskStarted, sess.ID)
	started.RunID, started.MissionID, started.AgentID = runID, sess.Mission.ID, string(team.RoleLead)
	if _, err := s.commitAndPublish(sess, started); err != nil {
		s.failLaunchedRun(sess, runID, err)
		return
	}
	profile, _ := s.owner.Load()
	var worker team.Worker = &app.TeamWorker{
		Workspace: s.Workspace, ConfigPath: s.ConfigPath, Selection: selection, APIKey: apiKey, Models: liveModels,
		OwnerContext: owner.Markdown(profile), ParentSessionID: sess.ID, ParentRunID: runID, ParentStore: s.store, Sink: s.publish,
	}
	if s.teamWorker != nil {
		worker = s.teamWorker
	}
	var sinkErr error
	coordinator := &team.Coordinator{
		Worker: worker,
		Sink: func(teamEvent team.TeamEvent) {
			if sinkErr != nil {
				return
			}
			runtimeEvent := event.New(teamEventType(teamEvent.Type), sess.ID)
			runtimeEvent.RunID, runtimeEvent.MissionID, runtimeEvent.WorkItemID = runID, sess.Mission.ID, teamEvent.WorkItemID
			runtimeEvent.AgentID, runtimeEvent.Message = string(teamEvent.Role), teamEvent.Message
			runtimeEvents := []event.Event{runtimeEvent}
			transition, transitionErr := runcontrol.ApplyTeamEvent(run.Plan, teamEvent, sess.Mission)
			if transitionErr != nil {
				sinkErr = transitionErr
				return
			}
			if transition.Changed {
				planEvent := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, transition.StepID, transition.Detail)
				planEvent.MissionID = sess.Mission.ID
				runtimeEvents = append(runtimeEvents, planEvent)
			}
			_, sinkErr = s.commitAndPublishMany(sess, runtimeEvents)
		},
		Checkpoint: func(mission *team.Mission) error {
			if sinkErr != nil {
				return sinkErr
			}
			sess.Mission = mission
			if active := sess.ActiveRun(); active != nil && active.ID == runID {
				active.ModelCalls = mission.Usage.ModelCalls
				active.TotalTokens = mission.Usage.Tokens
				active.UpdatedAt = mission.UpdatedAt
			}
			sess.GitState = session.GitState(ctx, s.Workspace)
			return s.store.Save(context.WithoutCancel(ctx), sess)
		},
	}
	err := coordinator.Run(ctx, sess.Mission)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.finishTeamCancelled(sess, run, err)
			return
		}
		s.failLaunchedRun(sess, runID, err)
		return
	}
	if run.Plan == nil || run.Plan.Status != taskplan.Completed {
		s.failLaunchedRun(sess, runID, errors.New("team live plan completion gate failed"))
		return
	}
	final := finalTeamHandoff(sess.Mission)
	now = time.Now().UTC()
	run.Status, run.FinalMessage, run.CompletedAt, run.UpdatedAt = session.RunCompleted, final.Summary, &now, now
	run.ModelCalls, run.TotalTokens = sess.Mission.Usage.ModelCalls, sess.Mission.Usage.Tokens
	run.ModifiedFiles = missionModifiedFiles(sess.Mission)
	for _, handoff := range sess.Mission.Handoffs {
		for _, check := range handoff.Checks {
			sess.TestResults = append(sess.TestResults, session.TestResult{Command: check.Command, Passed: check.Passed, Summary: check.Summary, Time: handoff.CreatedAt, RunID: runID})
		}
	}
	sess.ActiveRunID, sess.LastError = "", ""
	sess.CompletedSteps = append(sess.CompletedSteps, "Personal Agent Team completed mission "+sess.Mission.ID)
	if final.Summary == "" {
		final.Summary = "团队任务已完成并通过独立验证。"
		run.FinalMessage = final.Summary
	}
	_ = s.store.AppendMessage(sess.ID, session.MessageRecord{RunID: runID, Role: "assistant", Content: final.Summary})
	completed := event.New(event.TaskCompleted, sess.ID)
	completed.RunID, completed.MissionID, completed.AgentID, completed.Message = runID, sess.Mission.ID, string(team.RoleLead), final.Summary
	if _, err := s.commitAndPublish(sess, completed); err != nil {
		sess.LastError = err.Error()
	}
}

func (s *Server) finishTeamCancelled(sess *session.Session, run *session.Run, cause error) {
	now := time.Now().UTC()
	status, eventType := session.RunCancelled, event.TaskCancelled
	var runtimeEvents []event.Event
	if errors.Is(cause, context.DeadlineExceeded) {
		status, eventType = session.RunInterrupted, event.RunInterrupted
		if sess.Mission != nil && sess.Mission.Status != team.Interrupted {
			sess.Mission.Interrupt(cause.Error())
		}
		if run.Plan != nil && run.Plan.Current() != nil {
			if transition, noteErr := runcontrol.Interrupt(run.Plan, "运行已中断，可从当前步骤恢复"); noteErr == nil && transition.Changed {
				planEvent := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, transition.StepID, "运行已中断，可恢复")
				planEvent.MissionID = sess.Mission.ID
				runtimeEvents = append(runtimeEvents, planEvent)
			}
		}
	} else if sess.Mission != nil {
		sess.Mission.Cancel(cause.Error())
		if run.Plan != nil {
			if transition, cancelErr := runcontrol.Cancel(run.Plan, "任务已由用户停止"); cancelErr == nil && transition.Changed {
				planEvent := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, transition.StepID, "任务已停止")
				planEvent.MissionID = sess.Mission.ID
				runtimeEvents = append(runtimeEvents, planEvent)
			}
		}
	}
	run.Status, run.Error, run.UpdatedAt = status, cause.Error(), now
	if sess.Mission != nil {
		run.ModelCalls, run.TotalTokens = sess.Mission.Usage.ModelCalls, sess.Mission.Usage.Tokens
	}
	if status == session.RunInterrupted {
		run.CompletedAt = nil
	} else {
		run.CompletedAt = &now
		sess.ActiveRunID = ""
	}
	runtimeEvent := event.New(eventType, sess.ID)
	runtimeEvent.RunID, runtimeEvent.MissionID, runtimeEvent.Error = run.ID, sess.Mission.ID, cause.Error()
	runtimeEvents = append(runtimeEvents, runtimeEvent)
	if _, err := s.commitAndPublishMany(sess, runtimeEvents); err != nil {
		sess.LastError = err.Error()
	}
}

func teamEventType(value team.TeamEventType) event.Type {
	switch value {
	case team.MissionStarted:
		return event.MissionStarted
	case team.MissionFinished:
		return event.MissionCompleted
	case team.MissionFailed:
		return event.MissionFailed
	case team.WorkItemStarted:
		return event.WorkItemStarted
	case team.WorkItemDone:
		return event.WorkItemCompleted
	case team.WorkItemFailed:
		return event.WorkItemFailed
	default:
		return event.SessionUpdated
	}
}

func (s *Server) planRuntimeEvent(sessionID string, run *session.Run, eventType event.Type, stepID, message string) event.Event {
	runtimeEvent := event.New(eventType, sessionID)
	runtimeEvent.RunID, runtimeEvent.PlanStepID, runtimeEvent.Message = run.ID, stepID, message
	runtimeEvent.Data, _ = json.Marshal(map[string]any{"plan": run.Plan})
	return runtimeEvent
}

func finalTeamHandoff(mission *team.Mission) team.Handoff {
	if mission == nil {
		return team.Handoff{}
	}
	for i := len(mission.Handoffs) - 1; i >= 0; i-- {
		if mission.Handoffs[i].Role == team.RoleLead {
			return mission.Handoffs[i]
		}
	}
	return team.Handoff{}
}

func missionModifiedFiles(mission *team.Mission) []string {
	seen := map[string]bool{}
	var files []string
	if mission == nil {
		return files
	}
	for _, handoff := range mission.Handoffs {
		for _, file := range handoff.ModifiedFiles {
			if file != "" && !seen[file] {
				seen[file] = true
				files = append(files, file)
			}
		}
	}
	sort.Strings(files)
	return files
}

func (s *Server) applyOwner(runtime *app.Runtime) {
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

// teamValidationRoles fixes a stable order so the first reported validation
// failure is deterministic.
var teamValidationRoles = []string{
	string(team.RoleLead), string(team.RoleExplorer), string(team.RoleBuilder),
	string(team.RoleReviewer), string(team.RoleVerifier),
}

// validateTeamSelections checks every team role's effective provider/model
// selection from the team template before any session state exists. An empty
// template keeps the legacy behavior: every role runs on the session-level
// selection, which createSession already validated.
func (s *Server) validateTeamSelections(ctx context.Context, sessionSelection config.RuntimeSelection) error {
	if s.teamTemplatesErr != nil {
		return fmt.Errorf("team template store unavailable: %w", s.teamTemplatesErr)
	}
	if s.teamTemplates == nil {
		return errors.New("team template store unavailable")
	}
	template, err := s.teamTemplates.Load()
	if err != nil {
		return fmt.Errorf("load team template: %w", err)
	}
	if template.Empty() {
		return nil
	}
	selections := teamtemplate.EffectiveSelections(template)
	checked := map[string]bool{}
	for _, role := range teamValidationRoles {
		roleSelection := selections[role]
		selection := config.RuntimeSelection{Company: roleSelection.Company, Access: roleSelection.Access, Model: roleSelection.Model, Agent: sessionSelection.Agent}
		key := selection.Company + "\x00" + selection.Access + "\x00" + selection.Model
		if checked[key] {
			continue
		}
		checked[key] = true
		if err := s.validateTeamRoleSelection(ctx, selection); err != nil {
			return fmt.Errorf("team role %q: %w", role, err)
		}
	}
	return nil
}

// validateTeamRoleSelection reruns the session-level catalog, credential, and
// provider capability checks for one role's effective selection.
func (s *Server) validateTeamRoleSelection(ctx context.Context, selection config.RuntimeSelection) error {
	liveModels, err := s.validateSelection(ctx, selection)
	if err != nil {
		return err
	}
	preset, _, err := config.ResolveSelectionWithModels(selection, liveModels)
	if err != nil {
		return err
	}
	access, ok := config.AccessProfile(selection.Company, selection.Access)
	if !ok {
		return errors.New("未知的接入方式")
	}
	configured, _, detail := s.accessStatus(ctx, access)
	if !configured {
		return fmt.Errorf("%s: %s", access.Label, detail)
	}
	apiKey, err := s.resolveCredential(ctx, selection)
	if err != nil {
		return err
	}
	runtime, err := s.build(ctx, s.Workspace, s.ConfigPath, selection, apiKey, liveModels)
	if err != nil {
		return err
	}
	defer runtime.Close()
	// Team roles always send tools, so a provider without tool-call support
	// can never serve them.
	if runtime.Runner == nil || runtime.Runner.Provider == nil || !runtime.Runner.Provider.Capabilities().ToolCalls {
		return fmt.Errorf("provider %q does not support the tool calls every team role requires", preset.Provider)
	}
	return nil
}

func (s *Server) failLaunchedRun(sess *session.Session, runID string, cause error) {
	if run := sess.ActiveRun(); run != nil && run.ID == runID {
		var runtimeEvents []event.Event
		if run.Plan != nil && run.Plan.Status == taskplan.Active {
			step := run.Plan.Current()
			if step == nil {
				step = run.Plan.NextPending()
			}
			if step != nil {
				if changed, planErr := run.Plan.Fail(step.ID, cause.Error()); planErr == nil && changed {
					planEvent := s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, step.ID, cause.Error())
					runtimeEvents = append(runtimeEvents, planEvent)
				}
			}
		}
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
		runtimeEvents = append(runtimeEvents, e)
		if _, err := s.commitAndPublishMany(sess, runtimeEvents); err != nil {
			sess.LastError = err.Error()
		}
	}
}

func (s *Server) cancelSessionRun(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	s.runMu.Lock()
	if s.activeSession != r.PathValue("id") || s.activeRun != r.PathValue("run") || s.cancelRun == nil {
		sess, loadErr := s.store.Load(r.Context(), r.PathValue("id"))
		if loadErr == nil {
			run := sess.ActiveRun()
			if run != nil && run.ID == r.PathValue("run") && run.Status == session.RunQueued && run.PlanMode == session.PlanReview && !run.PlanApproved {
				transition, cancelErr := runcontrol.Cancel(run.Plan, "计划未执行，已由用户停止")
				if cancelErr == nil {
					now := time.Now().UTC()
					run.Status, run.CompletedAt, run.UpdatedAt = session.RunCancelled, &now, now
					sess.ActiveRunID = ""
					runtimeEvents := make([]event.Event, 0, 2)
					if transition.Changed {
						runtimeEvents = append(runtimeEvents, s.planRuntimeEvent(sess.ID, run, event.PlanUpdated, transition.StepID, transition.Detail))
					}
					cancelled := event.New(event.TaskCancelled, sess.ID)
					cancelled.RunID, cancelled.Message = run.ID, "计划已停止"
					runtimeEvents = append(runtimeEvents, cancelled)
					_, cancelErr = s.commitAndPublishMany(sess, runtimeEvents)
				}
				s.runMu.Unlock()
				if cancelErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]any{"error": cancelErr.Error()})
					return
				}
				writeJSON(w, http.StatusAccepted, map[string]any{"status": "cancelling"})
				return
			}
		}
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
	if lastEventID, parseErr := strconv.ParseUint(r.Header.Get("Last-Event-ID"), 10, 64); parseErr == nil && lastEventID > after {
		after = lastEventID
	}
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

func (s *Server) commitAndPublish(sess *session.Session, runtimeEvent event.Event) (event.Event, error) {
	committed, err := s.commitAndPublishMany(sess, []event.Event{runtimeEvent})
	if err != nil {
		return event.Event{}, err
	}
	return committed[0], nil
}

func (s *Server) commitAndPublishMany(sess *session.Session, runtimeEvents []event.Event) ([]event.Event, error) {
	committed, err := s.store.CommitEvents(context.Background(), sess, runtimeEvents)
	if err != nil {
		return nil, err
	}
	for _, runtimeEvent := range committed {
		s.publish(runtimeEvent)
	}
	return committed, nil
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
