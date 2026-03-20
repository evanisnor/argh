package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrTokenNotFound is returned by LoadToken when the token file does not exist.
var ErrTokenNotFound = errors.New("token not found")

const tokenFileName = "token"
const tokenTypeFileName = "token_type"

// TokenType identifies the authentication method used to obtain the token.
type TokenType string

const (
	TokenTypePAT   TokenType = "pat"
	TokenTypeOAuth TokenType = "oauth"
)

// ConfigDirPath returns the path to the argh config directory
// (~/.config/argh on macOS).
func ConfigDirPath(fs Filesystem) (string, error) {
	return configDirPath(fs)
}

// LoadToken reads the PAT from the token file in the config directory.
// Returns ErrTokenNotFound if the file does not exist, or an error if
// the file exists but is empty.
func LoadToken(fs Filesystem) (string, error) {
	dir, err := configDirPath(fs)
	if err != nil {
		return "", fmt.Errorf("resolving config directory: %w", err)
	}

	data, err := fs.ReadFile(filepath.Join(dir, tokenFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrTokenNotFound
		}
		return "", fmt.Errorf("reading token file: %w", err)
	}

	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file is empty")
	}

	return token, nil
}

// SaveToken writes the PAT to the token file in the config directory,
// creating the directory if needed. The file is written with mode 0600.
func SaveToken(fs Filesystem, token string) error {
	dir, err := configDirPath(fs)
	if err != nil {
		return fmt.Errorf("resolving config directory: %w", err)
	}

	if err := fs.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := fs.WriteFile(filepath.Join(dir, tokenFileName), []byte(token), 0o600); err != nil {
		return fmt.Errorf("writing token file: %w", err)
	}

	return nil
}

// DeleteToken removes the token file from the config directory.
func DeleteToken(fs Filesystem) error {
	dir, err := configDirPath(fs)
	if err != nil {
		return fmt.Errorf("resolving config directory: %w", err)
	}

	if err := fs.Remove(filepath.Join(dir, tokenFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing token file: %w", err)
	}

	return nil
}

// LoadTokenType reads the token type from the token_type file in the config
// directory. Returns TokenTypePAT if the file does not exist (backward compat).
func LoadTokenType(fs Filesystem) (TokenType, error) {
	dir, err := configDirPath(fs)
	if err != nil {
		return "", fmt.Errorf("resolving config directory: %w", err)
	}

	data, err := fs.ReadFile(filepath.Join(dir, tokenTypeFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return TokenTypePAT, nil
		}
		return "", fmt.Errorf("reading token_type file: %w", err)
	}

	val := strings.TrimSpace(string(data))
	if val == "" {
		return TokenTypePAT, nil
	}

	return TokenType(val), nil
}

// SaveTokenType writes the token type to the token_type file in the config
// directory, creating the directory if needed. The file is written with mode 0600.
func SaveTokenType(fs Filesystem, tt TokenType) error {
	dir, err := configDirPath(fs)
	if err != nil {
		return fmt.Errorf("resolving config directory: %w", err)
	}

	if err := fs.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := fs.WriteFile(filepath.Join(dir, tokenTypeFileName), []byte(string(tt)), 0o600); err != nil {
		return fmt.Errorf("writing token_type file: %w", err)
	}

	return nil
}
