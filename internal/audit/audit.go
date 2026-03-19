package audit

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger appends audit entries to a log file.
// It is safe for concurrent use.
type Logger struct {
	mu       sync.Mutex
	logPath  string
	mkdirAll func(path string, perm os.FileMode) error
	openFile func(name string, flag int, perm os.FileMode) (io.WriteCloser, error)
}

// New returns a Logger that writes to logPath.
// The parent directory is created on the first write.
func New(logPath string) *Logger {
	return &Logger{
		logPath:  logPath,
		mkdirAll: os.MkdirAll,
		openFile: func(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
			return os.OpenFile(name, flag, perm)
		},
	}
}

// DefaultLogPath returns the default audit log path: ~/.local/share/argh/audit.log.
func DefaultLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "argh", "audit.log"), nil
}

// Log appends a single audit entry.
// Format: <ISO-8601 timestamp>\t<action>\t<owner>/<repo>\t<number>\t<details>\n
// When owner and repo are both empty (e.g. resolveThread), the repo column is
// left empty and the number column is 0.
func (l *Logger) Log(_ context.Context, action, owner, repo string, number int, details string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.mkdirAll(filepath.Dir(l.logPath), 0o755); err != nil {
		return fmt.Errorf("creating audit log directory: %w", err)
	}

	f, err := l.openFile(l.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening audit log: %w", err)
	}
	defer f.Close()

	repoField := fmt.Sprintf("%s/%s", owner, repo)
	if owner == "" && repo == "" {
		repoField = ""
	}

	line := fmt.Sprintf("%s\t%s\t%s\t%d\t%s\n",
		time.Now().UTC().Format(time.RFC3339),
		action,
		repoField,
		number,
		details,
	)

	if _, err = fmt.Fprint(f, line); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return nil
}
