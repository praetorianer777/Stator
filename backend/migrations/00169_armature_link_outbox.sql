-- +goose Up
-- An armature.links event names one page and the person whose change it is,
-- nothing else: the worker reads what the page names when it runs. Like every
-- event, the database lets a request write it only in its own actor's name,
-- so the sync always runs with the token of whoever caused it.
CREATE POLICY outbox_event_armature_links_shape ON outbox_event AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (topic <> 'armature.links' OR (
        jsonb_typeof(payload -> 'pageId') = 'string'
        AND payload ->> 'pageId' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        AND payload - 'pageId' - 'actorId' = '{}'::jsonb));

-- +goose Down
DROP POLICY IF EXISTS outbox_event_armature_links_shape ON outbox_event;
