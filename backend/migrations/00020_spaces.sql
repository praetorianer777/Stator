-- +goose Up
-- A space is a team's or a topic's tree of pages, reached by a short key in
-- every address. Its home page is the root of that tree: every other page
-- hangs somewhere below it, which the constraints here keep true.
CREATE TABLE space (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    key          text NOT NULL,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    -- Set in the transaction that makes the space, right after its home page.
    home_page_id uuid,
    created_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    -- The same shape as a project key in Armature, so the two read alike.
    CONSTRAINT space_key_shape CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$'),
    CONSTRAINT space_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT space_key_unique UNIQUE (org_id, key),
    -- A foreign key is checked past row level security, so everything that
    -- points at a space names its organization too and cannot cross tenants.
    CONSTRAINT space_org_id_key UNIQUE (org_id, id)
);

CREATE TRIGGER space_set_updated_at BEFORE UPDATE ON space
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A page's current title and body. Drafts and published versions will live in
-- tables of their own keyed on (org_id, page_id), so this row stays the page's
-- identity, its place in the tree and its latest published content.
CREATE TABLE page (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    space_id   uuid NOT NULL,
    -- NULL only for the space's home page, the root of its tree.
    parent_id  uuid,
    -- A lexicographic rank among siblings; "C" so the database sorts it byte
    -- by byte, exactly as the rank generator compares.
    rank       text COLLATE "C" NOT NULL,
    title      text NOT NULL,
    body       jsonb NOT NULL DEFAULT '{"type":"doc","content":[{"type":"paragraph"}]}'::jsonb,
    -- Counts saves of the body, so a save made from an older copy is refused.
    version    integer NOT NULL DEFAULT 1,
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT page_title_not_blank CHECK (btrim(title) <> ''),
    CONSTRAINT page_rank_not_blank CHECK (rank <> ''),
    CONSTRAINT page_version_positive CHECK (version > 0),
    CONSTRAINT page_org_id_key UNIQUE (org_id, id),
    CONSTRAINT page_space_id_key UNIQUE (org_id, space_id, id),
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE CASCADE,
    -- The parent is in the same organization and the same space. Moving a
    -- page to another space rewrites its space and its parent in one
    -- statement, and the cascade carries its whole subtree along.
    CONSTRAINT page_parent_fkey FOREIGN KEY (org_id, space_id, parent_id)
        REFERENCES page (org_id, space_id, id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- One root per space, which is its home page.
CREATE UNIQUE INDEX page_one_root_idx ON page (org_id, space_id) WHERE parent_id IS NULL;
CREATE INDEX page_children_idx ON page (org_id, parent_id, rank);
CREATE INDEX page_space_idx ON page (org_id, space_id);

CREATE TRIGGER page_set_updated_at BEFORE UPDATE ON page
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The home page is in the space it is the home of.
ALTER TABLE space ADD CONSTRAINT space_home_page_fkey FOREIGN KEY (org_id, id, home_page_id)
    REFERENCES page (org_id, space_id, id);

ALTER TABLE space ENABLE ROW LEVEL SECURITY;
ALTER TABLE space FORCE  ROW LEVEL SECURITY;
ALTER TABLE page  ENABLE ROW LEVEL SECURITY;
ALTER TABLE page  FORCE  ROW LEVEL SECURITY;

CREATE POLICY space_tenant_isolation ON space
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_tenant_isolation ON page
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY space_admin_bypass ON space TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_admin_bypass ON page TO stator_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON space, page TO stator_app, stator_admin;

-- +goose Down
ALTER TABLE IF EXISTS space DROP CONSTRAINT IF EXISTS space_home_page_fkey;
DROP TABLE IF EXISTS page;
DROP TABLE IF EXISTS space;
