-- +goose Up
-- Remote links (#419): another tool's object about a page, such as an
-- Exciter requirement it specifies, put on the page by that tool with the
-- token of somebody who may edit it, as Armature's issues take Stator's
-- links. One row per address on a page, so a second put replaces the first
-- rather than repeating it. A link is its page's: it is read with the page,
-- stays with it in the trash and goes when the page is purged.
CREATE TABLE page_remote_link (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    page_id    uuid NOT NULL REFERENCES page(id) ON DELETE CASCADE,
    url        text NOT NULL,
    title      text NOT NULL,
    source     text NOT NULL,
    icon_url   text,
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- The bounds remotelink checks, so a row written any other way is held
    -- to them too: remotelink.MaxURLLength, MaxTitleLength, MaxSourceLength.
    CONSTRAINT page_remote_link_url_shape CHECK (url ~* '^https?://[^\s/?#]+' AND url !~ '\s' AND char_length(url) <= 2048),
    CONSTRAINT page_remote_link_icon_shape CHECK (icon_url IS NULL OR (icon_url ~* '^https?://[^\s/?#]+' AND icon_url !~ '\s' AND char_length(icon_url) <= 2048)),
    CONSTRAINT page_remote_link_title_shape CHECK (btrim(title) <> '' AND char_length(title) <= 255),
    CONSTRAINT page_remote_link_source_shape CHECK (btrim(source) <> '' AND char_length(source) <= 100)
);

CREATE UNIQUE INDEX page_remote_link_url_idx ON page_remote_link (page_id, url);

ALTER TABLE page_remote_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_remote_link FORCE  ROW LEVEL SECURITY;
CREATE POLICY page_remote_link_app ON page_remote_link USING (true) WITH CHECK (true);
CREATE POLICY page_remote_link_admin_bypass ON page_remote_link TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_remote_link_not_anonymous ON page_remote_link AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
-- Seen with the page, put and taken by who edits it, in their own name.
CREATE POLICY page_remote_link_readers ON page_remote_link AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY page_remote_link_putters ON page_remote_link AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (created_by = current_actor_id() AND perm_page_editable(page_id, current_actor_id()));
CREATE POLICY page_remote_link_replacers ON page_remote_link AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_page_editable(page_id, current_actor_id()))
    WITH CHECK (perm_page_editable(page_id, current_actor_id()));
CREATE POLICY page_remote_link_removers ON page_remote_link AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_page_editable(page_id, current_actor_id()));

-- A replacement changes what the tool says of its object, never which page
-- or address it is, nor who first put it.
REVOKE ALL ON page_remote_link FROM stator_app;
GRANT SELECT, INSERT, DELETE ON page_remote_link TO stator_app;
GRANT UPDATE (title, source, icon_url, updated_at) ON page_remote_link TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_remote_link TO stator_admin;

-- A link taken off a page is told to webhooks, so the tool that put it can
-- leave it off. 00290 listed every topic so far.
ALTER TABLE webhook_endpoint DROP CONSTRAINT webhook_endpoint_topics_known;
ALTER TABLE webhook_endpoint ADD CONSTRAINT webhook_endpoint_topics_known CHECK (
    topics <@ ARRAY['*', 'page.published', 'page.moved', 'page.deleted', 'comment.created', 'page.remote_link.removed']::text[]);

-- +goose Down
UPDATE webhook_endpoint SET topics = array_remove(topics, 'page.remote_link.removed');
ALTER TABLE webhook_endpoint DROP CONSTRAINT webhook_endpoint_topics_known;
ALTER TABLE webhook_endpoint ADD CONSTRAINT webhook_endpoint_topics_known CHECK (
    topics <@ ARRAY['*', 'page.published', 'page.moved', 'page.deleted', 'comment.created']::text[]);
DROP TABLE IF EXISTS page_remote_link;
