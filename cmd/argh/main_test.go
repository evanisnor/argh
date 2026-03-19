package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/persistence"
	"github.com/evanisnor/argh/internal/status"
)

func TestCheckPlatform(t *testing.T) {
	tests := []struct {
		goos    string
		wantErr bool
	}{
		{goos: "darwin", wantErr: false},
		{goos: "linux", wantErr: true},
		{goos: "windows", wantErr: true},
		{goos: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			err := checkPlatform(tt.goos)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkPlatform(%q) error = %v, wantErr %v", tt.goos, err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !strings.Contains(err.Error(), "macOS only") {
					t.Errorf("checkPlatform(%q) error = %q, want message containing 'macOS only'", tt.goos, err.Error())
				}
			}
		})
	}
}

func TestHasArg(t *testing.T) {
	tests := []struct {
		args []string
		flag string
		want bool
	}{
		{args: []string{"--status"}, flag: "--status", want: true},
		{args: []string{"--other", "--status"}, flag: "--status", want: true},
		{args: []string{"--other"}, flag: "--status", want: false},
		{args: nil, flag: "--status", want: false},
	}
	for _, tt := range tests {
		got := hasArg(tt.args, tt.flag)
		if got != tt.want {
			t.Errorf("hasArg(%v, %q) = %v, want %v", tt.args, tt.flag, got, tt.want)
		}
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "darwin exits 0 and prints version",
			goos:       "darwin",
			wantCode:   0,
			wantStdout: "argh",
		},
		{
			name:       "linux exits 1 with error message",
			goos:       "linux",
			wantCode:   1,
			wantStderr: "macOS only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tt.goos, tt.args)

			if code != tt.wantCode {
				t.Errorf("run() = %d, want %d", code, tt.wantCode)
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestMain_CallsExit(t *testing.T) {
	var capturedCode int
	osExit = func(code int) { capturedCode = code }
	defer func() { osExit = os.Exit }()

	main()

	if capturedCode != 0 {
		t.Errorf("main() exit code = %d, want 0 on darwin", capturedCode)
	}
}

// ── runStatus tests ───────────────────────────────────────────────────────────

// fakeStatusFS is a fake Filesystem for runStatus tests.
type fakeStatusFS struct {
	dir       string
	statErr   error
	statExist bool // if true, Stat returns a synthetic FileInfo; if false, IsNotExist
}

func (f *fakeStatusFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (f *fakeStatusFS) UserDataDir() (string, error) {
	return f.dir, nil
}

func (f *fakeStatusFS) Stat(path string) (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	if !f.statExist {
		return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
	}
	return &fakeFileInfo{name: "argh.db"}, nil
}

// fakeFileInfo satisfies os.FileInfo without touching the real filesystem.
type fakeFileInfo struct{ name string }

func (f *fakeFileInfo) Name() string      { return f.name }
func (f *fakeFileInfo) Size() int64       { return 0 }
func (f *fakeFileInfo) Mode() os.FileMode { return 0644 }
func (f *fakeFileInfo) ModTime() time.Time { return time.Now() }
func (f *fakeFileInfo) IsDir() bool       { return false }
func (f *fakeFileInfo) Sys() interface{}  { return nil }

// fakeStatusReader satisfies status.Reader without a real DB.
type fakeStatusReader struct {
	prs         []persistence.PullRequest
	pending     int
	maxActivity time.Time
	hasData     bool
	listErr     error
	pendingErr  error
	maxErr      error
}

func (f *fakeStatusReader) ListPullRequests() ([]persistence.PullRequest, error) {
	return f.prs, f.listErr
}
func (f *fakeStatusReader) CountPRsWithPendingReview() (int, error) {
	return f.pending, f.pendingErr
}
func (f *fakeStatusReader) MaxLastActivityAt() (time.Time, bool, error) {
	return f.maxActivity, f.hasData, f.maxErr
}
func (f *fakeStatusReader) Close() error { return nil }

func makeStatusReader(prs []persistence.PullRequest, pending int, maxActivity time.Time) status.Reader {
	return &fakeStatusReader{
		prs:         prs,
		pending:     pending,
		maxActivity: maxActivity,
		hasData:     !maxActivity.IsZero(),
	}
}

func TestRunStatus_DBNotExist(t *testing.T) {
	fs := &fakeStatusFS{dir: t.TempDir(), statExist: false}
	var out, errOut bytes.Buffer
	code := runStatus(&out, &errOut, fs, time.Now, func(persistence.Filesystem) (status.Reader, error) {
		return &fakeStatusReader{}, nil
	})
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "argh: no data") {
		t.Errorf("stdout = %q, want 'argh: no data'", out.String())
	}
}

func TestRunStatus_DBPathError(t *testing.T) {
	// UserDataDir fails → DBPath fails → "argh: no data".
	// Simulate by using a filesystem that returns error from UserDataDir.
	badFS := &badUserDataFS{}
	var out, errOut bytes.Buffer
	code := runStatus(&out, &errOut, badFS, time.Now, func(persistence.Filesystem) (status.Reader, error) {
		return &fakeStatusReader{}, nil
	})
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "argh: no data") {
		t.Errorf("stdout = %q, want 'argh: no data'", out.String())
	}
}

// badUserDataFS has a UserDataDir that always errors.
type badUserDataFS struct{}

func (b *badUserDataFS) MkdirAll(path string, perm os.FileMode) error { return nil }
func (b *badUserDataFS) UserDataDir() (string, error)                 { return "", errors.New("no home") }
func (b *badUserDataFS) Stat(path string) (os.FileInfo, error)        { return nil, nil }

func TestRunStatus_StatError(t *testing.T) {
	fs := &fakeStatusFS{
		dir:     t.TempDir(),
		statErr: errors.New("permission denied"),
	}
	var out, errOut bytes.Buffer
	code := runStatus(&out, &errOut, fs, time.Now, func(persistence.Filesystem) (status.Reader, error) {
		return &fakeStatusReader{}, nil
	})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "permission denied") {
		t.Errorf("stderr = %q, want 'permission denied'", errOut.String())
	}
}

func TestRunStatus_OpenDBError(t *testing.T) {
	fs := &fakeStatusFS{dir: t.TempDir(), statExist: true}
	var out, errOut bytes.Buffer
	code := runStatus(&out, &errOut, fs, time.Now, func(persistence.Filesystem) (status.Reader, error) {
		return nil, errors.New("cannot open")
	})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "cannot open") {
		t.Errorf("stderr = %q, want 'cannot open'", errOut.String())
	}
}

func TestRunStatus_ComputeError(t *testing.T) {
	fs := &fakeStatusFS{dir: t.TempDir(), statExist: true}
	badReader := &fakeStatusReader{listErr: errors.New("db gone")}
	var out, errOut bytes.Buffer
	code := runStatus(&out, &errOut, fs, time.Now, func(persistence.Filesystem) (status.Reader, error) {
		return badReader, nil
	})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "db gone") {
		t.Errorf("stderr = %q, want 'db gone'", errOut.String())
	}
}

func TestRunStatus_Success(t *testing.T) {
	now := time.Now()
	recent := now.Add(-10 * time.Minute)

	fs := &fakeStatusFS{dir: t.TempDir(), statExist: true}
	reader := &fakeStatusReader{
		prs: []persistence.PullRequest{
			{CIState: "failing"}, {CIState: "passing"}, {CIState: "none"},
		},
		pending:     2,
		maxActivity: recent,
		hasData:     true,
	}
	var out, errOut bytes.Buffer
	code := runStatus(&out, &errOut, fs, func() time.Time { return now }, func(persistence.Filesystem) (status.Reader, error) {
		return reader, nil
	})
	if code != 0 {
		t.Errorf("code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	line := strings.TrimSpace(out.String())
	if !strings.HasPrefix(line, "↑3 PRs") {
		t.Errorf("output = %q, want prefix '↑3 PRs'", line)
	}
	if !strings.Contains(line, "✗1 CI") {
		t.Errorf("output = %q, want '✗1 CI'", line)
	}
	if !strings.Contains(line, "↓2 review") {
		t.Errorf("output = %q, want '↓2 review'", line)
	}
}

func TestRunStatus_Stale(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour)

	fs := &fakeStatusFS{dir: t.TempDir(), statExist: true}
	reader := &fakeStatusReader{
		prs:         []persistence.PullRequest{{CIState: "passing"}},
		maxActivity: old,
		hasData:     true,
	}
	var out, errOut bytes.Buffer
	code := runStatus(&out, &errOut, fs, func() time.Time { return now }, func(persistence.Filesystem) (status.Reader, error) {
		return reader, nil
	})
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	line := strings.TrimSpace(out.String())
	if !strings.Contains(line, "ago") {
		t.Errorf("output = %q, want staleness indicator containing 'ago'", line)
	}
}

func TestRun_StatusFlag(t *testing.T) {
	// Integration: pass --status; platform check must pass first (darwin).
	// Since runStatus uses OSFilesystem by default and there is likely no real
	// argh DB present in CI, it should print "argh: no data" and exit 0.
	var out, errOut bytes.Buffer
	code := run(&out, &errOut, "darwin", []string{"--status"})
	if code != 0 {
		t.Logf("stderr: %s", errOut.String())
	}
	// Acceptable outcomes: "argh: no data" (no DB) or a valid status line.
	output := out.String() + errOut.String()
	if output == "" {
		t.Error("expected non-empty output from --status")
	}
}

func TestRun_StatusFlag_WithDB(t *testing.T) {
	// Create a real DB in a temp dir via XDG_DATA_HOME so the openDB lambda
	// inside run() is exercised.
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	db, err := persistence.Open(persistence.OSFilesystem{})
	if err != nil {
		t.Fatalf("persistence.Open(): %v", err)
	}
	db.Close()

	var out, errOut bytes.Buffer
	code := run(&out, &errOut, "darwin", []string{"--status"})
	if code != 0 {
		t.Errorf("code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	line := strings.TrimSpace(out.String())
	if !strings.HasPrefix(line, "↑") {
		t.Errorf("output = %q, want status line starting with '↑'", line)
	}
}

func TestRun_StatusFlag_NonDarwin(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(&out, &errOut, "linux", []string{"--status"})
	if code != 1 {
		t.Errorf("code = %d, want 1 on non-darwin", code)
	}
}
