package api

import (
	"context"
	"fmt"
)

// StubCommandExecutor is a test double for CommandExecutor.
// It returns preconfigured responses for specific commands.
type StubCommandExecutor struct {
	// Responses maps "name arg1 arg2 ..." to the bytes to return.
	Responses map[string][]byte
	// Errors maps "name arg1 arg2 ..." to the error to return.
	Errors map[string]error
}

// NewStubCommandExecutor creates a StubCommandExecutor with empty maps.
func NewStubCommandExecutor() *StubCommandExecutor {
	return &StubCommandExecutor{
		Responses: make(map[string][]byte),
		Errors:    make(map[string]error),
	}
}

// Output returns the preconfigured response for the command or an error.
func (s *StubCommandExecutor) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := commandKey(name, args...)
	if err, ok := s.Errors[key]; ok {
		return nil, err
	}
	if resp, ok := s.Responses[key]; ok {
		return resp, nil
	}
	return nil, fmt.Errorf("stub: no response configured for %q", key)
}

func commandKey(name string, args ...string) string {
	parts := append([]string{name}, args...)
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " "
		}
		result += p
	}
	return result
}
