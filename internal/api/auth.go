package api

import (
	"context"
	"fmt"
	"net/url"

	"github.com/evanisnor/argh/internal/config"
	"github.com/google/go-github/v69/github"
	"golang.org/x/oauth2"
)

// TokenVerifier verifies a GitHub PAT and returns the authenticated user's login.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (login string, err error)
}

// GitHubTokenVerifier implements TokenVerifier using the GitHub REST API.
// BaseURL is optional; when empty, the default GitHub API URL is used.
type GitHubTokenVerifier struct {
	BaseURL string // for testing; empty uses github.com
}

// Verify creates an oauth2 client with the given token and fetches the
// authenticated user's login via the GitHub Users API.
func (v *GitHubTokenVerifier) Verify(ctx context.Context, token string) (string, error) {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(ctx, src)
	client := github.NewClient(httpClient)

	if v.BaseURL != "" {
		u, err := url.Parse(v.BaseURL)
		if err != nil {
			return "", fmt.Errorf("parsing base URL: %w", err)
		}
		client.BaseURL = u
	}

	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return "", fmt.Errorf("verifying token: %w", err)
	}

	login := user.GetLogin()
	if login == "" {
		return "", fmt.Errorf("GitHub API returned empty login")
	}

	return login, nil
}

// Credentials holds the authenticated user's token and login.
type Credentials struct {
	Token string
	Login string
}

// Authenticate loads a saved PAT and verifies it against the GitHub API.
// Returns config.ErrTokenNotFound if no token file exists (caller should
// show the setup flow). Returns other errors if the token is invalid.
func Authenticate(ctx context.Context, fs config.Filesystem, verifier TokenVerifier) (*Credentials, error) {
	token, err := config.LoadToken(fs)
	if err != nil {
		return nil, err
	}

	login, err := verifier.Verify(ctx, token)
	if err != nil {
		return nil, err
	}

	return &Credentials{
		Token: token,
		Login: login,
	}, nil
}
