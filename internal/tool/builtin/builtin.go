// Package builtin implements GoHermit's core workspace tools.
package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/policy"
	core "github.com/Rj455555/GoHermit/internal/tool"
)

type functionTool struct {
	def core.Definition
	run func(context.Context, core.Call) (core.Result, error)
}

func (t functionTool) Definition() core.Definition { return t.def }
func (t functionTool) Execute(ctx context.Context, c core.Call) (core.Result, error) {
	return t.run(ctx, c)
}
func schema(properties string, required string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}
func def(name, description string, s json.RawMessage, permission core.Permission, mutates bool, timeout time.Duration, max int) core.Definition {
	return core.Definition{Name: name, Description: description, InputSchema: s, Permission: permission, MutatesWorkspace: mutates, DefaultTimeout: timeout, MaxOutputBytes: max}
}

func RegisterAll(r *core.Registry, w *Workspace, timeout time.Duration, maxStdout, maxStderr int, allowNetwork bool) error {
	if maxStdout <= 0 || maxStderr <= 0 {
		return errors.New("stdout and stderr limits must be positive")
	}
	w.maxStdout, w.maxStderr = maxStdout, maxStderr
	maxOutput := maxStdout + maxStderr
	tools := []core.Tool{
		functionTool{def("filesystem.read", "Read a UTF-8 workspace file", schema(`"path":{"type":"string"}`, `"path"`), core.PermissionRead, false, timeout, maxOutput), w.read},
		functionTool{def("filesystem.list", "List workspace directory entries", schema(`"path":{"type":"string"},"recursive":{"type":"boolean"}`, ``), core.PermissionRead, false, timeout, maxOutput), w.list},
		functionTool{def("filesystem.search", "Search text in workspace files", schema(`"query":{"type":"string"},"path":{"type":"string"},"max_results":{"type":"integer"}`, `"query"`), core.PermissionRead, false, timeout, maxOutput), w.search},
		functionTool{def("filesystem.write", "Atomically write a UTF-8 workspace file", schema(`"path":{"type":"string"},"content":{"type":"string"}`, `"path","content"`), core.PermissionWrite, true, timeout, maxOutput), w.write},
		functionTool{def("patch.apply", "Apply a unified diff inside the workspace", schema(`"patch":{"type":"string"}`, `"patch"`), core.PermissionWrite, true, timeout, maxOutput), w.patch},
		functionTool{def("shell.execute", "Execute an allowlisted non-interactive command", schema(`"command":{"type":"string"}`, `"command"`), core.PermissionExecute, false, timeout, maxOutput), w.shell(allowNetwork)},
		functionTool{def("git.status", "Show Git working tree status", schema(``, ``), core.PermissionRead, false, timeout, maxOutput), w.command("git", "status", "--short", "--branch")},
		functionTool{def("git.diff", "Show Git changes", schema(`"staged":{"type":"boolean"}`, ``), core.PermissionRead, false, timeout, maxOutput), w.gitDiff},
		functionTool{def("git.log", "Show recent Git commits", schema(`"limit":{"type":"integer"}`, ``), core.PermissionRead, false, timeout, maxOutput), w.gitLog},
		functionTool{def("test.run", "Run Go tests in the workspace", schema(`"package":{"type":"string"}`, ``), core.PermissionExecute, false, timeout, maxOutput), w.testRun(allowNetwork)},
	}
	for _, t := range tools {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func decode(raw json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}
func (w *Workspace) read(_ context.Context, c core.Call) (core.Result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := decode(c.Arguments, &a); err != nil {
		return core.Result{}, err
	}
	p, err := w.resolve(a.Path, readAccess)
	if err != nil {
		return core.Result{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return core.Result{}, err
	}
	return core.Result{Output: string(b)}, nil
}
func (w *Workspace) list(ctx context.Context, c core.Call) (core.Result, error) {
	var a struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := decode(c.Arguments, &a); err != nil {
		return core.Result{}, err
	}
	p, err := w.resolve(a.Path, readAccess)
	if err != nil {
		return core.Result{}, err
	}
	var entries []string
	if !a.Recursive {
		items, err := os.ReadDir(p)
		if err != nil {
			return core.Result{}, err
		}
		for _, item := range items {
			if item.Name() == ".git" || item.Name() == ".gohermit" || isSensitive(item.Name()) {
				continue
			}
			entries = append(entries, item.Name())
		}
	} else {
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() && (d.Name() == ".git" || d.Name() == ".gohermit" || isSensitive(d.Name())) {
				return filepath.SkipDir
			}
			if isSensitive(d.Name()) {
				return nil
			}
			rel, _ := filepath.Rel(w.Root, path)
			if rel != "." {
				entries = append(entries, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return core.Result{}, err
		}
	}
	sort.Strings(entries)
	return core.Result{Output: strings.Join(entries, "\n")}, nil
}
func (w *Workspace) search(ctx context.Context, c core.Call) (core.Result, error) {
	var a struct {
		Query, Path string
		Max         int `json:"max_results"`
	}
	if err := decode(c.Arguments, &a); err != nil {
		return core.Result{}, err
	}
	if a.Query == "" {
		return core.Result{}, errors.New("query is empty")
	}
	if a.Max <= 0 || a.Max > 1000 {
		a.Max = 200
	}
	p, err := w.resolve(a.Path, readAccess)
	if err != nil {
		return core.Result{}, err
	}
	var out []string
	err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".gohermit" || isSensitive(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if isSensitive(d.Name()) {
			return nil
		}
		if len(out) >= a.Max {
			return filepath.SkipAll
		}
		b, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return nil
		}
		rel, _ := filepath.Rel(w.Root, path)
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, a.Query) {
				out = append(out, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(rel), i+1, line))
				if len(out) >= a.Max {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return core.Result{}, err
	}
	return core.Result{Output: strings.Join(out, "\n")}, nil
}
func (w *Workspace) write(_ context.Context, c core.Call) (core.Result, error) {
	var a struct{ Path, Content string }
	if err := decode(c.Arguments, &a); err != nil {
		return core.Result{}, err
	}
	p, err := w.resolve(a.Path, writeAccess)
	if err != nil {
		return core.Result{}, err
	}
	if err := atomicWrite(p, []byte(a.Content), 0644); err != nil {
		return core.Result{}, err
	}
	return core.Result{Output: fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path)}, nil
}
func (w *Workspace) patch(ctx context.Context, c core.Call) (core.Result, error) {
	var a struct{ Patch string }
	if err := decode(c.Arguments, &a); err != nil {
		return core.Result{}, err
	}
	if len(a.Patch) > 4<<20 {
		return core.Result{}, errors.New("patch exceeds 4 MiB")
	}
	if strings.Contains(a.Patch, "../") || strings.Contains(a.Patch, `..\`) {
		return core.Result{}, errors.New("patch contains traversal path")
	}
	cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "-")
	cmd.Dir = w.Root
	cmd.Stdin = strings.NewReader(a.Patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return core.Result{}, fmt.Errorf("git apply: %w: %s", err, out)
	}
	return core.Result{Output: "patch applied"}, nil
}

type limitedBuffer struct {
	buf          bytes.Buffer
	limit, total int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.total += len(p)
	n := len(p)
	if b.buf.Len() < b.limit {
		keep := min(len(p), b.limit-b.buf.Len())
		_, _ = b.buf.Write(p[:keep])
	}
	return n, nil
}
func run(ctx context.Context, dir string, allowNetwork bool, stdoutLimit, stderrLimit int, name string, args ...string) (core.Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if !allowNetwork {
		cmd.Env = append(os.Environ(), "GOPROXY=off")
	}
	stdout := &limitedBuffer{limit: stdoutLimit}
	stderr := &limitedBuffer{limit: stderrLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	res := core.Result{Stdout: stdout.buf.String(), Stderr: stderr.buf.String(), Truncated: stdout.total > stdout.buf.Len() || stderr.total > stderr.buf.Len(), OriginalSize: stdout.total + stderr.total, ReturnedSize: stdout.buf.Len() + stderr.buf.Len()}
	if err != nil {
		return res, fmt.Errorf("command failed: %w", err)
	}
	return res, nil
}
func (w *Workspace) command(name string, args ...string) func(context.Context, core.Call) (core.Result, error) {
	return func(ctx context.Context, _ core.Call) (core.Result, error) {
		return run(ctx, w.Root, true, w.maxStdout, w.maxStderr, name, args...)
	}
}
func (w *Workspace) shell(allowNetwork bool) func(context.Context, core.Call) (core.Result, error) {
	return func(ctx context.Context, c core.Call) (core.Result, error) {
		var a struct{ Command string }
		if err := decode(c.Arguments, &a); err != nil {
			return core.Result{}, err
		}
		decision := policy.ClassifyShell(a.Command)
		if decision.Risk != policy.Safe {
			return core.Result{Error: &core.Error{Code: string(decision.Risk), Message: decision.Reason}}, nil
		}
		shell, flag := "/bin/sh", "-c"
		if runtime.GOOS == "windows" {
			shell, flag = "cmd.exe", "/C"
		}
		return run(ctx, w.Root, allowNetwork, w.maxStdout, w.maxStderr, shell, flag, a.Command)
	}
}
func (w *Workspace) gitDiff(ctx context.Context, c core.Call) (core.Result, error) {
	var a struct{ Staged bool }
	if err := decode(c.Arguments, &a); err != nil {
		return core.Result{}, err
	}
	args := []string{"diff"}
	if a.Staged {
		args = append(args, "--staged")
	}
	return run(ctx, w.Root, true, w.maxStdout, w.maxStderr, "git", args...)
}
func (w *Workspace) gitLog(ctx context.Context, c core.Call) (core.Result, error) {
	var a struct{ Limit int }
	if err := decode(c.Arguments, &a); err != nil {
		return core.Result{}, err
	}
	if a.Limit <= 0 || a.Limit > 100 {
		a.Limit = 20
	}
	return run(ctx, w.Root, true, w.maxStdout, w.maxStderr, "git", "log", "--oneline", fmt.Sprintf("-%d", a.Limit))
}
func (w *Workspace) testRun(allowNetwork bool) func(context.Context, core.Call) (core.Result, error) {
	return func(ctx context.Context, c core.Call) (core.Result, error) {
		var a struct{ Package string }
		if err := decode(c.Arguments, &a); err != nil {
			return core.Result{}, err
		}
		if a.Package == "" {
			a.Package = "./..."
		}
		if filepath.IsAbs(a.Package) || strings.Contains(a.Package, "..") {
			return core.Result{}, errors.New("invalid package path")
		}
		return run(ctx, w.Root, allowNetwork, w.maxStdout, w.maxStderr, "go", "test", a.Package)
	}
}

var _ io.Writer = (*limitedBuffer)(nil)
