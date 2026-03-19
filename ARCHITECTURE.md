# Architecture & Technical Specification

## Project Overview

`argh` is a terminal-native pull request dashboard and automation engine for GitHub, built in Go. It wraps the `gh` CLI and GitHub REST/GraphQL APIs to provide a real-time control panel for pull request workflows.

## Technology Stack

| Layer | Choice | Rationale |
|---|---|---|
| **Language** | Go 1.22+ | Performance, single binary distribution, strong concurrency |
| **TUI Framework** | [Bubble Tea](https://github.com/charmbracelet/bubbletea) | Elm-architecture TUI, excellent ecosystem |
| **Styling** | [Lip Gloss](https://github.com/charmbracelet/lipgloss) | Composable terminal styles, color support |
| **Components** | [Bubbles](https://github.com/charmbracelet/bubbles) | Input, spinner, list, viewport, paginator |
| **Markdown** | [Glamour](https://github.com/charmbracelet/glamour) | Render PR descriptions in terminal |
| **Diff Viewer** | [delta](https://github.com/dandavison/delta) | Beautiful syntax-highlighted diffs |
| **GitHub API** | Native Go Clients | `shurcooL/githubv4` + `google/go-github`; `gh` CLI used only for auth token |
| **Config** | YAML via `gopkg.in/yaml.v3` | Native Go config management; no external dependencies |
| **Database** | SQLite (`mattn/go-sqlite3`) | Reactive persistent cache, complex queries, robust offline state |
| **Fuzzy Match** | `go-fuzz` or `fzf-lib` | Command bar autocomplete |

## High-Level Architecture

The architecture follows a reactive, offline-first approach where the UI is powered entirely by a local SQLite database acting as the single source of truth.

```mermaid
graph TD
    subgraph "argh Process"
        direction TB
        
        Ticker((Poll Ticker<br>10s default))
        API[API Client<br>(gh token)]
        DB[(Cache / DB<br>Source of Truth)]
        UI[Bubble Tea<br>Model/View]
        Watch[Watch Engine]
        Notify[Notifier<br>(macOS only)]
    end
    
    GH[GitHub GraphQL / REST]
    
    Ticker -->|Trigger| API
    API <-->|Fetch / Mutate| GH
    API -->|Write| DB
    DB -->|Emit Events| UI
    DB -->|State Changes| Notify
    UI -->|Render| Terminal
    UI -->|User Actions| Watch
    Watch -->|Execute Actions| API
```

### Data Flow Principles

1.  **Single Source of Truth**: The UI *always* renders the state of the database. It never reads directly from API responses.
2.  **Reactive Updates**: The API client writes to the database. The database layer emits events/updates. The UI subscribes to these updates and re-renders.
3.  **Continuous Persistence**: All data is persisted to disk immediately upon write.
4.  **Offline First**: `argh` is fully functional offline (read-only) using the last known state.
5.  **Concurrency**: Multiple instances can run simultaneously; SQLite handles concurrent access.

## Directory Structure

```
argh/
├── cmd/
│   └── argh/
│       └── main.go       # Entry point
├── internal/
│   ├── api/              # GitHub GraphQL + REST client
│   ├── model/            # Bubble Tea model + update logic
│   ├── view/             # Rendering functions (panels, command bar)
│   ├── watches/          # Watch engine, queue, action executor
│   ├── notify/           # OS notification dispatch (macOS)
│   ├── config/           # Config loading, defaults
│   ├── persistence/      # Reactive cache layer (SQLite)
│   └── diff/             # Delta integration
├── Brewfile              # Dependency pinning
├── VISION.md             # Product Vision & Requirements
├── go.mod
└── go.sum
```

`argh` defaults to a **global view** of all PRs. If run inside a git repository, it prioritizes that repository's context for certain operations but remains a global dashboard. Fork handling prioritizes `upstream` over `origin` if configured.

## UI & Theming

`argh` uses `lipgloss.HasDarkBackground()` to automatically adapt to the terminal's color scheme — no configuration needed.

## GitHub API Strategy

### Query & Mutation
-   **GraphQL Search**: Finds PRs across all repositories (`is:pr author:@me` etc).
-   **GraphQL Query**: Fetches details (reviews, checks) in bulk.
-   **REST API**: Handles mutations (merge, approve, etc) and specific resource actions.
-   **Authentication**: Uses `gh auth token` to obtain credentials—no separate OAuth flow.

### Polling & Rate Limits

GitHub enforces two independent limit systems that both apply:

#### Primary Rate Limit (per hour)
- **5,000 points/hour** for authenticated users (personal access token or `gh` auth)
- Shared across REST and GraphQL combined

#### Secondary Rate Limits (per minute — the real constraint)
- **2,000 points/minute** for GraphQL API endpoint
- **900 points/minute** for REST API endpoints
- No more than 100 concurrent requests

#### Estimating Our Query Cost
A single argh poll query fetches My PRs + Review Queue with nested reviews, check runs, and review requests. Using GitHub's formula (sum of connection requests ÷ 100, minimum 1):

| Scenario | Connections | Est. Cost |
|---|---|---|
| 10 PRs × (reviews + checks + reviewRequests) | ~31 | **1 point** |
| 30 PRs × (reviews + checks + reviewRequests) | ~91 | **1 point** |
| 50 PRs × deeply nested connections | ~251 | **3 points** |

> The minimum GraphQL query cost is **1 point**. For a typical argh user with <30 open/review PRs, every poll costs **1 point**.

#### Budget Calculation (typical user, 1pt/poll)

| Budget Slice | Allocation | Available polls |
|---|---|---|
| Polling (read) | 4,000 pts/hr | 4,000 polls/hr |
| Mutations (approve, merge, comment) | 500 pts/hr | 500 actions/hr |
| Safety headroom | 500 pts/hr | — |

4,000 polls/hour = **~1 poll per second** is theoretically safe against the primary limit.
The secondary limit (2,000 pts/min for GraphQL) caps this at **2,000 polls/minute**.

#### Chosen Poll Interval: **10 seconds** (default)

| Interval | Polls/hr | Points used/hr | % of primary limit |
|---|---|---|---|
| 10s **(default)** | 360 | 360 | **7.2%** |
| 30s | 120 | 120 | 2.4% |
| 60s | 60 | 60 | 1.2% |

**10 seconds** gives near-real-time responsiveness while consuming only ~7% of the hourly budget, leaving ample headroom for user-triggered mutations and any other `gh` usage the user has running. Configurable in `~/.config/argh/config.yaml`.

#### Adaptive Back-Off
argh reads the `x-ratelimit-remaining` and `x-ratelimit-reset` response headers on every request and applies automatic back-off:

```
remaining > 1000  → poll at configured interval (default 10s)
remaining 500–999 → poll at 2× interval (20s default)
remaining 100–499 → poll at 5× interval (50s default)
remaining < 100   → pause polling, show warning in status bar, resume at reset time
```

The status bar displays current rate limit health: `API ●●●○ 3,847/5,000`.

#### REST Conditional Requests
Where using REST endpoints (e.g., PR details, check runs), argh uses `If-None-Match` (ETag) and `If-Modified-Since` headers. A **304 Not Modified** response costs **0 points** against the primary limit and counts as only 1 point against secondary limits — effectively free polling when nothing has changed.

## Distribution & Dependencies

### Homebrew Formula

`argh` will be distributed via a custom Homebrew tap: `evanisnor/tap`.

**Formula highlights:**
- Single Go binary, cross-compiled for macOS arm64 and amd64.
- Declares runtime dependencies on `gh` and `delta`.
- Post-install message guides user through `gh auth login` if not already authenticated.

### Brewfile

A `Brewfile` in the repo root will pin all required dependencies:

```ruby
# Brewfile
tap "evanisnor/tap"

# Runtime dependencies
brew "gh"                           # GitHub CLI — authentication and fallback operations
brew "dandavison/delta/git-delta"   # Syntax-highlighted diff pager

# The app itself (once formula is published)
brew "evanisnor/tap/argh"
```

### Installation Flow

```bash
# Install dependencies + argh in one step
brew bundle

# Authenticate with GitHub (one-time)
gh auth login

# Launch
argh
```

### Build & Release

-   **CI**: GitHub Actions workflow builds and cross-compiles on tag push. Includes version number from tag in the build.
-   **Release**: `goreleaser` cross-compiles for macOS (arm64/amd64) and creates GitHub Releases.
-   **Tap Update**: Update `https://github.com/evanisnor/homebrew-tap` to include the release (cloned repos available in `/Users/evan/Code/github.com/evanisnor`).
