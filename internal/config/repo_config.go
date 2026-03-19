package config

import (
	"context"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ErrRepoConfigNotFound is returned by RepoFileFetcher when .argh.yaml does
// not exist in the repository root.
var ErrRepoConfigNotFound = errors.New("repo config not found")

// RepoFileFetcher fetches the raw content of a file from a GitHub repository.
// Implementations must return ErrRepoConfigNotFound when the file does not exist.
type RepoFileFetcher interface {
	GetFileContent(ctx context.Context, owner, repo, path string) ([]byte, error)
}

// RepoConfig holds per-repository overrides read from .argh.yaml in the repo root.
// Fields absent in the file retain their zero values; callers fall back to the
// global Config for any zero-value field.
type RepoConfig struct {
	DefaultReviewers []string          `yaml:"default_reviewers"`
	LabelConventions map[string]string `yaml:"label_conventions"`
	MergeStrategy    string            `yaml:"merge_strategy"`
}

// LoadRepoConfig fetches and parses .argh.yaml from the repository root via the
// GitHub API. If the file does not exist, ErrRepoConfigNotFound is returned and
// callers should fall back to the global config. If the file exists but is
// malformed, a parse error is returned.
func LoadRepoConfig(ctx context.Context, fetcher RepoFileFetcher, owner, repo string) (RepoConfig, error) {
	data, err := fetcher.GetFileContent(ctx, owner, repo, ".argh.yaml")
	if err != nil {
		if errors.Is(err, ErrRepoConfigNotFound) {
			return RepoConfig{}, ErrRepoConfigNotFound
		}
		return RepoConfig{}, fmt.Errorf("fetching .argh.yaml for %s/%s: %w", owner, repo, err)
	}

	var cfg RepoConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return RepoConfig{}, fmt.Errorf("parsing .argh.yaml for %s/%s: %w", owner, repo, err)
	}
	return cfg, nil
}
