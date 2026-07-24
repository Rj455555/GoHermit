// Package app assembles the CLI without leaking presentation into the runtime.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/agent"
	modelauth "github.com/Rj455555/GoHermit/internal/auth"
	"github.com/Rj455555/GoHermit/internal/config"
	"github.com/Rj455555/GoHermit/internal/contextmgr"
	"github.com/Rj455555/GoHermit/internal/event"
	"github.com/Rj455555/GoHermit/internal/model"
	"github.com/Rj455555/GoHermit/internal/plugin"
	"github.com/Rj455555/GoHermit/internal/session"
	"github.com/Rj455555/GoHermit/internal/tool"
	"github.com/Rj455555/GoHermit/internal/tool/builtin"
)

const Version = "0.6.0-dev"
const (
	ExitOK        = 0
	ExitRuntime   = 1
	ExitUsage     = 2
	ExitConfig    = 3
	ExitCancelled = 130
)

type CLI struct {
	Stdout, Stderr io.Writer
	NewProvider    func(config.Config) (model.Provider, error)
}

type Runtime struct {
	Workspace string
	Config    config.Config
	Store     *session.Store
	Runner    *agent.Runner
	close     func()
}

// RuntimeOptions contains validated, non-secret per-run overrides.
type RuntimeOptions struct {
	Selection *config.RuntimeSelection
	APIKey    string
	Models    []config.ModelOption
	// Approvals, when set, lets the runner park confirmation-required calls
	// until the owner decides (ADR 0011). Nil keeps deny-by-default: no
	// approval request is ever created.
	Approvals agent.ApprovalDecisions
}

func (r *Runtime) Close() {
	if r != nil && r.close != nil {
		r.close()
	}
}

func (c CLI) Run(ctx context.Context, args []string) int {
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	if c.Stderr == nil {
		c.Stderr = os.Stderr
	}
	if len(args) == 0 {
		return c.usage("missing command")
	}
	switch args[0] {
	case "run":
		return c.runTask(ctx, args[1:])
	case "resume":
		return c.resume(ctx, args[1:])
	case "status":
		return c.status(ctx, args[1:], false)
	case "context":
		return c.status(ctx, args[1:], true)
	case "clean":
		return c.clean(ctx, args[1:])
	case "config":
		return c.configCommand(args[1:])
	case "version", "--version", "-v":
		fmt.Fprintln(c.Stdout, Version)
		return ExitOK
	case "help", "--help", "-h":
		c.printUsage(c.Stdout)
		return ExitOK
	default:
		return c.usage("unknown command: " + args[0])
	}
}

type commonFlags struct{ workspace, configPath, output string }

func addCommon(fs *flag.FlagSet, f *commonFlags) {
	cwd, _ := os.Getwd()
	fs.StringVar(&f.workspace, "workspace", cwd, "workspace directory")
	fs.StringVar(&f.configPath, "config", "", "configuration file")
	fs.StringVar(&f.output, "output", "human", "output mode: human or json")
}
func (c CLI) runTask(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	var f commonFlags
	addCommon(fs, &f)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		return c.usage("run requires exactly one task")
	}
	workspace, conf, store, runner, cleanup, err := c.assemble(ctx, f)
	if err != nil {
		return c.reportError(err, ExitConfig)
	}
	defer cleanup()
	if conf.Agent.Profile == "team" {
		return c.reportError(errors.New("the team profile currently requires the Web Session API"), ExitUsage)
	}
	s, err := session.New(fs.Arg(0), workspace, session.ConfigDigest(conf))
	if err != nil {
		return c.reportError(err, ExitRuntime)
	}
	s.GitState = session.GitState(ctx, workspace)
	runner.Sink = c.renderer(f.output)
	err = runner.Run(ctx, s)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ExitCancelled
		}
		return c.reportError(err, ExitRuntime)
	}
	_ = store
	return ExitOK
}
func (c CLI) resume(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	var f commonFlags
	addCommon(fs, &f)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		return c.usage("resume requires one session ID")
	}
	_, _, store, runner, cleanup, err := c.assemble(ctx, f)
	if err != nil {
		return c.reportError(err, ExitConfig)
	}
	defer cleanup()
	s, err := store.Recover(ctx, fs.Arg(0))
	if err != nil {
		return c.reportError(err, ExitRuntime)
	}
	if s.Selection.Agent == "team" || s.Mission != nil {
		return c.reportError(errors.New("team runs must be resumed through the Web Session API"), ExitUsage)
	}
	if run := s.ActiveRun(); run == nil || run.Status != session.RunInterrupted {
		return c.reportError(errors.New("session has no interrupted run to resume"), ExitRuntime)
	}
	runner.Sink = c.renderer(f.output)
	if err = runner.Run(ctx, s); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ExitCancelled
		}
		return c.reportError(err, ExitRuntime)
	}
	return ExitOK
}
func (c CLI) status(ctx context.Context, args []string, summaryOnly bool) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	var f commonFlags
	addCommon(fs, &f)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		return c.usage("session ID is required")
	}
	workspace, err := filepath.Abs(f.workspace)
	if err != nil {
		return c.reportError(err, ExitUsage)
	}
	conf, err := LoadConfig(workspace, f.configPath)
	if err != nil {
		return c.reportError(err, ExitConfig)
	}
	store, err := session.NewStore(workspace, conf.Storage.Directory)
	if err != nil {
		return c.reportError(err, ExitConfig)
	}
	s, err := store.Load(ctx, fs.Arg(0))
	if err != nil {
		return c.reportError(err, ExitRuntime)
	}
	if summaryOnly {
		fmt.Fprintln(c.Stdout, s.Summary)
		return ExitOK
	}
	if f.output == "json" {
		return c.writeJSON(s)
	}
	runStatus := "idle"
	if run := s.ActiveRun(); run != nil {
		runStatus = string(run.Status)
	} else if len(s.Runs) > 0 {
		runStatus = string(s.Runs[len(s.Runs)-1].Status)
	}
	fmt.Fprintf(c.Stdout, "Session: %s\nStatus: %s\nRun: %s\nTurns: %d\nUpdated: %s\nGoal: %s\n", s.ID, s.Status, runStatus, s.Turns, s.UpdatedAt.Format(time.RFC3339), s.Goal)
	if s.LastError != "" {
		fmt.Fprintln(c.Stdout, "Last error:", s.LastError)
	}
	return ExitOK
}
func (c CLI) clean(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	var f commonFlags
	addCommon(fs, &f)
	older := fs.String("older-than", "", "age such as 7d or 168h")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	d, err := parseAge(*older)
	if err != nil {
		return c.reportError(err, ExitUsage)
	}
	workspace, err := filepath.Abs(f.workspace)
	if err != nil {
		return c.reportError(err, ExitUsage)
	}
	conf, err := LoadConfig(workspace, f.configPath)
	if err != nil {
		return c.reportError(err, ExitConfig)
	}
	store, err := session.NewStore(workspace, conf.Storage.Directory)
	if err != nil {
		return c.reportError(err, ExitConfig)
	}
	n, err := store.Clean(ctx, d)
	if err != nil {
		return c.reportError(err, ExitRuntime)
	}
	if f.output == "json" {
		return c.writeJSON(map[string]any{"cleaned": n})
	}
	fmt.Fprintf(c.Stdout, "Cleaned %d sessions.\n", n)
	return ExitOK
}
func (c CLI) configCommand(args []string) int {
	if len(args) == 0 || args[0] != "validate" {
		return c.usage("config requires the validate subcommand")
	}
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	path := fs.String("config", "hermit.toml", "configuration file")
	output := fs.String("output", "human", "output mode")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage
	}
	conf, err := config.Load(*path, false)
	if err != nil {
		return c.reportError(err, ExitConfig)
	}
	if *output == "json" {
		return c.writeJSON(map[string]any{"valid": true, "config": conf})
	}
	fmt.Fprintln(c.Stdout, "Configuration is valid.")
	return ExitOK
}

func (c CLI) assemble(ctx context.Context, f commonFlags) (string, config.Config, *session.Store, *agent.Runner, func(), error) {
	noop := func() {}
	if f.output != "human" && f.output != "json" {
		return "", config.Config{}, nil, nil, noop, errors.New("output must be human or json")
	}
	runtime, err := BuildRuntime(ctx, f.workspace, f.configPath, c.NewProvider)
	if err != nil {
		return "", config.Config{}, nil, nil, noop, err
	}
	return runtime.Workspace, runtime.Config, runtime.Store, runtime.Runner, runtime.Close, nil
}

func BuildRuntime(ctx context.Context, workspace, configPath string, newProvider func(config.Config) (model.Provider, error)) (*Runtime, error) {
	return BuildRuntimeWithOptions(ctx, workspace, configPath, RuntimeOptions{}, newProvider)
}

// BuildRuntimeWithOptions assembles a runtime after applying a catalog selection.
func BuildRuntimeWithOptions(ctx context.Context, workspace, configPath string, options RuntimeOptions, newProvider func(config.Config) (model.Provider, error)) (*Runtime, error) {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	conf, err := LoadConfig(workspace, configPath)
	if err != nil {
		return nil, err
	}
	if options.Selection != nil {
		preset, profile, selectionErr := config.ResolveSelectionWithModels(*options.Selection, options.Models)
		if selectionErr != nil {
			return nil, selectionErr
		}
		conf.Model.Provider = preset.Provider
		conf.Model.BaseURL = preset.BaseURL
		conf.Model.Name = preset.Model
		conf.Model.APIKeyEnv = preset.APIKeyEnv
		conf.Model.APIKey = strings.TrimSpace(options.APIKey)
		conf.Agent.Profile = profile.ID
		if err = conf.Validate(); err != nil {
			return nil, err
		}
	}
	if conf.Model.Name == "" {
		return nil, errors.New("model.model must be configured")
	}
	var provider model.Provider
	if newProvider != nil {
		provider, err = newProvider(conf)
	} else {
		provider, err = NewProvider(conf)
	}
	if err != nil {
		return nil, err
	}
	ws, err := builtin.NewWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	registry := tool.NewRegistry()
	profile, _ := config.AgentProfile(conf.Agent.Profile)
	switch profile.ToolPolicy {
	case "verify":
		err = builtin.RegisterVerification(registry, ws, conf.Tools.DefaultTimeout.Value(), conf.Tools.MaxStdoutBytes, conf.Tools.MaxStderrBytes)
	case "read", "team":
		err = builtin.RegisterReadOnly(registry, ws, conf.Tools.DefaultTimeout.Value(), conf.Tools.MaxStdoutBytes, conf.Tools.MaxStderrBytes)
	default:
		err = builtin.RegisterAll(registry, ws, conf.Tools.DefaultTimeout.Value(), conf.Tools.MaxStdoutBytes, conf.Tools.MaxStderrBytes, conf.Permissions.AllowNetwork)
	}
	if err != nil {
		return nil, err
	}
	var clients []*plugin.Client
	cleanup := func() {
		for i := len(clients) - 1; i >= 0; i-- {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = clients[i].Shutdown(shutdownCtx)
			cancel()
		}
	}
	if conf.Plugins.Enabled {
		for _, process := range conf.Plugins.Processes {
			if !process.Enabled {
				continue
			}
			client, startErr := plugin.Start(ctx, plugin.Config{Command: append([]string{process.Command}, process.Args...), Directory: workspace, MaxMessageBytes: conf.Plugins.MaxMessageBytes, DefaultTimeout: conf.Plugins.DefaultTimeout.Value()})
			if startErr != nil {
				cleanup()
				return nil, fmt.Errorf("start plugin %s: %w", process.Name, startErr)
			}
			clients = append(clients, client)
			var allowPluginTool func(tool.Definition) bool
			if profile.ToolPolicy == "read" || profile.ToolPolicy == "verify" || profile.ToolPolicy == "team" {
				allowPluginTool = func(definition tool.Definition) bool {
					return definition.Permission == tool.PermissionRead && !definition.MutatesWorkspace
				}
			}
			if startErr = plugin.RegisterToolsWithPolicy(ctx, registry, process.Name, client, allowPluginTool); startErr != nil {
				cleanup()
				return nil, fmt.Errorf("register plugin %s: %w", process.Name, startErr)
			}
		}
	}
	store, err := session.NewStore(workspace, conf.Storage.Directory)
	if err != nil {
		cleanup()
		return nil, err
	}
	manager, err := contextmgr.New(contextmgr.Config{MaxTokens: conf.Context.MaxTokens, CompressionThreshold: conf.Context.CompressionThreshold, HardLimitThreshold: conf.Context.HardLimitThreshold, ReserveOutputTokens: conf.Context.ReserveOutputTokens, SystemPrompt: contextmgr.PromptForProfile(conf.Agent.Profile)})
	if err != nil {
		cleanup()
		return nil, err
	}
	runner := &agent.Runner{Provider: provider, Executor: tool.Executor{Registry: registry, DefaultTimeout: conf.Tools.DefaultTimeout.Value()}, Context: manager, Store: store, Config: agent.Config{MaxTurns: conf.Agent.MaxTurns, Timeout: conf.Agent.Timeout.Value(), Model: conf.Model.Name, Stream: conf.Model.Stream, CheckpointEveryTurns: conf.Storage.CheckpointEveryTurns, CheckpointOnToolCompletion: conf.Storage.CheckpointOnToolCompletion}, Approvals: options.Approvals}
	return &Runtime{Workspace: workspace, Config: conf, Store: store, Runner: runner, close: cleanup}, nil
}

func NewProvider(conf config.Config) (model.Provider, error) {
	if conf.Model.Provider == "openai-codex" {
		if strings.TrimSpace(conf.Model.APIKey) != "" {
			return model.NewResponsesProvider(model.ResponsesConfig{BaseURL: conf.Model.BaseURL, APIKey: conf.Model.APIKey, Headers: modelauth.CodexHeaders(conf.Model.APIKey), Timeout: conf.Model.RequestTimeout.Value(), MaxRetries: conf.Model.MaxRetries})
		}
		credentials, err := modelauth.ResolveCodex(context.Background())
		if err != nil {
			return nil, err
		}
		return model.NewResponsesProvider(model.ResponsesConfig{BaseURL: conf.Model.BaseURL, APIKey: credentials.Token, Headers: credentials.Headers, Timeout: conf.Model.RequestTimeout.Value(), MaxRetries: conf.Model.MaxRetries})
	}
	key, err := conf.APIKey()
	if err != nil {
		return nil, err
	}
	switch conf.Model.Protocol() {
	case "responses":
		return model.NewResponsesProvider(model.ResponsesConfig{BaseURL: conf.Model.BaseURL, APIKey: key, Timeout: conf.Model.RequestTimeout.Value(), MaxRetries: conf.Model.MaxRetries})
	case "chat_completions":
		sanitizeToolNames := false
		if preset, ok := conf.Model.Preset(); ok {
			sanitizeToolNames = preset.SanitizeToolNames
		}
		return model.NewOpenAIProvider(model.OpenAIConfig{BaseURL: conf.Model.BaseURL, APIKey: key, Timeout: conf.Model.RequestTimeout.Value(), MaxRetries: conf.Model.MaxRetries, SanitizeToolNames: sanitizeToolNames})
	default:
		return nil, fmt.Errorf("unsupported model provider %q", conf.Model.Provider)
	}
}

func LoadConfig(workspace, path string) (config.Config, error) {
	optional := path == ""
	if path == "" {
		path = filepath.Join(workspace, "hermit.toml")
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	return config.Load(path, optional)
}
func (c CLI) renderer(mode string) event.Sink {
	return func(e event.Event) {
		if mode == "json" {
			b, _ := json.Marshal(e)
			fmt.Fprintln(c.Stdout, string(b))
			return
		}
		switch e.Type {
		case event.TaskStarted:
			fmt.Fprintln(c.Stdout, "Session:", e.SessionID)
		case event.TurnStarted:
			fmt.Fprintf(c.Stdout, "Turn %d\n", e.Turn)
		case event.ModelDelta:
			fmt.Fprint(c.Stdout, e.Message)
		case event.ToolStarted:
			fmt.Fprintf(c.Stdout, "\n→ %s\n", e.Tool)
		case event.ToolCompleted:
			fmt.Fprintf(c.Stdout, "✓ %s: %s\n", e.Tool, e.Message)
		case event.PermissionRequired:
			fmt.Fprintf(c.Stdout, "! permission required for %s: %s\n", e.Tool, e.Message)
		case event.TaskCompleted:
			if e.Message != "" {
				fmt.Fprintln(c.Stdout, e.Message)
			}
		case event.TaskFailed, event.TaskCancelled:
			fmt.Fprintln(c.Stderr, e.Error)
		}
	}
}
func parseAge(v string) (time.Duration, error) {
	if v == "" {
		return 0, errors.New("--older-than is required")
	}
	if strings.HasSuffix(v, "d") {
		days := strings.TrimSuffix(v, "d")
		n, err := time.ParseDuration(days + "h")
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q", v)
		}
		return n * 24, nil
	}
	return time.ParseDuration(v)
}
func (c CLI) writeJSON(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return c.reportError(err, ExitRuntime)
	}
	fmt.Fprintln(c.Stdout, string(b))
	return ExitOK
}
func (c CLI) reportError(err error, code int) int {
	fmt.Fprintln(c.Stderr, "hermit:", err)
	return code
}
func (c CLI) usage(message string) int {
	fmt.Fprintln(c.Stderr, "hermit:", message)
	c.printUsage(c.Stderr)
	return ExitUsage
}
func (c CLI) printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: hermit <run|resume|status|context|clean|config validate> [options]")
}
