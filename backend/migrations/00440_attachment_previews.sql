-- +goose Up
-- Previews of office documents (#61): each version of a file is converted to
-- PDF once, by the first reader who asks, and the PDF kept in the bucket
-- beside it. A file that cannot be converted is noted as failed, so nobody
-- pays for the attempt again; its next version is tried afresh. A version
-- never changes, so neither does its preview: there is no UPDATE.
ALTER TABLE attachment ADD CONSTRAINT attachment_org_id_key UNIQUE (org_id, id);

CREATE TABLE attachment_preview (
    attachment_id uuid PRIMARY KEY,
    org_id        uuid NOT NULL,
    state         text NOT NULL CHECK (state IN ('ready', 'failed')),
    size_bytes    bigint,
    -- Derived from the row, so no insert can point a preview at another
    -- organization's object or at the file itself.
    object_key    text GENERATED ALWAYS AS
        ('org/' || org_id::text || '/preview/' || attachment_id::text || '.pdf') STORED,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attachment_preview_size CHECK ((state = 'ready') = (size_bytes IS NOT NULL) AND (size_bytes IS NULL OR size_bytes > 0)),
    -- Named with its organization, as a foreign key is checked past row level
    -- security; the preview goes with its file, whichever way that goes.
    FOREIGN KEY (org_id, attachment_id) REFERENCES attachment (org_id, id) ON DELETE CASCADE
);

-- A ready preview has bytes in the bucket, so it leaves a tombstone for the
-- reaper like the file it was made from.
CREATE TRIGGER attachment_preview_tombstone_on_delete AFTER DELETE ON attachment_preview
    FOR EACH ROW WHEN (OLD.state = 'ready') EXECUTE FUNCTION attachment_leave_tombstone();

ALTER TABLE attachment_preview ENABLE ROW LEVEL SECURITY;
ALTER TABLE attachment_preview FORCE  ROW LEVEL SECURITY;

CREATE POLICY attachment_preview_tenant_isolation ON attachment_preview
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY attachment_preview_admin_bypass ON attachment_preview TO stator_admin USING (true) WITH CHECK (true);

-- A preview is its file's: whoever may see the file sees the preview and may
-- be the one whose visit makes it. The file's own policies decide who sees it.
CREATE POLICY attachment_preview_viewers ON attachment_preview AS RESTRICTIVE FOR SELECT TO stator_app
    USING (EXISTS (SELECT 1 FROM attachment a WHERE a.org_id = attachment_preview.org_id AND a.id = attachment_preview.attachment_id));
CREATE POLICY attachment_preview_makers ON attachment_preview AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (EXISTS (SELECT 1 FROM attachment a WHERE a.org_id = attachment_preview.org_id AND a.id = attachment_preview.attachment_id));

-- Removed only with its file, by the cascade; the default privileges grant
-- UPDATE and DELETE, so they are taken back.
GRANT SELECT, INSERT, UPDATE, DELETE ON attachment_preview TO stator_admin;
GRANT SELECT, INSERT ON attachment_preview TO stator_app;
REVOKE UPDATE, DELETE ON attachment_preview FROM stator_app;

-- +goose Down
DROP TABLE IF EXISTS attachment_preview;
ALTER TABLE attachment DROP CONSTRAINT IF EXISTS attachment_org_id_key;
