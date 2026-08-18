package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/getorcal/orcal/internal/sandbox"
)

type sandboxRepo struct {
	db *sql.DB
}

const sandboxColumns = `id, name, image, state, runtime, runtime_id, cpu_millis, memory_bytes, pids_limit, env, labels, created_at, updated_at, parent_snapshot_id`

func (r *sandboxRepo) Create(ctx context.Context, s *sandbox.Sandbox) error {
	env, err := json.Marshal(s.Env)
	if err != nil {
		return fmt.Errorf("sqlite: encode env: %w", err)
	}
	labels, err := json.Marshal(s.Labels)
	if err != nil {
		return fmt.Errorf("sqlite: encode labels: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO sandboxes (`+sandboxColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Name, s.Image, string(s.State), s.Runtime, s.RuntimeID,
		s.Resources.CPUMillis, s.Resources.MemoryBytes, s.Resources.PidsLimit,
		string(env), string(labels),
		s.CreatedAt.Format(timeFormat), s.UpdatedAt.Format(timeFormat), s.ParentSnapshotID)
	if err != nil {
		if isNameUniqueViolation(err) {
			return fmt.Errorf("%w: %s", sandbox.ErrNameTaken, s.Name)
		}
		return fmt.Errorf("sqlite: insert sandbox: %w", err)
	}
	return nil
}

func (r *sandboxRepo) Get(ctx context.Context, id string) (*sandbox.Sandbox, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+sandboxColumns+` FROM sandboxes WHERE id = ?`, id)
	return scanSandbox(row)
}

func (r *sandboxRepo) GetByName(ctx context.Context, name string) (*sandbox.Sandbox, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+sandboxColumns+` FROM sandboxes WHERE name = ? AND state != 'destroyed'`, name)
	return scanSandbox(row)
}

func (r *sandboxRepo) List(ctx context.Context, f sandbox.Filter) ([]*sandbox.Sandbox, error) {
	query := `SELECT ` + sandboxColumns + ` FROM sandboxes WHERE 1 = 1`
	args := []any{}

	if len(f.States) > 0 {
		placeholders := make([]string, len(f.States))
		for i, st := range f.States {
			placeholders[i] = "?"
			args = append(args, string(st))
		}
		query += ` AND state IN (` + strings.Join(placeholders, ",") + `)`
	} else {
		query += ` AND state != 'destroyed'`
	}
	if len(f.Labels) > 0 {
		keys := make([]string, 0, len(f.Labels))
		for k := range f.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			query += ` AND EXISTS (SELECT 1 FROM json_each(labels) WHERE json_each.key = ? AND json_each.value = ?)`
			args = append(args, k, f.Labels[k])
		}
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
		return nil, fmt.Errorf("sqlite: list sandboxes: %w", err)
	}
	defer rows.Close()

	var out []*sandbox.Sandbox
	for rows.Next() {
		s, err := scanSandbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *sandboxRepo) Update(ctx context.Context, s *sandbox.Sandbox) error {
	env, _ := json.Marshal(s.Env)
	labels, _ := json.Marshal(s.Labels)
	res, err := r.db.ExecContext(ctx,
		`UPDATE sandboxes SET name=?, image=?, state=?, runtime=?, runtime_id=?, cpu_millis=?, memory_bytes=?, pids_limit=?, env=?, labels=?, updated_at=?, parent_snapshot_id=? WHERE id=?`,
		s.Name, s.Image, string(s.State), s.Runtime, s.RuntimeID,
		s.Resources.CPUMillis, s.Resources.MemoryBytes, s.Resources.PidsLimit,
		string(env), string(labels), s.UpdatedAt.Format(timeFormat), s.ParentSnapshotID, s.ID)
	if err != nil {
		if isNameUniqueViolation(err) {
			return fmt.Errorf("%w: %s", sandbox.ErrNameTaken, s.Name)
		}
		return fmt.Errorf("sqlite: update sandbox: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: update sandbox rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", sandbox.ErrNotFound, s.ID)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSandbox(row scanner) (*sandbox.Sandbox, error) {
	var (
		s                    sandbox.Sandbox
		state                string
		env, labels          string
		createdAt, updatedAt string
		parentSnapshotID     sql.NullString
	)
	err := row.Scan(&s.ID, &s.Name, &s.Image, &state, &s.Runtime, &s.RuntimeID,
		&s.Resources.CPUMillis, &s.Resources.MemoryBytes, &s.Resources.PidsLimit,
		&env, &labels, &createdAt, &updatedAt, &parentSnapshotID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sandbox.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: scan sandbox: %w", err)
	}
	if parentSnapshotID.Valid {
		id := parentSnapshotID.String
		s.ParentSnapshotID = &id
	}

	s.State = sandbox.State(state)
	if err := json.Unmarshal([]byte(env), &s.Env); err != nil {
		return nil, fmt.Errorf("sqlite: decode env: %w", err)
	}
	if err := json.Unmarshal([]byte(labels), &s.Labels); err != nil {
		return nil, fmt.Errorf("sqlite: decode labels: %w", err)
	}
	if s.CreatedAt, err = time.Parse(timeFormat, createdAt); err != nil {
		return nil, fmt.Errorf("sqlite: decode created_at: %w", err)
	}
	if s.UpdatedAt, err = time.Parse(timeFormat, updatedAt); err != nil {
		return nil, fmt.Errorf("sqlite: decode updated_at: %w", err)
	}
	return &s, nil
}

func isNameUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "sandboxes.name")
}
