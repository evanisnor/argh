# CLAUDE.md — argh

## Module

```
github.com/evanisnor/argh
Go 1.22+
```

## Build & Test

```bash
go build ./...
go test -race -coverprofile=coverage.out ./...
```

Coverage must not drop below 100% branch and line coverage.

## Task Workflow

Tasks are tracked in `plan.yaml`. Before starting a task, set its `status` to `inprogress`. When the implementation and tests are complete and passing, set it to `done`.

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

## Recurring Patterns

**Interface injection** — every external boundary (GitHub API client, command executor, OS notifier, clock, filesystem) must be defined as an interface and injected as a dependency. Tests provide fakes or stubs; production wires the real implementation in `main.go`.

**SQLite WAL mode** — enable WAL mode immediately after every database open:
```sql
PRAGMA journal_mode=WAL;
```

**macOS-only** — argh is macOS only for v1. The platform guard lives in `cmd/argh/main.go` and accepts GOOS as a parameter. Do not add Windows or Linux support.
