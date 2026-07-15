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

// ClassifyShell permits a deliberately small, non-mutating command grammar.
func ClassifyShell(command string) Decision {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return Decision{Blocked, "empty command"}
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{"rm -rf", "mkfs", "diskutil erase", "shutdown", "reboot", "launchctl", "kubectl delete", "terraform destroy"} {
		if strings.Contains(lower, marker) {
			return Decision{Blocked, "destructive operation"}
		}
	}
	if strings.ContainsAny(trimmed, ";|&><`$\n\r()") {
		return Decision{Blocked, "shell operators and expansion are not allowed"}
	}
	fields := strings.Fields(trimmed)
	for _, arg := range fields[1:] {
		raw := strings.Trim(arg, `"'`)
		clean := filepath.Clean(raw)
		if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) || filepath.IsAbs(clean) || windowsDrive.MatchString(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, `..\`) {
			return Decision{Blocked, "path may escape workspace"}
		}
	}
	commandName := filepath.Base(fields[0])
	if runtime.GOOS == "windows" {
		commandName = strings.TrimSuffix(strings.ToLower(commandName), ".exe")
	}
	switch commandName {
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

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
