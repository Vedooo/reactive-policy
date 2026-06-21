/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package postgres

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vedooo/reactive-policy/internal/audit/sink"
)

//go:embed schema.sql
var schemaSQL string

const insertExecutionSQL = `
INSERT INTO action_executions
  (audit_uid, audit_name, audit_namespace, policy_ref, policy_uid, triggered_at,
   metric_value, action_index, action_id, plugin, target_api_version, target_kind,
   target_namespace, target_name, status, message, reversible, details)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
ON CONFLICT (audit_uid, action_index) DO NOTHING
`

const insertRevertSQL = `
INSERT INTO revert_outcomes
  (audit_uid, audit_name, audit_namespace, policy_ref, action_index, plugin,
   reverted_at, status, message)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (audit_uid, action_index) DO NOTHING
`

// New connects to Postgres at dsn, applies the schema, and starts an async
// audit sink. Caller must Close it to drain.
func New(ctx context.Context, dsn string, cfg Config) (*Sink, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres sink: connecting: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres sink: ping: %w", err)
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres sink: applying schema: %w", err)
	}
	return newSink(&pgxWriter{pool: pool}, cfg), nil
}

type pgxWriter struct {
	pool *pgxpool.Pool
}

func (w *pgxWriter) writeTriggers(ctx context.Context, events []sink.Event) error {
	batch := &pgx.Batch{}
	for i := range events {
		e := events[i]
		var details any
		if len(e.DetailsJSON) > 0 {
			details = e.DetailsJSON
		}
		batch.Queue(insertExecutionSQL,
			e.AuditUID, e.AuditName, e.AuditNamespace, e.PolicyRef, e.PolicyUID,
			e.TriggeredAt, e.MetricValue, e.ActionIndex, e.ActionID, e.Plugin,
			e.TargetAPIVersion, e.TargetKind, e.TargetNamespace, e.TargetName,
			e.Status, e.Message, e.Reversible, details,
		)
	}
	br := w.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range events {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("postgres sink: insert executions: %w", err)
		}
	}
	return nil
}

func (w *pgxWriter) writeReverts(ctx context.Context, events []sink.RevertEvent) error {
	batch := &pgx.Batch{}
	for i := range events {
		e := events[i]
		batch.Queue(insertRevertSQL,
			e.AuditUID, e.AuditName, e.AuditNamespace, e.PolicyRef,
			e.ActionIndex, e.Plugin, e.RevertedAt, e.Status, e.Message,
		)
	}
	br := w.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range events {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("postgres sink: insert reverts: %w", err)
		}
	}
	return nil
}

func (w *pgxWriter) close(_ context.Context) error {
	w.pool.Close()
	return nil
}
