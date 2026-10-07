-- +goose Up
-- Scheduled publishing (#71). An editor names a time at which their draft of
-- a page is published, as they would publish it then, by the worker in their
-- name. A page has at most one schedule, so its editors see the one plan and
-- nobody's schedule quietly overtakes another's.
--
-- The schedule hangs off the draft it publishes: discarding or publishing
-- the draft, the page going live, the page purged or its author leaving the
-- organization all take the draft, and the schedule goes with it.
CREATE TABLE page_schedule (
    org_id          uuid NOT NULL,
    page_id         uuid NOT NULL,
    user_id         uuid NOT NULL,
    publish_at      timestamptz NOT NULL,
    comment         text NOT NULL DEFAULT '',
    notify_watchers boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- Set by the worker alone, when the time came and the publish was refused.
    failed_at       timestamptz,
    failure         text,
    PRIMARY KEY (org_id, page_id),
    FOREIGN KEY (org_id, page_id, user_id) REFERENCES page_draft (org_id, page_id, user_id) ON DELETE CASCADE,
    CONSTRAINT page_schedule_failure_known CHECK (failure IN ('gone', 'forbidden', 'archived', 'conflict')),
    CONSTRAINT page_schedule_failed_whole CHECK ((failed_at IS NULL) = (failure IS NULL)),
    -- page.MaxCommentLength.
    CONSTRAINT page_schedule_comment_length CHECK (char_length(comment) <= 500)
);
CREATE INDEX page_schedule_due_idx ON page_schedule (publish_at) WHERE failed_at IS NULL;
CREATE INDEX page_schedule_user_idx ON page_schedule (org_id, user_id);

ALTER TABLE page_schedule ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_schedule FORCE  ROW LEVEL SECURITY;
CREATE POLICY page_schedule_tenant_isolation ON page_schedule
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_schedule_admin_bypass ON page_schedule TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_schedule_not_anonymous ON page_schedule AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
-- Its author, and whoever may edit the page; a reader learns of a publish
-- when it happens. The author keeps seeing their own after losing edit, so
-- the worker acting for them finds it and records why it was refused.
CREATE POLICY page_schedule_readers ON page_schedule AS RESTRICTIVE FOR SELECT TO stator_app
    USING (user_id = current_actor_id() OR perm_page_editable(page_id, current_actor_id()));
-- Only one's own, only as an editor, only ahead, and never as a failure.
CREATE POLICY page_schedule_planners ON page_schedule AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (user_id = current_actor_id() AND perm_page_editable(page_id, current_actor_id())
                AND publish_at > now() AND failed_at IS NULL);
-- Rescheduling is the author's. Its USING names the author alone, so the
-- worker, acting for them, can lock their row whatever they may still do.
CREATE POLICY page_schedule_reschedulers ON page_schedule AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id() AND perm_page_editable(page_id, current_actor_id())
                AND publish_at > now() AND failed_at IS NULL);
-- Any editor may call a schedule off, as any editor may publish over it.
CREATE POLICY page_schedule_cancellers ON page_schedule AS RESTRICTIVE FOR DELETE TO stator_app
    USING (user_id = current_actor_id() OR perm_page_editable(page_id, current_actor_id()));
REVOKE ALL ON page_schedule FROM stator_app;
GRANT SELECT, INSERT, DELETE ON page_schedule TO stator_app;
GRANT UPDATE (publish_at, comment, notify_watchers, failed_at, failure) ON page_schedule TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_schedule TO stator_admin;

-- A scheduled publish that did not go out tells its author, as a kind of its
-- own. It follows 00410, which last redefined the kinds, so it lists every one.
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('assigned', 'due', 'mentioned', 'shared', 'replied', 'commented', 'resolved', 'published', 'created', 'expired', 'failed'));

-- +goose Down
DELETE FROM notification WHERE kind = 'failed';
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('assigned', 'due', 'mentioned', 'shared', 'replied', 'commented', 'resolved', 'published', 'created', 'expired'));
DROP TABLE IF EXISTS page_schedule;
