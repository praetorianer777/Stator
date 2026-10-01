-- +goose Up
-- A member's Armature personal access token (#27), sealed with
-- STATOR_SECRET_KEY and bound to the organization and the person. Every call
-- to Armature is made with the viewer's own, so a page shows an issue only to
-- somebody Armature shows it to.
CREATE TABLE armature_token (
    org_id        uuid NOT NULL,
    user_id       uuid NOT NULL,
    -- New with every token stored, so a cache entry keyed by it is never
    -- read for another token or another Armature identity behind it.
    id            uuid NOT NULL DEFAULT uuidv7(),
    token         bytea NOT NULL,
    -- Whom the token acts as in Armature, as the last check found.
    armature_user_id    uuid,
    armature_user_name  text,
    armature_user_email text,
    status        text NOT NULL CHECK (status IN ('ok', 'rejected', 'unreachable')),
    checked_at    timestamptz NOT NULL DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id),
    CONSTRAINT armature_token_id_key UNIQUE (id),
    -- The row goes with the membership, and with the connection it was
    -- checked against.
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id) REFERENCES armature_connection (org_id) ON DELETE CASCADE
);

ALTER TABLE armature_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_token FORCE  ROW LEVEL SECURITY;

CREATE POLICY armature_token_tenant_isolation ON armature_token
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY armature_token_admin_bypass ON armature_token TO stator_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON armature_token TO stator_admin;

-- +goose Down
DROP TABLE IF EXISTS armature_token;
