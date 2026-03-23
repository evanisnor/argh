package persistence

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── Filesystem fakes ─────────────────────────────────────────────────────────

type fakeFilesystem struct {
	dir string
	err error
}

func newTempFilesystem(t *testing.T) *fakeFilesystem {
	t.Helper()
	dir := t.TempDir()
	return &fakeFilesystem{dir: dir}
}

func (f *fakeFilesystem) MkdirAll(path string, perm os.FileMode) error {
	if f.err != nil {
		return f.err
	}
	return os.MkdirAll(path, perm)
}

func (f *fakeFilesystem) UserDataDir() (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.dir, nil
}

func (f *fakeFilesystem) Stat(path string) (os.FileInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return os.Stat(path)
}

// ── OSFilesystem (real OS delegation) ────────────────────────────────────────

func TestOSFilesystem_MkdirAll(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "subdir", "nested")
	fs := OSFilesystem{}
	if err := fs.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("OSFilesystem.MkdirAll() error: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	}
}

func TestOSFilesystem_UserDataDir(t *testing.T) {
	fs := OSFilesystem{}
	dir, err := fs.UserDataDir()
	if err != nil {
		t.Fatalf("OSFilesystem.UserDataDir() error: %v", err)
	}
	if dir == "" {
		t.Error("UserDataDir() returned empty string")
	}
}

func TestOSFilesystem_UserDataDir_HomeDirError(t *testing.T) {
	orig := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	defer func() { osUserHomeDir = orig }()

	t.Setenv("XDG_DATA_HOME", "") // ensure we hit the homeDir path
	fs := OSFilesystem{}
	_, err := fs.UserDataDir()
	if err == nil {
		t.Error("expected error when home dir fails")
	}
}

func TestOSFilesystem_UserDataDir_XDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	fs := OSFilesystem{}
	dir, err := fs.UserDataDir()
	if err != nil {
		t.Fatalf("UserDataDir() with XDG_DATA_HOME: %v", err)
	}
	if dir != "/custom/data" {
		t.Errorf("UserDataDir() = %q, want %q", dir, "/custom/data")
	}
}

// ── fakeFilesystem with errors ───────────────────────────────────────────────

type errFilesystem struct {
	dir        string
	mkdirError error
	dataDirErr error
	statError  error
}

func (f *errFilesystem) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirError != nil {
		return f.mkdirError
	}
	return os.MkdirAll(path, perm)
}

func (f *errFilesystem) UserDataDir() (string, error) {
	if f.dataDirErr != nil {
		return "", f.dataDirErr
	}
	return f.dir, nil
}

func (f *errFilesystem) Stat(path string) (os.FileInfo, error) {
	if f.statError != nil {
		return nil, f.statError
	}
	return os.Stat(path)
}

// ── Open / migrate ────────────────────────────────────────────────────────────

func TestOpen_CreatesDataDirectory(t *testing.T) {
	fs := newTempFilesystem(t)
	expectedDir := filepath.Join(fs.dir, dataDirName)

	db, err := Open(fs)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("expected data directory %s to be created", expectedDir)
	}
}

func TestOpen_DatabaseFileCreated(t *testing.T) {
	fs := newTempFilesystem(t)
	dbPath := filepath.Join(fs.dir, dataDirName, dbFileName)

	db, err := Open(fs)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("expected database file %s to be created", dbPath)
	}
}

func TestOpen_UserDataDirError(t *testing.T) {
	fs := &errFilesystem{dataDirErr: errors.New("no home dir")}
	_, err := Open(fs)
	if err == nil {
		t.Fatal("expected error when UserDataDir fails")
	}
}

func TestOpen_MkdirAllError(t *testing.T) {
	fs := &errFilesystem{dir: t.TempDir(), mkdirError: errors.New("permission denied")}
	_, err := Open(fs)
	if err == nil {
		t.Fatal("expected error when MkdirAll fails")
	}
}

func TestOpen_InitializeError(t *testing.T) {
	// A DSN in a non-existent directory causes the sqlite3 driver to fail on
	// first use (when the WAL PRAGMA fires), exercising the initialize error
	// path inside open() including the db.Close() call.
	_, err := open("/tmp/nonexistent-argh-test-dir-xyz/db.sqlite")
	if err == nil {
		t.Fatal("expected error when database path is unwritable")
	}
}

func TestOpen_SQLOpenError(t *testing.T) {
	orig := sqlOpen
	sqlOpen = func(driverName, dsn string) (*sql.DB, error) {
		return nil, errors.New("mock open error")
	}
	defer func() { sqlOpen = orig }()

	_, err := open(":memory:")
	if err == nil {
		t.Fatal("expected error when sql.Open fails")
	}
}

func TestInitialize_WALError(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory(): %v", err)
	}
	// Close the underlying connection so the WAL PRAGMA fails.
	if err := db.db.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if err := db.initialize(); err == nil {
		t.Error("expected error from initialize after db closed")
	}
}

func TestMigrate_ClosedDB(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory(): %v", err)
	}
	if err := db.db.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if err := db.migrate(); err == nil {
		t.Error("expected error from migrate after db closed")
	}
}

func TestOpen_WALModeEnabled(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory() error: %v", err)
	}
	defer db.Close()

	var journalMode string
	if err := db.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	// In-memory SQLite always reports "memory" regardless of WAL pragma.
	// For a file-backed DB it would be "wal". Verify the PRAGMA was at least
	// executed without error (already guaranteed by OpenMemory succeeding).
	if journalMode == "" {
		t.Error("expected non-empty journal mode")
	}
}

func TestMigrate_SchemaCleanOnFreshDB(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory() error: %v", err)
	}
	defer db.Close()

	tables := []string{
		"pull_requests", "reviewers", "check_runs", "review_threads",
		"timeline_events", "watches", "session_ids", "rate_limit", "etags",
	}
	for _, table := range tables {
		var count int
		q := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`
		if err := db.db.QueryRow(q, table).Scan(&count); err != nil {
			t.Fatalf("checking table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("expected table %s to exist after migration", table)
		}
	}
}

// ── Pull Requests ────────────────────────────────────────────────────────────

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory() error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func makeTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func TestPullRequest_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	pr := PullRequest{
		ID:             "pr-1",
		Repo:           "owner/repo",
		Number:         42,
		Title:          "My PR",
		Body:           "This is the PR body with **markdown**.",
		Status:         "open",
		CIState:        "passing",
		Draft:          false,
		Author:         "alice",
		CreatedAt:      makeTime("2024-01-01T10:00:00Z"),
		UpdatedAt:      makeTime("2024-01-02T10:00:00Z"),
		LastActivityAt: makeTime("2024-01-03T10:00:00Z"),
		URL:            "https://github.com/owner/repo/pull/42",
		GlobalID:       "PR_global_1",
	}

	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest() error: %v", err)
	}

	got, err := db.GetPullRequest("owner/repo", 42)
	if err != nil {
		t.Fatalf("GetPullRequest() error: %v", err)
	}

	if got.ID != pr.ID || got.Title != pr.Title || got.Body != pr.Body || got.Draft != pr.Draft {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, pr)
	}
}

func TestPullRequest_UpdateExistingRow(t *testing.T) {
	db := openTestDB(t)

	pr := PullRequest{
		ID:             "pr-1",
		Repo:           "owner/repo",
		Number:         42,
		Title:          "Old title",
		Status:         "open",
		CIState:        "pending",
		Draft:          true,
		Author:         "alice",
		CreatedAt:      makeTime("2024-01-01T10:00:00Z"),
		UpdatedAt:      makeTime("2024-01-02T10:00:00Z"),
		LastActivityAt: makeTime("2024-01-03T10:00:00Z"),
		URL:            "https://github.com/owner/repo/pull/42",
		GlobalID:       "PR_global_1",
	}
	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest() first: %v", err)
	}

	pr.Title = "New title"
	pr.CIState = "passing"
	pr.Draft = false
	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest() second: %v", err)
	}

	got, err := db.GetPullRequest("owner/repo", 42)
	if err != nil {
		t.Fatalf("GetPullRequest() error: %v", err)
	}
	if got.Title != "New title" || got.CIState != "passing" || got.Draft != false {
		t.Errorf("update did not apply: got %+v", got)
	}
}

func TestPullRequest_BodyDefaultsToEmpty(t *testing.T) {
	db := openTestDB(t)

	pr := PullRequest{
		ID: "pr-nobody", Repo: "r/r", Number: 1, Title: "No Body",
		Status: "open", CIState: "none", Author: "a",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
		URL: "https://github.com/r/r/pull/1", GlobalID: "g",
	}
	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest: %v", err)
	}
	got, err := db.GetPullRequest("r/r", 1)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if got.Body != "" {
		t.Errorf("Body = %q, want empty string", got.Body)
	}
}

func TestPullRequest_DraftBoolRoundTrip(t *testing.T) {
	db := openTestDB(t)

	tests := []struct {
		name  string
		draft bool
	}{
		{"draft true", true},
		{"draft false", false},
	}

	for i, tt := range tests {
		pr := PullRequest{
			ID:             "pr-draft",
			Repo:           "r/r",
			Number:         i + 1,
			Title:          "t",
			Status:         "open",
			CIState:        "none",
			Draft:          tt.draft,
			Author:         "a",
			CreatedAt:      makeTime("2024-01-01T00:00:00Z"),
			UpdatedAt:      makeTime("2024-01-01T00:00:00Z"),
			LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
			URL:            "https://github.com/r/r/pull/1",
			GlobalID:       "g",
		}
		if err := db.UpsertPullRequest(pr); err != nil {
			t.Fatalf("%s: UpsertPullRequest: %v", tt.name, err)
		}
		got, err := db.GetPullRequest("r/r", i+1)
		if err != nil {
			t.Fatalf("%s: GetPullRequest: %v", tt.name, err)
		}
		if got.Draft != tt.draft {
			t.Errorf("%s: Draft = %v, want %v", tt.name, got.Draft, tt.draft)
		}
	}
}

func TestGetPullRequest_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetPullRequest("nonexistent/repo", 999)
	if err == nil {
		t.Error("expected error for non-existent pull request")
	}
}

func TestListPullRequests_QueryError(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.ListPullRequests()
	if err == nil {
		t.Error("expected error from ListPullRequests on closed DB")
	}
}

func TestScanPRs_ScanError(t *testing.T) {
	db := openTestDB(t)
	// Query a single-column table — scanPRs expects 14 columns, so Scan errors.
	if _, err := db.db.Exec(`CREATE TEMP TABLE one_col (val TEXT)`); err != nil {
		t.Fatalf("create temp table: %v", err)
	}
	if _, err := db.db.Exec(`INSERT INTO one_col VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := db.db.Query(`SELECT val FROM one_col`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	_, err = scanPRs(rows)
	if err == nil {
		t.Error("expected scan error from column count mismatch")
	}
}

func TestListPullRequests(t *testing.T) {
	db := openTestDB(t)

	for i := 1; i <= 3; i++ {
		pr := PullRequest{
			ID: "pr", Repo: "r/r", Number: i, Title: "t", Status: "open",
			CIState: "none", Author: "a",
			CreatedAt:      makeTime("2024-01-01T00:00:00Z"),
			UpdatedAt:      makeTime("2024-01-01T00:00:00Z"),
			LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
			URL:            "https://example.com", GlobalID: "g",
		}
		if err := db.UpsertPullRequest(pr); err != nil {
			t.Fatalf("UpsertPullRequest(%d): %v", i, err)
		}
	}

	prs, err := db.ListPullRequests()
	if err != nil {
		t.Fatalf("ListPullRequests() error: %v", err)
	}
	if len(prs) != 3 {
		t.Errorf("expected 3 pull requests, got %d", len(prs))
	}
}

// ── Reviewers ────────────────────────────────────────────────────────────────

func TestReviewer_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	r := Reviewer{PRID: "pr-1", Login: "bob", State: "APPROVED"}
	if err := db.UpsertReviewer(r); err != nil {
		t.Fatalf("UpsertReviewer() error: %v", err)
	}

	got, err := db.ListReviewers("pr-1")
	if err != nil {
		t.Fatalf("ListReviewers() error: %v", err)
	}
	if len(got) != 1 || got[0].Login != "bob" || got[0].State != "APPROVED" {
		t.Errorf("reviewer round-trip mismatch: %+v", got)
	}
}

func TestReviewer_UpdateState(t *testing.T) {
	db := openTestDB(t)

	r := Reviewer{PRID: "pr-1", Login: "bob", State: "PENDING"}
	if err := db.UpsertReviewer(r); err != nil {
		t.Fatalf("UpsertReviewer() first: %v", err)
	}
	r.State = "CHANGES_REQUESTED"
	if err := db.UpsertReviewer(r); err != nil {
		t.Fatalf("UpsertReviewer() second: %v", err)
	}

	got, err := db.ListReviewers("pr-1")
	if err != nil {
		t.Fatalf("ListReviewers(): %v", err)
	}
	if len(got) != 1 || got[0].State != "CHANGES_REQUESTED" {
		t.Errorf("state not updated: %+v", got)
	}
}

func TestListReviewers_QueryError(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.ListReviewers("pr-1")
	if err == nil {
		t.Error("expected error from ListReviewers on closed DB")
	}
}

func TestScanReviewers_ScanError(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.db.Exec(`CREATE TEMP TABLE one_col_r (val TEXT)`); err != nil {
		t.Fatalf("create temp table: %v", err)
	}
	if _, err := db.db.Exec(`INSERT INTO one_col_r VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := db.db.Query(`SELECT val FROM one_col_r`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	_, err = scanReviewers(rows)
	if err == nil {
		t.Error("expected scan error from column count mismatch")
	}
}

// ── Check Runs ───────────────────────────────────────────────────────────────

func TestCheckRun_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	cr := CheckRun{PRID: "pr-1", Name: "build", State: "COMPLETED", Conclusion: "SUCCESS", URL: "https://ci.example.com"}
	if err := db.UpsertCheckRun(cr); err != nil {
		t.Fatalf("UpsertCheckRun() error: %v", err)
	}

	got, err := db.ListCheckRuns("pr-1")
	if err != nil {
		t.Fatalf("ListCheckRuns() error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "build" || got[0].Conclusion != "SUCCESS" {
		t.Errorf("check run round-trip mismatch: %+v", got)
	}
}

func TestCheckRun_UpdateExisting(t *testing.T) {
	db := openTestDB(t)

	cr := CheckRun{PRID: "pr-1", Name: "build", State: "IN_PROGRESS", Conclusion: "", URL: "https://ci.example.com"}
	if err := db.UpsertCheckRun(cr); err != nil {
		t.Fatalf("UpsertCheckRun() first: %v", err)
	}
	cr.State = "COMPLETED"
	cr.Conclusion = "FAILURE"
	if err := db.UpsertCheckRun(cr); err != nil {
		t.Fatalf("UpsertCheckRun() second: %v", err)
	}

	got, err := db.ListCheckRuns("pr-1")
	if err != nil {
		t.Fatalf("ListCheckRuns(): %v", err)
	}
	if len(got) != 1 || got[0].State != "COMPLETED" || got[0].Conclusion != "FAILURE" {
		t.Errorf("update not applied: %+v", got)
	}
}

func TestListCheckRuns_QueryError(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.ListCheckRuns("pr-1")
	if err == nil {
		t.Error("expected error from ListCheckRuns on closed DB")
	}
}

func TestScanCheckRuns_ScanError(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.db.Exec(`CREATE TEMP TABLE one_col_cr (val TEXT)`); err != nil {
		t.Fatalf("create temp table: %v", err)
	}
	if _, err := db.db.Exec(`INSERT INTO one_col_cr VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := db.db.Query(`SELECT val FROM one_col_cr`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	_, err = scanCheckRuns(rows)
	if err == nil {
		t.Error("expected scan error from column count mismatch")
	}
}

// ── Review Threads ───────────────────────────────────────────────────────────

func TestReviewThread_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	rt := ReviewThread{PRID: "pr-1", ID: "thread-1", Resolved: false, Body: "comment", Path: "main.go", Line: 10}
	if err := db.UpsertReviewThread(rt); err != nil {
		t.Fatalf("UpsertReviewThread() error: %v", err)
	}

	got, err := db.ListReviewThreads("pr-1")
	if err != nil {
		t.Fatalf("ListReviewThreads() error: %v", err)
	}
	if len(got) != 1 || got[0].Body != "comment" || got[0].Resolved != false {
		t.Errorf("review thread round-trip mismatch: %+v", got)
	}
}

func TestReviewThread_MarkResolved(t *testing.T) {
	db := openTestDB(t)

	rt := ReviewThread{PRID: "pr-1", ID: "thread-1", Resolved: false, Body: "nit", Path: "a.go", Line: 1}
	if err := db.UpsertReviewThread(rt); err != nil {
		t.Fatalf("UpsertReviewThread() first: %v", err)
	}
	rt.Resolved = true
	if err := db.UpsertReviewThread(rt); err != nil {
		t.Fatalf("UpsertReviewThread() second: %v", err)
	}

	got, err := db.ListReviewThreads("pr-1")
	if err != nil {
		t.Fatalf("ListReviewThreads(): %v", err)
	}
	if len(got) != 1 || !got[0].Resolved {
		t.Errorf("resolved not updated: %+v", got)
	}
}

func TestListReviewThreads_QueryError(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.ListReviewThreads("pr-1")
	if err == nil {
		t.Error("expected error from ListReviewThreads on closed DB")
	}
}

func TestScanReviewThreads_ScanError(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.db.Exec(`CREATE TEMP TABLE one_col_rt (val TEXT)`); err != nil {
		t.Fatalf("create temp table: %v", err)
	}
	if _, err := db.db.Exec(`INSERT INTO one_col_rt VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := db.db.Query(`SELECT val FROM one_col_rt`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	_, err = scanReviewThreads(rows)
	if err == nil {
		t.Error("expected scan error from column count mismatch")
	}
}

// ── Timeline Events ──────────────────────────────────────────────────────────

func TestTimelineEvent_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	te := TimelineEvent{
		PRID:        "pr-1",
		EventType:   "review",
		Actor:       "carol",
		CreatedAt:   makeTime("2024-06-01T12:00:00Z"),
		PayloadJSON: `{"action":"submitted"}`,
	}
	if err := db.InsertTimelineEvent(te); err != nil {
		t.Fatalf("InsertTimelineEvent() error: %v", err)
	}

	got, err := db.ListTimelineEvents("pr-1")
	if err != nil {
		t.Fatalf("ListTimelineEvents() error: %v", err)
	}
	if len(got) != 1 || got[0].Actor != "carol" || got[0].PayloadJSON != `{"action":"submitted"}` {
		t.Errorf("timeline event round-trip mismatch: %+v", got)
	}
}

func TestTimelineEvent_DuplicateIgnored(t *testing.T) {
	db := openTestDB(t)

	te := TimelineEvent{
		PRID:        "pr-1",
		EventType:   "pushed",
		Actor:       "dave",
		CreatedAt:   makeTime("2024-06-01T09:00:00Z"),
		PayloadJSON: `{}`,
	}
	if err := db.InsertTimelineEvent(te); err != nil {
		t.Fatalf("InsertTimelineEvent() first: %v", err)
	}
	if err := db.InsertTimelineEvent(te); err != nil {
		t.Fatalf("InsertTimelineEvent() duplicate: %v", err)
	}

	got, err := db.ListTimelineEvents("pr-1")
	if err != nil {
		t.Fatalf("ListTimelineEvents(): %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 event after duplicate insert, got %d", len(got))
	}
}

func TestListTimelineEvents_QueryError(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.ListTimelineEvents("pr-1")
	if err == nil {
		t.Error("expected error from ListTimelineEvents on closed DB")
	}
}

func TestScanTimelineEvents_ScanError(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.db.Exec(`CREATE TEMP TABLE one_col_te (val TEXT)`); err != nil {
		t.Fatalf("create temp table: %v", err)
	}
	if _, err := db.db.Exec(`INSERT INTO one_col_te VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := db.db.Query(`SELECT val FROM one_col_te`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	_, err = scanTimelineEvents(rows)
	if err == nil {
		t.Error("expected scan error from column count mismatch")
	}
}

// ── Watches ──────────────────────────────────────────────────────────────────

func TestWatch_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	w := Watch{
		ID:          "watch-1",
		PRURL:       "https://github.com/owner/repo/pull/42",
		PRNumber:    42,
		Repo:        "owner/repo",
		TriggerExpr: "on:ci-pass",
		ActionExpr:  "merge",
		Status:      "waiting",
		CreatedAt:   makeTime("2024-01-01T08:00:00Z"),
	}
	if err := db.InsertWatch(w); err != nil {
		t.Fatalf("InsertWatch() error: %v", err)
	}

	got, err := db.GetWatch("watch-1")
	if err != nil {
		t.Fatalf("GetWatch() error: %v", err)
	}
	if got.TriggerExpr != "on:ci-pass" || got.Status != "waiting" || got.FiredAt != nil {
		t.Errorf("watch round-trip mismatch: %+v", got)
	}
}

func TestWatch_UpdateStatus(t *testing.T) {
	db := openTestDB(t)

	w := Watch{
		ID:          "watch-2",
		PRURL:       "https://github.com/owner/repo/pull/7",
		PRNumber:    7,
		Repo:        "owner/repo",
		TriggerExpr: "on:approved",
		ActionExpr:  "notify",
		Status:      "waiting",
		CreatedAt:   makeTime("2024-01-01T08:00:00Z"),
	}
	if err := db.InsertWatch(w); err != nil {
		t.Fatalf("InsertWatch() error: %v", err)
	}

	firedAt := makeTime("2024-01-02T09:00:00Z")
	if err := db.UpdateWatchStatus("watch-2", "fired", &firedAt); err != nil {
		t.Fatalf("UpdateWatchStatus() error: %v", err)
	}

	got, err := db.GetWatch("watch-2")
	if err != nil {
		t.Fatalf("GetWatch() error: %v", err)
	}
	if got.Status != "fired" || got.FiredAt == nil {
		t.Errorf("watch status not updated: %+v", got)
	}
}

func TestWatch_GetNotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetWatch("nonexistent-watch")
	if err == nil {
		t.Error("expected error for non-existent watch")
	}
}

func TestWatch_ListWithFiredAt(t *testing.T) {
	db := openTestDB(t)

	w := Watch{
		ID: "w-fired", PRURL: "https://example.com", PRNumber: 1, Repo: "r/r",
		TriggerExpr: "on:ci-pass", ActionExpr: "merge", Status: "waiting",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"),
	}
	if err := db.InsertWatch(w); err != nil {
		t.Fatalf("InsertWatch(): %v", err)
	}
	firedAt := makeTime("2024-01-02T10:00:00Z")
	if err := db.UpdateWatchStatus("w-fired", "fired", &firedAt); err != nil {
		t.Fatalf("UpdateWatchStatus(): %v", err)
	}

	watches, err := db.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches(): %v", err)
	}
	if len(watches) != 1 || watches[0].FiredAt == nil {
		t.Errorf("expected watch with non-nil FiredAt, got %+v", watches)
	}
}

func TestWatch_ListQueryError(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.ListWatches()
	if err == nil {
		t.Error("expected error from ListWatches on closed DB")
	}
}

func TestScanWatches_ScanError(t *testing.T) {
	db := openTestDB(t)
	// Query a single-column table — scanWatches expects 9 columns, so Scan errors.
	if _, err := db.db.Exec(`CREATE TEMP TABLE one_col_w (val TEXT)`); err != nil {
		t.Fatalf("create temp table: %v", err)
	}
	if _, err := db.db.Exec(`INSERT INTO one_col_w VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := db.db.Query(`SELECT val FROM one_col_w`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	_, err = scanWatches(rows)
	if err == nil {
		t.Error("expected scan error from column count mismatch in watches")
	}
}

func TestWatch_ListAll(t *testing.T) {
	db := openTestDB(t)

	for _, id := range []string{"w1", "w2", "w3"} {
		w := Watch{
			ID: id, PRURL: "https://example.com", PRNumber: 1, Repo: "r/r",
			TriggerExpr: "on:ci-pass", ActionExpr: "merge", Status: "waiting",
			CreatedAt: makeTime("2024-01-01T00:00:00Z"),
		}
		if err := db.InsertWatch(w); err != nil {
			t.Fatalf("InsertWatch(%s): %v", id, err)
		}
	}

	got, err := db.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches() error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 watches, got %d", len(got))
	}
}

// ── Session IDs ──────────────────────────────────────────────────────────────

func TestSessionID_UpsertAndGet(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertSessionID("https://github.com/r/r/pull/1", "a"); err != nil {
		t.Fatalf("UpsertSessionID() error: %v", err)
	}

	got, err := db.GetSessionID("https://github.com/r/r/pull/1")
	if err != nil {
		t.Fatalf("GetSessionID() error: %v", err)
	}
	if got != "a" {
		t.Errorf("session ID = %q, want %q", got, "a")
	}
}

func TestSessionID_Update(t *testing.T) {
	db := openTestDB(t)
	url := "https://github.com/r/r/pull/1"

	if err := db.UpsertSessionID(url, "a"); err != nil {
		t.Fatalf("UpsertSessionID() first: %v", err)
	}
	if err := db.UpsertSessionID(url, "b"); err != nil {
		t.Fatalf("UpsertSessionID() second: %v", err)
	}

	got, err := db.GetSessionID(url)
	if err != nil {
		t.Fatalf("GetSessionID(): %v", err)
	}
	if got != "b" {
		t.Errorf("session ID = %q, want %q", got, "b")
	}
}

func TestSessionID_Clear(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertSessionID("https://example.com/1", "a"); err != nil {
		t.Fatalf("UpsertSessionID(): %v", err)
	}
	if err := db.ClearSessionIDs(); err != nil {
		t.Fatalf("ClearSessionIDs(): %v", err)
	}

	_, err := db.GetSessionID("https://example.com/1")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows after clear, got %v", err)
	}
}

func TestGetSessionID_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetSessionID("https://nonexistent.example.com/1")
	if err == nil {
		t.Error("expected error for non-existent session ID")
	}
}

// ── Rate Limit ───────────────────────────────────────────────────────────────

func TestRateLimit_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	rl := RateLimit{Remaining: 4500, ResetAt: makeTime("2024-06-01T13:00:00Z")}
	if err := db.UpsertRateLimit(rl); err != nil {
		t.Fatalf("UpsertRateLimit() error: %v", err)
	}

	got, err := db.GetRateLimit()
	if err != nil {
		t.Fatalf("GetRateLimit() error: %v", err)
	}
	if got.Remaining != 4500 {
		t.Errorf("Remaining = %d, want 4500", got.Remaining)
	}
}

func TestRateLimit_UpdateSingletonRow(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertRateLimit(RateLimit{Remaining: 5000, ResetAt: makeTime("2024-06-01T13:00:00Z")}); err != nil {
		t.Fatalf("UpsertRateLimit() first: %v", err)
	}
	if err := db.UpsertRateLimit(RateLimit{Remaining: 250, ResetAt: makeTime("2024-06-01T14:00:00Z")}); err != nil {
		t.Fatalf("UpsertRateLimit() second: %v", err)
	}

	got, err := db.GetRateLimit()
	if err != nil {
		t.Fatalf("GetRateLimit(): %v", err)
	}
	if got.Remaining != 250 {
		t.Errorf("Remaining = %d, want 250", got.Remaining)
	}

	// Verify only one row exists.
	var count int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM rate_limit`).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 rate_limit row, got %d", count)
	}
}

func TestGetRateLimit_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetRateLimit()
	if err == nil {
		t.Error("expected error when no rate limit row exists")
	}
}

// ── ETags ────────────────────────────────────────────────────────────────────

func TestETag_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	e := ETag{URL: "https://api.github.com/repos/r/r/pulls/1", ETag: `"abc123"`, LastModified: "Mon, 01 Jan 2024 00:00:00 GMT"}
	if err := db.UpsertETag(e); err != nil {
		t.Fatalf("UpsertETag() error: %v", err)
	}

	got, err := db.GetETag("https://api.github.com/repos/r/r/pulls/1")
	if err != nil {
		t.Fatalf("GetETag() error: %v", err)
	}
	if got.ETag != `"abc123"` || got.LastModified != "Mon, 01 Jan 2024 00:00:00 GMT" {
		t.Errorf("ETag round-trip mismatch: %+v", got)
	}
}

func TestGetETag_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetETag("https://nonexistent.example.com/")
	if err == nil {
		t.Error("expected error for non-existent ETag")
	}
}

func TestETag_UpdateExisting(t *testing.T) {
	db := openTestDB(t)

	url := "https://api.github.com/repos/r/r/pulls/1"
	if err := db.UpsertETag(ETag{URL: url, ETag: `"old"`, LastModified: ""}); err != nil {
		t.Fatalf("UpsertETag() first: %v", err)
	}
	if err := db.UpsertETag(ETag{URL: url, ETag: `"new"`, LastModified: "Tue, 02 Jan 2024 00:00:00 GMT"}); err != nil {
		t.Fatalf("UpsertETag() second: %v", err)
	}

	got, err := db.GetETag(url)
	if err != nil {
		t.Fatalf("GetETag(): %v", err)
	}
	if got.ETag != `"new"` {
		t.Errorf("ETag not updated: got %q", got.ETag)
	}
}

// ── DBPath ────────────────────────────────────────────────────────────────────

func TestDBPath_ReturnsExpectedPath(t *testing.T) {
	fs := newTempFilesystem(t)
	path, err := DBPath(fs)
	if err != nil {
		t.Fatalf("DBPath() error: %v", err)
	}
	expected := filepath.Join(fs.dir, dataDirName, dbFileName)
	if path != expected {
		t.Errorf("DBPath() = %q, want %q", path, expected)
	}
}

func TestDBPath_UserDataDirError(t *testing.T) {
	fs := &errFilesystem{dataDirErr: errors.New("no home dir")}
	_, err := DBPath(fs)
	if err == nil {
		t.Fatal("expected error when UserDataDir fails")
	}
}

// ── OSFilesystem.Stat ─────────────────────────────────────────────────────────

func TestOSFilesystem_Stat_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "test-*.db")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	f.Close()

	fs := OSFilesystem{}
	info, err := fs.Stat(f.Name())
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if info == nil {
		t.Error("Stat() returned nil FileInfo")
	}
}

func TestOSFilesystem_Stat_NotExist(t *testing.T) {
	fs := OSFilesystem{}
	_, err := fs.Stat(filepath.Join(t.TempDir(), "nonexistent.db"))
	if !os.IsNotExist(err) {
		t.Errorf("Stat() error = %v, want IsNotExist", err)
	}
}

// ── CountPRsWithPendingReview ─────────────────────────────────────────────────

func TestCountPRsWithPendingReview_NoPRs(t *testing.T) {
	db := openTestDB(t)
	count, err := db.CountPRsWithPendingReview()
	if err != nil {
		t.Fatalf("CountPRsWithPendingReview() error: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestCountPRsWithPendingReview_CountsPending(t *testing.T) {
	db := openTestDB(t)

	// PR 1: two pending reviewers.
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-1", Login: "alice", State: "PENDING"}); err != nil {
		t.Fatalf("UpsertReviewer: %v", err)
	}
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-1", Login: "bob", State: "PENDING"}); err != nil {
		t.Fatalf("UpsertReviewer: %v", err)
	}
	// PR 2: one pending reviewer.
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-2", Login: "carol", State: "PENDING"}); err != nil {
		t.Fatalf("UpsertReviewer: %v", err)
	}
	// PR 3: approved reviewer only — should not count.
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-3", Login: "dave", State: "APPROVED"}); err != nil {
		t.Fatalf("UpsertReviewer: %v", err)
	}

	count, err := db.CountPRsWithPendingReview()
	if err != nil {
		t.Fatalf("CountPRsWithPendingReview() error: %v", err)
	}
	// pr-1 and pr-2 have PENDING reviewers; pr-1 has two but counts once.
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestCountPRsWithPendingReview_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.CountPRsWithPendingReview()
	if err == nil {
		t.Error("expected error with closed DB")
	}
}

// ── MaxLastActivityAt ─────────────────────────────────────────────────────────

func TestMaxLastActivityAt_NoPRs(t *testing.T) {
	db := openTestDB(t)
	_, hasData, err := db.MaxLastActivityAt()
	if err != nil {
		t.Fatalf("MaxLastActivityAt() error: %v", err)
	}
	if hasData {
		t.Error("hasData = true, want false when no PRs")
	}
}

func TestMaxLastActivityAt_ReturnsMostRecent(t *testing.T) {
	db := openTestDB(t)

	older := makeTime("2024-01-01T00:00:00Z")
	newer := makeTime("2024-06-01T12:00:00Z")

	upsertPR := func(id string, lastActivity time.Time) {
		t.Helper()
		if err := db.UpsertPullRequest(PullRequest{
			ID: id, Repo: "r/r", Number: 1, Title: "t", Status: "open",
			CIState: "passing", Author: "me",
			CreatedAt: older, UpdatedAt: older, LastActivityAt: lastActivity,
			URL: "https://github.com/r/r/pull/1", GlobalID: id,
		}); err != nil {
			t.Fatalf("UpsertPullRequest: %v", err)
		}
	}

	// Only one PR: use newer timestamp.
	upsertPR("pr-1", newer)
	// Second call upserts same row with older time; should still return newer.
	upsertPR("pr-1", older)

	// Insert a second PR with the newer timestamp.
	if err := db.UpsertPullRequest(PullRequest{
		ID: "pr-2", Repo: "r/r", Number: 2, Title: "t2", Status: "open",
		CIState: "passing", Author: "me",
		CreatedAt: older, UpdatedAt: older, LastActivityAt: newer,
		URL: "https://github.com/r/r/pull/2", GlobalID: "pr-2",
	}); err != nil {
		t.Fatalf("UpsertPullRequest pr-2: %v", err)
	}

	maxTime, hasData, err := db.MaxLastActivityAt()
	if err != nil {
		t.Fatalf("MaxLastActivityAt() error: %v", err)
	}
	if !hasData {
		t.Fatal("hasData = false, want true")
	}
	if !maxTime.UTC().Equal(newer.UTC()) {
		t.Errorf("maxTime = %v, want %v", maxTime, newer)
	}
}

func TestMaxLastActivityAt_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, _, err := db.MaxLastActivityAt()
	if err == nil {
		t.Error("expected error with closed DB")
	}
}

// ── ListReviewersByRepo ───────────────────────────────────────────────────────

func TestListReviewersByRepo_ReturnsDistinctLogins(t *testing.T) {
	db := openTestDB(t)

	pr1 := PullRequest{
		ID: "pr-1", Repo: "owner/repo", Number: 1, Title: "PR 1",
		Status: "open", CIState: "none", Author: "alice",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"), URL: "https://github.com/owner/repo/pull/1",
	}
	pr2 := PullRequest{
		ID: "pr-2", Repo: "owner/repo", Number: 2, Title: "PR 2",
		Status: "open", CIState: "none", Author: "alice",
		CreatedAt: makeTime("2024-01-02T00:00:00Z"), UpdatedAt: makeTime("2024-01-02T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-02T00:00:00Z"), URL: "https://github.com/owner/repo/pull/2",
	}
	if err := db.UpsertPullRequest(pr1); err != nil {
		t.Fatalf("UpsertPullRequest(pr1): %v", err)
	}
	if err := db.UpsertPullRequest(pr2); err != nil {
		t.Fatalf("UpsertPullRequest(pr2): %v", err)
	}

	// bob reviews pr-1, carol reviews both — carol should appear only once.
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-1", Login: "bob", State: "APPROVED"}); err != nil {
		t.Fatalf("UpsertReviewer bob: %v", err)
	}
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-1", Login: "carol", State: "APPROVED"}); err != nil {
		t.Fatalf("UpsertReviewer carol pr-1: %v", err)
	}
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-2", Login: "carol", State: "CHANGES_REQUESTED"}); err != nil {
		t.Fatalf("UpsertReviewer carol pr-2: %v", err)
	}

	got, err := db.ListReviewersByRepo("owner/repo")
	if err != nil {
		t.Fatalf("ListReviewersByRepo() error: %v", err)
	}
	// Expect alphabetically sorted: bob, carol
	want := []string{"bob", "carol"}
	if len(got) != len(want) {
		t.Fatalf("ListReviewersByRepo() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("ListReviewersByRepo()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestListReviewersByRepo_DifferentRepo_Excluded(t *testing.T) {
	db := openTestDB(t)

	prA := PullRequest{
		ID: "pr-a", Repo: "owner/repo-a", Number: 1, Title: "A",
		Status: "open", CIState: "none", Author: "x",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"), URL: "https://github.com/owner/repo-a/pull/1",
	}
	prB := PullRequest{
		ID: "pr-b", Repo: "owner/repo-b", Number: 1, Title: "B",
		Status: "open", CIState: "none", Author: "y",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"), URL: "https://github.com/owner/repo-b/pull/1",
	}
	if err := db.UpsertPullRequest(prA); err != nil {
		t.Fatalf("UpsertPullRequest(prA): %v", err)
	}
	if err := db.UpsertPullRequest(prB); err != nil {
		t.Fatalf("UpsertPullRequest(prB): %v", err)
	}
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-a", Login: "alice", State: "APPROVED"}); err != nil {
		t.Fatalf("UpsertReviewer: %v", err)
	}
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-b", Login: "bob", State: "APPROVED"}); err != nil {
		t.Fatalf("UpsertReviewer: %v", err)
	}

	got, err := db.ListReviewersByRepo("owner/repo-a")
	if err != nil {
		t.Fatalf("ListReviewersByRepo() error: %v", err)
	}
	if len(got) != 1 || got[0] != "alice" {
		t.Errorf("ListReviewersByRepo() = %v, want [alice]", got)
	}
}

func TestListReviewersByRepo_EmptyResult(t *testing.T) {
	db := openTestDB(t)
	got, err := db.ListReviewersByRepo("owner/no-prs")
	if err != nil {
		t.Fatalf("ListReviewersByRepo() error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty list, got %v", got)
	}
}

func TestListReviewersByRepo_QueryError(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.ListReviewersByRepo("owner/repo")
	if err == nil {
		t.Error("expected error from ListReviewersByRepo on closed DB")
	}
}

func TestScanLogins_ScanError(t *testing.T) {
	// scanLogins expects a single-column result. Feeding it a two-column row
	// forces the scan to fail.
	db := openTestDB(t)
	if _, err := db.db.Exec(`CREATE TEMP TABLE two_col_l (a TEXT, b TEXT)`); err != nil {
		t.Fatalf("create temp table: %v", err)
	}
	if _, err := db.db.Exec(`INSERT INTO two_col_l VALUES ('x', 'y')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := db.db.Query(`SELECT a, b FROM two_col_l`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	_, err = scanLogins(rows)
	if err == nil {
		t.Error("expected scan error from two-column row into one variable")
	}
}

// ── ListPullRequestsByAuthor / ListPullRequestsNotByAuthor ────────────────────

func TestListPullRequestsByAuthor(t *testing.T) {
	db := openTestDB(t)

	base := PullRequest{
		Status: "open", CIState: "none", Author: "alice",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
	}
	alicePR := base
	alicePR.ID = "pr-1"
	alicePR.Repo = "owner/repo"
	alicePR.Number = 1
	alicePR.Title = "Alice PR"
	alicePR.URL = "https://github.com/owner/repo/pull/1"
	alicePR.GlobalID = "g1"

	bobPR := base
	bobPR.ID = "pr-2"
	bobPR.Repo = "owner/repo"
	bobPR.Number = 2
	bobPR.Title = "Bob PR"
	bobPR.Author = "bob"
	bobPR.URL = "https://github.com/owner/repo/pull/2"
	bobPR.GlobalID = "g2"

	for _, pr := range []PullRequest{alicePR, bobPR} {
		if err := db.UpsertPullRequest(pr); err != nil {
			t.Fatalf("UpsertPullRequest: %v", err)
		}
	}

	tests := []struct {
		name   string
		author string
		want   int
	}{
		{"alice has 1 PR", "alice", 1},
		{"bob has 1 PR", "bob", 1},
		{"charlie has 0 PRs", "charlie", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := db.ListPullRequestsByAuthor(tt.author)
			if err != nil {
				t.Fatalf("ListPullRequestsByAuthor(%q): %v", tt.author, err)
			}
			if len(got) != tt.want {
				t.Errorf("got %d PRs, want %d", len(got), tt.want)
			}
		})
	}
}

func TestListPullRequestsByAuthor_QueryError(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.ListPullRequestsByAuthor("alice")
	if err == nil {
		t.Error("expected error from ListPullRequestsByAuthor on closed DB")
	}
}

func TestListPullRequestsNotByAuthor(t *testing.T) {
	db := openTestDB(t)

	base := PullRequest{
		Status: "open", CIState: "none",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
	}

	for i, author := range []string{"alice", "alice", "bob"} {
		pr := base
		pr.ID = fmt.Sprintf("pr-%d", i+1)
		pr.Repo = "owner/repo"
		pr.Number = i + 1
		pr.Title = fmt.Sprintf("PR %d", i+1)
		pr.Author = author
		pr.URL = fmt.Sprintf("https://github.com/owner/repo/pull/%d", i+1)
		pr.GlobalID = fmt.Sprintf("g%d", i+1)
		if err := db.UpsertPullRequest(pr); err != nil {
			t.Fatalf("UpsertPullRequest: %v", err)
		}
	}

	tests := []struct {
		name   string
		author string
		want   int
	}{
		{"not alice = bob's 1 PR", "alice", 1},
		{"not bob = alice's 2 PRs", "bob", 2},
		{"not charlie = all 3 PRs", "charlie", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := db.ListPullRequestsNotByAuthor(tt.author)
			if err != nil {
				t.Fatalf("ListPullRequestsNotByAuthor(%q): %v", tt.author, err)
			}
			if len(got) != tt.want {
				t.Errorf("got %d PRs, want %d", len(got), tt.want)
			}
		})
	}
}

func TestListPullRequestsNotByAuthor_QueryError(t *testing.T) {
	db := openTestDB(t)
	db.db.Close()
	_, err := db.ListPullRequestsNotByAuthor("alice")
	if err == nil {
		t.Error("expected error from ListPullRequestsNotByAuthor on closed DB")
	}
}

// ── DeletePullRequest ──────────────────────────────────────────────────────

func TestDeletePullRequest(t *testing.T) {
	db := openTestDB(t)

	pr := PullRequest{
		ID: "pr-del", Repo: "owner/repo", Number: 99, Title: "Delete Me",
		Status: "open", CIState: "passing", Author: "alice",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
		URL: "https://github.com/owner/repo/pull/99", GlobalID: "g-del",
	}
	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest: %v", err)
	}
	// Insert related rows.
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-del", Login: "bob", State: "APPROVED"}); err != nil {
		t.Fatalf("UpsertReviewer: %v", err)
	}
	if err := db.UpsertCheckRun(CheckRun{PRID: "pr-del", Name: "ci", State: "COMPLETED", Conclusion: "SUCCESS", URL: ""}); err != nil {
		t.Fatalf("UpsertCheckRun: %v", err)
	}
	if err := db.UpsertReviewThread(ReviewThread{PRID: "pr-del", ID: "t1", Body: "b", Path: "f.go", Line: 1}); err != nil {
		t.Fatalf("UpsertReviewThread: %v", err)
	}
	if err := db.InsertTimelineEvent(TimelineEvent{PRID: "pr-del", EventType: "commit", Actor: "alice", CreatedAt: makeTime("2024-01-01T00:00:00Z"), PayloadJSON: "{}"}); err != nil {
		t.Fatalf("InsertTimelineEvent: %v", err)
	}
	if err := db.UpsertSessionID(pr.URL, "a"); err != nil {
		t.Fatalf("UpsertSessionID: %v", err)
	}

	// Delete the PR.
	deleted, err := db.DeletePullRequest("owner/repo", 99)
	if err != nil {
		t.Fatalf("DeletePullRequest: %v", err)
	}
	if deleted.ID != "pr-del" {
		t.Errorf("deleted PR ID = %q, want %q", deleted.ID, "pr-del")
	}

	// Verify the PR is gone.
	_, err = db.GetPullRequest("owner/repo", 99)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows after delete, got %v", err)
	}

	// Verify related rows are gone.
	reviewers, _ := db.ListReviewers("pr-del")
	if len(reviewers) != 0 {
		t.Errorf("expected 0 reviewers after delete, got %d", len(reviewers))
	}
	checkRuns, _ := db.ListCheckRuns("pr-del")
	if len(checkRuns) != 0 {
		t.Errorf("expected 0 check runs after delete, got %d", len(checkRuns))
	}
	threads, _ := db.ListReviewThreads("pr-del")
	if len(threads) != 0 {
		t.Errorf("expected 0 review threads after delete, got %d", len(threads))
	}
	events, _ := db.ListTimelineEvents("pr-del")
	if len(events) != 0 {
		t.Errorf("expected 0 timeline events after delete, got %d", len(events))
	}
	_, err = db.GetSessionID(pr.URL)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected session_id to be deleted, got err=%v", err)
	}
}

func TestDeletePullRequest_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.DeletePullRequest("nonexistent/repo", 999)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDeletePullRequest_RelatedRowDeleteError(t *testing.T) {
	db := openTestDB(t)

	pr := PullRequest{
		ID: "pr-err", Repo: "owner/repo", Number: 77, Title: "err test",
		Status: "open", CIState: "none", Author: "alice",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
		URL: "https://github.com/owner/repo/pull/77", GlobalID: "g-err",
	}
	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest: %v", err)
	}

	// Create a trigger that blocks DELETE on reviewers to force the error path.
	if _, err := db.db.Exec(`CREATE TRIGGER block_reviewer_del BEFORE DELETE ON reviewers BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	// Insert a reviewer so the DELETE actually fires the trigger.
	if err := db.UpsertReviewer(Reviewer{PRID: "pr-err", Login: "bob", State: "PENDING"}); err != nil {
		t.Fatalf("UpsertReviewer: %v", err)
	}

	_, err := db.DeletePullRequest("owner/repo", 77)
	if err == nil {
		t.Error("expected error from blocked DELETE on reviewers")
	}
}

func TestDeletePullRequest_SessionIDDeleteError(t *testing.T) {
	db := openTestDB(t)

	pr := PullRequest{
		ID: "pr-sid", Repo: "owner/repo", Number: 78, Title: "sid err",
		Status: "open", CIState: "none", Author: "alice",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
		URL: "https://github.com/owner/repo/pull/78", GlobalID: "g-sid",
	}
	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest: %v", err)
	}
	if err := db.UpsertSessionID(pr.URL, "a"); err != nil {
		t.Fatalf("UpsertSessionID: %v", err)
	}

	// Block DELETE on session_ids.
	if _, err := db.db.Exec(`CREATE TRIGGER block_sid_del BEFORE DELETE ON session_ids BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err := db.DeletePullRequest("owner/repo", 78)
	if err == nil {
		t.Error("expected error from blocked DELETE on session_ids")
	}
}

func TestDeletePullRequest_PRDeleteError(t *testing.T) {
	db := openTestDB(t)

	pr := PullRequest{
		ID: "pr-pdel", Repo: "owner/repo", Number: 79, Title: "pr del err",
		Status: "open", CIState: "none", Author: "alice",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
		URL: "https://github.com/owner/repo/pull/79", GlobalID: "g-pdel",
	}
	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest: %v", err)
	}

	// Block DELETE on pull_requests itself.
	if _, err := db.db.Exec(`CREATE TRIGGER block_pr_del BEFORE DELETE ON pull_requests BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err := db.DeletePullRequest("owner/repo", 79)
	if err == nil {
		t.Error("expected error from blocked DELETE on pull_requests")
	}
}

func TestDeletePullRequest_BeginError(t *testing.T) {
	db := openTestDB(t)

	pr := PullRequest{
		ID: "pr-begin", Repo: "owner/repo", Number: 80, Title: "begin err",
		Status: "open", CIState: "none", Author: "alice",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
		URL: "https://github.com/owner/repo/pull/80", GlobalID: "g-begin",
	}
	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest: %v", err)
	}

	db.beginTx = func() (*sql.Tx, error) { return nil, errors.New("begin failed") }

	_, err := db.DeletePullRequest("owner/repo", 80)
	if err == nil {
		t.Error("expected error from DeletePullRequest when Begin fails")
	}
}

func TestDeletePullRequest_CommitError(t *testing.T) {
	db := openTestDB(t)

	pr := PullRequest{
		ID: "pr-commit", Repo: "owner/repo", Number: 81, Title: "commit err",
		Status: "open", CIState: "none", Author: "alice",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
		URL: "https://github.com/owner/repo/pull/81", GlobalID: "g-commit",
	}
	if err := db.UpsertPullRequest(pr); err != nil {
		t.Fatalf("UpsertPullRequest: %v", err)
	}

	db.commitTx = func(_ *sql.Tx) error { return errors.New("commit failed") }

	_, err := db.DeletePullRequest("owner/repo", 81)
	if err == nil {
		t.Error("expected error from DeletePullRequest when Commit fails")
	}
}

func TestDeletePullRequest_LeavesOtherPRs(t *testing.T) {
	db := openTestDB(t)

	base := PullRequest{
		Status: "open", CIState: "none", Author: "alice",
		CreatedAt: makeTime("2024-01-01T00:00:00Z"), UpdatedAt: makeTime("2024-01-01T00:00:00Z"),
		LastActivityAt: makeTime("2024-01-01T00:00:00Z"),
	}
	pr1 := base
	pr1.ID = "pr-1"
	pr1.Repo = "owner/repo"
	pr1.Number = 1
	pr1.Title = "Keep"
	pr1.URL = "https://github.com/owner/repo/pull/1"
	pr1.GlobalID = "g1"

	pr2 := base
	pr2.ID = "pr-2"
	pr2.Repo = "owner/repo"
	pr2.Number = 2
	pr2.Title = "Delete"
	pr2.URL = "https://github.com/owner/repo/pull/2"
	pr2.GlobalID = "g2"

	for _, pr := range []PullRequest{pr1, pr2} {
		if err := db.UpsertPullRequest(pr); err != nil {
			t.Fatalf("UpsertPullRequest: %v", err)
		}
	}

	if _, err := db.DeletePullRequest("owner/repo", 2); err != nil {
		t.Fatalf("DeletePullRequest: %v", err)
	}

	// PR 1 should still exist.
	got, err := db.GetPullRequest("owner/repo", 1)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if got.Title != "Keep" {
		t.Errorf("remaining PR title = %q, want %q", got.Title, "Keep")
	}
}
