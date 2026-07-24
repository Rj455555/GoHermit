// Package policy classifies shell operations before execution.
package policy

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type Risk string

const (
	Safe                 Risk = "safe"
	ConfirmationRequired Risk = "confirmation_required"
	Blocked              Risk = "blocked"
)

type Decision struct {
	Risk   Risk   `json:"risk"`
	Reason string `json:"reason"`
}

var windowsDrive = regexp.MustCompile(`(?i)^[a-z]:[\\/]`)

// destructiveMarkers is the single deny table both classifiers share.
var destructiveMarkers = []string{"rm -rf", "mkfs", "diskutil erase", "shutdown", "reboot", "launchctl", "kubectl delete", "terraform destroy"}

// ClassifyShell permits a deliberately small, non-mutating command grammar.
func ClassifyShell(command string) Decision {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return Decision{Blocked, "empty command"}
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range destructiveMarkers {
		if strings.Contains(lower, marker) {
			return Decision{Blocked, "destructive operation"}
		}
	}
	if strings.ContainsAny(trimmed, ";|&><`$\n\r()") {
		return Decision{Blocked, "shell operators and expansion are not allowed"}
	}
	fields := strings.Fields(trimmed)
	if pathEscape(fields[1:]) {
		return Decision{Blocked, "path may escape workspace"}
	}
	return classifyAllowlist(fields)
}

// ClassifyArgv classifies an argv array with the exact same allowlist and
// deny table as ClassifyShell. No shell string is ever built: arguments are
// literal, so shell metacharacters inside an argument are inert data, never
// operators. Anything not explicitly Safe is denied — deterministic check
// execution has no approval path.
func ClassifyArgv(argv []string) Decision {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Decision{Blocked, "empty command"}
	}
	if destructiveArgv(argv) {
		return Decision{Blocked, "destructive operation"}
	}
	if pathEscape(argv[1:]) {
		return Decision{Blocked, "path may escape workspace"}
	}
	return classifyAllowlist(argv)
}

// classifyAllowlist is the shared program allowlist for both forms.
func classifyAllowlist(fields []string) Decision {
	switch commandBase(fields[0]) {
	case "go":
		if len(fields) > 1 && contains([]string{"test", "vet", "build", "list", "version", "env"}, fields[1]) {
			return Decision{Safe, "allowlisted Go inspection/build command"}
		}
	case "git":
		if len(fields) > 1 && contains([]string{"status", "diff", "log", "show", "rev-parse", "ls-files"}, fields[1]) {
			return Decision{Safe, "allowlisted Git read command"}
		}
	case "pwd":
		return Decision{Safe, "workspace location"}
	}
	return Decision{ConfirmationRequired, "command is not in the non-interactive allowlist"}
}

// commandBase normalizes an invoked program path to its base name.
func commandBase(path string) string {
	name := filepath.Base(path)
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}
	return name
}

// pathEscape reports whether any argument may escape the workspace.
func pathEscape(args []string) bool {
	for _, arg := range args {
		raw := strings.Trim(arg, `"'`)
		clean := filepath.Clean(raw)
		if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) || filepath.IsAbs(clean) || windowsDrive.MatchString(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, `..\`) {
			return true
		}
	}
	return false
}

// destructiveArgv mirrors destructiveMarkers for argv form: the marker's
// program must be the invoked command and each of its remaining tokens must
// appear among the arguments. A short-flag token matches any argument
// carrying all of its letters, so "-rf" and "-fr" are equivalent.
func destructiveArgv(argv []string) bool {
	name := commandBase(argv[0])
	args := argv[1:]
	hasToken := func(token string) bool {
		for _, arg := range args {
			if arg == token {
				return true
			}
			if strings.HasPrefix(token, "-") && !strings.HasPrefix(token, "--") && strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
				letters := true
				for _, r := range token[1:] {
					if !strings.ContainsRune(arg[1:], r) {
						letters = false
						break
					}
				}
				if letters {
					return true
				}
			}
		}
		return false
	}
	for _, marker := range destructiveMarkers {
		fields := strings.Fields(marker)
		if fields[0] != name {
			continue
		}
		matched := true
		for _, token := range fields[1:] {
			if !hasToken(token) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
