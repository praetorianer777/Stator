-- +goose Up
-- Mentions (#24) keep no table: the people a page or comment names are read
-- from its document and travel in the outbox event.

-- +goose StatementBegin
-- Whether a person may view a page once it is published: the page's view
-- rule without the unpublished rule, so a new page's author can see whom a
-- mention will reach. The mention picker reads it for each person offered.
CREATE FUNCTION perm_page_viewable_published(page_id uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(page_id)),
         sp AS (SELECT space_id FROM chain LIMIT 1)
    SELECT EXISTS (SELECT 1 FROM chain)
       AND perm_space_holds(actor, (SELECT space_id FROM sp), 'view')
       AND (perm_space_holds(actor, (SELECT space_id FROM sp), 'administer')
            OR perm_lists_pass(actor, ARRAY(SELECT id FROM chain), 'view'))
$$;

-- Whether every id an event says it mentions is a member of the
-- organization. A malformed id is no member; the CASE keeps the cast from
-- running on one.
CREATE FUNCTION outbox_mentions_members(payload jsonb) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT NOT (payload ? 'mentioned')
        OR (jsonb_typeof(payload -> 'mentioned') = 'array'
            AND NOT EXISTS (
                SELECT 1 FROM jsonb_array_elements(payload -> 'mentioned') AS m (id)
                WHERE CASE WHEN jsonb_typeof(m.id) = 'string'
                            AND m.id #>> '{}' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                           THEN NOT perm_is_member((m.id #>> '{}')::uuid)
                           ELSE true END))
$$;
-- +goose StatementEnd

-- A request tells the worker to mention only members of its organization,
-- so a forged event cannot reach somebody the organization does not hold.
CREATE POLICY outbox_event_mentions_members ON outbox_event AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (outbox_mentions_members(payload));

-- +goose Down
DROP POLICY IF EXISTS outbox_event_mentions_members ON outbox_event;
DROP FUNCTION IF EXISTS outbox_mentions_members(jsonb);
DROP FUNCTION IF EXISTS perm_page_viewable_published(uuid, uuid);
