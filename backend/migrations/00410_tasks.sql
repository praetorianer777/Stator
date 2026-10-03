-- +goose Up
-- Tasks (#56): the checklist items of a page's published version, each with
-- the first person it mentions as its assignee and the first date in it as
-- its due day. The document is the only record a person writes; these rows
-- are read from it on every publish, so they list and remind without parsing
-- every page, and a report of tasks (#57) can filter them by space, assignee,
-- day and state.
CREATE TABLE page_task (
    org_id      uuid NOT NULL,
    id          uuid NOT NULL DEFAULT uuidv7(),
    page_id     uuid NOT NULL,
    -- The item's taskId in the document, unique on its page only: a copy of a
    -- page keeps the ids of the items it copied.
    task_id     uuid NOT NULL,
    position    integer NOT NULL CHECK (position >= 0),
    summary     text NOT NULL CHECK (char_length(summary) <= 500),
    done        boolean NOT NULL DEFAULT false,
    assignee_id uuid,
    due_on      date,
    -- Stamped by page_task_stamp, never written by the app: who assigned the
    -- task and in which version, which the worker reads to tell the assignee once.
    assigned_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    assigned_version integer,
    assigned_at      timestamptz,
    done_at          timestamptz,
    -- Set by the worker once it has reminded the assignee on the due day, and
    -- at once for a day already past, so a reminder is told once per day and
    -- assignee; a new day or assignee clears it.
    due_noticed_at   timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    UNIQUE (org_id, page_id, task_id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    -- Somebody who leaves the organization leaves their tasks unassigned.
    FOREIGN KEY (org_id, assignee_id) REFERENCES org_member (org_id, user_id) ON DELETE SET NULL (assignee_id),
    CONSTRAINT page_task_done_stamp CHECK (done = (done_at IS NOT NULL)),
    CONSTRAINT page_task_assigned_stamp CHECK ((assignee_id IS NULL) = (assigned_version IS NULL))
);

-- The open tasks of a person, soonest first and those without a day last, and
-- their done tasks, the latest first: both walked by keyset.
CREATE INDEX page_task_open_idx ON page_task (org_id, assignee_id, (COALESCE(due_on, DATE '9999-12-31')), id)
    WHERE NOT done AND assignee_id IS NOT NULL;
CREATE INDEX page_task_done_idx ON page_task (org_id, assignee_id, done_at DESC, id DESC)
    WHERE done AND assignee_id IS NOT NULL;
-- The worker looks for days that came, across organizations.
CREATE INDEX page_task_due_idx ON page_task (due_on)
    WHERE NOT done AND assignee_id IS NOT NULL AND due_noticed_at IS NULL;

-- +goose StatementBegin
-- The day a due day is judged against. UTC, as a date node is read in UTC, so
-- a task is due on the same day for everybody.
CREATE FUNCTION task_today() RETURNS date
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT (now() AT TIME ZONE 'UTC')::date
$$;

-- Who assigned a task, in which version, and when it was done are the
-- database's to say, so nobody can make a task look assigned by somebody
-- else or have the worker tell an assignee twice. The worker's note of a
-- reminder changes nothing else and keeps them.
CREATE FUNCTION page_task_stamp() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    reassigned boolean := TG_OP = 'INSERT' OR NEW.assignee_id IS DISTINCT FROM OLD.assignee_id;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.due_noticed_at IS DISTINCT FROM OLD.due_noticed_at AND NOT reassigned
       AND NEW.due_on IS NOT DISTINCT FROM OLD.due_on AND NEW.done = OLD.done THEN
        RETURN NEW;
    END IF;
    IF reassigned AND NEW.assignee_id IS NOT NULL
       AND NOT perm_page_viewable(NEW.page_id, NEW.assignee_id) THEN
        RAISE EXCEPTION 'a task can only be assigned to somebody who may view its page'
            USING ERRCODE = '42501';
    END IF;
    NEW.updated_at := now();
    IF reassigned THEN
        NEW.assigned_at := CASE WHEN NEW.assignee_id IS NULL THEN NULL ELSE now() END;
        NEW.assigned_by := CASE WHEN NEW.assignee_id IS NULL THEN NULL ELSE current_actor_id() END;
        NEW.assigned_version := CASE WHEN NEW.assignee_id IS NULL THEN NULL
            ELSE (SELECT p.version FROM page p WHERE p.org_id = NEW.org_id AND p.id = NEW.page_id) END;
    ELSE
        NEW.assigned_at := OLD.assigned_at;
        NEW.assigned_by := OLD.assigned_by;
        NEW.assigned_version := OLD.assigned_version;
    END IF;
    IF TG_OP = 'INSERT' OR NEW.done IS DISTINCT FROM OLD.done THEN
        NEW.done_at := CASE WHEN NEW.done THEN now() END;
    ELSE
        NEW.done_at := OLD.done_at;
    END IF;
    IF reassigned OR NEW.due_on IS DISTINCT FROM OLD.due_on THEN
        NEW.due_noticed_at := CASE WHEN NEW.due_on < task_today() THEN now() END;
    ELSE
        NEW.due_noticed_at := OLD.due_noticed_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_task_stamp BEFORE INSERT OR UPDATE ON page_task
    FOR EACH ROW EXECUTE FUNCTION page_task_stamp();

ALTER TABLE page_task ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_task FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_task_tenant_isolation ON page_task
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_task_admin_bypass ON page_task TO stator_admin USING (true) WITH CHECK (true);

-- Read with the page. Written only by somebody who may edit the published page,
-- out of the trash, which is who publishes it.
CREATE POLICY page_task_read ON page_task AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY page_task_insert ON page_task AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (page_stewardable(page_id, current_actor_id()));
CREATE POLICY page_task_update ON page_task AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (page_stewardable(page_id, current_actor_id()))
    WITH CHECK (page_stewardable(page_id, current_actor_id()));
CREATE POLICY page_task_delete ON page_task AS RESTRICTIVE FOR DELETE TO stator_app
    USING (page_stewardable(page_id, current_actor_id()));

-- Only the worker notes a reminder, and only the stamp writes who assigned.
REVOKE ALL ON page_task FROM stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_task TO stator_admin;
GRANT SELECT, DELETE ON page_task TO stator_app;
GRANT INSERT (org_id, page_id, task_id, position, summary, done, assignee_id, due_on),
      UPDATE (position, summary, done, assignee_id, due_on) ON page_task TO stator_app;
REVOKE EXECUTE ON FUNCTION page_task_stamp() FROM PUBLIC;

-- An assignment and a due day tell the assignee, each as a kind of its own.
-- It follows 00251, which last redefined the kinds, so it lists every one.
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('assigned', 'due', 'mentioned', 'shared', 'replied', 'commented', 'resolved', 'published', 'created', 'expired'));

-- +goose Down
DELETE FROM notification WHERE kind IN ('assigned', 'due');
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('mentioned', 'shared', 'replied', 'commented', 'resolved', 'published', 'created', 'expired'));
DROP TABLE IF EXISTS page_task;
DROP FUNCTION IF EXISTS page_task_stamp();
DROP FUNCTION IF EXISTS task_today();
