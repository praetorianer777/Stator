-- +goose Up
-- Browser sessions and local passwords.

-- NULL for somebody who only ever signs in through a provider. Only bootstrap
-- administrators are given one.
ALTER TABLE app_user ADD COLUMN password_hash text;

-- A session spans organizations because a person switches without signing in
-- again; current_org_id is where it acts now. Only the SHA-256 of the token is
-- stored, so a copy of this table opens no session.
CREATE TABLE user_session (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id        uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    token_hash     bytea NOT NULL UNIQUE,
    current_org_id uuid REFERENCES org(id) ON DELETE SET NULL,
    -- How the session was proven, and for which organization, which decides
    -- where it may go.
    proof          text NOT NULL CHECK (proof IN ('password', 'oidc')),
    proof_org_id   uuid REFERENCES org(id) ON DELETE SET NULL,
    user_agent     text,
    ip             inet,
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_seen_at   timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL,
    CONSTRAINT user_session_token_is_sha256 CHECK (octet_length(token_hash) = 32)
);

CREATE INDEX user_session_user_idx ON user_session (user_id);
CREATE INDEX user_session_expiry_idx ON user_session (expires_at);

-- Where a session may go. A password vouches for the person everywhere. A
-- provider's sign-in vouches for them wherever that same provider is trusted,
-- and in an organization they own; otherwise an organization's own provider
-- could vouch for somebody in another organization that never trusted it.
-- +goose StatementBegin
CREATE FUNCTION session_reaches(proof text, proof_org uuid, person uuid, target uuid) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT proof = 'password'
        OR target = proof_org
        OR (proof = 'oidc' AND (
            EXISTS (SELECT 1 FROM org_member
                    WHERE org_id = target AND user_id = person AND org_role = 'owner')
            OR EXISTS (SELECT 1 FROM oidc_provider home
                       JOIN oidc_provider away ON away.issuer = home.issuer
                       WHERE home.org_id = proof_org AND away.org_id = target
                         AND home.enabled AND away.enabled)))
$$;
-- +goose StatementEnd

-- The service asks session_reaches before switching; the trigger asks again, so
-- a later code path that forgets to cannot move a session where it may not go.
-- +goose StatementBegin
CREATE FUNCTION session_stays_home() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
    -- The organization going, which nulls the column, is the one way it changes.
    IF NEW.proof_org_id IS NOT NULL AND NEW.proof_org_id IS DISTINCT FROM OLD.proof_org_id THEN
        RAISE EXCEPTION 'a session keeps the organization it was proven for' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.current_org_id IS NOT NULL AND NEW.current_org_id IS DISTINCT FROM OLD.current_org_id
       AND NOT session_reaches(OLD.proof, OLD.proof_org_id, OLD.user_id, NEW.current_org_id) THEN
        RAISE EXCEPTION 'a session opened by % does not reach that organization', OLD.proof
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER session_stays_home BEFORE UPDATE OF current_org_id, proof, proof_org_id ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_stays_home();

-- Sessions are looked up by token before any tenant is known, so only the admin
-- role reaches them; the application role sees none, in any tenant.
ALTER TABLE user_session ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_session FORCE  ROW LEVEL SECURITY;
CREATE POLICY user_session_admin_bypass ON user_session TO stator_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON user_session TO stator_app, stator_admin;

-- +goose Down
DROP TABLE IF EXISTS user_session CASCADE;
DROP FUNCTION IF EXISTS session_stays_home();
DROP FUNCTION IF EXISTS session_reaches(text, uuid, uuid, uuid);
ALTER TABLE app_user DROP COLUMN IF EXISTS password_hash;
