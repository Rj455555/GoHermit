// Package owner stores the single owner's explicit preferences outside repositories.
package owner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Rj455555/GoHermit/internal/storage"
)

const (
	SchemaVersion = 1
	MaxFacts      = 256
	MaxTextBytes  = 8 << 10
)

type Identity struct {
	DisplayName string `json:"display_name,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Language    string `json:"language,omitempty"`
}

type Preferences struct {
	Communication string `json:"communication,omitempty"`
	Coding        string `json:"coding,omitempty"`
	Git           string `json:"git,omitempty"`
	Verification  string `json:"verification,omitempty"`
	Risk          string `json:"risk,omitempty"`
}

type Environment struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind,omitempty"`
	Alias string `json:"alias,omitempty"`
	Notes string `json:"notes,omitempty"`
}

type Fact struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Value     string    `json:"value"`
	Source    string    `json:"source,omitempty"`
	Confirmed bool      `json:"confirmed"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Profile struct {
	SchemaVersion int           `json:"schema_version"`
	Identity      Identity      `json:"identity"`
	Preferences   Preferences   `json:"preferences"`
	Environments  []Environment `json:"environments,omitempty"`
	Facts         []Fact        `json:"facts,omitempty"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("GOHERMIT_OWNER_STORE"))
	}
	if path == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve owner profile directory: %w", err)
		}
		path = filepath.Join(root, "gohermit", "owner.json")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve owner profile path: %w", err)
	}
	return &Store{path: abs}, nil
}

func (s *Store) Load() (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) Save(profile Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(profile)
}

func (s *Store) UpsertFact(fact Fact) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, err := s.load()
	if err != nil {
		return Profile{}, err
	}
	fact.ID, fact.Category, fact.Value, fact.Source = clean(fact.ID), clean(fact.Category), clean(fact.Value), clean(fact.Source)
	if fact.ID == "" || fact.Category == "" || fact.Value == "" {
		return Profile{}, errors.New("fact id, category, and value are required")
	}
	now := time.Now().UTC()
	for i := range profile.Facts {
		if profile.Facts[i].ID == fact.ID {
			fact.CreatedAt = profile.Facts[i].CreatedAt
			fact.UpdatedAt = now
			profile.Facts[i] = fact
			return profile, s.save(profile)
		}
	}
	if len(profile.Facts) >= MaxFacts {
		return Profile{}, errors.New("owner fact limit exceeded")
	}
	fact.CreatedAt, fact.UpdatedAt = now, now
	profile.Facts = append(profile.Facts, fact)
	return profile, s.save(profile)
}

func (s *Store) ForgetFact(id string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, err := s.load()
	if err != nil {
		return Profile{}, err
	}
	id = clean(id)
	for i := range profile.Facts {
		if profile.Facts[i].ID != id {
			continue
		}
		profile.Facts = append(profile.Facts[:i], profile.Facts[i+1:]...)
		return profile, s.save(profile)
	}
	return Profile{}, fmt.Errorf("owner fact %q not found", id)
}

func (s *Store) load() (Profile, error) {
	profile := Profile{SchemaVersion: SchemaVersion}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return profile, nil
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read owner profile: %w", err)
	}
	if len(raw) > 256<<10 {
		return Profile{}, errors.New("owner profile exceeds size limit")
	}
	if err = json.Unmarshal(raw, &profile); err != nil {
		return Profile{}, fmt.Errorf("decode owner profile: %w", err)
	}
	if profile.SchemaVersion != SchemaVersion {
		return Profile{}, fmt.Errorf("unsupported owner profile version %d", profile.SchemaVersion)
	}
	if err = Validate(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (s *Store) save(profile Profile) error {
	profile.SchemaVersion = SchemaVersion
	profile.UpdatedAt = time.Now().UTC()
	if err := Validate(profile); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("encode owner profile: %w", err)
	}
	if len(raw) > 256<<10 {
		return errors.New("owner profile exceeds size limit")
	}
	if err = storage.AtomicWrite(s.path, append(raw, '\n'), 0600); err != nil {
		return fmt.Errorf("save owner profile: %w", err)
	}
	return nil
}

func Validate(profile Profile) error {
	if len(profile.Facts) > MaxFacts || len(profile.Environments) > 64 {
		return errors.New("owner profile exceeds item limits")
	}
	values := []string{profile.Identity.DisplayName, profile.Identity.Timezone, profile.Identity.Language, profile.Preferences.Communication, profile.Preferences.Coding, profile.Preferences.Git, profile.Preferences.Verification, profile.Preferences.Risk}
	for _, environment := range profile.Environments {
		values = append(values, environment.ID, environment.Label, environment.Kind, environment.Alias, environment.Notes)
	}
	for _, fact := range profile.Facts {
		if clean(fact.ID) == "" || clean(fact.Category) == "" || clean(fact.Value) == "" {
			return errors.New("owner facts require id, category, and value")
		}
		values = append(values, fact.ID, fact.Category, fact.Value, fact.Source)
	}
	for _, value := range values {
		if len(value) > MaxTextBytes {
			return errors.New("owner profile text exceeds size limit")
		}
		if LooksSecret(value) {
			return errors.New("owner profile must not contain credentials or tokens")
		}
	}
	return nil
}

// Markdown returns the compact, confirmed context injected into Workers.
func Markdown(profile Profile) string {
	var lines []string
	if value := clean(profile.Identity.DisplayName); value != "" {
		lines = append(lines, "- Owner: "+value)
	}
	if value := clean(profile.Identity.Timezone); value != "" {
		lines = append(lines, "- Timezone: "+value)
	}
	if value := clean(profile.Identity.Language); value != "" {
		lines = append(lines, "- Preferred language: "+value)
	}
	preferences := []struct{ label, value string }{{"Communication", profile.Preferences.Communication}, {"Coding", profile.Preferences.Coding}, {"Git", profile.Preferences.Git}, {"Verification", profile.Preferences.Verification}, {"Risk", profile.Preferences.Risk}}
	for _, preference := range preferences {
		if value := clean(preference.value); value != "" {
			lines = append(lines, "- "+preference.label+": "+value)
		}
	}
	facts := append([]Fact(nil), profile.Facts...)
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	for _, fact := range facts {
		if fact.Confirmed {
			lines = append(lines, "- "+fact.Category+": "+clean(fact.Value))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "# Owner profile\n\n" + strings.Join(lines, "\n") + "\n"
}

func clean(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
}

// LooksSecret reports whether value matches a credential or token marker.
func LooksSecret(value string) bool {
	lower := strings.ToLower(value)
	markers := []string{"authorization: bearer ", "password=", "passwd=", "api_key=", "apikey=", "access_token=", "refresh_token=", "ghp_", "github_pat_", "sk-proj-"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
