-- +goose Up
-- An organization's own templates (#63), beside the built-ins: one for every
-- space of the organization when space_id is null, else for one space. A
-- template's variables are blanks the author fills in when a page is made
-- from it; its body marks where each goes with a templateVariable node,
-- which the service replaces, so no page ever holds one.
CREATE TABLE page_template (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    space_id    uuid,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    title       text NOT NULL DEFAULT '',
    body        jsonb NOT NULL,
    variables   jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE CASCADE,
    CONSTRAINT page_template_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT page_template_description_length CHECK (char_length(description) <= 500),
    CONSTRAINT page_template_title_length CHECK (char_length(title) <= 255),
    CONSTRAINT page_template_body_doc CHECK (jsonb_typeof(body) = 'object' AND body ->> 'type' = 'doc'),
    CONSTRAINT page_template_variables_shape CHECK (jsonb_typeof(variables) = 'array' AND jsonb_array_length(variables) <= 20)
);

CREATE UNIQUE INDEX page_template_name_idx
    ON page_template (org_id, COALESCE(space_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));
CREATE INDEX page_template_space_idx ON page_template (org_id, space_id);

-- +goose StatementBegin
-- Who made and last changed a template is the transaction's actor, never what
-- the caller says.
CREATE FUNCTION page_template_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.created_by := current_actor_id();
        NEW.created_at := now();
    ELSE
        NEW.created_by := OLD.created_by;
        NEW.created_at := OLD.created_at;
        IF NEW.space_id IS DISTINCT FROM OLD.space_id THEN
            RAISE EXCEPTION 'a template stays where it was made'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'page_template_stays';
        END IF;
    END IF;
    NEW.updated_by := current_actor_id();
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- Whether a document holds a template's variable. Only a template may: in a
-- page, a draft or a version it would be a blank nobody filled.
CREATE FUNCTION document_has_variables(body jsonb) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT jsonb_path_exists(body, 'lax $.** ? (@.type == "templateVariable")')
$$;
-- +goose StatementEnd

CREATE TRIGGER page_template_stamp BEFORE INSERT OR UPDATE ON page_template
    FOR EACH ROW EXECUTE FUNCTION page_template_stamp();

ALTER TABLE page ADD CONSTRAINT page_body_no_variables CHECK (NOT document_has_variables(body));
ALTER TABLE page_version ADD CONSTRAINT page_version_body_no_variables CHECK (NOT document_has_variables(body));
ALTER TABLE page_draft ADD CONSTRAINT page_draft_body_no_variables CHECK (NOT document_has_variables(body));

ALTER TABLE page_template ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_template FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_template_tenant_isolation ON page_template
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_template_admin_bypass ON page_template TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_template_not_anonymous ON page_template AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

-- Everybody who uses the organization reads its templates, and a space's
-- templates are read by whoever may view the space. A guest reads only their
-- space's, since the organization's are of the organization as a whole, and
-- somebody reading without signing in reads none. The organization's are
-- kept by its administrators and a space's by the space's.
CREATE POLICY page_template_readers ON page_template AS RESTRICTIVE FOR SELECT TO stator_app
    USING (CASE WHEN space_id IS NULL THEN perm_global_holds(current_actor_id(), 'use') AND perm_guest_space(current_actor_id()) IS NULL
                ELSE perm_space_holds(current_actor_id(), space_id, 'view') END);
CREATE POLICY page_template_adders ON page_template AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (CASE WHEN space_id IS NULL THEN perm_is_admin(current_actor_id())
                     ELSE perm_space_holds(current_actor_id(), space_id, 'administer') END);
CREATE POLICY page_template_keepers ON page_template AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (CASE WHEN space_id IS NULL THEN perm_is_admin(current_actor_id())
                ELSE perm_space_holds(current_actor_id(), space_id, 'administer') END)
    WITH CHECK (CASE WHEN space_id IS NULL THEN perm_is_admin(current_actor_id())
                     ELSE perm_space_holds(current_actor_id(), space_id, 'administer') END);
CREATE POLICY page_template_removers ON page_template AS RESTRICTIVE FOR DELETE TO stator_app
    USING (CASE WHEN space_id IS NULL THEN perm_is_admin(current_actor_id())
                ELSE perm_space_holds(current_actor_id(), space_id, 'administer') END);

-- +goose Down
DROP TABLE IF EXISTS page_template;
ALTER TABLE page_draft DROP CONSTRAINT IF EXISTS page_draft_body_no_variables;
ALTER TABLE page_version DROP CONSTRAINT IF EXISTS page_version_body_no_variables;
ALTER TABLE page DROP CONSTRAINT IF EXISTS page_body_no_variables;
DROP FUNCTION IF EXISTS document_has_variables(jsonb);
DROP FUNCTION IF EXISTS page_template_stamp();
