package web

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Rj455555/GoHermit/internal/channelstore"
	"github.com/Rj455555/GoHermit/internal/controlplane"
)

func (s *Server) listChannels(w http.ResponseWriter, _ *http.Request) {
	accounts, err := s.channels.ListAccounts()
	if err != nil {
		writeChannelError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": []any{map[string]any{"id": "weixin", "label": "微信", "enabled": true, "accounts": publicAccounts(accounts)}}})
}

func (s *Server) listWeixinAccounts(w http.ResponseWriter, _ *http.Request) {
	accounts, err := s.channels.ListAccounts()
	if err != nil {
		writeChannelError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": publicAccounts(accounts)})
}

func (s *Server) startWeixinLogin(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var body map[string]any
	if err := decodeStrictJSON(w, r, 16<<10, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Weixin login request"})
		return
	}
	attempt, err := s.channels.StartLogin(r.Context(), textValue(body["account_id"]), textValue(body["base_url"]), textValue(body["label"]))
	if err != nil {
		writeChannelError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, publicAttempt(attempt))
}

func (s *Server) weixinLoginStatus(w http.ResponseWriter, r *http.Request) {
	attempt, err := s.channels.LoginStatus(r.Context(), r.PathValue("attemptID"))
	if err != nil {
		writeChannelError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicAttempt(attempt))
}

func (s *Server) weixinLoginQR(w http.ResponseWriter, r *http.Request) {
	content, err := s.channels.LoginQR(r.PathValue("attemptID"))
	if err != nil {
		writeChannelError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(content, "data:image/") {
		parts := strings.SplitN(content, ",", 2)
		if len(parts) == 2 {
			mediaType := strings.TrimSuffix(strings.TrimPrefix(parts[0], "data:"), ";base64")
			allowed := mediaType == "image/png" || mediaType == "image/jpeg" || mediaType == "image/webp" || mediaType == "image/gif"
			decoded, decodeErr := base64.StdEncoding.DecodeString(parts[1])
			if allowed && decodeErr == nil && len(decoded) <= channelstore.MaxQRContentBytes {
				w.Header().Set("Content-Type", mediaType)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(decoded)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 320 320\"><rect width=\"320\" height=\"320\" fill=\"white\"/><text x=\"160\" y=\"150\" text-anchor=\"middle\" font-size=\"20\">微信二维码待刷新</text><text x=\"160\" y=\"180\" text-anchor=\"middle\" font-size=\"14\">请点击刷新</text></svg>")
}

func (s *Server) cancelWeixinLogin(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if err := s.channels.CancelLogin(r.PathValue("attemptID")); err != nil {
		writeChannelError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logoutWeixinAccount(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if err := s.channels.Logout(r.Context(), r.PathValue("accountID")); err != nil {
		writeChannelError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listWeixinBindings(w http.ResponseWriter, r *http.Request) {
	bindings, err := s.channels.ListBindings()
	if err != nil {
		writeChannelError(w, err)
		return
	}
	if accountID := r.PathValue("accountID"); accountID != "" {
		filtered := bindings[:0]
		for _, binding := range bindings {
			if binding.AccountID == accountID {
				filtered = append(filtered, binding)
			}
		}
		bindings = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": publicBindings(bindings)})
}

func (s *Server) saveWeixinBinding(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	var body map[string]any
	if err := decodeStrictJSON(w, r, 16<<10, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Weixin binding request"})
		return
	}
	binding := channelstore.Binding{ID: textValue(body["id"]), AccountID: textValue(body["account_id"]), PeerID: textValue(body["peer_id"]), GroupID: textValue(body["group_id"]), EmployeeID: textValue(body["employee_id"]), Enabled: boolValue(body["enabled"], true), MentionRequired: boolValue(body["mention_required"], false)}
	if accountID := r.PathValue("accountID"); accountID != "" {
		if binding.AccountID != "" && binding.AccountID != accountID {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Weixin account binding mismatch"})
			return
		}
		binding.AccountID = accountID
	}
	if binding.ID == "" {
		binding.ID = channelstore.NewID("binding")
	}
	if err := s.channels.SaveBinding(binding); err != nil {
		writeChannelError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": []map[string]any{publicBinding(binding)}})
}

func (s *Server) deleteWeixinBinding(w http.ResponseWriter, r *http.Request) {
	if !requireSameOrigin(w, r) {
		return
	}
	if accountID := r.PathValue("accountID"); accountID != "" {
		bindings, err := s.channels.ListBindings()
		if err != nil {
			writeChannelError(w, err)
			return
		}
		found := false
		for _, binding := range bindings {
			if binding.ID == r.PathValue("bindingID") {
				found = binding.AccountID == accountID
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Weixin channel record not found"})
			return
		}
	}
	if err := s.channels.DeleteBinding(r.PathValue("bindingID")); err != nil {
		writeChannelError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listWeixinInbox(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	items, err := s.channels.ListInbox(accountID, 100)
	if err != nil {
		writeChannelError(w, err)
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{"id": item.ID, "account_id": item.AccountID, "peer_id": item.PeerID, "group_id": item.GroupID, "message_id": item.MessageID, "sequence": item.Sequence, "text": item.Text, "state": item.State, "task_id": item.TaskID, "received_at": item.ReceivedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func publicAccounts(accounts []channelstore.Account) []map[string]any {
	result := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, map[string]any{"id": account.ID, "label": account.Label, "state": account.State, "weixin_user_id": mask(account.WeixinUserID), "created_at": account.CreatedAt, "updated_at": account.UpdatedAt, "last_error": account.LastError})
	}
	return result
}

func publicAttempt(attempt channelstore.LoginAttempt) map[string]any {
	return map[string]any{"id": attempt.ID, "account_id": attempt.AccountID, "state": attempt.State, "expires_at": attempt.ExpiresAt, "created_at": attempt.CreatedAt, "updated_at": attempt.UpdatedAt, "qr_available": attempt.QRImage != ""}
}

func publicBindings(bindings []channelstore.Binding) []map[string]any {
	result := make([]map[string]any, 0, len(bindings))
	for _, binding := range bindings {
		result = append(result, publicBinding(binding))
	}
	return result
}

func publicBinding(binding channelstore.Binding) map[string]any {
	return map[string]any{"id": binding.ID, "account_id": binding.AccountID, "peer_id": binding.PeerID, "group_id": binding.GroupID, "employee_id": binding.EmployeeID, "enabled": binding.Enabled, "mention_required": binding.MentionRequired, "created_at": binding.CreatedAt, "updated_at": binding.UpdatedAt}
}

func writeChannelError(w http.ResponseWriter, err error) {
	var serviceErr *controlplane.Error
	if errors.As(err, &serviceErr) {
		status := http.StatusInternalServerError
		switch serviceErr.Kind {
		case controlplane.KindInvalid:
			status = http.StatusBadRequest
		case controlplane.KindNotFound:
			status = http.StatusNotFound
		case controlplane.KindConflict:
			status = http.StatusConflict
		case controlplane.KindBadGateway:
			status = http.StatusBadGateway
		}
		writeJSON(w, status, map[string]any{"error": "Weixin channel request failed"})
		return
	}
	switch {
	case errors.Is(err, channelstore.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Weixin channel record not found"})
	case errors.Is(err, channelstore.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Weixin channel state conflict"})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Weixin channel request failed"})
	}
}

func textValue(value any) string {
	if value, ok := value.(string); ok && len(value) <= 4096 {
		return value
	}
	return ""
}

func boolValue(value any, fallback bool) bool {
	if value, ok := value.(bool); ok {
		return value
	}
	return fallback
}

func mask(value string) string {
	if len(value) <= 4 {
		if value == "" {
			return ""
		}
		return "••••"
	}
	return value[:2] + "••••" + value[len(value)-2:]
}
