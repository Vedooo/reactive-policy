-- reactive-policy audit sink schema.
-- Idempotent: CREATE … IF NOT EXISTS so reapplying on operator restart is safe.

CREATE TABLE IF NOT EXISTS action_executions (
    id BIGSERIAL PRIMARY KEY,
    audit_uid TEXT NOT NULL,
    audit_name TEXT NOT NULL,
    audit_namespace TEXT NOT NULL,
    policy_ref TEXT NOT NULL,
    policy_uid TEXT,
    triggered_at TIMESTAMPTZ NOT NULL,
    metric_value TEXT,
    action_index INT NOT NULL,
    action_id TEXT,
    plugin TEXT NOT NULL,
    target_api_version TEXT,
    target_kind TEXT,
    target_namespace TEXT,
    target_name TEXT,
    status TEXT NOT NULL,
    message TEXT,
    reversible BOOLEAN NOT NULL,
    details JSONB,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (audit_uid, action_index)
);

CREATE INDEX IF NOT EXISTS idx_action_executions_policy_triggered_at
    ON action_executions (policy_ref, triggered_at DESC);

CREATE INDEX IF NOT EXISTS idx_action_executions_audit_uid
    ON action_executions (audit_uid);

CREATE TABLE IF NOT EXISTS revert_outcomes (
    id BIGSERIAL PRIMARY KEY,
    audit_uid TEXT NOT NULL,
    audit_name TEXT NOT NULL,
    audit_namespace TEXT NOT NULL,
    policy_ref TEXT NOT NULL,
    action_index INT NOT NULL,
    plugin TEXT NOT NULL,
    reverted_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    message TEXT,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (audit_uid, action_index)
);

CREATE INDEX IF NOT EXISTS idx_revert_outcomes_audit_uid
    ON revert_outcomes (audit_uid);
