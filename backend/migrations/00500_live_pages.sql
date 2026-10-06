-- +goose Up
-- Live pages (#70). A page is edited either through drafts its editors
-- publish, or live: what its editors type is the page as soon as it is saved.
-- A live page's history still records versions, each one held open for
-- live_version_span() and amended by every save within it, so the history
-- reads in steps of work rather than one per keystroke.
ALTER TABLE page
    ADD COLUMN mode text NOT NULL DEFAULT 'draft',
    ADD CONSTRAINT page_mode_known CHECK (mode IN ('draft', 'live')),
    -- A folder has no words to edit, live or otherwise.
    ADD CONSTRAINT page_folder_not_live CHECK (kind = 'page' OR mode = 'draft');

ALTER TABLE page_version
    ADD COLUMN live boolean NOT NULL DEFAULT false,
    ADD COLUMN updated_at timestamptz;
UPDATE page_version SET updated_at = created_at;
ALTER TABLE page_version
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now();

CREATE TRIGGER page_version_set_updated_at BEFORE UPDATE ON page_version
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- page.LiveVersionSpan, which a test holds to this.
CREATE FUNCTION live_version_span() RETURNS interval
    LANGUAGE sql IMMUTABLE
AS $$ SELECT interval '10 minutes' $$;

-- +goose StatementBegin
-- Whether a version is the open one of a live page: its latest, saved live,
-- and begun within the span. Only that one is ever amended.
CREATE FUNCTION page_version_open(org uuid, page_id uuid, number integer) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM page_version v JOIN page p ON p.org_id = v.org_id AND p.id = v.page_id
        WHERE v.org_id = org AND v.page_id = page_version_open.page_id AND v.number = page_version_open.number
          AND v.live AND p.mode = 'live' AND p.version = v.number
          AND v.created_at > now() - live_version_span());
$$;
-- +goose StatementEnd

-- History stays append only but for the open version of a live page, whose
-- title and body its editors amend until its span ends. Nothing else of a
-- version changes, so the grant names the two columns.
GRANT UPDATE (title, body) ON page_version TO stator_app;
CREATE POLICY page_version_amenders ON page_version AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (live AND created_at > now() - live_version_span() AND perm_page_editable(page_id, current_actor_id())
           AND EXISTS (SELECT 1 FROM page p WHERE p.org_id = page_version.org_id AND p.id = page_version.page_id
                       AND p.mode = 'live' AND p.version = page_version.number))
    WITH CHECK (live AND perm_page_editable(page_id, current_actor_id()));
-- A version saved live belongs to a live page; a page of drafts publishes.
CREATE POLICY page_version_live_on_live_pages ON page_version AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (NOT live OR EXISTS (SELECT 1 FROM page p WHERE p.org_id = page_version.org_id AND p.id = page_version.page_id AND p.mode = 'live'));

-- Everybody who saved into a live version, so an amended version still names
-- each of the people whose words it holds. Written once per person and version.
CREATE TABLE page_version_editor (
    org_id     uuid NOT NULL,
    page_id    uuid NOT NULL,
    number     integer NOT NULL,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, page_id, number, user_id),
    FOREIGN KEY (org_id, page_id, number) REFERENCES page_version (org_id, page_id, number) ON DELETE CASCADE
);
CREATE INDEX page_version_editor_user_idx ON page_version_editor (org_id, user_id, page_id);

ALTER TABLE page_version_editor ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_version_editor FORCE  ROW LEVEL SECURITY;
CREATE POLICY page_version_editor_tenant_isolation ON page_version_editor
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_version_editor_admin_bypass ON page_version_editor TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_version_editor_viewers ON page_version_editor AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
-- Only oneself, only into the open version, only as an editor of the page.
CREATE POLICY page_version_editor_savers ON page_version_editor AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (user_id = current_actor_id() AND perm_page_editable(page_id, current_actor_id())
                AND page_version_open(org_id, page_id, number));
REVOKE ALL ON page_version_editor FROM stator_app;
GRANT SELECT, INSERT ON page_version_editor TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_version_editor TO stator_admin;

-- +goose StatementBegin
-- A live page has no drafts: what is typed is the page.
CREATE FUNCTION page_draft_not_live() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM page WHERE org_id = NEW.org_id AND id = NEW.page_id AND mode = 'live') THEN
        RAISE EXCEPTION 'A live page has no drafts; what is typed is the page.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_draft_not_live';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER page_draft_not_live BEFORE INSERT OR UPDATE ON page_draft
    FOR EACH ROW EXECUTE FUNCTION page_draft_not_live();

-- +goose StatementBegin
-- Choosing how a page is edited is editing it, so it needs edit as its words
-- do, and an archived page keeps its mode.
CREATE FUNCTION page_mode_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_user = 'stator_app' AND NOT perm_page_editable(OLD.id, current_actor_id()) THEN
        RAISE EXCEPTION 'you may not change how that page is edited' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER page_mode_check BEFORE UPDATE OF mode ON page
    FOR EACH ROW WHEN (NEW.mode IS DISTINCT FROM OLD.mode)
    EXECUTE FUNCTION page_mode_guard();

-- +goose StatementBegin
-- A page that goes live takes everybody's drafts of it away, which row level
-- security would hide from the person switching: the service has asked them
-- first, naming whose drafts they are.
CREATE FUNCTION page_live_drops_drafts() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM page_draft WHERE org_id = NEW.org_id AND page_id = NEW.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER page_live_drops_drafts AFTER UPDATE OF mode ON page
    FOR EACH ROW WHEN (NEW.mode = 'live' AND OLD.mode <> 'live')
    EXECUTE FUNCTION page_live_drops_drafts();

-- +goose StatementBegin
-- Who holds a draft of a page that differs from it, for whoever may edit the
-- page and is about to make it live; nobody else learns of anybody's drafts.
CREATE FUNCTION page_pending_drafts(page uuid) RETURNS TABLE (user_id uuid, name text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT d.user_id, COALESCE(u.name, '')
    FROM page_draft d
    JOIN page p ON p.org_id = d.org_id AND p.id = d.page_id
    LEFT JOIN app_user u ON u.id = d.user_id
    WHERE d.org_id = current_org_id() AND d.page_id = page_pending_drafts.page
      AND (d.title, d.body) IS DISTINCT FROM (p.title, p.body)
      AND perm_page_editable(page_pending_drafts.page, current_actor_id())
    ORDER BY lower(COALESCE(u.name, '')), d.user_id;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION page_pending_drafts(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION page_pending_drafts(uuid) TO stator_app, stator_admin;

-- +goose Down
DROP FUNCTION IF EXISTS page_pending_drafts(uuid);
DROP TRIGGER IF EXISTS page_live_drops_drafts ON page;
DROP FUNCTION IF EXISTS page_live_drops_drafts();
DROP TRIGGER IF EXISTS page_mode_check ON page;
DROP FUNCTION IF EXISTS page_mode_guard();
DROP TRIGGER IF EXISTS page_draft_not_live ON page_draft;
DROP FUNCTION IF EXISTS page_draft_not_live();
DROP TABLE IF EXISTS page_version_editor;
DROP POLICY IF EXISTS page_version_live_on_live_pages ON page_version;
DROP POLICY IF EXISTS page_version_amenders ON page_version;
REVOKE UPDATE (title, body) ON page_version FROM stator_app;
DROP FUNCTION IF EXISTS page_version_open(uuid, uuid, integer);
DROP FUNCTION IF EXISTS live_version_span();
DROP TRIGGER IF EXISTS page_version_set_updated_at ON page_version;
ALTER TABLE page_version DROP COLUMN IF EXISTS updated_at, DROP COLUMN IF EXISTS live;
ALTER TABLE page
    DROP CONSTRAINT IF EXISTS page_folder_not_live,
    DROP CONSTRAINT IF EXISTS page_mode_known,
    DROP COLUMN IF EXISTS mode;
