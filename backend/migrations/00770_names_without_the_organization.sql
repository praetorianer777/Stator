-- +goose Up
-- The indexes and constraints that 00760 rebuilt without org_id kept names
-- that still said org (#380). Renaming takes a moment's lock and moves no row.
ALTER INDEX api_token_org_user_idx RENAME TO api_token_user_idx;
ALTER INDEX audit_log_org_action_idx RENAME TO audit_log_action_idx;
ALTER INDEX audit_log_org_actor_idx RENAME TO audit_log_actor_idx;
ALTER INDEX audit_log_org_created_idx RENAME TO audit_log_created_idx;
ALTER INDEX groups_org_name_idx RENAME TO groups_name_idx;
ALTER INDEX groups_org_ref_idx RENAME TO groups_ref_idx;
ALTER INDEX page_posts_org_idx RENAME TO page_posts_idx;
ALTER INDEX theme_org_idx RENAME TO theme_shared_idx;

ALTER TABLE member RENAME CONSTRAINT member_org_role_check TO member_role_check;
ALTER TABLE member RENAME CONSTRAINT member_org_role_not_null TO member_role_not_null;
ALTER TABLE oidc_group_role RENAME CONSTRAINT oidc_group_role_org_role_check TO oidc_group_role_role_check;
ALTER TABLE oidc_group_role RENAME CONSTRAINT oidc_group_role_org_role_not_null TO oidc_group_role_role_not_null;

-- +goose Down
ALTER TABLE oidc_group_role RENAME CONSTRAINT oidc_group_role_role_not_null TO oidc_group_role_org_role_not_null;
ALTER TABLE oidc_group_role RENAME CONSTRAINT oidc_group_role_role_check TO oidc_group_role_org_role_check;
ALTER TABLE member RENAME CONSTRAINT member_role_not_null TO member_org_role_not_null;
ALTER TABLE member RENAME CONSTRAINT member_role_check TO member_org_role_check;

ALTER INDEX theme_shared_idx RENAME TO theme_org_idx;
ALTER INDEX page_posts_idx RENAME TO page_posts_org_idx;
ALTER INDEX groups_ref_idx RENAME TO groups_org_ref_idx;
ALTER INDEX groups_name_idx RENAME TO groups_org_name_idx;
ALTER INDEX audit_log_created_idx RENAME TO audit_log_org_created_idx;
ALTER INDEX audit_log_actor_idx RENAME TO audit_log_org_actor_idx;
ALTER INDEX audit_log_action_idx RENAME TO audit_log_org_action_idx;
ALTER INDEX api_token_user_idx RENAME TO api_token_org_user_idx;
