package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/getorcal/orcal/internal/exec"
)

type execRepo struct {
	db *sql.DB
}

const execColumns = `id, sandbox_id, runtime_exec_id, command, env, working_dir, user, state, exit_code, output_bytes, truncated, started_at, finished_at`

func (r *execRepo) Create(ctx context.Context, e *exec.Exec) error {
	command, err := json.Marshal(e.Command)
	if err != nil {
		return fmt.Errorf("sqlite: encode command: %w", err)
	}
	env, err := json.Marshal(e.Env)
	if err != nil {
		return fmt.Errorf("sqlite: encode exec env: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO execs (`+execColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.SandboxID, e.RuntimeExecID, string(command), string(env),
		e.WorkingDir, e.User, string(e.State), e.ExitCode, e.OutputBytes,
		boolToInt(e.Truncated), e.StartedAt.Format(timeFormat), formatNullable(e.FinishedAt))
	if err != nil {
		return fmt.Errorf("sqlite: insert exec: %w", err)
	}
	return nil
}

func (r *execRepo) Get(ctx context.Context, id string) (*exec.Exec, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+execColumns+` FROM execs WHERE id = ?`, id)
	return scanExec(row)
}

func (r *execRepo) ListBySandbox(ctx context.Context, sandboxID string, f exec.Filter) ([]*exec.Exec, error) {
	query := `SELECT ` + execColumns + ` FROM execs WHERE sandbox_id = ?`
	args := []any{sandboxID}
	if f.Cursor != "" {
		query += ` AND id > ?`
		args = append(args, f.Cursor)
	}
	query += ` ORDER BY id`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	return r.query(ctx, query, args...)
}

func (r *execRepo) ListRunning(ctx context.Context) ([]*exec.Exec, error) {
	return r.query(ctx, `SELECT `+execColumns+` FROM execs WHERE state = ? ORDER BY id`, string(exec.StateRunning))
}

func (r *execRepo) Update(ctx context.Context, e *exec.Exec) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE execs SET runtime_exec_id=?, state=?, exit_code=?, output_bytes=?, truncated=?, finished_at=? WHERE id=?`,
		e.RuntimeExecID, string(e.State), e.ExitCode, e.OutputBytes,
		boolToInt(e.Truncated), formatNullable(e.FinishedAt), e.ID)
	if err != nil {
		return fmt.Errorf("sqlite: update exec: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: update exec rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", exec.ErrNotFound, e.ID)
	}
	return nil
}

func (r *execRepo) query(ctx context.Context, query string, args ...any) ([]*exec.Exec, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list execs: %w", err)
	}
	defer rows.Close()

	var out []*exec.Exec
	for rows.Next() {
		e, err := scanExec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanExec(row scanner) (*exec.Exec, error) {
	var (
		e          exec.Exec
		state      string
		command    string
		env        string
		exitCode   sql.NullInt64
		truncated  int
		startedAt  string
		finishedAt sql.NullString
	)
	err := row.Scan(&e.ID, &e.SandboxID, &e.RuntimeExecID, &command, &env,
		&e.WorkingDir, &e.User, &state, &exitCode, &e.OutputBytes,
		&truncated, &startedAt, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, exec.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: scan exec: %w", err)
	}

	e.State = exec.State(state)
	e.Truncated = truncated == 1
	if err := json.Unmarshal([]byte(command), &e.Command); err != nil {
		return nil, fmt.Errorf("sqlite: decode command: %w", err)
	}
	if err := json.Unmarshal([]byte(env), &e.Env); err != nil {
		return nil, fmt.Errorf("sqlite: decode exec env: %w", err)
	}
	if exitCode.Valid {
		code := int(exitCode.Int64)
		e.ExitCode = &code
	}
	if e.StartedAt, err = time.Parse(timeFormat, startedAt); err != nil {
		return nil, fmt.Errorf("sqlite: decode started_at: %w", err)
	}
	if finishedAt.Valid {
		t, err := time.Parse(timeFormat, finishedAt.String)
		if err != nil {
			return nil, fmt.Errorf("sqlite: decode finished_at: %w", err)
		}
		e.FinishedAt = &t
	}
	return &e, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func formatNullable(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(timeFormat)
}
