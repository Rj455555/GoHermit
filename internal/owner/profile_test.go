package owner

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTripAndForget(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "owner.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile := Profile{Identity: Identity{DisplayName: "Yuanxin", Language: "Chinese"}, Preferences: Preferences{Verification: "run tests before completion"}}
	if err = store.Save(profile); err != nil {
		t.Fatal(err)
	}
	profile, err = store.UpsertFact(Fact{ID: "host-macmini", Category: "environment", Value: "macmini is the development host", Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	markdown := Markdown(profile)
	if !strings.Contains(markdown, "Yuanxin") || !strings.Contains(markdown, "macmini") {
		t.Fatalf("markdown=%q", markdown)
	}
	profile, err = store.ForgetFact("host-macmini")
	if err != nil || len(profile.Facts) != 0 {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
}

func TestStoreRejectsSecrets(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "owner.json"))
	err := store.Save(Profile{Preferences: Preferences{Coding: "api_key=do-not-store-this"}})
	if err == nil {
		t.Fatal("expected secret rejection")
	}
}
