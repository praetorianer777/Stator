-- +goose Up
-- The sign-in provider and its group roles were held only by the tenant and
-- guest policies, so straight through SQL any member could point the
-- organization's sign-in elsewhere or map a group to admin (#401). Only the
-- organization's settings routes touch them as stator_app, behind
-- requireAdmin, which refuses a token limited to spaces as perm_is_admin
-- does. Sign-in, the seed, the test reset and the organization import and
-- export read and write them as stator_admin.
CREATE POLICY oidc_provider_admins ON oidc_provider AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_is_admin(current_actor_id()))
    WITH CHECK (perm_is_admin(current_actor_id()));
CREATE POLICY oidc_group_role_admins ON oidc_group_role AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_is_admin(current_actor_id()))
    WITH CHECK (perm_is_admin(current_actor_id()));

-- +goose Down
DROP POLICY IF EXISTS oidc_group_role_admins ON oidc_group_role;
DROP POLICY IF EXISTS oidc_provider_admins ON oidc_provider;
