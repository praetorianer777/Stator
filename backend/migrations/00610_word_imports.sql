-- +goose Up
-- Importing several Word documents at once (#89) runs in the worker, as the
-- example space does (00580): each document is a page with its pictures,
-- some tens of transactions, and fifty of them outlast a request. A request
-- stores the upload and queues a row, the page follows it, and the worker
-- claims it with a lease and makes the pages as the person who asked.
CREATE TABLE word_import (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    requested_by uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    -- The page the documents go under; the import goes with it.
    parent_id    uuid NOT NULL,
    file_count   integer NOT NULL,
    size_bytes   bigint NOT NULL,
    -- Files of the upload that are no Word documents, named in the report.
    skipped      text[] NOT NULL DEFAULT '{}',
    state        text NOT NULL DEFAULT 'queued',
    requested_at timestamptz NOT NULL DEFAULT now(),
    -- Set by the worker alone. made is the pages an attempt made at the top,
    -- which one taking over after a crash trashes before it begins again.
    attempts     integer NOT NULL DEFAULT 0,
    lease_until  timestamptz,
    done_steps   integer NOT NULL DEFAULT 0,
    total_steps  integer NOT NULL DEFAULT 0,
    made         uuid[] NOT NULL DEFAULT '{}',
    -- What became of each file: the page it made and its warnings, or why not.
    report       jsonb NOT NULL DEFAULT '[]',
    failure      text,
    written_lsn  pg_lsn,
    started_at   timestamptz,
    finished_at  timestamptz,
    -- Where the upload waits, under the organization's prefix, so deleting
    -- the organization takes it along.
    object_key   text GENERATED ALWAYS AS ('org/' || org_id::text || '/word-import/' || id::text) STORED,
    FOREIGN KEY (org_id, parent_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    CONSTRAINT word_import_files CHECK (file_count BETWEEN 1 AND 50),
    CONSTRAINT word_import_size_positive CHECK (size_bytes > 0),
    CONSTRAINT word_import_state_known CHECK (state IN ('queued', 'running', 'done', 'failed')),
    CONSTRAINT word_import_failure_known CHECK (failure IN ('forbidden', 'parent_gone', 'failed')),
    CONSTRAINT word_import_failed_whole CHECK ((state = 'failed') = (failure IS NOT NULL)),
    CONSTRAINT word_import_running_leased CHECK ((state = 'running') = (lease_until IS NOT NULL)),
    CONSTRAINT word_import_steps CHECK (done_steps >= 0 AND total_steps >= 0 AND done_steps <= total_steps)
);
CREATE INDEX word_import_waiting_idx ON word_import (requested_at) WHERE state IN ('queued', 'running');
CREATE INDEX word_import_parent_idx ON word_import (org_id, parent_id);

ALTER TABLE word_import ENABLE ROW LEVEL SECURITY;
ALTER TABLE word_import FORCE  ROW LEVEL SECURITY;
CREATE POLICY word_import_tenant_isolation ON word_import
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY word_import_admin_bypass ON word_import TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY word_import_not_anonymous ON word_import AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
-- The report names the pages made, which only their maker follows. An import
-- is queued only by whoever may add pages under the parent and edit it, for
-- themselves and as a job still to run; what it then does is the worker's.
CREATE POLICY word_import_readers ON word_import AS RESTRICTIVE FOR SELECT TO stator_app
    USING (requested_by = current_actor_id());
CREATE POLICY word_import_requesters ON word_import AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (requested_by = current_actor_id() AND state = 'queued'
                AND perm_page_arrangeable(parent_id, current_actor_id())
                AND perm_page_editable(parent_id, current_actor_id()));
REVOKE ALL ON word_import FROM stator_app;
GRANT SELECT ON word_import TO stator_app;
GRANT INSERT (id, org_id, requested_by, parent_id, file_count, size_bytes, skipped) ON word_import TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON word_import TO stator_admin;

-- +goose Down
DROP TABLE IF EXISTS word_import;
