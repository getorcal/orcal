package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/getorcal/orcal/internal/audit"
)

type eventRepo struct {
	db *sql.DB
}

const eventColumns = `id, ts, actor_token_id, actor_name, action, resource_type, resource_id, request_id, status, remote_addr, details`

func (r *eventRepo) Create(ctx context.Context, e *audit.Event) error {
	details := "{}"
	if len(e.Details) > 0 {
		encoded, err := json.Marshal(e.Details)
		if err != nil {
			return fmt.Errorf("sqlite: encode event details: %w", err)
		}
		details = string(encoded)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO events (`+eventColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Timestamp.Format(timeFormat), e.ActorTokenID, e.ActorName, string(e.Action),
		e.ResourceType, e.ResourceID, e.RequestID, e.Status, e.RemoteAddr, details)
	if err != nil {
		return fmt.Errorf("sqlite: insert event: %w", err)
	}
	return nil
}

func (r *eventRepo) List(ctx context.Context, f audit.Filter) ([]*audit.Event, error) {
	query := `SELECT ` + eventColumns + ` FROM events WHERE 1 = 1`
	args := []any{}
	if f.Actor != "" {
		query += ` AND actor_token_id = ?`
		args = append(args, f.Actor)
	}
	if f.Action != "" {
		query += ` AND action = ?`
		args = append(args, string(f.Action))
	}
	if f.ResourceType != "" {
		query += ` AND resource_type = ?`
		args = append(args, f.ResourceType)
	}
	if f.ResourceID != "" {
		query += ` AND resource_id = ?`
		args = append(args, f.ResourceID)
	}
	if !f.Since.IsZero() {
		query += ` AND ts >= ?`
		args = append(args, f.Since.Format(timeFormat))
	}
	if !f.Until.IsZero() {
		query += ` AND ts < ?`
		args = append(args, f.Until.Format(timeFormat))
	}
	if f.Cursor != "" {
		query += ` AND id < ?`
		args = append(args, f.Cursor)
	}
	query += ` ORDER BY id DESC`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list events: %w", err)
	}
	defer rows.Close()

	var out []*audit.Event
	for rows.Next() {
		var (
			e       audit.Event
			ts      string
			action  string
			details string
		)
		if err := rows.Scan(&e.ID, &ts, &e.ActorTokenID, &e.ActorName, &action,
			&e.ResourceType, &e.ResourceID, &e.RequestID, &e.Status, &e.RemoteAddr, &details); err != nil {
			return nil, fmt.Errorf("sqlite: scan event: %w", err)
		}
		if e.Timestamp, err = time.Parse(timeFormat, ts); err != nil {
			return nil, fmt.Errorf("sqlite: decode ts: %w", err)
		}
		if err := json.Unmarshal([]byte(details), &e.Details); err != nil {
			return nil, fmt.Errorf("sqlite: decode details: %w", err)
		}
		e.Action = audit.Action(action)
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list events: %w", err)
	}
	return out, nil
}

func (r *eventRepo) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM events WHERE ts < ?`, cutoff.Format(timeFormat))
	if err != nil {
		return 0, fmt.Errorf("sqlite: prune events by age: %w", err)
	}
	return res.RowsAffected()
}

func (r *eventRepo) DeleteBeyondCount(ctx context.Context, keep int) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM events WHERE id NOT IN (SELECT id FROM events ORDER BY id DESC LIMIT `+strconv.Itoa(keep)+`)`)
	if err != nil {
		return 0, fmt.Errorf("sqlite: prune events by count: %w", err)
	}
	return res.RowsAffected()
}
