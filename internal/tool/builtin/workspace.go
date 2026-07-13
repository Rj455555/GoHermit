package builtin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type access int

const (
	readAccess access = iota
	writeAccess
)

var drivePath = regexp.MustCompile(`(?i)^[a-z]:[\\/]`)

type Workspace struct {
	Root      string
	maxStdout int
	maxStderr int
}

func NewWorkspace(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return nil, errors.New("workspace is not a directory")
	}
	return &Workspace{Root: real}, nil
}

func (w *Workspace) resolve(path string, mode access) (string, error) {
	if path == "" {
		path = "."
	}
	if filepath.IsAbs(path) || drivePath.MatchString(path) {
		return "", errors.New("absolute paths are not allowed")
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal is not allowed")
	}
	parts := strings.Split(filepath.ToSlash(clean), "/")
	if len(parts) > 0 && (parts[0] == ".git" || parts[0] == ".gohermit") {
		return "", errors.New("direct access to runtime or Git internals is not allowed")
	}
	for _, part := range parts {
		if isSensitive(part) {
			return "", errors.New("access to credential-like files is not allowed")
		}
	}
	target := filepath.Join(w.Root, clean)
	check := target
	if mode == writeAccess {
		for {
			if _, err := os.Lstat(check); err == nil {
				break
			} else if !os.IsNotExist(err) {
				return "", fmt.Errorf("inspect path: %w", err)
			}
			parent := filepath.Dir(check)
			if parent == check {
				return "", errors.New("no existing workspace ancestor")
			}
			check = parent
		}
	}
	real, err := filepath.EvalSymlinks(check)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	rel, err := filepath.Rel(w.Root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("symlink escapes workspace")
	}
	return target, nil
}
func isSensitive(name string) bool {
	n := strings.ToLower(name)
	if n == ".ssh" || n == ".aws" || n == ".gnupg" || n == ".env" || n == "id_rsa" || n == "id_ed25519" || strings.HasSuffix(n, ".pem") || strings.HasSuffix(n, ".key") {
		return true
	}
	return false
}

func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".hermit-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if err = f.Chmod(perm); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}
