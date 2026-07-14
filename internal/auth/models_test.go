package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverCodexModelsUsesLiveVisibleCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{"slug": "later", "priority": 20},
			{"slug": "hidden", "priority": 1, "visibility": "hidden"},
			{"slug": "first", "priority": 10},
		}})
	}))
	defer server.Close()
	models, err := discoverCodexModels(t.Context(), server.Client(), server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "first" || models[1].ID != "later" {
		t.Fatalf("models=%+v", models)
	}
}
