package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getorcal/orcal/internal/auth"
)

type tokenRepo struct {
	db *sql.DB
}

const tokenColumns = `id, name, hash, prefix, scopes, created_at, expires_at, last_used_at, revoked_at`

func (r *tokenRepo) Create(ctx context.Context, t *auth.Token) error {
	scopes, err := json.Marshal(t.Scopes)
	if err != nil {
		return fmt.Errorf("sqlite: encode scopes: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO tokens (`+tokenColumns+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.Hash, t.Prefix, string(scopes),
		t.CreatedAt.Format(timeFormat), nullTime(t.ExpiresAt), nullTime(t.LastUsedAt), nullTime(t.RevokedAt))
	if err != nil {
		if strings.Contains(err.Error(), "tokens.name") {
			return fmt.Errorf("%w: %s", auth.ErrNameTaken, t.Name)
		}
		return fmt.Errorf("sqlite: insert token: %w", err)
	}
	return nil
}

func (r *tokenRepo) Get(ctx context.Context, id string) (*auth.Token, error) {
	return scanToken(r.db.QueryRowContext(ctx, `SELECT `+tokenColumns+` FROM tokens WHERE id = ?`, id))
}

func (r *tokenRepo) GetByHash(ctx context.Context, hash string) (*auth.Token, error) {
	return scanToken(r.db.QueryRowContext(ctx, `SELECT `+tokenColumns+` FROM tokens WHERE hash = ?`, hash))
}

func (r *tokenRepo) GetByName(ctx context.Context, name string) (*auth.Token, error) {
	return scanToken(r.db.QueryRowContext(ctx,
		`SELECT `+tokenColumns+` FROM tokens WHERE name = ? AND revoked_at IS NULL`, name))
}

func (r *tokenRepo) List(ctx context.Context) ([]*auth.Token, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+tokenColumns+` FROM tokens ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list tokens: %w", err)
	}
	defer rows.Close()

	var out []*auth.Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list tokens: %w", err)
	}
	return out, nil
}

func (r *tokenRepo) Update(ctx context.Context, t *auth.Token) error {
	scopes, err := json.Marshal(t.Scopes)
	if err != nil {
		return fmt.Errorf("sqlite: encode scopes: %w", err)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE tokens SET name = ?, hash = ?, prefix = ?, scopes = ?, expires_at = ?, last_used_at = ?, revoked_at = ? WHERE id = ?`,
		t.Name, t.Hash, t.Prefix, string(scopes),
		nullTime(t.ExpiresAt), nullTime(t.LastUsedAt), nullTime(t.RevokedAt), t.ID)
	if err != nil {
		return fmt.Errorf("sqlite: update token: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: update token: %w", err)
	}
	if affected == 0 {
		return auth.ErrNotFound
	}
	return nil
}

func (r *tokenRepo) TouchLastUsed(ctx context.Context, id string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tokens SET last_used_at = ? WHERE id = ?`, at.Format(timeFormat), id)
	if err != nil {
		return fmt.Errorf("sqlite: touch token: %w", err)
	}
	return nil
}

func scanToken(row scanner) (*auth.Token, error) {
	var (
		t                                auth.Token
		scopes, createdAt                string
		expiresAt, lastUsedAt, revokedAt sql.NullString
	)
	err := row.Scan(&t.ID, &t.Name, &t.Hash, &t.Prefix, &scopes, &createdAt, &expiresAt, &lastUsedAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, auth.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: scan token: %w", err)
	}
	if err := json.Unmarshal([]byte(scopes), &t.Scopes); err != nil {
		return nil, fmt.Errorf("sqlite: decode scopes: %w", err)
	}
	if t.CreatedAt, err = time.Parse(timeFormat, createdAt); err != nil {
		return nil, fmt.Errorf("sqlite: decode created_at: %w", err)
	}
	if t.ExpiresAt, err = parseNullTime(expiresAt); err != nil {
		return nil, fmt.Errorf("sqlite: decode expires_at: %w", err)
	}
	if t.LastUsedAt, err = parseNullTime(lastUsedAt); err != nil {
		return nil, fmt.Errorf("sqlite: decode last_used_at: %w", err)
	}
	if t.RevokedAt, err = parseNullTime(revokedAt); err != nil {
		return nil, fmt.Errorf("sqlite: decode revoked_at: %w", err)
	}
	return &t, nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(timeFormat)
}

func parseNullTime(v sql.NullString) (*time.Time, error) {
	if !v.Valid {
		return nil, nil
	}
	parsed, err := time.Parse(timeFormat, v.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
