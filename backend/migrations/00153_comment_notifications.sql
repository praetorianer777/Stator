-- +goose Up
-- Ties notifications to the comments they are about. It belongs to the
-- comments (#22) but has to follow 00152, which makes the notification table,
-- so it takes the first number after it.
ALTER TABLE notification
    ADD CONSTRAINT notification_thread_fk FOREIGN KEY (org_id, thread_id) REFERENCES comment_thread (org_id, id) ON DELETE CASCADE,
    ADD CONSTRAINT notification_comment_fk FOREIGN KEY (org_id, comment_id) REFERENCES comment (org_id, id) ON DELETE CASCADE;

CREATE INDEX notification_comment_idx ON notification (org_id, comment_id) WHERE comment_id IS NOT NULL;

-- +goose StatementBegin
-- Deleting a comment takes back what people were told about it. The app role
-- may not delete notifications, and the people told are others, so this runs
-- as the table's owner, and only ever for a comment just deleted.
CREATE FUNCTION comment_forget_notifications() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM notification WHERE org_id = NEW.org_id AND comment_id = NEW.id;
    RETURN NULL;
END;
$$;

-- The worker may be about to tell somebody of a comment its author is
-- deleting. The share lock makes the two wait for each other: a delete that
-- committed first leaves nothing to insert, and one that commits after finds
-- the row to take back.
CREATE FUNCTION notification_comment_alive() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.comment_id IS NULL THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM comment c
    WHERE c.org_id = NEW.org_id AND c.id = NEW.comment_id AND c.deleted_at IS NULL
    FOR SHARE;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER comment_deleted_forget AFTER UPDATE OF deleted_at ON comment
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION comment_forget_notifications();

CREATE TRIGGER notification_about_live_comment BEFORE INSERT ON notification
    FOR EACH ROW EXECUTE FUNCTION notification_comment_alive();

-- +goose Down
DROP TRIGGER IF EXISTS notification_about_live_comment ON notification;
DROP TRIGGER IF EXISTS comment_deleted_forget ON comment;
DROP FUNCTION IF EXISTS notification_comment_alive();
DROP FUNCTION IF EXISTS comment_forget_notifications();
DROP INDEX IF EXISTS notification_comment_idx;
ALTER TABLE notification DROP CONSTRAINT IF EXISTS notification_comment_fk, DROP CONSTRAINT IF EXISTS notification_thread_fk;
