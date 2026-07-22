package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	core "github.com/Rj455555/GoHermit/internal/tool"
)

func newTestWorkspace(t *testing.T) (*Workspace, *core.Registry) {
	t.Helper()
	root := t.TempDir()
	w, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	r := core.NewRegistry()
	if err = RegisterAll(r, w, time.Second, 1024, 1024, false); err != nil {
		t.Fatal(err)
	}
	return w, r
}
func call(t *testing.T, r *core.Registry, name, args string) core.Result {
	t.Helper()
	res, _ := (core.Executor{Registry: r}).Execute(context.Background(), core.Call{Name: name, Arguments: json.RawMessage(args)})
	return res
}
func TestWorkspaceRejectsTraversalAbsoluteAndWindowsPaths(t *testing.T) {
	_, r := newTestWorkspace(t)
	for _, path := range []string{"../secret", "/etc/passwd", `C:\\Users\\secret`} {
		res := call(t, r, "filesystem.read", `{"path":`+strconvQuote(path)+`}`)
		if res.Error == nil {
			t.Errorf("path %q was allowed: %+v", path, res)
		}
	}
}
func TestWorkspaceRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary")
	}
	w, r := newTestWorkspace(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(w.Root, "escape")); err != nil {
		t.Fatal(err)
	}
	res := call(t, r, "filesystem.read", `{"path":"escape/secret"}`)
	if res.Error == nil {
		t.Fatalf("symlink escape allowed: %+v", res)
	}
}
func TestWriteNestedAndGitNonRepository(t *testing.T) {
	w, r := newTestWorkspace(t)
	res := call(t, r, "filesystem.write", `{"path":"a/b/c.txt","content":"ok"}`)
	if res.Error != nil {
		t.Fatalf("write failed: %+v", res)
	}
	b, err := os.ReadFile(filepath.Join(w.Root, "a", "b", "c.txt"))
	if err != nil || string(b) != "ok" {
		t.Fatalf("file=%q err=%v", b, err)
	}
	res = call(t, r, "git.status", `{}`)
	if res.Error == nil || !strings.Contains(res.Error.Message, "exit status") {
		t.Fatalf("non-repository result=%+v", res)
	}
}
func TestShellPermissionRequired(t *testing.T) {
	_, r := newTestWorkspace(t)
	res := call(t, r, "shell.execute", `{"command":"npm install"}`)
	if res.Error == nil || res.Error.Code != core.CodeApprovalRequired {
		t.Fatalf("result=%+v", res)
	}
	res = call(t, r, "shell.execute", `{"command":"rm -rf /"}`)
	if res.Error == nil || res.Error.Code != "blocked" || res.Approval != nil {
		t.Fatalf("result=%+v", res)
	}
}

func TestSearchAndListSkipSensitiveFiles(t *testing.T) {
	w, r := newTestWorkspace(t)
	if err := os.WriteFile(filepath.Join(w.Root, "visible.txt"), []byte("needle"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Root, ".env"), []byte("needle=secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(w.Root, ".gohermit"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Root, ".gohermit", "session.json"), []byte("needle"), 0600); err != nil {
		t.Fatal(err)
	}
	search := call(t, r, "filesystem.search", `{"query":"needle"}`)
	if search.Error != nil {
		t.Fatalf("search failed: %+v", search)
	}
	if !strings.Contains(search.Output, "visible.txt") || strings.Contains(search.Output, ".env") || strings.Contains(search.Output, ".gohermit") {
		t.Fatalf("search output=%q", search.Output)
	}
	list := call(t, r, "filesystem.list", `{"recursive":true}`)
	if list.Error != nil {
		t.Fatalf("list failed: %+v", list)
	}
	if strings.Contains(list.Output, ".env") || strings.Contains(list.Output, ".gohermit") {
		t.Fatalf("list output=%q", list.Output)
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

// TestShellConfirmationRequiredCarriesBoundedApprovalHint: a parked shell
// call proposes the bounded request scope — workspace-relative path tokens,
// deduped and capped — and falls back to a placeholder when the command
// names no path.
func TestShellConfirmationRequiredCarriesBoundedApprovalHint(t *testing.T) {
	_, r := newTestWorkspace(t)
	res := call(t, r, "shell.execute", `{"command":"touch reports/out.txt reports/out.txt plain"}`)
	if res.Error == nil || res.Error.Code != core.CodeApprovalRequired || res.Approval == nil {
		t.Fatalf("result=%+v", res)
	}
	if len(res.Approval.Paths) != 1 || res.Approval.Paths[0] != "reports/out.txt" {
		t.Fatalf("paths=%v", res.Approval.Paths)
	}
	if res.Approval.Summary != "touch reports/out.txt reports/out.txt plain" {
		t.Fatalf("summary=%q", res.Approval.Summary)
	}
	res = call(t, r, "shell.execute", `{"command":"mkdir newdir"}`)
	if res.Approval == nil || len(res.Approval.Paths) != 1 || res.Approval.Paths[0] != "<command>" {
		t.Fatalf("placeholder paths=%+v", res.Approval)
	}
}

// TestShellApprovedOverrideExecutesSingleInvocation: the executor's approved
// re-execution skips classification once; a plain execute of the same call
// parks again.
func TestShellApprovedOverrideExecutesSingleInvocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("touch is not available on windows")
	}
	w, r := newTestWorkspace(t)
	executor := core.Executor{Registry: r}
	shellCall := core.Call{Name: "shell.execute", Arguments: json.RawMessage(`{"command":"touch approved-proof.txt"}`)}
	res, _ := executor.Execute(context.Background(), shellCall)
	if res.Error == nil || res.Error.Code != core.CodeApprovalRequired {
		t.Fatalf("unapproved result=%+v", res)
	}
	if _, err := os.Stat(filepath.Join(w.Root, "approved-proof.txt")); !os.IsNotExist(err) {
		t.Fatalf("parked call produced a side effect: %v", err)
	}
	res, _ = executor.ExecuteApproved(context.Background(), shellCall)
	if res.Error != nil {
		t.Fatalf("approved result=%+v", res)
	}
	if _, err := os.Stat(filepath.Join(w.Root, "approved-proof.txt")); err != nil {
		t.Fatalf("approved call did not run: %v", err)
	}
	res, _ = executor.Execute(context.Background(), shellCall)
	if res.Error == nil || res.Error.Code != core.CodeApprovalRequired {
		t.Fatalf("override leaked beyond one invocation: %+v", res)
	}
}
