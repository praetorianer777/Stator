-- +goose Up
-- Which pages link sync (#32) has to look at after a change, and how a page
-- is titled on an issue.

-- +goose StatementBegin
-- Whether a body names an Armature issue as a chip or an issue block. A list
-- block is a query, not a mention.
CREATE FUNCTION armature_names_issue(body jsonb) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT COALESCE(jsonb_path_exists(body, 'lax $.** ? (@.type == "armatureIssue" || @.type == "armatureIssueBlock")'), false)
$$;

-- Whether every member with use may view a page: nothing on the way up
-- carries a view list, and the space lets everyone view it. A space opened to
-- a group that happens to hold everybody counts as closed, which errs towards
-- telling Armature less.
CREATE FUNCTION page_open_to_members(page_id uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id))
    SELECT EXISTS (SELECT 1 FROM chain)
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN chain c ON c.id = r.page_id
                       WHERE r.org_id = current_org_id() AND r.kind = 'view')
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.org_id = current_org_id() AND g.space_id = (SELECT space_id FROM chain LIMIT 1)
                     AND g.subject_type = 'everyone')
$$;

-- Tells the worker to sync the links of the pages named, and with below of
-- every page under them too, as the person the transaction acts for. Only
-- pages that name an issue or carry links are worth an event, and none is
-- while the organization has no connection. It writes the events itself
-- rather than answering which pages they are, so nobody learns the ids of
-- pages below that they may not see.
CREATE FUNCTION armature_links_emit(roots uuid[], below boolean, trace text) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    made  integer;
BEGIN
    IF actor IS NULL OR NOT perm_is_member(actor)
       OR NOT EXISTS (SELECT 1 FROM armature_connection WHERE org_id = current_org_id()) THEN
        RETURN 0;
    END IF;
    WITH RECURSIVE pages (id) AS (
        SELECT p.id FROM page p WHERE p.org_id = current_org_id() AND p.id = ANY (roots)
        UNION
        SELECT p.id FROM page p JOIN pages ON p.parent_id = pages.id
        WHERE below AND p.org_id = current_org_id()
    )
    INSERT INTO outbox_event (org_id, topic, payload, trace_parent)
    SELECT current_org_id(), 'armature.links', jsonb_build_object('pageId', p.id, 'actorId', actor), NULLIF(trace, '')
    FROM pages JOIN page p ON p.id = pages.id
    WHERE armature_names_issue(p.body)
       OR EXISTS (SELECT 1 FROM armature_remote_link l WHERE l.org_id = current_org_id() AND l.page_id = p.id);
    GET DIAGNOSTICS made = ROW_COUNT;
    RETURN made;
END;
$$;

-- The same for every page of a space, after its permissions changed, and
-- for every page in its trash, before the trash is emptied.
CREATE FUNCTION armature_links_emit_space(target uuid, trashed boolean, trace text) RETURNS integer
    LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT armature_links_emit(ARRAY(
        SELECT id FROM page
        WHERE org_id = current_org_id() AND space_id = target AND (NOT trashed OR trash_id IS NOT NULL)), false, trace)
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS armature_links_emit_space(uuid, boolean, text);
DROP FUNCTION IF EXISTS armature_links_emit(uuid[], boolean, text);
DROP FUNCTION IF EXISTS page_open_to_members(uuid);
DROP FUNCTION IF EXISTS armature_names_issue(jsonb);
