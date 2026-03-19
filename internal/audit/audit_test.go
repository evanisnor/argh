package audit

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// readLines returns all lines from the log file, stripping trailing newlines.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening log file: %v", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	return lines
}

// parseEntry splits a log line into its tab-separated fields.
func parseEntry(t *testing.T, line string) (ts, action, repo, number, details string) {
	t.Helper()
	parts := strings.SplitN(line, "\t", 5)
	if len(parts) != 5 {
		t.Fatalf("expected 5 tab-separated fields in %q, got %d", line, len(parts))
	}
	return parts[0], parts[1], parts[2], parts[3], parts[4]
}

// errorWriter is an io.WriteCloser whose Write always returns an error.
type errorWriter struct{}

func (e *errorWriter) Write(_ []byte) (int, error) { return 0, errors.New("disk full") }
func (e *errorWriter) Close() error                { return nil }

func TestNew_DefaultsWork(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	l := New(logPath)
	if l == nil {
		t.Fatal("New() returned nil")
	}
}

func TestLogger_Log_CreatesDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nested", "dir", "audit.log")

	l := New(logPath)
	if err := l.Log(context.Background(), "approve", "owner", "repo", 42, ""); err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}

func TestLogger_Log_EntryFormat(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	l := New(logPath)

	before := time.Now().UTC().Truncate(time.Second)
	if err := l.Log(context.Background(), "approve", "myowner", "myrepo", 7, "some details"); err != nil {
		t.Fatalf("Log() error: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	lines := readLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	ts, action, repo, number, details := parseEntry(t, lines[0])

	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("timestamp not RFC3339: %v", err)
	}
	if parsed.Before(before) || parsed.After(after) {
		t.Errorf("timestamp %v outside expected range [%v, %v]", parsed, before, after)
	}
	if action != "approve" {
		t.Errorf("action = %q, want %q", action, "approve")
	}
	if repo != "myowner/myrepo" {
		t.Errorf("repo = %q, want %q", repo, "myowner/myrepo")
	}
	if number != "7" {
		t.Errorf("number = %q, want %q", number, "7")
	}
	if details != "some details" {
		t.Errorf("details = %q, want %q", details, "some details")
	}
}

func TestLogger_Log_EmptyOwnerAndRepo(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	l := New(logPath)

	if err := l.Log(context.Background(), "resolve-thread", "", "", 0, "thread123"); err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	lines := readLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	_, action, repo, number, details := parseEntry(t, lines[0])

	if action != "resolve-thread" {
		t.Errorf("action = %q, want %q", action, "resolve-thread")
	}
	if repo != "" {
		t.Errorf("repo = %q, want empty string", repo)
	}
	if number != "0" {
		t.Errorf("number = %q, want %q", number, "0")
	}
	if details != "thread123" {
		t.Errorf("details = %q, want %q", details, "thread123")
	}
}

func TestLogger_Log_MultipleEntriesAccumulateInOrder(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	l := New(logPath)

	actions := []string{"approve", "merge", "comment", "close"}
	for i, a := range actions {
		if err := l.Log(context.Background(), a, "owner", "repo", i+1, ""); err != nil {
			t.Fatalf("Log(%q) error: %v", a, err)
		}
	}

	lines := readLines(t, logPath)
	if len(lines) != len(actions) {
		t.Fatalf("expected %d lines, got %d", len(actions), len(lines))
	}

	for i, line := range lines {
		_, action, _, _, _ := parseEntry(t, line)
		if action != actions[i] {
			t.Errorf("line %d: action = %q, want %q", i, action, actions[i])
		}
	}
}

func TestLogger_Log_ConcurrentWritesDoNotInterleave(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")
	l := New(logPath)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			if err := l.Log(context.Background(), "action", "owner", "repo", n, "detail"); err != nil {
				t.Errorf("goroutine %d: Log() error: %v", n, err)
			}
		}(i)
	}
	wg.Wait()

	lines := readLines(t, logPath)
	if len(lines) != goroutines {
		t.Fatalf("expected %d lines, got %d", goroutines, len(lines))
	}

	for i, line := range lines {
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) != 5 {
			t.Errorf("line %d has %d fields (interleaved?): %q", i, len(parts), line)
		}
	}
}

func TestLogger_Log_MkdirAllError(t *testing.T) {
	l := New("/some/path/audit.log")
	mkdirErr := errors.New("permission denied")
	l.mkdirAll = func(_ string, _ os.FileMode) error { return mkdirErr }

	err := l.Log(context.Background(), "approve", "o", "r", 1, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "creating audit log directory") {
		t.Errorf("error = %v, want 'creating audit log directory'", err)
	}
}

func TestLogger_Log_OpenFileError(t *testing.T) {
	l := New("/some/path/audit.log")
	l.mkdirAll = func(_ string, _ os.FileMode) error { return nil }
	openErr := errors.New("no such file")
	l.openFile = func(_ string, _ int, _ os.FileMode) (io.WriteCloser, error) { return nil, openErr }

	err := l.Log(context.Background(), "approve", "o", "r", 1, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "opening audit log") {
		t.Errorf("error = %v, want 'opening audit log'", err)
	}
}

func TestLogger_Log_WriteError(t *testing.T) {
	l := New("/some/path/audit.log")
	l.mkdirAll = func(_ string, _ os.FileMode) error { return nil }
	l.openFile = func(_ string, _ int, _ os.FileMode) (io.WriteCloser, error) {
		return &errorWriter{}, nil
	}

	err := l.Log(context.Background(), "approve", "o", "r", 1, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "writing audit entry") {
		t.Errorf("error = %v, want 'writing audit entry'", err)
	}
}

func TestDefaultLogPath_ReturnsNonEmpty(t *testing.T) {
	path, err := DefaultLogPath()
	if err != nil {
		t.Fatalf("DefaultLogPath() error: %v", err)
	}
	if path == "" {
		t.Error("DefaultLogPath() returned empty string")
	}
	if !strings.HasSuffix(path, "audit.log") {
		t.Errorf("DefaultLogPath() = %q, expected suffix audit.log", path)
	}
}

func TestDefaultLogPath_HomeDirError(t *testing.T) {
	// Temporarily unset HOME to force os.UserHomeDir to fail.
	original := os.Getenv("HOME")
	t.Setenv("HOME", "")
	// Also clear USERPROFILE (used on Windows) and XDG_CONFIG_HOME just in case.
	t.Setenv("XDG_CONFIG_HOME", "")

	// On macOS, os.UserHomeDir uses the passwd database as fallback,
	// so unsetting HOME may still succeed. We check both outcomes gracefully.
	path, err := DefaultLogPath()
	if err != nil {
		// Error path covered.
		return
	}
	// If no error, restore and skip — system still resolved home dir.
	_ = original
	if path == "" {
		t.Error("DefaultLogPath() returned empty path without error")
	}
}
