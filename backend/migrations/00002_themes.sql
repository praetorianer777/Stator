-- +goose Up
-- A theme is a saved redefinition of the interface's tokens, with the files
-- it draws from. It is its maker's, shared with the organization when they
-- say so, and chosen per person; deleting it returns its people to the
-- built-in theme by the cascade. Adapted from Armature, whose theme files
-- import here unchanged.
CREATE TABLE theme (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    owner_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name       text NOT NULL,
    shared     boolean NOT NULL DEFAULT false,
    spec       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT theme_name_not_blank CHECK (btrim(name) <> ''),
    -- A foreign key is checked past row level security, so the rows that
    -- point at a theme name its organization too and cannot cross tenants.
    CONSTRAINT theme_org_id_key UNIQUE (org_id, id)
);

-- Unique per owner within an organization, so saving an example in a second
-- organization is not refused for a theme the person cannot see from there.
CREATE UNIQUE INDEX theme_owner_name_idx ON theme (org_id, owner_id, lower(name));
CREATE INDEX theme_org_idx ON theme (org_id, shared);

CREATE TRIGGER theme_set_updated_at BEFORE UPDATE ON theme
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The files a theme draws from. The bytes live in the object store under
-- theme/<theme>/<asset>; the row is what says the file exists.
CREATE TABLE theme_asset (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    theme_id     uuid NOT NULL,
    name         text NOT NULL,
    content_type text NOT NULL,
    size         bigint NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, theme_id) REFERENCES theme (org_id, id) ON DELETE CASCADE
);

CREATE INDEX theme_asset_theme_idx ON theme_asset (theme_id);

-- Who chose what. One row per person per organization.
CREATE TABLE user_theme (
    org_id   uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    theme_id uuid NOT NULL,
    PRIMARY KEY (org_id, user_id),
    FOREIGN KEY (org_id, theme_id) REFERENCES theme (org_id, id) ON DELETE CASCADE
);

CREATE INDEX user_theme_theme_idx ON user_theme (theme_id);

ALTER TABLE theme       ENABLE ROW LEVEL SECURITY;
ALTER TABLE theme       FORCE  ROW LEVEL SECURITY;
ALTER TABLE theme_asset ENABLE ROW LEVEL SECURITY;
ALTER TABLE theme_asset FORCE  ROW LEVEL SECURITY;
ALTER TABLE user_theme  ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_theme  FORCE  ROW LEVEL SECURITY;

CREATE POLICY theme_tenant_isolation ON theme
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY theme_asset_tenant_isolation ON theme_asset
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY user_theme_tenant_isolation ON user_theme
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());

CREATE POLICY theme_admin_bypass ON theme TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY theme_asset_admin_bypass ON theme_asset TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY user_theme_admin_bypass ON user_theme TO stator_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON theme, theme_asset, user_theme TO stator_app, stator_admin;

-- +goose Down
DROP TABLE IF EXISTS user_theme;
DROP TABLE IF EXISTS theme_asset;
DROP TABLE IF EXISTS theme;
