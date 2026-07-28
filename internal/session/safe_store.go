package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rj455555/GoHermit/internal/storage"
)

const maxSessionStoreFileBytes = 8 << 20

var sessionStoreFiles = []string{
	"session.json",
	"commit.json",
	"summary.md",
	"events.jsonl",
	"messages.jsonl",
}

func canonicalWorkspaceRoot(workspace string) (string, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace real path: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect workspace real path: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workspace is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func validateStoreDirectory(directory string) error {
	if directory == "" || filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return errors.New("session directory must be a canonical relative path")
	}
	for _, part := range strings.Split(filepath.ToSlash(directory), "/") {
		if part == "" || part == ".." || strings.ContainsAny(part, "%\\\r\n") {
			return errors.New("session directory contains an unsafe path component")
		}
	}
	return nil
}

func ensureSessionContained(root, target string) error {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("session store path escapes workspace")
	}
	return nil
}

// ensureDirectoryChain validates every existing component from base through
// target without following symlinks. Missing components are either created
// one at a time or reported as a safe missing path.
func ensureDirectoryChain(base, target string, create bool) (bool, error) {
	if err := ensureSessionContained(base, target); err != nil {
		return false, err
	}
	baseInfo, err := os.Lstat(base)
	if err != nil {
		return false, err
	}
	if baseInfo.Mode()&os.ModeSymlink != 0 || !baseInfo.IsDir() {
		return false, errors.New("session store workspace root is unsafe")
	}
	relative, err := filepath.Rel(base, target)
	if err != nil {
		return false, err
	}
	if relative == "." {
		return true, nil
	}
	current := base
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." {
			return false, errors.New("session store directory component is invalid")
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if !create {
				return false, nil
			}
			if mkdirErr := os.Mkdir(current, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return false, mkdirErr
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return false, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("session store parent directory must not be a symlink")
		}
		if !info.IsDir() {
			return false, errors.New("session store parent path is not a directory")
		}
	}
	return true, nil
}

func (s *Store) sessionsDir(create bool) (string, bool, error) {
	if s == nil {
		return "", false, errors.New("Session Store is unavailable")
	}
	if _, err := ensureDirectoryChain(s.workspace, s.root, create); err != nil {
		return "", false, err
	}
	path := filepath.Join(s.root, "sessions")
	exists, err := ensureDirectoryChain(s.workspace, path, create)
	return path, exists, err
}

func (s *Store) safeSessionDir(id string, create bool) (string, bool, error) {
	path, err := s.sessionDir(id)
	if err != nil {
		return "", false, err
	}
	sessions, exists, err := s.sessionsDir(create)
	if err != nil {
		return "", false, err
	}
	if !exists && !create {
		return path, false, nil
	}
	if err = ensureSessionContained(sessions, path); err != nil {
		return "", false, err
	}
	exists, err = ensureDirectoryChain(s.workspace, path, create)
	return path, exists, err
}

func validateSessionStoreFilename(name string) error {
	for _, allowed := range sessionStoreFiles {
		if name == allowed {
			return nil
		}
	}
	return errors.New("unsupported Session Store filename")
}

func (s *Store) safeSessionFile(id, name string, createParent bool) (string, os.FileInfo, bool, error) {
	if err := validateSessionStoreFilename(name); err != nil {
		return "", nil, false, err
	}
	dir, exists, err := s.safeSessionDir(id, createParent)
	if err != nil {
		return "", nil, false, err
	}
	path := filepath.Join(dir, name)
	if !exists && !createParent {
		return path, nil, false, nil
	}
	if err = ensureSessionContained(s.root, path); err != nil {
		return "", nil, false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", nil, false, errors.New("Session Store file must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return "", nil, false, errors.New("Session Store file is not regular")
	}
	return path, info, true, nil
}

// CheckTarget validates every file that Save/Load/Recover may touch and
// distinguishes a safe missing checkpoint from an unsafe path.
func (s *Store) CheckTarget(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	exists := false
	for _, name := range sessionStoreFiles {
		_, _, present, err := s.safeSessionFile(id, name, false)
		if err != nil {
			return false, err
		}
		if name == "session.json" {
			exists = present
		}
	}
	return exists, nil
}

func (s *Store) readSessionFile(id, name string, maximum int64) ([]byte, error) {
	path, expected, exists, err := s.safeSessionFile(id, name, false)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, os.ErrNotExist
	}
	if maximum > 0 && expected.Size() > maximum {
		return nil, errors.New("Session Store file exceeds size limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() ||
		!os.SameFile(expected, opened) || !os.SameFile(current, opened) {
		return nil, errors.New("Session Store file changed during safe open")
	}
	reader := io.Reader(file)
	if maximum > 0 {
		reader = io.LimitReader(file, maximum+1)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maximum > 0 && int64(len(raw)) > maximum {
		return nil, errors.New("Session Store file exceeds size limit")
	}
	return raw, nil
}

func (s *Store) openSessionFile(id, name string) (*os.File, error) {
	path, expected, exists, err := s.safeSessionFile(id, name, false)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, os.ErrNotExist
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	current, lstatErr := os.Lstat(path)
	if statErr != nil || lstatErr != nil || current.Mode()&os.ModeSymlink != 0 ||
		!current.Mode().IsRegular() || !os.SameFile(expected, opened) || !os.SameFile(current, opened) {
		_ = file.Close()
		return nil, errors.New("Session Store file changed during safe open")
	}
	return file, nil
}

func (s *Store) openSessionFileAppend(id, name string) (*os.File, error) {
	path, expected, exists, err := s.safeSessionFile(id, name, true)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	current, lstatErr := os.Lstat(path)
	if statErr != nil || lstatErr != nil || current.Mode()&os.ModeSymlink != 0 ||
		!current.Mode().IsRegular() || (exists && !os.SameFile(expected, opened)) ||
		!os.SameFile(current, opened) {
		_ = file.Close()
		return nil, errors.New("Session Store append target changed during safe open")
	}
	return file, nil
}

func (s *Store) atomicWriteSessionFile(id, name string, data []byte) error {
	path, _, _, err := s.safeSessionFile(id, name, true)
	if err != nil {
		return err
	}
	if err = storage.AtomicWrite(path, data, 0o600); err != nil {
		return err
	}
	_, written, exists, err := s.safeSessionFile(id, name, false)
	if err != nil {
		return err
	}
	if !exists || written.Mode().Perm() != 0o600 {
		return errors.New("Session Store atomic write produced an unsafe file")
	}
	return nil
}

func (s *Store) removeSessionFile(id, name string) error {
	path, _, exists, err := s.safeSessionFile(id, name, false)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	return nil
}
