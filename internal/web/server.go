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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Rj455555/GoHermit/internal/app"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/controlplane"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/owner"
	"github.com/Rj455555/GoHermit/internal/teamtemplate"
)

//go:embed assets/*
var assets embed.FS

// Server is the thin HTTP transport over the control-plane service. It owns
// routing, request parsing, response writing, static assets, the
// same-origin guard, and the SSE subscriber fan-out that implements the
// service's Publisher port; every state transition lives in the service.
type Server struct {
	Workspace     string
	ConfigPath    string
	svc           *controlplane.Service
	subscribersMu sync.Mutex
	subscribers   map[string]map[chan event.Event]struct{}
	static        http.Handler
}

func New(workspace, configPath string) (*Server, error) {
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		return nil, err
	}
	server := &Server{
		Workspace: workspace, ConfigPath: configPath,
		static:      http.FileServer(http.FS(root)),
		subscribers: map[string]map[chan event.Event]struct{}{},
	}
	svc, err := controlplane.New(workspace, configPath, server.publish)
	if err != nil {
		return nil, err
	}
	server.svc = svc
	return server, nil
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
	mux.HandleFunc("GET /api/sessions/{id}/approvals", s.listApprovals)
	mux.HandleFunc("POST /api/sessions/{id}/approvals/{requestID}/decide", s.decideApproval)
	mux.HandleFunc("PUT /api/settings/providers/{provider}/api-key", s.saveAPIKey)
	mux.HandleFunc("DELETE /api/settings/providers/{provider}/credentials", s.deleteCredentials)
	mux.HandleFunc("POST /api/settings/providers/openai-codex/login", s.startCodexLogin)
	mux.HandleFunc("GET /api/settings/logins/{session}", s.loginStatus)
	mux.Handle("GET /", s.static)
	return securityHeaders(mux)
}

// statusForKind maps the service error classification to HTTP statuses.
func statusForKind(kind controlplane.Kind) int {
	switch kind {
	case controlplane.KindInvalid:
		return http.StatusBadRequest
	case controlplane.KindNotFound:
		return http.StatusNotFound
	case controlplane.KindConflict:
		return http.StatusConflict
	case controlplane.KindBadGateway:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

// writeServiceError serializes a service failure exactly like the
// pre-refactor handlers did, echoing the affected approval request when the
// error carries one.
func writeServiceError(w http.ResponseWriter, err error) {
	var serviceErr *controlplane.Error
	if errors.As(err, &serviceErr) {
		body := map[string]any{"error": serviceErr.Error()}
		if serviceErr.Request != nil {
			body["request"] = serviceErr.Request
		}
		writeJSON(w, statusForKind(serviceErr.Kind), body)
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": app.Version, "active": s.svc.Active()})
}

func (s *Server) getOwner(w http.ResponseWriter, _ *http.Request) {
	profile, err := s.svc.OwnerProfile()
	if err != nil {
		writeServiceError(w, err)
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
	profile, err := s.svc.SaveOwnerProfile(profile)
	if err != nil {
		writeServiceError(w, err)
		return
	}
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
	profile, err := s.svc.UpsertOwnerFact(owner.Fact{ID: r.PathValue("id"), Category: request.Category, Value: request.Value, Source: request.Source, Confirmed: request.Confirmed})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) deleteOwnerFact(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	profile, err := s.svc.ForgetOwnerFact(r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

// exportTeamTemplate downloads the stored team template with credential
// redaction applied, so the exported file carries zero secret content even
// if the in-memory template was tampered with.
func (s *Server) exportTeamTemplate(w http.ResponseWriter, _ *http.Request) {
	template, err := s.svc.TeamTemplate()
	if err != nil {
		writeServiceError(w, err)
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
	if err := s.svc.SaveTeamTemplate(template); err != nil {
		writeServiceError(w, err)
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
			configured, source, detail := s.svc.AccessStatus(r.Context(), access)
			if configured && access.ID == "openai-codex" {
				models, modelErr := s.svc.CodexCatalog(r.Context())
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
		currentConfigured, _, _ = s.svc.AccessStatus(r.Context(), config.AccessPreset{ID: "openai-codex", AuthType: "oauth_external"})
	}
	selection := normalizeSelection(conf.CurrentSelection(), availableCompanies)
	ownerProfile, ownerErr := s.svc.OwnerProfile()
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
		"auth_status": authStatus, "active": s.svc.Active(), "owner": ownerStatus,
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

// run streams the legacy single-shot task over SSE. The service owns the
// whole lifecycle; this handler only parses the request, upgrades to SSE on
// the first event, and maps pre-stream failures to HTTP statuses.
func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	if !s.svc.TryAcquireRun() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "another task is already running"})
		return
	}
	defer s.svc.ReleaseRun()
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
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming is unavailable"})
		return
	}
	streamed := false
	send := func(e event.Event) {
		if !streamed {
			streamed = true
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache, no-store")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
		}
		payload, _ := json.Marshal(e)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, payload)
		flusher.Flush()
	}
	err := s.svc.RunOnce(r.Context(), controlplane.RunOnceInput{Task: request.Task, Company: request.Company, Access: request.Access, Model: request.Model, Agent: request.Agent}, send)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if !streamed {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
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
	sess, err := s.svc.CreateSession(r.Context(), controlplane.CreateSessionInput{Title: request.Title, Company: request.Company, Access: request.Access, Model: request.Model, Agent: request.Agent, PlanMode: request.PlanMode})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.svc.ListSessions(r.Context(), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": items})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	sess, messages, err := s.svc.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
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
	runID, err := s.svc.StartRun(r.Context(), r.PathValue("id"), request.Message)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"session_id": r.PathValue("id"), "run_id": runID})
}

func (s *Server) resumeSessionRun(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	runID, err := s.svc.ResumeRun(r.Context(), r.PathValue("id"), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"session_id": r.PathValue("id"), "run_id": runID})
}

func (s *Server) approveSessionRun(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	runID, err := s.svc.ApprovePlan(r.Context(), r.PathValue("id"), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"session_id": r.PathValue("id"), "run_id": runID})
}

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	filter := strings.TrimSpace(r.URL.Query().Get("status"))
	if filter == "" {
		filter = "pending"
	}
	items, err := s.svc.ListApprovals(r.Context(), r.PathValue("id"), filter)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": items})
}

// decideApproval parses the owner decision and relays it to the service,
// which owns both the active-run broker rendezvous and the C2
// load-decide-save path (ADR 0011).
func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	var request struct {
		Decision string `json:"decision"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid decision: " + err.Error()})
		return
	}
	var approve bool
	switch strings.TrimSpace(request.Decision) {
	case "approve":
		approve = true
	case "deny":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "decision must be approve or deny"})
		return
	}
	target, decided, err := s.svc.DecideApproval(r.Context(), r.PathValue("id"), r.PathValue("requestID"), approve)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": target, "event": decided})
}

func (s *Server) cancelSessionRun(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	activeCancelled, err := s.svc.CancelRun(r.Context(), r.PathValue("id"), r.PathValue("run"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if activeCancelled {
		writeJSON(w, http.StatusAccepted, map[string]any{"cancelled": true})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "cancelling"})
}

func (s *Server) sessionEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.svc.LoadSession(r.Context(), r.PathValue("id")); err != nil {
		writeServiceError(w, err)
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
	history, err := s.svc.SessionEvents(r.Context(), r.PathValue("id"), after)
	if err != nil {
		writeServiceError(w, err)
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

// publish is the service's Publisher port: it fans a committed event out to
// the session's SSE subscribers without ever blocking the service.
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
	if err := s.svc.SaveAPIKey(provider, request.APIKey); err != nil {
		writeServiceError(w, err)
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
	if err := s.svc.DeleteCredentials(provider); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": false, "provider": provider})
}

func (s *Server) startCodexLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
		return
	}
	login, err := s.svc.StartLogin(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, login)
}

func (s *Server) loginStatus(w http.ResponseWriter, r *http.Request) {
	login, ok := s.svc.LoginStatus(r.PathValue("session"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "登录会话不存在"})
		return
	}
	writeJSON(w, http.StatusOK, login)
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
