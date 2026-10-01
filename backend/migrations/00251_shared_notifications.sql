-- +goose Up
-- A share (#67) tells people as the kind shared. It follows 00250, which
-- last redefined the kinds, so it lists every one of them.
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('mentioned', 'shared', 'replied', 'commented', 'resolved', 'published', 'created', 'expired'));

-- +goose Down
DELETE FROM notification WHERE kind = 'shared';
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('mentioned', 'replied', 'commented', 'resolved', 'published', 'created', 'expired'));
