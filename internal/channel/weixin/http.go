package weixin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Rj455555/GoHermit/internal/channelstore"
)

type HTTPBackend struct {
	Client *http.Client
}

func NewHTTPBackend(client *http.Client) *HTTPBackend {
	if client == nil {
		client = &http.Client{Timeout: 40 * time.Second}
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("weixin redirects are disabled")
	}
	return &HTTPBackend{Client: client}
}

func (b *HTTPBackend) StartLogin(ctx context.Context, baseURL string) (QRStart, error) {
	var value map[string]any
	err := b.get(ctx, baseURL, "/ilink/bot/get_bot_qrcode?bot_type=3", nil, &value)
	if err != nil {
		return QRStart{}, err
	}
	if err := rejectUnknown(value, "qrcode", "qrcode_img_content", "expires_in_seconds", "ret"); err != nil {
		return QRStart{}, err
	}
	if code := number(value["ret"]); code != 0 {
		return QRStart{}, fmt.Errorf("weixin QR start returned %d", code)
	}
	result := QRStart{QRContent: text(value["qrcode"]), ImageContent: text(value["qrcode_img_content"]), ExpiresInSeconds: number(value["expires_in_seconds"])}
	if result.QRContent == "" || result.ImageContent == "" {
		return QRStart{}, errors.New("weixin QR response is incomplete")
	}
	if result.ExpiresInSeconds < 1 || result.ExpiresInSeconds > 600 {
		result.ExpiresInSeconds = 120
	}
	return result, nil
}

func (b *HTTPBackend) PollLogin(ctx context.Context, baseURL, qrContent string) (QRStatus, error) {
	if len(qrContent) == 0 || len(qrContent) > 256<<10 {
		return QRStatus{}, errors.New("weixin QR content is invalid")
	}
	var value map[string]any
	err := b.get(ctx, baseURL, "/ilink/bot/get_qrcode_status?qrcode="+url.QueryEscape(qrContent), nil, &value)
	if err != nil {
		return QRStatus{}, err
	}
	if err := rejectUnknown(value, "status", "ilink_bot_id", "ilink_user_id", "bot_token", "ret"); err != nil {
		return QRStatus{}, err
	}
	if code := number(value["ret"]); code != 0 {
		if code == -14 {
			return QRStatus{}, ErrSessionExpired
		}
		return QRStatus{}, fmt.Errorf("weixin QR status returned %d", code)
	}
	return QRStatus{Status: text(value["status"]), AccountID: text(value["ilink_bot_id"]), UserID: text(value["ilink_user_id"]), Token: text(value["bot_token"])}, nil
}

func (b *HTTPBackend) GetUpdates(ctx context.Context, baseURL, token, cursor string) (Updates, error) {
	var value map[string]any
	err := b.post(ctx, baseURL, "/ilink/bot/getupdates", token, map[string]any{"get_updates_buf": cursor, "bot_agent": BotAgent}, &value)
	if err != nil {
		return Updates{}, err
	}
	if code := number(value["ret"]); code != 0 {
		if code == -14 {
			return Updates{}, ErrSessionExpired
		}
		return Updates{}, fmt.Errorf("weixin getUpdates returned %d", code)
	}
	if err := rejectUnknown(value, "ret", "get_updates_buf", "msgs", "longpolling_timeout_ms"); err != nil {
		return Updates{}, err
	}
	result := Updates{Cursor: text(value["get_updates_buf"]), LongPollTimeoutMS: number(value["longpolling_timeout_ms"])}
	raw, ok := value["msgs"].([]any)
	if !ok {
		return result, nil
	}
	if len(raw) > MaxUpdates {
		return Updates{}, errors.New("weixin update batch is too large")
	}
	for _, item := range raw {
		decoded, ok := item.(map[string]any)
		if !ok {
			return Updates{}, errors.New("weixin update schema is invalid")
		}
		if err := rejectUnknown(decoded, "message_id", "msg_id", "seq", "from_user_id", "group_id", "context_token", "item_list", "to_user_id", "create_time", "message_type", "client_id"); err != nil {
			return Updates{}, err
		}
		messageID := text(decoded["message_id"])
		if messageID == "" {
			messageID = text(decoded["msg_id"])
		}
		message := Message{ID: messageID, Sequence: int64(number(decoded["seq"])), PeerID: text(decoded["from_user_id"]), GroupID: text(decoded["group_id"]), ContextToken: text(decoded["context_token"])}
		if list, ok := decoded["item_list"].([]any); ok {
			for _, rawItem := range list {
				part, ok := rawItem.(map[string]any)
				if !ok {
					return Updates{}, errors.New("weixin update item schema is invalid")
				}
				if err := rejectUnknown(part, "type", "text_item", "image_item", "video_item", "file_item", "voice_item", "mention_item"); err != nil {
					return Updates{}, err
				}
				if textPart := part["text_item"]; textPart != nil {
					if obj, ok := textPart.(map[string]any); ok {
						message.Text += text(obj["text"])
					}
				}
				if part["mention_item"] != nil {
					message.Mentioned = true
				}
				if part["image_item"] != nil || part["video_item"] != nil || part["file_item"] != nil || part["voice_item"] != nil {
					message.UnsupportedMedia = true
				}
			}
		}
		if message.ID == "" || message.PeerID == "" {
			continue
		}
		if len(message.ID) > 512 || len(message.PeerID) > 512 || len(message.GroupID) > 512 || len(message.ContextToken) > channelstore.MaxContextToken || len(message.Text) > MaxTextBytes {
			return Updates{}, errors.New("weixin update message is too large")
		}
		result.Messages = append(result.Messages, message)
	}
	return result, nil
}

func (b *HTTPBackend) SendMessage(ctx context.Context, baseURL, token, peerID, contextToken, message string) error {
	body := map[string]any{"msg": map[string]any{"to_user_id": peerID, "context_token": contextToken, "item_list": []any{map[string]any{"type": 1, "text_item": map[string]any{"text": message}}}}, "client_id": fmt.Sprintf("%d", time.Now().UnixNano())}
	var value map[string]any
	return b.post(ctx, baseURL, "/ilink/bot/sendmessage", token, body, &value)
}

func (b *HTTPBackend) GetConfig(ctx context.Context, baseURL, token, peerID, contextToken string) error {
	var value map[string]any
	return b.post(ctx, baseURL, "/ilink/bot/getconfig", token, map[string]any{"ilink_user_id": peerID, "context_token": contextToken}, &value)
}

func (b *HTTPBackend) SendTyping(ctx context.Context, baseURL, token, peerID, contextToken string, active bool) error {
	var value map[string]any
	status := 0
	if active {
		status = 1
	}
	return b.post(ctx, baseURL, "/ilink/bot/sendtyping", token, map[string]any{"ilink_user_id": peerID, "context_token": contextToken, "status": status}, &value)
}

func (b *HTTPBackend) Logout(ctx context.Context, baseURL, token string) error {
	var value map[string]any
	return b.post(ctx, baseURL, "/ilink/bot/logout", token, map[string]any{}, &value)
}

func (b *HTTPBackend) get(ctx context.Context, baseURL, endpoint string, token *string, target any) error {
	return b.do(ctx, http.MethodGet, baseURL, endpoint, token, nil, target)
}

func (b *HTTPBackend) post(ctx context.Context, baseURL, endpoint string, token string, body any, target any) error {
	return b.do(ctx, http.MethodPost, baseURL, endpoint, &token, body, target)
}

func (b *HTTPBackend) do(ctx context.Context, method, baseURL, endpoint string, token *string, body any, target any) error {
	parsed, err := url.Parse(baseURL)
	if err != nil || !allowedOrigin(parsed) {
		return errors.New("weixin endpoint is invalid")
	}
	endpointURL, endpointErr := url.Parse(endpoint)
	if endpointErr != nil || endpointURL.IsAbs() || endpointURL.Host != "" || strings.Contains(endpointURL.Path, "..") || strings.ContainsAny(endpoint, "\r\n") {
		return errors.New("weixin endpoint path is invalid")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + endpointURL.Path
	parsed.RawQuery = endpointURL.RawQuery
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil || len(data) > MaxResponseBytes {
			return errors.New("weixin request is too large")
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, parsed.String(), reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("AuthorizationType", "ilink_bot_token")
	request.Header.Set("User-Agent", BotAgent)
	var uin [4]byte
	if _, err := rand.Read(uin[:]); err == nil {
		request.Header.Set("X-WECHAT-UIN", base64.StdEncoding.EncodeToString(uin[:]))
	}
	if token != nil && strings.TrimSpace(*token) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(*token))
	}
	response, err := b.Client.Do(request)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return context.DeadlineExceeded
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("weixin endpoint returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > MaxResponseBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') || strings.ContainsRune(string(data), '\uFFFD') {
		return errors.New("weixin response is invalid or too large")
	}
	if target == nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("weixin response JSON is invalid")
	}
	return nil
}

func allowedOrigin(parsed *url.URL) bool {
	if parsed == nil || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.HasSuffix(host, ".test") {
		return parsed.Scheme == "http" || parsed.Scheme == "https"
	}
	if parsed.Scheme == "http" {
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	return parsed.Scheme == "https" && host == "ilinkai.weixin.qq.com"
}

func text(value any) string {
	if value, ok := value.(string); ok {
		if len(value) <= MaxResponseBytes && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00') && !strings.ContainsRune(value, '\uFFFD') {
			return value
		}
	}
	return ""
}

func number(value any) int {
	switch value := value.(type) {
	case float64:
		if value >= float64(-int(^uint(0)>>1))-1 && value <= float64(^uint(0)>>1) && math.Trunc(value) == value {
			return int(value)
		}
	case json.Number:
		var parsed int
		if _, err := fmt.Sscan(value.String(), &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func rejectUnknown(value map[string]any, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range value {
		if _, ok := set[key]; !ok {
			return fmt.Errorf("weixin response contains unsupported field %q", key)
		}
	}
	return nil
}
