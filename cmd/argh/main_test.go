package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
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

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
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
			code := run(&stdout, &stderr, tt.goos)

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
