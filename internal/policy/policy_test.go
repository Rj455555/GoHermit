package policy

import "testing"

func TestClassifyShell(t *testing.T) {
	cases := map[string]Risk{"go test ./...": Safe, "git status --short": Safe, "npm install": ConfirmationRequired, "rm -rf /": Blocked, "git diff /etc/passwd": Blocked, "go test $(whoami)": Blocked}
	for command, want := range cases {
		if got := ClassifyShell(command).Risk; got != want {
			t.Errorf("%q: got %s want %s", command, got, want)
		}
	}
}
