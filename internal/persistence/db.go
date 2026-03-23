// Package persistence provides a SQLite-backed cache for argh's PR state.
package persistence

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// sqlOpen is the sql.Open function; replaced in tests to simulate driver errors.
var sqlOpen = sql.Open

// osUserHomeDir is the os.UserHomeDir function; replaced in tests.
var osUserHomeDir = os.UserHomeDir

const (
	dataDirName = "argh"
	dbFileName  = "argh.db"
)

// Filesystem abstracts OS file operations for testability.
type Filesystem interface {
	MkdirAll(path string, perm os.FileMode) error
	UserDataDir() (string, error)
	Stat(path string) (os.FileInfo, error)
}

// OSFilesystem implements Filesystem using the real OS.
type OSFilesystem struct{}

func (OSFilesystem) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (OSFilesystem) Stat(path string) (os.FileInfo, error)        { return os.Stat(path) }

// UserDataDir returns ~/.local/share on macOS (XDG convention) falling back
// to ~/Library/Application Support when $XDG_DATA_HOME is not set.
func (OSFilesystem) UserDataDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	home, err := osUserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// DB wraps a *sql.DB with typed read/write methods for argh's schema.
type DB struct {
	db       *sql.DB
	beginTx  func() (*sql.Tx, error)        // defaults to db.Begin; overridable in tests
	commitTx func(tx *sql.Tx) error          // defaults to tx.Commit; overridable in tests
}

// Open opens (or creates) the argh SQLite database at
// ~/.local/share/argh/argh.db, enabling WAL mode immediately.
// The data directory is created if absent.
func Open(fs Filesystem) (*DB, error) {
	dataDir, err := dataDirPath(fs)
	if err != nil {
		return nil, fmt.Errorf("resolving data directory: %w", err)
	}
	if err := fs.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}
	dsn := filepath.Join(dataDir, dbFileName)
	return open(dsn)
}

// OpenMemory opens an in-memory SQLite database. Intended for tests.
func OpenMemory() (*DB, error) {
	return open(":memory:")
}

func open(dsn string) (*DB, error) {
	db, err := sqlOpen("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}
	d := &DB{db: db}
	d.beginTx = db.Begin
	d.commitTx = func(tx *sql.Tx) error { return tx.Commit() }
	if err := d.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return d, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// initialize enables WAL mode and runs the schema migration.
func (d *DB) initialize() error {
	if _, err := d.db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return fmt.Errorf("enabling WAL mode: %w", err)
	}
	return d.migrate()
}

func (d *DB) migrate() error {
	_, err := d.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("running schema migration: %w", err)
	}
	// Add body column to existing databases. Ignore "duplicate column name" errors.
	_, _ = d.db.Exec(`ALTER TABLE pull_requests ADD COLUMN body TEXT NOT NULL DEFAULT ''`)
	return nil
}

const schema = `
CREATE TABLE IF NOT EXISTS pull_requests (
    id             TEXT NOT NULL,
    repo           TEXT NOT NULL,
    number         INTEGER NOT NULL,
    title          TEXT NOT NULL,
    body           TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL,
    ci_state       TEXT NOT NULL,
    draft          INTEGER NOT NULL DEFAULT 0,
    author         TEXT NOT NULL,
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL,
    last_activity_at DATETIME NOT NULL,
    url            TEXT NOT NULL,
    global_id      TEXT NOT NULL,
    PRIMARY KEY (repo, number)
);

CREATE TABLE IF NOT EXISTS reviewers (
    pr_id  TEXT NOT NULL,
    login  TEXT NOT NULL,
    state  TEXT NOT NULL,
    PRIMARY KEY (pr_id, login)
);

CREATE TABLE IF NOT EXISTS check_runs (
    pr_id      TEXT NOT NULL,
    name       TEXT NOT NULL,
    state      TEXT NOT NULL,
    conclusion TEXT NOT NULL,
    url        TEXT NOT NULL,
    PRIMARY KEY (pr_id, name)
);

CREATE TABLE IF NOT EXISTS review_threads (
    pr_id    TEXT NOT NULL,
    id       TEXT NOT NULL,
    resolved INTEGER NOT NULL DEFAULT 0,
    body     TEXT NOT NULL,
    path     TEXT NOT NULL,
    line     INTEGER NOT NULL,
    PRIMARY KEY (pr_id, id)
);

CREATE TABLE IF NOT EXISTS timeline_events (
    pr_id        TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    actor        TEXT NOT NULL,
    created_at   DATETIME NOT NULL,
    payload_json TEXT NOT NULL,
    PRIMARY KEY (pr_id, event_type, actor, created_at)
);

CREATE TABLE IF NOT EXISTS watches (
    id           TEXT NOT NULL PRIMARY KEY,
    pr_url       TEXT NOT NULL,
    pr_number    INTEGER NOT NULL,
    repo         TEXT NOT NULL,
    trigger_expr TEXT NOT NULL,
    action_expr  TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'waiting',
    created_at   DATETIME NOT NULL,
    fired_at     DATETIME
);

CREATE TABLE IF NOT EXISTS session_ids (
    pr_url     TEXT NOT NULL PRIMARY KEY,
    session_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rate_limit (
    id        INTEGER NOT NULL PRIMARY KEY DEFAULT 1,
    remaining INTEGER NOT NULL DEFAULT 5000,
    reset_at  DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS etags (
    url           TEXT NOT NULL PRIMARY KEY,
    etag          TEXT NOT NULL DEFAULT '',
    last_modified TEXT NOT NULL DEFAULT ''
);
`

// ── Pull Requests ────────────────────────────────────────────────────────────

// PullRequest represents a row in the pull_requests table.
type PullRequest struct {
	ID             string
	Repo           string
	Number         int
	Title          string
	Body           string
	Status         string
	CIState        string
	Draft          bool
	Author         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastActivityAt time.Time
	URL            string
	GlobalID       string
}

// UpsertPullRequest inserts or replaces a pull request row.
func (d *DB) UpsertPullRequest(pr PullRequest) error {
	_, err := d.db.Exec(`
		INSERT INTO pull_requests
			(id, repo, number, title, body, status, ci_state, draft, author,
			 created_at, updated_at, last_activity_at, url, global_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo, number) DO UPDATE SET
			id             = excluded.id,
			title          = excluded.title,
			body           = excluded.body,
			status         = excluded.status,
			ci_state       = excluded.ci_state,
			draft          = excluded.draft,
			author         = excluded.author,
			created_at     = excluded.created_at,
			updated_at     = excluded.updated_at,
			last_activity_at = excluded.last_activity_at,
			url            = excluded.url,
			global_id      = excluded.global_id
	`, pr.ID, pr.Repo, pr.Number, pr.Title, pr.Body, pr.Status, pr.CIState,
		boolToInt(pr.Draft), pr.Author,
		pr.CreatedAt.UTC(), pr.UpdatedAt.UTC(), pr.LastActivityAt.UTC(),
		pr.URL, pr.GlobalID,
	)
	return err
}

// GetPullRequest returns the pull request identified by repo and number.
func (d *DB) GetPullRequest(repo string, number int) (PullRequest, error) {
	row := d.db.QueryRow(`
		SELECT id, repo, number, title, body, status, ci_state, draft, author,
		       created_at, updated_at, last_activity_at, url, global_id
		FROM pull_requests
		WHERE repo = ? AND number = ?
	`, repo, number)
	return scanPR(row)
}

// ListPullRequests returns all pull request rows.
func (d *DB) ListPullRequests() ([]PullRequest, error) {
	rows, err := d.db.Query(`
		SELECT id, repo, number, title, body, status, ci_state, draft, author,
		       created_at, updated_at, last_activity_at, url, global_id
		FROM pull_requests
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPRs(rows)
}

func scanPR(row *sql.Row) (PullRequest, error) {
	var pr PullRequest
	var draft int
	err := row.Scan(
		&pr.ID, &pr.Repo, &pr.Number, &pr.Title, &pr.Body, &pr.Status, &pr.CIState,
		&draft, &pr.Author,
		&pr.CreatedAt, &pr.UpdatedAt, &pr.LastActivityAt,
		&pr.URL, &pr.GlobalID,
	)
	if err != nil {
		return PullRequest{}, err
	}
	pr.Draft = draft != 0
	return pr, nil
}

func scanPRs(rows *sql.Rows) ([]PullRequest, error) {
	var prs []PullRequest
	for rows.Next() {
		var pr PullRequest
		var draft int
		if err := rows.Scan(
			&pr.ID, &pr.Repo, &pr.Number, &pr.Title, &pr.Body, &pr.Status, &pr.CIState,
			&draft, &pr.Author,
			&pr.CreatedAt, &pr.UpdatedAt, &pr.LastActivityAt,
			&pr.URL, &pr.GlobalID,
		); err != nil {
			return nil, err
		}
		pr.Draft = draft != 0
		prs = append(prs, pr)
	}
	return prs, rows.Err()
}

// ListPullRequestsByAuthor returns all pull requests authored by the given user.
func (d *DB) ListPullRequestsByAuthor(author string) ([]PullRequest, error) {
	rows, err := d.db.Query(`
		SELECT id, repo, number, title, body, status, ci_state, draft, author,
		       created_at, updated_at, last_activity_at, url, global_id
		FROM pull_requests
		WHERE author = ?
	`, author)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPRs(rows)
}

// ListPullRequestsNotByAuthor returns all pull requests NOT authored by the given user.
func (d *DB) ListPullRequestsNotByAuthor(author string) ([]PullRequest, error) {
	rows, err := d.db.Query(`
		SELECT id, repo, number, title, body, status, ci_state, draft, author,
		       created_at, updated_at, last_activity_at, url, global_id
		FROM pull_requests
		WHERE author != ?
	`, author)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPRs(rows)
}

// DeletePullRequest removes a pull request and all its associated rows
// (reviewers, check_runs, review_threads, timeline_events, session_ids).
// Returns the deleted PullRequest for event emission. Returns sql.ErrNoRows
// if the PR does not exist.
func (d *DB) DeletePullRequest(repo string, number int) (PullRequest, error) {
	pr, err := d.GetPullRequest(repo, number)
	if err != nil {
		return PullRequest{}, err
	}

	tx, err := d.beginTx()
	if err != nil {
		return PullRequest{}, fmt.Errorf("beginning delete transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range []string{
		`DELETE FROM reviewers WHERE pr_id = ?`,
		`DELETE FROM check_runs WHERE pr_id = ?`,
		`DELETE FROM review_threads WHERE pr_id = ?`,
		`DELETE FROM timeline_events WHERE pr_id = ?`,
	} {
		if _, err := tx.Exec(stmt, pr.ID); err != nil {
			return PullRequest{}, fmt.Errorf("deleting related rows: %w", err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM session_ids WHERE pr_url = ?`, pr.URL); err != nil {
		return PullRequest{}, fmt.Errorf("deleting session_id: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM pull_requests WHERE repo = ? AND number = ?`, repo, number); err != nil {
		return PullRequest{}, fmt.Errorf("deleting pull request: %w", err)
	}

	if err := d.commitTx(tx); err != nil {
		return PullRequest{}, fmt.Errorf("committing delete transaction: %w", err)
	}

	return pr, nil
}

// ── Reviewers ────────────────────────────────────────────────────────────────

// Reviewer represents a row in the reviewers table.
type Reviewer struct {
	PRID  string
	Login string
	State string
}

// UpsertReviewer inserts or updates a reviewer row.
func (d *DB) UpsertReviewer(r Reviewer) error {
	_, err := d.db.Exec(`
		INSERT INTO reviewers (pr_id, login, state)
		VALUES (?, ?, ?)
		ON CONFLICT(pr_id, login) DO UPDATE SET state = excluded.state
	`, r.PRID, r.Login, r.State)
	return err
}

// ListReviewers returns all reviewers for a given PR ID.
func (d *DB) ListReviewers(prID string) ([]Reviewer, error) {
	rows, err := d.db.Query(`SELECT pr_id, login, state FROM reviewers WHERE pr_id = ?`, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReviewers(rows)
}

func scanReviewers(rows *sql.Rows) ([]Reviewer, error) {
	var reviewers []Reviewer
	for rows.Next() {
		var r Reviewer
		if err := rows.Scan(&r.PRID, &r.Login, &r.State); err != nil {
			return nil, err
		}
		reviewers = append(reviewers, r)
	}
	return reviewers, rows.Err()
}

// ListReviewersByRepo returns all distinct reviewer logins that have reviewed
// pull requests in the given repository, ordered alphabetically.
func (d *DB) ListReviewersByRepo(repo string) ([]string, error) {
	rows, err := d.db.Query(`
		SELECT DISTINCT r.login
		FROM reviewers r
		JOIN pull_requests p ON r.pr_id = p.id
		WHERE p.repo = ?
		ORDER BY r.login
	`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLogins(rows)
}

func scanLogins(rows *sql.Rows) ([]string, error) {
	var logins []string
	for rows.Next() {
		var login string
		if err := rows.Scan(&login); err != nil {
			return nil, err
		}
		logins = append(logins, login)
	}
	return logins, rows.Err()
}

// ── Check Runs ───────────────────────────────────────────────────────────────

// CheckRun represents a row in the check_runs table.
type CheckRun struct {
	PRID       string
	Name       string
	State      string
	Conclusion string
	URL        string
}

// UpsertCheckRun inserts or updates a check run row.
func (d *DB) UpsertCheckRun(cr CheckRun) error {
	_, err := d.db.Exec(`
		INSERT INTO check_runs (pr_id, name, state, conclusion, url)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(pr_id, name) DO UPDATE SET
			state      = excluded.state,
			conclusion = excluded.conclusion,
			url        = excluded.url
	`, cr.PRID, cr.Name, cr.State, cr.Conclusion, cr.URL)
	return err
}

// ListCheckRuns returns all check runs for a given PR ID.
func (d *DB) ListCheckRuns(prID string) ([]CheckRun, error) {
	rows, err := d.db.Query(`SELECT pr_id, name, state, conclusion, url FROM check_runs WHERE pr_id = ?`, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCheckRuns(rows)
}

func scanCheckRuns(rows *sql.Rows) ([]CheckRun, error) {
	var crs []CheckRun
	for rows.Next() {
		var cr CheckRun
		if err := rows.Scan(&cr.PRID, &cr.Name, &cr.State, &cr.Conclusion, &cr.URL); err != nil {
			return nil, err
		}
		crs = append(crs, cr)
	}
	return crs, rows.Err()
}

// ── Review Threads ───────────────────────────────────────────────────────────

// ReviewThread represents a row in the review_threads table.
type ReviewThread struct {
	PRID     string
	ID       string
	Resolved bool
	Body     string
	Path     string
	Line     int
}

// UpsertReviewThread inserts or updates a review thread row.
func (d *DB) UpsertReviewThread(rt ReviewThread) error {
	_, err := d.db.Exec(`
		INSERT INTO review_threads (pr_id, id, resolved, body, path, line)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(pr_id, id) DO UPDATE SET
			resolved = excluded.resolved,
			body     = excluded.body,
			path     = excluded.path,
			line     = excluded.line
	`, rt.PRID, rt.ID, boolToInt(rt.Resolved), rt.Body, rt.Path, rt.Line)
	return err
}

// ListReviewThreads returns all review threads for a given PR ID.
func (d *DB) ListReviewThreads(prID string) ([]ReviewThread, error) {
	rows, err := d.db.Query(`SELECT pr_id, id, resolved, body, path, line FROM review_threads WHERE pr_id = ?`, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReviewThreads(rows)
}

func scanReviewThreads(rows *sql.Rows) ([]ReviewThread, error) {
	var threads []ReviewThread
	for rows.Next() {
		var rt ReviewThread
		var resolved int
		if err := rows.Scan(&rt.PRID, &rt.ID, &resolved, &rt.Body, &rt.Path, &rt.Line); err != nil {
			return nil, err
		}
		rt.Resolved = resolved != 0
		threads = append(threads, rt)
	}
	return threads, rows.Err()
}

// ── Timeline Events ──────────────────────────────────────────────────────────

// TimelineEvent represents a row in the timeline_events table.
type TimelineEvent struct {
	PRID        string
	EventType   string
	Actor       string
	CreatedAt   time.Time
	PayloadJSON string
}

// InsertTimelineEvent inserts a timeline event. Duplicate rows are silently ignored.
func (d *DB) InsertTimelineEvent(te TimelineEvent) error {
	_, err := d.db.Exec(`
		INSERT OR IGNORE INTO timeline_events (pr_id, event_type, actor, created_at, payload_json)
		VALUES (?, ?, ?, ?, ?)
	`, te.PRID, te.EventType, te.Actor, te.CreatedAt.UTC(), te.PayloadJSON)
	return err
}

// ListTimelineEvents returns all timeline events for a given PR ID.
func (d *DB) ListTimelineEvents(prID string) ([]TimelineEvent, error) {
	rows, err := d.db.Query(`
		SELECT pr_id, event_type, actor, created_at, payload_json
		FROM timeline_events
		WHERE pr_id = ?
		ORDER BY created_at ASC
	`, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTimelineEvents(rows)
}

func scanTimelineEvents(rows *sql.Rows) ([]TimelineEvent, error) {
	var events []TimelineEvent
	for rows.Next() {
		var te TimelineEvent
		if err := rows.Scan(&te.PRID, &te.EventType, &te.Actor, &te.CreatedAt, &te.PayloadJSON); err != nil {
			return nil, err
		}
		events = append(events, te)
	}
	return events, rows.Err()
}

// ── Watches ──────────────────────────────────────────────────────────────────

// Watch represents a row in the watches table.
type Watch struct {
	ID          string
	PRURL       string
	PRNumber    int
	Repo        string
	TriggerExpr string
	ActionExpr  string
	Status      string
	CreatedAt   time.Time
	FiredAt     *time.Time
}

// InsertWatch inserts a new watch row.
func (d *DB) InsertWatch(w Watch) error {
	_, err := d.db.Exec(`
		INSERT INTO watches (id, pr_url, pr_number, repo, trigger_expr, action_expr, status, created_at, fired_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, w.ID, w.PRURL, w.PRNumber, w.Repo, w.TriggerExpr, w.ActionExpr, w.Status, w.CreatedAt.UTC(), w.FiredAt)
	return err
}

// UpdateWatchStatus updates the status (and optionally fired_at) of a watch.
func (d *DB) UpdateWatchStatus(id string, status string, firedAt *time.Time) error {
	_, err := d.db.Exec(`
		UPDATE watches SET status = ?, fired_at = ? WHERE id = ?
	`, status, firedAt, id)
	return err
}

// ListWatches returns all watch rows.
func (d *DB) ListWatches() ([]Watch, error) {
	rows, err := d.db.Query(`
		SELECT id, pr_url, pr_number, repo, trigger_expr, action_expr, status, created_at, fired_at
		FROM watches
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWatches(rows)
}

// GetWatch returns the watch with the given ID.
func (d *DB) GetWatch(id string) (Watch, error) {
	row := d.db.QueryRow(`
		SELECT id, pr_url, pr_number, repo, trigger_expr, action_expr, status, created_at, fired_at
		FROM watches WHERE id = ?
	`, id)
	var w Watch
	var firedAt sql.NullTime
	err := row.Scan(&w.ID, &w.PRURL, &w.PRNumber, &w.Repo, &w.TriggerExpr, &w.ActionExpr,
		&w.Status, &w.CreatedAt, &firedAt)
	if err != nil {
		return Watch{}, err
	}
	if firedAt.Valid {
		t := firedAt.Time
		w.FiredAt = &t
	}
	return w, nil
}

func scanWatches(rows *sql.Rows) ([]Watch, error) {
	var watches []Watch
	for rows.Next() {
		var w Watch
		var firedAt sql.NullTime
		if err := rows.Scan(&w.ID, &w.PRURL, &w.PRNumber, &w.Repo,
			&w.TriggerExpr, &w.ActionExpr, &w.Status, &w.CreatedAt, &firedAt); err != nil {
			return nil, err
		}
		if firedAt.Valid {
			t := firedAt.Time
			w.FiredAt = &t
		}
		watches = append(watches, w)
	}
	return watches, rows.Err()
}

// ── Session IDs ──────────────────────────────────────────────────────────────

// UpsertSessionID sets the session ID for a PR URL.
func (d *DB) UpsertSessionID(prURL string, sessionID string) error {
	_, err := d.db.Exec(`
		INSERT INTO session_ids (pr_url, session_id)
		VALUES (?, ?)
		ON CONFLICT(pr_url) DO UPDATE SET session_id = excluded.session_id
	`, prURL, sessionID)
	return err
}

// GetSessionID returns the session ID for a PR URL, or an error if not found.
func (d *DB) GetSessionID(prURL string) (string, error) {
	var sessionID string
	err := d.db.QueryRow(`SELECT session_id FROM session_ids WHERE pr_url = ?`, prURL).Scan(&sessionID)
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

// ClearSessionIDs removes all session ID mappings.
func (d *DB) ClearSessionIDs() error {
	_, err := d.db.Exec(`DELETE FROM session_ids`)
	return err
}

// ── Rate Limit ───────────────────────────────────────────────────────────────

// RateLimit represents the singleton rate_limit row.
type RateLimit struct {
	Remaining int
	ResetAt   time.Time
}

// UpsertRateLimit writes the current rate limit state.
func (d *DB) UpsertRateLimit(rl RateLimit) error {
	_, err := d.db.Exec(`
		INSERT INTO rate_limit (id, remaining, reset_at)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			remaining = excluded.remaining,
			reset_at  = excluded.reset_at
	`, rl.Remaining, rl.ResetAt.UTC())
	return err
}

// GetRateLimit returns the current rate limit state.
func (d *DB) GetRateLimit() (RateLimit, error) {
	var rl RateLimit
	err := d.db.QueryRow(`SELECT remaining, reset_at FROM rate_limit WHERE id = 1`).
		Scan(&rl.Remaining, &rl.ResetAt)
	if err != nil {
		return RateLimit{}, err
	}
	return rl, nil
}

// ── ETags ────────────────────────────────────────────────────────────────────

// ETag represents a row in the etags table.
type ETag struct {
	URL          string
	ETag         string
	LastModified string
}

// UpsertETag inserts or updates the ETag/Last-Modified for a URL.
func (d *DB) UpsertETag(e ETag) error {
	_, err := d.db.Exec(`
		INSERT INTO etags (url, etag, last_modified)
		VALUES (?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			etag          = excluded.etag,
			last_modified = excluded.last_modified
	`, e.URL, e.ETag, e.LastModified)
	return err
}

// GetETag returns the ETag entry for a URL.
func (d *DB) GetETag(url string) (ETag, error) {
	var e ETag
	err := d.db.QueryRow(`SELECT url, etag, last_modified FROM etags WHERE url = ?`, url).
		Scan(&e.URL, &e.ETag, &e.LastModified)
	if err != nil {
		return ETag{}, err
	}
	return e, nil
}

// ── Status Queries ────────────────────────────────────────────────────────────

// CountPRsWithPendingReview returns the number of distinct PRs that have at
// least one reviewer in PENDING state.
func (d *DB) CountPRsWithPendingReview() (int, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(DISTINCT pr_id) FROM reviewers WHERE state = 'PENDING'`,
	).Scan(&count)
	return count, err
}

// MaxLastActivityAt returns the most recent last_activity_at across all PRs
// and whether any rows exist. Uses ORDER BY + LIMIT to avoid aggregate type
// conversion issues with the SQLite driver.
func (d *DB) MaxLastActivityAt() (time.Time, bool, error) {
	var t time.Time
	err := d.db.QueryRow(
		`SELECT last_activity_at FROM pull_requests ORDER BY last_activity_at DESC LIMIT 1`,
	).Scan(&t)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DBPath returns the full path to the argh.db file for the given filesystem.
func DBPath(fs Filesystem) (string, error) {
	dir, err := dataDirPath(fs)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dbFileName), nil
}

func dataDirPath(fs Filesystem) (string, error) {
	base, err := fs.UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, dataDirName), nil
}
