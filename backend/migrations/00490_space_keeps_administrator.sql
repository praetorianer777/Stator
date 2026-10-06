-- +goose Up
-- A space keeps an administrator of its own (#82). Taking administer from the
-- last subject that holds it in a space is refused, unless the person doing
-- it administers the organization, who holds every permission in every space
-- anyway and may close a space down to themselves (2026-09-30). What goes
-- with its subject or its space, a member removed, a group or a space
-- deleted, is not a removal anybody chose, and passes.
--
-- The check runs at commit, so a transaction that grants the new
-- administrators before it takes the old ones away, as every rewrite of a
-- space's table does, is judged by where it ends.

-- +goose StatementBegin
CREATE FUNCTION space_grant_keeps_administrator() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.permission <> 'administer' THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM space WHERE org_id = OLD.org_id AND id = OLD.space_id) THEN
        RETURN NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM space_grant
               WHERE org_id = OLD.org_id AND space_id = OLD.space_id AND permission = 'administer') THEN
        RETURN NULL;
    END IF;
    IF (OLD.user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM org_member WHERE org_id = OLD.org_id AND user_id = OLD.user_id))
       OR (OLD.group_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM groups WHERE org_id = OLD.org_id AND id = OLD.group_id)) THEN
        RETURN NULL;
    END IF;
    IF current_actor_id() IS NULL OR perm_is_admin(current_actor_id()) THEN
        RETURN NULL;
    END IF;
    RAISE EXCEPTION 'A space keeps at least one administrator. Give administer to another person or group before you take it from the last one.'
        USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_keeps_administrator';
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER space_grant_keeps_administrator
    AFTER DELETE OR UPDATE OF permission, space_id ON space_grant
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION space_grant_keeps_administrator();

-- +goose Down
DROP TRIGGER IF EXISTS space_grant_keeps_administrator ON space_grant;
DROP FUNCTION IF EXISTS space_grant_keeps_administrator();
