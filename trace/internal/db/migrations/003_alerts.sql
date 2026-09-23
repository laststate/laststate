-- alerts, integrations, webhooks
CREATE TABLE IF NOT EXISTS alert_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL, -- new_fatal_issue | new_issue | issue_resolved
    enabled BOOLEAN NOT NULL DEFAULT true,
    channel TEXT NOT NULL DEFAULT 'webhook', -- webhook | email (email noop in local)
    target_url TEXT NOT NULL DEFAULT '',
    secret TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    rule_id UUID REFERENCES alert_rules(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    target_url TEXT NOT NULL,
    status_code INT NOT NULL DEFAULT 0,
    success BOOLEAN NOT NULL DEFAULT false,
    response_body TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS webhook_deliveries_created_idx ON webhook_deliveries(created_at DESC);
