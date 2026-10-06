-- +goose Up
-- Public links (#80): whoever may edit a page makes a link that lets anybody
-- read that one published page without an account, until it is revoked or
-- runs out. The link's token lives here only as a digest; a revoked link
-- keeps its row, so who opened what to whom stays on record.
--
-- Reading through a link is the anonymous reader of 00470 holding one page
-- more: the transaction sets app.page_link to the digest of the token it was
-- given, and perm_page_viewable lets that reader view the page a live link
-- with that digest opens, and no other page, file or row because of it.

-- Whether links work at all in the organization; off, every link stops
-- working and none can be made, and turned on again they work again.
ALTER TABLE org ADD COLUMN public_links boolean NOT NULL DEFAULT true;

CREATE TABLE page_link (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    page_id    uuid NOT NULL,
    -- sha256 of the token; the token itself is shown once and never kept.
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    label      text NOT NULL DEFAULT '' CHECK (char_length(label) <= 60),
    -- NULL once its maker has left the organization; the link keeps working
    -- until the page's editors revoke it.
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz CHECK (expires_at IS NULL OR expires_at > created_at),
    revoked_at timestamptz,
    revoked_by uuid,
    PRIMARY KEY (org_id, id),
    CONSTRAINT page_link_token_hash_key UNIQUE (token_hash),
    CONSTRAINT page_link_revoked_by_somebody CHECK (revoked_by IS NULL OR revoked_at IS NOT NULL),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, created_by) REFERENCES org_member (org_id, user_id) ON DELETE SET NULL (created_by),
    FOREIGN KEY (org_id, revoked_by) REFERENCES org_member (org_id, user_id) ON DELETE SET NULL (revoked_by)
);

CREATE INDEX page_link_page_idx ON page_link (org_id, page_id) WHERE revoked_at IS NULL;

-- +goose StatementBegin
-- How many live links one page may have. public.MaxLinksPerPage is the same
-- number, and a test holds the two together.
CREATE FUNCTION page_link_max() RETURNS integer
    LANGUAGE sql IMMUTABLE
AS $$ SELECT 5 $$;

-- The digest of the token the transaction reads through, or NULL. Anything
-- but 64 hex digits is no digest, so a malformed setting fails no decode.
CREATE FUNCTION current_link_digest() RETURNS bytea
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT CASE WHEN current_setting('app.page_link', true) ~ '^[0-9a-f]{64}$'
                THEN decode(current_setting('app.page_link', true), 'hex') END
$$;

-- Why a page cannot be opened through a link, whoever holds one, or NULL:
-- it is a personal space's, a folder, in the trash, not published all the
-- way up, or under a view list, which always names people.
CREATE FUNCTION page_link_closed(target uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(target))
    SELECT CASE
        WHEN NOT EXISTS (SELECT 1 FROM page p WHERE p.org_id = current_org_id() AND p.id = target) THEN 'missing'
        WHEN EXISTS (SELECT 1 FROM page p JOIN space s ON s.org_id = p.org_id AND s.id = p.space_id
                     WHERE p.org_id = current_org_id() AND p.id = target AND s.owner_id IS NOT NULL) THEN 'personal'
        WHEN EXISTS (SELECT 1 FROM page p WHERE p.org_id = current_org_id() AND p.id = target AND p.kind = 'folder') THEN 'folder'
        WHEN EXISTS (SELECT 1 FROM page p WHERE p.org_id = current_org_id() AND p.id = target AND p.trashed_at IS NOT NULL) THEN 'trashed'
        WHEN EXISTS (SELECT 1 FROM chain WHERE version = 0) THEN 'unpublished'
        WHEN EXISTS (SELECT 1 FROM page_restriction r
                     WHERE r.org_id = current_org_id() AND r.kind = 'view' AND r.page_id IN (SELECT id FROM chain)) THEN 'restricted'
    END
$$;

-- Who makes, sees and revokes a page's links: whoever may change it, or
-- could but for its being archived, and never a guest, who is from outside.
CREATE FUNCTION perm_page_links_manageable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(target)),
         sp AS (SELECT space_id FROM chain LIMIT 1)
    SELECT actor IS NOT NULL
       AND perm_guest_space(actor) IS NULL
       AND perm_page_viewable(target, actor)
       AND perm_space_holds(actor, (SELECT space_id FROM sp), 'addPages')
       AND (perm_space_holds(actor, (SELECT space_id FROM sp), 'administer')
            OR perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'edit'))
$$;

-- Why the actor may not make a link for a page now, or NULL. How many links
-- the page already has is page_link_guard's to say.
CREATE FUNCTION page_link_refusal(target uuid, actor uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE
        WHEN NOT perm_page_links_manageable(target, actor) THEN 'cannotManage'
        WHEN NOT EXISTS (SELECT 1 FROM org WHERE id = current_org_id() AND public_links) THEN 'off'
        ELSE page_link_closed(target)
    END
$$;

-- The page the transaction's link opens: a live link of this organization
-- with the digest the reader holds, while the organization allows links and
-- the page may be opened at all. Only an anonymous reader holds one.
CREATE FUNCTION perm_link_page() RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT l.page_id FROM page_link l JOIN org o ON o.id = l.org_id
    WHERE current_anonymous() AND l.org_id = current_org_id()
      AND l.token_hash = current_link_digest()
      AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now())
      AND o.public_links AND o.archived_at IS NULL
      AND page_link_closed(l.page_id) IS NULL
$$;

-- As in 00470, and an anonymous reader holding a link views its page as
-- though its space were open: published, out of the trash, under no list.
CREATE OR REPLACE FUNCTION perm_page_viewable(page_id uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id)),
         sp AS (SELECT space_id FROM chain LIMIT 1)
    SELECT EXISTS (SELECT 1 FROM chain)
       AND NOT EXISTS (SELECT 1 FROM chain WHERE version = 0 AND created_by IS DISTINCT FROM actor)
       AND (perm_space_holds(actor, (SELECT space_id FROM sp), 'view')
            OR CASE WHEN actor IS NULL THEN perm_link_page() IS NOT DISTINCT FROM perm_page_viewable.page_id ELSE false END)
       AND (perm_space_holds(actor, (SELECT space_id FROM sp), 'administer')
            OR perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'view'))
       AND (actor IS NOT NULL
            OR (NOT EXISTS (SELECT 1 FROM chain WHERE version = 0)
                AND EXISTS (SELECT 1 FROM page t WHERE t.org_id = current_org_id()
                            AND t.id = perm_page_viewable.page_id AND t.trashed_at IS NULL)))
$$;

-- A page has at most page_link_max() live links; the lock makes two links
-- of one page wait for each other, so each counts the other. A revoked link
-- stays revoked, and is revoked now, whatever the statement says.
CREATE FUNCTION page_link_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        PERFORM pg_advisory_xact_lock(hashtextextended('page_link:' || NEW.org_id::text || ':' || NEW.page_id::text, 0));
        IF (SELECT count(*) FROM page_link
            WHERE org_id = NEW.org_id AND page_id = NEW.page_id
              AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())) >= page_link_max() THEN
            RAISE EXCEPTION 'a page has at most % live public links', page_link_max()
                USING ERRCODE = 'check_violation', CONSTRAINT = 'page_link_per_page';
        END IF;
        RETURN NEW;
    END IF;
    IF current_user = 'stator_app' THEN
        IF OLD.revoked_at IS NOT NULL THEN
            RAISE EXCEPTION 'a revoked link stays revoked'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'page_link_revoked_for_good';
        END IF;
        NEW.revoked_at := now();
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_link_guard BEFORE INSERT OR UPDATE ON page_link
    FOR EACH ROW EXECUTE FUNCTION page_link_guard();

-- Only an administrator of the organization turns links on or off, as only
-- one opens it to readers who are not signed in.
CREATE TRIGGER org_public_links_guard BEFORE UPDATE OF public_links ON org
    FOR EACH ROW EXECUTE FUNCTION org_anonymous_guard();

ALTER TABLE page_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_link FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_link_tenant_isolation ON page_link
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_link_admin_bypass ON page_link TO stator_admin USING (true) WITH CHECK (true);
-- A reader holding a link never reads the links themselves.
CREATE POLICY page_link_not_anonymous ON page_link AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY page_link_managers ON page_link AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_links_manageable(page_id, current_actor_id()));
CREATE POLICY page_link_makers ON page_link AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (created_by = current_actor_id() AND revoked_at IS NULL AND revoked_by IS NULL
                AND page_link_refusal(page_id, current_actor_id()) IS NULL);
CREATE POLICY page_link_revokers ON page_link AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_page_links_manageable(page_id, current_actor_id()))
    WITH CHECK (revoked_by = current_actor_id());

-- A link is made and revoked, never changed otherwise or deleted; the page
-- going for good takes its links along.
REVOKE ALL ON page_link FROM stator_app;
GRANT SELECT, INSERT ON page_link TO stator_app;
GRANT UPDATE (revoked_at, revoked_by) ON page_link TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_link TO stator_admin;

REVOKE EXECUTE ON FUNCTION perm_link_page(), page_link_closed(uuid), page_link_refusal(uuid, uuid),
    perm_page_links_manageable(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION perm_link_page(), page_link_closed(uuid), page_link_refusal(uuid, uuid),
    perm_page_links_manageable(uuid, uuid) TO stator_app, stator_admin;

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_page_viewable(page_id uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id)),
         sp AS (SELECT space_id FROM chain LIMIT 1)
    SELECT EXISTS (SELECT 1 FROM chain)
       AND NOT EXISTS (SELECT 1 FROM chain WHERE version = 0 AND created_by IS DISTINCT FROM actor)
       AND perm_space_holds(actor, (SELECT space_id FROM sp), 'view')
       AND (perm_space_holds(actor, (SELECT space_id FROM sp), 'administer')
            OR perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'view'))
       AND (actor IS NOT NULL
            OR (NOT EXISTS (SELECT 1 FROM chain WHERE version = 0)
                AND EXISTS (SELECT 1 FROM page t WHERE t.org_id = current_org_id()
                            AND t.id = perm_page_viewable.page_id AND t.trashed_at IS NULL)))
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS org_public_links_guard ON org;
DROP TABLE IF EXISTS page_link;
DROP FUNCTION IF EXISTS page_link_guard();
DROP FUNCTION IF EXISTS perm_link_page();
DROP FUNCTION IF EXISTS page_link_refusal(uuid, uuid);
DROP FUNCTION IF EXISTS perm_page_links_manageable(uuid, uuid);
DROP FUNCTION IF EXISTS page_link_closed(uuid);
DROP FUNCTION IF EXISTS current_link_digest();
DROP FUNCTION IF EXISTS page_link_max();
ALTER TABLE org DROP COLUMN IF EXISTS public_links;
