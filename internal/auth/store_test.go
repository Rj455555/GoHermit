package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStorePersistsAndDeletesCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials", "auth.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetAPIKey("deepseek", "secret-value"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if key, ok := reloaded.APIKey("deepseek"); !ok || key != "secret-value" {
		t.Fatalf("key=%q ok=%v", key, ok)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("credential file permissions=%v", info.Mode().Perm())
	}
	if err = reloaded.Delete("deepseek"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.APIKey("deepseek"); ok {
		t.Fatal("deleted key is still available")
	}
}
