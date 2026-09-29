-- +goose Up
-- Somebody the identity provider vouches for, who has not been let in. The row
-- is what an administrator sees under single sign-on, so letting them in is a
-- click there rather than an invitation typed out and mailed.
CREATE TABLE org_join_request (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

-- The organization's record of who did what to it, as Armature keeps it. It
-- starts with letting people in and turning them away; the audit log reads it.
CREATE TABLE audit_log (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    actor_user_id uuid REFERENCES app_user(id) ON DELETE SET NULL,
    action        text NOT NULL CHECK (btrim(action) <> ''),
    target_type   text NOT NULL,
    target_id     uuid,
    data          jsonb NOT NULL DEFAULT '{}'::jsonb,
    ip            inet,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_org_created_idx ON audit_log (org_id, created_at DESC);
CREATE INDEX audit_log_target_idx ON audit_log (org_id, target_type, target_id);

ALTER TABLE org_join_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_join_request FORCE  ROW LEVEL SECURITY;
ALTER TABLE audit_log        ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log        FORCE  ROW LEVEL SECURITY;

CREATE POLICY org_join_request_tenant_isolation ON org_join_request
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY audit_log_tenant_isolation ON audit_log
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY org_join_request_admin_bypass ON org_join_request TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY audit_log_admin_bypass ON audit_log TO stator_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON org_join_request TO stator_app, stator_admin;
-- A record that can be rewritten records nothing, so it is only ever added to.
REVOKE UPDATE, DELETE ON audit_log FROM stator_app, stator_admin;
GRANT SELECT, INSERT ON audit_log TO stator_app, stator_admin;

-- +goose Down
DROP TABLE IF EXISTS audit_log, org_join_request;
