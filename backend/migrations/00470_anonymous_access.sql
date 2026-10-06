-- +goose Up
-- Reading without signing in (#79). An organization may let anybody read the
-- spaces that allow it: a switch on the organization, off until an
-- administrator turns it on, and a grant of view to the anonymous subject in
-- each such space, which counts only while the switch is on.
--
-- An anonymous reader is a principal of its own: a transaction that sets
-- app.anonymous and names no person. The permission functions give it view
-- of those spaces and nothing else, and restrictive policies on every table
-- keep it to reading spaces, pages and files, and to writing nothing.

ALTER TABLE org
    ADD COLUMN anonymous_access boolean NOT NULL DEFAULT false,
    -- Whether search engines are asked to leave the public pages alone.
    ADD COLUMN anonymous_indexable boolean NOT NULL DEFAULT false;

ALTER TABLE space_grant DROP CONSTRAINT space_grant_subject_type_check;
ALTER TABLE space_grant ADD CONSTRAINT space_grant_subject_type_check
    CHECK (subject_type IN ('user', 'group', 'everyone', 'anonymous'));
ALTER TABLE space_grant DROP CONSTRAINT space_grant_subject;
ALTER TABLE space_grant ADD CONSTRAINT space_grant_subject CHECK (
    (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
    (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL) OR
    (subject_type = 'everyone' AND user_id IS NULL AND group_id IS NULL) OR
    -- Somebody who is not signed in may read, and never more.
    (subject_type = 'anonymous' AND user_id IS NULL AND group_id IS NULL AND permission = 'view'));

-- +goose StatementBegin
-- Whether the transaction reads for somebody who is not signed in.
CREATE FUNCTION current_anonymous() RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT COALESCE(current_setting('app.anonymous', true) = 'on', false)
$$;

-- Whether anybody may read a space without signing in: the organization
-- allows it and the space grants it.
CREATE FUNCTION perm_space_anonymous(space uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM org o WHERE o.id = current_org_id() AND o.anonymous_access)
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.org_id = current_org_id() AND g.space_id = space
                     AND g.subject_type = 'anonymous' AND g.permission = 'view')
$$;

-- As in 00310, and an anonymous reader, who is nobody, views the spaces
-- that allow it and holds nothing else anywhere.
CREATE OR REPLACE FUNCTION perm_space_holds(actor uuid, space uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN actor IS NULL
        THEN current_anonymous() AND permission = 'view' AND perm_space_anonymous(space)
        ELSE perm_token_reaches(actor, space)
             AND (perm_role_admin(actor)
                  OR (perm_global_holds(actor, 'use') AND EXISTS (
                      SELECT 1 FROM unnest(perm_space_grants(actor, space)) AS held (p)
                      WHERE held.p = permission OR held.p = 'administer' OR permission = 'view')))
    END
$$;

-- As in 00090, and an anonymous reader views only published pages out of the
-- trash: an unpublished page of somebody since deleted names no creator.
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

-- Only an organization administrator opens the organization to readers who
-- are not signed in, or asks search engines in.
CREATE FUNCTION org_anonymous_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF current_user = 'stator_app' AND NOT perm_is_admin(current_actor_id()) THEN
        RAISE EXCEPTION 'only an administrator of the organization lets people read without signing in'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

-- A personal space is named after its owner, and its pages are theirs; it
-- is never public.
CREATE FUNCTION space_grant_anonymous_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.subject_type = 'anonymous'
       AND EXISTS (SELECT 1 FROM space WHERE org_id = NEW.org_id AND id = NEW.space_id AND owner_id IS NOT NULL) THEN
        RAISE EXCEPTION 'A personal space cannot be read without signing in. Move the pages to a team space to publish them.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_anonymous_team_space';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_anonymous_guard BEFORE UPDATE OF anonymous_access, anonymous_indexable ON org
    FOR EACH ROW EXECUTE FUNCTION org_anonymous_guard();
CREATE TRIGGER space_grant_anonymous_check BEFORE INSERT OR UPDATE ON space_grant
    FOR EACH ROW EXECUTE FUNCTION space_grant_anonymous_guard();

-- Every table is closed to an anonymous reader, but for the three a public
-- page is read from, and those it may only read; there the policies that
-- were already there decide which rows. A table added later needs a policy
-- of its own, which TestAnonymousReadersAreHeldByEveryTable asks for.
-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOR t IN
        SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND c.relrowsecurity
    LOOP
        IF t IN ('space', 'page', 'attachment') THEN
            EXECUTE format('CREATE POLICY %I ON %I AS RESTRICTIVE FOR INSERT TO stator_app WITH CHECK (NOT current_anonymous())',
                           t || '_anonymous_adds_nothing', t);
            EXECUTE format('CREATE POLICY %I ON %I AS RESTRICTIVE FOR UPDATE TO stator_app USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous())',
                           t || '_anonymous_changes_nothing', t);
            EXECUTE format('CREATE POLICY %I ON %I AS RESTRICTIVE FOR DELETE TO stator_app USING (NOT current_anonymous())',
                           t || '_anonymous_removes_nothing', t);
        ELSE
            EXECUTE format('CREATE POLICY %I ON %I AS RESTRICTIVE FOR ALL TO stator_app USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous())',
                           t || '_not_anonymous', t);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION perm_space_anonymous(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION perm_space_anonymous(uuid) TO stator_app, stator_admin;

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE
    p record;
BEGIN
    FOR p IN
        SELECT policyname, tablename FROM pg_policies
        WHERE schemaname = 'public'
          AND (policyname LIKE '%\_not\_anonymous' OR policyname LIKE '%\_anonymous\_adds\_nothing'
               OR policyname LIKE '%\_anonymous\_changes\_nothing' OR policyname LIKE '%\_anonymous\_removes\_nothing')
    LOOP
        EXECUTE format('DROP POLICY %I ON %I', p.policyname, p.tablename);
    END LOOP;
END;
$$;

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
$$;

CREATE OR REPLACE FUNCTION perm_space_holds(actor uuid, space uuid, permission text) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_token_reaches(actor, space)
       AND (perm_role_admin(actor)
            OR (perm_global_holds(actor, 'use') AND EXISTS (
                SELECT 1 FROM unnest(perm_space_grants(actor, space)) AS held (p)
                WHERE held.p = permission OR held.p = 'administer' OR permission = 'view')))
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS space_grant_anonymous_check ON space_grant;
DROP TRIGGER IF EXISTS org_anonymous_guard ON org;
DROP FUNCTION IF EXISTS space_grant_anonymous_guard();
DROP FUNCTION IF EXISTS org_anonymous_guard();
DROP FUNCTION IF EXISTS perm_space_anonymous(uuid);
DROP FUNCTION IF EXISTS current_anonymous();

DELETE FROM space_grant WHERE subject_type = 'anonymous';
ALTER TABLE space_grant DROP CONSTRAINT space_grant_subject;
ALTER TABLE space_grant ADD CONSTRAINT space_grant_subject CHECK (
    (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
    (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL) OR
    (subject_type = 'everyone' AND user_id IS NULL AND group_id IS NULL));
ALTER TABLE space_grant DROP CONSTRAINT space_grant_subject_type_check;
ALTER TABLE space_grant ADD CONSTRAINT space_grant_subject_type_check
    CHECK (subject_type IN ('user', 'group', 'everyone'));

ALTER TABLE org DROP COLUMN IF EXISTS anonymous_indexable, DROP COLUMN IF EXISTS anonymous_access;
