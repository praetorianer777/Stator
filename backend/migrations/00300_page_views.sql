-- +goose Up
-- Page views (#97): how often a page is read and by how many people, counted
-- once per person, page and day, and named only to the page's editors.

-- +goose StatementBegin
-- The day a view is counted on. UTC, so a view does not move between days with
-- the session's time zone.
CREATE FUNCTION page_view_today() RETURNS date
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT (now() AT TIME ZONE 'UTC')::date
$$;

-- The fewest days of views the worker keeps, so the recent counts always have
-- their whole period; pageview.MinRetention is the same, which a test holds.
CREATE FUNCTION page_view_min_days() RETURNS integer
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT 30
$$;
-- +goose StatementEnd

-- Whether a person's name appears among the readers of the pages they read.
-- They are counted either way.
ALTER TABLE app_user ADD COLUMN show_in_readers boolean NOT NULL DEFAULT true;

-- +goose StatementBegin
-- app_user is open to every member of an organization the person is in, so
-- without this a colleague could name somebody who chose not to be named.
CREATE FUNCTION app_user_readers_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF current_user = 'stator_app' AND NEW.show_in_readers IS DISTINCT FROM OLD.show_in_readers
       AND NEW.id IS DISTINCT FROM current_actor_id() THEN
        RAISE EXCEPTION 'only the person themselves chooses whether their name is shown among a page''s readers'
            USING ERRCODE = '42501';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER app_user_readers_check BEFORE UPDATE ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_readers_guard();

-- One row per person, page and day they opened it, written once however often
-- the page is loaded that day. Somebody who leaves the organization leaves
-- their rows behind without a name, so the views stay counted.
CREATE TABLE page_view (
    org_id  uuid NOT NULL,
    page_id uuid NOT NULL,
    user_id uuid,
    day     date NOT NULL,
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE SET NULL (user_id)
);

CREATE UNIQUE INDEX page_view_once_idx ON page_view (org_id, page_id, user_id, day);
-- The recent counts read a page's days, and its distinct readers, off this
-- index alone.
CREATE INDEX page_view_day_idx ON page_view (org_id, page_id, day) INCLUDE (user_id);
CREATE INDEX page_view_age_idx ON page_view (org_id, day);

ALTER TABLE page_view ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_view FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_view_tenant_isolation ON page_view
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_view_admin_bypass ON page_view TO stator_admin USING (true) WITH CHECK (true);

-- A person reads and counts only their own views, of a page they may view,
-- and only today: nobody counts a view for somebody else or for another day.
CREATE POLICY page_view_own ON page_view AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id() AND day = page_view_today()
                AND perm_page_viewable(page_id, current_actor_id()));

REVOKE ALL ON page_view FROM stator_app;
GRANT SELECT, INSERT ON page_view TO stator_app;

-- The views the worker took out of page_view once they passed the retention,
-- per page and without anybody's name, so a page's total survives them.
CREATE TABLE page_view_tally (
    org_id  uuid NOT NULL,
    page_id uuid NOT NULL,
    views   bigint NOT NULL CHECK (views > 0),
    PRIMARY KEY (org_id, page_id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE
);

ALTER TABLE page_view_tally ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_view_tally FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_view_tally_tenant_isolation ON page_view_tally
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_view_tally_admin_bypass ON page_view_tally TO stator_admin USING (true) WITH CHECK (true);

REVOKE ALL ON page_view_tally FROM stator_app;

-- Every visit noted so far counts as one view on its day, so no page starts
-- with more readers than views.
INSERT INTO page_view (org_id, page_id, user_id, day)
SELECT org_id, page_id, user_id, (visited_at AT TIME ZONE 'UTC')::date FROM page_visit
ON CONFLICT DO NOTHING;

-- +goose StatementBegin
-- Seeing who read a page is for whoever may change it: viewing it, the space's
-- addPages, and every edit list passed unless administering the space. Unlike
-- perm_page_editable an archived page still answers, since nothing changes.
CREATE FUNCTION perm_page_readers_listable(target uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_page_viewable(target, actor) AND EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = target AND p.org_id = current_org_id() AND p.trashed_at IS NULL
          AND perm_space_holds(actor, p.space_id, 'addPages')
          AND (perm_space_holds(actor, p.space_id, 'administer')
               OR perm_lists_pass(actor, ARRAY(SELECT c.id FROM perm_page_chain(target) c), 'edit')))
$$;

-- A page's counts for the actor, no row when they may not view it or it is in
-- the trash. Views are everybody's, which only these functions read; they
-- answer how many, never who. Views are person days in all, the pruned ones
-- included; readers are the members who ever opened it; the recent pair is
-- the last in_days days, today included.
CREATE FUNCTION page_view_stats(in_page uuid, in_days integer)
    RETURNS TABLE (views bigint, readers bigint, recent_views bigint, recent_readers bigint, can_list boolean)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
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
$$;

-- Who read a page within the retention, for those who may list them: the
-- latest reader first, after the keyset (after_at, after_id), each with the
-- days they opened it. Somebody who chose not to be named is left out, and
-- page_readers_unnamed counts them.
CREATE FUNCTION page_readers(in_page uuid, after_at timestamptz, after_id uuid, max_rows integer)
    RETURNS TABLE (user_id uuid, name text, avatar_url text, viewed_at timestamptz, days bigint)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
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
$$;

-- How many people who read the page within the retention chose not to be
-- named, for those who may list its readers; null for anybody else.
CREATE FUNCTION page_readers_unnamed(in_page uuid) RETURNS bigint
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN perm_page_readers_listable(in_page, current_actor_id()) THEN (
        SELECT count(DISTINCT v.user_id) FROM page_view v JOIN app_user u ON u.id = v.user_id
        WHERE v.org_id = current_org_id() AND v.page_id = in_page AND NOT u.show_in_readers)
    END
$$;

-- The worker's retention, as stator_admin inside one organization: views from
-- before cutoff leave page_view and are added to their page's tally. A cutoff
-- younger than page_view_min_days() is refused whatever the caller is handed.
CREATE FUNCTION page_view_prune(cutoff date) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
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
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION perm_page_readers_listable(uuid, uuid) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION page_view_stats(uuid, integer) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION page_readers(uuid, timestamptz, uuid, integer) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION page_readers_unnamed(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION perm_page_readers_listable(uuid, uuid) TO stator_app, stator_admin;
GRANT EXECUTE ON FUNCTION page_view_stats(uuid, integer) TO stator_app, stator_admin;
GRANT EXECUTE ON FUNCTION page_readers(uuid, timestamptz, uuid, integer) TO stator_app, stator_admin;
GRANT EXECUTE ON FUNCTION page_readers_unnamed(uuid) TO stator_app, stator_admin;
-- The schema's default privileges hand every new function to both roles.
REVOKE EXECUTE ON FUNCTION page_view_prune(date) FROM PUBLIC, stator_app;
GRANT EXECUTE ON FUNCTION page_view_prune(date) TO stator_admin;

-- +goose Down
DROP FUNCTION IF EXISTS page_view_prune(date);
DROP FUNCTION IF EXISTS page_readers_unnamed(uuid);
DROP FUNCTION IF EXISTS page_readers(uuid, timestamptz, uuid, integer);
DROP FUNCTION IF EXISTS page_view_stats(uuid, integer);
DROP FUNCTION IF EXISTS perm_page_readers_listable(uuid, uuid);
DROP TABLE IF EXISTS page_view_tally;
DROP TABLE IF EXISTS page_view;
DROP TRIGGER IF EXISTS app_user_readers_check ON app_user;
DROP FUNCTION IF EXISTS app_user_readers_guard();
ALTER TABLE app_user DROP COLUMN IF EXISTS show_in_readers;
DROP FUNCTION IF EXISTS page_view_min_days();
DROP FUNCTION IF EXISTS page_view_today();
