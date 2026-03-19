package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunPrintsVersion(t *testing.T) {
	var buf bytes.Buffer
	run(&buf)
	got := buf.String()
	if !strings.HasPrefix(got, "argh ") {
		t.Errorf("expected output to start with 'argh ', got: %q", got)
	}
}

func TestMainExitsCleanly(t *testing.T) {
	// main() calls run(os.Stdout); verify it does not panic.
	main()
}
