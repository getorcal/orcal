package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getorcal/orcal/internal/snapshot"
)

type snapshotRepo struct {
	db *sql.DB
}

const snapshotColumns = `id, name, sandbox_id, parent_id, runtime_ref, image, size_bytes, created_at`

func (r *snapshotRepo) Create(ctx context.Context, s *snapshot.Snapshot) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO snapshots (`+snapshotColumns+`) VALUES (?,?,?,?,?,?,?,?)`,
		s.ID, s.Name, s.SandboxID, s.ParentID, s.RuntimeRef, s.Image, s.SizeBytes,
		s.CreatedAt.Format(timeFormat))
	if err != nil {
		if strings.Contains(err.Error(), "snapshots.name") {
			return fmt.Errorf("%w: %s", snapshot.ErrNameTaken, s.Name)
		}
		return fmt.Errorf("sqlite: insert snapshot: %w", err)
	}
	return nil
}

func (r *snapshotRepo) Get(ctx context.Context, id string) (*snapshot.Snapshot, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM snapshots WHERE id = ?`, id)
	return scanSnapshot(row)
}

func (r *snapshotRepo) GetByName(ctx context.Context, name string) (*snapshot.Snapshot, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM snapshots WHERE name = ?`, name)
	return scanSnapshot(row)
}

func (r *snapshotRepo) List(ctx context.Context, f snapshot.Filter) ([]*snapshot.Snapshot, error) {
	query := `SELECT ` + snapshotColumns + ` FROM snapshots WHERE 1 = 1`
	args := []any{}

	if f.SandboxID != "" {
		query += ` AND sandbox_id = ?`
		args = append(args, f.SandboxID)
	}
	if f.Cursor != "" {
		query += ` AND id > ?`
		args = append(args, f.Cursor)
	}
	query += ` ORDER BY id`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list snapshots: %w", err)
	}
	defer rows.Close()

	var out []*snapshot.Snapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *snapshotRepo) CountChildren(ctx context.Context, id string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM snapshots WHERE parent_id = ?`, id).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("sqlite: count children: %w", err)
	}
	return n, nil
}

func (r *snapshotRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM snapshots WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete snapshot: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: delete snapshot rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", snapshot.ErrNotFound, id)
	}
	return nil
}

func scanSnapshot(row scanner) (*snapshot.Snapshot, error) {
	var (
		s         snapshot.Snapshot
		parent    sql.NullString
		createdAt string
	)
	err := row.Scan(&s.ID, &s.Name, &s.SandboxID, &parent, &s.RuntimeRef, &s.Image, &s.SizeBytes, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, snapshot.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: scan snapshot: %w", err)
	}
	if parent.Valid {
		id := parent.String
		s.ParentID = &id
	}
	if s.CreatedAt, err = time.Parse(timeFormat, createdAt); err != nil {
		return nil, fmt.Errorf("sqlite: decode created_at: %w", err)
	}
	return &s, nil
}
