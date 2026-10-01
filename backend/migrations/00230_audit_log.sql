-- +goose Up
-- The audit log gains its reader (#107): administrators page through it newest
-- first, by (time, id), narrowed by action, actor or target, so each filter
-- gets an index that ends in the keyset.
DROP INDEX audit_log_org_created_idx;
CREATE INDEX audit_log_org_created_idx ON audit_log (org_id, created_at DESC, id DESC);
CREATE INDEX audit_log_org_action_idx ON audit_log (org_id, action, created_at DESC, id DESC);
CREATE INDEX audit_log_org_actor_idx ON audit_log (org_id, actor_user_id, created_at DESC, id DESC);
DROP INDEX audit_log_target_idx;
CREATE INDEX audit_log_target_idx ON audit_log (org_id, target_id, created_at DESC, id DESC);

-- Only the organization's administrators read its record. The subquery runs
-- the check once per statement instead of once per row the scan meets.
CREATE POLICY audit_log_admins_read ON audit_log AS RESTRICTIVE FOR SELECT TO stator_app
    USING ((SELECT perm_is_admin(current_actor_id())));

-- An entry names the person the transaction acts for, or nobody when the act
-- is the system's, so the app role cannot write an act down to someone else.
CREATE POLICY audit_log_actor_is_caller ON audit_log AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (actor_user_id IS NULL OR actor_user_id = current_actor_id());

-- +goose StatementBegin
-- Retention: deletes the current organization's entries older than cutoff and
-- says how many went. Neither runtime role may delete from the log, so this is
-- the only way an entry goes before its organization does, and it never takes
-- one younger than a day, whatever the caller was configured with.
CREATE FUNCTION audit_log_prune(cutoff timestamptz) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gone bigint;
BEGIN
    IF current_org_id() IS NULL THEN
        RAISE EXCEPTION 'audit_log_prune runs inside one organization' USING ERRCODE = '42501';
    END IF;
    IF cutoff > now() - interval '1 day' THEN
        RAISE EXCEPTION 'the audit log keeps every entry for at least a day' USING ERRCODE = '22023';
    END IF;
    DELETE FROM audit_log WHERE org_id = current_org_id() AND created_at < cutoff;
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$$;
-- +goose StatementEnd

-- The schema's default privileges hand every new function to both roles.
REVOKE EXECUTE ON FUNCTION audit_log_prune(timestamptz) FROM PUBLIC, stator_app;
GRANT EXECUTE ON FUNCTION audit_log_prune(timestamptz) TO stator_admin;

-- +goose Down
DROP FUNCTION IF EXISTS audit_log_prune(timestamptz);
DROP POLICY IF EXISTS audit_log_actor_is_caller ON audit_log;
DROP POLICY IF EXISTS audit_log_admins_read ON audit_log;
DROP INDEX IF EXISTS audit_log_target_idx;
CREATE INDEX audit_log_target_idx ON audit_log (org_id, target_type, target_id);
DROP INDEX IF EXISTS audit_log_org_actor_idx;
DROP INDEX IF EXISTS audit_log_org_action_idx;
DROP INDEX IF EXISTS audit_log_org_created_idx;
CREATE INDEX audit_log_org_created_idx ON audit_log (org_id, created_at DESC);
