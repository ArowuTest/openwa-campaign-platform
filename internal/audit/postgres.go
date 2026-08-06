package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	pgretry "campaign-platform/internal/persistence/postgres"
)

type PostgreSQLRepository struct{ DB *sql.DB }

func (r *PostgreSQLRepository) Head(ctx context.Context) (uint64, string, error) {
	if r.DB == nil {
		return 0, "", errors.New("database is required")
	}
	var seq uint64
	var hash string
	err := r.DB.QueryRowContext(ctx, `SELECT sequence,event_hash FROM audit_chain_head WHERE singleton=true`).Scan(&seq, &hash)
	return seq, hash, err
}
func (r *PostgreSQLRepository) Append(ctx context.Context, event Event, expectedSequence uint64, expectedHash string) (Event, error) {
	if r.DB == nil {
		return Event{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (Event, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return Event{}, err
		}
		defer tx.Rollback()

		existing, found, err := getAuditEventByID(ctx, tx, event.ID)
		if err != nil {
			return Event{}, err
		}
		if found {
			if !sameEventIntent(existing, event) {
				return Event{}, ErrIdempotencyConflict
			}
			if err := tx.Commit(); err != nil {
				return Event{}, err
			}
			return existing, nil
		}

		var current uint64
		var head string
		if err := tx.QueryRowContext(ctx, `SELECT sequence,event_hash FROM audit_chain_head WHERE singleton=true FOR UPDATE`).Scan(&current, &head); err != nil {
			return Event{}, err
		}
		if current != expectedSequence || head != expectedHash {
			return Event{}, ErrChainConflict
		}
		event.Sequence = current + 1
		event.PreviousHash = head
		event.Hash, err = calculateHash(event)
		if err != nil {
			return Event{}, err
		}
		var before, after any
		if len(event.Before) > 0 {
			before = []byte(event.Before)
		}
		if len(event.After) > 0 {
			after = []byte(event.After)
		}
		const insert = `INSERT INTO audit_events(id,actor_id,actor_type,action,entity_type,entity_id,organisation_id,outcome,sensitivity,request_id,source_ip,user_agent,before_state,after_state,created_at,sequence,previous_hash,event_hash,reason_code,reason,correlation_id) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6,NULLIF($7,'')::uuid,NULLIF($8,''),NULLIF($9,''),$10,NULLIF($11,'')::inet,NULLIF($12,''),$13,$14,$15,$16,$17,$18,NULLIF($19,''),NULLIF($20,''),$21)`
		_, err = tx.ExecContext(ctx, insert, event.ID, event.ActorID, event.ActorType, event.Action, event.ObjectType, event.ObjectID, event.OrganisationID, event.Outcome, event.Sensitivity, event.CorrelationID, event.IPAddress, event.Device, before, after, event.OccurredAt, event.Sequence, event.PreviousHash, event.Hash, event.ReasonCode, event.Reason, event.CorrelationID)
		if err != nil {
			return Event{}, fmt.Errorf("insert audit event: %w", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE audit_chain_head SET sequence=$1,event_hash=$2,updated_at=$3 WHERE singleton=true AND sequence=$4 AND event_hash=$5`, event.Sequence, event.Hash, event.OccurredAt, current, head)
		if err != nil {
			return Event{}, err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return Event{}, err
		}
		if updated != 1 {
			return Event{}, ErrChainConflict
		}
		if err = tx.Commit(); err != nil {
			return Event{}, err
		}
		return event, nil
	})
}

const auditEventSelect = `SELECT id,sequence,actor_type,coalesce(actor_id::text,''),action,entity_type,coalesce(entity_id,''),coalesce(organisation_id::text,''),coalesce(outcome,''),coalesce(sensitivity,''),coalesce(before_state,'null'::jsonb)::text,coalesce(after_state,'null'::jsonb)::text,coalesce(reason_code,''),coalesce(reason,''),coalesce(source_ip::text,''),coalesce(user_agent,''),coalesce(correlation_id,request_id,''),created_at,coalesce(previous_hash,''),coalesce(event_hash,'') FROM audit_events`

type auditScanner interface{ Scan(...any) error }

func scanAuditEvent(row auditScanner) (Event, error) {
	var event Event
	var before, afterJSON string
	if err := row.Scan(&event.ID, &event.Sequence, &event.ActorType, &event.ActorID, &event.Action, &event.ObjectType, &event.ObjectID, &event.OrganisationID, &event.Outcome, &event.Sensitivity, &before, &afterJSON, &event.ReasonCode, &event.Reason, &event.IPAddress, &event.Device, &event.CorrelationID, &event.OccurredAt, &event.PreviousHash, &event.Hash); err != nil {
		return Event{}, err
	}
	if before != "null" {
		event.Before = json.RawMessage(before)
	}
	if afterJSON != "null" {
		event.After = json.RawMessage(afterJSON)
	}
	return event, nil
}

func getAuditEventByID(ctx context.Context, tx *sql.Tx, identifier string) (Event, bool, error) {
	event, err := scanAuditEvent(tx.QueryRowContext(ctx, auditEventSelect+` WHERE id=$1::uuid`, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, false, nil
	}
	return event, err == nil, err
}

func (r *PostgreSQLRepository) List(ctx context.Context, after uint64, limit int) ([]Event, error) {
	page, err := r.Search(ctx, Query{AfterSequence: after, Limit: limit})
	return page.Items, err
}

func (r *PostgreSQLRepository) Search(ctx context.Context, query Query) (Page, error) {
	if r.DB == nil {
		return Page{}, errors.New("database is required")
	}
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	clauses := []string{"sequence>$1"}
	args := []any{query.AfterSequence}
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if value := strings.TrimSpace(query.ActorID); value != "" {
		add("actor_id=NULLIF($%d,'')::uuid", value)
	}
	if value := strings.TrimSpace(query.Action); value != "" {
		add("action=$%d", value)
	}
	if value := strings.TrimSpace(query.ObjectType); value != "" {
		add("entity_type=$%d", value)
	}
	if value := strings.TrimSpace(query.ObjectID); value != "" {
		add("entity_id=$%d", value)
	}
	if value := strings.TrimSpace(query.OrganisationID); value != "" {
		add("organisation_id=NULLIF($%d,'')::uuid", value)
	}
	if value := strings.TrimSpace(query.Outcome); value != "" {
		add("upper(coalesce(outcome,''))=upper($%d)", value)
	}
	if value := strings.TrimSpace(query.Sensitivity); value != "" {
		add("upper(coalesce(sensitivity,''))=upper($%d)", value)
	}
	if value := strings.TrimSpace(query.CorrelationID); value != "" {
		add("correlation_id=$%d", value)
	}
	if value := strings.TrimSpace(query.IPAddress); value != "" {
		add("source_ip=NULLIF($%d,'')::inet", value)
	}
	if query.From != nil {
		add("created_at >= $%d", query.From.UTC())
	}
	if query.To != nil {
		add("created_at < $%d", query.To.UTC())
	}
	args = append(args, limit+1)
	statement := auditEventSelect + " WHERE " + strings.Join(clauses, " AND ") + fmt.Sprintf(" ORDER BY sequence LIMIT $%d", len(args))
	rows, err := r.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	items := make([]Event, 0, limit+1)
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return Page{}, err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].Sequence
	}
	return Page{Items: items, NextSequence: next}, nil
}

func (r *PostgreSQLRepository) Verify(ctx context.Context) error {
	var after uint64
	var previous string
	for {
		items, err := r.List(ctx, after, 1000)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		for _, e := range items {
			if e.Sequence != after+1 || e.PreviousHash != previous {
				return fmt.Errorf("%w at sequence %d", ErrIntegrity, e.Sequence)
			}
			expected, err := calculateHash(e)
			if err != nil || expected != e.Hash {
				return fmt.Errorf("%w at sequence %d", ErrIntegrity, e.Sequence)
			}
			after = e.Sequence
			previous = e.Hash
		}
	}
}
