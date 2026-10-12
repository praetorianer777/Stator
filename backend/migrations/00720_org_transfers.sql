-- +goose Up
-- Organization export and import (#383). An administrator of the organization
-- exports all of it as one archive, each space in the space archive format
-- (00600) with everything that is not a space beside them, and a fresh
-- organization takes such an archive in whole. Both run in the worker as
-- leased jobs, as space transfers do.
--
-- The archive holds every space, draft and person of the organization, so
-- nobody but its administrators asks for one, reads one or downloads one.
CREATE TABLE org_export (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    requested_by uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    state        text NOT NULL DEFAULT 'queued',
    requested_at timestamptz NOT NULL DEFAULT now(),
    -- Set by the worker alone.
    attempts     integer NOT NULL DEFAULT 0,
    lease_until  timestamptz,
    done_steps   integer NOT NULL DEFAULT 0,
    total_steps  integer NOT NULL DEFAULT 0,
    file_name    text,
    size_bytes   bigint,
    expires_at   timestamptz,
    failure      text,
    -- The failure's particulars, such as the space too large for an archive.
    detail       text,
    started_at   timestamptz,
    finished_at  timestamptz,
    object_key   text GENERATED ALWAYS AS ('org/' || org_id::text || '/org-export/' || id::text) STORED,
    CONSTRAINT org_export_state_known CHECK (state IN ('queued', 'running', 'done', 'failed', 'expired')),
    CONSTRAINT org_export_failure_known CHECK (failure IN ('forbidden', 'too_large', 'failed')),
    CONSTRAINT org_export_failed_whole CHECK ((state = 'failed') = (failure IS NOT NULL)),
    CONSTRAINT org_export_running_leased CHECK ((state = 'running') = (lease_until IS NOT NULL)),
    CONSTRAINT org_export_done_whole CHECK (state <> 'done' OR (size_bytes IS NOT NULL AND expires_at IS NOT NULL AND file_name IS NOT NULL)),
    CONSTRAINT org_export_steps CHECK (done_steps >= 0 AND total_steps >= 0)
);
CREATE INDEX org_export_waiting_idx ON org_export (requested_at) WHERE state IN ('queued', 'running');
CREATE INDEX org_export_expiring_idx ON org_export (expires_at) WHERE state = 'done';
CREATE INDEX org_export_latest_idx ON org_export (org_id, requested_at DESC);

-- An import fills the organization it is asked in, which has no space yet,
-- from an archive kept in storage until the worker is done with it. It runs
-- in one transaction that also marks it done, so a worker that dies leaves
-- either nothing or a finished import behind.
CREATE TABLE org_import (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    requested_by uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    size_bytes   bigint NOT NULL,
    state        text NOT NULL DEFAULT 'queued',
    requested_at timestamptz NOT NULL DEFAULT now(),
    attempts     integer NOT NULL DEFAULT 0,
    lease_until  timestamptz,
    done_steps   integer NOT NULL DEFAULT 0,
    total_steps  integer NOT NULL DEFAULT 0,
    report       jsonb,
    failure      text,
    detail       text,
    written_lsn  pg_lsn,
    started_at   timestamptz,
    finished_at  timestamptz,
    object_key   text GENERATED ALWAYS AS ('org/' || org_id::text || '/org-import/' || id::text) STORED,
    CONSTRAINT org_import_size_positive CHECK (size_bytes > 0),
    CONSTRAINT org_import_state_known CHECK (state IN ('queued', 'running', 'done', 'failed')),
    CONSTRAINT org_import_failure_known CHECK (failure IN ('forbidden', 'not_empty', 'invalid', 'too_large', 'failed')),
    CONSTRAINT org_import_failed_whole CHECK ((state = 'failed') = (failure IS NOT NULL)),
    CONSTRAINT org_import_running_leased CHECK ((state = 'running') = (lease_until IS NOT NULL)),
    CONSTRAINT org_import_done_reported CHECK (state <> 'done' OR report IS NOT NULL),
    CONSTRAINT org_import_steps CHECK (done_steps >= 0 AND total_steps >= 0)
);
CREATE INDEX org_import_waiting_idx ON org_import (requested_at) WHERE state IN ('queued', 'running');
CREATE INDEX org_import_latest_idx ON org_import (org_id, requested_at DESC);

ALTER TABLE org_export ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_export FORCE  ROW LEVEL SECURITY;
ALTER TABLE org_import ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_import FORCE  ROW LEVEL SECURITY;
CREATE POLICY org_export_tenant_isolation ON org_export
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY org_import_tenant_isolation ON org_import
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY org_export_admin_bypass ON org_export TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY org_import_admin_bypass ON org_import TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY org_export_not_anonymous ON org_export AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY org_import_not_anonymous ON org_import AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

-- Read by the organization's administrators alone, the requester included
-- only while they are one; queued by an administrator for themselves, as a
-- job still to run with nothing but what they ask for.
CREATE POLICY org_export_readers ON org_export AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_is_admin(current_actor_id()) AND perm_token_whole(current_actor_id()));
CREATE POLICY org_export_requesters ON org_export AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (requested_by = current_actor_id() AND state = 'queued'
                AND perm_is_admin(current_actor_id()) AND perm_token_whole(current_actor_id()));
CREATE POLICY org_import_readers ON org_import AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_is_admin(current_actor_id()) AND perm_token_whole(current_actor_id()));
CREATE POLICY org_import_requesters ON org_import AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (requested_by = current_actor_id() AND state = 'queued'
                AND perm_is_admin(current_actor_id()) AND perm_token_whole(current_actor_id()));

REVOKE ALL ON org_export, org_import FROM stator_app;
GRANT SELECT ON org_export, org_import TO stator_app;
GRANT INSERT (id, org_id, requested_by) ON org_export TO stator_app;
GRANT INSERT (id, org_id, requested_by, size_bytes) ON org_import TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON org_export, org_import TO stator_admin;

-- +goose StatementBegin
-- Whether the organization has a space, one the caller may see or not, so an
-- import into one that is not empty is refused while its uploader waits.
CREATE FUNCTION org_has_spaces() RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM space WHERE org_id = current_org_id())
$$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION org_has_spaces() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION org_has_spaces() TO stator_app, stator_admin;

-- +goose Down
DROP FUNCTION IF EXISTS org_has_spaces();
DROP TABLE IF EXISTS org_import;
DROP TABLE IF EXISTS org_export;
