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
	Workspace  string
	ConfigPath string
	active     atomic.Bool
	static     http.Handler
	build      func(context.Context, string, string, config.RuntimeSelection) (*app.Runtime, error)
}

func New(workspace, configPath string) (*Server, error) {
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		return nil, err
	}
	return &Server{
		Workspace: workspace, ConfigPath: configPath,
		static: http.FileServer(http.FS(root)),
		build: func(ctx context.Context, workspace, configPath string, selection config.RuntimeSelection) (*app.Runtime, error) {
			return app.BuildRuntimeWithOptions(ctx, workspace, configPath, app.RuntimeOptions{Selection: &selection}, nil)
		},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("POST /api/run", s.run)
	mux.Handle("GET /", s.static)
	return securityHeaders(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": app.Version, "active": s.active.Load()})
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	conf, err := app.LoadConfig(s.Workspace, s.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	credentials := make(map[string]bool)
	authStatus := make(map[string]map[string]any)
	for _, company := range config.CompanyPresets() {
		for _, access := range company.Access {
			if access.APIKeyEnv != "" {
				configured := os.Getenv(access.APIKeyEnv) != ""
				credentials[access.APIKeyEnv] = configured
				authStatus[access.ID] = map[string]any{"configured": configured, "detail": access.APIKeyEnv}
			}
		}
	}
	codexConfigured, codexDetail := modelauth.CodexStatus()
	authStatus["openai-codex"] = map[string]any{"configured": codexConfigured, "detail": codexDetail}
	_, keyErr := conf.APIKey()
	currentConfigured := keyErr == nil
	if conf.Model.Provider == "openai-codex" {
		currentConfigured = codexConfigured
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   app.Version,
		"workspace": s.Workspace,
		"model": map[string]any{
			"provider": conf.Model.Provider, "protocol": conf.Model.Protocol(), "base_url": conf.Model.BaseURL,
			"model": conf.Model.Name, "api_key_env": conf.Model.APIKeyEnv, "api_key_configured": currentConfigured,
		},
		"selection": conf.CurrentSelection(), "companies": config.CompanyPresets(), "agents": config.AgentPresets(),
		"credentials": credentials, "auth_status": authStatus, "active": s.active.Load(),
	})
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
	runtime, err := s.build(r.Context(), s.Workspace, s.ConfigPath, selection)
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
