-- +goose Up
-- Space shortcuts (#39): links the administrators of a space pin above its
-- page tree, each to a page or to an address on the web. Ordered by a rank
-- from internal/rank, as sibling pages are.
CREATE TABLE space_shortcut (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    space_id   uuid NOT NULL,
    page_id    uuid,
    url        text,
    -- Empty on a page shortcut shows the page's title as it is now.
    label      text NOT NULL DEFAULT '',
    rank       text COLLATE "C" NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE CASCADE,
    -- A purged page takes its shortcuts along; one in the trash is kept.
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    CONSTRAINT space_shortcut_target CHECK ((page_id IS NULL) <> (url IS NULL)),
    -- Only the web's own schemes, no name or password before the host, and no
    -- blanks or control characters anywhere, so no address runs a script.
    CONSTRAINT space_shortcut_url_shape CHECK (url IS NULL OR (
        url ~ '^https?://[^/?#@[:space:][:cntrl:]]+([/?#][^[:space:][:cntrl:]]*)?$' AND length(url) <= 2000)),
    CONSTRAINT space_shortcut_label_shape CHECK (
        label = btrim(label) AND length(label) <= 100 AND (url IS NULL OR label <> '')),
    CONSTRAINT space_shortcut_rank_shape CHECK (rank <> '')
);

CREATE INDEX space_shortcut_order_idx ON space_shortcut (org_id, space_id, rank, id);
CREATE INDEX space_shortcut_page_idx ON space_shortcut (org_id, page_id) WHERE page_id IS NOT NULL;

-- +goose StatementBegin
-- How many shortcuts one space holds. shortcut.MaxPerSpace is the same
-- number, and a test holds the two together.
CREATE FUNCTION space_shortcut_max() RETURNS integer
    LANGUAGE sql IMMUTABLE
AS $$ SELECT 30 $$;

-- The limit, kept by the database so no path around the service pins more.
-- It counts as the owner, so shortcuts the actor may not see count too, and
-- the lock makes two additions to one space wait for each other.
CREATE FUNCTION space_shortcut_limit() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('space_shortcut:' || NEW.space_id::text, 0));
    IF (SELECT count(*) FROM space_shortcut
        WHERE org_id = NEW.org_id AND space_id = NEW.space_id) >= space_shortcut_max() THEN
        RAISE EXCEPTION 'a space holds at most % shortcuts', space_shortcut_max()
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_shortcut_limit';
    END IF;
    RETURN NEW;
END;
$$;

-- Whether the actor may point a shortcut at a page: one they may view, out
-- of the trash, so the administrator never pins what they cannot see.
CREATE FUNCTION space_shortcut_page_ok(page uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT page IS NULL OR (
        EXISTS (SELECT 1 FROM page p WHERE p.id = page AND p.org_id = current_org_id() AND p.trashed_at IS NULL)
        AND perm_page_viewable(page, current_actor_id()))
$$;
-- +goose StatementEnd

CREATE TRIGGER space_shortcut_limit BEFORE INSERT ON space_shortcut
    FOR EACH ROW EXECUTE FUNCTION space_shortcut_limit();

ALTER TABLE space_shortcut ENABLE ROW LEVEL SECURITY;
ALTER TABLE space_shortcut FORCE  ROW LEVEL SECURITY;

CREATE POLICY space_shortcut_tenant_isolation ON space_shortcut
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY space_shortcut_admin_bypass ON space_shortcut TO stator_admin USING (true) WITH CHECK (true);

-- Everybody who may view the space reads its shortcuts, but a shortcut to a
-- page only while they may view that page: the shortcut never names it.
CREATE POLICY space_shortcut_read ON space_shortcut AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_space_holds(current_actor_id(), space_id, 'view')
           AND (page_id IS NULL OR perm_page_viewable(page_id, current_actor_id())));
-- Only the space's administrators add, order and remove them.
CREATE POLICY space_shortcut_add ON space_shortcut AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (perm_space_holds(current_actor_id(), space_id, 'administer') AND space_shortcut_page_ok(page_id));
CREATE POLICY space_shortcut_change ON space_shortcut AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_space_holds(current_actor_id(), space_id, 'administer'))
    WITH CHECK (perm_space_holds(current_actor_id(), space_id, 'administer'));
CREATE POLICY space_shortcut_remove ON space_shortcut AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_space_holds(current_actor_id(), space_id, 'administer'));

-- A shortcut moves among its space's, and is otherwise removed and added
-- again: where it points, and in which space, never changes.
REVOKE ALL ON space_shortcut FROM stator_app;
GRANT SELECT, INSERT, DELETE ON space_shortcut TO stator_app;
GRANT UPDATE (rank, label) ON space_shortcut TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON space_shortcut TO stator_admin;

-- +goose Down
DROP TABLE IF EXISTS space_shortcut;
DROP FUNCTION IF EXISTS space_shortcut_page_ok(uuid);
DROP FUNCTION IF EXISTS space_shortcut_limit();
DROP FUNCTION IF EXISTS space_shortcut_max();
