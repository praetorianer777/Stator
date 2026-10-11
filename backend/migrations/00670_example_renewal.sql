-- +goose Up
-- Renewing the example space (#320). The space records the version of the
-- content it was made or last renewed from, and the language of its pages;
-- an example made before versions were recorded is version 0.
ALTER TABLE space
    ADD COLUMN example_version integer,
    ADD COLUMN example_language text;

UPDATE space s
SET example_version = 0,
    example_language = COALESCE(
        (SELECT j.language FROM example_job j
         WHERE j.space_id = s.id AND j.state = 'done'
         ORDER BY j.finished_at DESC LIMIT 1), 'en')
WHERE example;

ALTER TABLE space
    ADD CONSTRAINT space_example_versioned CHECK (
        (example_version IS NOT NULL) = example AND (example_language IS NOT NULL) = example),
    ADD CONSTRAINT space_example_version_counted CHECK (example_version >= 0),
    ADD CONSTRAINT space_example_language_known CHECK (example_language IN ('en', 'de'));

-- +goose StatementBegin
-- What a space was renewed from is the worker's to say, once it renewed it:
-- through the app role an administrator could otherwise hide a newer example
-- by claiming to have it.
CREATE FUNCTION space_example_version_fixed() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF current_user = 'stator_app'
       AND (NEW.example_version IS DISTINCT FROM OLD.example_version
            OR NEW.example_language IS DISTINCT FROM OLD.example_language) THEN
        RAISE EXCEPTION 'The version of the example space changes only when it is renewed. Renew the example space instead.'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER space_example_version_fixed BEFORE UPDATE OF example_version, example_language ON space
    FOR EACH ROW EXECUTE FUNCTION space_example_version_fixed();

-- The pages the example made, by the name of the file each was made from,
-- with the version it last wrote: a page whose version moved on since was
-- changed by somebody, and a renewal leaves it to them. Only the worker
-- keeps them, as it keeps the jobs.
CREATE TABLE example_page (
    space_id uuid NOT NULL REFERENCES space(id) ON DELETE CASCADE,
    name     text NOT NULL,
    org_id   uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    -- Null once the page is purged; a page in the trash is still named.
    page_id  uuid REFERENCES page(id) ON DELETE SET NULL,
    version  integer NOT NULL,
    -- A folder has no versions to move on, so its title says if it was renamed.
    title    text NOT NULL,
    PRIMARY KEY (space_id, name)
);
CREATE INDEX example_page_page_idx ON example_page (page_id);

ALTER TABLE example_page ENABLE ROW LEVEL SECURITY;
ALTER TABLE example_page FORCE  ROW LEVEL SECURITY;
CREATE POLICY example_page_tenant_isolation ON example_page
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY example_page_admin_bypass ON example_page TO stator_admin USING (true) WITH CHECK (true);
REVOKE ALL ON example_page FROM stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON example_page TO stator_admin;

-- A job makes the example or renews the one there is. A renewal names the
-- space from the start, loses it when the space is deleted before it runs,
-- and ends with what it left to people.
ALTER TABLE example_job
    ADD COLUMN kind text NOT NULL DEFAULT 'make',
    ADD COLUMN report jsonb,
    ADD CONSTRAINT example_job_kind_known CHECK (kind IN ('make', 'renew'));

ALTER TABLE example_job DROP CONSTRAINT example_job_failure_known;
ALTER TABLE example_job ADD CONSTRAINT example_job_failure_known CHECK (failure IN ('keys_taken', 'forbidden', 'failed', 'gone'));

-- The app role queues a making without a space, or a renewal of the
-- organization's example, and nothing else.
CREATE POLICY example_job_renews_the_example ON example_job AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK ((kind = 'make' AND space_id IS NULL)
                OR (kind = 'renew' AND space_id IN (SELECT id FROM space WHERE org_id = current_org_id() AND example)));
GRANT INSERT (kind, space_id) ON example_job TO stator_app;

-- +goose Down
REVOKE INSERT (kind, space_id) ON example_job FROM stator_app;
DROP POLICY IF EXISTS example_job_renews_the_example ON example_job;
DELETE FROM example_job WHERE kind = 'renew';
ALTER TABLE example_job DROP CONSTRAINT IF EXISTS example_job_failure_known;
ALTER TABLE example_job ADD CONSTRAINT example_job_failure_known CHECK (failure IN ('keys_taken', 'forbidden', 'failed'));
ALTER TABLE example_job
    DROP CONSTRAINT IF EXISTS example_job_kind_known,
    DROP COLUMN IF EXISTS report,
    DROP COLUMN IF EXISTS kind;
DROP TABLE IF EXISTS example_page;
DROP TRIGGER IF EXISTS space_example_version_fixed ON space;
DROP FUNCTION IF EXISTS space_example_version_fixed();
ALTER TABLE space
    DROP CONSTRAINT IF EXISTS space_example_language_known,
    DROP CONSTRAINT IF EXISTS space_example_version_counted,
    DROP CONSTRAINT IF EXISTS space_example_versioned,
    DROP COLUMN IF EXISTS example_language,
    DROP COLUMN IF EXISTS example_version;
