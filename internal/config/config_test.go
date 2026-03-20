package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/config"
	"gopkg.in/yaml.v3"
)

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// fakeFS is an in-memory Filesystem for tests.
type fakeFS struct {
	files     map[string][]byte
	mkdirDirs []string
	configDir string
	mkdirErr  error
	configErr error
	readErr   error
	writeErr  error
	removeErr error
}

func newFakeFS(configDir string) *fakeFS {
	return &fakeFS{
		files:     make(map[string][]byte),
		configDir: configDir,
	}
}

func (f *fakeFS) ReadFile(path string) ([]byte, error) {
	if f.readErr != nil {
		if _, ok := f.files[path]; ok {
			return nil, f.readErr
		}
	}
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (f *fakeFS) WriteFile(path string, data []byte, _ os.FileMode) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.files[path] = data
	return nil
}

func (f *fakeFS) Remove(path string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	delete(f.files, path)
	return nil
}

func (f *fakeFS) MkdirAll(path string, _ os.FileMode) error {
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	f.mkdirDirs = append(f.mkdirDirs, path)
	return nil
}

func (f *fakeFS) UserConfigDir() (string, error) {
	if f.configErr != nil {
		return "", f.configErr
	}
	return f.configDir, nil
}

func (f *fakeFS) writeConfig(dir, content string) {
	path := filepath.Join(dir, "argh", "config.yaml")
	f.files[path] = []byte(content)
}

func TestLoad_MissingFile_AllDefaults(t *testing.T) {
	fs := newFakeFS(t.TempDir())
	cfg, err := config.Load(fs)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.PollInterval.Duration != 10*time.Second {
		t.Errorf("PollInterval: got %v, want 10s", cfg.PollInterval.Duration)
	}
	if cfg.SleepSchedule.PollInterval.Duration != 5*time.Minute {
		t.Errorf("SleepSchedule.PollInterval: got %v, want 5m", cfg.SleepSchedule.PollInterval.Duration)
	}
	// Notification defaults should all be true.
	n := cfg.Notifications
	if !n.CIPass || !n.CIFail || !n.Approved || !n.ChangesRequested ||
		!n.ReviewRequested || !n.Merged || !n.WatchTriggered {
		t.Errorf("expected all notification defaults to be true, got: %+v", n)
	}
}

func TestLoad_MissingFile_ConfigDirCreated(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	_, err := config.Load(fs)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	expected := filepath.Join(dir, "argh")
	for _, d := range fs.mkdirDirs {
		if d == expected {
			return
		}
	}
	t.Errorf("expected config dir %q to be created; got dirs: %v", expected, fs.mkdirDirs)
}

func TestLoad_PartialFile_OverridesAndDefaults(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	fs.writeConfig(dir, `
poll_interval: "30s"
notifications:
  ci_fail: false
`)
	cfg, err := config.Load(fs)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.PollInterval.Duration != 30*time.Second {
		t.Errorf("PollInterval: got %v, want 30s", cfg.PollInterval.Duration)
	}
	if cfg.Notifications.CIFail {
		t.Error("expected CIFail to be false")
	}
	// Other notification defaults remain true.
	if !cfg.Notifications.CIPass {
		t.Error("expected CIPass to remain true (default)")
	}
	// Sleep poll interval should still be the default.
	if cfg.SleepSchedule.PollInterval.Duration != 5*time.Minute {
		t.Errorf("SleepSchedule.PollInterval: got %v, want 5m", cfg.SleepSchedule.PollInterval.Duration)
	}
}

func TestLoad_FullFile_AllFieldsParsed(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	fs.writeConfig(dir, `
poll_interval: "20s"
notifications:
  ci_pass: false
  ci_fail: false
  approved: false
  changes_requested: false
  review_requested: false
  merged: false
  watch_triggered: false
do_not_disturb:
  schedule:
    - days: [Mon, Tue]
      from: "22:00"
      to: "08:00"
    - all_day: true
      days: [Sat, Sun]
sleep_schedule:
  poll_interval: "10m"
  windows:
    - days: [Mon, Tue, Wed, Thu, Fri]
      from: "20:00"
      to: "07:00"
`)
	cfg, err := config.Load(fs)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.PollInterval.Duration != 20*time.Second {
		t.Errorf("PollInterval: got %v, want 20s", cfg.PollInterval.Duration)
	}
	n := cfg.Notifications
	if n.CIPass || n.CIFail || n.Approved || n.ChangesRequested ||
		n.ReviewRequested || n.Merged || n.WatchTriggered {
		t.Errorf("expected all notifications false, got: %+v", n)
	}
	if len(cfg.DoNotDisturb.Schedule) != 2 {
		t.Errorf("expected 2 DND windows, got %d", len(cfg.DoNotDisturb.Schedule))
	}
	if cfg.DoNotDisturb.Schedule[0].From != "22:00" {
		t.Errorf("DND window From: got %q, want 22:00", cfg.DoNotDisturb.Schedule[0].From)
	}
	if !cfg.DoNotDisturb.Schedule[1].AllDay {
		t.Error("expected second DND window to be all_day")
	}
	if cfg.SleepSchedule.PollInterval.Duration != 10*time.Minute {
		t.Errorf("SleepSchedule.PollInterval: got %v, want 10m", cfg.SleepSchedule.PollInterval.Duration)
	}
	if len(cfg.SleepSchedule.Windows) != 1 {
		t.Errorf("expected 1 sleep window, got %d", len(cfg.SleepSchedule.Windows))
	}
	if cfg.SleepSchedule.Windows[0].From != "20:00" {
		t.Errorf("sleep window From: got %q, want 20:00", cfg.SleepSchedule.Windows[0].From)
	}
}

func TestLoad_MalformedYAML_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	fs.writeConfig(dir, `poll_interval: [not a duration`)
	_, err := config.Load(fs)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestLoad_InvalidDuration_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	fs.writeConfig(dir, `poll_interval: "notaduration"`)
	_, err := config.Load(fs)
	if err == nil {
		t.Fatal("expected error for invalid duration, got nil")
	}
}

func TestLoad_UserConfigDirError_ReturnsError(t *testing.T) {
	fs := newFakeFS("")
	fs.configErr = errors.New("no home dir")
	_, err := config.Load(fs)
	if err == nil {
		t.Fatal("expected error when UserConfigDir fails, got nil")
	}
}

func TestLoad_ReadFileError_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	// Add the config file path with a non-ErrNotExist error by using a custom readFile func.
	configPath := filepath.Join(dir, "argh", "config.yaml")
	readErr := errors.New("permission denied")
	fs.files[configPath] = nil // sentinel: present but will return error
	fs.readErr = readErr
	_, err := config.Load(fs)
	if err == nil {
		t.Fatal("expected error when ReadFile fails with non-ErrNotExist, got nil")
	}
}

func TestLoad_MkdirError_ReturnsError(t *testing.T) {
	fs := newFakeFS(t.TempDir())
	fs.mkdirErr = errors.New("permission denied")
	_, err := config.Load(fs)
	if err == nil {
		t.Fatal("expected error when MkdirAll fails, got nil")
	}
}

func TestLoad_ZeroPollInterval_DefaultApplied(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	// Explicitly setting "0s" triggers the re-apply-defaults branch.
	fs.writeConfig(dir, `poll_interval: "0s"`)
	cfg, err := config.Load(fs)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.PollInterval.Duration != 10*time.Second {
		t.Errorf("PollInterval: got %v, want 10s (default)", cfg.PollInterval.Duration)
	}
}

func TestLoad_ZeroSleepPollInterval_DefaultApplied(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	// sleep_schedule present but poll_interval not set → zero value → default applied.
	fs.writeConfig(dir, `
sleep_schedule:
  poll_interval: "0s"
  windows:
    - days: [Mon]
      from: "22:00"
      to: "08:00"
`)
	cfg, err := config.Load(fs)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.SleepSchedule.PollInterval.Duration != 5*time.Minute {
		t.Errorf("SleepSchedule.PollInterval: got %v, want 5m (default)", cfg.SleepSchedule.PollInterval.Duration)
	}
}

func TestLoad_DurationDecodeError_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	// A YAML mapping cannot be decoded into a string, triggering the Decode error branch.
	fs.writeConfig(dir, `
poll_interval:
  nested: value
`)
	_, err := config.Load(fs)
	if err == nil {
		t.Fatal("expected error when duration YAML node is a mapping, got nil")
	}
}

func TestDurationMarshalYAML(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	fs.writeConfig(dir, `poll_interval: "30s"`)

	cfg, err := config.Load(fs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Marshal back to YAML to exercise MarshalYAML on the duration type.
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	// The marshaled output should contain the string form of the duration.
	if !containsString(string(data), "30s") {
		t.Errorf("marshaled YAML %q does not contain 30s", string(data))
	}
}

func TestOSFilesystem_UserConfigDir(t *testing.T) {
	fs := config.OSFilesystem{}
	dir, err := fs.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	if dir == "" {
		t.Error("UserConfigDir returned empty string")
	}
}

func TestOSFilesystem_MkdirAllAndReadFile(t *testing.T) {
	dir := t.TempDir()
	fs := config.OSFilesystem{}

	subdir := filepath.Join(dir, "testdir")
	if err := fs.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	filePath := filepath.Join(subdir, "file.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	data, err := fs.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("ReadFile: got %q, want %q", string(data), "hello")
	}

	// ReadFile on missing file returns os.ErrNotExist.
	_, err = fs.ReadFile(filepath.Join(subdir, "missing.txt"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadFile missing: got %v, want ErrNotExist", err)
	}
}

func TestOSFilesystem_WriteFile(t *testing.T) {
	dir := t.TempDir()
	fs := config.OSFilesystem{}
	filePath := filepath.Join(dir, "written.txt")

	if err := fs.WriteFile(filePath, []byte("hello write"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	data, err := fs.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile after WriteFile: %v", err)
	}
	if string(data) != "hello write" {
		t.Errorf("ReadFile: got %q, want %q", string(data), "hello write")
	}
}

func TestOSFilesystem_Remove(t *testing.T) {
	dir := t.TempDir()
	fs := config.OSFilesystem{}
	filePath := filepath.Join(dir, "removeme.txt")

	if err := os.WriteFile(filePath, []byte("temp"), 0o644); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}

	if err := fs.Remove(filePath); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	_, err := fs.ReadFile(filePath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadFile after Remove: got %v, want ErrNotExist", err)
	}
}

// ── Defaults ──────────────────────────────────────────────────────────────────

func TestConfig_Defaults(t *testing.T) {
	cfg := config.Defaults()
	if cfg.PollInterval.Duration <= 0 {
		t.Errorf("Defaults().PollInterval: got %v, want > 0", cfg.PollInterval.Duration)
	}
	if !cfg.Notifications.CIPass {
		t.Error("Defaults().Notifications.CIPass: want true")
	}
	if !cfg.Notifications.CIFail {
		t.Error("Defaults().Notifications.CIFail: want true")
	}
	if !cfg.Notifications.Approved {
		t.Error("Defaults().Notifications.Approved: want true")
	}
	if !cfg.Notifications.Merged {
		t.Error("Defaults().Notifications.Merged: want true")
	}
	if cfg.OAuth.ClientID == "" {
		t.Error("Defaults().OAuth.ClientID: want non-empty default")
	}
}

func TestLoad_OAuthSection_DefaultClientID(t *testing.T) {
	fs := newFakeFS(t.TempDir())
	cfg, err := config.Load(fs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OAuth.ClientID == "" {
		t.Error("OAuth.ClientID should have a default value when config file is missing")
	}
}

func TestLoad_OAuthSection_CustomClientID(t *testing.T) {
	dir := t.TempDir()
	fs := newFakeFS(dir)
	fs.writeConfig(dir, `
oauth:
  client_id: "my-custom-client-id"
`)
	cfg, err := config.Load(fs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OAuth.ClientID != "my-custom-client-id" {
		t.Errorf("OAuth.ClientID: got %q, want %q", cfg.OAuth.ClientID, "my-custom-client-id")
	}
}
