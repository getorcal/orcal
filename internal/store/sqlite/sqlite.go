package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"

	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/snapshot"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Fixed-width fractional seconds, unlike time.RFC3339Nano which trims trailing zeros.
// Timestamps are stored as TEXT and compared lexicographically, so a variable-width
// format sorts wrong — "…:05.9Z" would order after "…:05.10Z".
const timeFormat = "2006-01-02T15:04:05.000000000Z07:00"

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	// SQLite permits one writer; serializing every connection avoids SQLITE_BUSY under
	// concurrent sandbox operations at the cost of throughput this daemon does not need.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("sqlite: migration table: %w", err)
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("sqlite: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for i, name := range names {
		version := i + 1
		var applied int
		err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&applied)
		if err != nil {
			return fmt.Errorf("sqlite: check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("sqlite: read migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("sqlite: apply migration %s: %w", name, err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			return fmt.Errorf("sqlite: record migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) Sandboxes() sandbox.Repo { return &sandboxRepo{db: s.db} }

func (s *Store) Execs() exec.Repo { return &execRepo{db: s.db} }

func (s *Store) Snapshots() snapshot.Repo { return &snapshotRepo{db: s.db} }

func (s *Store) Settings() *SettingsStore { return &SettingsStore{db: s.db} }

func (s *Store) Close() error { return s.db.Close() }
