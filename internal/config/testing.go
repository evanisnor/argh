package config

import "context"

// StubRepoFileFetcher is a test double for RepoFileFetcher.
type StubRepoFileFetcher struct {
	GetFileContentFunc func(ctx context.Context, owner, repo, path string) ([]byte, error)
}

// NewStubRepoFileFetcher returns a StubRepoFileFetcher that returns
// ErrRepoConfigNotFound by default (file absent).
func NewStubRepoFileFetcher() *StubRepoFileFetcher {
	return &StubRepoFileFetcher{
		GetFileContentFunc: func(_ context.Context, _, _, _ string) ([]byte, error) {
			return nil, ErrRepoConfigNotFound
		},
	}
}

// GetFileContent delegates to GetFileContentFunc.
func (s *StubRepoFileFetcher) GetFileContent(ctx context.Context, owner, repo, path string) ([]byte, error) {
	return s.GetFileContentFunc(ctx, owner, repo, path)
}
