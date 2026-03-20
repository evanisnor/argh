package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceCodeResponse holds the response from the device authorization endpoint.
type DeviceCodeResponse struct {
	DeviceCode      string        `json:"device_code"`
	UserCode        string        `json:"user_code"`
	VerificationURI string        `json:"verification_uri"`
	ExpiresIn       int           `json:"expires_in"`
	Interval        time.Duration `json:"-"`
}

// TokenResponse holds the response from the token endpoint on success.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

// DeviceFlowError represents an error returned by the device flow endpoints.
type DeviceFlowError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *DeviceFlowError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Description)
	}
	return e.Code
}

// HTTPDoer abstracts http.Client.Do for testability.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Sleeper abstracts time.Sleep for testability.
type Sleeper interface {
	Sleep(d time.Duration)
}

// realSleeper uses time.Sleep.
type realSleeper struct{}

func (realSleeper) Sleep(d time.Duration) { time.Sleep(d) }

// RealSleeper returns a Sleeper that delegates to time.Sleep.
func RealSleeper() Sleeper { return realSleeper{} }

// DeviceFlowClient is the interface for the OAuth device flow.
type DeviceFlowClient interface {
	RequestCode(ctx context.Context, clientID string, scopes []string) (*DeviceCodeResponse, error)
	PollToken(ctx context.Context, clientID string, deviceCode string, interval time.Duration) (*TokenResponse, error)
}

// GitHubDeviceFlowClient implements the OAuth device flow against GitHub.
type GitHubDeviceFlowClient struct {
	HTTP    HTTPDoer
	BaseURL string
	Sleeper Sleeper
}

func (c *GitHubDeviceFlowClient) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return "https://github.com"
}

func (c *GitHubDeviceFlowClient) sleeper() Sleeper {
	if c.Sleeper != nil {
		return c.Sleeper
	}
	return realSleeper{}
}

// RequestCode initiates the device authorization flow.
func (c *GitHubDeviceFlowClient) RequestCode(ctx context.Context, clientID string, scopes []string) (*DeviceCodeResponse, error) {
	form := url.Values{
		"client_id": {clientID},
		"scope":     {strings.Join(scopes, " ")},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL()+"/login/device/code", strings.NewReader(form.Encode()))
	if err != nil {
		slog.Error("device flow: creating request", "error", err)
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		slog.Error("device flow: requesting device code", "error", err)
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("device flow: reading response", "error", err)
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		slog.Error("device flow: OAuth app not found", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("GitHub OAuth app not found — verify oauth.client_id in config")
	}
	if resp.StatusCode != http.StatusOK {
		slog.Error("device flow: unexpected status", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var dcr DeviceCodeResponse
	if err := json.Unmarshal(body, &dcr); err != nil {
		slog.Error("device flow: decoding response", "error", err)
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	// GitHub returns interval in seconds; parse it from the raw JSON.
	var raw struct {
		Interval int `json:"interval"`
	}
	if err := json.Unmarshal(body, &raw); err == nil && raw.Interval > 0 {
		dcr.Interval = time.Duration(raw.Interval) * time.Second
	} else if dcr.Interval == 0 {
		dcr.Interval = 5 * time.Second // default per RFC 8628
	}

	return &dcr, nil
}

// PollToken polls for the access token after the user has authorized the device.
func (c *GitHubDeviceFlowClient) PollToken(ctx context.Context, clientID string, deviceCode string, interval time.Duration) (*TokenResponse, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	sleeper := c.sleeper()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		sleeper.Sleep(interval)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		form := url.Values{
			"client_id":   {clientID},
			"device_code": {deviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL()+"/login/oauth/access_token", strings.NewReader(form.Encode()))
		if err != nil {
			slog.Error("device flow: creating token request", "error", err)
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			slog.Error("device flow: polling token", "error", err)
			return nil, fmt.Errorf("polling token: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			slog.Error("device flow: reading token response", "error", err)
			return nil, fmt.Errorf("reading response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			slog.Error("device flow: unexpected token status", "status", resp.StatusCode, "body", string(body))
			return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
		}

		// Check for error field first (GitHub returns 200 with error payload).
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &errResp); err != nil {
			slog.Error("device flow: decoding token response", "error", err)
			return nil, fmt.Errorf("decoding response: %w", err)
		}

		switch errResp.Error {
		case "":
			// Success — parse the token response.
			var tr TokenResponse
			if err := json.Unmarshal(body, &tr); err != nil {
				slog.Error("device flow: decoding token payload", "error", err)
				return nil, fmt.Errorf("decoding token response: %w", err)
			}
			return &tr, nil

		case "authorization_pending":
			slog.Debug("device flow: authorization pending, retrying", "interval", interval)
			continue

		case "slow_down":
			interval += 5 * time.Second
			slog.Debug("device flow: slow down requested", "new_interval", interval)
			continue

		case "expired_token":
			slog.Error("device flow: device code expired", "error", errResp.Error, "description", errResp.ErrorDescription)
			return nil, &DeviceFlowError{Code: errResp.Error, Description: errResp.ErrorDescription}

		case "access_denied":
			slog.Error("device flow: access denied", "error", errResp.Error, "description", errResp.ErrorDescription)
			return nil, &DeviceFlowError{Code: errResp.Error, Description: errResp.ErrorDescription}

		default:
			slog.Error("device flow: unexpected error", "error", errResp.Error, "description", errResp.ErrorDescription)
			return nil, &DeviceFlowError{Code: errResp.Error, Description: errResp.ErrorDescription}
		}
	}
}
