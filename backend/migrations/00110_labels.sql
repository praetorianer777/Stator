-- +goose Up
-- Labels are words on pages. A label has no row of its own: it exists while a
-- page carries it, so nobody learns a word from a page they may not view.
-- Names are stored as label.Normalize writes them; the check below is the part
-- of that rule the database can hold without depending on its locale.
CREATE TABLE page_label (
    org_id     uuid NOT NULL,
    page_id    uuid NOT NULL,
    name       text NOT NULL,
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, page_id, name),
    CONSTRAINT page_label_name_normal CHECK (
        char_length(name) BETWEEN 1 AND 40
        AND name = lower(name)
        AND name !~ '^[-_.]'
        AND name !~ '[[:space:][:cntrl:]!"#$%&''()*+,/:;<=>?@\[\\\]^`{|}~]'),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE
);

-- text_pattern_ops serves both a label's pages and the prefix autocomplete asks for.
CREATE INDEX page_label_name_idx ON page_label (org_id, name text_pattern_ops, page_id);

ALTER TABLE page_label ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_label FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_label_tenant_isolation ON page_label
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_label_admin_bypass ON page_label TO stator_admin USING (true) WITH CHECK (true);

-- A label is its page's: seen with it, put on and taken off by who edits it.
CREATE POLICY page_label_viewers ON page_label AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY page_label_editors_add ON page_label AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (created_by = current_actor_id() AND perm_page_editable(page_id, current_actor_id()));
CREATE POLICY page_label_editors_remove ON page_label AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_page_editable(page_id, current_actor_id()));

-- A label is put on or taken off, never changed in place: the schema's
-- default privileges would give the app role UPDATE too.
GRANT SELECT, INSERT, UPDATE, DELETE ON page_label TO stator_admin;
REVOKE ALL ON page_label FROM stator_app;
GRANT SELECT, INSERT, DELETE ON page_label TO stator_app;

-- +goose Down
DROP TABLE IF EXISTS page_label;
