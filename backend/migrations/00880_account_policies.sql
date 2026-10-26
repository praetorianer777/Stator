-- +goose Up
-- Accounts, pending sign-ins and join requests were held below the service
-- only against anonymous readers and guests (#535). As stator_app the
-- service writes none of the three but the join requests a guest invitation
-- answers, behind the site administrator check. Sign-in and partner sign-in,
-- first-login provisioning, created members, join requests and their
-- answers, the profile routes, the bootstrap, the seed, the site import and
-- the test reset all write as stator_admin, which these do not bind.

-- With the row left visible, a change to somebody else's account is refused
-- with an error rather than reaching no rows, as the readers guard does.
CREATE POLICY app_user_own_change ON app_user AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (true)
    WITH CHECK (id = current_actor_id());
CREATE POLICY app_user_none_added ON app_user AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (false);
CREATE POLICY app_user_none_removed ON app_user AS RESTRICTIVE FOR DELETE TO stator_app
    USING (false);
-- The address ties an account to the provider, the password and whether the
-- account is switched on are the service's to set, and the name and picture
-- come from the provider and are shown to everybody; a person keeps only
-- their own settings.
REVOKE UPDATE ON app_user FROM stator_app;
GRANT UPDATE (timezone, locale, show_in_readers) ON app_user TO stator_app;

CREATE POLICY oidc_login_not_app ON oidc_login AS RESTRICTIVE FOR ALL TO stator_app
    USING (false)
    WITH CHECK (false);

-- The requester is no member and has no session, so nothing is theirs here.
CREATE POLICY join_request_admins ON join_request AS RESTRICTIVE FOR ALL TO stator_app
    USING (perm_is_admin(current_actor_id()))
    WITH CHECK (perm_is_admin(current_actor_id()));

-- +goose Down
DROP POLICY IF EXISTS join_request_admins ON join_request;
DROP POLICY IF EXISTS oidc_login_not_app ON oidc_login;
REVOKE UPDATE (timezone, locale, show_in_readers) ON app_user FROM stator_app;
GRANT UPDATE ON app_user TO stator_app;
DROP POLICY IF EXISTS app_user_none_removed ON app_user;
DROP POLICY IF EXISTS app_user_none_added ON app_user;
DROP POLICY IF EXISTS app_user_own_change ON app_user;
