// Package store persists hosts, tasks, runs and logs in SQLite (pure Go driver).
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Songxwn/Auto-Deploy/internal/models"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("记录不存在")

// NewID returns a random 24-char hex identifier.
func NewID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (and migrates) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// A single connection avoids SQLITE_BUSY and makes PRAGMAs reliable.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=10000; PRAGMA synchronous=NORMAL;`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS hosts (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	addr        TEXT NOT NULL,
	port        INTEGER NOT NULL DEFAULT 22,
	user        TEXT NOT NULL,
	auth_type   TEXT NOT NULL DEFAULT 'password',
	secret      TEXT NOT NULL DEFAULT '',
	passphrase  TEXT NOT NULL DEFAULT '',
	os_type     TEXT NOT NULL DEFAULT 'linux',
	tags        TEXT NOT NULL DEFAULT '',
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tasks (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	os_type     TEXT NOT NULL DEFAULT 'linux',
	steps_json  TEXT NOT NULL DEFAULT '[]',
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS runs (
	id          TEXT PRIMARY KEY,
	task_id     TEXT NOT NULL,
	task_name   TEXT NOT NULL,
	status      TEXT NOT NULL,
	created_at  INTEGER NOT NULL,
	started_at  INTEGER NOT NULL,
	finished_at INTEGER
);
CREATE TABLE IF NOT EXISTS run_hosts (
	id          TEXT PRIMARY KEY,
	run_id      TEXT NOT NULL,
	host_id     TEXT NOT NULL,
	host_name   TEXT NOT NULL,
	status      TEXT NOT NULL,
	exit_code   INTEGER NOT NULL DEFAULT -1,
	started_at  INTEGER,
	finished_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_run_hosts_run ON run_hosts(run_id);
CREATE TABLE IF NOT EXISTS logs (
	run_host_id TEXT NOT NULL,
	seq         INTEGER NOT NULL,
	ts          INTEGER NOT NULL,
	stream      TEXT NOT NULL,
	text        TEXT NOT NULL,
	PRIMARY KEY (run_host_id, seq)
);
CREATE TABLE IF NOT EXISTS templates (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	os_type     TEXT NOT NULL DEFAULT 'linux',
	category    TEXT NOT NULL DEFAULT 'custom',
	script      TEXT NOT NULL,
	built_in    INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);
`)
	return err
}

func ts(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromTs(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

func nullTs(v sql.NullInt64) *time.Time {
	if !v.Valid || v.Int64 == 0 {
		return nil
	}
	t := time.Unix(v.Int64, 0)
	return &t
}

// ---------------------------------------------------------------- hosts

const hostCols = `id,name,addr,port,user,auth_type,secret,passphrase,os_type,tags,created_at,updated_at`

func scanHost(row interface{ Scan(...any) error }) (models.Host, error) {
	var h models.Host
	var created, updated int64
	err := row.Scan(&h.ID, &h.Name, &h.Addr, &h.Port, &h.User, &h.AuthType,
		&h.Secret, &h.Passphrase, &h.OSType, &h.Tags, &created, &updated)
	h.CreatedAt, h.UpdatedAt = fromTs(created), fromTs(updated)
	return h, err
}

// CreateHost inserts a host.
func (s *Store) CreateHost(h *models.Host) error {
	_, err := s.db.Exec(`INSERT INTO hosts(`+hostCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		h.ID, h.Name, h.Addr, h.Port, h.User, h.AuthType, h.Secret, h.Passphrase,
		h.OSType, h.Tags, ts(h.CreatedAt), ts(h.UpdatedAt))
	return err
}

// ListHosts returns all hosts ordered by creation time.
func (s *Store) ListHosts() ([]models.Host, error) {
	rows, err := s.db.Query(`SELECT ` + hostCols + ` FROM hosts ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hosts := []models.Host{}
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

// GetHost returns one host or ErrNotFound.
func (s *Store) GetHost(id string) (models.Host, error) {
	h, err := scanHost(s.db.QueryRow(`SELECT `+hostCols+` FROM hosts WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Host{}, ErrNotFound
	}
	return h, err
}

// UpdateHost updates all mutable fields of a host.
func (s *Store) UpdateHost(h *models.Host) error {
	res, err := s.db.Exec(`UPDATE hosts SET name=?,addr=?,port=?,user=?,auth_type=?,secret=?,passphrase=?,os_type=?,tags=?,updated_at=? WHERE id=?`,
		h.Name, h.Addr, h.Port, h.User, h.AuthType, h.Secret, h.Passphrase,
		h.OSType, h.Tags, ts(h.UpdatedAt), h.ID)
	if err != nil {
		return err
	}
	return affected(res)
}

// DeleteHost removes a host.
func (s *Store) DeleteHost(id string) error {
	res, err := s.db.Exec(`DELETE FROM hosts WHERE id=?`, id)
	if err != nil {
		return err
	}
	return affected(res)
}

// ---------------------------------------------------------------- tasks

const taskCols = `id,name,description,os_type,steps_json,created_at,updated_at`

func scanTask(row interface{ Scan(...any) error }) (models.Task, error) {
	var t models.Task
	var stepsJSON string
	var created, updated int64
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.OSType, &stepsJSON, &created, &updated)
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal([]byte(stepsJSON), &t.Steps); err != nil {
		return t, fmt.Errorf("任务步骤数据损坏: %w", err)
	}
	t.CreatedAt, t.UpdatedAt = fromTs(created), fromTs(updated)
	return t, nil
}

// CreateTask inserts a task.
func (s *Store) CreateTask(t *models.Task) error {
	steps, err := json.Marshal(t.Steps)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO tasks(`+taskCols+`) VALUES(?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.Description, t.OSType, string(steps), ts(t.CreatedAt), ts(t.UpdatedAt))
	return err
}

// ListTasks returns all tasks ordered by creation time.
func (s *Store) ListTasks() ([]models.Task, error) {
	rows, err := s.db.Query(`SELECT ` + taskCols + ` FROM tasks ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []models.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// GetTask returns one task or ErrNotFound.
func (s *Store) GetTask(id string) (models.Task, error) {
	t, err := scanTask(s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Task{}, ErrNotFound
	}
	return t, err
}

// UpdateTask updates all mutable fields of a task.
func (s *Store) UpdateTask(t *models.Task) error {
	steps, err := json.Marshal(t.Steps)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE tasks SET name=?,description=?,os_type=?,steps_json=?,updated_at=? WHERE id=?`,
		t.Name, t.Description, t.OSType, string(steps), ts(t.UpdatedAt), t.ID)
	if err != nil {
		return err
	}
	return affected(res)
}

// DeleteTask removes a task.
func (s *Store) DeleteTask(id string) error {
	res, err := s.db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	if err != nil {
		return err
	}
	return affected(res)
}

// ---------------------------------------------------------------- runs

// CreateRun inserts a run together with its per-host records.
func (s *Store) CreateRun(r *models.Run) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO runs(id,task_id,task_name,status,created_at,started_at,finished_at) VALUES(?,?,?,?,?,?,?)`,
		r.ID, r.TaskID, r.TaskName, r.Status, ts(r.CreatedAt), ts(r.StartedAt), nil); err != nil {
		return err
	}
	for _, rh := range r.Hosts {
		if _, err := tx.Exec(`INSERT INTO run_hosts(id,run_id,host_id,host_name,status,exit_code,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?)`,
			rh.ID, rh.RunID, rh.HostID, rh.HostName, rh.Status, rh.ExitCode, nil, nil); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const runCols = `id,task_id,task_name,status,created_at,started_at,finished_at`

func scanRun(row interface{ Scan(...any) error }) (models.Run, error) {
	var r models.Run
	var created, started int64
	var finished sql.NullInt64
	err := row.Scan(&r.ID, &r.TaskID, &r.TaskName, &r.Status, &created, &started, &finished)
	r.CreatedAt, r.StartedAt, r.FinishedAt = fromTs(created), fromTs(started), nullTs(finished)
	return r, err
}

// ListRuns returns the most recent runs (newest first) with their hosts.
func (s *Store) ListRuns(limit int) ([]models.Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT `+runCols+` FROM runs ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	runs := []models.Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	// Close the result set before issuing further queries: the pool is
	// limited to a single connection, so nested queries would deadlock.
	rows.Close()
	for i := range runs {
		hosts, err := s.RunHosts(runs[i].ID)
		if err != nil {
			return nil, err
		}
		runs[i].Hosts = hosts
	}
	return runs, nil
}

// GetRun returns one run with its hosts, or ErrNotFound.
func (s *Store) GetRun(id string) (models.Run, error) {
	r, err := scanRun(s.db.QueryRow(`SELECT `+runCols+` FROM runs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Run{}, ErrNotFound
	}
	if err != nil {
		return models.Run{}, err
	}
	r.Hosts, err = s.RunHosts(id)
	return r, err
}

// RunHosts returns the per-host records of a run.
func (s *Store) RunHosts(runID string) ([]models.RunHost, error) {
	rows, err := s.db.Query(`SELECT id,run_id,host_id,host_name,status,exit_code,started_at,finished_at FROM run_hosts WHERE run_id=? ORDER BY rowid`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hosts := []models.RunHost{}
	for rows.Next() {
		var rh models.RunHost
		var started, finished sql.NullInt64
		if err := rows.Scan(&rh.ID, &rh.RunID, &rh.HostID, &rh.HostName, &rh.Status, &rh.ExitCode, &started, &finished); err != nil {
			return nil, err
		}
		rh.StartedAt, rh.FinishedAt = nullTs(started), nullTs(finished)
		hosts = append(hosts, rh)
	}
	return hosts, rows.Err()
}

// UpdateRunStatus sets the final status of a run.
func (s *Store) UpdateRunStatus(id, status string, finished *time.Time) error {
	var fin any
	if finished != nil {
		fin = ts(*finished)
	}
	res, err := s.db.Exec(`UPDATE runs SET status=?,finished_at=? WHERE id=?`, status, fin, id)
	if err != nil {
		return err
	}
	return affected(res)
}

// StartRunHost marks a run-host as running.
func (s *Store) StartRunHost(id string, t time.Time) error {
	res, err := s.db.Exec(`UPDATE run_hosts SET status=?,started_at=? WHERE id=?`, models.StatusRunning, ts(t), id)
	if err != nil {
		return err
	}
	return affected(res)
}

// FinishRunHost records the final state of a run-host.
func (s *Store) FinishRunHost(id, status string, exitCode int, t time.Time) error {
	res, err := s.db.Exec(`UPDATE run_hosts SET status=?,exit_code=?,finished_at=? WHERE id=?`, status, exitCode, ts(t), id)
	if err != nil {
		return err
	}
	return affected(res)
}

// ---------------------------------------------------------------- logs

// AppendLog stores one output line.
func (s *Store) AppendLog(l models.LogLine) error {
	_, err := s.db.Exec(`INSERT INTO logs(run_host_id,seq,ts,stream,text) VALUES(?,?,?,?,?)`,
		l.RunHostID, l.Seq, ts(l.TS), l.Stream, l.Text)
	return err
}

// LogsForRun returns all persisted lines of a run ordered by host and sequence.
func (s *Store) LogsForRun(runID string) ([]models.LogLine, error) {
	rows, err := s.db.Query(`
SELECT l.run_host_id,l.seq,l.ts,l.stream,l.text
FROM logs l JOIN run_hosts rh ON l.run_host_id=rh.id
WHERE rh.run_id=? ORDER BY rh.rowid, l.seq`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []models.LogLine{}
	for rows.Next() {
		var l models.LogLine
		var t int64
		if err := rows.Scan(&l.RunHostID, &l.Seq, &t, &l.Stream, &l.Text); err != nil {
			return nil, err
		}
		l.TS = fromTs(t)
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// ---------------------------------------------------------------- templates

const templateCols = `id,name,description,os_type,category,script,built_in,created_at,updated_at`

func scanTemplate(row interface{ Scan(...any) error }) (models.Template, error) {
	var t models.Template
	var builtIn int
	var created, updated int64
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.OSType, &t.Category,
		&t.Script, &builtIn, &created, &updated)
	t.BuiltIn = builtIn != 0
	t.CreatedAt, t.UpdatedAt = fromTs(created), fromTs(updated)
	return t, err
}

// SeedTemplates inserts built-in templates idempotently (stable IDs).
func (s *Store) SeedTemplates(list []models.Template) error {
	for i := range list {
		t := &list[i]
		built := 0
		if t.BuiltIn {
			built = 1
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO templates(`+templateCols+`) VALUES(?,?,?,?,?,?,?,?,?)`,
			t.ID, t.Name, t.Description, t.OSType, t.Category, t.Script, built, ts(t.CreatedAt), ts(t.UpdatedAt)); err != nil {
			return err
		}
	}
	return nil
}

// CreateTemplate inserts a template.
func (s *Store) CreateTemplate(t *models.Template) error {
	built := 0
	if t.BuiltIn {
		built = 1
	}
	_, err := s.db.Exec(`INSERT INTO templates(`+templateCols+`) VALUES(?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.Description, t.OSType, t.Category, t.Script, built, ts(t.CreatedAt), ts(t.UpdatedAt))
	return err
}

// ListTemplates returns all templates, built-ins first then by category/name.
func (s *Store) ListTemplates() ([]models.Template, error) {
	rows, err := s.db.Query(`SELECT ` + templateCols + ` FROM templates ORDER BY built_in DESC, category, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates := []models.Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

// GetTemplate returns one template or ErrNotFound.
func (s *Store) GetTemplate(id string) (models.Template, error) {
	t, err := scanTemplate(s.db.QueryRow(`SELECT `+templateCols+` FROM templates WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Template{}, ErrNotFound
	}
	return t, err
}

// UpdateTemplate updates all mutable fields of a template.
func (s *Store) UpdateTemplate(t *models.Template) error {
	res, err := s.db.Exec(`UPDATE templates SET name=?,description=?,os_type=?,category=?,script=?,updated_at=? WHERE id=?`,
		t.Name, t.Description, t.OSType, t.Category, t.Script, ts(t.UpdatedAt), t.ID)
	if err != nil {
		return err
	}
	return affected(res)
}

// DeleteTemplate removes a template.
func (s *Store) DeleteTemplate(id string) error {
	res, err := s.db.Exec(`DELETE FROM templates WHERE id=?`, id)
	if err != nil {
		return err
	}
	return affected(res)
}

func affected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
