// Package storage provides atomic files, redaction, and bounded logs.
package storage

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gohermit-*")
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

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer\s+)?)[^\s,;]+`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|token|password|cookie|secret)\s*[:=]\s*)[^\s,;]+`),
	regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |EC )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |EC )?PRIVATE KEY-----`),
}

func Redact(s string) string {
	for _, p := range secretPatterns {
		s = p.ReplaceAllString(s, "${1}[REDACTED]")
	}
	return s
}

type RotatingLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	writer   *bufio.Writer
}

func OpenRotatingLog(path string, maxBytes int64) (*RotatingLog, error) {
	if maxBytes <= 0 {
		return nil, errors.New("max log size must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	l := &RotatingLog{path: path, maxBytes: maxBytes}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}
func (l *RotatingLog) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	l.file = f
	l.writer = bufio.NewWriterSize(f, 32<<10)
	return nil
}
func (l *RotatingLog) WriteLine(line string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	line = Redact(strings.TrimSuffix(line, "\n")) + "\n"
	info, err := l.file.Stat()
	if err != nil {
		return err
	}
	if info.Size()+int64(len(line)) > l.maxBytes {
		if err = l.writer.Flush(); err != nil {
			return err
		}
		if err = l.file.Close(); err != nil {
			return err
		}
		_ = os.Remove(l.path + ".1")
		if err = os.Rename(l.path, l.path+".1"); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate log: %w", err)
		}
		if err = l.open(); err != nil {
			return err
		}
	}
	_, err = l.writer.WriteString(line)
	return err
}
func (l *RotatingLog) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.writer.Flush(); err != nil {
		return err
	}
	return l.file.Sync()
}
func (l *RotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.writer.Flush()
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
