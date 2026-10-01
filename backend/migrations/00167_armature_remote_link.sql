-- +goose Up
-- What link sync (#32) last did in Armature for each issue a page names: the
-- remote link's id there, the url and title it was sent with, and whether it
-- got there. No foreign key to the page, so the sync after a purge still
-- finds what it has to take off the issues.
CREATE TABLE armature_remote_link (
    org_id         uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    page_id        uuid NOT NULL,
    issue_key      text NOT NULL CHECK (issue_key ~ '^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$'),
    -- Null until Armature made the link, and again once it took it off.
    remote_link_id uuid,
    url            text,
    title          text,
    state          text NOT NULL CHECK (state IN ('synced', 'pending', 'failed')),
    error          text,
    synced_at      timestamptz,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, page_id, issue_key),
    CONSTRAINT armature_remote_link_failed_says_why CHECK ((state = 'failed') = (error IS NOT NULL))
);

ALTER TABLE armature_remote_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_remote_link FORCE  ROW LEVEL SECURITY;

CREATE POLICY armature_remote_link_tenant_isolation ON armature_remote_link
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY armature_remote_link_admin_bypass ON armature_remote_link TO stator_admin USING (true) WITH CHECK (true);

-- Only the worker writes the records; a member reads those of the pages they
-- may view, which is all GET /pages/{id}/armature-links shows.
CREATE POLICY armature_remote_link_viewable ON armature_remote_link AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON armature_remote_link TO stator_admin;
REVOKE ALL ON armature_remote_link FROM stator_app;
GRANT SELECT ON armature_remote_link TO stator_app;

-- A new address or Armature organization, or none, drops the records with
-- the tokens: they describe links in an Armature that is no longer this
-- organization's. A later sync to the same instance finds its links by url.
-- +goose StatementBegin
CREATE FUNCTION armature_connection_forgets_links() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_remote_link WHERE org_id = OLD.org_id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER armature_connection_forgets_links AFTER UPDATE OR DELETE ON armature_connection
    FOR EACH ROW EXECUTE FUNCTION armature_connection_forgets_links();

-- +goose Down
DROP TRIGGER IF EXISTS armature_connection_forgets_links ON armature_connection;
DROP FUNCTION IF EXISTS armature_connection_forgets_links();
DROP TABLE IF EXISTS armature_remote_link;
