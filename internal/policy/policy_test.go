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

func TestClassifyArgv(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want Risk
	}{
		{"go test is safe", []string{"go", "test", "./..."}, Safe},
		{"go vet is safe", []string{"go", "vet", "./..."}, Safe},
		{"git status is safe", []string{"git", "status", "--short"}, Safe},
		{"pwd is safe", []string{"pwd"}, Safe},
		{"unknown program is denied", []string{"python3", "-c", "print(1)"}, ConfirmationRequired},
		{"allowlisted program with unknown subcommand is denied", []string{"go", "run", "main.go"}, ConfirmationRequired},
		{"empty argv is blocked", nil, Blocked},
		{"blank program is blocked", []string{"  "}, Blocked},
		{"rm -rf is blocked", []string{"rm", "-rf", "build"}, Blocked},
		{"rm -fr is blocked", []string{"rm", "-fr", "build"}, Blocked},
		{"shutdown is blocked", []string{"shutdown"}, Blocked},
		{"kubectl delete is blocked", []string{"kubectl", "delete", "pod", "x"}, Blocked},
		{"absolute path argument is blocked", []string{"go", "test", "/etc/passwd"}, Blocked},
		{"parent traversal argument is blocked", []string{"go", "test", "../x"}, Blocked},
		// Shell metacharacters inside an argv entry are literal data, never
		// operators: classification judges program and paths only.
		{"metacharacter argument is literal", []string{"go", "version", "$(touch pwned);rm -rf x"}, Safe},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyArgv(tc.argv).Risk; got != tc.want {
				t.Fatalf("ClassifyArgv(%v) = %s, want %s", tc.argv, got, tc.want)
			}
		})
	}
}
