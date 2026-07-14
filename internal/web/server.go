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
	"strings"
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
	Workspace   string
	ConfigPath  string
	active      atomic.Bool
	static      http.Handler
	credentials *modelauth.Store
	logins      *modelauth.LoginManager
	build       func(context.Context, string, string, config.RuntimeSelection, string) (*app.Runtime, error)
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
	return &Server{
		Workspace: workspace, ConfigPath: configPath,
		static:      http.FileServer(http.FS(root)),
		credentials: credentials,
		logins:      modelauth.NewLoginManager(credentials),
		build: func(ctx context.Context, workspace, configPath string, selection config.RuntimeSelection, apiKey string) (*app.Runtime, error) {
			return app.BuildRuntimeWithOptions(ctx, workspace, configPath, app.RuntimeOptions{Selection: &selection, APIKey: apiKey}, nil)
		},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("POST /api/run", s.run)
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
	for _, company := range companies {
		available := company
		available.Access = nil
		for _, access := range company.Access {
			configured, source, detail := s.accessStatus(r.Context(), access)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   app.Version,
		"workspace": s.Workspace,
		"model": map[string]any{
			"provider": conf.Model.Provider, "protocol": conf.Model.Protocol(), "base_url": conf.Model.BaseURL,
			"model": conf.Model.Name, "api_key_env": conf.Model.APIKeyEnv, "api_key_configured": currentConfigured,
		},
		"selection": conf.CurrentSelection(), "companies": companies, "available_companies": availableCompanies, "agents": config.AgentPresets(),
		"auth_status": authStatus, "active": s.active.Load(),
	})
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
	if _, _, err := config.ResolveSelection(selection); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	apiKey, err := s.resolveCredential(r.Context(), selection)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	runtime, err := s.build(r.Context(), s.Workspace, s.ConfigPath, selection, apiKey)
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
