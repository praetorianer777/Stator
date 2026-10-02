-- +goose Up
-- Personal spaces (#35): a space that belongs to the person who made it. Any
-- member may make one, one each, and nobody else sees it until its owner
-- shares it through the space's permissions like any other space.
ALTER TABLE space ADD COLUMN owner_id uuid REFERENCES app_user(id) ON DELETE SET NULL;

-- One each per organization; a space nobody owns any more is no longer counted.
CREATE UNIQUE INDEX space_one_personal ON space (org_id, owner_id) WHERE owner_id IS NOT NULL;

-- +goose StatementBegin
-- The owner is fixed when the space is made: a space handed to somebody else
-- would be theirs without their asking. Losing the owner, when their account
-- goes, is the one change, and leaves an ordinary space.
CREATE FUNCTION space_owner_fixed() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.owner_id IS DISTINCT FROM OLD.owner_id AND NEW.owner_id IS NOT NULL THEN
        RAISE EXCEPTION 'A personal space keeps the owner it was made for.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_owner_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER space_owner_fixed BEFORE UPDATE OF owner_id ON space
    FOR EACH ROW EXECUTE FUNCTION space_owner_fixed();

-- +goose StatementBegin
-- A personal space starts with its owner alone: no grant to everyone.
CREATE OR REPLACE FUNCTION space_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.owner_id IS NULL THEN
        INSERT INTO space_grant (org_id, space_id, permission, subject_type)
        SELECT NEW.org_id, NEW.id, p, 'everyone' FROM unnest(ARRAY['view', 'addPages', 'addComments', 'delete']) AS p;
    END IF;
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.created_by) THEN
        INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id)
        VALUES (NEW.org_id, NEW.id, 'administer', 'user', NEW.created_by);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Making a space takes createSpace, but anybody who uses Stator, with a token
-- for the whole organization, makes their own personal one.
DROP POLICY space_creators ON space;
CREATE POLICY space_creators ON space AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (created_by = current_actor_id() AND (
        (owner_id IS NULL AND perm_global_holds(current_actor_id(), 'createSpace'))
        OR (owner_id = current_actor_id() AND perm_global_holds(current_actor_id(), 'use') AND perm_token_whole(current_actor_id()))));

-- +goose Down
DROP POLICY space_creators ON space;
CREATE POLICY space_creators ON space AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (perm_global_holds(current_actor_id(), 'createSpace') AND created_by = current_actor_id());

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO space_grant (org_id, space_id, permission, subject_type)
    SELECT NEW.org_id, NEW.id, p, 'everyone' FROM unnest(ARRAY['view', 'addPages', 'addComments', 'delete']) AS p;
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.created_by) THEN
        INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id)
        VALUES (NEW.org_id, NEW.id, 'administer', 'user', NEW.created_by);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS space_owner_fixed ON space;
DROP FUNCTION IF EXISTS space_owner_fixed();
DROP INDEX IF EXISTS space_one_personal;
ALTER TABLE space DROP COLUMN IF EXISTS owner_id;
