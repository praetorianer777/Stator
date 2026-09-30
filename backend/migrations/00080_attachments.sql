-- +goose Up
-- Files on a page. The bytes live in the bucket under object_key; this row is
-- what is known about them and what row level security guards. No row, no way
-- to reach the object. The key is derived from the row, so no write can point
-- a row at another organization's object.
CREATE TABLE attachment (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    page_id      uuid NOT NULL,
    uploaded_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    file_name    text NOT NULL,
    content_type text NOT NULL DEFAULT 'application/octet-stream',
    size_bytes   bigint NOT NULL,
    width        integer,
    height       integer,
    object_key   text GENERATED ALWAYS AS
        ('org/' || org_id::text || '/page/' || page_id::text || '/' || id::text) STORED,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attachment_name_not_blank CHECK (btrim(file_name) <> ''),
    CONSTRAINT attachment_size_positive CHECK (size_bytes > 0),
    CONSTRAINT attachment_dimensions CHECK ((width IS NULL) = (height IS NULL) AND (width IS NULL OR (width > 0 AND height > 0))),
    CONSTRAINT attachment_object_key_unique UNIQUE (object_key),
    -- A foreign key is checked past row level security, so the page is named
    -- with its organization and cannot be another tenant's.
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE
);

CREATE INDEX attachment_page_idx ON attachment (org_id, page_id, created_at DESC);

-- A tombstone is an object whose row is gone and whose bytes may still be in
-- the bucket, as in Armature. The trigger below writes it in the statement
-- that removes the row, whichever path removes it: a delete, a purged page, a
-- deleted space or organization. It is cleared once the bytes are gone. There
-- is no foreign key to org, so the tombstones of a deleted organization stay
-- for the worker's reaper.
CREATE TABLE attachment_tombstone (
    object_key text PRIMARY KEY,
    org_id     uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- The reaper deletes whatever a tombstone names, so a tenant can only
    -- ever name objects under its own prefix.
    CONSTRAINT attachment_tombstone_own_prefix CHECK (starts_with(object_key, 'org/' || org_id::text || '/'))
);

CREATE INDEX attachment_tombstone_created_idx ON attachment_tombstone (created_at);

-- +goose StatementBegin
CREATE FUNCTION attachment_leave_tombstone() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO attachment_tombstone (object_key, org_id) VALUES (OLD.object_key, OLD.org_id)
        ON CONFLICT (object_key) DO NOTHING;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER attachment_tombstone_on_delete AFTER DELETE ON attachment
    FOR EACH ROW EXECUTE FUNCTION attachment_leave_tombstone();

ALTER TABLE attachment ENABLE ROW LEVEL SECURITY;
ALTER TABLE attachment FORCE  ROW LEVEL SECURITY;
ALTER TABLE attachment_tombstone ENABLE ROW LEVEL SECURITY;
ALTER TABLE attachment_tombstone FORCE  ROW LEVEL SECURITY;

CREATE POLICY attachment_tenant_isolation ON attachment
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY attachment_tombstone_tenant_isolation ON attachment_tombstone
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY attachment_admin_bypass ON attachment TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY attachment_tombstone_admin_bypass ON attachment_tombstone TO stator_admin USING (true) WITH CHECK (true);

-- A file never changes once stored; a new one is uploaded instead. Without
-- UPDATE the app cannot move a row, and with it its key, off its object. The
-- default privileges grant it, so it is taken back.
GRANT SELECT, INSERT, UPDATE, DELETE ON attachment, attachment_tombstone TO stator_admin;
GRANT SELECT, INSERT, DELETE ON attachment, attachment_tombstone TO stator_app;
REVOKE UPDATE ON attachment, attachment_tombstone FROM stator_app;

-- +goose Down
DROP TABLE IF EXISTS attachment;
DROP FUNCTION IF EXISTS attachment_leave_tombstone();
DROP TABLE IF EXISTS attachment_tombstone;
