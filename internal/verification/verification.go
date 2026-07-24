// Package verification runs a loop's deterministic verification recipe:
// bounded, policy-gated argv commands executed without a shell and without a
// model. Every command passes the exact same policy allowlist as interactive
// shell commands; anything not explicitly Safe is denied and never executed.
package verification

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Rj455555/GoHermit/internal/loop"
	"github.com/Rj455555/GoHermit/internal/policy"
)

const (
	// MaxOutputBytes bounds one captured stream per check; the kept evidence
	// text is bounded by the same size after both streams are combined.
	MaxOutputBytes = 8 << 10
	// notRun marks checks that never executed, so no real process exit code
	// exists for them.
	notRun = -1
)

// CheckResult is the bounded evidence of one recipe check.
type CheckResult struct {
	ID         string   `json:"id"`
	Command    []string `json:"command"`
	Required   bool     `json:"required"`
	Passed     bool     `json:"passed"`
	ExitCode   int      `json:"exit_code"`
	DurationMS int64    `json:"duration_ms"`
	// PolicyDenied marks a check that failed policy classification and was
	// never executed; Passed is always false for it.
	PolicyDenied bool   `json:"policy_denied,omitempty"`
	Output       string `json:"output,omitempty"`
}

// CommandString renders an argv for display and evidence matching. It is
// never executed: execution always uses the argv array directly.
func CommandString(argv []string) string {
	return strings.Join(argv, " ")
}

// RunChecks executes each check in order inside workspace and returns one
// result per check; zero checks return zero results. A check whose argv is
// empty or not policy-Safe is recorded denied and never executed.
func RunChecks(ctx context.Context, workspace string, checks []loop.RecipeCheck) []CheckResult {
	results := make([]CheckResult, 0, len(checks))
	for _, check := range checks {
		results = append(results, runCheck(ctx, workspace, check))
	}
	return results
}

func runCheck(ctx context.Context, workspace string, check loop.RecipeCheck) CheckResult {
	result := CheckResult{ID: check.ID, Command: append([]string(nil), check.Command...), Required: check.Required, ExitCode: notRun}
	if decision := policy.ClassifyArgv(check.Command); decision.Risk != policy.Safe {
		result.PolicyDenied = true
		result.Output = "policy denied: " + decision.Reason
		return result
	}
	// Defense in depth: recipe validation already bounds the timeout, an
	// out-of-range value is clamped to the contract ceiling.
	timeout := time.Duration(check.TimeoutSeconds) * time.Second
	if check.TimeoutSeconds < 1 || check.TimeoutSeconds > loop.MaxCheckTimeoutSeconds {
		timeout = time.Duration(loop.MaxCheckTimeoutSeconds) * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()
	cmd := exec.CommandContext(checkCtx, check.Command[0], check.Command[1:]...)
	cmd.Dir = workspace
	// Same bounded discipline as the builtin runner: checks get no network.
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	stdout := &limitedBuffer{limit: MaxOutputBytes}
	stderr := &limitedBuffer{limit: MaxOutputBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	result.DurationMS = time.Since(started).Milliseconds()
	result.Passed = err == nil
	if result.Passed {
		result.ExitCode = 0
	} else if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	}
	var output strings.Builder
	output.Write(stdout.buf.Bytes())
	if stderr.buf.Len() > 0 {
		if output.Len() > 0 {
			output.WriteByte('\n')
		}
		output.Write(stderr.buf.Bytes())
	}
	// The markers reserve room inside the bound so they can never be
	// clipped away by a check that filled the buffer exactly.
	body := clip(output.String(), MaxOutputBytes-len("\n[output truncated]\n[check timed out]"))
	var marked strings.Builder
	marked.WriteString(body)
	if stdout.truncated() || stderr.truncated() {
		if marked.Len() > 0 {
			marked.WriteByte('\n')
		}
		marked.WriteString("[output truncated]")
	}
	if checkCtx.Err() == context.DeadlineExceeded {
		if marked.Len() > 0 {
			marked.WriteByte('\n')
		}
		marked.WriteString("[check timed out]")
	}
	result.Output = marked.String()
	return result
}

func clip(value string, limit int) string {
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

// limitedBuffer keeps at most limit bytes while counting everything written,
// mirroring the builtin runner's bounded capture.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
	total int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.total += len(p)
	if b.buf.Len() < b.limit {
		keep := min(len(p), b.limit-b.buf.Len())
		_, _ = b.buf.Write(p[:keep])
	}
	return len(p), nil
}

func (b *limitedBuffer) truncated() bool { return b.total > b.buf.Len() }
