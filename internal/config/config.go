// Package config loads and validates hermit.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Duration is a TOML string duration.
type Duration time.Duration

func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", text, err)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) Value() time.Duration { return time.Duration(d) }

type Config struct {
	Agent       Agent       `toml:"agent" json:"agent"`
	Model       Model       `toml:"model" json:"model"`
	Context     Context     `toml:"context" json:"context"`
	Tools       Tools       `toml:"tools" json:"tools"`
	Permissions Permissions `toml:"permissions" json:"permissions"`
	Storage     Storage     `toml:"storage" json:"storage"`
	Plugins     Plugins     `toml:"plugins" json:"plugins"`
}

type Agent struct {
	MaxTurns int      `toml:"max_turns" json:"max_turns"`
	Timeout  Duration `toml:"timeout" json:"timeout"`
}

type Model struct {
	Provider       string   `toml:"provider" json:"provider"`
	BaseURL        string   `toml:"base_url" json:"base_url"`
	Name           string   `toml:"model" json:"model"`
	APIKey         string   `toml:"api_key" json:"-"`
	APIKeyEnv      string   `toml:"api_key_env" json:"api_key_env"`
	RequestTimeout Duration `toml:"request_timeout" json:"request_timeout"`
	MaxRetries     int      `toml:"max_retries" json:"max_retries"`
	Stream         bool     `toml:"stream" json:"stream"`
}

type ModelPreset struct {
	Provider  string `json:"provider"`
	Protocol  string `json:"protocol"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"api_key_env"`
}

var modelPresets = map[string]ModelPreset{
	"codex":             {Provider: "codex", Protocol: "responses", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.3-codex", APIKeyEnv: "OPENAI_API_KEY"},
	"openai":            {Provider: "openai", Protocol: "responses", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.6", APIKeyEnv: "OPENAI_API_KEY"},
	"deepseek":          {Provider: "deepseek", Protocol: "chat_completions", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro", APIKeyEnv: "DEEPSEEK_API_KEY"},
	"qwen":              {Provider: "qwen", Protocol: "chat_completions", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3.7-plus", APIKeyEnv: "DASHSCOPE_API_KEY"},
	"openai-compatible": {Provider: "openai-compatible", Protocol: "chat_completions", BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY"},
	"openai-chat":       {Provider: "openai-chat", Protocol: "chat_completions", BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY"},
}

func ModelPresets() []ModelPreset {
	names := []string{"codex", "openai", "deepseek", "qwen", "openai-compatible", "openai-chat"}
	out := make([]ModelPreset, 0, len(names))
	for _, name := range names {
		out = append(out, modelPresets[name])
	}
	return out
}

func (m Model) Preset() (ModelPreset, bool) {
	p, ok := modelPresets[m.Provider]
	return p, ok
}

func (m Model) Protocol() string {
	if p, ok := m.Preset(); ok {
		return p.Protocol
	}
	return ""
}

type Context struct {
	MaxTokens            int     `toml:"max_tokens" json:"max_tokens"`
	CompressionThreshold float64 `toml:"compression_threshold" json:"compression_threshold"`
	HardLimitThreshold   float64 `toml:"hard_limit_threshold" json:"hard_limit_threshold"`
	ReserveOutputTokens  int     `toml:"reserve_output_tokens" json:"reserve_output_tokens"`
}

type Tools struct {
	DefaultTimeout Duration `toml:"default_timeout" json:"default_timeout"`
	MaxStdoutBytes int      `toml:"max_stdout_bytes" json:"max_stdout_bytes"`
	MaxStderrBytes int      `toml:"max_stderr_bytes" json:"max_stderr_bytes"`
}

type Permissions struct {
	WorkspaceOnly                           bool `toml:"workspace_only" json:"workspace_only"`
	AllowNetwork                            bool `toml:"allow_network" json:"allow_network"`
	AllowWriteOutsideWorkspace              bool `toml:"allow_write_outside_workspace" json:"allow_write_outside_workspace"`
	RequireConfirmationForDangerousCommands bool `toml:"require_confirmation_for_dangerous_commands" json:"require_confirmation_for_dangerous_commands"`
}

type Storage struct {
	Directory                  string   `toml:"directory" json:"directory"`
	FlushInterval              Duration `toml:"flush_interval" json:"flush_interval"`
	CheckpointEveryTurns       int      `toml:"checkpoint_every_turns" json:"checkpoint_every_turns"`
	CheckpointOnToolCompletion bool     `toml:"checkpoint_on_tool_completion" json:"checkpoint_on_tool_completion"`
	SaveStreamChunks           bool     `toml:"save_stream_chunks" json:"save_stream_chunks"`
	SaveFullPrompts            bool     `toml:"save_full_prompts" json:"save_full_prompts"`
	SaveFullToolOutput         bool     `toml:"save_full_tool_output" json:"save_full_tool_output"`
	MaxLogSizeMB               int      `toml:"max_log_size_mb" json:"max_log_size_mb"`
	RetentionDays              int      `toml:"retention_days" json:"retention_days"`
}

type Plugins struct {
	Enabled         bool            `toml:"enabled" json:"enabled"`
	MaxMessageBytes int             `toml:"max_message_bytes" json:"max_message_bytes"`
	DefaultTimeout  Duration        `toml:"default_timeout" json:"default_timeout"`
	Processes       []PluginProcess `toml:"process" json:"process,omitempty"`
}

type PluginProcess struct {
	Name    string   `toml:"name" json:"name"`
	Command string   `toml:"command" json:"command"`
	Args    []string `toml:"args" json:"args,omitempty"`
	Enabled bool     `toml:"enabled" json:"enabled"`
}

func Default() Config {
	return Config{
		Agent:       Agent{MaxTurns: 50, Timeout: Duration(30 * time.Minute)},
		Model:       Model{Provider: "openai-compatible", BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY", RequestTimeout: Duration(120 * time.Second), MaxRetries: 3, Stream: true},
		Context:     Context{MaxTokens: 128000, CompressionThreshold: .80, HardLimitThreshold: .92, ReserveOutputTokens: 16000},
		Tools:       Tools{DefaultTimeout: Duration(120 * time.Second), MaxStdoutBytes: 1 << 20, MaxStderrBytes: 1 << 20},
		Permissions: Permissions{WorkspaceOnly: true, RequireConfirmationForDangerousCommands: true},
		Storage:     Storage{Directory: ".gohermit", FlushInterval: Duration(10 * time.Second), CheckpointEveryTurns: 5, CheckpointOnToolCompletion: true, MaxLogSizeMB: 20, RetentionDays: 7},
		Plugins:     Plugins{Enabled: true, MaxMessageBytes: 4 << 20, DefaultTimeout: Duration(60 * time.Second)},
	}
}

// Load loads path over safe defaults. A missing optional path returns defaults.
func Load(path string, optional bool) (Config, error) {
	c := Default()
	if path == "" {
		path = "hermit.toml"
	}
	meta, err := toml.DecodeFile(path, &c)
	if errors.Is(err, os.ErrNotExist) && optional {
		return c, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("load config %s: %w", path, err)
	}
	applyModelPreset(&c, func(key string) bool { return meta.IsDefined("model", key) })
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		parts := make([]string, len(undecoded))
		for i, key := range undecoded {
			parts[i] = key.String()
		}
		return Config{}, fmt.Errorf("unknown configuration keys: %s", strings.Join(parts, ", "))
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func applyModelPreset(c *Config, isDefined func(string) bool) {
	preset, ok := modelPresets[c.Model.Provider]
	if !ok {
		return
	}
	if !isDefined("base_url") {
		c.Model.BaseURL = preset.BaseURL
	}
	if !isDefined("model") && preset.Model != "" {
		c.Model.Name = preset.Model
	}
	if !isDefined("api_key_env") {
		c.Model.APIKeyEnv = preset.APIKeyEnv
	}
}

func (c Config) Validate() error {
	var problems []string
	if c.Agent.MaxTurns < 1 || c.Agent.MaxTurns > 1000 {
		problems = append(problems, "agent.max_turns must be between 1 and 1000")
	}
	if c.Agent.Timeout.Value() <= 0 {
		problems = append(problems, "agent.timeout must be positive")
	}
	if _, ok := modelPresets[c.Model.Provider]; !ok {
		problems = append(problems, "model.provider must be codex, openai, deepseek, qwen, openai-chat, or openai-compatible")
	}
	if !strings.HasPrefix(c.Model.BaseURL, "https://") && !strings.HasPrefix(c.Model.BaseURL, "http://localhost") && !strings.HasPrefix(c.Model.BaseURL, "http://127.0.0.1") {
		problems = append(problems, "model.base_url must use HTTPS or loopback HTTP")
	}
	if c.Model.RequestTimeout.Value() <= 0 {
		problems = append(problems, "model.request_timeout must be positive")
	}
	if c.Model.MaxRetries < 0 || c.Model.MaxRetries > 10 {
		problems = append(problems, "model.max_retries must be between 0 and 10")
	}
	if c.Context.MaxTokens < 1024 || c.Context.ReserveOutputTokens < 1 || c.Context.ReserveOutputTokens >= c.Context.MaxTokens {
		problems = append(problems, "context token budget is invalid")
	}
	if c.Context.CompressionThreshold <= 0 || c.Context.CompressionThreshold >= c.Context.HardLimitThreshold || c.Context.HardLimitThreshold > 1 {
		problems = append(problems, "context thresholds must satisfy 0 < compression < hard_limit <= 1")
	}
	if c.Tools.DefaultTimeout.Value() <= 0 || c.Tools.MaxStdoutBytes < 1024 || c.Tools.MaxStderrBytes < 1024 {
		problems = append(problems, "tool limits must be positive and output limits at least 1024 bytes")
	}
	if !c.Permissions.WorkspaceOnly || c.Permissions.AllowWriteOutsideWorkspace {
		problems = append(problems, "v0.1.0 requires workspace_only=true and allow_write_outside_workspace=false")
	}
	if filepath.IsAbs(c.Storage.Directory) || c.Storage.Directory == "" || strings.Contains(filepath.Clean(c.Storage.Directory), "..") {
		problems = append(problems, "storage.directory must be a relative workspace path")
	}
	if c.Storage.CheckpointEveryTurns < 1 || c.Storage.RetentionDays < 1 || c.Storage.MaxLogSizeMB < 1 {
		problems = append(problems, "storage checkpoint, retention, and log limits must be positive")
	}
	if c.Storage.SaveStreamChunks || c.Storage.SaveFullPrompts || c.Storage.SaveFullToolOutput {
		problems = append(problems, "unsafe high-write storage options are not supported in v0.1.0")
	}
	if c.Plugins.MaxMessageBytes < 1024 || c.Plugins.DefaultTimeout.Value() <= 0 {
		problems = append(problems, "plugin limits must be positive")
	}
	pluginNames := map[string]bool{}
	for _, process := range c.Plugins.Processes {
		if process.Name == "" || strings.ContainsAny(process.Name, "/\\") {
			problems = append(problems, "plugin process names must be non-empty identifiers")
		}
		if pluginNames[process.Name] {
			problems = append(problems, "plugin process names must be unique")
		}
		pluginNames[process.Name] = true
		if process.Enabled && process.Command == "" {
			problems = append(problems, "enabled plugin process command must not be empty")
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) APIKey() (string, error) {
	if c.Model.APIKey != "" {
		return c.Model.APIKey, nil
	}
	if c.Model.APIKeyEnv == "" {
		return "", errors.New("model.api_key_env is empty")
	}
	key := os.Getenv(c.Model.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("API key environment variable %s is not set", c.Model.APIKeyEnv)
	}
	return key, nil
}
