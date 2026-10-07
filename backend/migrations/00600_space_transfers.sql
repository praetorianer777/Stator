-- +goose Up
-- Space export and import (#88). Both run in the worker, since a space of
-- some hundred pages with their history and files takes longer than a
-- request may last. A request queues a row, the page follows its state, and
-- the worker claims it with a lease, as it does the example space (00580).
--
-- An export is asked for by an administrator of the space, and its file is
-- kept in storage for its requester until it expires.
CREATE TABLE space_export (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    -- Null once the space is deleted; the key stays to name it.
    space_id     uuid,
    space_key    text NOT NULL,
    requested_by uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    format       text NOT NULL,
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
    started_at   timestamptz,
    finished_at  timestamptz,
    -- Where the file is kept, under the organization's prefix, so deleting
    -- the organization takes it along and a tombstone may name it.
    object_key   text GENERATED ALWAYS AS ('org/' || org_id::text || '/space-export/' || id::text) STORED,
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE SET NULL (space_id),
    CONSTRAINT space_export_format_known CHECK (format IN ('archive', 'html')),
    CONSTRAINT space_export_state_known CHECK (state IN ('queued', 'running', 'done', 'failed', 'expired')),
    CONSTRAINT space_export_failure_known CHECK (failure IN ('forbidden', 'too_large', 'failed')),
    CONSTRAINT space_export_failed_whole CHECK ((state = 'failed') = (failure IS NOT NULL)),
    CONSTRAINT space_export_running_leased CHECK ((state = 'running') = (lease_until IS NOT NULL)),
    CONSTRAINT space_export_done_whole CHECK (state <> 'done' OR (size_bytes IS NOT NULL AND expires_at IS NOT NULL AND file_name IS NOT NULL)),
    CONSTRAINT space_export_steps CHECK (done_steps >= 0 AND total_steps >= 0)
);
CREATE INDEX space_export_waiting_idx ON space_export (requested_at) WHERE state IN ('queued', 'running');
CREATE INDEX space_export_expiring_idx ON space_export (expires_at) WHERE state = 'done';
CREATE INDEX space_export_space_idx ON space_export (org_id, space_id, requested_at DESC);

-- An import makes a new space of an archive its requester uploaded, kept in
-- storage until the worker is done with it.
CREATE TABLE space_import (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    requested_by uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    -- The key and name asked for; null takes the archive's own.
    key          text,
    name         text,
    size_bytes   bigint NOT NULL,
    state        text NOT NULL DEFAULT 'queued',
    requested_at timestamptz NOT NULL DEFAULT now(),
    -- Set by the worker alone. While it runs, space_id is the space it made
    -- so far, which a worker taking over after a crash deletes first.
    attempts     integer NOT NULL DEFAULT 0,
    lease_until  timestamptz,
    done_steps   integer NOT NULL DEFAULT 0,
    total_steps  integer NOT NULL DEFAULT 0,
    space_id     uuid,
    space_key    text,
    -- What could not come across as it was: people and groups not found here,
    -- what was dropped with them, and whose work is now the importer's.
    report       jsonb,
    failure      text,
    -- The failure's particulars, such as the page whose document was refused.
    detail       text,
    written_lsn  pg_lsn,
    started_at   timestamptz,
    finished_at  timestamptz,
    object_key   text GENERATED ALWAYS AS ('org/' || org_id::text || '/space-import/' || id::text) STORED,
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE SET NULL (space_id),
    CONSTRAINT space_import_key_shape CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$'),
    CONSTRAINT space_import_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT space_import_size_positive CHECK (size_bytes > 0),
    CONSTRAINT space_import_state_known CHECK (state IN ('queued', 'running', 'done', 'failed')),
    CONSTRAINT space_import_failure_known CHECK (failure IN ('forbidden', 'key_taken', 'invalid', 'too_large', 'failed')),
    CONSTRAINT space_import_failed_whole CHECK ((state = 'failed') = (failure IS NOT NULL)),
    CONSTRAINT space_import_running_leased CHECK ((state = 'running') = (lease_until IS NOT NULL)),
    CONSTRAINT space_import_steps CHECK (done_steps >= 0 AND total_steps >= 0)
);
CREATE INDEX space_import_waiting_idx ON space_import (requested_at) WHERE state IN ('queued', 'running');
CREATE INDEX space_import_latest_idx ON space_import (org_id, requested_by, requested_at DESC);

ALTER TABLE space_export ENABLE ROW LEVEL SECURITY;
ALTER TABLE space_export FORCE  ROW LEVEL SECURITY;
ALTER TABLE space_import ENABLE ROW LEVEL SECURITY;
ALTER TABLE space_import FORCE  ROW LEVEL SECURITY;
CREATE POLICY space_export_tenant_isolation ON space_export
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY space_import_tenant_isolation ON space_import
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY space_export_admin_bypass ON space_export TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY space_import_admin_bypass ON space_import TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY space_export_not_anonymous ON space_export AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY space_import_not_anonymous ON space_import AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

-- An export holds everything in the space, so it is the requester's, and
-- the space's administrators' while the space is there, to read; it is
-- queued only by an administrator of the space, for themselves.
CREATE POLICY space_export_readers ON space_export AS RESTRICTIVE FOR SELECT TO stator_app
    USING (requested_by = current_actor_id()
           OR (space_id IS NOT NULL AND perm_space_holds(current_actor_id(), space_id, 'administer'))
           OR (perm_is_admin(current_actor_id()) AND perm_token_whole(current_actor_id())));
CREATE POLICY space_export_requesters ON space_export AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (requested_by = current_actor_id() AND state = 'queued'
                AND perm_space_holds(current_actor_id(), space_id, 'administer'));
-- An import makes a space, so only whoever may create spaces queues one, for
-- themselves; its report names people, so it is theirs and the
-- organization's administrators' to read.
CREATE POLICY space_import_readers ON space_import AS RESTRICTIVE FOR SELECT TO stator_app
    USING (requested_by = current_actor_id()
           OR (perm_is_admin(current_actor_id()) AND perm_token_whole(current_actor_id())));
CREATE POLICY space_import_requesters ON space_import AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (requested_by = current_actor_id() AND state = 'queued'
                AND perm_global_holds(current_actor_id(), 'createSpace'));

REVOKE ALL ON space_export, space_import FROM stator_app;
GRANT SELECT ON space_export, space_import TO stator_app;
GRANT INSERT (id, org_id, space_id, space_key, requested_by, format) ON space_export TO stator_app;
GRANT INSERT (id, org_id, requested_by, key, name, size_bytes) ON space_import TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON space_export, space_import TO stator_admin;

-- +goose StatementBegin
-- Whether a key is taken in the organization, by a space the caller may
-- see or not, so an import is refused while its uploader waits rather than
-- later in the worker. Making a space with that key would tell as much.
CREATE FUNCTION space_key_taken(wanted text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM space WHERE org_id = current_org_id() AND key = upper(btrim(wanted)))
$$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION space_key_taken(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION space_key_taken(text) TO stator_app, stator_admin;

-- Whom an imported version, comment or file was by in the archive, when
-- nobody of that address is in this organization and it is attributed to
-- whoever imported it instead.
ALTER TABLE page_version ADD COLUMN original_author text;
ALTER TABLE comment ADD COLUMN original_author text;
ALTER TABLE attachment ADD COLUMN original_author text;

-- +goose StatementBegin
-- As in 00260, and a role other than the app's that sets updated_at itself
-- keeps what it set: an import writes a page's history in one go and then
-- gives the page the time of its last change in the archive.
CREATE OR REPLACE FUNCTION page_touch() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_user <> 'stator_app' AND NEW.updated_at IS DISTINCT FROM OLD.updated_at THEN
        RETURN NEW;
    END IF;
    IF (NEW.archived_at, NEW.archive_id) IS DISTINCT FROM (OLD.archived_at, OLD.archive_id)
       AND (NEW.title, NEW.body, NEW.version, NEW.parent_id, NEW.space_id, NEW.rank, NEW.trashed_at)
           IS NOT DISTINCT FROM (OLD.title, OLD.body, OLD.version, OLD.parent_id, OLD.space_id, OLD.rank, OLD.trashed_at) THEN
        NEW.updated_at := OLD.updated_at;
    ELSIF NEW.body IS DISTINCT FROM OLD.body
       AND (NEW.title, NEW.version) IS NOT DISTINCT FROM (OLD.title, OLD.version)
       AND document_without_anchors(NEW.body, NULL) = document_without_anchors(OLD.body, NULL) THEN
        NEW.updated_at := OLD.updated_at;
    ELSE
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END;
$$;

-- As in 00340, and likewise for the time a page was published.
CREATE OR REPLACE FUNCTION page_published_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.kind = 'folder' THEN
        NEW.published_at := NULL;
    ELSIF current_user <> 'stator_app' AND TG_OP = 'UPDATE' AND NEW.published_at IS DISTINCT FROM OLD.published_at THEN
        RETURN NEW;
    ELSIF TG_OP = 'INSERT' THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSIF NEW.version IS DISTINCT FROM OLD.version THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSE
        NEW.published_at := OLD.published_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_published_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.kind = 'folder' THEN
        NEW.published_at := NULL;
    ELSIF TG_OP = 'INSERT' THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSIF NEW.version IS DISTINCT FROM OLD.version THEN
        NEW.published_at := CASE WHEN NEW.version > 0 THEN now() END;
    ELSE
        NEW.published_at := OLD.published_at;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION page_touch() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.archived_at, NEW.archive_id) IS DISTINCT FROM (OLD.archived_at, OLD.archive_id)
       AND (NEW.title, NEW.body, NEW.version, NEW.parent_id, NEW.space_id, NEW.rank, NEW.trashed_at)
           IS NOT DISTINCT FROM (OLD.title, OLD.body, OLD.version, OLD.parent_id, OLD.space_id, OLD.rank, OLD.trashed_at) THEN
        NEW.updated_at := OLD.updated_at;
    ELSIF NEW.body IS DISTINCT FROM OLD.body
       AND (NEW.title, NEW.version) IS NOT DISTINCT FROM (OLD.title, OLD.version)
       AND document_without_anchors(NEW.body, NULL) = document_without_anchors(OLD.body, NULL) THEN
        NEW.updated_at := OLD.updated_at;
    ELSE
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
ALTER TABLE attachment DROP COLUMN IF EXISTS original_author;
ALTER TABLE comment DROP COLUMN IF EXISTS original_author;
ALTER TABLE page_version DROP COLUMN IF EXISTS original_author;
DROP FUNCTION IF EXISTS space_key_taken(text);
DROP TABLE IF EXISTS space_import;
DROP TABLE IF EXISTS space_export;
