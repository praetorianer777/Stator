-- +goose Up
-- Themes, personal access tokens and tombstones were held only by the tenant
-- policy, so straight through SQL any member could read or change another's
-- themes and tokens, or aim the reaper at another's file (#395). These keep
-- each row to whoever the services already keep it to.

-- A theme is its owner's, seen by everybody once shared, and tidied by an
-- administrator when shared, as lockOwned in the theme service decides. The
-- service asks the stored role, not the token, so perm_role_admin rather than
-- perm_is_admin.
CREATE POLICY theme_viewers ON theme AS RESTRICTIVE FOR SELECT TO stator_app
    USING (owner_id = current_actor_id() OR shared);
CREATE POLICY theme_makers ON theme AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (owner_id = current_actor_id());
CREATE POLICY theme_changers ON theme AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (owner_id = current_actor_id() OR (shared AND perm_role_admin(current_actor_id())))
    WITH CHECK (owner_id = current_actor_id() OR perm_role_admin(current_actor_id()));
CREATE POLICY theme_removers ON theme AS RESTRICTIVE FOR DELETE TO stator_app
    USING (owner_id = current_actor_id() OR (shared AND perm_role_admin(current_actor_id())));

-- A theme's files go with it: seen by whoever sees the theme, and changed by
-- whoever may change it. The theme's own policies hold the subqueries.
CREATE POLICY theme_asset_viewers ON theme_asset AS RESTRICTIVE FOR SELECT TO stator_app
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id));
CREATE POLICY theme_asset_makers ON theme_asset AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                        AND (t.owner_id = current_actor_id() OR (t.shared AND perm_role_admin(current_actor_id())))));
CREATE POLICY theme_asset_changers ON theme_asset AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND perm_role_admin(current_actor_id())))))
    WITH CHECK (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                        AND (t.owner_id = current_actor_id() OR (t.shared AND perm_role_admin(current_actor_id())))));
CREATE POLICY theme_asset_removers ON theme_asset AS RESTRICTIVE FOR DELETE TO stator_app
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.org_id = theme_asset.org_id AND t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND perm_role_admin(current_actor_id())))));

-- A choice is its person's own, of a theme they may see. Everybody's choices
-- stay readable: a theme's list counts the people using it.
CREATE POLICY user_theme_choosers ON user_theme AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (user_id = current_actor_id()
                AND (theme_id IS NULL OR EXISTS (SELECT 1 FROM theme t WHERE t.org_id = user_theme.org_id AND t.id = user_theme.theme_id)));
CREATE POLICY user_theme_changers ON user_theme AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id()
                AND (theme_id IS NULL OR EXISTS (SELECT 1 FROM theme t WHERE t.org_id = user_theme.org_id AND t.id = user_theme.theme_id)));
CREATE POLICY user_theme_removers ON user_theme AS RESTRICTIVE FOR DELETE TO stator_app
    USING (user_id = current_actor_id());

-- A token is its owner's; an administrator lists and revokes anybody's, as
-- the organization's token routes do. Authentication reads as stator_admin.
CREATE POLICY api_token_holders ON api_token AS RESTRICTIVE FOR SELECT TO stator_app
    USING (user_id = current_actor_id() OR perm_is_admin(current_actor_id()));
CREATE POLICY api_token_makers ON api_token AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (user_id = current_actor_id());
CREATE POLICY api_token_keepers ON api_token AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (user_id = current_actor_id()) WITH CHECK (user_id = current_actor_id());
CREATE POLICY api_token_revokers ON api_token AS RESTRICTIVE FOR DELETE TO stator_app
    USING (user_id = current_actor_id() OR perm_is_admin(current_actor_id()));

-- A token's spaces are seen and dropped with the token; adding them is held
-- by api_token_space_added_with_token already.
CREATE POLICY api_token_space_holders ON api_token_space AS RESTRICTIVE FOR SELECT TO stator_app
    USING (EXISTS (SELECT 1 FROM api_token t WHERE t.org_id = api_token_space.org_id AND t.id = api_token_space.token_id));
CREATE POLICY api_token_space_removers ON api_token_space AS RESTRICTIVE FOR DELETE TO stator_app
    USING (EXISTS (SELECT 1 FROM api_token t WHERE t.org_id = api_token_space.org_id AND t.id = api_token_space.token_id));

-- The app role leaves a tombstone only as a file or its preview goes, whose
-- delete their own policies allowed, so a tombstone names the key of a file
-- or a preview whose row, and whose file's row, are gone. Reads past the
-- access policies, since a file the writer cannot see keeps its bytes too.
-- The trigger that leaves them stays the invoker's: as SECURITY DEFINER it
-- would run as an owner the tenant policy binds, and the worker deletes
-- spaces with no tenant named.
-- +goose StatementBegin
CREATE FUNCTION attachment_tombstone_leavable(org uuid, key text) RETURNS boolean
    LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    id_pattern constant text := '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
    prefix constant text := '^org/' || org::text;
    file uuid;
BEGIN
    IF key ~ (prefix || '/page/' || id_pattern || '/' || id_pattern || '$') THEN
        RETURN NOT EXISTS (SELECT 1 FROM attachment a WHERE a.object_key = key);
    END IF;
    file := substring(key FROM prefix || '/preview/(' || id_pattern || ')\.pdf$')::uuid;
    IF file IS NULL THEN
        RETURN false;
    END IF;
    RETURN NOT EXISTS (SELECT 1 FROM attachment_preview p WHERE p.object_key = key)
       AND NOT EXISTS (SELECT 1 FROM attachment a WHERE a.org_id = org AND a.id = file);
END;
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION attachment_tombstone_leavable(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION attachment_tombstone_leavable(uuid, text) TO stator_app, stator_admin;

CREATE POLICY attachment_tombstone_left_by_deletes ON attachment_tombstone AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (attachment_tombstone_leavable(org_id, object_key));

-- +goose Down
DROP POLICY IF EXISTS attachment_tombstone_left_by_deletes ON attachment_tombstone;
DROP FUNCTION IF EXISTS attachment_tombstone_leavable(uuid, text);
DROP POLICY IF EXISTS api_token_space_removers ON api_token_space;
DROP POLICY IF EXISTS api_token_space_holders ON api_token_space;
DROP POLICY IF EXISTS api_token_revokers ON api_token;
DROP POLICY IF EXISTS api_token_keepers ON api_token;
DROP POLICY IF EXISTS api_token_makers ON api_token;
DROP POLICY IF EXISTS api_token_holders ON api_token;
DROP POLICY IF EXISTS user_theme_removers ON user_theme;
DROP POLICY IF EXISTS user_theme_changers ON user_theme;
DROP POLICY IF EXISTS user_theme_choosers ON user_theme;
DROP POLICY IF EXISTS theme_asset_removers ON theme_asset;
DROP POLICY IF EXISTS theme_asset_changers ON theme_asset;
DROP POLICY IF EXISTS theme_asset_makers ON theme_asset;
DROP POLICY IF EXISTS theme_asset_viewers ON theme_asset;
DROP POLICY IF EXISTS theme_removers ON theme;
DROP POLICY IF EXISTS theme_changers ON theme;
DROP POLICY IF EXISTS theme_makers ON theme;
DROP POLICY IF EXISTS theme_viewers ON theme;
