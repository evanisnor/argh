package diff

import "fmt"

// StubDiffFetcher is a test double for DiffFetcher.
type StubDiffFetcher struct {
	FetchFunc  func(url, token string) ([]byte, error)
	FetchURL   string
	FetchToken string
}

// NewStubDiffFetcher returns a StubDiffFetcher that succeeds with minimal diff content.
func NewStubDiffFetcher() *StubDiffFetcher {
	return &StubDiffFetcher{
		FetchFunc: func(url, token string) ([]byte, error) {
			return []byte("--- a/file\n+++ b/file\n@@ -1 +1 @@\n-old\n+new\n"), nil
		},
	}
}

// Fetch records the URL and token, then delegates to FetchFunc.
func (s *StubDiffFetcher) Fetch(url, token string) ([]byte, error) {
	s.FetchURL = url
	s.FetchToken = token
	return s.FetchFunc(url, token)
}

// SubprocessCall records a single call to SubprocessRunner.Run.
type SubprocessCall struct {
	Name  string
	Args  []string
	Stdin []byte
}

// StubSubprocessRunner is a test double for SubprocessRunner.
type StubSubprocessRunner struct {
	RunFunc func(name string, args []string, stdin []byte) ([]byte, error)
	Calls   []SubprocessCall
}

// NewStubSubprocessRunner returns a StubSubprocessRunner that succeeds by default.
func NewStubSubprocessRunner() *StubSubprocessRunner {
	return &StubSubprocessRunner{
		RunFunc: func(name string, args []string, stdin []byte) ([]byte, error) {
			return stdin, nil
		},
	}
}

// Run records the call and delegates to RunFunc.
func (s *StubSubprocessRunner) Run(name string, args []string, stdin []byte) ([]byte, error) {
	s.Calls = append(s.Calls, SubprocessCall{Name: name, Args: args, Stdin: stdin})
	return s.RunFunc(name, args, stdin)
}

// StubBinaryLookup is a test double for BinaryLookup.
type StubBinaryLookup struct {
	LookPathFunc func(name string) (string, error)
}

// NewStubBinaryLookup returns a StubBinaryLookup that returns path when non-empty,
// or a "not found" error when path is empty.
func NewStubBinaryLookup(path string) *StubBinaryLookup {
	return &StubBinaryLookup{
		LookPathFunc: func(name string) (string, error) {
			if path == "" {
				return "", fmt.Errorf("%q: executable file not found in $PATH", name)
			}
			return path, nil
		},
	}
}

// LookPath delegates to LookPathFunc.
func (s *StubBinaryLookup) LookPath(name string) (string, error) {
	return s.LookPathFunc(name)
}
