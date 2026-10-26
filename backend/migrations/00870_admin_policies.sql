-- +goose Up
-- Since the tenant policies gave way to open _app policies (00750), nothing
-- below the service kept roles, memberships, groups or the site's settings
-- to administrators: straight through SQL any member could make themselves
-- an administrator, remove somebody, put themselves in a group that holds a
-- space, or name the site's default theme (#530). As stator_app these are
-- written only behind requireAdmin (removing a member, inviting and removing
-- guests, the hub, anonymous access, public links) or, for the default
-- theme, by somebody the theme service finds to be an administrator by their
-- stored role. Sign-in and its provider groups, created members, join
-- requests, the bootstrap, the seed, the site import and the test reset
-- write as stator_admin, which these do not bind.
CREATE POLICY member_admins_add ON member AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY member_admins_change ON member AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_is_admin(current_actor_id()))
    WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY member_admins_remove ON member AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_is_admin(current_actor_id()));
DROP POLICY member_guests_by_admins_add ON member;
DROP POLICY member_guests_by_admins_remove ON member;

CREATE POLICY groups_admins_add ON groups AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY groups_admins_change ON groups AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_is_admin(current_actor_id()))
    WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY groups_admins_remove ON groups AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_is_admin(current_actor_id()));

CREATE POLICY group_member_admins_add ON group_member AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY group_member_admins_change ON group_member AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_is_admin(current_actor_id()))
    WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY group_member_admins_remove ON group_member AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_is_admin(current_actor_id()));

-- The stored role rather than perm_is_admin, as the theme service asks it;
-- the hub, anonymous access and public links keep their stricter guards,
-- whose errors still come first since the row is not hidden.
CREATE POLICY site_admins_change ON site AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (true)
    WITH CHECK (perm_role_admin(current_actor_id()));

-- A member who unshares their own theme takes it off as the default, which
-- site_admins_change would now refuse; as the definer the clearing
-- runs past the app role's policies, as the foreign key's own clearing does.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION theme_unshared_default() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE site SET default_theme_id = NULL WHERE default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION theme_unshared_default() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE site SET default_theme_id = NULL WHERE default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP POLICY IF EXISTS site_admins_change ON site;
DROP POLICY IF EXISTS group_member_admins_remove ON group_member;
DROP POLICY IF EXISTS group_member_admins_change ON group_member;
DROP POLICY IF EXISTS group_member_admins_add ON group_member;
DROP POLICY IF EXISTS groups_admins_remove ON groups;
DROP POLICY IF EXISTS groups_admins_change ON groups;
DROP POLICY IF EXISTS groups_admins_add ON groups;
CREATE POLICY member_guests_by_admins_add ON member AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (role <> 'guest' OR perm_is_admin(current_actor_id()));
CREATE POLICY member_guests_by_admins_remove ON member AS RESTRICTIVE FOR DELETE TO stator_app
    USING (role <> 'guest' OR perm_is_admin(current_actor_id()));
DROP POLICY IF EXISTS member_admins_remove ON member;
DROP POLICY IF EXISTS member_admins_change ON member;
DROP POLICY IF EXISTS member_admins_add ON member;
