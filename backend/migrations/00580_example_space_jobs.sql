-- +goose Up
-- The example space is made by the worker (#306): making it takes some
-- hundred transactions, more than a request may last on a busy machine. An
-- administrator's click queues a job, and the page follows its state.
--
-- An organization has at most one job queued or running, so a second click
-- finds the first. A finished job stays as the latest word on how it went.
CREATE TABLE example_job (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    requested_by uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    language     text NOT NULL,
    state        text NOT NULL DEFAULT 'queued',
    requested_at timestamptz NOT NULL DEFAULT now(),
    -- Set by the worker alone. While it runs, space_id is the space it made
    -- so far, which a worker taking over after a crash deletes first.
    attempts     integer NOT NULL DEFAULT 0,
    lease_until  timestamptz,
    space_id     uuid REFERENCES space(id) ON DELETE SET NULL,
    failure      text,
    -- Past the worker's last write, for the requester's next read.
    written_lsn  pg_lsn,
    started_at   timestamptz,
    finished_at  timestamptz,
    CONSTRAINT example_job_language_known CHECK (language IN ('en', 'de')),
    CONSTRAINT example_job_state_known CHECK (state IN ('queued', 'running', 'done', 'failed')),
    CONSTRAINT example_job_failure_known CHECK (failure IN ('keys_taken', 'forbidden', 'failed')),
    CONSTRAINT example_job_failed_whole CHECK ((state = 'failed') = (failure IS NOT NULL)),
    CONSTRAINT example_job_running_leased CHECK ((state = 'running') = (lease_until IS NOT NULL))
);
CREATE UNIQUE INDEX example_job_one_open ON example_job (org_id) WHERE state IN ('queued', 'running');
CREATE INDEX example_job_waiting_idx ON example_job (requested_at) WHERE state IN ('queued', 'running');
CREATE INDEX example_job_latest_idx ON example_job (org_id, requested_at DESC);

ALTER TABLE example_job ENABLE ROW LEVEL SECURITY;
ALTER TABLE example_job FORCE  ROW LEVEL SECURITY;
CREATE POLICY example_job_tenant_isolation ON example_job
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY example_job_admin_bypass ON example_job TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY example_job_not_anonymous ON example_job AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
-- Those who may make the example follow its making, and queue it for
-- themselves; what it then does is the worker's to write.
CREATE POLICY example_job_readers ON example_job AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_is_admin(current_actor_id()) AND perm_token_whole(current_actor_id()));
CREATE POLICY example_job_requesters ON example_job AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (requested_by = current_actor_id() AND state = 'queued'
                AND perm_is_admin(current_actor_id()) AND perm_token_whole(current_actor_id()));
REVOKE ALL ON example_job FROM stator_app;
GRANT SELECT ON example_job TO stator_app;
GRANT INSERT (id, org_id, requested_by, language) ON example_job TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON example_job TO stator_admin;

-- +goose Down
DROP TABLE IF EXISTS example_job;
