-- +goose Up
-- Personal access tokens, as Armature keeps them. A token belongs to one person
-- in one organization and acts as them there. Only the SHA-256 of the secret is
-- stored, so a copy of this table opens nothing.
CREATE TABLE api_token (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name         text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    -- Empty for a token that may do whatever its owner may; read for one that
    -- may only read. Nothing else is enforced, so nothing else is stored.
    scopes       text[] NOT NULL DEFAULT '{}',
    last_used_at timestamptz,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT api_token_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT api_token_is_sha256 CHECK (octet_length(token_hash) = 32),
    CONSTRAINT api_token_known_scopes CHECK (scopes <@ ARRAY['read']::text[]),
    -- A foreign key is checked past row level security, so a row that points
    -- at a token names its organization too and cannot cross tenants.
    CONSTRAINT api_token_org_id_key UNIQUE (org_id, id),
    -- Leaving the organization takes the person's tokens there with it.
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE
);

CREATE INDEX api_token_org_user_idx ON api_token (org_id, user_id);

ALTER TABLE api_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_token FORCE  ROW LEVEL SECURITY;

CREATE POLICY api_token_tenant_isolation ON api_token
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
-- A token is looked up by its hash before any tenant is known.
CREATE POLICY api_token_admin_bypass ON api_token TO stator_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON api_token TO stator_app, stator_admin;

-- +goose Down
DROP TABLE IF EXISTS api_token;
