-- +goose Up
-- What a person was told, as in Armature: one row per person per event, which
-- makes a second delivery of the same event a no-op. The row holds facts, not
-- words: the client words it, and the page's title is read as it is now. Mail
-- is a copy of a row, never the record itself.
CREATE TABLE notification (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    user_id    uuid NOT NULL,
    event_id   uuid NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('mentioned', 'replied', 'commented', 'resolved', 'published', 'created')),
    actor_id   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    page_id    uuid NOT NULL,
    -- Comments are #22's; its migration ties them to the comment table.
    thread_id  uuid,
    comment_id uuid,
    version    integer,
    excerpt    text NOT NULL DEFAULT '' CHECK (char_length(excerpt) <= 200),
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at    timestamptz,
    PRIMARY KEY (org_id, id),
    CONSTRAINT notification_once_per_event UNIQUE (event_id, user_id),
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE
);

CREATE INDEX notification_latest_idx ON notification (org_id, user_id, created_at DESC);
CREATE INDEX notification_unread_idx ON notification (org_id, user_id) WHERE read_at IS NULL;
CREATE INDEX notification_page_idx ON notification (org_id, page_id);

-- How a person wants to hear. A kind missing from a switch map is on, so a
-- kind added later reaches everybody until they say otherwise; no row at all
-- is everything on, no digest, and watching one's own pages.
CREATE TABLE notification_preference (
    org_id     uuid NOT NULL,
    user_id    uuid NOT NULL,
    in_app     jsonb NOT NULL DEFAULT '{}'::jsonb,
    email      jsonb NOT NULL DEFAULT '{}'::jsonb,
    digest     text NOT NULL DEFAULT 'off' CHECK (digest IN ('off', 'hourly', 'daily')),
    auto_watch boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id),
    CONSTRAINT notification_preference_objects CHECK (jsonb_typeof(in_app) = 'object' AND jsonb_typeof(email) = 'object'),
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE
);

-- Rows waiting to be bundled into one mail.
CREATE TABLE notification_digest (
    org_id          uuid NOT NULL,
    user_id         uuid NOT NULL,
    notification_id uuid NOT NULL,
    PRIMARY KEY (org_id, user_id, notification_id),
    FOREIGN KEY (org_id, notification_id) REFERENCES notification (org_id, id) ON DELETE CASCADE
);

ALTER TABLE notification            ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification            FORCE  ROW LEVEL SECURITY;
ALTER TABLE notification_preference ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_preference FORCE  ROW LEVEL SECURITY;
ALTER TABLE notification_digest     ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_digest     FORCE  ROW LEVEL SECURITY;

CREATE POLICY notification_tenant_isolation ON notification
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY notification_preference_tenant_isolation ON notification_preference
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY notification_digest_tenant_isolation ON notification_digest
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY notification_admin_bypass ON notification TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY notification_preference_admin_bypass ON notification_preference TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY notification_digest_admin_bypass ON notification_digest TO stator_admin USING (true) WITH CHECK (true);

-- A person reads and marks only their own rows, and only about pages they may
-- still view. The worker writes each row acting for its recipient, so a row
-- about a page the recipient may not view cannot be written at all.
CREATE POLICY notification_own_read ON notification AS RESTRICTIVE FOR SELECT TO stator_app
    USING (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY notification_own_write ON notification AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY notification_own_mark ON notification AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (user_id = current_actor_id()) WITH CHECK (user_id = current_actor_id());
CREATE POLICY notification_preference_own ON notification_preference AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id()) WITH CHECK (user_id = current_actor_id());
CREATE POLICY notification_digest_own ON notification_digest AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id()) WITH CHECK (user_id = current_actor_id());

-- Marking read is the one change a person makes to a row.
GRANT SELECT, INSERT, UPDATE, DELETE ON notification, notification_preference, notification_digest TO stator_admin;
REVOKE ALL ON notification, notification_preference, notification_digest FROM stator_app;
GRANT SELECT, INSERT ON notification TO stator_app;
GRANT UPDATE (read_at) ON notification TO stator_app;
GRANT SELECT, INSERT, UPDATE ON notification_preference TO stator_app;
GRANT SELECT, INSERT, DELETE ON notification_digest TO stator_app;

-- +goose Down
DROP TABLE IF EXISTS notification_digest;
DROP TABLE IF EXISTS notification_preference;
DROP TABLE IF EXISTS notification;
