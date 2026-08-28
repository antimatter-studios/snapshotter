// Package changedb is the change_detection table: the differences this
// application has found, kept between runs.
//
// # Why only differences, and never the absence of one
//
// A folder that DIFFERS is proved by one file. A folder that does not differ is
// proved by reading everything under it, and there is no early exit from that.
// So the two facts are worth wildly different amounts and, more importantly, they
// keep differently.
//
// A recorded difference is safe to keep for ever, because it is never trusted.
// Every use of one re-checks the path it names: still different, and the folder
// still differs — for that folder and every folder above it. Put back, and the
// row is dropped and the walk happens once more. Nothing is ever concluded from
// this table alone.
//
// A recorded SAMENESS would be a different promise entirely: a claim about
// everything that happened while this application was not running. Nothing here
// can keep that promise, so nothing here makes it. Unchanged verdicts live in
// memory and die with the process.
//
// # Why the parent and the name are not stored
//
// They are columns, and they are derived. A path is one fact; a path plus its
// directory plus its filename is one fact and two copies of parts of it, and
// copies can disagree — after which nothing can say which is right. SQLite's
// generated columns give the table its dirs-and-filenames shape while only the
// path is ever written, so the two cannot drift apart.
package changedb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the table on disk.
type Store struct{ db *sql.DB }

// schema is deliberately small. Two tables: what was found, and how far the event
// log has been read.
//
// name and parent are GENERATED ALWAYS ... VIRTUAL, so they cost no storage and
// cannot be written. The expressions are the standard SQLite idiom for splitting
// a path: rtrim(path, replace(path,'/',”)) removes the trailing run of
// non-slash characters, leaving the directory with its slash.
const schema = `
CREATE TABLE IF NOT EXISTS change_detection (
    snapshot TEXT NOT NULL,
    path     TEXT NOT NULL,
    parent   TEXT GENERATED ALWAYS AS (
                 CASE WHEN rtrim(rtrim(path, replace(path, '/', '')), '/') = ''
                      THEN '/'
                      ELSE rtrim(rtrim(path, replace(path, '/', '')), '/')
                 END) VIRTUAL,
    name     TEXT GENERATED ALWAYS AS (
                 replace(path, rtrim(path, replace(path, '/', '')), '')) VIRTUAL,
    recorded INTEGER NOT NULL,
    PRIMARY KEY (snapshot, path)
);
CREATE INDEX IF NOT EXISTS change_detection_by_parent
    ON change_detection(snapshot, parent);

-- The settings the recorded differences were found under.
--
-- Kept because "does this row still mean anything" is a question about the rules
-- in force when it was written, and those outlive the process just as the rows
-- do. Without it a new run starts with no idea what the old one was doing, sees
-- a mismatch against its own settings, and throws away everything it just loaded.
CREATE TABLE IF NOT EXISTS rules (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    fingerprint TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS event_log_cursor (
    volume TEXT PRIMARY KEY,
    uuid   TEXT NOT NULL,
    id     INTEGER NOT NULL
);
`

// Open prepares the table, creating it if this is the first run.
//
// A store that will not open is returned as an error and the caller carries on
// without one. Everything this holds is an optimisation: without it every folder
// is walked, which is what the application did before this existed and is slow
// rather than wrong.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("changedb: %w", err)
	}
	// WAL so a reader is never blocked by the writer, and a five-second busy
	// timeout so a slow disk is waited for rather than reported as a failure.
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("changedb: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("changedb: %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Record remembers that a path differed from a snapshot.
//
// Idempotent: the same difference found twice is one row, and the second sighting
// refreshes when it was last seen rather than adding another.
func (s *Store) Record(snapshot, path string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO change_detection (snapshot, path, recorded) VALUES (?, ?, ?)
		 ON CONFLICT(snapshot, path) DO UPDATE SET recorded = excluded.recorded`,
		snapshot, filepath.Clean(path), time.Now().Unix())
	return err
}

// Forget drops a path that no longer differs.
//
// Keeping it would cost a stat before every walk it was meant to save, and that
// stat can never again succeed.
func (s *Store) Forget(snapshot, path string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM change_detection WHERE snapshot = ? AND path = ?`,
		snapshot, filepath.Clean(path))
	return err
}

// Under returns a known difference somewhere beneath a folder, shallowest first.
//
// Shallowest because it is the one most likely to still be there, and because
// re-checking it reads fewer directories on the way. The caller re-checks it: this
// says where a difference was last seen, not that one is there now.
func (s *Store) Under(snapshot, folder string) (string, bool) {
	if s == nil || s.db == nil {
		return "", false
	}
	dir := filepath.Clean(folder)
	// The folder itself as well as everything under it: a difference recorded
	// against a path IS a difference under that path.
	prefix := dir + "/"
	if dir == "/" {
		prefix = "/"
	}

	var path string
	err := s.db.QueryRow(
		`SELECT path FROM change_detection
		 WHERE snapshot = ? AND (path = ? OR path LIKE ? || '%')
		 ORDER BY length(path) LIMIT 1`,
		snapshot, dir, prefix).Scan(&path)
	if err != nil {
		return "", false
	}
	return path, true
}

// ForgetSnapshot drops everything recorded against one snapshot.
//
// Called when it is unmounted. The paths are on the live disk, but what they
// record is a difference from that snapshot in particular.
func (s *Store) ForgetSnapshot(snapshot string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM change_detection WHERE snapshot = ?`, snapshot)
	return err
}

// Clear empties the table.
//
// Used when the settings that decide what counts as a difference change: paths
// recorded under the old ones are not answers to the new question.
func (s *Store) Clear() error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM change_detection`)
	return err
}

// Count reports how many differences are held, for tests and for deciding whether
// any of this is worth its keep.
func (s *Store) Count(snapshot string) int {
	if s == nil || s.db == nil {
		return 0
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM change_detection WHERE snapshot = ?`, snapshot).Scan(&n); err != nil {
		return 0
	}
	return n
}

// Cursor is how far a volume's event log has been read, and which log that was.
//
// The uuid is the half that makes the id mean anything. A log that has been wiped
// and started again gets a new one, and every id recorded against the old one is
// meaningless — which is a thing that can happen to a removable disk whose
// /.fseventsd its owner can delete.
func (s *Store) Cursor(volume string) (uuid string, id uint64, ok bool) {
	if s == nil || s.db == nil {
		return "", 0, false
	}
	if err := s.db.QueryRow(`SELECT uuid, id FROM event_log_cursor WHERE volume = ?`, volume).
		Scan(&uuid, &id); err != nil {
		return "", 0, false
	}
	return uuid, id, true
}

// SetCursor records how far the log has been read.
func (s *Store) SetCursor(volume, uuid string, id uint64) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO event_log_cursor (volume, uuid, id) VALUES (?, ?, ?)
		 ON CONFLICT(volume) DO UPDATE SET uuid = excluded.uuid, id = excluded.id`,
		volume, uuid, id)
	return err
}

// Rules is the settings fingerprint the stored differences were found under.
//
// Empty when nothing has been recorded yet, which a caller must treat as "adopt
// mine" rather than as a mismatch — otherwise the first lookup of every run
// discards the table it has just opened.
func (s *Store) Rules() string {
	if s == nil || s.db == nil {
		return ""
	}
	var fingerprint string
	if err := s.db.QueryRow(`SELECT fingerprint FROM rules WHERE id = 1`).Scan(&fingerprint); err != nil {
		return ""
	}
	return fingerprint
}

// SetRules records the settings the stored differences were found under.
func (s *Store) SetRules(fingerprint string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO rules (id, fingerprint) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET fingerprint = excluded.fingerprint`, fingerprint)
	return err
}

// Path is where the table lives.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("changedb: cannot find the home directory: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "Snapshotter", "change-detection.db"), nil
}
