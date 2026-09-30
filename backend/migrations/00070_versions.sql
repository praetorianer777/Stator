-- +goose Up
-- Published versions and drafts. page.version is the number of the latest
-- published version, and 0 while the page has never been published: such a
-- page is its creator's alone until they publish it.
ALTER TABLE page
    DROP CONSTRAINT page_version_positive,
    ADD CONSTRAINT page_version_not_negative CHECK (version >= 0),
    ALTER COLUMN version SET DEFAULT 0;

-- A version is written once and never changed; the page row carries a copy of
-- the latest one so nothing that reads a page joins the history.
CREATE TABLE page_version (
    org_id          uuid NOT NULL,
    page_id         uuid NOT NULL,
    number          integer NOT NULL,
    title           text NOT NULL,
    body            jsonb NOT NULL,
    comment         text NOT NULL DEFAULT '',
    notify_watchers boolean NOT NULL DEFAULT false,
    restored_from   integer,
    created_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, page_id, number),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    CONSTRAINT page_version_number_positive CHECK (number > 0),
    CONSTRAINT page_version_title_not_blank CHECK (btrim(title) <> ''),
    CONSTRAINT page_version_comment_length CHECK (char_length(comment) <= 500),
    CONSTRAINT page_version_restores_older CHECK (restored_from > 0 AND restored_from < number)
);

-- Numbers run 1, 2, 3 with no gaps, whoever writes them: a version is always
-- the one after the page's current version. The page row is locked so two
-- publishes queue rather than both reading the same number.
-- +goose StatementBegin
CREATE FUNCTION page_version_next() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    current integer;
BEGIN
    SELECT version INTO current FROM page WHERE org_id = NEW.org_id AND id = NEW.page_id FOR UPDATE;
    IF current IS NULL OR NEW.number <> current + 1 THEN
        RAISE EXCEPTION 'version % of a page is not the one after its current version %', NEW.number, current
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_version_in_sequence';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- One draft per page and person. It goes with the page, and with the person
-- when they leave the organization.
CREATE TABLE page_draft (
    org_id       uuid NOT NULL,
    page_id      uuid NOT NULL,
    user_id      uuid NOT NULL,
    title        text NOT NULL,
    body         jsonb NOT NULL,
    base_version integer NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, page_id, user_id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    CONSTRAINT page_draft_title_not_blank CHECK (btrim(title) <> ''),
    CONSTRAINT page_draft_base_not_negative CHECK (base_version >= 0)
);

CREATE INDEX page_draft_user_idx ON page_draft (org_id, user_id);

CREATE TRIGGER page_draft_set_updated_at BEFORE UPDATE ON page_draft
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE page_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_version FORCE  ROW LEVEL SECURITY;
ALTER TABLE page_draft   ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_draft   FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_version_tenant_isolation ON page_version
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_draft_tenant_isolation ON page_draft
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_version_admin_bypass ON page_version TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_draft_admin_bypass ON page_draft TO stator_admin USING (true) WITH CHECK (true);

-- History is append only for the app: no UPDATE, and rows leave only with
-- their page, by the cascade.
REVOKE ALL ON page_version FROM stator_app;
GRANT SELECT, INSERT ON page_version TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_version TO stator_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_draft TO stator_app, stator_admin;

-- Every page there is becomes version 1 of itself. The sequence trigger
-- comes after, since it would read the old save counts; updated_at stays, as
-- nobody changed the pages.
INSERT INTO page_version (org_id, page_id, number, title, body, created_by, created_at)
SELECT org_id, id, 1, title, body, updated_by, updated_at FROM page;
ALTER TABLE page DISABLE TRIGGER page_set_updated_at;
UPDATE page SET version = 1;
ALTER TABLE page ENABLE TRIGGER page_set_updated_at;

CREATE TRIGGER page_version_in_sequence BEFORE INSERT ON page_version
    FOR EACH ROW EXECUTE FUNCTION page_version_next();

-- +goose Down
DROP TABLE IF EXISTS page_draft;
DROP TABLE IF EXISTS page_version;
DROP FUNCTION IF EXISTS page_version_next();
UPDATE page SET version = 1 WHERE version = 0;
ALTER TABLE page
    DROP CONSTRAINT IF EXISTS page_version_not_negative,
    ADD CONSTRAINT page_version_positive CHECK (version > 0),
    ALTER COLUMN version SET DEFAULT 1;
