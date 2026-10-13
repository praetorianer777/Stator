-- +goose Up
-- An instance serves one organization (#380), so nothing sets or reads the
-- tenant any more: the tenant policies go, and so does the organization
-- filter inside every function. What keeps one person from another's rows
-- are the restrictive access policies, which stay as they were.

-- Kept one more migration only so that the column defaults below, and
-- anything this one missed, still find the organization; the schema change
-- that drops org_id drops it too. SECURITY DEFINER so that a caller whom a
-- policy keeps from org, such as an anonymous reader, still gets it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT id FROM org
$$;
-- +goose StatementEnd

-- Inserts stop naming the organization; the column fills itself until it is
-- dropped.
-- +goose StatementBegin
DO $$
DECLARE
    t record;
BEGIN
    FOR t IN
        SELECT c.relname FROM pg_attribute a
        JOIN pg_class c ON c.oid = a.attrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind = 'r' AND a.attname = 'org_id'
          AND NOT a.attisdropped AND a.attgenerated = ''
    LOOP
        EXECUTE format('ALTER TABLE %I ALTER COLUMN org_id SET DEFAULT current_org_id()', t.relname);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- The tenant policies were the permissive ones: every access policy is
-- restrictive, and restrictive policies alone admit no row. Each table gets
-- a permissive policy that admits every row in their place. It is for
-- PUBLIC, as the tenant policies were, because the SECURITY DEFINER
-- functions run as the migrations' role, which forced row level security
-- binds wherever that role is no superuser (stator_owner under CNPG).
-- app_user keeps to people who are members, as it did through the tenant.
-- org is read and changed but never made or removed through the app role:
-- the tenant policy left nothing else to make, and removing it took all.
-- +goose StatementBegin
DO $$
DECLARE
    p record;
BEGIN
    FOR p IN
        SELECT tablename FROM pg_policies
        WHERE schemaname = 'public' AND policyname = tablename || '_tenant_isolation'
    LOOP
        EXECUTE format('DROP POLICY %I ON %I', p.tablename || '_tenant_isolation', p.tablename);
        IF p.tablename = 'app_user' THEN
            CREATE POLICY app_user_app ON app_user
                USING (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id))
                WITH CHECK (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id));
        ELSIF p.tablename = 'org' THEN
            CREATE POLICY org_app ON org FOR SELECT USING (true);
            CREATE POLICY org_app_changes ON org FOR UPDATE USING (true) WITH CHECK (true);
        ELSE
            EXECUTE format('CREATE POLICY %I ON %I USING (true) WITH CHECK (true)', p.tablename || '_app', p.tablename);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

ALTER POLICY example_job_renews_the_example ON example_job
    WITH CHECK ((kind = 'make' AND space_id IS NULL)
                OR (kind = 'renew' AND space_id IN (SELECT s.id FROM space s WHERE s.example)));
ALTER POLICY page_review_subject_namers ON page_review_subject
    WITH CHECK (page_review_by(review_id, current_actor_id())
                AND page_review_pending_on((SELECT r.page_id FROM page_review r WHERE r.id = page_review_subject.review_id)) = review_id
                AND page_review_approvals(review_id) = 0);

-- An index that led with org_id served queries that named the organization;
-- without it the planner cannot read such an index in order, and a list
-- such as the home page's updates sorts the whole table instead of stopping
-- after a page. Each is made again without org_id. Unique indexes stay as
-- they are until org_id goes: an equality on their later columns still finds
-- its way in, and ON CONFLICT names them by their columns.
DROP INDEX api_token_org_user_idx;
CREATE INDEX api_token_org_user_idx ON public.api_token USING btree (user_id);
DROP INDEX api_token_space_space_idx;
CREATE INDEX api_token_space_space_idx ON public.api_token_space USING btree (space_id);
DROP INDEX attachment_page_idx;
CREATE INDEX attachment_page_idx ON public.attachment USING btree (page_id, created_at DESC);
DROP INDEX audit_log_org_action_idx;
CREATE INDEX audit_log_org_action_idx ON public.audit_log USING btree (action, created_at DESC, id DESC);
DROP INDEX audit_log_org_actor_idx;
CREATE INDEX audit_log_org_actor_idx ON public.audit_log USING btree (actor_user_id, created_at DESC, id DESC);
DROP INDEX audit_log_org_created_idx;
CREATE INDEX audit_log_org_created_idx ON public.audit_log USING btree (created_at DESC, id DESC);
DROP INDEX audit_log_target_idx;
CREATE INDEX audit_log_target_idx ON public.audit_log USING btree (target_id, created_at DESC, id DESC);
DROP INDEX calendar_event_range_idx;
CREATE INDEX calendar_event_range_idx ON public.calendar_event USING btree (calendar_id, starts_at);
DROP INDEX comment_page_idx;
CREATE INDEX comment_page_idx ON public.comment USING btree (page_id) WHERE (deleted_at IS NULL);
DROP INDEX comment_thread_idx;
CREATE INDEX comment_thread_idx ON public.comment USING btree (thread_id, created_at, id);
DROP INDEX comment_thread_page_idx;
CREATE INDEX comment_thread_page_idx ON public.comment_thread USING btree (page_id, id);
DROP INDEX example_job_latest_idx;
CREATE INDEX example_job_latest_idx ON public.example_job USING btree (requested_at DESC);
DROP INDEX group_member_user_idx;
CREATE INDEX group_member_user_idx ON public.group_member USING btree (user_id);
DROP INDEX notification_comment_idx;
CREATE INDEX notification_comment_idx ON public.notification USING btree (comment_id) WHERE (comment_id IS NOT NULL);
DROP INDEX notification_latest_idx;
CREATE INDEX notification_latest_idx ON public.notification USING btree (user_id, created_at DESC);
DROP INDEX notification_page_idx;
CREATE INDEX notification_page_idx ON public.notification USING btree (page_id);
DROP INDEX notification_unread_idx;
CREATE INDEX notification_unread_idx ON public.notification USING btree (user_id) WHERE (read_at IS NULL);
DROP INDEX org_export_latest_idx;
CREATE INDEX org_export_latest_idx ON public.org_export USING btree (requested_at DESC);
DROP INDEX org_import_latest_idx;
CREATE INDEX org_import_latest_idx ON public.org_import USING btree (requested_at DESC);
DROP INDEX org_member_guest_space_idx;
CREATE INDEX org_member_guest_space_idx ON public.org_member USING btree (guest_space_id) WHERE (guest_space_id IS NOT NULL);
DROP INDEX page_archive_items_idx;
CREATE INDEX page_archive_items_idx ON public.page USING btree (archive_id) WHERE (archive_id IS NOT NULL);
DROP INDEX page_archive_roots_idx;
CREATE INDEX page_archive_roots_idx ON public.page USING btree (space_id, archived_at DESC) WHERE (archive_id = id);
DROP INDEX page_children_idx;
CREATE INDEX page_children_idx ON public.page USING btree (parent_id, rank);
DROP INDEX page_posts_org_idx;
CREATE INDEX page_posts_org_idx ON public.page USING btree (posted_at DESC, id DESC) WHERE ((kind = 'post'::text) AND (posted_at IS NOT NULL));
DROP INDEX page_posts_space_idx;
CREATE INDEX page_posts_space_idx ON public.page USING btree (space_id, posted_at DESC, id DESC) WHERE ((kind = 'post'::text) AND (posted_at IS NOT NULL));
DROP INDEX page_published_idx;
CREATE INDEX page_published_idx ON public.page USING btree (published_at DESC, id DESC) WHERE ((published_at IS NOT NULL) AND (trashed_at IS NULL));
DROP INDEX page_space_idx;
CREATE INDEX page_space_idx ON public.page USING btree (space_id);
DROP INDEX page_trash_items_idx;
CREATE INDEX page_trash_items_idx ON public.page USING btree (trash_id) WHERE (trash_id IS NOT NULL);
DROP INDEX page_trash_roots_idx;
CREATE INDEX page_trash_roots_idx ON public.page USING btree (space_id, trashed_at DESC) WHERE (trash_id = id);
DROP INDEX page_unpublished_idx;
CREATE INDEX page_unpublished_idx ON public.page USING btree (created_by) WHERE (version = 0);
DROP INDEX page_draft_user_idx;
CREATE INDEX page_draft_user_idx ON public.page_draft USING btree (user_id);
DROP INDEX page_label_name_idx;
CREATE INDEX page_label_name_idx ON public.page_label USING btree (name text_pattern_ops, page_id);
DROP INDEX page_link_page_idx;
CREATE INDEX page_link_page_idx ON public.page_link USING btree (page_id) WHERE (revoked_at IS NULL);
DROP INDEX page_owner_user_idx;
CREATE INDEX page_owner_user_idx ON public.page_owner USING btree (user_id);
DROP INDEX page_restriction_page_idx;
CREATE INDEX page_restriction_page_idx ON public.page_restriction USING btree (page_id, kind);
DROP INDEX page_review_page_idx;
CREATE INDEX page_review_page_idx ON public.page_review USING btree (page_id, submitted_at DESC);
DROP INDEX page_schedule_user_idx;
CREATE INDEX page_schedule_user_idx ON public.page_schedule USING btree (user_id);
DROP INDEX page_share_page_idx;
CREATE INDEX page_share_page_idx ON public.page_share USING btree (page_id);
DROP INDEX page_share_sharer_idx;
CREATE INDEX page_share_sharer_idx ON public.page_share USING btree (sharer_id, created_at DESC);
DROP INDEX page_task_done_idx;
CREATE INDEX page_task_done_idx ON public.page_task USING btree (assignee_id, done_at DESC, id DESC) WHERE (done AND (assignee_id IS NOT NULL));
DROP INDEX page_task_open_idx;
CREATE INDEX page_task_open_idx ON public.page_task USING btree (assignee_id, COALESCE(due_on, '9999-12-31'::date), id) WHERE ((NOT done) AND (assignee_id IS NOT NULL));
DROP INDEX page_template_space_idx;
CREATE INDEX page_template_space_idx ON public.page_template USING btree (space_id);
DROP INDEX page_version_author_idx;
CREATE INDEX page_version_author_idx ON public.page_version USING btree (created_by, page_id, created_at DESC);
DROP INDEX page_version_editor_user_idx;
CREATE INDEX page_version_editor_user_idx ON public.page_version_editor USING btree (user_id, page_id);
DROP INDEX page_view_age_idx;
CREATE INDEX page_view_age_idx ON public.page_view USING btree (day);
DROP INDEX page_view_day_idx;
CREATE INDEX page_view_day_idx ON public.page_view USING btree (page_id, day) INCLUDE (user_id);
DROP INDEX page_visit_latest_idx;
CREATE INDEX page_visit_latest_idx ON public.page_visit USING btree (page_id, visited_at DESC);
DROP INDEX page_visit_recent_idx;
CREATE INDEX page_visit_recent_idx ON public.page_visit USING btree (user_id, visited_at DESC);
DROP INDEX reaction_comment_idx;
CREATE INDEX reaction_comment_idx ON public.reaction USING btree (comment_id) WHERE (comment_id IS NOT NULL);
DROP INDEX space_export_space_idx;
CREATE INDEX space_export_space_idx ON public.space_export USING btree (space_id, requested_at DESC);
DROP INDEX space_import_latest_idx;
CREATE INDEX space_import_latest_idx ON public.space_import USING btree (requested_by, requested_at DESC);
DROP INDEX space_shortcut_order_idx;
CREATE INDEX space_shortcut_order_idx ON public.space_shortcut USING btree (space_id, rank, id);
DROP INDEX space_shortcut_page_idx;
CREATE INDEX space_shortcut_page_idx ON public.space_shortcut USING btree (page_id) WHERE (page_id IS NOT NULL);
DROP INDEX star_latest_idx;
CREATE INDEX star_latest_idx ON public.star USING btree (user_id, created_at DESC, id DESC);
DROP INDEX star_page_idx;
CREATE INDEX star_page_idx ON public.star USING btree (page_id) WHERE (page_id IS NOT NULL);
DROP INDEX star_space_idx;
CREATE INDEX star_space_idx ON public.star USING btree (space_id) WHERE (space_id IS NOT NULL);
DROP INDEX theme_mirror_idx;
CREATE INDEX theme_mirror_idx ON public.theme USING btree (owner_id) WHERE (armature_theme_id IS NOT NULL);
DROP INDEX theme_org_idx;
CREATE INDEX theme_org_idx ON public.theme USING btree (shared);
DROP INDEX watch_latest_idx;
CREATE INDEX watch_latest_idx ON public.watch USING btree (user_id, created_at DESC);
DROP INDEX watch_page_idx;
CREATE INDEX watch_page_idx ON public.watch USING btree (page_id) WHERE (page_id IS NOT NULL);
DROP INDEX watch_space_idx;
CREATE INDEX watch_space_idx ON public.watch USING btree (space_id) WHERE (space_id IS NOT NULL);
DROP INDEX word_import_parent_idx;
CREATE INDEX word_import_parent_idx ON public.word_import USING btree (parent_id);

-- The latest definition of every function that read the organization,
-- without it. org_default_grants no longer sets app.org_id: the policy it
-- satisfied for the migrations' role now admits every row.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.api_token_space_addable(token uuid, space uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM api_token t
                   WHERE t.id = token AND t.spaces_only
                     AND t.user_id = current_actor_id() AND t.created_at = now())
       AND perm_space_holds(current_actor_id(), space, 'view')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connected_count()
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE WHEN perm_is_admin(current_actor_id())
                THEN (SELECT count(*)::integer FROM armature_token)
           END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_endpoint()
 RETURNS TABLE(base_url text, org_slug text, armature_org_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT c.base_url, c.org_slug, c.armature_org_id
    FROM armature_connection c
    WHERE perm_is_member(current_actor_id())
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_links_emit(roots uuid[], below boolean, trace text)
 RETURNS integer
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
    made  integer;
BEGIN
    IF actor IS NULL OR NOT perm_is_member(actor)
       OR NOT EXISTS (SELECT 1 FROM armature_connection) THEN
        RETURN 0;
    END IF;
    WITH RECURSIVE pages (id) AS (
        SELECT p.id FROM page p WHERE p.id = ANY (roots)
        UNION
        SELECT p.id FROM page p JOIN pages ON p.parent_id = pages.id
        WHERE below
    )
    INSERT INTO outbox_event (org_id, topic, payload, trace_parent)
    SELECT p.org_id, 'armature.links', jsonb_build_object('pageId', p.id, 'actorId', actor), NULLIF(trace, '')
    FROM pages JOIN page p ON p.id = pages.id
    WHERE armature_names_issue(p.body)
       OR EXISTS (SELECT 1 FROM armature_remote_link l WHERE l.page_id = p.id);
    GET DIAGNOSTICS made = ROW_COUNT;
    RETURN made;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_links_emit_space(target uuid, trashed boolean, trace text)
 RETURNS integer
 LANGUAGE sql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT armature_links_emit(ARRAY(
        SELECT id FROM page
        WHERE space_id = target AND (NOT trashed OR trash_id IS NOT NULL)), false, trace)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_search_hits(q tsquery)
 RETURNS SETOF uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH hit AS MATERIALIZED (
        SELECT a.id, a.page_id, p.space_id FROM attachment a
        JOIN page p ON p.org_id = a.org_id AND p.id = a.page_id
        WHERE a.search_vector @@ q
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.audit_log_prune(cutoff timestamp with time zone)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF cutoff > now() - interval '1 day' THEN
        RAISE EXCEPTION 'the audit log keeps every entry for at least a day' USING ERRCODE = '22023';
    END IF;
    DELETE FROM audit_log WHERE created_at < cutoff;
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.calendar_writable(space uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_space_holds(current_actor_id(), space, 'addPages')
       AND EXISTS (SELECT 1 FROM space s WHERE s.id = space AND s.archived_at IS NULL)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.comment_search_hits(q tsquery)
 RETURNS SETOF uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH hit AS MATERIALIZED (
        SELECT c.id, c.page_id, p.space_id FROM comment c
        JOIN page p ON p.org_id = c.org_id AND p.id = c.page_id
        WHERE c.search_vector @@ q AND NOT current_anonymous()
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.home_edited(after_at timestamp with time zone, after_id uuid, max integer)
 RETURNS TABLE(page_id uuid, title text, space_key text, space_name text, edited_at timestamp with time zone, draft boolean, unpublished boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH touched (page_id, at, draft) AS (
        SELECT v.page_id, max(v.created_at), false FROM page_version v
        WHERE v.created_by = current_actor_id()
        GROUP BY v.page_id
        UNION ALL
        SELECT d.page_id, d.updated_at, true FROM page_draft d
        WHERE d.user_id = current_actor_id()
        UNION ALL
        SELECT p.id, p.updated_at, true FROM page p
        WHERE p.created_by = current_actor_id() AND p.version = 0
    ), latest AS (
        SELECT t.page_id, max(t.at) AS at, bool_or(t.draft) AS draft FROM touched t GROUP BY t.page_id
    ), picked AS (
        SELECT l.page_id, l.at, l.draft, p.title, p.space_id, p.version
        FROM latest l
        JOIN page p ON p.id = l.page_id
        JOIN space s ON s.id = p.space_id
        WHERE p.trashed_at IS NULL AND p.archived_at IS NULL AND s.archived_at IS NULL
          AND (after_at IS NULL OR (l.at, l.page_id) < (after_at, after_id))
          AND perm_page_viewable(p.id, current_actor_id())
        ORDER BY l.at DESC, l.page_id DESC
        LIMIT max
    )
    SELECT k.page_id, k.title, s.key, s.name, k.at, k.draft, k.version = 0
    FROM picked k JOIN space s ON s.id = k.space_id
    ORDER BY k.at DESC, k.page_id DESC
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.home_updates(watched boolean, after_at timestamp with time zone, after_id uuid, max integer)
 RETURNS TABLE(page_id uuid, title text, space_key text, space_name text, version integer, published_at timestamp with time zone, author_id uuid, author_name text, comment text)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH viewable_spaces AS MATERIALIZED (
        SELECT s.id FROM space s
        WHERE s.archived_at IS NULL AND perm_space_holds(current_actor_id(), s.id, 'view')
    ), watched_spaces AS MATERIALIZED (
        SELECT w.space_id AS id FROM watch w
        WHERE watched AND w.user_id = current_actor_id() AND w.kind = 'space'
    ), watched_pages AS MATERIALIZED (
        WITH RECURSIVE below (id) AS (
            SELECT w.page_id FROM watch w
            WHERE watched AND w.user_id = current_actor_id() AND w.kind = 'subtree'
            UNION
            SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
        )
        SELECT id FROM below
        UNION
        SELECT w.page_id FROM watch w
        WHERE watched AND w.user_id = current_actor_id() AND w.kind = 'page'
    ), picked AS (
        SELECT p.id, p.title, p.space_id, p.version, p.published_at, p.updated_by
        FROM page p
        WHERE p.published_at IS NOT NULL AND p.trashed_at IS NULL AND p.archived_at IS NULL
          AND (after_at IS NULL OR (p.published_at, p.id) < (after_at, after_id))
          AND p.updated_by IS DISTINCT FROM current_actor_id()
          -- A CASE fixes the order; as plain conditions the planner joined the
          -- spaces after the view rule and ran it on every hidden page it met.
          AND CASE WHEN p.space_id IN (SELECT id FROM viewable_spaces)
                        AND (NOT watched OR p.space_id IN (SELECT id FROM watched_spaces)
                             OR p.id IN (SELECT id FROM watched_pages))
                   THEN perm_page_viewable(p.id, current_actor_id())
                   ELSE false END
        ORDER BY p.published_at DESC, p.id DESC
        LIMIT max
    )
    SELECT k.id, k.title, s.key, s.name, k.version, k.published_at, k.updated_by, COALESCE(u.name, ''), COALESCE(v.comment, '')
    FROM picked k
    JOIN space s ON s.id = k.space_id
    LEFT JOIN app_user u ON u.id = k.updated_by
    LEFT JOIN page_version v ON v.page_id = k.id AND v.number = k.version
    ORDER BY k.published_at DESC, k.id DESC
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_default_grants()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    INSERT INTO global_grant (org_id, permission, subject_type) VALUES (NEW.id, 'use', 'everyone');
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_has_spaces()
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.outbox_share_is_fresh(payload jsonb)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE WHEN jsonb_typeof(payload -> 'shareId') = 'string'
                 AND payload ->> 'shareId' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
           THEN EXISTS (
               SELECT 1 FROM page_share s
               WHERE s.id = (payload ->> 'shareId')::uuid
                 AND s.sharer_id = current_actor_id()
                 AND s.page_id::text = payload ->> 'pageId'
                 AND s.created_at = now())
           ELSE false END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_anchor_added(target uuid, before jsonb, after jsonb, actor uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 STABLE
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    added text[] := ARRAY(SELECT unnest(document_anchor_ids(after)) EXCEPT SELECT unnest(document_anchor_ids(before)));
BEGIN
    IF cardinality(added) <> 1 OR added[1] !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        RETURN false;
    END IF;
    RETURN perm_page_commentable(target, actor)
       AND EXISTS (
           SELECT 1 FROM comment_thread t
           WHERE t.id = added[1]::uuid AND t.page_id = target
             AND t.kind = 'inline' AND t.created_by = actor AND t.detached_at IS NULL)
       AND document_without_anchors(after, added[1]) = document_without_anchors(before, added[1]);
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_archive(root uuid)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
    marked bigint;
BEGIN
    IF NOT perm_page_archivable(root, actor) THEN
        RAISE EXCEPTION 'you may not archive that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page WHERE id = root AND archived_at IS NOT NULL) THEN
        RETURN 0;
    END IF;
    WITH RECURSIVE below (id) AS (
        SELECT root
        UNION ALL
        SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
        WHERE p.trashed_at IS NULL AND p.archived_at IS NULL
    )
    UPDATE page SET archived_at = now(), archived_by = actor, archive_id = root
    WHERE id IN (SELECT id FROM below);
    GET DIAGNOSTICS marked = ROW_COUNT;
    RETURN marked;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_archived(target uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
        WHERE p.id = target
          AND (p.archived_at IS NOT NULL OR s.archived_at IS NOT NULL))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_current_version(target uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT p.version FROM page p WHERE p.id = target
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_edit_blocked(target uuid, edit_users uuid[], edit_groups uuid[], grant_users uuid[], grant_groups uuid[])
 RETURNS TABLE(user_id uuid, group_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH sp AS (SELECT p.space_id FROM page p WHERE p.id = target),
         above AS (SELECT c.id FROM perm_page_chain(target) c WHERE c.id <> target),
         named (user_id, group_id) AS (
             SELECT u, NULL::uuid FROM unnest(edit_users) AS u
             UNION
             SELECT gm.user_id, gm.group_id FROM group_member gm
             WHERE gm.group_id = ANY (edit_groups))
    SELECT n.user_id, n.group_id FROM named n
    WHERE perm_page_editable(target, current_actor_id())
      AND NOT perm_space_holds(n.user_id, (SELECT space_id FROM sp), 'addPages')
      AND NOT n.user_id = ANY (COALESCE(grant_users, '{}'))
      AND NOT EXISTS (SELECT 1 FROM group_member gm
                      WHERE gm.user_id = n.user_id AND gm.group_id = ANY (COALESCE(grant_groups, '{}')))
      AND NOT perm_grants_name(n.user_id, ARRAY(SELECT id FROM above))
    ORDER BY n.group_id NULLS FIRST, n.user_id
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_link_closed(target uuid)
 RETURNS text
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(target))
    SELECT CASE
        WHEN NOT EXISTS (SELECT 1 FROM page p WHERE p.id = target) THEN 'missing'
        WHEN EXISTS (SELECT 1 FROM page p JOIN space s ON s.org_id = p.org_id AND s.id = p.space_id
                     WHERE p.id = target AND s.owner_id IS NOT NULL) THEN 'personal'
        WHEN EXISTS (SELECT 1 FROM page p WHERE p.id = target AND p.kind = 'folder') THEN 'folder'
        WHEN EXISTS (SELECT 1 FROM page p WHERE p.id = target AND p.trashed_at IS NOT NULL) THEN 'trashed'
        WHEN EXISTS (SELECT 1 FROM chain WHERE version = 0) THEN 'unpublished'
        WHEN EXISTS (SELECT 1 FROM page_restriction r
                     WHERE r.kind = 'view' AND r.page_id IN (SELECT id FROM chain)) THEN 'restricted'
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_link_refusal(target uuid, actor uuid)
 RETURNS text
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE
        WHEN NOT perm_page_links_manageable(target, actor) THEN 'cannotManage'
        WHEN NOT EXISTS (SELECT 1 FROM org WHERE public_links) THEN 'off'
        ELSE page_link_closed(target)
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_live_within(within_space uuid, top uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH RECURSIVE below (id) AS (
        SELECT p.id FROM page p WHERE top IS NOT NULL AND p.id = top
        UNION ALL
        SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
    )
    SELECT EXISTS (
        SELECT 1 FROM page p
        WHERE p.mode = 'live' AND p.trashed_at IS NULL
          AND ((top IS NULL AND p.space_id = within_space) OR p.id IN (SELECT id FROM below)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_open_to_members(page_id uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id))
    SELECT EXISTS (SELECT 1 FROM chain)
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN chain c ON c.id = r.page_id
                       WHERE r.kind = 'view')
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = (SELECT space_id FROM chain LIMIT 1)
                     AND g.subject_type = 'everyone')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_outline_rows(root uuid)
 RETURNS TABLE(id uuid, parent_id uuid, title text, depth integer, path text)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH RECURSIVE judged AS MATERIALIZED (
        SELECT p.id, p.parent_id, p.title, p.rank, a.actor,
               perm_space_holds(a.actor, p.space_id, 'view') AS holds,
               perm_space_holds(a.actor, p.space_id, 'administer') AS administers,
               CASE WHEN a.actor IS NULL THEN perm_link_page() END AS link
        FROM page p CROSS JOIN (SELECT current_actor_id() AS actor) a
        WHERE p.id = root AND perm_page_viewable(root, a.actor)
    ), tree (id, parent_id, title, depth, path) AS (
        SELECT j.id, j.parent_id, j.title, 0, j.rank || chr(2) || j.id::text || chr(1)
        FROM judged j
        UNION ALL
        SELECT p.id, p.parent_id, p.title, t.depth + 1, t.path || p.rank || chr(2) || p.id::text || chr(1)
        FROM tree t
        JOIN page p ON p.parent_id = t.id
        CROSS JOIN judged j
        WHERE p.trashed_at IS NULL AND p.archived_at IS NULL
          AND NOT (p.version = 0 AND p.created_by IS DISTINCT FROM j.actor)
          AND (j.actor IS NOT NULL OR p.version <> 0)
          AND (j.holds OR j.link IS NOT DISTINCT FROM p.id)
          AND (j.administers
               OR NOT EXISTS (SELECT 1 FROM page_restriction r
                              WHERE r.page_id = p.id AND r.kind = 'view')
               OR perm_lists_pass(j.actor, ARRAY[p.id], 'view'))
    )
    SELECT id, parent_id, title, depth, path FROM tree
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_pending_drafts(page uuid)
 RETURNS TABLE(user_id uuid, name text)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT d.user_id, COALESCE(u.name, '')
    FROM page_draft d
    JOIN page p ON p.org_id = d.org_id AND p.id = d.page_id
    LEFT JOIN app_user u ON u.id = d.user_id
    WHERE d.page_id = page_pending_drafts.page
      AND (d.title, d.body) IS DISTINCT FROM (p.title, p.body)
      AND perm_page_editable(page_pending_drafts.page, current_actor_id())
    ORDER BY lower(COALESCE(u.name, '')), d.user_id;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_place(ids uuid[], parent uuid, ranks text[])
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
    sp uuid := (SELECT space_id FROM page WHERE id = parent);
BEGIN
    IF sp IS NULL OR NOT (perm_space_holds(actor, sp, 'administer') OR (
        perm_page_arrangeable(parent, actor) AND NOT EXISTS (
            SELECT 1 FROM page p WHERE p.id = ANY (ids)
              AND NOT perm_page_arrangeable(p.parent_id, actor)))) THEN
        RAISE EXCEPTION 'you may not rearrange the pages there' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
               WHERE p.id = ANY (ids) AND p.parent_id IS DISTINCT FROM parent
                 AND (page_archived(parent) OR s.archived_at IS NOT NULL OR p.archive_id <> p.id)) THEN
        RAISE EXCEPTION 'archived pages stay where they are' USING ERRCODE = 'insufficient_privilege';
    END IF;
    UPDATE page p SET parent_id = parent, rank = u.rank
    FROM unnest(ids, ranks) AS u (id, rank)
    WHERE p.id = u.id;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_purge(item uuid)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM page WHERE id = item AND trash_id = item
                   AND perm_space_holds(current_actor_id(), space_id, 'administer')) THEN
        RAISE EXCEPTION 'you may not delete that page for good' USING ERRCODE = 'insufficient_privilege';
    END IF;
    DELETE FROM page WHERE trash_id = item;
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_read_confirmable(target uuid, confirmed integer)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page_read_ask a WHERE a.page_id = target AND a.since <= confirmed)
       AND confirmed = page_current_version(target)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_readers(in_page uuid, after_at timestamp with time zone, after_id uuid, max_rows integer)
 RETURNS TABLE(user_id uuid, name text, avatar_url text, viewed_at timestamp with time zone, days bigint)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH allowed AS MATERIALIZED (
        SELECT perm_page_readers_listable(in_page, current_actor_id()) AS ok
    )
    SELECT r.user_id, COALESCE(NULLIF(u.name, ''), u.email::text, ''), COALESCE(u.avatar_url, ''), r.visited_at, d.days
    FROM page_visit r
    JOIN app_user u ON u.id = r.user_id
    CROSS JOIN LATERAL (
        SELECT count(*) AS days FROM page_view v
        WHERE v.page_id = in_page AND v.user_id = r.user_id
    ) d
    WHERE (SELECT ok FROM allowed)
      AND r.page_id = in_page
      AND u.show_in_readers AND d.days > 0
      AND (after_at IS NULL OR (r.visited_at, r.user_id) < (after_at, after_id))
    ORDER BY r.visited_at DESC, r.user_id DESC
    LIMIT max_rows
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_readers_unnamed(in_page uuid)
 RETURNS bigint
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE WHEN perm_page_readers_listable(in_page, current_actor_id()) THEN (
        SELECT count(DISTINCT v.user_id) FROM page_view v JOIN app_user u ON u.id = v.user_id
        WHERE v.page_id = in_page AND NOT u.show_in_readers)
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_approvals(review uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT count(*)::integer FROM page_review_decision d
    WHERE d.review_id = review AND d.decision = 'approve'
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_by(review uuid, author uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page_review r WHERE r.id = review AND r.author_id = author)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_decidable(review uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM page_review r
        WHERE r.id = review AND r.state = 'pending'
          AND r.author_id IS DISTINCT FROM actor
          AND perm_page_viewable(r.page_id, actor) AND page_review_names(review, actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_names(review uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM page_review_subject s
        WHERE s.review_id = review
          AND perm_subject_matches(actor, s.subject_type, s.user_id, s.group_id))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_pending_on(target uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT r.id FROM page_review r WHERE r.page_id = target AND r.state = 'pending'
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_viewable(review uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page_review r WHERE r.id = review
                     AND perm_page_viewable(r.page_id, actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_search_rows(q tsquery)
 RETURNS TABLE(id uuid, org_id uuid, space_id uuid, parent_id uuid, title text, body jsonb, kind text, version integer, archived_at timestamp with time zone, trashed_at timestamp with time zone, changed_at timestamp with time zone, changed_by uuid, rank real)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH hit AS MATERIALIZED (
        SELECT p.id, p.org_id, p.space_id, p.parent_id, p.title, p.body, p.kind, p.version, p.archived_at, p.trashed_at,
               ts_rank(p.search_vector, q) AS rank
        FROM page p WHERE p.search_vector @@ q
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id, h.org_id, h.space_id, h.parent_id, h.title, h.body, h.kind, h.version, h.archived_at, h.trashed_at,
           v.created_at, v.created_by, h.rank
    FROM hit h
    JOIN judged j ON j.space_id = h.space_id
    JOIN page_version v ON v.page_id = h.id AND v.number = h.version
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.id, current_actor_id()) END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_share_recipient_allowed(share uuid, recipient uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM page_share s
        WHERE s.id = share
          AND s.sharer_id = current_actor_id()
          AND recipient <> current_actor_id()
          AND perm_is_member(recipient)
          AND perm_page_viewable(s.page_id, recipient))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_shareable(page_id uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = page_id
          AND p.version > 0 AND p.trashed_at IS NULL)
       AND perm_page_viewable(page_id, actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_sibling_ranks(parent uuid, skip uuid)
 RETURNS TABLE(id uuid, rank text)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT p.id, p.rank::text FROM page p
    WHERE p.parent_id = parent AND p.id IS DISTINCT FROM skip
      AND p.trashed_at IS NULL AND perm_page_viewable(parent, current_actor_id())
    ORDER BY p.rank, p.id
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_stewardable(page uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page p WHERE p.id = page
                     AND p.trashed_at IS NULL AND p.version > 0)
       AND perm_page_editable(page, actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_trash(root uuid)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF NOT perm_page_deletable(root, actor) THEN
        RAISE EXCEPTION 'you may not delete that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    WITH RECURSIVE below (id) AS (
        SELECT root
        UNION ALL
        SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
        WHERE p.trashed_at IS NULL
    )
    UPDATE page SET trashed_at = now(), trashed_by = actor, trash_id = root
    WHERE id IN (SELECT id FROM below);
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_unarchive(item uuid)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    cleared bigint;
BEGIN
    IF NOT perm_page_archivable(item, current_actor_id())
       OR NOT EXISTS (SELECT 1 FROM page WHERE id = item AND archive_id = item) THEN
        RAISE EXCEPTION 'you may not unarchive that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page p JOIN page up ON up.id = p.parent_id
               WHERE p.id = item AND up.archived_at IS NOT NULL) THEN
        RAISE EXCEPTION 'the page above it is archived; unarchive that one first' USING ERRCODE = 'check_violation',
            CONSTRAINT = 'page_archive_parent_first';
    END IF;
    UPDATE page SET archived_at = NULL, archived_by = NULL, archive_id = NULL
    WHERE archive_id = item;
    GET DIAGNOSTICS cleared = ROW_COUNT;
    RETURN cleared;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_untrash(item uuid, home uuid, home_rank text)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM page WHERE id = item AND trash_id = item)
       OR NOT perm_page_deletable(item, current_actor_id()) THEN
        RAISE EXCEPTION 'you may not restore that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF home IS NOT NULL THEN
        UPDATE page p SET parent_id = home, rank = home_rank
        FROM space s
        WHERE p.id = item AND s.id = p.space_id AND s.home_page_id = home;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'a restored page goes back under its own space''s home page' USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    UPDATE page SET trashed_at = NULL, trashed_by = NULL, trash_id = NULL
    WHERE trash_id = item;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_view_prune(cutoff date)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF cutoff > page_view_today() - page_view_min_days() THEN
        RAISE EXCEPTION 'page views are kept for at least % days', page_view_min_days() USING ERRCODE = '22023';
    END IF;
    WITH old AS (
        DELETE FROM page_view WHERE day < cutoff RETURNING org_id, page_id
    ), counted AS (
        SELECT org_id, page_id, count(*) AS n FROM old GROUP BY org_id, page_id
    ), added AS (
        INSERT INTO page_view_tally (org_id, page_id, views)
        SELECT org_id, page_id, n FROM counted
        ON CONFLICT (org_id, page_id) DO UPDATE SET views = page_view_tally.views + EXCLUDED.views
    )
    SELECT COALESCE(sum(n), 0) INTO gone FROM counted;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_view_stats(in_page uuid, in_days integer)
 RETURNS TABLE(views bigint, readers bigint, recent_views bigint, recent_readers bigint, can_list boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH recent AS (
        SELECT count(*) AS n_views, count(DISTINCT v.user_id) AS n_readers FROM page_view v
        WHERE v.page_id = in_page AND v.day > page_view_today() - in_days
    )
    SELECT COALESCE((SELECT t.views FROM page_view_tally t WHERE t.page_id = p.id), 0)
             + (SELECT count(*) FROM page_view v WHERE v.page_id = p.id),
           (SELECT count(*) FROM page_visit r WHERE r.page_id = p.id),
           (SELECT n_views FROM recent), (SELECT n_readers FROM recent),
           perm_page_readers_listable(p.id, current_actor_id())
    FROM page p
    WHERE p.id = in_page AND p.trashed_at IS NULL
      AND perm_page_viewable(p.id, current_actor_id())
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_watch_coverage(target uuid, with_parent boolean)
 RETURNS TABLE(user_id uuid, via text, via_page uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH RECURSIVE up (id, parent_id, space_id, kind, depth) AS (
        SELECT p.id, p.parent_id, p.space_id, p.kind, 0 FROM page p
        WHERE p.id = target
        UNION ALL
        SELECT p.id, p.parent_id, p.space_id, p.kind, up.depth + 1 FROM page p JOIN up ON p.id = up.parent_id
    ), found (user_id, via, via_page, depth) AS (
        SELECT w.user_id, w.kind, CASE WHEN w.kind = 'subtree' OR up.depth > 0 THEN w.page_id END, up.depth
        FROM watch w JOIN up ON w.page_id = up.id
        WHERE (up.depth = 0 OR w.kind = 'subtree' OR (with_parent AND up.depth = 1))
        UNION ALL
        SELECT w.user_id, 'blog', NULL, 2147483646
        FROM watch w
        WHERE with_parent AND w.kind = 'blog'
          AND w.space_id = (SELECT space_id FROM up WHERE depth = 0 AND kind = 'post')
        UNION ALL
        SELECT w.user_id, 'space', NULL, 2147483647
        FROM watch w
        WHERE w.kind = 'space'
          AND w.space_id = (SELECT space_id FROM up WHERE depth = 0)
    )
    SELECT DISTINCT ON (f.user_id) f.user_id, f.via, f.via_page
    FROM found f
    ORDER BY f.user_id, f.depth
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_comment_deletable(target uuid, author uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_page_viewable(target, actor) AND (
        author = actor OR EXISTS (
            SELECT 1 FROM page p
            WHERE p.id = target
              AND perm_space_holds(actor, p.space_id, 'administer')))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_global_grant_sources(actor uuid, permission text)
 RETURNS TABLE(subject_type text, user_id uuid, group_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT g.subject_type, g.user_id, g.group_id
    FROM global_grant g
    WHERE g.permission = perm_global_grant_sources.permission
      AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
      AND (actor = current_actor_id() OR perm_is_admin(current_actor_id()) OR EXISTS (
          SELECT 1 FROM space s
          WHERE perm_space_holds(current_actor_id(), s.id, 'administer')))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_global_grants(actor uuid)
 RETURNS text[]
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM global_grant g
    WHERE perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_grants_name(actor uuid, pages uuid[])
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM page_restriction r
        WHERE r.page_id = ANY (pages) AND r.kind = 'editGrant'
          AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_guest_space(actor uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE PARALLEL SAFE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT guest_space_id FROM org_member WHERE user_id = actor
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_is_member(actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM org_member WHERE user_id = actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_link_page()
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT l.page_id FROM page_link l JOIN org o ON o.id = l.org_id
    WHERE current_anonymous()
      AND l.token_hash = current_link_digest()
      AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now())
      AND o.public_links AND o.archived_at IS NULL
      AND page_link_closed(l.page_id) IS NULL
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_lists_pass(actor uuid, pages uuid[], kind text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT NOT EXISTS (
        SELECT 1 FROM unnest(pages) AS up (id)
        WHERE EXISTS (SELECT 1 FROM page_restriction r
                      WHERE r.page_id = up.id AND r.kind = perm_lists_pass.kind)
          AND NOT EXISTS (SELECT 1 FROM page_restriction r
                          WHERE r.page_id = up.id
                            AND (r.kind = perm_lists_pass.kind OR (perm_lists_pass.kind = 'edit' AND r.kind = 'editGrant'))
                            AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_archivable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
        WHERE p.id = target AND p.trashed_at IS NULL AND s.archived_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'administer'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_arrangeable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_page_editable(target, actor) AND EXISTS (
        SELECT 1 FROM page p WHERE p.id = target
          AND perm_space_holds(actor, p.space_id, 'addPages'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_chain(page_id uuid)
 RETURNS TABLE(id uuid, space_id uuid, version integer, created_by uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH RECURSIVE up (id, parent_id, space_id, version, created_by, depth) AS (
        SELECT p.id, p.parent_id, p.space_id, p.version, p.created_by, 0
        FROM page p WHERE p.id = perm_page_chain.page_id
        UNION ALL
        SELECT p.id, p.parent_id, p.space_id, p.version, p.created_by, up.depth + 1
        FROM page p JOIN up ON p.id = up.parent_id
    )
    SELECT id, space_id, version, created_by FROM up ORDER BY depth
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_commentable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.version > 0 AND p.trashed_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'addComments'))
       AND NOT page_archived(target)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_grantable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page p WHERE p.id = target
                     AND perm_space_holds(actor, p.space_id, 'administer'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_insertable(space uuid, parent uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE
        WHEN parent IS NULL THEN perm_space_holds(actor, space, 'administer')
        WHEN NOT EXISTS (SELECT 1 FROM page WHERE id = parent)
            THEN perm_space_holds(actor, space, 'addPages')
        ELSE perm_page_arrangeable(parent, actor)
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_lists(page_id uuid, actor uuid)
 RETURNS TABLE(id uuid, space_id uuid, hidden boolean, view_listed boolean, on_view_list boolean, edit_listed boolean, on_edit_list boolean, granted boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT c.id, c.space_id, c.version = 0 AND c.created_by IS DISTINCT FROM actor,
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.page_id = c.id AND r.kind = 'view'),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.page_id = c.id AND r.kind = 'view'
                   AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.page_id = c.id AND r.kind = 'edit'),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.page_id = c.id AND r.kind = 'edit'
                   AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)),
           perm_grants_name(actor, ARRAY[c.id])
    FROM perm_page_chain(page_id) c
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_readers_listable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH chain AS MATERIALIZED (SELECT c.id FROM perm_page_chain(target) c)
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.trashed_at IS NULL
          AND (perm_space_holds(actor, p.space_id, 'administer')
               OR ((perm_space_holds(actor, p.space_id, 'addPages') OR perm_grants_name(actor, ARRAY(SELECT id FROM chain)))
                   AND perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'edit'))))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_viewable(page_id uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
                AND EXISTS (SELECT 1 FROM page t WHERE t.id = perm_page_viewable.page_id AND t.trashed_at IS NULL)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_post_insertable(space uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_space_holds(actor, space, 'addPages')
       AND EXISTS (SELECT 1 FROM space s WHERE s.id = space AND s.archived_at IS NULL)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_role_admin(actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM org_member
                   WHERE user_id = actor AND org_role IN ('owner', 'admin'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_anonymous(space uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM org o WHERE o.anonymous_access)
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = space
                     AND g.subject_type = 'anonymous' AND g.permission = 'view')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_grants(actor uuid, space uuid)
 RETURNS text[]
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM space_grant g, (SELECT perm_guest_space(actor) AS gs) AS guest
    WHERE g.space_id = space AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
      AND (guest.gs IS NULL OR (g.subject_type = 'user' AND g.space_id = guest.gs))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_person(space uuid, person uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = space
                     AND (g.user_id = person OR (g.group_id IS NOT NULL AND EXISTS (
                         SELECT 1 FROM group_member gm WHERE gm.group_id = g.group_id AND gm.user_id = person))))
        OR EXISTS (SELECT 1 FROM page p JOIN page_version v ON v.org_id = p.org_id AND v.page_id = p.id
                   WHERE p.space_id = space AND v.created_by = person)
        OR EXISTS (SELECT 1 FROM page p JOIN comment c ON c.org_id = p.org_id AND c.page_id = p.id
                   WHERE p.space_id = space AND c.author_id = person)
        OR EXISTS (SELECT 1 FROM page p JOIN page_owner o ON o.org_id = p.org_id AND o.page_id = p.id
                   WHERE p.space_id = space AND o.user_id = person)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_plain(space uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT actor IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN page p ON p.org_id = r.org_id AND p.id = r.page_id
                       WHERE p.space_id = space AND r.kind = 'view')
       AND NOT EXISTS (SELECT 1 FROM page p
                       WHERE p.space_id = space
                         AND p.version = 0 AND p.created_by IS DISTINCT FROM actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_subject_matches(actor uuid, subject_type text, user_id uuid, group_id uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT subject_type = 'everyone'
        OR user_id = actor
        OR (group_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM group_member gm
            WHERE gm.group_id = perm_subject_matches.group_id AND gm.user_id = actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_token_reaches_page(target uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_token_whole(current_actor_id()) OR EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target
          AND perm_token_reaches(current_actor_id(), p.space_id))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.review_rule_administrable_by_id(rule uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM review_rule r WHERE r.id = rule
                     AND review_rule_administrable(r.space_id, r.page_id, actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.review_rule_space(rule_space uuid, rule_page uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(rule_space, (SELECT p.space_id FROM page p WHERE p.id = rule_page))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.review_rule_viewable_by_id(rule uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM review_rule r WHERE r.id = rule
                     AND review_rule_viewable(r.space_id, r.page_id, actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_empty_trash(target uuid)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF NOT perm_space_holds(current_actor_id(), target, 'administer') THEN
        RAISE EXCEPTION 'you may not empty that trash' USING ERRCODE = 'insufficient_privilege';
    END IF;
    DELETE FROM page WHERE space_id = target AND trash_id IS NOT NULL;
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_key_taken(wanted text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space WHERE key = upper(btrim(wanted)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_shortcut_page_ok(page uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT page IS NULL OR (
        EXISTS (SELECT 1 FROM page p WHERE p.id = page AND p.trashed_at IS NULL)
        AND perm_page_viewable(page, current_actor_id()))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.stale_pages(in_space uuid, in_owner uuid, in_unowned boolean, in_verification text, in_archived boolean, in_days integer, after_at timestamp with time zone, after_id uuid, max_rows integer)
 RETURNS TABLE(page_id uuid, title text, space_key text, space_name text, version integer, published_at timestamp with time zone, viewed_at timestamp with time zone, active_at timestamp with time zone, owner_id uuid, owner_name text, owner_can_view boolean, verification_state text, verification_expires_at timestamp with time zone, archived boolean, archivable boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH reviewed AS MATERIALIZED (
        SELECT s.id FROM space s
        WHERE (in_space IS NULL OR s.id = in_space)
          AND (in_archived OR s.archived_at IS NULL)
          AND perm_space_holds(current_actor_id(), s.id, 'administer')
    ), cut AS MATERIALIZED (
        SELECT now() - make_interval(days => in_days) AS at
    ), candidates AS (
        SELECT p.id, p.title, p.space_id, p.parent_id, p.archived_at, p.version, p.published_at, lv.at AS viewed_at,
               greatest(p.published_at, lv.at) AS active_at
        FROM page p
        LEFT JOIN LATERAL (
            SELECT max(v.visited_at) AS at FROM page_visit v
            WHERE v.page_id = p.id
        ) lv ON true
        WHERE p.published_at IS NOT NULL AND p.trashed_at IS NULL
          AND p.published_at < (SELECT at FROM cut)
          AND p.space_id IN (SELECT id FROM reviewed)
          AND (in_archived OR p.archived_at IS NULL)
          AND (lv.at IS NULL OR lv.at < (SELECT at FROM cut))
          AND (in_owner IS NULL OR EXISTS (
                SELECT 1 FROM page_owner o
                WHERE o.page_id = p.id AND o.user_id = in_owner))
          AND (NOT in_unowned OR NOT EXISTS (
                SELECT 1 FROM page_owner o WHERE o.page_id = p.id))
          AND CASE in_verification
                WHEN 'verified' THEN EXISTS (
                    SELECT 1 FROM page_verification pv
                    WHERE pv.page_id = p.id AND pv.expires_at > now())
                WHEN 'expired' THEN EXISTS (
                    SELECT 1 FROM page_verification pv
                    WHERE pv.page_id = p.id AND pv.expires_at <= now())
                WHEN 'none' THEN NOT EXISTS (
                    SELECT 1 FROM page_verification pv WHERE pv.page_id = p.id)
                ELSE true
              END
    ), picked AS (
        SELECT o.* FROM (
            SELECT c.* FROM candidates c
            WHERE after_at IS NULL OR (c.active_at, c.id) > (after_at, after_id)
            ORDER BY c.active_at, c.id
            -- OFFSET 0 keeps the view rule out of this subquery, so it runs
            -- above the sort, on as many rows as the window needs.
            OFFSET 0
        ) o
        WHERE perm_page_viewable(o.id, current_actor_id())
        LIMIT max_rows
    )
    SELECT k.id, k.title, s.key, s.name, k.version, k.published_at, k.viewed_at, k.active_at,
           ow.user_id, COALESCE(NULLIF(u.name, ''), u.email::text, ''),
           CASE WHEN ow.user_id IS NULL THEN false ELSE perm_page_viewable(k.id, ow.user_id) END,
           CASE WHEN pv.page_id IS NULL THEN 'none' WHEN pv.expires_at > now() THEN 'verified' ELSE 'expired' END,
           pv.expires_at,
           k.archived_at IS NOT NULL OR s.archived_at IS NOT NULL,
           -- What page_archive would take: never a home page, which stands
           -- for its space, nor a page already archived or in an archived space.
           k.parent_id IS NOT NULL AND k.archived_at IS NULL AND s.archived_at IS NULL
    FROM picked k
    JOIN space s ON s.id = k.space_id
    LEFT JOIN page_owner ow ON ow.page_id = k.id
    LEFT JOIN app_user u ON u.id = ow.user_id
    LEFT JOIN page_verification pv ON pv.page_id = k.id
    ORDER BY k.active_at, k.id
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.stale_reviewer()
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space s
                   WHERE perm_space_holds(current_actor_id(), s.id, 'administer'))
$function$;
-- +goose StatementEnd

-- +goose Down
DROP POLICY IF EXISTS org_app_changes ON org;

DROP INDEX api_token_org_user_idx;
CREATE INDEX api_token_org_user_idx ON public.api_token USING btree (org_id, user_id);
DROP INDEX api_token_space_space_idx;
CREATE INDEX api_token_space_space_idx ON public.api_token_space USING btree (org_id, space_id);
DROP INDEX attachment_page_idx;
CREATE INDEX attachment_page_idx ON public.attachment USING btree (org_id, page_id, created_at DESC);
DROP INDEX audit_log_org_action_idx;
CREATE INDEX audit_log_org_action_idx ON public.audit_log USING btree (org_id, action, created_at DESC, id DESC);
DROP INDEX audit_log_org_actor_idx;
CREATE INDEX audit_log_org_actor_idx ON public.audit_log USING btree (org_id, actor_user_id, created_at DESC, id DESC);
DROP INDEX audit_log_org_created_idx;
CREATE INDEX audit_log_org_created_idx ON public.audit_log USING btree (org_id, created_at DESC, id DESC);
DROP INDEX audit_log_target_idx;
CREATE INDEX audit_log_target_idx ON public.audit_log USING btree (org_id, target_id, created_at DESC, id DESC);
DROP INDEX calendar_event_range_idx;
CREATE INDEX calendar_event_range_idx ON public.calendar_event USING btree (org_id, calendar_id, starts_at);
DROP INDEX comment_page_idx;
CREATE INDEX comment_page_idx ON public.comment USING btree (org_id, page_id) WHERE (deleted_at IS NULL);
DROP INDEX comment_thread_idx;
CREATE INDEX comment_thread_idx ON public.comment USING btree (org_id, thread_id, created_at, id);
DROP INDEX comment_thread_page_idx;
CREATE INDEX comment_thread_page_idx ON public.comment_thread USING btree (org_id, page_id, id);
DROP INDEX example_job_latest_idx;
CREATE INDEX example_job_latest_idx ON public.example_job USING btree (org_id, requested_at DESC);
DROP INDEX group_member_user_idx;
CREATE INDEX group_member_user_idx ON public.group_member USING btree (org_id, user_id);
DROP INDEX notification_comment_idx;
CREATE INDEX notification_comment_idx ON public.notification USING btree (org_id, comment_id) WHERE (comment_id IS NOT NULL);
DROP INDEX notification_latest_idx;
CREATE INDEX notification_latest_idx ON public.notification USING btree (org_id, user_id, created_at DESC);
DROP INDEX notification_page_idx;
CREATE INDEX notification_page_idx ON public.notification USING btree (org_id, page_id);
DROP INDEX notification_unread_idx;
CREATE INDEX notification_unread_idx ON public.notification USING btree (org_id, user_id) WHERE (read_at IS NULL);
DROP INDEX org_export_latest_idx;
CREATE INDEX org_export_latest_idx ON public.org_export USING btree (org_id, requested_at DESC);
DROP INDEX org_import_latest_idx;
CREATE INDEX org_import_latest_idx ON public.org_import USING btree (org_id, requested_at DESC);
DROP INDEX org_member_guest_space_idx;
CREATE INDEX org_member_guest_space_idx ON public.org_member USING btree (org_id, guest_space_id) WHERE (guest_space_id IS NOT NULL);
DROP INDEX page_archive_items_idx;
CREATE INDEX page_archive_items_idx ON public.page USING btree (org_id, archive_id) WHERE (archive_id IS NOT NULL);
DROP INDEX page_archive_roots_idx;
CREATE INDEX page_archive_roots_idx ON public.page USING btree (org_id, space_id, archived_at DESC) WHERE (archive_id = id);
DROP INDEX page_children_idx;
CREATE INDEX page_children_idx ON public.page USING btree (org_id, parent_id, rank);
DROP INDEX page_posts_org_idx;
CREATE INDEX page_posts_org_idx ON public.page USING btree (org_id, posted_at DESC, id DESC) WHERE ((kind = 'post'::text) AND (posted_at IS NOT NULL));
DROP INDEX page_posts_space_idx;
CREATE INDEX page_posts_space_idx ON public.page USING btree (org_id, space_id, posted_at DESC, id DESC) WHERE ((kind = 'post'::text) AND (posted_at IS NOT NULL));
DROP INDEX page_published_idx;
CREATE INDEX page_published_idx ON public.page USING btree (org_id, published_at DESC, id DESC) WHERE ((published_at IS NOT NULL) AND (trashed_at IS NULL));
DROP INDEX page_space_idx;
CREATE INDEX page_space_idx ON public.page USING btree (org_id, space_id);
DROP INDEX page_trash_items_idx;
CREATE INDEX page_trash_items_idx ON public.page USING btree (org_id, trash_id) WHERE (trash_id IS NOT NULL);
DROP INDEX page_trash_roots_idx;
CREATE INDEX page_trash_roots_idx ON public.page USING btree (org_id, space_id, trashed_at DESC) WHERE (trash_id = id);
DROP INDEX page_unpublished_idx;
CREATE INDEX page_unpublished_idx ON public.page USING btree (org_id, created_by) WHERE (version = 0);
DROP INDEX page_draft_user_idx;
CREATE INDEX page_draft_user_idx ON public.page_draft USING btree (org_id, user_id);
DROP INDEX page_label_name_idx;
CREATE INDEX page_label_name_idx ON public.page_label USING btree (org_id, name text_pattern_ops, page_id);
DROP INDEX page_link_page_idx;
CREATE INDEX page_link_page_idx ON public.page_link USING btree (org_id, page_id) WHERE (revoked_at IS NULL);
DROP INDEX page_owner_user_idx;
CREATE INDEX page_owner_user_idx ON public.page_owner USING btree (org_id, user_id);
DROP INDEX page_restriction_page_idx;
CREATE INDEX page_restriction_page_idx ON public.page_restriction USING btree (org_id, page_id, kind);
DROP INDEX page_review_page_idx;
CREATE INDEX page_review_page_idx ON public.page_review USING btree (org_id, page_id, submitted_at DESC);
DROP INDEX page_schedule_user_idx;
CREATE INDEX page_schedule_user_idx ON public.page_schedule USING btree (org_id, user_id);
DROP INDEX page_share_page_idx;
CREATE INDEX page_share_page_idx ON public.page_share USING btree (org_id, page_id);
DROP INDEX page_share_sharer_idx;
CREATE INDEX page_share_sharer_idx ON public.page_share USING btree (org_id, sharer_id, created_at DESC);
DROP INDEX page_task_done_idx;
CREATE INDEX page_task_done_idx ON public.page_task USING btree (org_id, assignee_id, done_at DESC, id DESC) WHERE (done AND (assignee_id IS NOT NULL));
DROP INDEX page_task_open_idx;
CREATE INDEX page_task_open_idx ON public.page_task USING btree (org_id, assignee_id, COALESCE(due_on, '9999-12-31'::date), id) WHERE ((NOT done) AND (assignee_id IS NOT NULL));
DROP INDEX page_template_space_idx;
CREATE INDEX page_template_space_idx ON public.page_template USING btree (org_id, space_id);
DROP INDEX page_version_author_idx;
CREATE INDEX page_version_author_idx ON public.page_version USING btree (org_id, created_by, page_id, created_at DESC);
DROP INDEX page_version_editor_user_idx;
CREATE INDEX page_version_editor_user_idx ON public.page_version_editor USING btree (org_id, user_id, page_id);
DROP INDEX page_view_age_idx;
CREATE INDEX page_view_age_idx ON public.page_view USING btree (org_id, day);
DROP INDEX page_view_day_idx;
CREATE INDEX page_view_day_idx ON public.page_view USING btree (org_id, page_id, day) INCLUDE (user_id);
DROP INDEX page_visit_latest_idx;
CREATE INDEX page_visit_latest_idx ON public.page_visit USING btree (org_id, page_id, visited_at DESC);
DROP INDEX page_visit_recent_idx;
CREATE INDEX page_visit_recent_idx ON public.page_visit USING btree (org_id, user_id, visited_at DESC);
DROP INDEX reaction_comment_idx;
CREATE INDEX reaction_comment_idx ON public.reaction USING btree (org_id, comment_id) WHERE (comment_id IS NOT NULL);
DROP INDEX space_export_space_idx;
CREATE INDEX space_export_space_idx ON public.space_export USING btree (org_id, space_id, requested_at DESC);
DROP INDEX space_import_latest_idx;
CREATE INDEX space_import_latest_idx ON public.space_import USING btree (org_id, requested_by, requested_at DESC);
DROP INDEX space_shortcut_order_idx;
CREATE INDEX space_shortcut_order_idx ON public.space_shortcut USING btree (org_id, space_id, rank, id);
DROP INDEX space_shortcut_page_idx;
CREATE INDEX space_shortcut_page_idx ON public.space_shortcut USING btree (org_id, page_id) WHERE (page_id IS NOT NULL);
DROP INDEX star_latest_idx;
CREATE INDEX star_latest_idx ON public.star USING btree (org_id, user_id, created_at DESC, id DESC);
DROP INDEX star_page_idx;
CREATE INDEX star_page_idx ON public.star USING btree (org_id, page_id) WHERE (page_id IS NOT NULL);
DROP INDEX star_space_idx;
CREATE INDEX star_space_idx ON public.star USING btree (org_id, space_id) WHERE (space_id IS NOT NULL);
DROP INDEX theme_mirror_idx;
CREATE INDEX theme_mirror_idx ON public.theme USING btree (org_id, owner_id) WHERE (armature_theme_id IS NOT NULL);
DROP INDEX theme_org_idx;
CREATE INDEX theme_org_idx ON public.theme USING btree (org_id, shared);
DROP INDEX watch_latest_idx;
CREATE INDEX watch_latest_idx ON public.watch USING btree (org_id, user_id, created_at DESC);
DROP INDEX watch_page_idx;
CREATE INDEX watch_page_idx ON public.watch USING btree (org_id, page_id) WHERE (page_id IS NOT NULL);
DROP INDEX watch_space_idx;
CREATE INDEX watch_space_idx ON public.watch USING btree (org_id, space_id) WHERE (space_id IS NOT NULL);
DROP INDEX word_import_parent_idx;
CREATE INDEX word_import_parent_idx ON public.word_import USING btree (org_id, parent_id);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
    LANGUAGE sql STABLE SECURITY INVOKER PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.org_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE
    t record;
BEGIN
    FOR t IN
        SELECT c.relname FROM pg_attribute a
        JOIN pg_class c ON c.oid = a.attrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind = 'r' AND a.attname = 'org_id'
          AND NOT a.attisdropped AND a.attgenerated = ''
    LOOP
        EXECUTE format('ALTER TABLE %I ALTER COLUMN org_id DROP DEFAULT', t.relname);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE
    p record;
BEGIN
    FOR p IN
        SELECT tablename FROM pg_policies
        WHERE schemaname = 'public' AND policyname = tablename || '_app'
    LOOP
        EXECUTE format('DROP POLICY %I ON %I', p.tablename || '_app', p.tablename);
        IF p.tablename = 'app_user' THEN
            CREATE POLICY app_user_tenant_isolation ON app_user
                USING (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id AND m.org_id = current_org_id()))
                WITH CHECK (EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = app_user.id AND m.org_id = current_org_id()));
        ELSIF p.tablename = 'org' THEN
            CREATE POLICY org_tenant_isolation ON org
                USING (id = current_org_id()) WITH CHECK (id = current_org_id());
        ELSE
            EXECUTE format('CREATE POLICY %I ON %I USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id())',
                           p.tablename || '_tenant_isolation', p.tablename);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

ALTER POLICY example_job_renews_the_example ON example_job
    WITH CHECK ((kind = 'make' AND space_id IS NULL)
                OR (kind = 'renew' AND space_id IN (SELECT space.id FROM space WHERE space.org_id = current_org_id() AND space.example)));
ALTER POLICY page_review_subject_namers ON page_review_subject
    WITH CHECK (page_review_by(review_id, current_actor_id())
                AND page_review_pending_on((SELECT r.page_id FROM page_review r WHERE r.org_id = current_org_id() AND r.id = page_review_subject.review_id)) = review_id
                AND page_review_approvals(review_id) = 0);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.api_token_space_addable(token uuid, space uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM api_token t
                   WHERE t.org_id = current_org_id() AND t.id = token AND t.spaces_only
                     AND t.user_id = current_actor_id() AND t.created_at = now())
       AND perm_space_holds(current_actor_id(), space, 'view')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connected_count()
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE WHEN perm_is_admin(current_actor_id())
                THEN (SELECT count(*)::integer FROM armature_token WHERE org_id = current_org_id())
           END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_endpoint()
 RETURNS TABLE(base_url text, org_slug text, armature_org_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT c.base_url, c.org_slug, c.armature_org_id
    FROM armature_connection c
    WHERE c.org_id = current_org_id() AND perm_is_member(current_actor_id())
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_links_emit(roots uuid[], below boolean, trace text)
 RETURNS integer
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_links_emit_space(target uuid, trashed boolean, trace text)
 RETURNS integer
 LANGUAGE sql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT armature_links_emit(ARRAY(
        SELECT id FROM page
        WHERE org_id = current_org_id() AND space_id = target AND (NOT trashed OR trash_id IS NOT NULL)), false, trace)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_search_hits(q tsquery)
 RETURNS SETOF uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH hit AS MATERIALIZED (
        SELECT a.id, a.page_id, p.space_id FROM attachment a
        JOIN page p ON p.org_id = a.org_id AND p.id = a.page_id
        WHERE a.search_vector @@ q AND a.org_id = current_org_id()
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.audit_log_prune(cutoff timestamp with time zone)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF current_org_id() IS NULL THEN
        RAISE EXCEPTION 'audit_log_prune runs inside one organization' USING ERRCODE = '42501';
    END IF;
    IF cutoff > now() - interval '1 day' THEN
        RAISE EXCEPTION 'the audit log keeps every entry for at least a day' USING ERRCODE = '22023';
    END IF;
    DELETE FROM audit_log WHERE org_id = current_org_id() AND created_at < cutoff;
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.calendar_writable(space uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_space_holds(current_actor_id(), space, 'addPages')
       AND EXISTS (SELECT 1 FROM space s WHERE s.id = space AND s.org_id = current_org_id() AND s.archived_at IS NULL)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.comment_search_hits(q tsquery)
 RETURNS SETOF uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH hit AS MATERIALIZED (
        SELECT c.id, c.page_id, p.space_id FROM comment c
        JOIN page p ON p.org_id = c.org_id AND p.id = c.page_id
        WHERE c.search_vector @@ q AND c.org_id = current_org_id() AND NOT current_anonymous()
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.home_edited(after_at timestamp with time zone, after_id uuid, max integer)
 RETURNS TABLE(page_id uuid, title text, space_key text, space_name text, edited_at timestamp with time zone, draft boolean, unpublished boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH touched (page_id, at, draft) AS (
        SELECT v.page_id, max(v.created_at), false FROM page_version v
        WHERE v.org_id = current_org_id() AND v.created_by = current_actor_id()
        GROUP BY v.page_id
        UNION ALL
        SELECT d.page_id, d.updated_at, true FROM page_draft d
        WHERE d.org_id = current_org_id() AND d.user_id = current_actor_id()
        UNION ALL
        SELECT p.id, p.updated_at, true FROM page p
        WHERE p.org_id = current_org_id() AND p.created_by = current_actor_id() AND p.version = 0
    ), latest AS (
        SELECT t.page_id, max(t.at) AS at, bool_or(t.draft) AS draft FROM touched t GROUP BY t.page_id
    ), picked AS (
        SELECT l.page_id, l.at, l.draft, p.title, p.space_id, p.version
        FROM latest l
        JOIN page p ON p.org_id = current_org_id() AND p.id = l.page_id
        JOIN space s ON s.id = p.space_id
        WHERE p.trashed_at IS NULL AND p.archived_at IS NULL AND s.archived_at IS NULL
          AND (after_at IS NULL OR (l.at, l.page_id) < (after_at, after_id))
          AND perm_page_viewable(p.id, current_actor_id())
        ORDER BY l.at DESC, l.page_id DESC
        LIMIT max
    )
    SELECT k.page_id, k.title, s.key, s.name, k.at, k.draft, k.version = 0
    FROM picked k JOIN space s ON s.id = k.space_id
    ORDER BY k.at DESC, k.page_id DESC
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.home_updates(watched boolean, after_at timestamp with time zone, after_id uuid, max integer)
 RETURNS TABLE(page_id uuid, title text, space_key text, space_name text, version integer, published_at timestamp with time zone, author_id uuid, author_name text, comment text)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH viewable_spaces AS MATERIALIZED (
        SELECT s.id FROM space s
        WHERE s.org_id = current_org_id() AND s.archived_at IS NULL AND perm_space_holds(current_actor_id(), s.id, 'view')
    ), watched_spaces AS MATERIALIZED (
        SELECT w.space_id AS id FROM watch w
        WHERE watched AND w.org_id = current_org_id() AND w.user_id = current_actor_id() AND w.kind = 'space'
    ), watched_pages AS MATERIALIZED (
        WITH RECURSIVE below (id) AS (
            SELECT w.page_id FROM watch w
            WHERE watched AND w.org_id = current_org_id() AND w.user_id = current_actor_id() AND w.kind = 'subtree'
            UNION
            SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
            WHERE p.org_id = current_org_id()
        )
        SELECT id FROM below
        UNION
        SELECT w.page_id FROM watch w
        WHERE watched AND w.org_id = current_org_id() AND w.user_id = current_actor_id() AND w.kind = 'page'
    ), picked AS (
        SELECT p.id, p.title, p.space_id, p.version, p.published_at, p.updated_by
        FROM page p
        WHERE p.org_id = current_org_id() AND p.published_at IS NOT NULL AND p.trashed_at IS NULL AND p.archived_at IS NULL
          AND (after_at IS NULL OR (p.published_at, p.id) < (after_at, after_id))
          AND p.updated_by IS DISTINCT FROM current_actor_id()
          -- A CASE fixes the order; as plain conditions the planner joined the
          -- spaces after the view rule and ran it on every hidden page it met.
          AND CASE WHEN p.space_id IN (SELECT id FROM viewable_spaces)
                        AND (NOT watched OR p.space_id IN (SELECT id FROM watched_spaces)
                             OR p.id IN (SELECT id FROM watched_pages))
                   THEN perm_page_viewable(p.id, current_actor_id())
                   ELSE false END
        ORDER BY p.published_at DESC, p.id DESC
        LIMIT max
    )
    SELECT k.id, k.title, s.key, s.name, k.version, k.published_at, k.updated_by, COALESCE(u.name, ''), COALESCE(v.comment, '')
    FROM picked k
    JOIN space s ON s.id = k.space_id
    LEFT JOIN app_user u ON u.id = k.updated_by
    LEFT JOIN page_version v ON v.org_id = current_org_id() AND v.page_id = k.id AND v.number = k.version
    ORDER BY k.published_at DESC, k.id DESC
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_default_grants()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    before text := current_setting('app.org_id', true);
BEGIN
    PERFORM set_config('app.org_id', NEW.id::text, true);
    INSERT INTO global_grant (org_id, permission, subject_type) VALUES (NEW.id, 'use', 'everyone');
    PERFORM set_config('app.org_id', COALESCE(before, ''), true);
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_has_spaces()
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space WHERE org_id = current_org_id())
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.outbox_share_is_fresh(payload jsonb)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE WHEN jsonb_typeof(payload -> 'shareId') = 'string'
                 AND payload ->> 'shareId' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
           THEN EXISTS (
               SELECT 1 FROM page_share s
               WHERE s.id = (payload ->> 'shareId')::uuid AND s.org_id = current_org_id()
                 AND s.sharer_id = current_actor_id()
                 AND s.page_id::text = payload ->> 'pageId'
                 AND s.created_at = now())
           ELSE false END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_anchor_added(target uuid, before jsonb, after jsonb, actor uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 STABLE
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    added text[] := ARRAY(SELECT unnest(document_anchor_ids(after)) EXCEPT SELECT unnest(document_anchor_ids(before)));
BEGIN
    IF cardinality(added) <> 1 OR added[1] !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        RETURN false;
    END IF;
    RETURN perm_page_commentable(target, actor)
       AND EXISTS (
           SELECT 1 FROM comment_thread t
           WHERE t.org_id = current_org_id() AND t.id = added[1]::uuid AND t.page_id = target
             AND t.kind = 'inline' AND t.created_by = actor AND t.detached_at IS NULL)
       AND document_without_anchors(after, added[1]) = document_without_anchors(before, added[1]);
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_archive(root uuid)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
    marked bigint;
BEGIN
    IF NOT perm_page_archivable(root, actor) THEN
        RAISE EXCEPTION 'you may not archive that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page WHERE id = root AND org_id = current_org_id() AND archived_at IS NOT NULL) THEN
        RETURN 0;
    END IF;
    WITH RECURSIVE below (id) AS (
        SELECT root
        UNION ALL
        SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
        WHERE p.org_id = current_org_id() AND p.trashed_at IS NULL AND p.archived_at IS NULL
    )
    UPDATE page SET archived_at = now(), archived_by = actor, archive_id = root
    WHERE org_id = current_org_id() AND id IN (SELECT id FROM below);
    GET DIAGNOSTICS marked = ROW_COUNT;
    RETURN marked;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_archived(target uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
        WHERE p.id = target AND p.org_id = current_org_id()
          AND (p.archived_at IS NOT NULL OR s.archived_at IS NOT NULL))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_current_version(target uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT p.version FROM page p WHERE p.org_id = current_org_id() AND p.id = target
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_edit_blocked(target uuid, edit_users uuid[], edit_groups uuid[], grant_users uuid[], grant_groups uuid[])
 RETURNS TABLE(user_id uuid, group_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH sp AS (SELECT p.space_id FROM page p WHERE p.id = target AND p.org_id = current_org_id()),
         above AS (SELECT c.id FROM perm_page_chain(target) c WHERE c.id <> target),
         named (user_id, group_id) AS (
             SELECT u, NULL::uuid FROM unnest(edit_users) AS u
             UNION
             SELECT gm.user_id, gm.group_id FROM group_member gm
             WHERE gm.org_id = current_org_id() AND gm.group_id = ANY (edit_groups))
    SELECT n.user_id, n.group_id FROM named n
    WHERE perm_page_editable(target, current_actor_id())
      AND NOT perm_space_holds(n.user_id, (SELECT space_id FROM sp), 'addPages')
      AND NOT n.user_id = ANY (COALESCE(grant_users, '{}'))
      AND NOT EXISTS (SELECT 1 FROM group_member gm
                      WHERE gm.org_id = current_org_id() AND gm.user_id = n.user_id AND gm.group_id = ANY (COALESCE(grant_groups, '{}')))
      AND NOT perm_grants_name(n.user_id, ARRAY(SELECT id FROM above))
    ORDER BY n.group_id NULLS FIRST, n.user_id
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_link_closed(target uuid)
 RETURNS text
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_link_refusal(target uuid, actor uuid)
 RETURNS text
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE
        WHEN NOT perm_page_links_manageable(target, actor) THEN 'cannotManage'
        WHEN NOT EXISTS (SELECT 1 FROM org WHERE id = current_org_id() AND public_links) THEN 'off'
        ELSE page_link_closed(target)
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_live_within(within_space uuid, top uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH RECURSIVE below (id) AS (
        SELECT p.id FROM page p WHERE p.org_id = current_org_id() AND top IS NOT NULL AND p.id = top
        UNION ALL
        SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id WHERE p.org_id = current_org_id()
    )
    SELECT EXISTS (
        SELECT 1 FROM page p
        WHERE p.org_id = current_org_id() AND p.mode = 'live' AND p.trashed_at IS NULL
          AND ((top IS NULL AND p.space_id = within_space) OR p.id IN (SELECT id FROM below)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_open_to_members(page_id uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id))
    SELECT EXISTS (SELECT 1 FROM chain)
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN chain c ON c.id = r.page_id
                       WHERE r.org_id = current_org_id() AND r.kind = 'view')
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.org_id = current_org_id() AND g.space_id = (SELECT space_id FROM chain LIMIT 1)
                     AND g.subject_type = 'everyone')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_outline_rows(root uuid)
 RETURNS TABLE(id uuid, parent_id uuid, title text, depth integer, path text)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH RECURSIVE judged AS MATERIALIZED (
        SELECT p.id, p.parent_id, p.title, p.rank, a.actor,
               perm_space_holds(a.actor, p.space_id, 'view') AS holds,
               perm_space_holds(a.actor, p.space_id, 'administer') AS administers,
               CASE WHEN a.actor IS NULL THEN perm_link_page() END AS link
        FROM page p CROSS JOIN (SELECT current_actor_id() AS actor) a
        WHERE p.org_id = current_org_id() AND p.id = root AND perm_page_viewable(root, a.actor)
    ), tree (id, parent_id, title, depth, path) AS (
        SELECT j.id, j.parent_id, j.title, 0, j.rank || chr(2) || j.id::text || chr(1)
        FROM judged j
        UNION ALL
        SELECT p.id, p.parent_id, p.title, t.depth + 1, t.path || p.rank || chr(2) || p.id::text || chr(1)
        FROM tree t
        JOIN page p ON p.org_id = current_org_id() AND p.parent_id = t.id
        CROSS JOIN judged j
        WHERE p.trashed_at IS NULL AND p.archived_at IS NULL
          AND NOT (p.version = 0 AND p.created_by IS DISTINCT FROM j.actor)
          AND (j.actor IS NOT NULL OR p.version <> 0)
          AND (j.holds OR j.link IS NOT DISTINCT FROM p.id)
          AND (j.administers
               OR NOT EXISTS (SELECT 1 FROM page_restriction r
                              WHERE r.org_id = current_org_id() AND r.page_id = p.id AND r.kind = 'view')
               OR perm_lists_pass(j.actor, ARRAY[p.id], 'view'))
    )
    SELECT id, parent_id, title, depth, path FROM tree
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_pending_drafts(page uuid)
 RETURNS TABLE(user_id uuid, name text)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT d.user_id, COALESCE(u.name, '')
    FROM page_draft d
    JOIN page p ON p.org_id = d.org_id AND p.id = d.page_id
    LEFT JOIN app_user u ON u.id = d.user_id
    WHERE d.org_id = current_org_id() AND d.page_id = page_pending_drafts.page
      AND (d.title, d.body) IS DISTINCT FROM (p.title, p.body)
      AND perm_page_editable(page_pending_drafts.page, current_actor_id())
    ORDER BY lower(COALESCE(u.name, '')), d.user_id;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_place(ids uuid[], parent uuid, ranks text[])
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
    sp uuid := (SELECT space_id FROM page WHERE id = parent AND org_id = current_org_id());
BEGIN
    IF sp IS NULL OR NOT (perm_space_holds(actor, sp, 'administer') OR (
        perm_page_arrangeable(parent, actor) AND NOT EXISTS (
            SELECT 1 FROM page p WHERE p.id = ANY (ids) AND p.org_id = current_org_id()
              AND NOT perm_page_arrangeable(p.parent_id, actor)))) THEN
        RAISE EXCEPTION 'you may not rearrange the pages there' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
               WHERE p.id = ANY (ids) AND p.org_id = current_org_id() AND p.parent_id IS DISTINCT FROM parent
                 AND (page_archived(parent) OR s.archived_at IS NOT NULL OR p.archive_id <> p.id)) THEN
        RAISE EXCEPTION 'archived pages stay where they are' USING ERRCODE = 'insufficient_privilege';
    END IF;
    UPDATE page p SET parent_id = parent, rank = u.rank
    FROM unnest(ids, ranks) AS u (id, rank)
    WHERE p.id = u.id AND p.org_id = current_org_id();
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_purge(item uuid)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM page WHERE id = item AND trash_id = item AND org_id = current_org_id()
                   AND perm_space_holds(current_actor_id(), space_id, 'administer')) THEN
        RAISE EXCEPTION 'you may not delete that page for good' USING ERRCODE = 'insufficient_privilege';
    END IF;
    DELETE FROM page WHERE trash_id = item AND org_id = current_org_id();
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_read_confirmable(target uuid, confirmed integer)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page_read_ask a WHERE a.org_id = current_org_id() AND a.page_id = target AND a.since <= confirmed)
       AND confirmed = page_current_version(target)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_readers(in_page uuid, after_at timestamp with time zone, after_id uuid, max_rows integer)
 RETURNS TABLE(user_id uuid, name text, avatar_url text, viewed_at timestamp with time zone, days bigint)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH allowed AS MATERIALIZED (
        SELECT perm_page_readers_listable(in_page, current_actor_id()) AS ok
    )
    SELECT r.user_id, COALESCE(NULLIF(u.name, ''), u.email::text, ''), COALESCE(u.avatar_url, ''), r.visited_at, d.days
    FROM page_visit r
    JOIN app_user u ON u.id = r.user_id
    CROSS JOIN LATERAL (
        SELECT count(*) AS days FROM page_view v
        WHERE v.org_id = current_org_id() AND v.page_id = in_page AND v.user_id = r.user_id
    ) d
    WHERE (SELECT ok FROM allowed)
      AND r.org_id = current_org_id() AND r.page_id = in_page
      AND u.show_in_readers AND d.days > 0
      AND (after_at IS NULL OR (r.visited_at, r.user_id) < (after_at, after_id))
    ORDER BY r.visited_at DESC, r.user_id DESC
    LIMIT max_rows
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_readers_unnamed(in_page uuid)
 RETURNS bigint
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE WHEN perm_page_readers_listable(in_page, current_actor_id()) THEN (
        SELECT count(DISTINCT v.user_id) FROM page_view v JOIN app_user u ON u.id = v.user_id
        WHERE v.org_id = current_org_id() AND v.page_id = in_page AND NOT u.show_in_readers)
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_approvals(review uuid)
 RETURNS integer
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT count(*)::integer FROM page_review_decision d
    WHERE d.org_id = current_org_id() AND d.review_id = review AND d.decision = 'approve'
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_by(review uuid, author uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page_review r WHERE r.org_id = current_org_id() AND r.id = review AND r.author_id = author)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_decidable(review uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM page_review r
        WHERE r.org_id = current_org_id() AND r.id = review AND r.state = 'pending'
          AND r.author_id IS DISTINCT FROM actor
          AND perm_page_viewable(r.page_id, actor) AND page_review_names(review, actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_names(review uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM page_review_subject s
        WHERE s.org_id = current_org_id() AND s.review_id = review
          AND perm_subject_matches(actor, s.subject_type, s.user_id, s.group_id))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_pending_on(target uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT r.id FROM page_review r WHERE r.org_id = current_org_id() AND r.page_id = target AND r.state = 'pending'
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_viewable(review uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page_review r WHERE r.org_id = current_org_id() AND r.id = review
                     AND perm_page_viewable(r.page_id, actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_search_rows(q tsquery)
 RETURNS TABLE(id uuid, org_id uuid, space_id uuid, parent_id uuid, title text, body jsonb, kind text, version integer, archived_at timestamp with time zone, trashed_at timestamp with time zone, changed_at timestamp with time zone, changed_by uuid, rank real)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH hit AS MATERIALIZED (
        SELECT p.id, p.space_id, p.parent_id, p.title, p.body, p.kind, p.version, p.archived_at, p.trashed_at,
               ts_rank(p.search_vector, q) AS rank
        FROM page p WHERE p.search_vector @@ q AND p.org_id = current_org_id()
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id, current_org_id(), h.space_id, h.parent_id, h.title, h.body, h.kind, h.version, h.archived_at, h.trashed_at,
           v.created_at, v.created_by, h.rank
    FROM hit h
    JOIN judged j ON j.space_id = h.space_id
    JOIN page_version v ON v.org_id = current_org_id() AND v.page_id = h.id AND v.number = h.version
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.id, current_actor_id()) END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_share_recipient_allowed(share uuid, recipient uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM page_share s
        WHERE s.id = share AND s.org_id = current_org_id()
          AND s.sharer_id = current_actor_id()
          AND recipient <> current_actor_id()
          AND perm_is_member(recipient)
          AND perm_page_viewable(s.page_id, recipient))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_shareable(page_id uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = page_id AND p.org_id = current_org_id()
          AND p.version > 0 AND p.trashed_at IS NULL)
       AND perm_page_viewable(page_id, actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_sibling_ranks(parent uuid, skip uuid)
 RETURNS TABLE(id uuid, rank text)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT p.id, p.rank::text FROM page p
    WHERE p.org_id = current_org_id() AND p.parent_id = parent AND p.id IS DISTINCT FROM skip
      AND p.trashed_at IS NULL AND perm_page_viewable(parent, current_actor_id())
    ORDER BY p.rank, p.id
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_stewardable(page uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page p WHERE p.id = page AND p.org_id = current_org_id()
                     AND p.trashed_at IS NULL AND p.version > 0)
       AND perm_page_editable(page, actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_trash(root uuid)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF NOT perm_page_deletable(root, actor) THEN
        RAISE EXCEPTION 'you may not delete that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    WITH RECURSIVE below (id) AS (
        SELECT root
        UNION ALL
        SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id
        WHERE p.org_id = current_org_id() AND p.trashed_at IS NULL
    )
    UPDATE page SET trashed_at = now(), trashed_by = actor, trash_id = root
    WHERE org_id = current_org_id() AND id IN (SELECT id FROM below);
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_unarchive(item uuid)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    cleared bigint;
BEGIN
    IF NOT perm_page_archivable(item, current_actor_id())
       OR NOT EXISTS (SELECT 1 FROM page WHERE id = item AND archive_id = item AND org_id = current_org_id()) THEN
        RAISE EXCEPTION 'you may not unarchive that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF EXISTS (SELECT 1 FROM page p JOIN page up ON up.id = p.parent_id
               WHERE p.id = item AND up.archived_at IS NOT NULL) THEN
        RAISE EXCEPTION 'the page above it is archived; unarchive that one first' USING ERRCODE = 'check_violation',
            CONSTRAINT = 'page_archive_parent_first';
    END IF;
    UPDATE page SET archived_at = NULL, archived_by = NULL, archive_id = NULL
    WHERE archive_id = item AND org_id = current_org_id();
    GET DIAGNOSTICS cleared = ROW_COUNT;
    RETURN cleared;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_untrash(item uuid, home uuid, home_rank text)
 RETURNS void
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM page WHERE id = item AND trash_id = item AND org_id = current_org_id())
       OR NOT perm_page_deletable(item, current_actor_id()) THEN
        RAISE EXCEPTION 'you may not restore that page' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF home IS NOT NULL THEN
        UPDATE page p SET parent_id = home, rank = home_rank
        FROM space s
        WHERE p.id = item AND p.org_id = current_org_id() AND s.id = p.space_id AND s.home_page_id = home;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'a restored page goes back under its own space''s home page' USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    UPDATE page SET trashed_at = NULL, trashed_by = NULL, trash_id = NULL
    WHERE trash_id = item AND org_id = current_org_id();
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_view_prune(cutoff date)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF current_org_id() IS NULL THEN
        RAISE EXCEPTION 'page_view_prune runs inside one organization' USING ERRCODE = '42501';
    END IF;
    IF cutoff > page_view_today() - page_view_min_days() THEN
        RAISE EXCEPTION 'page views are kept for at least % days', page_view_min_days() USING ERRCODE = '22023';
    END IF;
    WITH old AS (
        DELETE FROM page_view WHERE org_id = current_org_id() AND day < cutoff RETURNING page_id
    ), counted AS (
        SELECT page_id, count(*) AS n FROM old GROUP BY page_id
    ), added AS (
        INSERT INTO page_view_tally (org_id, page_id, views)
        SELECT current_org_id(), page_id, n FROM counted
        ON CONFLICT (org_id, page_id) DO UPDATE SET views = page_view_tally.views + EXCLUDED.views
    )
    SELECT COALESCE(sum(n), 0) INTO gone FROM counted;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_view_stats(in_page uuid, in_days integer)
 RETURNS TABLE(views bigint, readers bigint, recent_views bigint, recent_readers bigint, can_list boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH recent AS (
        SELECT count(*) AS n_views, count(DISTINCT v.user_id) AS n_readers FROM page_view v
        WHERE v.org_id = current_org_id() AND v.page_id = in_page AND v.day > page_view_today() - in_days
    )
    SELECT COALESCE((SELECT t.views FROM page_view_tally t WHERE t.org_id = current_org_id() AND t.page_id = p.id), 0)
             + (SELECT count(*) FROM page_view v WHERE v.org_id = current_org_id() AND v.page_id = p.id),
           (SELECT count(*) FROM page_visit r WHERE r.org_id = current_org_id() AND r.page_id = p.id),
           (SELECT n_views FROM recent), (SELECT n_readers FROM recent),
           perm_page_readers_listable(p.id, current_actor_id())
    FROM page p
    WHERE p.org_id = current_org_id() AND p.id = in_page AND p.trashed_at IS NULL
      AND perm_page_viewable(p.id, current_actor_id())
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_watch_coverage(target uuid, with_parent boolean)
 RETURNS TABLE(user_id uuid, via text, via_page uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH RECURSIVE up (id, parent_id, space_id, kind, depth) AS (
        SELECT p.id, p.parent_id, p.space_id, p.kind, 0 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id()
        UNION ALL
        SELECT p.id, p.parent_id, p.space_id, p.kind, up.depth + 1 FROM page p JOIN up ON p.id = up.parent_id
        WHERE p.org_id = current_org_id()
    ), found (user_id, via, via_page, depth) AS (
        SELECT w.user_id, w.kind, CASE WHEN w.kind = 'subtree' OR up.depth > 0 THEN w.page_id END, up.depth
        FROM watch w JOIN up ON w.page_id = up.id
        WHERE w.org_id = current_org_id()
          AND (up.depth = 0 OR w.kind = 'subtree' OR (with_parent AND up.depth = 1))
        UNION ALL
        SELECT w.user_id, 'blog', NULL, 2147483646
        FROM watch w
        WHERE with_parent AND w.org_id = current_org_id() AND w.kind = 'blog'
          AND w.space_id = (SELECT space_id FROM up WHERE depth = 0 AND kind = 'post')
        UNION ALL
        SELECT w.user_id, 'space', NULL, 2147483647
        FROM watch w
        WHERE w.org_id = current_org_id() AND w.kind = 'space'
          AND w.space_id = (SELECT space_id FROM up WHERE depth = 0)
    )
    SELECT DISTINCT ON (f.user_id) f.user_id, f.via, f.via_page
    FROM found f
    ORDER BY f.user_id, f.depth
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_comment_deletable(target uuid, author uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_page_viewable(target, actor) AND (
        author = actor OR EXISTS (
            SELECT 1 FROM page p
            WHERE p.id = target AND p.org_id = current_org_id()
              AND perm_space_holds(actor, p.space_id, 'administer')))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_global_grant_sources(actor uuid, permission text)
 RETURNS TABLE(subject_type text, user_id uuid, group_id uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT g.subject_type, g.user_id, g.group_id
    FROM global_grant g
    WHERE g.org_id = current_org_id() AND g.permission = perm_global_grant_sources.permission
      AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
      AND (actor = current_actor_id() OR perm_is_admin(current_actor_id()) OR EXISTS (
          SELECT 1 FROM space s
          WHERE s.org_id = current_org_id() AND perm_space_holds(current_actor_id(), s.id, 'administer')))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_global_grants(actor uuid)
 RETURNS text[]
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM global_grant g
    WHERE g.org_id = current_org_id() AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_grants_name(actor uuid, pages uuid[])
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM page_restriction r
        WHERE r.org_id = current_org_id() AND r.page_id = ANY (pages) AND r.kind = 'editGrant'
          AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_guest_space(actor uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE PARALLEL SAFE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT guest_space_id FROM org_member WHERE org_id = current_org_id() AND user_id = actor
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_is_member(actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM org_member WHERE org_id = current_org_id() AND user_id = actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_link_page()
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT l.page_id FROM page_link l JOIN org o ON o.id = l.org_id
    WHERE current_anonymous() AND l.org_id = current_org_id()
      AND l.token_hash = current_link_digest()
      AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now())
      AND o.public_links AND o.archived_at IS NULL
      AND page_link_closed(l.page_id) IS NULL
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_lists_pass(actor uuid, pages uuid[], kind text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT NOT EXISTS (
        SELECT 1 FROM unnest(pages) AS up (id)
        WHERE EXISTS (SELECT 1 FROM page_restriction r
                      WHERE r.org_id = current_org_id() AND r.page_id = up.id AND r.kind = perm_lists_pass.kind)
          AND NOT EXISTS (SELECT 1 FROM page_restriction r
                          WHERE r.org_id = current_org_id() AND r.page_id = up.id
                            AND (r.kind = perm_lists_pass.kind OR (perm_lists_pass.kind = 'edit' AND r.kind = 'editGrant'))
                            AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_archivable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
        WHERE p.id = target AND p.org_id = current_org_id() AND p.trashed_at IS NULL AND s.archived_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'administer'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_arrangeable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_page_editable(target, actor) AND EXISTS (
        SELECT 1 FROM page p WHERE p.id = target AND p.org_id = current_org_id()
          AND perm_space_holds(actor, p.space_id, 'addPages'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_chain(page_id uuid)
 RETURNS TABLE(id uuid, space_id uuid, version integer, created_by uuid)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH RECURSIVE up (id, parent_id, space_id, version, created_by, depth) AS (
        SELECT p.id, p.parent_id, p.space_id, p.version, p.created_by, 0
        FROM page p WHERE p.id = perm_page_chain.page_id AND p.org_id = current_org_id()
        UNION ALL
        SELECT p.id, p.parent_id, p.space_id, p.version, p.created_by, up.depth + 1
        FROM page p JOIN up ON p.id = up.parent_id
        WHERE p.org_id = current_org_id()
    )
    SELECT id, space_id, version, created_by FROM up ORDER BY depth
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_commentable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id() AND p.version > 0 AND p.trashed_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'addComments'))
       AND NOT page_archived(target)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_grantable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM page p WHERE p.id = target AND p.org_id = current_org_id()
                     AND perm_space_holds(actor, p.space_id, 'administer'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_insertable(space uuid, parent uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE
        WHEN parent IS NULL THEN perm_space_holds(actor, space, 'administer')
        WHEN NOT EXISTS (SELECT 1 FROM page WHERE id = parent AND org_id = current_org_id())
            THEN perm_space_holds(actor, space, 'addPages')
        ELSE perm_page_arrangeable(parent, actor)
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_lists(page_id uuid, actor uuid)
 RETURNS TABLE(id uuid, space_id uuid, hidden boolean, view_listed boolean, on_view_list boolean, edit_listed boolean, on_edit_list boolean, granted boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT c.id, c.space_id, c.version = 0 AND c.created_by IS DISTINCT FROM actor,
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'view'),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'view'
                   AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'edit'),
           EXISTS (SELECT 1 FROM page_restriction r WHERE r.org_id = current_org_id() AND r.page_id = c.id AND r.kind = 'edit'
                   AND perm_subject_matches(actor, r.subject_type, r.user_id, r.group_id)),
           perm_grants_name(actor, ARRAY[c.id])
    FROM perm_page_chain(page_id) c
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_readers_listable(target uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH chain AS MATERIALIZED (SELECT c.id FROM perm_page_chain(target) c)
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id() AND p.trashed_at IS NULL
          AND (perm_space_holds(actor, p.space_id, 'administer')
               OR ((perm_space_holds(actor, p.space_id, 'addPages') OR perm_grants_name(actor, ARRAY(SELECT id FROM chain)))
                   AND perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'edit'))))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_page_viewable(page_id uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_post_insertable(space uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_space_holds(actor, space, 'addPages')
       AND EXISTS (SELECT 1 FROM space s WHERE s.id = space AND s.org_id = current_org_id() AND s.archived_at IS NULL)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_role_admin(actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM org_member
                   WHERE org_id = current_org_id() AND user_id = actor AND org_role IN ('owner', 'admin'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_anonymous(space uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM org o WHERE o.id = current_org_id() AND o.anonymous_access)
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.org_id = current_org_id() AND g.space_id = space
                     AND g.subject_type = 'anonymous' AND g.permission = 'view')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_grants(actor uuid, space uuid)
 RETURNS text[]
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(array_agg(DISTINCT g.permission ORDER BY g.permission), '{}')
    FROM space_grant g, (SELECT perm_guest_space(actor) AS gs) AS guest
    WHERE g.org_id = current_org_id() AND g.space_id = space AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
      AND (guest.gs IS NULL OR (g.subject_type = 'user' AND g.space_id = guest.gs))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_person(space uuid, person uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.org_id = current_org_id() AND g.space_id = space
                     AND (g.user_id = person OR (g.group_id IS NOT NULL AND EXISTS (
                         SELECT 1 FROM group_member gm WHERE gm.org_id = current_org_id() AND gm.group_id = g.group_id AND gm.user_id = person))))
        OR EXISTS (SELECT 1 FROM page p JOIN page_version v ON v.org_id = p.org_id AND v.page_id = p.id
                   WHERE p.org_id = current_org_id() AND p.space_id = space AND v.created_by = person)
        OR EXISTS (SELECT 1 FROM page p JOIN comment c ON c.org_id = p.org_id AND c.page_id = p.id
                   WHERE p.org_id = current_org_id() AND p.space_id = space AND c.author_id = person)
        OR EXISTS (SELECT 1 FROM page p JOIN page_owner o ON o.org_id = p.org_id AND o.page_id = p.id
                   WHERE p.org_id = current_org_id() AND p.space_id = space AND o.user_id = person)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_plain(space uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT actor IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN page p ON p.org_id = r.org_id AND p.id = r.page_id
                       WHERE r.org_id = current_org_id() AND p.space_id = space AND r.kind = 'view')
       AND NOT EXISTS (SELECT 1 FROM page p
                       WHERE p.org_id = current_org_id() AND p.space_id = space
                         AND p.version = 0 AND p.created_by IS DISTINCT FROM actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_subject_matches(actor uuid, subject_type text, user_id uuid, group_id uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT subject_type = 'everyone'
        OR user_id = actor
        OR (group_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM group_member gm
            WHERE gm.org_id = current_org_id() AND gm.group_id = perm_subject_matches.group_id AND gm.user_id = actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_token_reaches_page(target uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT perm_token_whole(current_actor_id()) OR EXISTS (
        SELECT 1 FROM page p
        WHERE p.org_id = current_org_id() AND p.id = target
          AND perm_token_reaches(current_actor_id(), p.space_id))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.review_rule_administrable_by_id(rule uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM review_rule r WHERE r.org_id = current_org_id() AND r.id = rule
                     AND review_rule_administrable(r.space_id, r.page_id, actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.review_rule_space(rule_space uuid, rule_page uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT COALESCE(rule_space, (SELECT p.space_id FROM page p WHERE p.org_id = current_org_id() AND p.id = rule_page))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.review_rule_viewable_by_id(rule uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM review_rule r WHERE r.org_id = current_org_id() AND r.id = rule
                     AND review_rule_viewable(r.space_id, r.page_id, actor))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_empty_trash(target uuid)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF NOT perm_space_holds(current_actor_id(), target, 'administer') THEN
        RAISE EXCEPTION 'you may not empty that trash' USING ERRCODE = 'insufficient_privilege';
    END IF;
    DELETE FROM page WHERE space_id = target AND trash_id IS NOT NULL AND org_id = current_org_id();
    GET DIAGNOSTICS gone = ROW_COUNT;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_key_taken(wanted text)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space WHERE org_id = current_org_id() AND key = upper(btrim(wanted)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_shortcut_page_ok(page uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT page IS NULL OR (
        EXISTS (SELECT 1 FROM page p WHERE p.id = page AND p.org_id = current_org_id() AND p.trashed_at IS NULL)
        AND perm_page_viewable(page, current_actor_id()))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.stale_pages(in_space uuid, in_owner uuid, in_unowned boolean, in_verification text, in_archived boolean, in_days integer, after_at timestamp with time zone, after_id uuid, max_rows integer)
 RETURNS TABLE(page_id uuid, title text, space_key text, space_name text, version integer, published_at timestamp with time zone, viewed_at timestamp with time zone, active_at timestamp with time zone, owner_id uuid, owner_name text, owner_can_view boolean, verification_state text, verification_expires_at timestamp with time zone, archived boolean, archivable boolean)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH reviewed AS MATERIALIZED (
        SELECT s.id FROM space s
        WHERE s.org_id = current_org_id() AND (in_space IS NULL OR s.id = in_space)
          AND (in_archived OR s.archived_at IS NULL)
          AND perm_space_holds(current_actor_id(), s.id, 'administer')
    ), cut AS MATERIALIZED (
        SELECT now() - make_interval(days => in_days) AS at
    ), candidates AS (
        SELECT p.id, p.title, p.space_id, p.parent_id, p.archived_at, p.version, p.published_at, lv.at AS viewed_at,
               greatest(p.published_at, lv.at) AS active_at
        FROM page p
        LEFT JOIN LATERAL (
            SELECT max(v.visited_at) AS at FROM page_visit v
            WHERE v.org_id = current_org_id() AND v.page_id = p.id
        ) lv ON true
        WHERE p.org_id = current_org_id() AND p.published_at IS NOT NULL AND p.trashed_at IS NULL
          AND p.published_at < (SELECT at FROM cut)
          AND p.space_id IN (SELECT id FROM reviewed)
          AND (in_archived OR p.archived_at IS NULL)
          AND (lv.at IS NULL OR lv.at < (SELECT at FROM cut))
          AND (in_owner IS NULL OR EXISTS (
                SELECT 1 FROM page_owner o
                WHERE o.org_id = current_org_id() AND o.page_id = p.id AND o.user_id = in_owner))
          AND (NOT in_unowned OR NOT EXISTS (
                SELECT 1 FROM page_owner o WHERE o.org_id = current_org_id() AND o.page_id = p.id))
          AND CASE in_verification
                WHEN 'verified' THEN EXISTS (
                    SELECT 1 FROM page_verification pv
                    WHERE pv.org_id = current_org_id() AND pv.page_id = p.id AND pv.expires_at > now())
                WHEN 'expired' THEN EXISTS (
                    SELECT 1 FROM page_verification pv
                    WHERE pv.org_id = current_org_id() AND pv.page_id = p.id AND pv.expires_at <= now())
                WHEN 'none' THEN NOT EXISTS (
                    SELECT 1 FROM page_verification pv WHERE pv.org_id = current_org_id() AND pv.page_id = p.id)
                ELSE true
              END
    ), picked AS (
        SELECT o.* FROM (
            SELECT c.* FROM candidates c
            WHERE after_at IS NULL OR (c.active_at, c.id) > (after_at, after_id)
            ORDER BY c.active_at, c.id
            -- OFFSET 0 keeps the view rule out of this subquery, so it runs
            -- above the sort, on as many rows as the window needs.
            OFFSET 0
        ) o
        WHERE perm_page_viewable(o.id, current_actor_id())
        LIMIT max_rows
    )
    SELECT k.id, k.title, s.key, s.name, k.version, k.published_at, k.viewed_at, k.active_at,
           ow.user_id, COALESCE(NULLIF(u.name, ''), u.email::text, ''),
           CASE WHEN ow.user_id IS NULL THEN false ELSE perm_page_viewable(k.id, ow.user_id) END,
           CASE WHEN pv.page_id IS NULL THEN 'none' WHEN pv.expires_at > now() THEN 'verified' ELSE 'expired' END,
           pv.expires_at,
           k.archived_at IS NOT NULL OR s.archived_at IS NOT NULL,
           -- What page_archive would take: never a home page, which stands
           -- for its space, nor a page already archived or in an archived space.
           k.parent_id IS NOT NULL AND k.archived_at IS NULL AND s.archived_at IS NULL
    FROM picked k
    JOIN space s ON s.org_id = current_org_id() AND s.id = k.space_id
    LEFT JOIN page_owner ow ON ow.org_id = current_org_id() AND ow.page_id = k.id
    LEFT JOIN app_user u ON u.id = ow.user_id
    LEFT JOIN page_verification pv ON pv.org_id = current_org_id() AND pv.page_id = k.id
    ORDER BY k.active_at, k.id
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.stale_reviewer()
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space s
                   WHERE s.org_id = current_org_id() AND perm_space_holds(current_actor_id(), s.id, 'administer'))
$function$;
-- +goose StatementEnd
