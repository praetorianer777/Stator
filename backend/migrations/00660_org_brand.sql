-- +goose Up
-- What an organization's exports carry: its logo and a line for the footer in
-- each language. The picture is in the object store; the row says it is there.
-- The colours come from the organization's default theme, not from here.
CREATE TABLE org_brand (
    org_id       uuid PRIMARY KEY REFERENCES org(id) ON DELETE CASCADE,
    footer_en    text NOT NULL DEFAULT '',
    footer_de    text NOT NULL DEFAULT '',
    logo_type    text,
    logo_size    integer,
    -- Each upload is a new object, so a browser may keep a logo for good.
    logo_version integer NOT NULL DEFAULT 0,
    updated_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT org_brand_footer_en_length CHECK (char_length(footer_en) <= 200),
    CONSTRAINT org_brand_footer_de_length CHECK (char_length(footer_de) <= 200),
    CONSTRAINT org_brand_logo_type CHECK (logo_type IS NULL OR logo_type IN ('image/png', 'image/jpeg', 'image/webp')),
    CONSTRAINT org_brand_logo_whole CHECK ((logo_type IS NULL) = (logo_size IS NULL))
);

ALTER TABLE org_brand ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_brand FORCE  ROW LEVEL SECURITY;

CREATE POLICY org_brand_tenant_isolation ON org_brand
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY org_brand_admin_bypass ON org_brand TO stator_admin USING (true) WITH CHECK (true);

GRANT SELECT, INSERT, UPDATE, DELETE ON org_brand TO stator_app, stator_admin;

-- Closed to an anonymous reader like every table a public page is not read
-- from; a public export reads the brand as the application does.
CREATE POLICY org_brand_not_anonymous ON org_brand AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

-- +goose StatementBegin
-- Only administrators change the brand, whatever the service above says.
CREATE FUNCTION org_brand_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF current_user = 'stator_app' AND NOT perm_is_admin(current_actor_id()) THEN
        RAISE EXCEPTION 'only an administrator of the organization changes its brand'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_brand_guard BEFORE INSERT OR UPDATE OR DELETE ON org_brand
    FOR EACH ROW EXECUTE FUNCTION org_brand_guard();

-- +goose Down
DROP TRIGGER IF EXISTS org_brand_guard ON org_brand;
DROP FUNCTION IF EXISTS org_brand_guard();
DROP TABLE IF EXISTS org_brand;
