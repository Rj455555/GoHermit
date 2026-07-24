package verification

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
)

// stubProgram installs an executable script under bin/name and prepends bin
// to PATH so recipe argv resolves to the stub.
func stubProgram(t *testing.T, name, script string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func check(id string, argv []string, timeout int) loop.RecipeCheck {
	return loop.RecipeCheck{ID: id, Command: argv, Required: true, TimeoutSeconds: timeout}
}

func TestRunChecksZeroChecks(t *testing.T) {
	if results := RunChecks(context.Background(), t.TempDir(), nil); len(results) != 0 {
		t.Fatalf("results=%+v", results)
	}
}

func TestRunChecksPassing(t *testing.T) {
	stubProgram(t, "go", "#!/bin/sh\necho vet-ok\nexit 0\n")
	workspace := t.TempDir()
	results := RunChecks(context.Background(), workspace, []loop.RecipeCheck{check("vet", []string{"go", "vet", "./..."}, 30)})
	if len(results) != 1 {
		t.Fatalf("results=%+v", results)
	}
	result := results[0]
	if !result.Passed || result.ExitCode != 0 || result.PolicyDenied || result.ID != "vet" || !result.Required {
		t.Fatalf("result=%+v", result)
	}
	if result.Command[0] != "go" || result.Command[2] != "./..." {
		t.Fatalf("command=%v", result.Command)
	}
	if !strings.Contains(result.Output, "vet-ok") {
		t.Fatalf("output=%q", result.Output)
	}
}

func TestRunChecksFailing(t *testing.T) {
	stubProgram(t, "go", "#!/bin/sh\necho broken >&2\nexit 3\n")
	results := RunChecks(context.Background(), t.TempDir(), []loop.RecipeCheck{check("test", []string{"go", "test", "./..."}, 30)})
	if len(results) != 1 {
		t.Fatalf("results=%+v", results)
	}
	result := results[0]
	if result.Passed || result.ExitCode != 3 || result.PolicyDenied {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Output, "broken") {
		t.Fatalf("output=%q", result.Output)
	}
}

func TestRunChecksTimeout(t *testing.T) {
	stubProgram(t, "go", "#!/bin/sh\nexec sleep 5\n")
	started := time.Now()
	results := RunChecks(context.Background(), t.TempDir(), []loop.RecipeCheck{check("slow", []string{"go", "test", "./..."}, 1)})
	if len(results) != 1 {
		t.Fatalf("results=%+v", results)
	}
	result := results[0]
	if result.Passed || result.PolicyDenied {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Output, "[check timed out]") {
		t.Fatalf("output=%q", result.Output)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("check was not bounded by its timeout: %s", elapsed)
	}
	if result.DurationMS < 900 {
		t.Fatalf("duration_ms=%d", result.DurationMS)
	}
}

func TestRunChecksTruncatesOutput(t *testing.T) {
	stubProgram(t, "go", "#!/bin/sh\nyes A | head -c 100000\nexit 0\n")
	results := RunChecks(context.Background(), t.TempDir(), []loop.RecipeCheck{check("loud", []string{"go", "version"}, 30)})
	if len(results) != 1 {
		t.Fatalf("results=%+v", results)
	}
	result := results[0]
	if !result.Passed {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Output) > MaxOutputBytes {
		t.Fatalf("output escaped its bound: %d > %d", len(result.Output), MaxOutputBytes)
	}
	if !strings.Contains(result.Output, "[output truncated]") {
		t.Fatalf("output=%q", result.Output[:100])
	}
}

func TestRunChecksPolicyDeniedNeverExecutes(t *testing.T) {
	workspace := t.TempDir()
	marker := filepath.Join(workspace, "executed")
	stubProgram(t, "python3", "#!/bin/sh\ntouch \""+marker+"\"\nexit 0\n")
	t.Setenv("MARKER", marker)
	cases := []struct {
		name string
		argv []string
	}{
		{"unknown program", []string{"python3", "run.py"}},
		{"destructive argv", []string{"rm", "-rf", "build"}},
		{"empty argv", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := RunChecks(context.Background(), workspace, []loop.RecipeCheck{check("denied", tc.argv, 30)})
			if len(results) != 1 {
				t.Fatalf("results=%+v", results)
			}
			result := results[0]
			if result.Passed || !result.PolicyDenied || !strings.Contains(result.Output, "policy denied") {
				t.Fatalf("result=%+v", result)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("denied check executed: %v", err)
			}
		})
	}
}

// TestRunChecksArgvNeverShellJoined proves an argument carrying shell
// metacharacters reaches the program literally and is never interpreted.
func TestRunChecksArgvNeverShellJoined(t *testing.T) {
	workspace := t.TempDir()
	argvFile := filepath.Join(t.TempDir(), "argv")
	stubProgram(t, "go", "#!/bin/sh\nfor arg in \"$@\"; do printf '%s\\n' \"$arg\"; done > \""+argvFile+"\"\nexit 0\n")
	payload := "$(touch pwned);`id`"
	results := RunChecks(context.Background(), workspace, []loop.RecipeCheck{check("literal", []string{"go", "version", payload}, 30)})
	if len(results) != 1 || !results[0].Passed {
		t.Fatalf("results=%+v", results)
	}
	data, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || lines[0] != "version" || lines[1] != payload {
		t.Fatalf("argv=%q", data)
	}
	if _, err := os.Stat(filepath.Join(workspace, "pwned")); !os.IsNotExist(err) {
		t.Fatal("shell metacharacters were interpreted")
	}
}

func TestCommandString(t *testing.T) {
	if got := CommandString([]string{"go", "test", "./..."}); got != "go test ./..." {
		t.Fatalf("got %q", got)
	}
}
