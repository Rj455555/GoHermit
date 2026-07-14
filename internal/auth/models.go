package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const codexModelsURL = "https://chatgpt.com/backend-api/codex/models?client_version=1.0.0"

// CodexModel is one model currently exposed to the authenticated ChatGPT account.
type CodexModel struct {
	ID       string
	Priority int
}

// DiscoverCodexModels reads the live Codex catalog instead of guessing account entitlements.
func DiscoverCodexModels(ctx context.Context, accessToken string) ([]CodexModel, error) {
	return discoverCodexModels(ctx, http.DefaultClient, codexModelsURL, accessToken)
}

func discoverCodexModels(ctx context.Context, client *http.Client, endpoint, accessToken string) ([]CodexModel, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("Codex access token is empty")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "gohermit/0.2")
	clientWithTimeout := client
	if clientWithTimeout == http.DefaultClient {
		clientWithTimeout = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := clientWithTimeout.Do(request)
	if err != nil {
		return nil, fmt.Errorf("discover Codex models: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Codex model discovery returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
			Priority   int    `json:"priority"`
		} `json:"models"`
	}
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode Codex model catalog: %w", err)
	}
	models := make([]CodexModel, 0, len(payload.Models))
	seen := make(map[string]bool)
	for _, item := range payload.Models {
		id := strings.TrimSpace(item.Slug)
		visibility := strings.ToLower(strings.TrimSpace(item.Visibility))
		if id == "" || visibility == "hide" || visibility == "hidden" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, CodexModel{ID: id, Priority: item.Priority})
	}
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Priority == models[j].Priority {
			return models[i].ID < models[j].ID
		}
		return models[i].Priority < models[j].Priority
	})
	if len(models) == 0 {
		return nil, errors.New("Codex account returned no visible models")
	}
	return models, nil
}
