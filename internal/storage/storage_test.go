package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	input := "Authorization: Bearer abc123 password=hunter2\n-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----"
	out := Redact(input)
	for _, secret := range []string{"abc123", "hunter2", "\nsecret\n"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret remained in %q", out)
		}
	}
}
func TestRotatingLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	l, err := OpenRotatingLog(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.WriteLine("123456789012345"); err != nil {
		t.Fatal(err)
	}
	if err = l.Flush(); err != nil {
		t.Fatal(err)
	}
	if err = l.WriteLine("abcdefghijklmno"); err != nil {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated log: %v", err)
	}
}
