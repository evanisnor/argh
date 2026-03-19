# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Module

```
github.com/evanisnor/argh
Go 1.22+
```

## Build & Test

```bash
go build ./...
go test -race -coverprofile=coverage.out ./...

# Run a single package
go test -race ./internal/api/

# Run a single test by name
go test -race -run TestPoller ./internal/api/
```

Coverage must not drop below 100% branch and line coverage.

## Runtime Dependencies

`gh` (GitHub CLI, used only for `gh auth token`) and `delta` (diff pager) must be installed. `brew bundle` installs both via the repo `Brewfile`.

## Task Workflow

Tasks are tracked in `plan.yaml`. Work through them one at a time, in order:

1. When implementing a new feature or fix, add a task entry (or entries) to `plan.yaml`. Break work into atomic units — each task should be independently implementable and testable. Tasks may form a dependency tree via `depends_on`.
2. Check that all tasks listed in `depends_on` are already `done` before starting
3. Set the task's `status` to `inprogress` in `plan.yaml`
4. Implement the task fully, including tests as described in the task's `testing` field
5. Set the task's `status` to `done` in `plan.yaml`
6. Commit all changes with a message describing the completed task
7. Push the commit to origin

Do not skip tasks or work on multiple tasks at once.

Valid status values: `pending` | `inprogress` | `done`

## Testing Policy

- **100% branch and line coverage** — no exceptions.
- **Table-driven tests** for any logic with multiple input variants.
- **Interface injection** for all external boundaries — do not call external systems directly from logic under test. Mock via interfaces and fakes.
- **In-memory SQLite** — use `:memory:` as the DSN for all persistence tests. Never write to disk in tests.
- **Clock injection** — any code that calls `time.Now()` must accept a clock interface so tests can control time without sleeping.
- **GOOS injection** — platform detection must accept GOOS as a parameter rather than reading `runtime.GOOS` directly, so it is testable without cross-compilation.

## Dependency Choices

Resolve ambiguities from ARCHITECTURE.md in favor of these:

| Purpose | Package |
|---|---|
| GitHub GraphQL | `shurcooL/githubv4` |
| GitHub REST | `google/go-github` |
| Config (YAML) | `gopkg.in/yaml.v3` |
| Database | `mattn/go-sqlite3` |
| Fuzzy match | `sahilm/fuzzy` |
| macOS notifications | `gen2brain/beeep` |
| TUI | `github.com/charmbracelet/bubbletea` |
| Styling | `github.com/charmbracelet/lipgloss` |
| Components | `github.com/charmbracelet/bubbles` |
| Markdown | `github.com/charmbracelet/glamour` |

## Architecture

argh follows a **reactive, offline-first** pattern. The SQLite database is the single source of truth; the UI never reads from API responses directly.

```
Poller (goroutine, 10s default)
  └─► GitHub GraphQL/REST API
        └─► persistence.DB  ──► eventbus.Bus ──► ui.Model (Bubble Tea)
                                              └─► watches.Engine (goroutine)
                                              └─► notify.Notifier (macOS)
```

- **`internal/api`** — GitHub client, polling loop (`Poller`), rate-limit tracking, ETag caching, GraphQL/REST fetchers.
- **`internal/persistence`** — SQLite wrapper (`DB`), schema, typed read/write methods. DB lives at `~/.local/share/argh/argh.db`. Publishes events to the bus on every write.
- **`internal/eventbus`** — In-process pub/sub (`Bus`). Persistence publishes; UI and Watch Engine subscribe.
- **`internal/ui`** — Bubble Tea root `Model` plus one file per panel (`panel_prs.go`, `panel_reviews.go`, `panel_watches.go`, `panel_detail.go`, `command_bar.go`). All panels implement `SubModel`.
- **`internal/watches`** — `Engine` subscribes to the bus, evaluates triggers, and executes actions (merge, comment, label, notify) via injected interfaces.
- **`internal/notify`** — macOS notification dispatch with debouncing and Do-Not-Disturb support.
- **`internal/config`** — YAML config at `~/.config/argh/config.yaml`; per-repo overrides via `.argh.yaml`.
- **`internal/status`** — Computes the condensed status-line string (used by `argh --status`).
- **`internal/suggest`** — Fuzzy-match autocomplete for the command bar.
- **`internal/diff`** — Shells out to `delta` for syntax-highlighted diffs.
- **`internal/audit`** — Persists watch execution history.
- **`internal/session`** — Stores the authenticated GitHub username.

Each package that needs test helpers has a `testing.go` file (e.g. `api/testing.go`, `diff/testing.go`) with shared fakes/stubs. These are compiled into test binaries only via `//go:build` or by being in `_test` packages where applicable.

## Recurring Patterns

**Interface injection** — every external boundary (GitHub API client, command executor, OS notifier, clock, filesystem) must be defined as an interface and injected as a dependency. Tests provide fakes or stubs; production wires the real implementation in `main.go`.

**SQLite WAL mode** — enable WAL mode immediately after every database open:
```sql
PRAGMA journal_mode=WAL;
```

**macOS-only** — argh is macOS only for v1. The platform guard lives in `cmd/argh/main.go` and accepts GOOS as a parameter. Do not add Windows or Linux support.
