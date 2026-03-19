package config_test

import (
	"context"
	"errors"
	"testing"

	"github.com/evanisnor/argh/internal/config"
)

func TestLoadRepoConfig_Present_AllFieldsParsed(t *testing.T) {
	fetcher := config.NewStubRepoFileFetcher()
	fetcher.GetFileContentFunc = func(_ context.Context, _, _, _ string) ([]byte, error) {
		return []byte(`
default_reviewers:
  - alice
  - bob
label_conventions:
  ready: "ready for review"
  wip: "work in progress"
merge_strategy: squash
`), nil
	}

	cfg, err := config.LoadRepoConfig(context.Background(), fetcher, "owner", "repo")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(cfg.DefaultReviewers) != 2 || cfg.DefaultReviewers[0] != "alice" || cfg.DefaultReviewers[1] != "bob" {
		t.Errorf("DefaultReviewers: got %v, want [alice bob]", cfg.DefaultReviewers)
	}
	if len(cfg.LabelConventions) != 2 {
		t.Errorf("LabelConventions: got %v, want 2 entries", cfg.LabelConventions)
	}
	if cfg.LabelConventions["ready"] != "ready for review" {
		t.Errorf(`LabelConventions["ready"]: got %q, want "ready for review"`, cfg.LabelConventions["ready"])
	}
	if cfg.LabelConventions["wip"] != "work in progress" {
		t.Errorf(`LabelConventions["wip"]: got %q, want "work in progress"`, cfg.LabelConventions["wip"])
	}
	if cfg.MergeStrategy != "squash" {
		t.Errorf("MergeStrategy: got %q, want squash", cfg.MergeStrategy)
	}
}

func TestLoadRepoConfig_Absent_ReturnsNotFound(t *testing.T) {
	fetcher := config.NewStubRepoFileFetcher() // default: returns ErrRepoConfigNotFound

	_, err := config.LoadRepoConfig(context.Background(), fetcher, "owner", "repo")
	if !errors.Is(err, config.ErrRepoConfigNotFound) {
		t.Fatalf("expected ErrRepoConfigNotFound, got: %v", err)
	}
}

func TestLoadRepoConfig_Malformed_ReturnsError(t *testing.T) {
	fetcher := config.NewStubRepoFileFetcher()
	fetcher.GetFileContentFunc = func(_ context.Context, _, _, _ string) ([]byte, error) {
		return []byte(`merge_strategy: [invalid yaml`), nil
	}

	_, err := config.LoadRepoConfig(context.Background(), fetcher, "owner", "repo")
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
	if errors.Is(err, config.ErrRepoConfigNotFound) {
		t.Fatal("expected parse error, not ErrRepoConfigNotFound")
	}
}

func TestLoadRepoConfig_FetchError_ReturnsWrappedError(t *testing.T) {
	fetchErr := errors.New("connection refused")
	fetcher := config.NewStubRepoFileFetcher()
	fetcher.GetFileContentFunc = func(_ context.Context, _, _, _ string) ([]byte, error) {
		return nil, fetchErr
	}

	_, err := config.LoadRepoConfig(context.Background(), fetcher, "owner", "repo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, fetchErr) {
		t.Errorf("expected wrapped fetchErr in error chain, got: %v", err)
	}
}

func TestLoadRepoConfig_MergeStrategy_OverridesDefault(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantStrategy string
	}{
		{
			name:         "squash strategy",
			yaml:         `merge_strategy: squash`,
			wantStrategy: "squash",
		},
		{
			name:         "merge strategy",
			yaml:         `merge_strategy: merge`,
			wantStrategy: "merge",
		},
		{
			name:         "rebase strategy",
			yaml:         `merge_strategy: rebase`,
			wantStrategy: "rebase",
		},
		{
			name:         "absent strategy is empty (use repo default)",
			yaml:         `default_reviewers: [alice]`,
			wantStrategy: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := config.NewStubRepoFileFetcher()
			fetcher.GetFileContentFunc = func(_ context.Context, _, _, _ string) ([]byte, error) {
				return []byte(tt.yaml), nil
			}

			cfg, err := config.LoadRepoConfig(context.Background(), fetcher, "owner", "repo")
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			// Repo-level MergeStrategy takes precedence over global config (which has no
			// merge_strategy field). An empty value means "use the repo's GitHub default".
			if cfg.MergeStrategy != tt.wantStrategy {
				t.Errorf("MergeStrategy: got %q, want %q", cfg.MergeStrategy, tt.wantStrategy)
			}
		})
	}
}

func TestLoadRepoConfig_GlobalConfigUnchanged_WhenAbsent(t *testing.T) {
	// When .argh.yaml is absent, the global config is unchanged (LoadRepoConfig
	// returns ErrRepoConfigNotFound and callers fall back to global config).
	fs := newFakeFS(t.TempDir())
	globalCfg, err := config.Load(fs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	fetcher := config.NewStubRepoFileFetcher() // absent by default
	_, repoErr := config.LoadRepoConfig(context.Background(), fetcher, "owner", "repo")
	if !errors.Is(repoErr, config.ErrRepoConfigNotFound) {
		t.Fatalf("expected ErrRepoConfigNotFound, got: %v", repoErr)
	}

	// Re-load global config to verify it is unchanged.
	globalCfg2, err := config.Load(fs)
	if err != nil {
		t.Fatalf("Load (second): %v", err)
	}
	if globalCfg.PollInterval != globalCfg2.PollInterval {
		t.Error("global PollInterval changed unexpectedly")
	}
}
