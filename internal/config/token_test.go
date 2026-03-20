package config_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/evanisnor/argh/internal/config"
)

func TestConfigDirPath(t *testing.T) {
	fs := newFakeFS("/home/user/.config")
	path, err := config.ConfigDirPath(fs)
	if err != nil {
		t.Fatalf("ConfigDirPath: %v", err)
	}
	want := filepath.Join("/home/user/.config", "argh")
	if path != want {
		t.Errorf("ConfigDirPath: got %q, want %q", path, want)
	}
}

func TestConfigDirPath_Error(t *testing.T) {
	fs := newFakeFS("")
	fs.configErr = errors.New("no config dir")
	_, err := config.ConfigDirPath(fs)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoadToken(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(fs *fakeFS)
		wantToken string
		wantErr   error
		wantErrAs bool // if true, check errors.Is; if false, just check err != nil
	}{
		{
			name: "token found",
			setup: func(fs *fakeFS) {
				fs.files[filepath.Join(fs.configDir, "argh", "token")] = []byte("ghp_abc123\n")
			},
			wantToken: "ghp_abc123",
		},
		{
			name:    "token not found",
			setup:   func(fs *fakeFS) {},
			wantErr: config.ErrTokenNotFound,
			wantErrAs: true,
		},
		{
			name: "token file empty",
			setup: func(fs *fakeFS) {
				fs.files[filepath.Join(fs.configDir, "argh", "token")] = []byte("  \n")
			},
			wantErrAs: false,
		},
		{
			name: "token file whitespace only",
			setup: func(fs *fakeFS) {
				fs.files[filepath.Join(fs.configDir, "argh", "token")] = []byte("   ")
			},
			wantErrAs: false,
		},
		{
			name: "UserConfigDir error",
			setup: func(fs *fakeFS) {
				fs.configErr = errors.New("no home dir")
			},
			wantErrAs: false,
		},
		{
			name: "read error non-ErrNotExist",
			setup: func(fs *fakeFS) {
				fs.files[filepath.Join(fs.configDir, "argh", "token")] = nil
				fs.readErr = errors.New("permission denied")
			},
			wantErrAs: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeFS(t.TempDir())
			tt.setup(fs)

			token, err := config.LoadToken(fs)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("LoadToken error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if tt.wantToken == "" && tt.name != "token found" {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("LoadToken: unexpected error: %v", err)
			}
			if token != tt.wantToken {
				t.Errorf("LoadToken: got %q, want %q", token, tt.wantToken)
			}
		})
	}
}

func TestSaveToken(t *testing.T) {
	t.Run("save and re-read", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		if err := config.SaveToken(fs, "ghp_saved"); err != nil {
			t.Fatalf("SaveToken: %v", err)
		}

		token, err := config.LoadToken(fs)
		if err != nil {
			t.Fatalf("LoadToken after save: %v", err)
		}
		if token != "ghp_saved" {
			t.Errorf("LoadToken: got %q, want %q", token, "ghp_saved")
		}
	})

	t.Run("MkdirAll error", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		fs.mkdirErr = errors.New("permission denied")
		err := config.SaveToken(fs, "ghp_saved")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("WriteFile error", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		fs.writeErr = errors.New("disk full")
		err := config.SaveToken(fs, "ghp_saved")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("UserConfigDir error", func(t *testing.T) {
		fs := newFakeFS("")
		fs.configErr = errors.New("no home dir")
		err := config.SaveToken(fs, "ghp_saved")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestLoadTokenType(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(fs *fakeFS)
		wantType config.TokenType
		wantErr  bool
	}{
		{
			name: "file present with pat",
			setup: func(fs *fakeFS) {
				fs.files[filepath.Join(fs.configDir, "argh", "token_type")] = []byte("pat")
			},
			wantType: config.TokenTypePAT,
		},
		{
			name: "file present with oauth",
			setup: func(fs *fakeFS) {
				fs.files[filepath.Join(fs.configDir, "argh", "token_type")] = []byte("oauth")
			},
			wantType: config.TokenTypeOAuth,
		},
		{
			name: "file present with ghcli",
			setup: func(fs *fakeFS) {
				fs.files[filepath.Join(fs.configDir, "argh", "token_type")] = []byte("ghcli")
			},
			wantType: config.TokenTypeGHCLI,
		},
		{
			name:     "file missing defaults to PAT",
			setup:    func(fs *fakeFS) {},
			wantType: config.TokenTypePAT,
		},
		{
			name: "empty file defaults to PAT",
			setup: func(fs *fakeFS) {
				fs.files[filepath.Join(fs.configDir, "argh", "token_type")] = []byte("  \n")
			},
			wantType: config.TokenTypePAT,
		},
		{
			name: "UserConfigDir error",
			setup: func(fs *fakeFS) {
				fs.configErr = errors.New("no home dir")
			},
			wantErr: true,
		},
		{
			name: "read error non-ErrNotExist",
			setup: func(fs *fakeFS) {
				fs.files[filepath.Join(fs.configDir, "argh", "token_type")] = nil
				fs.readErr = errors.New("permission denied")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeFS(t.TempDir())
			tt.setup(fs)

			got, err := config.LoadTokenType(fs)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadTokenType: unexpected error: %v", err)
			}
			if got != tt.wantType {
				t.Errorf("LoadTokenType: got %q, want %q", got, tt.wantType)
			}
		})
	}
}

func TestSaveTokenType(t *testing.T) {
	t.Run("save and re-read", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		if err := config.SaveTokenType(fs, config.TokenTypeOAuth); err != nil {
			t.Fatalf("SaveTokenType: %v", err)
		}

		got, err := config.LoadTokenType(fs)
		if err != nil {
			t.Fatalf("LoadTokenType after save: %v", err)
		}
		if got != config.TokenTypeOAuth {
			t.Errorf("LoadTokenType: got %q, want %q", got, config.TokenTypeOAuth)
		}
	})

	t.Run("round-trip GHCLI", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		if err := config.SaveTokenType(fs, config.TokenTypeGHCLI); err != nil {
			t.Fatalf("SaveTokenType: %v", err)
		}

		got, err := config.LoadTokenType(fs)
		if err != nil {
			t.Fatalf("LoadTokenType after save: %v", err)
		}
		if got != config.TokenTypeGHCLI {
			t.Errorf("LoadTokenType: got %q, want %q", got, config.TokenTypeGHCLI)
		}
	})

	t.Run("round-trip PAT", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		if err := config.SaveTokenType(fs, config.TokenTypePAT); err != nil {
			t.Fatalf("SaveTokenType: %v", err)
		}

		got, err := config.LoadTokenType(fs)
		if err != nil {
			t.Fatalf("LoadTokenType after save: %v", err)
		}
		if got != config.TokenTypePAT {
			t.Errorf("LoadTokenType: got %q, want %q", got, config.TokenTypePAT)
		}
	})

	t.Run("MkdirAll error", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		fs.mkdirErr = errors.New("permission denied")
		err := config.SaveTokenType(fs, config.TokenTypePAT)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("WriteFile error", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		fs.writeErr = errors.New("disk full")
		err := config.SaveTokenType(fs, config.TokenTypePAT)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("UserConfigDir error", func(t *testing.T) {
		fs := newFakeFS("")
		fs.configErr = errors.New("no home dir")
		err := config.SaveTokenType(fs, config.TokenTypePAT)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestDeleteToken(t *testing.T) {
	t.Run("delete existing token", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		if err := config.SaveToken(fs, "ghp_delete_me"); err != nil {
			t.Fatalf("SaveToken: %v", err)
		}

		if err := config.DeleteToken(fs); err != nil {
			t.Fatalf("DeleteToken: %v", err)
		}

		_, err := config.LoadToken(fs)
		if !errors.Is(err, config.ErrTokenNotFound) {
			t.Errorf("LoadToken after delete: got %v, want ErrTokenNotFound", err)
		}
	})

	t.Run("delete non-existent token is not an error", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		if err := config.DeleteToken(fs); err != nil {
			t.Fatalf("DeleteToken on missing file: %v", err)
		}
	})

	t.Run("UserConfigDir error", func(t *testing.T) {
		fs := newFakeFS("")
		fs.configErr = errors.New("no home dir")
		err := config.DeleteToken(fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("Remove error", func(t *testing.T) {
		fs := newFakeFS(t.TempDir())
		fs.removeErr = errors.New("permission denied")
		err := config.DeleteToken(fs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
