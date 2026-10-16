-- +goose Up
-- Armature names its installation by id alone (#422): /auth/me answers
-- installation, without a slug, where it answered org. A token is checked
-- against armature_org_id, which holds the installation's id, so the slug an
-- administrator typed is no longer asked for. A stored one stays: the
-- connection's triggers compare it, and as nothing writes it any more, it
-- never moves the connection.
ALTER TABLE armature_connection ALTER COLUMN org_slug DROP NOT NULL;

-- +goose Down
UPDATE armature_connection SET org_slug = 'armature' WHERE org_slug IS NULL;
ALTER TABLE armature_connection ALTER COLUMN org_slug SET NOT NULL;
