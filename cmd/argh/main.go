package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-github/v69/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/audit"
	"github.com/evanisnor/argh/internal/config"
	"github.com/evanisnor/argh/internal/diff"
	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/ghcli"
	"github.com/evanisnor/argh/internal/notify"
	"github.com/evanisnor/argh/internal/persistence"
	"github.com/evanisnor/argh/internal/status"
	"github.com/evanisnor/argh/internal/suggest"
	"github.com/evanisnor/argh/internal/ui"
	"github.com/evanisnor/argh/internal/watches"
)

// Version is set at build time via ldflags.
var Version = "dev"

// osExit is a variable so tests can intercept os.Exit calls.
var osExit = os.Exit

// tuiLauncher is the function that starts the full TUI application.
// It is a variable so tests can replace it without requiring GitHub authentication.
var tuiLauncher = func(ctx context.Context, version string) error {
	return runTUI(ctx, version, productionDeps())
}

// teaRun creates and runs the Bubble Tea program. It is a variable so tests can
// override it without needing a real terminal.
var teaRun = func(m tea.Model) error {
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// randRead is the function used to generate random bytes in newWatchID.
// It is a variable so tests can inject a failing implementation.
var randRead = rand.Read

// debugLogMkdirAll and debugLogOpenFile are variables so tests can inject
// failures when exercising the debug log setup.
var debugLogMkdirAll = os.MkdirAll
var debugLogOpenFile = func(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(name, flag, perm)
}

// checkPlatform returns an error if goos is not "darwin".
// goos is accepted as a parameter so tests can inject values without
// cross-compilation.
func checkPlatform(goos string) error {
	if goos != "darwin" {
		return fmt.Errorf("argh v1 is macOS only")
	}
	return nil
}

// hasArg reports whether flag appears in args.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func run(ctx context.Context, out io.Writer, errOut io.Writer, goos string, args []string) int {
	if err := checkPlatform(goos); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if hasArg(args, "--status") {
		return runStatus(out, errOut, persistence.OSFilesystem{}, time.Now,
			func(fs persistence.Filesystem) (status.Reader, error) {
				return persistence.Open(fs)
			})
	}
	if err := tuiLauncher(ctx, Version); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

// runStatus reads PR state from the DB and prints the condensed status line.
// openDB is injected for testability.
func runStatus(
	out io.Writer,
	errOut io.Writer,
	fs persistence.Filesystem,
	now func() time.Time,
	openDB func(persistence.Filesystem) (status.Reader, error),
) int {
	dbPath, err := persistence.DBPath(fs)
	if err != nil {
		fmt.Fprintln(out, "argh: no data")
		return 0
	}
	if _, err := fs.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintln(out, "argh: no data")
		return 0
	} else if err != nil {
		fmt.Fprintf(errOut, "argh: %v\n", err)
		return 1
	}

	r, err := openDB(fs)
	if err != nil {
		fmt.Fprintf(errOut, "argh: cannot open db: %v\n", err)
		return 1
	}
	defer r.Close()

	line, err := status.Compute(r, now)
	if err != nil {
		fmt.Fprintf(errOut, "argh: %v\n", err)
		return 1
	}
	fmt.Fprintln(out, line.String())
	return 0
}

// tuiDeps groups all injectable boundaries for runTUI so the full startup
// sequence can be exercised in tests without real GitHub credentials or disk I/O.
type tuiDeps struct {
	authenticate  func(ctx context.Context) (*api.Credentials, error)
	runSetup      func(ctx context.Context) (ui.SetupResult, error)
	saveToken     func(token string) error
	saveTokenType func(tt config.TokenType) error
	deleteToken   func() error
	loadTokenType func() (config.TokenType, error)
	loadConfig    func() (config.Config, error)
	openDB        func() (*persistence.DB, error)
	auditLogPath  func() (string, error)
	debugLogPath  func() (string, error)
	newTicker     api.NewTickerFunc // nil → api.NewRealTicker
	runProgram    func(m tea.Model) error
}

// setupVerify and setupRunProgram are the functions used by the setup modal in
// production. They are variables so tests can exercise them for coverage.
var setupVerify = func(ctx context.Context, token string) (string, error) {
	return (&api.GitHubTokenVerifier{}).Verify(ctx, token)
}

var setupRunProgram = func(m tea.Model) (tea.Model, error) {
	return tea.NewProgram(m, tea.WithAltScreen()).Run()
}

var setupGHCLIVerify = func(ctx context.Context) (string, error) {
	v := &ghcli.GHCLIAuthVerifier{Runner: &ghcli.ExecRunner{}}
	return v.Verify(ctx, "")
}

// productionDeps returns a tuiDeps wired to real OS resources.
// It is a variable so tests can override it without touching the real filesystem.
var productionDeps = func() tuiDeps {
	cfg, _ := config.Load(config.OSFilesystem{})
	browser := &osBrowserOpener{}
	return tuiDeps{
		authenticate: func(ctx context.Context) (*api.Credentials, error) {
			tt, _ := config.LoadTokenType(config.OSFilesystem{})
			if tt == config.TokenTypeGHCLI {
				return api.Authenticate(ctx, config.OSFilesystem{}, &ghcli.GHCLIAuthVerifier{Runner: &ghcli.ExecRunner{}})
			}
			return api.Authenticate(ctx, config.OSFilesystem{}, &api.GitHubTokenVerifier{})
		},
		runSetup: func(ctx context.Context) (ui.SetupResult, error) {
			return ui.RunSetup(ctx, ui.SetupDeps{
				Verify:     setupVerify,
				RunProgram: setupRunProgram,
				DeviceFlow: &api.GitHubDeviceFlowClient{
					HTTP:    &http.Client{Timeout: 30 * time.Second},
					Sleeper: api.RealSleeper(),
				},
				OpenBrowser: browser.Open,
				ClientID:    cfg.OAuth.ClientID,
				Scopes:      []string{"repo", "read:org"},
				GHCLIVerify: setupGHCLIVerify,
			})
		},
		saveToken: func(token string) error {
			return config.SaveToken(config.OSFilesystem{}, token)
		},
		saveTokenType: func(tt config.TokenType) error {
			return config.SaveTokenType(config.OSFilesystem{}, tt)
		},
		deleteToken: func() error {
			return config.DeleteToken(config.OSFilesystem{})
		},
		loadTokenType: func() (config.TokenType, error) {
			return config.LoadTokenType(config.OSFilesystem{})
		},
		loadConfig: func() (config.Config, error) {
			return config.Load(config.OSFilesystem{})
		},
		openDB: func() (*persistence.DB, error) {
			return persistence.Open(persistence.OSFilesystem{})
		},
		auditLogPath: audit.DefaultLogPath,
		debugLogPath: debugLogDefaultPath,
		newTicker:    nil, // use api.NewRealTicker
		runProgram:   teaRun,
	}
}

// execOpen is the function used to open a URL via the system browser.
// It is a variable so tests can replace it without launching Finder.
var execOpen = func(url string) error {
	return exec.Command("open", url).Run()
}

// osBrowserOpener opens a URL using the macOS open(1) command.
type osBrowserOpener struct{}

func (o *osBrowserOpener) Open(url string) error {
	return execOpen(url)
}

// newWatchID returns a random 16-hex-character string for use as a watch ID.
func newWatchID() string {
	b := make([]byte, 8)
	if _, err := randRead(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

// debugLogDefaultPath returns the default debug log path:
// ~/.local/share/argh/debug.log
func debugLogDefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "argh", "debug.log"), nil
}

// openDebugLog opens (or creates/appends) the debug log file, creating parent
// directories as needed. It uses debugLogMkdirAll and debugLogOpenFile so
// tests can inject failures.
func openDebugLog(path string) (io.WriteCloser, error) {
	if err := debugLogMkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating debug log directory: %w", err)
	}
	return debugLogOpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

// runTUI wires all application components and starts the Bubble Tea program.
func runTUI(parentCtx context.Context, version string, deps tuiDeps) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// ── Debug logger (best-effort) ────────────────────────────────────────────

	if logPath, err := deps.debugLogPath(); err == nil {
		if w, err := openDebugLog(logPath); err == nil {
			slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer w.Close()
		}
	}

	creds, err := deps.authenticate(ctx)
	if err != nil {
		needsSetup := errors.Is(err, config.ErrTokenNotFound)
		if !needsSetup {
			_ = deps.deleteToken()
			needsSetup = true
		}
		if needsSetup {
			result, setupErr := deps.runSetup(ctx)
			if setupErr != nil {
				return fmt.Errorf("setup: %w", setupErr)
			}
			if result.Quit {
				return nil
			}
			if err := deps.saveToken(result.Token); err != nil {
				return fmt.Errorf("saving token: %w", err)
			}
			if err := deps.saveTokenType(result.TokenType); err != nil {
				return fmt.Errorf("saving token type: %w", err)
			}
			creds, err = deps.authenticate(ctx)
			if err != nil {
				return fmt.Errorf("authentication after setup: %w", err)
			}
		}
	}

	cfg, err := deps.loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	db, err := deps.openDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	auditPath, err := deps.auditLogPath()
	if err != nil {
		return fmt.Errorf("resolving audit log path: %w", err)
	}

	// ── Event bus ────────────────────────────────────────────────────────────

	bus := eventbus.New()
	defer bus.Shutdown()

	// ── Audit logger ─────────────────────────────────────────────────────────

	auditLogger := audit.New(auditPath)

	// ── Backend selection ────────────────────────────────────────────────────

	tokenType, _ := deps.loadTokenType()

	var (
		myPRsFetcher  api.Fetcher
		reviewFetcher api.Fetcher
		rateLimiter   api.RateLimitReader
		mutator       ui.PRMutator
		resolver      ui.ThreadResolver
		actionExec    watches.ActionExecutor
		diffViewer    *diff.Viewer
	)

	if tokenType == config.TokenTypeGHCLI {
		runner := &ghcli.ExecRunner{}
		ghMutator := &ghcli.GHCLIMutator{Runner: runner, Audit: auditLogger}

		myPRsFetcher = ghcli.NewGHCLIMyPRsFetcher(runner, db, bus, creds.Login)
		reviewFetcher = ghcli.NewGHCLIReviewQueueFetcher(runner, db, bus, creds.Login)
		rateLimiter = &ghcli.FixedRateLimitReader{}
		mutator = ghMutator
		resolver = ghMutator
		actionExec = ghMutator
		diffViewer = diff.New("", &ghcli.GHCLIDiffFetcher{Runner: runner}, nil, nil)
	} else {
		rateLimitTracker := api.NewRateLimitTracker(db, bus)
		rateLimiter = rateLimitTracker

		src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: creds.Token})
		httpClient := oauth2.NewClient(ctx, src)

		ssoObserver := api.NewBrowserSSOObserver(bus, execOpen)
		httpClient.Transport = api.NewSSOTransport(httpClient.Transport, ssoObserver)

		gqlClient := githubv4.NewClient(httpClient)
		restClient := github.NewClient(httpClient)

		myPRsFetcher = api.NewMyPullRequestsFetcher(gqlClient, db, bus, creds.Login)
		reviewFetcher = api.NewReviewQueueFetcher(gqlClient, db, bus, creds.Login)
		apiMutator := api.NewMutator(restClient.PullRequests, restClient.Issues, gqlClient, auditLogger)
		mutator = apiMutator
		resolver = apiMutator
		actionExec = apiMutator
		diffViewer = diff.New(creds.Token, diff.NewHTTPFetcher(httpClient), nil, nil)
	}

	// ── Poller ───────────────────────────────────────────────────────────────

	newTicker := deps.newTicker
	if newTicker == nil {
		newTicker = api.NewRealTicker
	}
	pollInterval := cfg.PollInterval.Duration
	if tokenType == config.TokenTypeGHCLI && pollInterval < 30*time.Second {
		pollInterval = 30 * time.Second
	}
	poller := api.NewPoller(myPRsFetcher, reviewFetcher, rateLimiter,
		pollInterval, newTicker)
	if len(cfg.SleepSchedule.Windows) > 0 {
		sleep := api.NewSleepSchedule(cfg.SleepSchedule.Windows,
			cfg.SleepSchedule.PollInterval.Duration, api.RealClock{})
		poller.SetSleepSchedule(sleep)
	}

	// ── Do Not Disturb ───────────────────────────────────────────────────────

	dnd := notify.NewDNDManager(cfg.DoNotDisturb.Schedule, notify.RealClock{})

	// ── System notifications ─────────────────────────────────────────────────

	debouncer := notify.NewDebouncer(notify.RealClock{})
	sender := notify.NewBeeepSender()
	notifier := notify.New(bus, sender, cfg.Notifications, dnd, creds.Login, debouncer)
	defer notifier.Close()

	// ── Watches ──────────────────────────────────────────────────────────────

	watchManager := watches.NewManager(db, time.Now, newWatchID)
	watchEngine := watches.NewEngine(db, db, actionExec, sender, bus, auditLogger, time.Now)

	// ── UI panels ────────────────────────────────────────────────────────────

	myPRsPanel := ui.NewMyPRsPanel(db)
	reviewQueuePanel := ui.NewReviewQueuePanel(db, creds.Login)
	watchesPanel := ui.NewWatchesPanel(db)
	detailPane := ui.NewDetailPane(resolver)
	commandBar := ui.NewCommandBar()

	// ── Command executor ─────────────────────────────────────────────────────

	executor := ui.NewCommandExecutor(ui.CommandExecutorConfig{
		Mutator:   mutator,
		Store:     db,
		Poll:      poller,
		Browser:   &osBrowserOpener{},
		Diff:      diffViewer,
		DND:       dnd,
		Watches:   watchManager,
		Suggester: &suggest.Suggester{},
	})
	commandBar.SetExecutor(executor)

	// ── Root model ───────────────────────────────────────────────────────────

	model := ui.New(version, creds.Login, bus,
		myPRsPanel, reviewQueuePanel, watchesPanel, detailPane, commandBar).
		WithDNDToggler(dnd)

	// ── Launch background goroutines ─────────────────────────────────────────

	pollerDone := poller.Start(ctx)
	go watchEngine.Run(ctx)

	// ── Run the Bubble Tea program ───────────────────────────────────────────

	programErr := deps.runProgram(model)

	// Cancel context to stop goroutines, then wait for the poller to finish.
	cancel()
	<-pollerDone

	return programErr
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	osExit(run(ctx, os.Stdout, os.Stderr, runtime.GOOS, os.Args[1:]))
}
