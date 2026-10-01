-- +goose Up
-- Sharing a page (#67): a person sends a page they may read, with a note, to
-- people and groups who may read it too. A share is a record of who was sent
-- what; it grants nothing, so every check below is about viewing, never
-- about letting anybody in.
CREATE TABLE page_share (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    page_id    uuid NOT NULL,
    sharer_id  uuid NOT NULL,
    -- The note travels whole in the notification, which quotes at most 200.
    message    text NOT NULL DEFAULT '' CHECK (char_length(message) <= 200),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, sharer_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE
);

-- The people a share tells, groups already resolved to their members.
CREATE TABLE page_share_recipient (
    org_id   uuid NOT NULL,
    share_id uuid NOT NULL,
    user_id  uuid NOT NULL,
    PRIMARY KEY (org_id, share_id, user_id),
    FOREIGN KEY (org_id, share_id) REFERENCES page_share (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE
);

-- The brake reads a sharer's last hour; a purged page takes its shares along.
CREATE INDEX page_share_sharer_idx ON page_share (org_id, sharer_id, created_at DESC);
CREATE INDEX page_share_page_idx ON page_share (org_id, page_id);

-- +goose StatementBegin
-- How many pages one person may share in an hour, in one organization.
-- share.MaxPerHour is the same number, and a test holds the two together.
CREATE FUNCTION page_share_per_hour() RETURNS integer
    LANGUAGE sql IMMUTABLE
AS $$ SELECT 30 $$;

-- Whether a person may share a page: they may view it, and it is published
-- and out of the trash, so the people it names can read it too.
CREATE FUNCTION page_shareable(page_id uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM page p
        WHERE p.id = page_id AND p.org_id = current_org_id()
          AND p.version > 0 AND p.trashed_at IS NULL)
       AND perm_page_viewable(page_id, actor)
$$;

-- Whether a person may be told of a share: it is the actor's own, and the
-- person is a member, somebody else, and may view its page.
CREATE FUNCTION page_share_recipient_allowed(share uuid, recipient uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1 FROM page_share s
        WHERE s.id = share AND s.org_id = current_org_id()
          AND s.sharer_id = current_actor_id()
          AND recipient <> current_actor_id()
          AND perm_is_member(recipient)
          AND perm_page_viewable(s.page_id, recipient))
$$;

-- Whether an event about a share names one the actor made in this very
-- transaction, so a forged event cannot send an old share again.
CREATE FUNCTION outbox_share_is_fresh(payload jsonb) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE WHEN jsonb_typeof(payload -> 'shareId') = 'string'
                 AND payload ->> 'shareId' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
           THEN EXISTS (
               SELECT 1 FROM page_share s
               WHERE s.id = (payload ->> 'shareId')::uuid AND s.org_id = current_org_id()
                 AND s.sharer_id = current_actor_id()
                 AND s.page_id::text = payload ->> 'pageId'
                 AND s.created_at = now())
           ELSE false END
$$;

-- The brake, kept by the database so no path around the service shares
-- more. The lock makes two shares of one person wait for each other, so
-- each counts the other.
CREATE FUNCTION page_share_brake() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('page_share:' || NEW.org_id::text || ':' || NEW.sharer_id::text, 0));
    IF (SELECT count(*) FROM page_share
        WHERE org_id = NEW.org_id AND sharer_id = NEW.sharer_id
          AND created_at > now() - interval '1 hour') >= page_share_per_hour() THEN
        RAISE EXCEPTION 'too many pages shared in the last hour'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_share_per_hour';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_share_brake BEFORE INSERT ON page_share
    FOR EACH ROW EXECUTE FUNCTION page_share_brake();

ALTER TABLE page_share           ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_share           FORCE  ROW LEVEL SECURITY;
ALTER TABLE page_share_recipient ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_share_recipient FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_share_tenant_isolation ON page_share
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_share_recipient_tenant_isolation ON page_share_recipient
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_share_admin_bypass ON page_share TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_share_recipient_admin_bypass ON page_share_recipient TO stator_admin USING (true) WITH CHECK (true);

-- A person reads and makes only their own shares, of pages they may share.
CREATE POLICY page_share_own ON page_share AS RESTRICTIVE FOR ALL TO stator_app
    USING (sharer_id = current_actor_id())
    WITH CHECK (sharer_id = current_actor_id() AND page_shareable(page_id, current_actor_id()));
-- And names only people who may already view the page.
CREATE POLICY page_share_recipient_own ON page_share_recipient AS RESTRICTIVE FOR ALL TO stator_app
    USING (EXISTS (SELECT 1 FROM page_share s WHERE s.id = share_id))
    WITH CHECK (page_share_recipient_allowed(share_id, user_id));

CREATE POLICY outbox_event_share_fresh ON outbox_event AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (topic <> 'page.shared' OR outbox_share_is_fresh(payload));

-- A share is sent, never changed or taken back.
REVOKE ALL ON page_share, page_share_recipient FROM stator_app;
GRANT SELECT, INSERT ON page_share, page_share_recipient TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_share, page_share_recipient TO stator_admin;

-- +goose Down
DROP POLICY IF EXISTS outbox_event_share_fresh ON outbox_event;
DROP TABLE IF EXISTS page_share_recipient;
DROP TABLE IF EXISTS page_share;
DROP FUNCTION IF EXISTS page_share_brake();
DROP FUNCTION IF EXISTS outbox_share_is_fresh(jsonb);
DROP FUNCTION IF EXISTS page_share_recipient_allowed(uuid, uuid);
DROP FUNCTION IF EXISTS page_shareable(uuid, uuid);
DROP FUNCTION IF EXISTS page_share_per_hour();
