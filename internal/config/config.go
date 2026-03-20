package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultPollInterval      = 10 * time.Second
	defaultSleepPollInterval = 5 * time.Minute
	configDirName            = "argh"
	configFileName           = "config.yaml"
)

// ScheduleWindow defines a time window for DND or sleep schedule.
type ScheduleWindow struct {
	Days   []string `yaml:"days"`
	From   string   `yaml:"from"`
	To     string   `yaml:"to"`
	AllDay bool     `yaml:"all_day"`
}

// NotificationsConfig controls which events trigger system notifications.
type NotificationsConfig struct {
	CIPass           bool `yaml:"ci_pass"`
	CIFail           bool `yaml:"ci_fail"`
	Approved         bool `yaml:"approved"`
	ChangesRequested bool `yaml:"changes_requested"`
	ReviewRequested  bool `yaml:"review_requested"`
	Merged           bool `yaml:"merged"`
	WatchTriggered   bool `yaml:"watch_triggered"`
}

// DoNotDisturbConfig holds scheduled DND windows.
type DoNotDisturbConfig struct {
	Schedule []ScheduleWindow `yaml:"schedule"`
}

// SleepScheduleConfig holds reduced-polling windows and their poll interval.
type SleepScheduleConfig struct {
	PollInterval duration         `yaml:"poll_interval"`
	Windows      []ScheduleWindow `yaml:"windows"`
}

// OAuthConfig holds OAuth app configuration. Users can override the default
// client ID in config.yaml for GitHub Enterprise.
type OAuthConfig struct {
	ClientID string `yaml:"client_id"`
}

// Config is the top-level configuration for argh.
type Config struct {
	PollInterval  duration            `yaml:"poll_interval"`
	Notifications NotificationsConfig `yaml:"notifications"`
	DoNotDisturb  DoNotDisturbConfig  `yaml:"do_not_disturb"`
	SleepSchedule SleepScheduleConfig `yaml:"sleep_schedule"`
	OAuth         OAuthConfig         `yaml:"oauth"`
}

// duration is a wrapper around time.Duration that supports YAML unmarshaling
// from strings like "10s" or "5m".
type duration struct {
	time.Duration
}

func (d duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}

func (d *duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

const defaultOAuthClientID = "Ov23liYOUR_CLIENT_ID"

// defaults returns a Config with all default values applied.
func defaults() Config {
	return Config{
		PollInterval: duration{defaultPollInterval},
		Notifications: NotificationsConfig{
			CIPass:           true,
			CIFail:           true,
			Approved:         true,
			ChangesRequested: true,
			ReviewRequested:  true,
			Merged:           true,
			WatchTriggered:   true,
		},
		SleepSchedule: SleepScheduleConfig{
			PollInterval: duration{defaultSleepPollInterval},
		},
		OAuth: OAuthConfig{
			ClientID: defaultOAuthClientID,
		},
	}
}

// Defaults returns a Config with all default values applied. Useful in tests
// and for bootstrapping when no config file is present.
func Defaults() Config {
	return defaults()
}

// Filesystem abstracts file I/O for testability.
type Filesystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	UserConfigDir() (string, error)
}

// OSFilesystem implements Filesystem using the real OS.
type OSFilesystem struct{}

func (OSFilesystem) ReadFile(path string) ([]byte, error)                       { return os.ReadFile(path) }
func (OSFilesystem) WriteFile(path string, data []byte, perm os.FileMode) error { return os.WriteFile(path, data, perm) }
func (OSFilesystem) MkdirAll(path string, perm os.FileMode) error               { return os.MkdirAll(path, perm) }
func (OSFilesystem) Remove(path string) error                                   { return os.Remove(path) }
func (OSFilesystem) UserConfigDir() (string, error)                             { return os.UserConfigDir() }

// Load reads the config file from the user's config directory, creating the
// directory if it does not exist. Missing config file → all defaults applied.
// Partial file → specified fields overridden, rest use defaults.
func Load(fs Filesystem) (Config, error) {
	cfg := defaults()

	configDir, err := configDirPath(fs)
	if err != nil {
		return cfg, fmt.Errorf("resolving config directory: %w", err)
	}

	if err := fs.MkdirAll(configDir, 0o755); err != nil {
		return cfg, fmt.Errorf("creating config directory: %w", err)
	}

	configPath := filepath.Join(configDir, configFileName)
	data, err := fs.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config file: %w", err)
	}

	// Re-apply zero-value defaults for fields left unset by a partial file.
	if cfg.PollInterval.Duration == 0 {
		cfg.PollInterval = duration{defaultPollInterval}
	}
	if cfg.SleepSchedule.PollInterval.Duration == 0 {
		cfg.SleepSchedule.PollInterval = duration{defaultSleepPollInterval}
	}
	if cfg.OAuth.ClientID == "" {
		cfg.OAuth.ClientID = defaultOAuthClientID
	}

	return cfg, nil
}

func configDirPath(fs Filesystem) (string, error) {
	base, err := fs.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, configDirName), nil
}
