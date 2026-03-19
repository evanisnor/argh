# argh — Product Requirements Document

> **/ɑːrɡ/** — the sound you make when you realize your PR has been sitting in review for three days and CI is red again.*

`argh` is a terminal-native pull request dashboard and automation engine for GitHub, built in Go. It wraps the `gh` CLI and GitHub REST/GraphQL APIs to give engineers a living, reactive control panel for their pull request workflow — without ever leaving the terminal.

---

## Table of Contents

1. [Goals & Non-Goals](#1-goals--non-goals)
2. [User Personas](#2-user-personas)
3. [Feature Specification](#3-feature-specification)
4. [Wireframe Layout](#4-wireframe-layout)
5. [Interaction Model & Commands](#5-interaction-model--commands)
6. [Watches](#6-watches)
7. [Notification System](#7-notification-system)
8. [Architecture & Technical Spec](ARCHITECTURE.md)
9. [Additional Features](#9-additional-features)

---

## 1. Goals & Non-Goals

### Goals
- Give engineers a **real-time global pull request command center** in the terminal, aggregating PRs from **all repositories**.
- Surface the **right information at the right time** — eliminate context switching to the GitHub web UI.
- Enable **watches** that trigger actions on PR state changes.
- **Grab attention visually and via system notifications** when PR state changes (CI passes/fails, review requested, approved, etc.).
- Stay **composable** with existing `gh` CLI workflows.

### Non-Goals
- Full GitHub project management (issues, milestones, releases) — out of scope.
- Git write operations (commit, push, rebase) — delegate to the user's existing tooling. Read-only Git operations (e.g., `git blame`) are permitted where useful (see §9.2).
- Support for GitLab, Bitbucket, or other providers.
- A web UI or Electron wrapper.
- Windows or Linux support (v1 is macOS only; enforced by runtime check).
- Execution of arbitrary local shell commands in automation (security risk).

---

## 2. User Personas

| Persona | Context | Primary Need |
|---|---|---|
| **Author** | Has 2–5 open PRs | Track CI status, review state, quickly act when unblocked |
| **Reviewer** | Receives review requests from teammates | See what needs attention, approve or comment without leaving terminal |

---

## 3. Feature Specification

### 3.1 My Pull Requests Panel

Displays all non-closed pull requests authored by the current user across **all repositories**.

**Columns:**
| Field | Description |
|---|---|
| `id` | Short local session ID (e.g. `a`, `b`, `c`) — for fast command bar reference |
| 👁 | Eye icon shown when the PR has active watches |
| Repo | Repository name (e.g. `owner/repo`) |
| `#` | PR number (linkable) |
| Title | PR title, truncated to available width |
| Status | `draft`, `open`, `approved`, `changes requested`, `merge queued` |
| CI | Aggregated check state: `✓ passing`, `✗ failing`, `⟳ running`, `— none` |
| Reviews | Requested reviewers with per-person status icon |
| Comments | Total unresolved comment count |
| Age | Relative time since last state change (e.g., `3h`, `2d`) |

**Behaviors:**
- Rows are sorted by staleness (longest idle time first).
- Rows with failing CI or blocking reviews are visually highlighted.
- Draft PRs are visually distinct (dimmed or prefixed with `[draft]`).
- Rows animate or flash briefly when any field changes.
- PRs with one or more active watches show a `👁` icon to the left of the title.

---

### 3.2 Review Queue Panel

Displays pull requests where the current user is assigned as a reviewer, or is mentioned in an open review request, across **all repositories**.

**Columns:**
| Field | Description |
|---|---|
| `id` | Short local session ID (e.g. `a`, `b`, `c`) — for fast command bar reference |
| 👁 | Eye icon shown when the PR has active watches |
| Repo | Repository name |
| `#` | PR number |
| Title | PR title |
| Author | GitHub username of PR author |
| CI | Aggregated check state |
| Age | Relative time since last state change |
| Urgency | Derived from: staleness + CI state + author wait time (see formula below) |

**Urgency Formula (3 tiers):**

Urgency is calculated as a base staleness tier adjusted by CI state and author wait time, clamped to `[1, 3]`:

| Step | Signal | Rule |
|---|---|---|
| Base | Staleness (time since last PR activity) | <4h → 1 · 4–24h → 2 · >24h → 3 |
| Modifier | CI state | Failing → +1 · Passing → -1 · Running/None → ±0 |
| Modifier | Author wait time | Author waiting >72h → +1 |

| Score | Display | Meaning |
|---|---|---|
| 3 | `●●●` | High urgency — act soon |
| 2 | `●●○` | Medium urgency |
| 1 | `●○○` | Low urgency |

**Behaviors:**
- Sorted by urgency score (descending).
- PRs where you are the last required reviewer are highlighted.
- PRs with passing CI and no blockers are highlighted as "ready to review".

---

### 3.3 Detail / Preview Pane

A collapsible side or bottom pane showing extended details for the focused PR.

**Contains:**
- Full PR description (rendered markdown via Glamour)
- Check runs list with individual CI job names and states
- Review thread summary (open threads, resolved threads)
- Active watches for this PR
- Recent timeline events (commits pushed, reviews submitted, labels added)

---

### 3.4 Watches Panel

Always-visible third panel below the Review Queue. Shows all active watches across all PRs.

**Columns:**
| Field | Description |
|---|---|
| `id` | Watch ID — for use with `:watch cancel [id]` |
| `#` | PR number the watch applies to |
| Trigger | The condition being waited on (e.g. `on:approved+ci`, `on:ci-pass`) |
| Action | What will happen when the trigger fires |
| Status | `⟳ waiting`, `⟳ scheduled`, `✓ fired`, `✗ failed` |

**Behaviors:**
- Rows flash briefly when a watch fires.
- Completed or failed watches remain visible briefly then fade out.
- Panel is hidden (collapsed) when there are no active watches.

---

### 3.5 Command Bar

A persistent horizontal input bar pinned to the bottom of the screen.

**Features:**
- Fuzzy autocomplete for commands (`:approve`, `:merge`, `:open`, `:diff`, `:review`, `:watch`, etc.)
- Context-aware parameter completion: **local session IDs** (`a`, `b`, `c`…), PR numbers, or PR title fragments — whichever the user starts typing
- Local IDs are stable for the session, reassigned on restart, and shown in both panels
- History navigation with ↑/↓
- Inline help hint showing command signature as you type
- `/` or `:` prefix to enter command mode from anywhere

---

### 3.6 Interactive Commands

| Command | Description |
|---|---|
| `:open [#pr]` | Open PR in default browser |
| `:diff [#pr]` | Show diff in terminal using `delta` pager |
| `:approve [#pr]` | Submit approval review |
| `:review [#pr]` | Open review compose view (comment body + submit) |
| `:request [#pr] @user...` | Request review from one or more users |
| `:ready [#pr]` | Mark draft PR as ready for review |
| `:draft [#pr]` | Convert PR back to draft |
| `:merge [#pr]` | Merge PR using the repo's configured merge method |
| `:watch [#pr]` | Add watch for PR |
| `:close [#pr]` | Close PR without merging |
| `:reopen [#pr]` | Reopen a closed PR |
| `:label [#pr] [label]` | Add or remove a label |
| `:comment [#pr]` | Open inline editor to post a comment |
| `:dnd [duration]` | Toggle Do Not Disturb mode; optional duration e.g. `:dnd 2h` |
| `:wake` | Resume normal polling immediately if in a sleep window |
| `:reload` | Force refresh all data |
| `:help` | Show command reference overlay |
| `:quit` / `q` | Exit argh |

Commands act immediately with no confirmation prompt. Every action is recorded in the audit log (`~/.local/share/argh/audit.log`).

---

### 3.7 Watch (Automation Queue Management)

A rule-based engine that watches PR state and triggers actions automatically or on-demand.

**Trigger types:**
- CI checks all pass
- PR approved by N reviewers
- All review threads resolved
- PR marked ready for review
- Label added/removed
- Time-based (e.g., "if no review after 24h, ping reviewers")

**Action types:**
- Mark draft ready for review
- Add to merge queue / merge
- Request review from team/individual
- Post a comment
- Add a label
- Desktop notification

**Trigger Syntax:**

Triggers are composed using two combinators:
- `+` — AND: all conditions must be true simultaneously
- `,` — OR: any condition triggers the watch

```
on:approved+ci         # fires when approved AND CI passes
on:ci-pass,approved    # fires when CI passes OR when approved
on:ci-pass             # fires on CI pass alone
on:24h-stale           # fires after 24h with no activity
```

**Queue Management:**
- `:watch [#pr] on:approved merge:squash` — create a merge watch when approved
- `:watch [#pr] on:ci-pass ready` — mark ready when CI passes
- `:watch list` — show all active watches
- `:watch cancel [id]` — cancel an active watch
- Active watches persist across restarts (stored in `~/.config/argh/watches.yaml`). Persistence uses stable PR URLs or global IDs, mapped back to ephemeral session IDs at runtime.

---

## 4. Wireframe Layout

Three panels stack vertically: **My Pull Requests**, **Review Queue**, and **Watches**. The command bar is pinned to the bottom.

```
┌──────────────────────────────────────────────────────────────────────────┐
│  argh v0.1.0  @evanisnor  ●  GLOBAL DASHBOARD                   [?] help │
├──────────────────────────────────────────────────────────────────────────┤
│  MY PULL REQUESTS                                               [3 open]  │
├────┬───┬──────────────┬────┬────────────────────────┬──────────┬────┬────┤
│ id │ 👁 │ Repo         │ #  │ Title                  │ Status   │ CI │ Age│
├────┼───┼──────────────┼────┼────────────────────────┼──────────┼────┼────┤
│  a │ 👁 │ owner/repo-a │ 42 │ feat: add oauth flow   │ approved │ ✓  │ 2h │
│  b │   │ owner/repo-b │ 38 │ fix: null ptr          │ open     │ ✗  │ 1d │
│  c │   │ work/api     │ 31 │ [draft] wip: parser    │ draft    │ ⟳  │ 4d │
├──────────────────────────────────────────────────────────────────────────┤
│  REVIEW QUEUE                                                 [2 waiting] │
├────┬───┬──────────────┬────┬────────────────────────┬──────────┬────┬────┤
│ id │ 👁 │ Repo         │ #  │ Title                  │ Author   │ CI │ Urg│
├────┼───┼──────────────┼────┼────────────────────────┼──────────┼────┼────┤
│  d │   │ oss/library  │ 55 │ chore: bump deps       │ @carol   │ ✓  │ ●●●│
│  e │   │ work/ui      │ 51 │ feat: dark mode        │ @dave    │ ⟳  │ ●●○│
├──────────────────────────────────────────────────────────────────────────┤
│  WATCHES                                                       [3 active] │
├──────┬──────────────┬────┬───────────────────┬───────────────────┬───────┤
│  id  │ Repo         │ #  │ Trigger           │ Action            │ Status│
├──────┼──────────────┼────┼───────────────────┼───────────────────┼───────┤
│  1   │ owner/repo-a │ 42 │ on:approved+ci    │ merge             │ ⟳ wait│
│  2   │ work/api     │ 31 │ on:ci-pass        │ ready-for-review  │ ⟳ wait│
│  3   │ owner/repo-b │ 38 │ on:24h-stale      │ comment + notify  │ ⟳ schd│
├──────────────────────────────────────────────────────────────────────────┤
│  > :                                           [tab: complete] [↑: hist] │
└──────────────────────────────────────────────────────────────────────────┘
```

**Navigation:** `j`/`k` or `↑`/`↓` to move focus within a panel, `Tab` to switch panels, `Enter` to expand detail pane, `/` or `:` to focus command bar.

---

## 5. Interaction Model & Commands

### Keyboard Navigation (Global)

| Key | Action |
|---|---|
| `j` / `↓` | Move focus down |
| `k` / `↑` | Move focus up |
| `Tab` | Switch between panels |
| `Enter` | Expand/focus selected PR (detail pane) |
| `/` or `:` | Focus command bar |
| `Esc` | Dismiss overlay / unfocus command bar |
| `o` | Open focused PR in browser |
| `d` | Show diff for focused PR |
| `a` | Approve focused PR (if in review queue) |
| `r` | Request review (opens reviewer picker) |
| `?` | Toggle help overlay |
| `q` | Quit |
| `R` | Force reload/refresh |
| `D` | Toggle Do Not Disturb |
| `p` | Toggle detail pane |

### Local Session IDs

Every PR visible in either panel is assigned a short alphabetic local ID (`a`–`z`, then `aa`, `ab`, …) at startup and on each reload. IDs are displayed as the first column in both panels and are the fastest way to reference a PR in the command bar.

```
:approve a       ← approve PR with local ID "a"
:diff b          ← show diff for local ID "b"
:merge a         ← merge local ID "a"
```

Local IDs also accept PR numbers (`#42`) and fuzzy title fragments as fallbacks, so users familiar with the PR number can use either form.

### Command Bar Autocomplete Behavior

1. User types `:` — command list appears above the bar.
2. User types partial command (`:mer`) — fuzzy filtered to `:merge`, `:mergequeue`.
3. After command is selected, PR completion activates: local ID (`a`), PR number (`#42`), or title fragment — all accepted.
4. PR list is filtered from the currently visible panels.
5. For `:request`, `@` triggers user autocomplete from repo collaborators.
6. `Tab` accepts the top suggestion; `↑`/`↓` navigates suggestions.

---

## 6. Watches

Watches are created interactively via the `:watch` command and persisted in `~/.config/argh/watches.yaml`. No rules DSL or config file editing required. The Watches panel is always visible in the main UI as the third panel.

---

## 7. Notification System

### In-App Visual Notifications

- Row flash/highlight animation when a PR's state changes.
- Badge counters in panel headers update immediately.
- Status bar at top shows most recent event: `● #42 approved by @alice — 10s ago`.
- Color coding: green (positive/approved/passing), red (failing/changes requested), yellow (pending/waiting), blue (info).

### System Notifications (OS-level)

Triggered for high-signal events:
- CI transitions from running → passing or failing
- Review approval received
- Review with changes requested received
- PR merged or closed
- You are requested as a reviewer on a new PR
- Automation action executed

Uses the macOS Notification Center via a Go native library (no external `terminal-notifier` dependency required). Linux/Windows support planned for future versions. Configurable per-event in `~/.config/argh/config.yaml`.

```yaml
notifications:
  ci_pass: true
  ci_fail: true
  approved: true
  changes_requested: true
  review_requested: true
  merged: true
  watch_triggered: false
```

### Notification Deduplication

Events are debounced with a 5s window. Repeated state flapping (CI pass→fail→pass within 60s) is collapsed into a single notification.

---

## 8. Architecture & Technical Spec

For detailed architecture, data flow, and technical specifications, see [ARCHITECTURE.md](ARCHITECTURE.md).

---

## 9. Additional Features

### 9.1 PR Status Bar Overlay (`argh --status`)
A one-line tmux/terminal status bar output mode: prints a condensed summary (counts, CI state) suitable for embedding in tmux status bar or shell prompt. Reads state directly from the local SQLite database — **the main `argh` process must be running** to keep the database current. Outputs stale data gracefully if the database exists but polling has stopped.

```bash
# In .tmux.conf
set -g status-right '#(argh --status)'
# Output: ↑3 PRs  ✗1 CI  ↓2 review
```

### 9.2 Smart Review Assignment
When running `:request #pr`, show a ranked list of suggested reviewers based on:
- Who owns the most lines changed (via `git blame` on the local repo — used read-only if available)
- Who reviewed similar PRs recently (via GitHub API history)
- Team CODEOWNERS rules (via GitHub API)

All three signals are optional and used when available; the command works without any of them.

### 9.3 Inline Comment Thread Browser
In the detail pane, navigate through open review threads with `n`/`N`. Mark threads as resolved without opening the browser.

### 9.4 Per-Repo Configuration
Support a `.argh.yaml` in the repo root for repo-specific overrides: default reviewers, label conventions, merge strategy preference.

### 9.5 Audit Log
Every action `argh` takes (approve, merge, request, comment) is appended to `~/.local/share/argh/audit.log` with timestamp and PR number. Makes it easy to understand what the watch did.

### 9.6 Do Not Disturb Mode
Suppress all system notifications without stopping polling or watches. Useful during deep work, meetings, or outside working hours.

- Toggle with `:dnd` or the keyboard shortcut `D` — status bar shows `🔕 DND` when active
- Timed DND: `:dnd 2h` re-enables notifications automatically after the specified duration
- In-app visual alerts (row flashes, badge counts) continue as normal — only OS-level notifications are suppressed
- Configurable scheduled DND in `~/.config/argh/config.yaml`:

```yaml
do_not_disturb:
  schedule:
    - days: [monday, tuesday, wednesday, thursday, friday]
      from: "18:00"
      to: "09:00"
    - days: [saturday, sunday]
      all_day: true
```

### 9.7 Sleep Schedule
Reduce polling frequency during off-hours to avoid burning API budget and unnecessary background activity overnight and on weekends. Distinct from DND — sleep affects polling rate, not notifications.

- During sleep hours, polling slows to a configurable reduced interval (default: **5 minutes**)
- Status bar shows `💤 sleeping (next poll in 4m)` when in a sleep window
- `:wake` command immediately resumes normal polling ahead of schedule
- Configured in `~/.config/argh/config.yaml`:

```yaml
sleep_schedule:
  poll_interval: 5m   # polling interval during sleep windows
  windows:
    - days: [monday, tuesday, wednesday, thursday, friday]
      from: "19:00"
      to: "08:00"
    - days: [saturday, sunday]
      all_day: true
```

At the default 10s poll interval, argh uses ~360 pts/hr. During sleep windows at 5m intervals it drops to **12 pts/hr** — effectively idle.

---

*argh — because your PR dashboard should work as hard as you do.*
