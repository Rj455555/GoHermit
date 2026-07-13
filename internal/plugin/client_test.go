package plugin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	core "github.com/Rj455555/GoHermit/internal/tool"
)

func python(t *testing.T) string {
	t.Helper()
	name := "python3"
	if runtime.GOOS == "windows" {
		name = "python"
	}
	return name
}
func TestPythonEchoLifecycle(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "examples", "plugins", "python-echo", "plugin.py")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p, err := Start(ctx, Config{Command: []string{python(t), path}, Directory: root, DefaultTimeout: time.Second, MaxMessageBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Health(ctx); err != nil {
		t.Fatal(err)
	}
	tools, err := p.Tools(ctx)
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools=%+v err=%v", tools, err)
	}
	result, err := p.Execute(ctx, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil || result.Output != "hello" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	registry := core.NewRegistry()
	if err = RegisterTools(ctx, registry, "python", p); err != nil {
		t.Fatal(err)
	}
	registered, _ := registry.Get("plugin.python.echo")
	toolResult, err := registered.Execute(ctx, core.Call{Arguments: json.RawMessage(`{"text":"registered"}`)})
	if err != nil || toolResult.Output != "registered" {
		t.Fatalf("registered result=%+v err=%v", toolResult, err)
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err = p.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}

func TestNodeEchoLifecycle(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	path := filepath.Join(root, "examples", "plugins", "node-echo", "plugin.js")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p, err := Start(ctx, Config{Command: []string{node, path}, Directory: root, DefaultTimeout: time.Second, MaxMessageBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Execute(ctx, "echo", json.RawMessage(`{"text":"node"}`))
	if err != nil || result.Output != "node" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err = p.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidJSONAndCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Start(ctx, Config{Command: []string{python(t), "-c", "import time; print('not-json', flush=True); time.sleep(.1)"}, Directory: t.TempDir(), DefaultTimeout: time.Second, MaxMessageBytes: 4096})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("err=%v", err)
	}
	_, err = Start(ctx, Config{Command: []string{python(t), "-c", "import sys; sys.exit(3)"}, Directory: t.TempDir(), DefaultTimeout: time.Second, MaxMessageBytes: 4096})
	if err == nil {
		t.Fatal("expected crash error")
	}
}
func TestPluginTimeoutCancellation(t *testing.T) {
	script := `import json,sys,time
for line in sys.stdin:
 r=json.loads(line); m=r.get("method"); i=r.get("id")
 if i is None: continue
 if m=="plugin.initialize": out={"protocol_version":"1.0","name":"slow","version":"1","capabilities":{"tools":True,"cancellation":True,"health":True,"max_concurrency":1}}
 elif m=="tools.execute": time.sleep(1); out={"output":"late","is_error":False}
 elif m=="plugin.shutdown": out={}
 else: out={"tools":[]}
 print(json.dumps({"jsonrpc":"2.0","id":i,"result":out}),flush=True)
`
	path := filepath.Join(t.TempDir(), "slow.py")
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p, err := Start(ctx, Config{Command: []string{python(t), path}, Directory: t.TempDir(), DefaultTimeout: time.Second, MaxMessageBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	callCtx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	_, err = p.Execute(callCtx, "slow", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected timeout")
	}
	shutdownCtx, shutdown := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer shutdown()
	_ = p.Shutdown(shutdownCtx)
}
