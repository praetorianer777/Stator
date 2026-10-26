-- +goose Up
-- A narrower token for one run (#499), Armature's oauth_exchanged_token of
-- migration 00945_partner_tokens: an app trades an access token of its own
-- grant for a shorter one that may only read, or reach fewer spaces, while the
-- grant keeps what its person allowed (RFC 8693). Authentication reads it as
-- its grant narrowed, so it never reaches beyond it, and it ends with it.
CREATE TABLE oauth_exchanged_token (
    token_hash bytea PRIMARY KEY CONSTRAINT oauth_exchanged_token_is_sha256 CHECK (octet_length(token_hash) = 32),
    grant_id   uuid NOT NULL REFERENCES api_token (id) ON DELETE CASCADE,
    -- read for one that may only read, none for one that may do what its
    -- grant may, as a grant's own api_token row holds them.
    scopes     text[] NOT NULL CONSTRAINT oauth_exchanged_token_known_scopes CHECK (scopes <@ ARRAY['read']::text[]),
    -- NULL keeps the grant's spaces; otherwise the ones of them it reaches.
    spaces     uuid[] CONSTRAINT oauth_exchanged_token_spaces_named CHECK (cardinality(spaces) BETWEEN 1 AND 50),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_exchanged_token_grant_idx ON oauth_exchanged_token (grant_id);
CREATE INDEX oauth_exchanged_token_expiry_idx ON oauth_exchanged_token (expires_at);

-- An exchanged token comes from a live grant of a connected app and ends no
-- later than the grant's access token it was exchanged for, whoever writes it.
-- +goose StatementBegin
CREATE FUNCTION oauth_exchanged_token_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM api_token
                   WHERE id = NEW.grant_id AND client_id IS NOT NULL
                     AND access_expires_at > now() AND NEW.expires_at <= access_expires_at) THEN
        RAISE EXCEPTION 'an exchanged token ends no later than the live grant it narrows'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'oauth_exchanged_token_within_grant';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER oauth_exchanged_token_within_grant BEFORE INSERT OR UPDATE ON oauth_exchanged_token
    FOR EACH ROW EXECUTE FUNCTION oauth_exchanged_token_guard();

-- Only the token endpoint and authentication touch it, as the admin role, as
-- they do the codes; a member reads none, nor makes one.
ALTER TABLE oauth_exchanged_token ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_exchanged_token FORCE  ROW LEVEL SECURITY;
CREATE POLICY oauth_exchanged_token_admin_bypass ON oauth_exchanged_token TO stator_admin USING (true) WITH CHECK (true);
-- An anonymous reader names nobody, so none of them is theirs.
CREATE POLICY oauth_exchanged_token_not_anonymous ON oauth_exchanged_token AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
REVOKE ALL ON oauth_exchanged_token FROM stator_app;
GRANT SELECT, INSERT, DELETE ON oauth_exchanged_token TO stator_admin;

-- +goose Down
DROP TABLE IF EXISTS oauth_exchanged_token;
DROP FUNCTION IF EXISTS oauth_exchanged_token_guard();
