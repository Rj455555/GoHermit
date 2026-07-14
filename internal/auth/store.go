package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store keeps local provider secrets outside the workspace in a mode-0600 file.
type Store struct {
	path string
	mu   sync.Mutex
}

type storeData struct {
	Version int               `json:"version"`
	APIKeys map[string]string `json:"api_keys,omitempty"`
	Codex   *codexTokens      `json:"codex,omitempty"`
}

// NewStore opens a local credential store. The file is created only on write.
func NewStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("GOHERMIT_AUTH_STORE"))
	}
	if path == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user config directory: %w", err)
		}
		path = filepath.Join(root, "gohermit", "auth.json")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve credential store: %w", err)
	}
	return &Store{path: abs}, nil
}

// APIKey returns a stored key without logging or exposing it through JSON APIs.
func (s *Store) APIKey(provider string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return "", false
	}
	key := strings.TrimSpace(data.APIKeys[provider])
	return key, key != ""
}

// SetAPIKey atomically persists one provider key.
func (s *Store) SetAPIKey(provider, key string) error {
	provider, key = strings.TrimSpace(provider), strings.TrimSpace(key)
	if provider == "" || key == "" {
		return errors.New("provider and API key are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return err
	}
	if data.APIKeys == nil {
		data.APIKeys = make(map[string]string)
	}
	data.APIKeys[provider] = key
	return s.save(data)
}

// Delete removes any locally stored API key or Codex account for a provider.
func (s *Store) Delete(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return err
	}
	delete(data.APIKeys, provider)
	if provider == "openai-codex" {
		data.Codex = nil
	}
	return s.save(data)
}

func (s *Store) codexTokens() (codexTokens, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil || data.Codex == nil || strings.TrimSpace(data.Codex.AccessToken) == "" {
		return codexTokens{}, false
	}
	return *data.Codex, true
}

func (s *Store) saveCodex(tokens codexTokens) error {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return errors.New("Codex access token is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return err
	}
	data.Codex = &tokens
	return s.save(data)
}

func (s *Store) load() (storeData, error) {
	data := storeData{Version: 1, APIKeys: make(map[string]string)}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, fmt.Errorf("read credential store: %w", err)
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return storeData{}, fmt.Errorf("decode credential store: %w", err)
	}
	if data.Version != 1 {
		return storeData{}, fmt.Errorf("unsupported credential store version %d", data.Version)
	}
	if data.APIKeys == nil {
		data.APIKeys = make(map[string]string)
	}
	return data, nil
}

func (s *Store) save(data storeData) error {
	data.Version = 1
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credential store: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".auth-*.tmp")
	if err != nil {
		return fmt.Errorf("create credential temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(raw)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write credential store: %w", err)
	}
	if err = os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace credential store: %w", err)
	}
	return nil
}
