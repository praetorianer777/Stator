-- +goose Up
-- The organization's hub (#40): one of its pages, chosen by its
-- administrators, where everybody finds shared news and resources, and
-- which can be where everybody lands. A page, so it is written, published,
-- restricted and kept like any other.
ALTER TABLE org
    ADD COLUMN hub_page_id uuid,
    ADD COLUMN hub_landing boolean NOT NULL DEFAULT false,
    -- The pair keeps the hub one of the organization's own pages; a page
    -- deleted for good stops being the hub.
    ADD CONSTRAINT org_hub_page_fkey FOREIGN KEY (id, hub_page_id) REFERENCES page (org_id, id) ON DELETE SET NULL (hub_page_id),
    ADD CONSTRAINT org_hub_landing_needs_hub CHECK (NOT hub_landing OR hub_page_id IS NOT NULL);

-- +goose StatementBegin
-- Only administrators choose the hub. A hub page deleted for good clears it
-- as the table's owner, which is no person's choice, and then nobody lands
-- on a hub that is gone; asking to land on no hub is refused by the check.
CREATE FUNCTION org_hub_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF current_user = 'stator_app' AND NOT perm_is_admin(current_actor_id()) THEN
        RAISE EXCEPTION 'only an administrator of the organization chooses its hub'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.hub_page_id IS NULL AND OLD.hub_page_id IS NOT NULL THEN
        NEW.hub_landing := false;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_hub_guard BEFORE UPDATE OF hub_page_id, hub_landing ON org
    FOR EACH ROW EXECUTE FUNCTION org_hub_guard();

-- +goose Down
DROP TRIGGER IF EXISTS org_hub_guard ON org;
DROP FUNCTION IF EXISTS org_hub_guard();
ALTER TABLE org
    DROP CONSTRAINT IF EXISTS org_hub_landing_needs_hub,
    DROP CONSTRAINT IF EXISTS org_hub_page_fkey,
    DROP COLUMN IF EXISTS hub_landing,
    DROP COLUMN IF EXISTS hub_page_id;
