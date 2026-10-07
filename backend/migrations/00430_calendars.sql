-- +goose Up
-- Team calendars (#60): a space keeps calendars, each a name and its events.
-- An event is a title, a kind (an event or somebody's absence) and a span:
-- whole days, kept as midnights in UTC with the last day inclusive, as a
-- date node is read in UTC, or two instants for an event with times.
CREATE TABLE calendar (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    space_id   uuid NOT NULL,
    name       text NOT NULL,
    -- Stamped by calendar_stamp, never written by the app.
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    -- What an event names, so its space is its calendar's.
    UNIQUE (org_id, space_id, id),
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE CASCADE,
    CONSTRAINT calendar_name_shape CHECK (name = btrim(name) AND name <> '' AND char_length(name) <= 100)
);

-- Two calendars of one space are told apart by name, whatever its case.
CREATE UNIQUE INDEX calendar_name_idx ON calendar (org_id, space_id, lower(name));

CREATE TABLE calendar_event (
    org_id      uuid NOT NULL,
    id          uuid NOT NULL DEFAULT uuidv7(),
    space_id    uuid NOT NULL,
    calendar_id uuid NOT NULL,
    title       text NOT NULL,
    kind        text NOT NULL DEFAULT 'event' CHECK (kind IN ('event', 'absence')),
    all_day     boolean NOT NULL,
    starts_at   timestamptz NOT NULL,
    ends_at     timestamptz NOT NULL,
    -- Stamped by calendar_stamp, never written by the app.
    created_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    FOREIGN KEY (org_id, space_id, calendar_id) REFERENCES calendar (org_id, space_id, id) ON DELETE CASCADE,
    CONSTRAINT calendar_event_title_shape CHECK (title = btrim(title) AND title <> '' AND char_length(title) <= 200),
    CONSTRAINT calendar_event_order CHECK (ends_at >= starts_at),
    -- calendar.MaxEventDays, which a test holds to this.
    CONSTRAINT calendar_event_span CHECK (ends_at - starts_at <= interval '366 days'),
    CONSTRAINT calendar_event_whole_days CHECK (NOT all_day OR (
        (starts_at AT TIME ZONE 'UTC')::time = '00:00' AND (ends_at AT TIME ZONE 'UTC')::time = '00:00'))
);

-- A month of one calendar, read by when its events start.
CREATE INDEX calendar_event_range_idx ON calendar_event (org_id, calendar_id, starts_at);

-- +goose StatementBegin
-- How many calendars one space holds. calendar.MaxPerSpace is the same
-- number, and a test holds the two together.
CREATE FUNCTION calendar_max() RETURNS integer
    LANGUAGE sql IMMUTABLE
AS $$ SELECT 20 $$;

-- The limit, counted as the owner under a lock per space, so two additions
-- at once cannot both pass.
CREATE FUNCTION calendar_limit() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('calendar:' || NEW.space_id::text, 0));
    IF (SELECT count(*) FROM calendar WHERE org_id = NEW.org_id AND space_id = NEW.space_id) >= calendar_max() THEN
        RAISE EXCEPTION 'a space holds at most % calendars', calendar_max()
            USING ERRCODE = 'check_violation', CONSTRAINT = 'calendar_limit';
    END IF;
    RETURN NEW;
END;
$$;

-- Who made a calendar or an event is the database's to say, so nobody adds
-- one in somebody else's name; a change keeps it and moves updated_at.
CREATE FUNCTION calendar_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.created_by := current_actor_id();
        NEW.created_at := now();
    ELSE
        NEW.created_by := OLD.created_by;
        NEW.created_at := OLD.created_at;
    END IF;
    IF TG_TABLE_NAME = 'calendar_event' THEN
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END;
$$;

-- Whether the actor may change a space's calendars: whoever may add pages
-- to it, while the space is not archived, as for its pages.
CREATE FUNCTION calendar_writable(space uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT perm_space_holds(current_actor_id(), space, 'addPages')
       AND EXISTS (SELECT 1 FROM space s WHERE s.id = space AND s.org_id = current_org_id() AND s.archived_at IS NULL)
$$;
-- +goose StatementEnd

CREATE TRIGGER calendar_limit BEFORE INSERT ON calendar
    FOR EACH ROW EXECUTE FUNCTION calendar_limit();
CREATE TRIGGER calendar_stamp BEFORE INSERT OR UPDATE ON calendar
    FOR EACH ROW EXECUTE FUNCTION calendar_stamp();
CREATE TRIGGER calendar_event_stamp BEFORE INSERT OR UPDATE ON calendar_event
    FOR EACH ROW EXECUTE FUNCTION calendar_stamp();

ALTER TABLE calendar ENABLE ROW LEVEL SECURITY;
ALTER TABLE calendar FORCE  ROW LEVEL SECURITY;
ALTER TABLE calendar_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE calendar_event FORCE  ROW LEVEL SECURITY;

CREATE POLICY calendar_tenant_isolation ON calendar
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY calendar_admin_bypass ON calendar TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY calendar_event_tenant_isolation ON calendar_event
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY calendar_event_admin_bypass ON calendar_event TO stator_admin USING (true) WITH CHECK (true);

-- Everybody who may view the space reads its calendars and their events;
-- whoever may add pages to it, out of the archive, changes them.
CREATE POLICY calendar_read ON calendar AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_space_holds(current_actor_id(), space_id, 'view'));
CREATE POLICY calendar_add ON calendar AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (calendar_writable(space_id));
CREATE POLICY calendar_change ON calendar AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (calendar_writable(space_id)) WITH CHECK (calendar_writable(space_id));
CREATE POLICY calendar_remove ON calendar AS RESTRICTIVE FOR DELETE TO stator_app
    USING (calendar_writable(space_id));

CREATE POLICY calendar_event_read ON calendar_event AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_space_holds(current_actor_id(), space_id, 'view'));
CREATE POLICY calendar_event_add ON calendar_event AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (calendar_writable(space_id));
CREATE POLICY calendar_event_change ON calendar_event AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (calendar_writable(space_id)) WITH CHECK (calendar_writable(space_id));
CREATE POLICY calendar_event_remove ON calendar_event AS RESTRICTIVE FOR DELETE TO stator_app
    USING (calendar_writable(space_id));

-- A calendar stays in its space and an event in its calendar: only the name
-- of one and what the other says and when may change.
REVOKE ALL ON calendar, calendar_event FROM stator_app;
GRANT SELECT, DELETE ON calendar, calendar_event TO stator_app;
GRANT INSERT (org_id, id, space_id, name), UPDATE (name) ON calendar TO stator_app;
GRANT INSERT (org_id, id, space_id, calendar_id, title, kind, all_day, starts_at, ends_at),
      UPDATE (title, kind, all_day, starts_at, ends_at) ON calendar_event TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON calendar, calendar_event TO stator_admin;
REVOKE EXECUTE ON FUNCTION calendar_limit() FROM PUBLIC;

-- +goose Down
DROP TABLE IF EXISTS calendar_event;
DROP TABLE IF EXISTS calendar;
DROP FUNCTION IF EXISTS calendar_writable(uuid);
DROP FUNCTION IF EXISTS calendar_stamp();
DROP FUNCTION IF EXISTS calendar_limit();
DROP FUNCTION IF EXISTS calendar_max();
