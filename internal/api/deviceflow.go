package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var dcr DeviceCodeResponse
	if err := json.Unmarshal(body, &dcr); err != nil {
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
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("polling token: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
		}

		// Check for error field first (GitHub returns 200 with error payload).
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &errResp); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}

		switch errResp.Error {
		case "":
			// Success — parse the token response.
			var tr TokenResponse
			if err := json.Unmarshal(body, &tr); err != nil {
				return nil, fmt.Errorf("decoding token response: %w", err)
			}
			return &tr, nil

		case "authorization_pending":
			continue

		case "slow_down":
			interval += 5 * time.Second
			continue

		case "expired_token":
			return nil, &DeviceFlowError{Code: errResp.Error, Description: errResp.ErrorDescription}

		case "access_denied":
			return nil, &DeviceFlowError{Code: errResp.Error, Description: errResp.ErrorDescription}

		default:
			return nil, &DeviceFlowError{Code: errResp.Error, Description: errResp.ErrorDescription}
		}
	}
}
