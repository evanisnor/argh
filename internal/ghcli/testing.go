package ghcli

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// StubCommandRunner is a test double for CommandRunner that records calls
// and returns configurable responses per argument pattern.
type StubCommandRunner struct {
	mu       sync.Mutex
	Calls    [][]string
	RunFunc  func(ctx context.Context, args []string) ([]byte, error)
}

// NewStubCommandRunner returns a StubCommandRunner that returns empty output by default.
func NewStubCommandRunner() *StubCommandRunner {
	return &StubCommandRunner{
		RunFunc: func(_ context.Context, _ []string) ([]byte, error) {
			return nil, nil
		},
	}
}

// Run records the call and delegates to RunFunc.
func (s *StubCommandRunner) Run(ctx context.Context, args []string) ([]byte, error) {
	s.mu.Lock()
	s.Calls = append(s.Calls, args)
	s.mu.Unlock()
	return s.RunFunc(ctx, args)
}

// CallCount returns the number of recorded calls (thread-safe).
func (s *StubCommandRunner) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Calls)
}

// LastCall returns the args from the most recent call, or nil if no calls were made.
func (s *StubCommandRunner) LastCall() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Calls) == 0 {
		return nil
	}
	return s.Calls[len(s.Calls)-1]
}

// FindCall returns the first call whose args contain all of the given substrings.
func (s *StubCommandRunner) FindCall(substrings ...string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, call := range s.Calls {
		joined := strings.Join(call, " ")
		allMatch := true
		for _, sub := range substrings {
			if !strings.Contains(joined, sub) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return call
		}
	}
	return nil
}

// StubLookup returns a BinaryLookup that reports the given path and error.
func StubLookup(path string, err error) BinaryLookup {
	return func(name string) (string, error) {
		if err != nil {
			return "", err
		}
		return path, nil
	}
}

// StubLookupNotFound returns a BinaryLookup that reports the binary as not found.
func StubLookupNotFound() BinaryLookup {
	return func(name string) (string, error) {
		return "", fmt.Errorf("executable not found: %s", name)
	}
}
