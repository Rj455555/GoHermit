package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Rj455555/GoHermit/internal/loop"
)

const (
	maxLoopBodyBytes = 256 << 10
	maxHistoryLimit  = 100
)

func (s *Server) listLoops(w http.ResponseWriter, _ *http.Request) {
	definitions, err := s.svc.ListLoops()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if definitions == nil {
		definitions = []loop.Definition{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"loops": definitions})
}

func (s *Server) createLoop(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var definition loop.Definition
	if err := decodeStrictJSON(w, r, maxLoopBodyBytes, &definition); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid loop definition", "details": []string{err.Error()}})
		return
	}
	saved, err := s.svc.CreateLoop(definition)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Location", "/api/loops/"+saved.ID)
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) importLoop(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLoopBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid loop import", "details": []string{err.Error()}})
		return
	}
	saved, err := s.svc.ImportLoop(body)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Location", "/api/loops/"+saved.ID)
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) getLoop(w http.ResponseWriter, r *http.Request) {
	definition, err := s.svc.GetLoop(r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definition)
}

func (s *Server) updateLoop(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var definition loop.Definition
	if err := decodeStrictJSON(w, r, maxLoopBodyBytes, &definition); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid loop definition", "details": []string{err.Error()}})
		return
	}
	saved, err := s.svc.UpdateLoop(r.PathValue("id"), definition)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) dryRunLoop(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	report, err := s.svc.DryRunLoop(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) listLoopInvocations(w http.ResponseWriter, r *http.Request) {
	if _, err := s.svc.GetLoop(r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	limit, err := historyLimit(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	invocations, err := s.svc.ListInvocations(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	// The store returns a stable oldest-first order. The Workbench needs a
	// bounded recent-history view, so reverse only the selected tail.
	start := len(invocations) - limit
	if start < 0 {
		start = 0
	}
	recent := make([]loop.Invocation, 0, len(invocations)-start)
	for i := len(invocations) - 1; i >= start; i-- {
		recent = append(recent, invocations[i])
	}
	if recent == nil {
		recent = []loop.Invocation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"invocations": recent, "limit": limit})
}

func (s *Server) startLoopInvocation(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	invocation, err := s.svc.StartLoopInvocation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, invocation)
}

func (s *Server) getLoopInvocation(w http.ResponseWriter, r *http.Request) {
	invocation, err := s.svc.GetInvocation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, invocation)
}

func (s *Server) cancelLoopInvocation(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	invocation, err := s.svc.CancelLoopInvocation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, invocation)
}

func requireSameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if sameOrigin(r) {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin requests are not allowed"})
	return false
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, limit int64, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func historyLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 50, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxHistoryLimit {
		return 0, errors.New("limit must be an integer between 1 and 100")
	}
	return limit, nil
}
