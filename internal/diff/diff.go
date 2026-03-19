package diff

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// DiffFetcher fetches the raw unified diff for a pull request.
type DiffFetcher interface {
	Fetch(url, token string) ([]byte, error)
}

// SubprocessRunner runs an interactive subprocess with the given data on stdin.
// In production, stdout and stderr are connected to the terminal so the subprocess
// (e.g. delta pager) can take over the display. The call blocks until the process exits.
type SubprocessRunner interface {
	Run(name string, args []string, stdin []byte) error
}

// BinaryLookup reports the path of the named executable in PATH.
type BinaryLookup interface {
	LookPath(name string) (string, error)
}

// Viewer fetches PR diffs from GitHub and displays them via the delta pager.
// When delta is not installed it falls back to writing the raw diff to an
// io.Writer (os.Stdout by default) along with installation instructions.
type Viewer struct {
	token       string
	fetch       DiffFetcher
	run         SubprocessRunner
	lookup      BinaryLookup
	fallbackOut io.Writer
}

// New creates a Viewer with the given GitHub token and injected dependencies.
// Pass nil for run or lookup to use the OS-backed implementations.
func New(token string, fetch DiffFetcher, run SubprocessRunner, lookup BinaryLookup) *Viewer {
	v := &Viewer{
		token:       token,
		fetch:       fetch,
		run:         run,
		lookup:      lookup,
		fallbackOut: os.Stdout,
	}
	if v.run == nil {
		v.run = &osRunner{}
	}
	if v.lookup == nil {
		v.lookup = &osLookup{}
	}
	return v
}

// ShowDiff fetches the pull request diff from GitHub and displays it via delta.
// If delta is not installed, the raw diff is written to the fallback writer along
// with instructions for installing delta.
func (v *Viewer) ShowDiff(repo string, number int) error {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid repo %q: expected owner/name", repo)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", parts[0], parts[1], number)
	diffContent, err := v.fetch.Fetch(url, v.token)
	if err != nil {
		return fmt.Errorf("fetching diff for %s#%d: %w", repo, number, err)
	}

	deltaPath, err := v.lookup.LookPath("delta")
	if err != nil {
		// delta not found — write raw diff with install instructions.
		_, writeErr := fmt.Fprintf(v.fallbackOut,
			"delta not found — install with: brew install git-delta\n\n%s",
			string(diffContent))
		return writeErr
	}

	if err := v.run.Run(deltaPath, nil, diffContent); err != nil {
		return fmt.Errorf("running delta for %s#%d: %w", repo, number, err)
	}
	return nil
}

// ── HTTP implementation ────────────────────────────────────────────────────────

// HTTPFetcher fetches PR diffs via the real HTTP stack.
type HTTPFetcher struct {
	client *http.Client
}

// NewHTTPFetcher creates an HTTPFetcher using the given HTTP client.
func NewHTTPFetcher(client *http.Client) *HTTPFetcher {
	return &HTTPFetcher{client: client}
}

// Fetch performs a GET request for the PR diff, setting the GitHub diff Accept header.
func (f *HTTPFetcher) Fetch(url, token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.diff")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	return body, nil
}

// ── OS implementations ─────────────────────────────────────────────────────────

// osRunner runs subprocesses connected to the terminal (stdout/stderr pass-through).
type osRunner struct{}

func (r *osRunner) Run(name string, args []string, stdin []byte) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// osLookup uses exec.LookPath to find executables in PATH.
type osLookup struct{}

func (l *osLookup) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
