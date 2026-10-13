-- +goose Up
-- An instance serves one organization (#380), so the schema stops naming it:
-- org_id goes from every table, with the keys, indexes and foreign keys
-- that led with it, and the tables named after the organization take the
-- site's name. Row level security stays as it was: every table still forces
-- it, the restrictive access policies decide who reaches a row, and the
-- permissive _app and _admin_bypass policies are untouched.

-- Object keys stop being derived from org_id. Every key already stored stays
-- as it is, so no file in the bucket moves; a new row gets a key without the
-- organization from the triggers below, which the app cannot write past.
-- After a down the columns are plain already and the triggers are there,
-- hence IF EXISTS and OR REPLACE.
ALTER TABLE attachment ALTER COLUMN object_key DROP EXPRESSION IF EXISTS;
ALTER TABLE attachment_preview ALTER COLUMN object_key DROP EXPRESSION IF EXISTS;
ALTER TABLE space_export ALTER COLUMN object_key DROP EXPRESSION IF EXISTS;
ALTER TABLE space_import ALTER COLUMN object_key DROP EXPRESSION IF EXISTS;
ALTER TABLE word_import ALTER COLUMN object_key DROP EXPRESSION IF EXISTS;
ALTER TABLE org_export ALTER COLUMN object_key DROP EXPRESSION IF EXISTS;
ALTER TABLE org_import ALTER COLUMN object_key DROP EXPRESSION IF EXISTS;
ALTER TABLE attachment ALTER COLUMN object_key SET NOT NULL;
ALTER TABLE attachment_preview ALTER COLUMN object_key SET NOT NULL;
ALTER TABLE space_export ALTER COLUMN object_key SET NOT NULL;
ALTER TABLE space_import ALTER COLUMN object_key SET NOT NULL;
ALTER TABLE word_import ALTER COLUMN object_key SET NOT NULL;
ALTER TABLE org_export ALTER COLUMN object_key SET NOT NULL;
ALTER TABLE org_import ALTER COLUMN object_key SET NOT NULL;

-- Theme files and the logo had their keys built in Go from the
-- organization's id; they are stored now, filled from that same formula, so
-- the files already there stay where the rows say they are.
ALTER TABLE theme_asset ADD COLUMN object_key text;
UPDATE theme_asset SET object_key = 'org/' || org_id::text || '/theme/' || theme_id::text || '/' || id::text;
ALTER TABLE theme_asset ALTER COLUMN object_key SET NOT NULL;
ALTER TABLE org_brand ADD COLUMN logo_key text;
UPDATE org_brand SET logo_key = 'org/' || org_id::text || '/brand/logo-' || logo_version::text WHERE logo_type IS NOT NULL;
ALTER TABLE org_brand ADD CONSTRAINT org_brand_logo_keyed CHECK ((logo_type IS NULL) = (logo_key IS NULL));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_object_key() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.object_key := 'page/' || NEW.page_id::text || '/' || NEW.id::text;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_preview_object_key() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.object_key := 'preview/' || NEW.attachment_id::text || '.pdf';
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION theme_asset_object_key() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.object_key := 'theme/' || NEW.theme_id::text || '/' || NEW.id::text;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- A transfer's archive is named by its job; the trigger names the folder.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION job_object_key() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.object_key := TG_ARGV[0] || '/' || NEW.id::text;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- A generated key could not be written at all. The reaper and every reader
-- trust the key, so one written past the trigger would let a row name
-- somebody else's file.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION object_key_stays() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.object_key IS DISTINCT FROM OLD.object_key THEN
        RAISE EXCEPTION 'an object key never changes'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'object_key_stays';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- The logo's key follows its version, which every new picture raises.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION brand_logo_key() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.logo_type IS NULL THEN
        NEW.logo_key := NULL;
    ELSIF TG_OP = 'INSERT' OR OLD.logo_key IS NULL OR NEW.logo_version IS DISTINCT FROM OLD.logo_version THEN
        NEW.logo_key := 'brand/logo-' || NEW.logo_version::text;
    ELSE
        NEW.logo_key := OLD.logo_key;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER attachment_object_key BEFORE INSERT ON attachment
    FOR EACH ROW EXECUTE FUNCTION attachment_object_key();
CREATE OR REPLACE TRIGGER attachment_preview_object_key BEFORE INSERT ON attachment_preview
    FOR EACH ROW EXECUTE FUNCTION attachment_preview_object_key();
CREATE OR REPLACE TRIGGER theme_asset_object_key BEFORE INSERT ON theme_asset
    FOR EACH ROW EXECUTE FUNCTION theme_asset_object_key();
CREATE OR REPLACE TRIGGER space_export_object_key BEFORE INSERT ON space_export
    FOR EACH ROW EXECUTE FUNCTION job_object_key('space-export');
CREATE OR REPLACE TRIGGER space_import_object_key BEFORE INSERT ON space_import
    FOR EACH ROW EXECUTE FUNCTION job_object_key('space-import');
CREATE OR REPLACE TRIGGER word_import_object_key BEFORE INSERT ON word_import
    FOR EACH ROW EXECUTE FUNCTION job_object_key('word-import');
CREATE OR REPLACE TRIGGER org_export_object_key BEFORE INSERT ON org_export
    FOR EACH ROW EXECUTE FUNCTION job_object_key('site-export');
CREATE OR REPLACE TRIGGER org_import_object_key BEFORE INSERT ON org_import
    FOR EACH ROW EXECUTE FUNCTION job_object_key('site-import');
CREATE OR REPLACE TRIGGER attachment_object_key_stays BEFORE UPDATE OF object_key ON attachment
    FOR EACH ROW EXECUTE FUNCTION object_key_stays();
CREATE OR REPLACE TRIGGER attachment_preview_object_key_stays BEFORE UPDATE OF object_key ON attachment_preview
    FOR EACH ROW EXECUTE FUNCTION object_key_stays();
CREATE OR REPLACE TRIGGER theme_asset_object_key_stays BEFORE UPDATE OF object_key ON theme_asset
    FOR EACH ROW EXECUTE FUNCTION object_key_stays();
CREATE OR REPLACE TRIGGER space_export_object_key_stays BEFORE UPDATE OF object_key ON space_export
    FOR EACH ROW EXECUTE FUNCTION object_key_stays();
CREATE OR REPLACE TRIGGER space_import_object_key_stays BEFORE UPDATE OF object_key ON space_import
    FOR EACH ROW EXECUTE FUNCTION object_key_stays();
CREATE OR REPLACE TRIGGER word_import_object_key_stays BEFORE UPDATE OF object_key ON word_import
    FOR EACH ROW EXECUTE FUNCTION object_key_stays();
CREATE OR REPLACE TRIGGER org_export_object_key_stays BEFORE UPDATE OF object_key ON org_export
    FOR EACH ROW EXECUTE FUNCTION object_key_stays();
CREATE OR REPLACE TRIGGER org_import_object_key_stays BEFORE UPDATE OF object_key ON org_import
    FOR EACH ROW EXECUTE FUNCTION object_key_stays();
CREATE OR REPLACE TRIGGER org_brand_logo_key BEFORE INSERT OR UPDATE ON org_brand
    FOR EACH ROW EXECUTE FUNCTION brand_logo_key();

-- The reaper deletes whatever a tombstone names. The prefix check kept a
-- tombstone inside its organization's folder; with one organization the
-- policy's own check of the key's shape (attachment_tombstone_leavable) is
-- what holds, and it knows both the old and the new shape.
ALTER TABLE attachment_tombstone DROP CONSTRAINT attachment_tombstone_own_prefix;

-- A session no longer belongs to an organization, so it has none to keep.
DROP TRIGGER session_stays_home ON user_session;
DROP FUNCTION session_stays_home();
DROP FUNCTION session_reaches(text, uuid, uuid, uuid);
ALTER TABLE user_session DROP COLUMN current_org_id, DROP COLUMN proof_org_id;

-- Two functions took the organization as an argument; they lose it, and the
-- policies that call them follow.
-- +goose StatementBegin
CREATE FUNCTION attachment_tombstone_leavable(key text) RETURNS boolean
    LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    id_pattern constant text := '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
    -- Keys written before the organization left the schema start with it.
    prefix constant text := '^(?:org/' || id_pattern || '/)?';
    file uuid;
BEGIN
    IF key ~ (prefix || 'page/' || id_pattern || '/' || id_pattern || '$') THEN
        RETURN NOT EXISTS (SELECT 1 FROM attachment a WHERE a.object_key = key);
    END IF;
    file := substring(key FROM prefix || 'preview/(' || id_pattern || ')\.pdf$')::uuid;
    IF file IS NULL THEN
        RETURN false;
    END IF;
    RETURN NOT EXISTS (SELECT 1 FROM attachment_preview p WHERE p.object_key = key)
       AND NOT EXISTS (SELECT 1 FROM attachment a WHERE a.id = file);
END;
$$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION attachment_tombstone_leavable(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION attachment_tombstone_leavable(text) TO stator_app, stator_admin;

-- +goose StatementBegin
CREATE FUNCTION page_version_open(page_id uuid, number integer) RETURNS boolean
    LANGUAGE sql STABLE
AS $$
    SELECT EXISTS (
        SELECT 1 FROM page_version v JOIN page p ON p.id = v.page_id
        WHERE v.page_id = page_version_open.page_id AND v.number = page_version_open.number
          AND v.live AND p.mode = 'live' AND p.version = v.number
          AND v.created_at > now() - live_version_span());
$$;
-- +goose StatementEnd

-- Policies that named org_id, without it.
ALTER POLICY api_token_space_holders ON api_token_space
    USING (EXISTS (SELECT 1 FROM api_token t WHERE t.id = api_token_space.token_id));
ALTER POLICY api_token_space_removers ON api_token_space
    USING (EXISTS (SELECT 1 FROM api_token t WHERE t.id = api_token_space.token_id));
ALTER POLICY attachment_preview_makers ON attachment_preview
    WITH CHECK (EXISTS (SELECT 1 FROM attachment a WHERE a.id = attachment_preview.attachment_id));
ALTER POLICY attachment_preview_viewers ON attachment_preview
    USING (EXISTS (SELECT 1 FROM attachment a WHERE a.id = attachment_preview.attachment_id));
ALTER POLICY attachment_tombstone_left_by_deletes ON attachment_tombstone
    WITH CHECK (attachment_tombstone_leavable(object_key));
ALTER POLICY page_version_amenders ON page_version
    USING (live AND created_at > now() - live_version_span()
           AND perm_page_editable(page_id, current_actor_id())
           AND EXISTS (SELECT 1 FROM page p
                       WHERE p.id = page_version.page_id AND p.mode = 'live' AND p.version = page_version.number));
ALTER POLICY page_version_live_on_live_pages ON page_version
    WITH CHECK (NOT live OR EXISTS (SELECT 1 FROM page p WHERE p.id = page_version.page_id AND p.mode = 'live'));
ALTER POLICY page_version_editor_savers ON page_version_editor
    WITH CHECK (user_id = current_actor_id() AND perm_page_editable(page_id, current_actor_id())
                AND page_version_open(page_id, number));
ALTER POLICY theme_asset_viewers ON theme_asset
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.id = theme_asset.theme_id));
ALTER POLICY theme_asset_makers ON theme_asset
    WITH CHECK (EXISTS (SELECT 1 FROM theme t WHERE t.id = theme_asset.theme_id
                        AND (t.owner_id = current_actor_id() OR (t.shared AND perm_role_admin(current_actor_id())))));
ALTER POLICY theme_asset_changers ON theme_asset
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND perm_role_admin(current_actor_id())))))
    WITH CHECK (EXISTS (SELECT 1 FROM theme t WHERE t.id = theme_asset.theme_id
                        AND (t.owner_id = current_actor_id() OR (t.shared AND perm_role_admin(current_actor_id())))));
ALTER POLICY theme_asset_removers ON theme_asset
    USING (EXISTS (SELECT 1 FROM theme t WHERE t.id = theme_asset.theme_id
                   AND (t.owner_id = current_actor_id() OR (t.shared AND perm_role_admin(current_actor_id())))));
ALTER POLICY user_theme_choosers ON user_theme
    WITH CHECK (user_id = current_actor_id()
                AND (theme_id IS NULL OR EXISTS (SELECT 1 FROM theme t WHERE t.id = user_theme.theme_id)));
ALTER POLICY user_theme_changers ON user_theme
    WITH CHECK (user_id = current_actor_id()
                AND (theme_id IS NULL OR EXISTS (SELECT 1 FROM theme t WHERE t.id = user_theme.theme_id)));

DROP FUNCTION attachment_tombstone_leavable(uuid, text);
DROP FUNCTION page_version_open(uuid, uuid, integer);
-- Returned org_id among its columns, so it is made again below.
DROP FUNCTION page_search_rows(tsquery);

-- Every foreign key that names the organization goes first, then the keys
-- they lean on, then the indexes; each comes back below without it.
ALTER TABLE api_token DROP CONSTRAINT api_token_org_id_fkey;
ALTER TABLE api_token DROP CONSTRAINT api_token_org_id_user_id_fkey;
ALTER TABLE api_token_space DROP CONSTRAINT api_token_space_org_id_space_id_fkey;
ALTER TABLE api_token_space DROP CONSTRAINT api_token_space_org_id_token_id_fkey;
ALTER TABLE armature_connection DROP CONSTRAINT armature_connection_org_id_fkey;
ALTER TABLE armature_remote_link DROP CONSTRAINT armature_remote_link_org_id_fkey;
ALTER TABLE armature_token DROP CONSTRAINT armature_token_org_id_fkey;
ALTER TABLE armature_token DROP CONSTRAINT armature_token_org_id_user_id_fkey;
ALTER TABLE attachment DROP CONSTRAINT attachment_org_id_fkey;
ALTER TABLE attachment DROP CONSTRAINT attachment_org_id_page_id_fkey;
ALTER TABLE attachment_preview DROP CONSTRAINT attachment_preview_org_id_attachment_id_fkey;
ALTER TABLE audit_log DROP CONSTRAINT audit_log_org_id_fkey;
ALTER TABLE calendar DROP CONSTRAINT calendar_org_id_space_id_fkey;
ALTER TABLE calendar_event DROP CONSTRAINT calendar_event_org_id_space_id_calendar_id_fkey;
ALTER TABLE comment DROP CONSTRAINT comment_org_id_thread_id_page_id_fkey;
ALTER TABLE comment_thread DROP CONSTRAINT comment_thread_org_id_page_id_fkey;
ALTER TABLE example_job DROP CONSTRAINT example_job_org_id_fkey;
ALTER TABLE example_page DROP CONSTRAINT example_page_org_id_fkey;
ALTER TABLE global_grant DROP CONSTRAINT global_grant_org_id_fkey;
ALTER TABLE global_grant DROP CONSTRAINT global_grant_org_id_group_id_fkey;
ALTER TABLE global_grant DROP CONSTRAINT global_grant_org_id_user_id_fkey;
ALTER TABLE group_member DROP CONSTRAINT group_member_org_id_fkey;
ALTER TABLE groups DROP CONSTRAINT groups_org_id_fkey;
ALTER TABLE notification DROP CONSTRAINT notification_comment_fk;
ALTER TABLE notification DROP CONSTRAINT notification_org_id_page_id_fkey;
ALTER TABLE notification DROP CONSTRAINT notification_org_id_user_id_fkey;
ALTER TABLE notification DROP CONSTRAINT notification_thread_fk;
ALTER TABLE notification_digest DROP CONSTRAINT notification_digest_org_id_notification_id_fkey;
ALTER TABLE notification_preference DROP CONSTRAINT notification_preference_org_id_user_id_fkey;
ALTER TABLE oidc_group_role DROP CONSTRAINT oidc_group_role_org_id_fkey;
ALTER TABLE oidc_login DROP CONSTRAINT oidc_login_org_id_fkey;
ALTER TABLE oidc_provider DROP CONSTRAINT oidc_provider_org_id_fkey;
ALTER TABLE org DROP CONSTRAINT org_hub_page_fkey;
ALTER TABLE org_brand DROP CONSTRAINT org_brand_org_id_fkey;
ALTER TABLE org_export DROP CONSTRAINT org_export_org_id_fkey;
ALTER TABLE org_import DROP CONSTRAINT org_import_org_id_fkey;
ALTER TABLE org_join_request DROP CONSTRAINT org_join_request_org_id_fkey;
ALTER TABLE org_member DROP CONSTRAINT org_member_guest_space_fkey;
ALTER TABLE org_member DROP CONSTRAINT org_member_org_id_fkey;
ALTER TABLE outbox_event DROP CONSTRAINT outbox_event_org_id_fkey;
ALTER TABLE page DROP CONSTRAINT page_archive_fkey;
ALTER TABLE page DROP CONSTRAINT page_org_id_fkey;
ALTER TABLE page DROP CONSTRAINT page_org_id_space_id_fkey;
ALTER TABLE page DROP CONSTRAINT page_parent_fkey;
ALTER TABLE page DROP CONSTRAINT page_trash_fkey;
ALTER TABLE page_collab DROP CONSTRAINT page_collab_org_id_page_id_fkey;
ALTER TABLE page_collab_update DROP CONSTRAINT page_collab_update_org_id_page_id_room_id_fkey;
ALTER TABLE page_draft DROP CONSTRAINT page_draft_org_id_page_id_fkey;
ALTER TABLE page_draft DROP CONSTRAINT page_draft_org_id_user_id_fkey;
ALTER TABLE page_label DROP CONSTRAINT page_label_org_id_page_id_fkey;
ALTER TABLE page_link DROP CONSTRAINT page_link_org_id_created_by_fkey;
ALTER TABLE page_link DROP CONSTRAINT page_link_org_id_page_id_fkey;
ALTER TABLE page_link DROP CONSTRAINT page_link_org_id_revoked_by_fkey;
ALTER TABLE page_owner DROP CONSTRAINT page_owner_org_id_page_id_fkey;
ALTER TABLE page_owner DROP CONSTRAINT page_owner_org_id_user_id_fkey;
ALTER TABLE page_read_ask DROP CONSTRAINT page_read_ask_org_id_page_id_fkey;
ALTER TABLE page_read_confirmation DROP CONSTRAINT page_read_confirmation_org_id_page_id_fkey;
ALTER TABLE page_read_confirmation DROP CONSTRAINT page_read_confirmation_org_id_user_id_fkey;
ALTER TABLE page_restriction DROP CONSTRAINT page_restriction_org_id_group_id_fkey;
ALTER TABLE page_restriction DROP CONSTRAINT page_restriction_org_id_page_id_fkey;
ALTER TABLE page_restriction DROP CONSTRAINT page_restriction_org_id_user_id_fkey;
ALTER TABLE page_review DROP CONSTRAINT page_review_org_id_page_id_fkey;
ALTER TABLE page_review_decision DROP CONSTRAINT page_review_decision_org_id_review_id_fkey;
ALTER TABLE page_review_subject DROP CONSTRAINT page_review_subject_org_id_group_id_fkey;
ALTER TABLE page_review_subject DROP CONSTRAINT page_review_subject_org_id_review_id_fkey;
ALTER TABLE page_review_subject DROP CONSTRAINT page_review_subject_org_id_user_id_fkey;
ALTER TABLE page_schedule DROP CONSTRAINT page_schedule_org_id_page_id_user_id_fkey;
ALTER TABLE page_share DROP CONSTRAINT page_share_org_id_page_id_fkey;
ALTER TABLE page_share DROP CONSTRAINT page_share_org_id_sharer_id_fkey;
ALTER TABLE page_share_recipient DROP CONSTRAINT page_share_recipient_org_id_share_id_fkey;
ALTER TABLE page_share_recipient DROP CONSTRAINT page_share_recipient_org_id_user_id_fkey;
ALTER TABLE page_task DROP CONSTRAINT page_task_org_id_assignee_id_fkey;
ALTER TABLE page_task DROP CONSTRAINT page_task_org_id_page_id_fkey;
ALTER TABLE page_template DROP CONSTRAINT page_template_org_id_fkey;
ALTER TABLE page_template DROP CONSTRAINT page_template_org_id_space_id_fkey;
ALTER TABLE page_verification DROP CONSTRAINT page_verification_org_id_page_id_fkey;
ALTER TABLE page_version DROP CONSTRAINT page_version_org_id_page_id_fkey;
ALTER TABLE page_version DROP CONSTRAINT page_version_review_fk;
ALTER TABLE page_version_editor DROP CONSTRAINT page_version_editor_org_id_page_id_number_fkey;
ALTER TABLE page_view DROP CONSTRAINT page_view_org_id_page_id_fkey;
ALTER TABLE page_view DROP CONSTRAINT page_view_org_id_user_id_fkey;
ALTER TABLE page_view_tally DROP CONSTRAINT page_view_tally_org_id_page_id_fkey;
ALTER TABLE page_visit DROP CONSTRAINT page_visit_org_id_page_id_fkey;
ALTER TABLE page_visit DROP CONSTRAINT page_visit_org_id_user_id_fkey;
ALTER TABLE reaction DROP CONSTRAINT reaction_org_id_comment_id_page_id_fkey;
ALTER TABLE reaction DROP CONSTRAINT reaction_org_id_page_id_fkey;
ALTER TABLE reaction DROP CONSTRAINT reaction_org_id_user_id_fkey;
ALTER TABLE review_rule DROP CONSTRAINT review_rule_org_id_page_id_fkey;
ALTER TABLE review_rule DROP CONSTRAINT review_rule_org_id_space_id_fkey;
ALTER TABLE review_rule_subject DROP CONSTRAINT review_rule_subject_org_id_group_id_fkey;
ALTER TABLE review_rule_subject DROP CONSTRAINT review_rule_subject_org_id_rule_id_fkey;
ALTER TABLE review_rule_subject DROP CONSTRAINT review_rule_subject_org_id_user_id_fkey;
ALTER TABLE space DROP CONSTRAINT space_home_page_fkey;
ALTER TABLE space DROP CONSTRAINT space_org_id_fkey;
ALTER TABLE space_export DROP CONSTRAINT space_export_org_id_fkey;
ALTER TABLE space_export DROP CONSTRAINT space_export_org_id_space_id_fkey;
ALTER TABLE space_grant DROP CONSTRAINT space_grant_org_id_group_id_fkey;
ALTER TABLE space_grant DROP CONSTRAINT space_grant_org_id_space_id_fkey;
ALTER TABLE space_grant DROP CONSTRAINT space_grant_org_id_user_id_fkey;
ALTER TABLE space_import DROP CONSTRAINT space_import_org_id_fkey;
ALTER TABLE space_import DROP CONSTRAINT space_import_org_id_space_id_fkey;
ALTER TABLE space_shortcut DROP CONSTRAINT space_shortcut_org_id_page_id_fkey;
ALTER TABLE space_shortcut DROP CONSTRAINT space_shortcut_org_id_space_id_fkey;
ALTER TABLE star DROP CONSTRAINT star_org_id_page_id_fkey;
ALTER TABLE star DROP CONSTRAINT star_org_id_space_id_fkey;
ALTER TABLE star DROP CONSTRAINT star_org_id_user_id_fkey;
ALTER TABLE theme DROP CONSTRAINT theme_org_id_fkey;
ALTER TABLE theme_asset DROP CONSTRAINT theme_asset_org_id_fkey;
ALTER TABLE theme_asset DROP CONSTRAINT theme_asset_org_id_theme_id_fkey;
ALTER TABLE user_theme DROP CONSTRAINT user_theme_org_id_fkey;
ALTER TABLE user_theme DROP CONSTRAINT user_theme_org_id_theme_id_fkey;
ALTER TABLE watch DROP CONSTRAINT watch_org_id_page_id_fkey;
ALTER TABLE watch DROP CONSTRAINT watch_org_id_space_id_fkey;
ALTER TABLE watch DROP CONSTRAINT watch_org_id_user_id_fkey;
ALTER TABLE watch_optout DROP CONSTRAINT watch_optout_org_id_page_id_fkey;
ALTER TABLE watch_optout DROP CONSTRAINT watch_optout_org_id_user_id_fkey;
ALTER TABLE webhook_delivery DROP CONSTRAINT webhook_delivery_org_id_endpoint_id_fkey;
ALTER TABLE webhook_endpoint DROP CONSTRAINT webhook_endpoint_org_id_fkey;
ALTER TABLE webhook_endpoint DROP CONSTRAINT webhook_endpoint_org_id_owner_id_fkey;
ALTER TABLE word_import DROP CONSTRAINT word_import_org_id_fkey;
ALTER TABLE word_import DROP CONSTRAINT word_import_org_id_parent_id_fkey;
ALTER TABLE api_token DROP CONSTRAINT api_token_org_id_key;
ALTER TABLE attachment DROP CONSTRAINT attachment_org_id_key;
ALTER TABLE calendar DROP CONSTRAINT calendar_org_id_space_id_id_key;
ALTER TABLE comment DROP CONSTRAINT comment_on_page;
ALTER TABLE comment_thread DROP CONSTRAINT comment_thread_org_id_id_page_id_key;
ALTER TABLE groups DROP CONSTRAINT groups_org_id_key;
ALTER TABLE page DROP CONSTRAINT page_org_id_key;
ALTER TABLE page DROP CONSTRAINT page_space_id_key;
ALTER TABLE page_collab DROP CONSTRAINT page_collab_org_id_page_id_id_key;
ALTER TABLE page_review_decision DROP CONSTRAINT page_review_decision_org_id_review_id_reviewer_id_key;
ALTER TABLE page_task DROP CONSTRAINT page_task_org_id_page_id_task_id_key;
ALTER TABLE reaction DROP CONSTRAINT reaction_org_id_page_id_comment_id_user_id_emoji_key;
ALTER TABLE space DROP CONSTRAINT space_key_unique;
ALTER TABLE space DROP CONSTRAINT space_org_id_key;
ALTER TABLE theme DROP CONSTRAINT theme_org_id_key;
ALTER TABLE webhook_endpoint DROP CONSTRAINT webhook_endpoint_org_id_id_key;
ALTER TABLE armature_connection DROP CONSTRAINT armature_connection_pkey;
ALTER TABLE armature_remote_link DROP CONSTRAINT armature_remote_link_pkey;
ALTER TABLE armature_token DROP CONSTRAINT armature_token_pkey;
ALTER TABLE calendar DROP CONSTRAINT calendar_pkey;
ALTER TABLE calendar_event DROP CONSTRAINT calendar_event_pkey;
ALTER TABLE comment DROP CONSTRAINT comment_pkey;
ALTER TABLE comment_thread DROP CONSTRAINT comment_thread_pkey;
ALTER TABLE global_grant DROP CONSTRAINT global_grant_pkey;
ALTER TABLE notification DROP CONSTRAINT notification_pkey;
ALTER TABLE notification_digest DROP CONSTRAINT notification_digest_pkey;
ALTER TABLE notification_preference DROP CONSTRAINT notification_preference_pkey;
ALTER TABLE oidc_group_role DROP CONSTRAINT oidc_group_role_pkey;
ALTER TABLE oidc_provider DROP CONSTRAINT oidc_provider_pkey;
ALTER TABLE org_brand DROP CONSTRAINT org_brand_pkey;
ALTER TABLE org_join_request DROP CONSTRAINT org_join_request_pkey;
ALTER TABLE org_member DROP CONSTRAINT org_member_pkey;
ALTER TABLE page_collab DROP CONSTRAINT page_collab_pkey;
ALTER TABLE page_collab_update DROP CONSTRAINT page_collab_update_pkey;
ALTER TABLE page_draft DROP CONSTRAINT page_draft_pkey;
ALTER TABLE page_label DROP CONSTRAINT page_label_pkey;
ALTER TABLE page_link DROP CONSTRAINT page_link_pkey;
ALTER TABLE page_owner DROP CONSTRAINT page_owner_pkey;
ALTER TABLE page_read_ask DROP CONSTRAINT page_read_ask_pkey;
ALTER TABLE page_read_confirmation DROP CONSTRAINT page_read_confirmation_pkey;
ALTER TABLE page_restriction DROP CONSTRAINT page_restriction_pkey;
ALTER TABLE page_review DROP CONSTRAINT page_review_pkey;
ALTER TABLE page_review_decision DROP CONSTRAINT page_review_decision_pkey;
ALTER TABLE page_schedule DROP CONSTRAINT page_schedule_pkey;
ALTER TABLE page_share DROP CONSTRAINT page_share_pkey;
ALTER TABLE page_share_recipient DROP CONSTRAINT page_share_recipient_pkey;
ALTER TABLE page_task DROP CONSTRAINT page_task_pkey;
ALTER TABLE page_verification DROP CONSTRAINT page_verification_pkey;
ALTER TABLE page_version DROP CONSTRAINT page_version_pkey;
ALTER TABLE page_version_editor DROP CONSTRAINT page_version_editor_pkey;
ALTER TABLE page_view_tally DROP CONSTRAINT page_view_tally_pkey;
ALTER TABLE page_visit DROP CONSTRAINT page_visit_pkey;
ALTER TABLE reaction DROP CONSTRAINT reaction_pkey;
ALTER TABLE review_rule DROP CONSTRAINT review_rule_pkey;
ALTER TABLE space_grant DROP CONSTRAINT space_grant_pkey;
ALTER TABLE space_shortcut DROP CONSTRAINT space_shortcut_pkey;
ALTER TABLE star DROP CONSTRAINT star_pkey;
ALTER TABLE user_theme DROP CONSTRAINT user_theme_pkey;
ALTER TABLE watch DROP CONSTRAINT watch_pkey;
ALTER TABLE watch_optout DROP CONSTRAINT watch_optout_pkey;
DROP INDEX attachment_version_idx;
DROP INDEX calendar_name_idx;
DROP INDEX example_job_one_open;
DROP INDEX global_grant_unique_idx;
DROP INDEX groups_org_name_idx;
DROP INDEX groups_org_ref_idx;
DROP INDEX oidc_group_role_ref_idx;
DROP INDEX page_one_root_idx;
DROP INDEX page_restriction_unique_idx;
DROP INDEX page_review_pending_idx;
DROP INDEX page_review_subject_idx;
DROP INDEX page_template_name_idx;
DROP INDEX page_view_once_idx;
DROP INDEX review_rule_page_idx;
DROP INDEX review_rule_space_idx;
DROP INDEX review_rule_subject_idx;
DROP INDEX space_one_example;
DROP INDEX space_one_personal;
DROP INDEX space_grant_unique_idx;
DROP INDEX star_one_per_page_idx;
DROP INDEX star_one_per_space_idx;
DROP INDEX theme_owner_name_idx;
DROP INDEX watch_one_per_page_idx;
DROP INDEX watch_one_per_space_idx;
DROP INDEX webhook_endpoint_name_idx;

-- org_id goes, and with it its default, current_org_id().
ALTER TABLE api_token DROP COLUMN org_id;
ALTER TABLE api_token_space DROP COLUMN org_id;
ALTER TABLE armature_connection DROP COLUMN org_id;
ALTER TABLE armature_remote_link DROP COLUMN org_id;
ALTER TABLE armature_token DROP COLUMN org_id;
ALTER TABLE attachment DROP COLUMN org_id;
ALTER TABLE attachment_preview DROP COLUMN org_id;
ALTER TABLE attachment_tombstone DROP COLUMN org_id;
ALTER TABLE audit_log DROP COLUMN org_id;
ALTER TABLE calendar DROP COLUMN org_id;
ALTER TABLE calendar_event DROP COLUMN org_id;
ALTER TABLE comment DROP COLUMN org_id;
ALTER TABLE comment_thread DROP COLUMN org_id;
ALTER TABLE example_job DROP COLUMN org_id;
ALTER TABLE example_page DROP COLUMN org_id;
ALTER TABLE global_grant DROP COLUMN org_id;
ALTER TABLE group_member DROP COLUMN org_id;
ALTER TABLE groups DROP COLUMN org_id;
ALTER TABLE notification DROP COLUMN org_id;
ALTER TABLE notification_digest DROP COLUMN org_id;
ALTER TABLE notification_preference DROP COLUMN org_id;
ALTER TABLE oidc_group_role DROP COLUMN org_id;
ALTER TABLE oidc_login DROP COLUMN org_id;
ALTER TABLE oidc_provider DROP COLUMN org_id;
ALTER TABLE org_brand DROP COLUMN org_id;
ALTER TABLE org_export DROP COLUMN org_id;
ALTER TABLE org_import DROP COLUMN org_id;
ALTER TABLE org_join_request DROP COLUMN org_id;
ALTER TABLE org_member DROP COLUMN org_id;
ALTER TABLE outbox_event DROP COLUMN org_id;
ALTER TABLE page DROP COLUMN org_id;
ALTER TABLE page_collab DROP COLUMN org_id;
ALTER TABLE page_collab_update DROP COLUMN org_id;
ALTER TABLE page_draft DROP COLUMN org_id;
ALTER TABLE page_label DROP COLUMN org_id;
ALTER TABLE page_link DROP COLUMN org_id;
ALTER TABLE page_owner DROP COLUMN org_id;
ALTER TABLE page_read_ask DROP COLUMN org_id;
ALTER TABLE page_read_confirmation DROP COLUMN org_id;
ALTER TABLE page_restriction DROP COLUMN org_id;
ALTER TABLE page_review DROP COLUMN org_id;
ALTER TABLE page_review_decision DROP COLUMN org_id;
ALTER TABLE page_review_subject DROP COLUMN org_id;
ALTER TABLE page_schedule DROP COLUMN org_id;
ALTER TABLE page_share DROP COLUMN org_id;
ALTER TABLE page_share_recipient DROP COLUMN org_id;
ALTER TABLE page_task DROP COLUMN org_id;
ALTER TABLE page_template DROP COLUMN org_id;
ALTER TABLE page_verification DROP COLUMN org_id;
ALTER TABLE page_version DROP COLUMN org_id;
ALTER TABLE page_version_editor DROP COLUMN org_id;
ALTER TABLE page_view DROP COLUMN org_id;
ALTER TABLE page_view_tally DROP COLUMN org_id;
ALTER TABLE page_visit DROP COLUMN org_id;
ALTER TABLE reaction DROP COLUMN org_id;
ALTER TABLE review_rule DROP COLUMN org_id;
ALTER TABLE review_rule_subject DROP COLUMN org_id;
ALTER TABLE space DROP COLUMN org_id;
ALTER TABLE space_export DROP COLUMN org_id;
ALTER TABLE space_grant DROP COLUMN org_id;
ALTER TABLE space_import DROP COLUMN org_id;
ALTER TABLE space_shortcut DROP COLUMN org_id;
ALTER TABLE star DROP COLUMN org_id;
ALTER TABLE theme DROP COLUMN org_id;
ALTER TABLE theme_asset DROP COLUMN org_id;
ALTER TABLE user_theme DROP COLUMN org_id;
ALTER TABLE watch DROP COLUMN org_id;
ALTER TABLE watch_optout DROP COLUMN org_id;
ALTER TABLE webhook_delivery DROP COLUMN org_id;
ALTER TABLE webhook_endpoint DROP COLUMN org_id;
ALTER TABLE word_import DROP COLUMN org_id;

-- A table that was one row per organization is one row: its key is a
-- column that can only be true.
ALTER TABLE armature_connection ADD COLUMN singleton boolean NOT NULL DEFAULT true CONSTRAINT armature_connection_singleton CHECK (singleton);
ALTER TABLE oidc_provider ADD COLUMN singleton boolean NOT NULL DEFAULT true CONSTRAINT oidc_provider_singleton CHECK (singleton);
ALTER TABLE org_brand ADD COLUMN singleton boolean NOT NULL DEFAULT true CONSTRAINT org_brand_singleton CHECK (singleton);

-- Primary and unique keys without org_id. One left naming exactly the
-- primary key's columns would only repeat it, so it is not made again.
ALTER TABLE calendar ADD CONSTRAINT calendar_space_id_id_key UNIQUE (space_id, id);
ALTER TABLE comment ADD CONSTRAINT comment_on_page UNIQUE (id, page_id);
ALTER TABLE comment_thread ADD CONSTRAINT comment_thread_id_page_id_key UNIQUE (id, page_id);
ALTER TABLE page ADD CONSTRAINT page_space_id_key UNIQUE (space_id, id);
ALTER TABLE page_collab ADD CONSTRAINT page_collab_page_id_id_key UNIQUE (page_id, id);
ALTER TABLE page_review_decision ADD CONSTRAINT page_review_decision_review_id_reviewer_id_key UNIQUE (review_id, reviewer_id);
ALTER TABLE page_task ADD CONSTRAINT page_task_page_id_task_id_key UNIQUE (page_id, task_id);
ALTER TABLE reaction ADD CONSTRAINT reaction_page_id_comment_id_user_id_emoji_key UNIQUE NULLS NOT DISTINCT (page_id, comment_id, user_id, emoji);
ALTER TABLE space ADD CONSTRAINT space_key_unique UNIQUE (key);
ALTER TABLE armature_connection ADD CONSTRAINT armature_connection_pkey PRIMARY KEY (singleton);
ALTER TABLE armature_remote_link ADD CONSTRAINT armature_remote_link_pkey PRIMARY KEY (page_id, issue_key);
ALTER TABLE armature_token ADD CONSTRAINT armature_token_pkey PRIMARY KEY (user_id);
ALTER TABLE calendar ADD CONSTRAINT calendar_pkey PRIMARY KEY (id);
ALTER TABLE calendar_event ADD CONSTRAINT calendar_event_pkey PRIMARY KEY (id);
ALTER TABLE comment ADD CONSTRAINT comment_pkey PRIMARY KEY (id);
ALTER TABLE comment_thread ADD CONSTRAINT comment_thread_pkey PRIMARY KEY (id);
ALTER TABLE global_grant ADD CONSTRAINT global_grant_pkey PRIMARY KEY (id);
ALTER TABLE notification ADD CONSTRAINT notification_pkey PRIMARY KEY (id);
ALTER TABLE notification_digest ADD CONSTRAINT notification_digest_pkey PRIMARY KEY (user_id, notification_id);
ALTER TABLE notification_preference ADD CONSTRAINT notification_preference_pkey PRIMARY KEY (user_id);
ALTER TABLE oidc_group_role ADD CONSTRAINT oidc_group_role_pkey PRIMARY KEY (id);
ALTER TABLE oidc_provider ADD CONSTRAINT oidc_provider_pkey PRIMARY KEY (singleton);
ALTER TABLE org_brand ADD CONSTRAINT org_brand_pkey PRIMARY KEY (singleton);
ALTER TABLE org_join_request ADD CONSTRAINT org_join_request_pkey PRIMARY KEY (user_id);
ALTER TABLE org_member ADD CONSTRAINT org_member_pkey PRIMARY KEY (user_id);
ALTER TABLE page_collab ADD CONSTRAINT page_collab_pkey PRIMARY KEY (page_id);
ALTER TABLE page_collab_update ADD CONSTRAINT page_collab_update_pkey PRIMARY KEY (room_id, seq);
ALTER TABLE page_draft ADD CONSTRAINT page_draft_pkey PRIMARY KEY (page_id, user_id);
ALTER TABLE page_label ADD CONSTRAINT page_label_pkey PRIMARY KEY (page_id, name);
ALTER TABLE page_link ADD CONSTRAINT page_link_pkey PRIMARY KEY (id);
ALTER TABLE page_owner ADD CONSTRAINT page_owner_pkey PRIMARY KEY (page_id);
ALTER TABLE page_read_ask ADD CONSTRAINT page_read_ask_pkey PRIMARY KEY (page_id);
ALTER TABLE page_read_confirmation ADD CONSTRAINT page_read_confirmation_pkey PRIMARY KEY (page_id, user_id);
ALTER TABLE page_restriction ADD CONSTRAINT page_restriction_pkey PRIMARY KEY (id);
ALTER TABLE page_review ADD CONSTRAINT page_review_pkey PRIMARY KEY (id);
ALTER TABLE page_review_decision ADD CONSTRAINT page_review_decision_pkey PRIMARY KEY (id);
ALTER TABLE page_schedule ADD CONSTRAINT page_schedule_pkey PRIMARY KEY (page_id);
ALTER TABLE page_share ADD CONSTRAINT page_share_pkey PRIMARY KEY (id);
ALTER TABLE page_share_recipient ADD CONSTRAINT page_share_recipient_pkey PRIMARY KEY (share_id, user_id);
ALTER TABLE page_task ADD CONSTRAINT page_task_pkey PRIMARY KEY (id);
ALTER TABLE page_verification ADD CONSTRAINT page_verification_pkey PRIMARY KEY (page_id);
ALTER TABLE page_version ADD CONSTRAINT page_version_pkey PRIMARY KEY (page_id, number);
ALTER TABLE page_version_editor ADD CONSTRAINT page_version_editor_pkey PRIMARY KEY (page_id, number, user_id);
ALTER TABLE page_view_tally ADD CONSTRAINT page_view_tally_pkey PRIMARY KEY (page_id);
ALTER TABLE page_visit ADD CONSTRAINT page_visit_pkey PRIMARY KEY (user_id, page_id);
ALTER TABLE reaction ADD CONSTRAINT reaction_pkey PRIMARY KEY (id);
ALTER TABLE review_rule ADD CONSTRAINT review_rule_pkey PRIMARY KEY (id);
ALTER TABLE space_grant ADD CONSTRAINT space_grant_pkey PRIMARY KEY (id);
ALTER TABLE space_shortcut ADD CONSTRAINT space_shortcut_pkey PRIMARY KEY (id);
ALTER TABLE star ADD CONSTRAINT star_pkey PRIMARY KEY (id);
ALTER TABLE user_theme ADD CONSTRAINT user_theme_pkey PRIMARY KEY (user_id);
ALTER TABLE watch ADD CONSTRAINT watch_pkey PRIMARY KEY (id);
ALTER TABLE watch_optout ADD CONSTRAINT watch_optout_pkey PRIMARY KEY (user_id, page_id);

-- An index on org_id alone held one row per organization; on the
-- constant true it holds one row.
CREATE UNIQUE INDEX attachment_version_idx ON public.attachment USING btree (page_id, lower(file_name), version);
CREATE UNIQUE INDEX calendar_name_idx ON public.calendar USING btree (space_id, lower(name));
CREATE UNIQUE INDEX example_job_one_open ON public.example_job USING btree ((true)) WHERE (state = ANY (ARRAY['queued'::text, 'running'::text]));
CREATE UNIQUE INDEX global_grant_unique_idx ON public.global_grant USING btree (permission, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX groups_org_name_idx ON public.groups USING btree (lower(name));
CREATE UNIQUE INDEX groups_org_ref_idx ON public.groups USING btree (external_ref) WHERE (external_ref IS NOT NULL);
CREATE UNIQUE INDEX oidc_group_role_ref_idx ON public.oidc_group_role USING btree (group_ref);
CREATE UNIQUE INDEX page_one_root_idx ON public.page USING btree (space_id) WHERE ((parent_id IS NULL) AND (kind <> 'post'::text));
CREATE UNIQUE INDEX page_restriction_unique_idx ON public.page_restriction USING btree (page_id, kind, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX page_review_pending_idx ON public.page_review USING btree (page_id) WHERE (state = 'pending'::text);
CREATE UNIQUE INDEX page_review_subject_idx ON public.page_review_subject USING btree (review_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX page_template_name_idx ON public.page_template USING btree (COALESCE(space_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));
CREATE UNIQUE INDEX page_view_once_idx ON public.page_view USING btree (page_id, user_id, day);
CREATE UNIQUE INDEX review_rule_page_idx ON public.review_rule USING btree (page_id) WHERE (page_id IS NOT NULL);
CREATE UNIQUE INDEX review_rule_space_idx ON public.review_rule USING btree (space_id) WHERE (space_id IS NOT NULL);
CREATE UNIQUE INDEX review_rule_subject_idx ON public.review_rule_subject USING btree (rule_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX space_one_example ON public.space USING btree ((true)) WHERE example;
CREATE UNIQUE INDEX space_one_personal ON public.space USING btree (owner_id) WHERE (owner_id IS NOT NULL);
CREATE UNIQUE INDEX space_grant_unique_idx ON public.space_grant USING btree (space_id, permission, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX star_one_per_page_idx ON public.star USING btree (user_id, page_id) WHERE (page_id IS NOT NULL);
CREATE UNIQUE INDEX star_one_per_space_idx ON public.star USING btree (user_id, space_id) WHERE (space_id IS NOT NULL);
CREATE UNIQUE INDEX theme_owner_name_idx ON public.theme USING btree (owner_id, lower(name)) WHERE (armature_theme_id IS NULL);
CREATE UNIQUE INDEX watch_one_per_page_idx ON public.watch USING btree (user_id, page_id) WHERE (page_id IS NOT NULL);
CREATE UNIQUE INDEX watch_one_per_space_idx ON public.watch USING btree (user_id, space_id, kind) WHERE (space_id IS NOT NULL);
CREATE UNIQUE INDEX webhook_endpoint_name_idx ON public.webhook_endpoint USING btree (lower(name));

-- The foreign keys again, each without org_id. One that named only the
-- organization had nothing else to hold and stays gone, except where it
-- tied a row to its one-row parent: there a column that is always true
-- does what org_id did, so the row still goes with the parent.
ALTER TABLE api_token ADD CONSTRAINT api_token_user_id_member_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE api_token_space ADD CONSTRAINT api_token_space_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE api_token_space ADD CONSTRAINT api_token_space_token_id_fkey FOREIGN KEY (token_id) REFERENCES api_token(id) ON DELETE CASCADE;
ALTER TABLE armature_token ADD COLUMN connection boolean NOT NULL DEFAULT true CONSTRAINT armature_token_connection_one CHECK (connection);
ALTER TABLE armature_token ADD CONSTRAINT armature_token_connection_fkey FOREIGN KEY (connection) REFERENCES armature_connection(singleton) ON DELETE CASCADE;
ALTER TABLE armature_token ADD CONSTRAINT armature_token_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE attachment ADD CONSTRAINT attachment_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE attachment_preview ADD CONSTRAINT attachment_preview_attachment_id_fkey FOREIGN KEY (attachment_id) REFERENCES attachment(id) ON DELETE CASCADE;
ALTER TABLE calendar ADD CONSTRAINT calendar_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE calendar_event ADD CONSTRAINT calendar_event_space_id_calendar_id_fkey FOREIGN KEY (space_id, calendar_id) REFERENCES calendar(space_id, id) ON DELETE CASCADE;
ALTER TABLE comment ADD CONSTRAINT comment_thread_id_page_id_fkey FOREIGN KEY (thread_id, page_id) REFERENCES comment_thread(id, page_id) ON DELETE CASCADE;
ALTER TABLE comment_thread ADD CONSTRAINT comment_thread_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE global_grant ADD CONSTRAINT global_grant_group_id_fkey FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE;
ALTER TABLE global_grant ADD CONSTRAINT global_grant_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE notification ADD CONSTRAINT notification_comment_fk FOREIGN KEY (comment_id) REFERENCES comment(id) ON DELETE CASCADE;
ALTER TABLE notification ADD CONSTRAINT notification_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE notification ADD CONSTRAINT notification_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE notification ADD CONSTRAINT notification_thread_fk FOREIGN KEY (thread_id) REFERENCES comment_thread(id) ON DELETE CASCADE;
ALTER TABLE notification_digest ADD CONSTRAINT notification_digest_notification_id_fkey FOREIGN KEY (notification_id) REFERENCES notification(id) ON DELETE CASCADE;
ALTER TABLE notification_preference ADD CONSTRAINT notification_preference_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE oidc_group_role ADD COLUMN provider boolean NOT NULL DEFAULT true CONSTRAINT oidc_group_role_provider_one CHECK (provider);
ALTER TABLE oidc_group_role ADD CONSTRAINT oidc_group_role_provider_fkey FOREIGN KEY (provider) REFERENCES oidc_provider(singleton) ON DELETE CASCADE;
ALTER TABLE org ADD CONSTRAINT org_hub_page_fkey FOREIGN KEY (hub_page_id) REFERENCES page(id) ON DELETE SET NULL;
ALTER TABLE org_member ADD CONSTRAINT org_member_guest_space_fkey FOREIGN KEY (guest_space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE page ADD CONSTRAINT page_archive_fkey FOREIGN KEY (archive_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page ADD CONSTRAINT page_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE page ADD CONSTRAINT page_parent_fkey FOREIGN KEY (space_id, parent_id) REFERENCES page(space_id, id) ON UPDATE CASCADE ON DELETE CASCADE;
ALTER TABLE page ADD CONSTRAINT page_trash_fkey FOREIGN KEY (trash_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_collab ADD CONSTRAINT page_collab_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_collab_update ADD CONSTRAINT page_collab_update_page_id_room_id_fkey FOREIGN KEY (page_id, room_id) REFERENCES page_collab(page_id, id) ON DELETE CASCADE;
ALTER TABLE page_draft ADD CONSTRAINT page_draft_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_draft ADD CONSTRAINT page_draft_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE page_label ADD CONSTRAINT page_label_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_link ADD CONSTRAINT page_link_created_by_fkey FOREIGN KEY (created_by) REFERENCES org_member(user_id) ON DELETE SET NULL (created_by);
ALTER TABLE page_link ADD CONSTRAINT page_link_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_link ADD CONSTRAINT page_link_revoked_by_fkey FOREIGN KEY (revoked_by) REFERENCES org_member(user_id) ON DELETE SET NULL (revoked_by);
ALTER TABLE page_owner ADD CONSTRAINT page_owner_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_owner ADD CONSTRAINT page_owner_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE page_read_ask ADD CONSTRAINT page_read_ask_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_read_confirmation ADD CONSTRAINT page_read_confirmation_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_read_confirmation ADD CONSTRAINT page_read_confirmation_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE page_restriction ADD CONSTRAINT page_restriction_group_id_fkey FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE;
ALTER TABLE page_restriction ADD CONSTRAINT page_restriction_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_restriction ADD CONSTRAINT page_restriction_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE page_review ADD CONSTRAINT page_review_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_review_decision ADD CONSTRAINT page_review_decision_review_id_fkey FOREIGN KEY (review_id) REFERENCES page_review(id) ON DELETE CASCADE;
ALTER TABLE page_review_subject ADD CONSTRAINT page_review_subject_group_id_fkey FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE;
ALTER TABLE page_review_subject ADD CONSTRAINT page_review_subject_review_id_fkey FOREIGN KEY (review_id) REFERENCES page_review(id) ON DELETE CASCADE;
ALTER TABLE page_review_subject ADD CONSTRAINT page_review_subject_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE page_schedule ADD CONSTRAINT page_schedule_page_id_user_id_fkey FOREIGN KEY (page_id, user_id) REFERENCES page_draft(page_id, user_id) ON DELETE CASCADE;
ALTER TABLE page_share ADD CONSTRAINT page_share_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_share ADD CONSTRAINT page_share_sharer_id_fkey FOREIGN KEY (sharer_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE page_share_recipient ADD CONSTRAINT page_share_recipient_share_id_fkey FOREIGN KEY (share_id) REFERENCES page_share(id) ON DELETE CASCADE;
ALTER TABLE page_share_recipient ADD CONSTRAINT page_share_recipient_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE page_task ADD CONSTRAINT page_task_assignee_id_fkey FOREIGN KEY (assignee_id) REFERENCES org_member(user_id) ON DELETE SET NULL (assignee_id);
ALTER TABLE page_task ADD CONSTRAINT page_task_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_template ADD CONSTRAINT page_template_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE page_verification ADD CONSTRAINT page_verification_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_version ADD CONSTRAINT page_version_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_version ADD CONSTRAINT page_version_review_fk FOREIGN KEY (review_id) REFERENCES page_review(id) ON DELETE SET NULL (review_id);
ALTER TABLE page_version_editor ADD CONSTRAINT page_version_editor_page_id_number_fkey FOREIGN KEY (page_id, number) REFERENCES page_version(page_id, number) ON DELETE CASCADE;
ALTER TABLE page_view ADD CONSTRAINT page_view_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_view ADD CONSTRAINT page_view_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE SET NULL (user_id);
ALTER TABLE page_view_tally ADD CONSTRAINT page_view_tally_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_visit ADD CONSTRAINT page_visit_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE page_visit ADD CONSTRAINT page_visit_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE reaction ADD CONSTRAINT reaction_comment_id_page_id_fkey FOREIGN KEY (comment_id, page_id) REFERENCES comment(id, page_id) ON DELETE CASCADE;
ALTER TABLE reaction ADD CONSTRAINT reaction_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE reaction ADD CONSTRAINT reaction_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE review_rule ADD CONSTRAINT review_rule_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE review_rule ADD CONSTRAINT review_rule_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE review_rule_subject ADD CONSTRAINT review_rule_subject_group_id_fkey FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE;
ALTER TABLE review_rule_subject ADD CONSTRAINT review_rule_subject_rule_id_fkey FOREIGN KEY (rule_id) REFERENCES review_rule(id) ON DELETE CASCADE;
ALTER TABLE review_rule_subject ADD CONSTRAINT review_rule_subject_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE space ADD CONSTRAINT space_home_page_fkey FOREIGN KEY (id, home_page_id) REFERENCES page(space_id, id);
ALTER TABLE space_export ADD CONSTRAINT space_export_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE SET NULL (space_id);
ALTER TABLE space_grant ADD CONSTRAINT space_grant_group_id_fkey FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE;
ALTER TABLE space_grant ADD CONSTRAINT space_grant_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE space_grant ADD CONSTRAINT space_grant_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE space_import ADD CONSTRAINT space_import_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE SET NULL (space_id);
ALTER TABLE space_shortcut ADD CONSTRAINT space_shortcut_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE space_shortcut ADD CONSTRAINT space_shortcut_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE star ADD CONSTRAINT star_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE star ADD CONSTRAINT star_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE star ADD CONSTRAINT star_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE theme_asset ADD CONSTRAINT theme_asset_theme_id_fkey FOREIGN KEY (theme_id) REFERENCES theme(id) ON DELETE CASCADE;
ALTER TABLE user_theme ADD CONSTRAINT user_theme_theme_id_fkey FOREIGN KEY (theme_id) REFERENCES theme(id) ON DELETE CASCADE;
ALTER TABLE watch ADD CONSTRAINT watch_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE watch ADD CONSTRAINT watch_space_id_fkey FOREIGN KEY (space_id) REFERENCES space(id) ON DELETE CASCADE;
ALTER TABLE watch ADD CONSTRAINT watch_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE watch_optout ADD CONSTRAINT watch_optout_page_id_fkey FOREIGN KEY (page_id) REFERENCES page(id) ON DELETE CASCADE;
ALTER TABLE watch_optout ADD CONSTRAINT watch_optout_user_id_fkey FOREIGN KEY (user_id) REFERENCES org_member(user_id) ON DELETE CASCADE;
ALTER TABLE webhook_delivery ADD CONSTRAINT webhook_delivery_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES webhook_endpoint(id) ON DELETE CASCADE;
ALTER TABLE webhook_endpoint ADD CONSTRAINT webhook_endpoint_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES org_member(user_id) ON DELETE SET NULL (owner_id);
ALTER TABLE word_import ADD CONSTRAINT word_import_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES page(id) ON DELETE CASCADE;

-- The tables named after the organization take the site's name, and so do
-- the constraints, indexes, policies and triggers named after the tables.
ALTER TABLE org RENAME TO site;
ALTER TABLE org_member RENAME TO member;
ALTER TABLE org_join_request RENAME TO join_request;
ALTER TABLE org_brand RENAME TO brand;
ALTER TABLE org_export RENAME TO site_export;
ALTER TABLE org_import RENAME TO site_import;
ALTER TABLE member RENAME COLUMN org_role TO role;
ALTER TABLE oidc_group_role RENAME COLUMN org_role TO role;

-- +goose StatementBegin
DO $$
DECLARE
    r record;
    o record;
BEGIN
    FOR r IN SELECT * FROM (VALUES ('site', 'org_'), ('member', 'org_member_'), ('join_request', 'org_join_request_'),
                                   ('brand', 'org_brand_'), ('site_export', 'org_export_'), ('site_import', 'org_import_')) AS t (tbl, old)
    LOOP
        -- Renaming a key renames its index too, so constraints go first.
        FOR o IN SELECT conname AS name FROM pg_constraint
                 WHERE conrelid = r.tbl::regclass AND starts_with(conname, r.old)
        LOOP
            EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', r.tbl, o.name,
                           r.tbl || '_' || substr(o.name, length(r.old) + 1));
        END LOOP;
        FOR o IN SELECT c.relname AS name FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
                 WHERE i.indrelid = r.tbl::regclass AND starts_with(c.relname, r.old)
        LOOP
            EXECUTE format('ALTER INDEX %I RENAME TO %I', o.name, r.tbl || '_' || substr(o.name, length(r.old) + 1));
        END LOOP;
        FOR o IN SELECT policyname AS name FROM pg_policies
                 WHERE schemaname = 'public' AND tablename = r.tbl AND starts_with(policyname, r.old)
        LOOP
            EXECUTE format('ALTER POLICY %I ON %I RENAME TO %I', o.name, r.tbl, r.tbl || '_' || substr(o.name, length(r.old) + 1));
        END LOOP;
        FOR o IN SELECT tgname AS name FROM pg_trigger
                 WHERE tgrelid = r.tbl::regclass AND NOT tgisinternal AND starts_with(tgname, r.old)
        LOOP
            EXECUTE format('ALTER TRIGGER %I ON %I RENAME TO %I', o.name, r.tbl, r.tbl || '_' || substr(o.name, length(r.old) + 1));
        END LOOP;
    END LOOP;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION org_anonymous_guard() RENAME TO site_anonymous_guard;
ALTER FUNCTION org_brand_guard() RENAME TO brand_guard;
ALTER FUNCTION org_default_grants() RENAME TO site_default_grants;
ALTER FUNCTION org_default_theme_guard() RENAME TO site_default_theme_guard;
ALTER FUNCTION org_has_spaces() RENAME TO site_has_spaces;
ALTER FUNCTION org_hub_guard() RENAME TO site_hub_guard;
ALTER FUNCTION org_member_guest_fixed() RENAME TO member_guest_fixed;
ALTER FUNCTION org_member_owner_stays() RENAME TO member_owner_stays;

-- The one row is held by its key alone now: an index on a constant.
DROP TRIGGER site_is_the_only_one ON site;
DROP FUNCTION org_is_the_only_one();

-- Every function that named org_id or a renamed table, from its latest
-- definition, without the organization.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION armature_connection_forgets_links() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_remote_link;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_token;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION armature_links_emit(roots uuid[], below boolean, trace text) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
    made  integer;
BEGIN
    IF actor IS NULL OR NOT perm_is_member(actor)
       OR NOT EXISTS (SELECT 1 FROM armature_connection) THEN
        RETURN 0;
    END IF;
    WITH RECURSIVE pages (id) AS (
        SELECT p.id FROM page p WHERE p.id = ANY (roots)
        UNION
        SELECT p.id FROM page p JOIN pages ON p.parent_id = pages.id
        WHERE below
    )
    INSERT INTO outbox_event (topic, payload, trace_parent)
    SELECT 'armature.links', jsonb_build_object('pageId', p.id, 'actorId', actor), NULLIF(trace, '')
    FROM pages JOIN page p ON p.id = pages.id
    WHERE armature_names_issue(p.body)
       OR EXISTS (SELECT 1 FROM armature_remote_link l WHERE l.page_id = p.id);
    GET DIAGNOSTICS made = ROW_COUNT;
    RETURN made;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_leave_tombstone() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO attachment_tombstone (object_key) VALUES (OLD.object_key)
        ON CONFLICT (object_key) DO NOTHING;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_search_hits(q tsquery) RETURNS SETOF uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH hit AS MATERIALIZED (
        SELECT a.id, a.page_id, p.space_id FROM attachment a
        JOIN page p ON p.id = a.page_id
        WHERE a.search_vector @@ q
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_stamp_version() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.page_id::text || '/' || lower(NEW.file_name), 0));
    SELECT COALESCE(max(a.version), 0) + 1 INTO NEW.version
    FROM attachment a
    WHERE a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name);
    IF NEW.restored_from IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM attachment a
        WHERE a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name)
          AND a.version = NEW.restored_from AND a.version < NEW.version - 1) THEN
        RAISE EXCEPTION 'A restore brings back an earlier version of the same file on the same page, and version % of % is not one.', NEW.restored_from, NEW.file_name
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.edited_from IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM attachment a
        WHERE a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name)
          AND a.version = NEW.edited_from
          AND lower(btrim(split_part(a.content_type, ';', 1))) IN ('image/png', 'image/jpeg', 'image/webp')
          AND lower(btrim(split_part(a.content_type, ';', 1))) = lower(btrim(split_part(NEW.content_type, ';', 1)))) THEN
        RAISE EXCEPTION 'An edited picture is drawn on a PNG, JPEG or WebP version of the same file on the same page, and keeps its type; version % of % is not one.', NEW.edited_from, NEW.file_name
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION calendar_limit() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('calendar:' || NEW.space_id::text, 0));
    IF (SELECT count(*) FROM calendar WHERE space_id = NEW.space_id) >= calendar_max() THEN
        RAISE EXCEPTION 'a space holds at most % calendars', calendar_max()
            USING ERRCODE = 'check_violation', CONSTRAINT = 'calendar_limit';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION comment_forget_notifications() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM notification WHERE comment_id = NEW.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION comment_forget_reactions() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM reaction WHERE comment_id = NEW.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION comment_search_hits(q tsquery) RETURNS SETOF uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH hit AS MATERIALIZED (
        SELECT c.id, c.page_id, p.space_id FROM comment c
        JOIN page p ON p.id = c.page_id
        WHERE c.search_vector @@ q AND NOT current_anonymous()
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION group_member_guest_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM member WHERE user_id = NEW.user_id AND role = 'guest') THEN
        RAISE EXCEPTION 'A guest cannot join a group. Grant them their space by name instead.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'group_member_not_guest';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION group_member_scope_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM member WHERE user_id = NEW.user_id) THEN
        RAISE EXCEPTION 'only a member can join a group' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notification_comment_alive() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.comment_id IS NULL THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM comment c
    WHERE c.id = NEW.comment_id AND c.deleted_at IS NULL
    FOR SHARE;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION site_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO global_grant (permission, subject_type) VALUES ('use', 'everyone');
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION site_default_theme_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.default_theme_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.default_theme_id AND shared
    ) THEN
        RAISE EXCEPTION 'the site''s default theme has to be one of its shared themes'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION member_guest_fixed() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF (OLD.role = 'guest' OR NEW.role = 'guest')
       AND (NEW.role, NEW.guest_space_id) IS DISTINCT FROM (OLD.role, OLD.guest_space_id) THEN
        RAISE EXCEPTION 'A guest stays a guest of their one space. Remove them, then invite or let them in again.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'member_guest_fixed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION member_owner_stays() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.role = 'owner' AND NEW.role <> 'owner' AND NEW.role_source = 'oidc' THEN
        RAISE EXCEPTION 'the identity provider cannot change the role of the site''s owner'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_draft_not_live() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM page WHERE id = NEW.page_id AND mode = 'live') THEN
        RAISE EXCEPTION 'A live page has no drafts; what is typed is the page.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_draft_not_live';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_edit_grant_guest_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gs uuid;
BEGIN
    IF NEW.kind <> 'editGrant' OR NEW.user_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT guest_space_id INTO gs FROM member WHERE user_id = NEW.user_id;
    IF gs IS NOT NULL AND gs IS DISTINCT FROM (SELECT space_id FROM page WHERE id = NEW.page_id) THEN
        RAISE EXCEPTION 'A guest belongs to the one space they were invited to, so they cannot edit a page of another. Pick somebody else, or make them a member first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_edit_grant_guest_one_space';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_edit_grants_stay() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM page_restriction WHERE page_id = NEW.id AND kind = 'editGrant';
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_link_closed(target uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(target))
    SELECT CASE
        WHEN NOT EXISTS (SELECT 1 FROM page p WHERE p.id = target) THEN 'missing'
        WHEN EXISTS (SELECT 1 FROM page p JOIN space s ON s.id = p.space_id
                     WHERE p.id = target AND s.owner_id IS NOT NULL) THEN 'personal'
        WHEN EXISTS (SELECT 1 FROM page p WHERE p.id = target AND p.kind = 'folder') THEN 'folder'
        WHEN EXISTS (SELECT 1 FROM page p WHERE p.id = target AND p.trashed_at IS NOT NULL) THEN 'trashed'
        WHEN EXISTS (SELECT 1 FROM chain WHERE version = 0) THEN 'unpublished'
        WHEN EXISTS (SELECT 1 FROM page_restriction r
                     WHERE r.kind = 'view' AND r.page_id IN (SELECT id FROM chain)) THEN 'restricted'
    END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_link_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        PERFORM pg_advisory_xact_lock(hashtextextended('page_link:' || NEW.page_id::text, 0));
        IF (SELECT count(*) FROM page_link
            WHERE page_id = NEW.page_id
              AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())) >= page_link_max() THEN
            RAISE EXCEPTION 'a page has at most % live public links', page_link_max()
                USING ERRCODE = 'check_violation', CONSTRAINT = 'page_link_per_page';
        END IF;
        RETURN NEW;
    END IF;
    IF current_user = 'stator_app' THEN
        IF OLD.revoked_at IS NOT NULL THEN
            RAISE EXCEPTION 'a revoked link stays revoked'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'page_link_revoked_for_good';
        END IF;
        NEW.revoked_at := now();
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_link_refusal(target uuid, actor uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT CASE
        WHEN NOT perm_page_links_manageable(target, actor) THEN 'cannotManage'
        WHEN NOT EXISTS (SELECT 1 FROM site WHERE public_links) THEN 'off'
        ELSE page_link_closed(target)
    END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_live_drops_drafts() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    DELETE FROM page_draft WHERE page_id = NEW.id;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_pending_drafts(page uuid) RETURNS TABLE(user_id uuid, name text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT d.user_id, COALESCE(u.name, '')
    FROM page_draft d
    JOIN page p ON p.id = d.page_id
    LEFT JOIN app_user u ON u.id = d.user_id
    WHERE d.page_id = page_pending_drafts.page
      AND (d.title, d.body) IS DISTINCT FROM (p.title, p.body)
      AND perm_page_editable(page_pending_drafts.page, current_actor_id())
    ORDER BY lower(COALESCE(u.name, '')), d.user_id;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_review_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF OLD.state <> 'pending' THEN
        RAISE EXCEPTION 'a review that was decided stays as it was decided' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'pending' THEN
        RETURN NEW;
    END IF;
    IF NEW.state = 'withdrawn' AND OLD.author_id IS DISTINCT FROM actor THEN
        RAISE EXCEPTION 'only its author withdraws a review' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'changes_requested' AND NOT EXISTS (
        SELECT 1 FROM page_review_decision d
        WHERE d.review_id = OLD.id AND d.reviewer_id = actor AND d.decision = 'changes') THEN
        RAISE EXCEPTION 'only a reviewer who asked for changes sends a review back' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'published' AND page_review_approvals(OLD.id) < OLD.required THEN
        RAISE EXCEPTION 'a review is published only with its approvals' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'skipped' AND (NEW.skipped_by IS DISTINCT FROM actor OR NOT EXISTS (
        SELECT 1 FROM page p WHERE p.id = OLD.page_id
          AND perm_space_holds(actor, p.space_id, 'administer'))) THEN
        RAISE EXCEPTION 'only an administrator of the space publishes a review without its approvals' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state IN ('published', 'skipped') AND NOT EXISTS (
        SELECT 1 FROM page_version v
        WHERE v.page_id = OLD.page_id AND v.number = NEW.version AND v.review_id = OLD.id) THEN
        RAISE EXCEPTION 'a review goes out as a version that names it' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION page_search_rows(q tsquery)
    RETURNS TABLE(id uuid, space_id uuid, parent_id uuid, title text, body jsonb, kind text, version integer,
                  archived_at timestamptz, trashed_at timestamptz, changed_at timestamptz, changed_by uuid, rank real)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH hit AS MATERIALIZED (
        SELECT p.id, p.space_id, p.parent_id, p.title, p.body, p.kind, p.version, p.archived_at, p.trashed_at,
               ts_rank(p.search_vector, q) AS rank
        FROM page p WHERE p.search_vector @@ q
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id, h.space_id, h.parent_id, h.title, h.body, h.kind, h.version, h.archived_at, h.trashed_at,
           v.created_at, v.created_by, h.rank
    FROM hit h
    JOIN judged j ON j.space_id = h.space_id
    JOIN page_version v ON v.page_id = h.id AND v.number = h.version
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.id, current_actor_id()) END
$$;
-- +goose StatementEnd
REVOKE EXECUTE ON FUNCTION page_search_rows(tsquery) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION page_search_rows(tsquery) TO stator_app, stator_admin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_share_brake() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('page_share:' || NEW.sharer_id::text, 0));
    IF (SELECT count(*) FROM page_share
        WHERE sharer_id = NEW.sharer_id
          AND created_at > now() - interval '1 hour') >= page_share_per_hour() THEN
        RAISE EXCEPTION 'too many pages shared in the last hour'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_share_per_hour';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_task_stamp() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    reassigned boolean := TG_OP = 'INSERT' OR NEW.assignee_id IS DISTINCT FROM OLD.assignee_id;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.due_noticed_at IS DISTINCT FROM OLD.due_noticed_at AND NOT reassigned
       AND NEW.due_on IS NOT DISTINCT FROM OLD.due_on AND NEW.done = OLD.done THEN
        RETURN NEW;
    END IF;
    IF reassigned AND NEW.assignee_id IS NOT NULL
       AND NOT perm_page_viewable(NEW.page_id, NEW.assignee_id) THEN
        RAISE EXCEPTION 'a task can only be assigned to somebody who may view its page'
            USING ERRCODE = '42501';
    END IF;
    NEW.updated_at := now();
    IF reassigned THEN
        NEW.assigned_at := CASE WHEN NEW.assignee_id IS NULL THEN NULL ELSE now() END;
        NEW.assigned_by := CASE WHEN NEW.assignee_id IS NULL THEN NULL ELSE current_actor_id() END;
        NEW.assigned_version := CASE WHEN NEW.assignee_id IS NULL THEN NULL
            ELSE (SELECT p.version FROM page p WHERE p.id = NEW.page_id) END;
    ELSE
        NEW.assigned_at := OLD.assigned_at;
        NEW.assigned_by := OLD.assigned_by;
        NEW.assigned_version := OLD.assigned_version;
    END IF;
    IF TG_OP = 'INSERT' OR NEW.done IS DISTINCT FROM OLD.done THEN
        NEW.done_at := CASE WHEN NEW.done THEN now() END;
    ELSE
        NEW.done_at := OLD.done_at;
    END IF;
    IF reassigned OR NEW.due_on IS DISTINCT FROM OLD.due_on THEN
        NEW.due_noticed_at := CASE WHEN NEW.due_on < task_today() THEN now() END;
    ELSE
        NEW.due_noticed_at := OLD.due_noticed_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_verification_stamp() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.lapse_noticed_at IS DISTINCT FROM OLD.lapse_noticed_at
       AND NEW.verified_by IS NOT DISTINCT FROM OLD.verified_by AND NEW.expires_at = OLD.expires_at THEN
        NEW.verified_at := OLD.verified_at;
        NEW.version := OLD.version;
        RETURN NEW;
    END IF;
    NEW.verified_at := now();
    NEW.lapse_noticed_at := NULL;
    NEW.version := (SELECT p.version FROM page p WHERE p.id = NEW.page_id);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_version_next() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
DECLARE
    current integer;
BEGIN
    SELECT version INTO current FROM page WHERE id = NEW.page_id FOR UPDATE;
    IF current IS NULL OR NEW.number <> current + 1 THEN
        RAISE EXCEPTION 'version % of a page is not the one after its current version %', NEW.number, current
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_version_in_sequence';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_view_prune(cutoff date) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gone bigint;
BEGIN
    IF cutoff > page_view_today() - page_view_min_days() THEN
        RAISE EXCEPTION 'page views are kept for at least % days', page_view_min_days() USING ERRCODE = '22023';
    END IF;
    WITH old AS (
        DELETE FROM page_view WHERE day < cutoff RETURNING page_id
    ), counted AS (
        SELECT page_id, count(*) AS n FROM old GROUP BY page_id
    ), added AS (
        INSERT INTO page_view_tally (page_id, views)
        SELECT page_id, n FROM counted
        ON CONFLICT (page_id) DO UPDATE SET views = page_view_tally.views + EXCLUDED.views
    )
    SELECT COALESCE(sum(n), 0) INTO gone FROM counted;
    RETURN gone;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_guest_space(actor uuid) RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT guest_space_id FROM member WHERE user_id = actor
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_is_member(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM member WHERE user_id = actor)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_link_page() RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT l.page_id FROM page_link l CROSS JOIN site o
    WHERE current_anonymous()
      AND l.token_hash = current_link_digest()
      AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now())
      AND o.public_links AND o.archived_at IS NULL
      AND page_link_closed(l.page_id) IS NULL
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_role_admin(actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM member
                   WHERE user_id = actor AND role IN ('owner', 'admin'))
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_anonymous(space uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM site o WHERE o.anonymous_access)
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = space
                     AND g.subject_type = 'anonymous' AND g.permission = 'view')
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_person(space uuid, person uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = space
                     AND (g.user_id = person OR (g.group_id IS NOT NULL AND EXISTS (
                         SELECT 1 FROM group_member gm WHERE gm.group_id = g.group_id AND gm.user_id = person))))
        OR EXISTS (SELECT 1 FROM page p JOIN page_version v ON v.page_id = p.id
                   WHERE p.space_id = space AND v.created_by = person)
        OR EXISTS (SELECT 1 FROM page p JOIN comment c ON c.page_id = p.id
                   WHERE p.space_id = space AND c.author_id = person)
        OR EXISTS (SELECT 1 FROM page p JOIN page_owner o ON o.page_id = p.id
                   WHERE p.space_id = space AND o.user_id = person)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION perm_space_plain(space uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN page p ON p.id = r.page_id
                       WHERE p.space_id = space AND r.kind = 'view')
       AND NOT EXISTS (SELECT 1 FROM page p
                       WHERE p.space_id = space
                         AND p.version = 0 AND p.created_by IS DISTINCT FROM actor)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reaction_comment_alive() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.comment_id IS NULL THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM comment c
    WHERE c.id = NEW.comment_id AND c.deleted_at IS NULL
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'a deleted comment takes no reactions' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.owner_id IS NULL THEN
        INSERT INTO space_grant (space_id, permission, subject_type)
        SELECT NEW.id, p, 'everyone' FROM unnest(ARRAY['view', 'addPages', 'addComments', 'delete']) AS p;
    END IF;
    IF NEW.created_by IS NOT NULL AND EXISTS (
        SELECT 1 FROM member WHERE user_id = NEW.created_by) THEN
        INSERT INTO space_grant (space_id, permission, subject_type, user_id)
        VALUES (NEW.id, 'administer', 'user', NEW.created_by);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_grant_anonymous_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.subject_type = 'anonymous'
       AND EXISTS (SELECT 1 FROM space WHERE id = NEW.space_id AND owner_id IS NOT NULL) THEN
        RAISE EXCEPTION 'A personal space cannot be read without signing in. Move the pages to a team space to publish them.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_anonymous_team_space';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_grant_guest_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    gs uuid;
BEGIN
    IF NEW.user_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT guest_space_id INTO gs FROM member WHERE user_id = NEW.user_id;
    IF gs IS NULL THEN
        RETURN NEW;
    END IF;
    IF gs <> NEW.space_id THEN
        RAISE EXCEPTION 'A guest belongs to the one space they were invited to. Invite somebody else, or make them a member first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_guest_one_space';
    END IF;
    IF NEW.permission = 'administer' THEN
        RAISE EXCEPTION 'A guest cannot administer a space. Give them view, add pages, add comments or delete instead.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_guest_not_administrator';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_grant_keeps_administrator() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF OLD.permission <> 'administer' THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM space WHERE id = OLD.space_id) THEN
        RETURN NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM space_grant
               WHERE space_id = OLD.space_id AND permission = 'administer') THEN
        RETURN NULL;
    END IF;
    IF (OLD.user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM member WHERE user_id = OLD.user_id))
       OR (OLD.group_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM groups WHERE id = OLD.group_id)) THEN
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION space_shortcut_limit() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('space_shortcut:' || NEW.space_id::text, 0));
    IF (SELECT count(*) FROM space_shortcut
        WHERE space_id = NEW.space_id) >= space_shortcut_max() THEN
        RAISE EXCEPTION 'a space holds at most % shortcuts', space_shortcut_max()
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_shortcut_limit';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

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

DROP FUNCTION current_org_id();


-- +goose Down
-- A down cannot invent a second organization, so org_id comes back filled
-- with the one there is. Files stored after the upgrade keep their keys
-- without the organization: attachments, previews and transfer archives
-- still name them in their rows, but an earlier release builds theme file
-- and logo keys itself and does not find those written since.
ALTER FUNCTION site_anonymous_guard() RENAME TO org_anonymous_guard;
ALTER FUNCTION brand_guard() RENAME TO org_brand_guard;
ALTER FUNCTION site_default_grants() RENAME TO org_default_grants;
ALTER FUNCTION site_default_theme_guard() RENAME TO org_default_theme_guard;
ALTER FUNCTION site_has_spaces() RENAME TO org_has_spaces;
ALTER FUNCTION site_hub_guard() RENAME TO org_hub_guard;
ALTER FUNCTION member_guest_fixed() RENAME TO org_member_guest_fixed;
ALTER FUNCTION member_owner_stays() RENAME TO org_member_owner_stays;

ALTER TABLE member RENAME COLUMN role TO org_role;
ALTER TABLE oidc_group_role RENAME COLUMN role TO org_role;
ALTER TABLE site RENAME TO org;
ALTER TABLE member RENAME TO org_member;
ALTER TABLE join_request RENAME TO org_join_request;
ALTER TABLE brand RENAME TO org_brand;
ALTER TABLE site_export RENAME TO org_export;
ALTER TABLE site_import RENAME TO org_import;

-- +goose StatementBegin
DO $$
DECLARE
    r record;
    o record;
BEGIN
    FOR r IN SELECT * FROM (VALUES ('org', 'site_', 'org_'), ('org_member', 'member_', 'org_member_'),
                                   ('org_join_request', 'join_request_', 'org_join_request_'), ('org_brand', 'brand_', 'org_brand_'),
                                   ('org_export', 'site_export_', 'org_export_'), ('org_import', 'site_import_', 'org_import_')) AS t (tbl, new, old)
    LOOP
        FOR o IN SELECT conname AS name FROM pg_constraint
                 WHERE conrelid = r.tbl::regclass AND starts_with(conname, r.new)
        LOOP
            EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', r.tbl, o.name, r.old || substr(o.name, length(r.new) + 1));
        END LOOP;
        FOR o IN SELECT c.relname AS name FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
                 WHERE i.indrelid = r.tbl::regclass AND starts_with(c.relname, r.new)
        LOOP
            EXECUTE format('ALTER INDEX %I RENAME TO %I', o.name, r.old || substr(o.name, length(r.new) + 1));
        END LOOP;
        FOR o IN SELECT policyname AS name FROM pg_policies
                 WHERE schemaname = 'public' AND tablename = r.tbl AND starts_with(policyname, r.new)
        LOOP
            EXECUTE format('ALTER POLICY %I ON %I RENAME TO %I', o.name, r.tbl, r.old || substr(o.name, length(r.new) + 1));
        END LOOP;
        FOR o IN SELECT tgname AS name FROM pg_trigger
                 WHERE tgrelid = r.tbl::regclass AND NOT tgisinternal AND starts_with(tgname, r.new)
        LOOP
            EXECUTE format('ALTER TRIGGER %I ON %I RENAME TO %I', o.name, r.tbl, r.old || substr(o.name, length(r.new) + 1));
        END LOOP;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION current_org_id() RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT id FROM org
$$;
-- +goose StatementEnd

ALTER TABLE api_token DROP CONSTRAINT api_token_user_id_member_fkey;
ALTER TABLE api_token_space DROP CONSTRAINT api_token_space_space_id_fkey;
ALTER TABLE api_token_space DROP CONSTRAINT api_token_space_token_id_fkey;
ALTER TABLE armature_token DROP COLUMN connection;
ALTER TABLE armature_token DROP CONSTRAINT armature_token_user_id_fkey;
ALTER TABLE attachment DROP CONSTRAINT attachment_page_id_fkey;
ALTER TABLE attachment_preview DROP CONSTRAINT attachment_preview_attachment_id_fkey;
ALTER TABLE calendar DROP CONSTRAINT calendar_space_id_fkey;
ALTER TABLE calendar_event DROP CONSTRAINT calendar_event_space_id_calendar_id_fkey;
ALTER TABLE comment DROP CONSTRAINT comment_thread_id_page_id_fkey;
ALTER TABLE comment_thread DROP CONSTRAINT comment_thread_page_id_fkey;
ALTER TABLE global_grant DROP CONSTRAINT global_grant_group_id_fkey;
ALTER TABLE global_grant DROP CONSTRAINT global_grant_user_id_fkey;
ALTER TABLE notification DROP CONSTRAINT notification_comment_fk;
ALTER TABLE notification DROP CONSTRAINT notification_page_id_fkey;
ALTER TABLE notification DROP CONSTRAINT notification_user_id_fkey;
ALTER TABLE notification DROP CONSTRAINT notification_thread_fk;
ALTER TABLE notification_digest DROP CONSTRAINT notification_digest_notification_id_fkey;
ALTER TABLE notification_preference DROP CONSTRAINT notification_preference_user_id_fkey;
ALTER TABLE oidc_group_role DROP COLUMN provider;
ALTER TABLE org DROP CONSTRAINT org_hub_page_fkey;
ALTER TABLE org_member DROP CONSTRAINT org_member_guest_space_fkey;
ALTER TABLE page DROP CONSTRAINT page_archive_fkey;
ALTER TABLE page DROP CONSTRAINT page_space_id_fkey;
ALTER TABLE page DROP CONSTRAINT page_parent_fkey;
ALTER TABLE page DROP CONSTRAINT page_trash_fkey;
ALTER TABLE page_collab DROP CONSTRAINT page_collab_page_id_fkey;
ALTER TABLE page_collab_update DROP CONSTRAINT page_collab_update_page_id_room_id_fkey;
ALTER TABLE page_draft DROP CONSTRAINT page_draft_page_id_fkey;
ALTER TABLE page_draft DROP CONSTRAINT page_draft_user_id_fkey;
ALTER TABLE page_label DROP CONSTRAINT page_label_page_id_fkey;
ALTER TABLE page_link DROP CONSTRAINT page_link_created_by_fkey;
ALTER TABLE page_link DROP CONSTRAINT page_link_page_id_fkey;
ALTER TABLE page_link DROP CONSTRAINT page_link_revoked_by_fkey;
ALTER TABLE page_owner DROP CONSTRAINT page_owner_page_id_fkey;
ALTER TABLE page_owner DROP CONSTRAINT page_owner_user_id_fkey;
ALTER TABLE page_read_ask DROP CONSTRAINT page_read_ask_page_id_fkey;
ALTER TABLE page_read_confirmation DROP CONSTRAINT page_read_confirmation_page_id_fkey;
ALTER TABLE page_read_confirmation DROP CONSTRAINT page_read_confirmation_user_id_fkey;
ALTER TABLE page_restriction DROP CONSTRAINT page_restriction_group_id_fkey;
ALTER TABLE page_restriction DROP CONSTRAINT page_restriction_page_id_fkey;
ALTER TABLE page_restriction DROP CONSTRAINT page_restriction_user_id_fkey;
ALTER TABLE page_review DROP CONSTRAINT page_review_page_id_fkey;
ALTER TABLE page_review_decision DROP CONSTRAINT page_review_decision_review_id_fkey;
ALTER TABLE page_review_subject DROP CONSTRAINT page_review_subject_group_id_fkey;
ALTER TABLE page_review_subject DROP CONSTRAINT page_review_subject_review_id_fkey;
ALTER TABLE page_review_subject DROP CONSTRAINT page_review_subject_user_id_fkey;
ALTER TABLE page_schedule DROP CONSTRAINT page_schedule_page_id_user_id_fkey;
ALTER TABLE page_share DROP CONSTRAINT page_share_page_id_fkey;
ALTER TABLE page_share DROP CONSTRAINT page_share_sharer_id_fkey;
ALTER TABLE page_share_recipient DROP CONSTRAINT page_share_recipient_share_id_fkey;
ALTER TABLE page_share_recipient DROP CONSTRAINT page_share_recipient_user_id_fkey;
ALTER TABLE page_task DROP CONSTRAINT page_task_assignee_id_fkey;
ALTER TABLE page_task DROP CONSTRAINT page_task_page_id_fkey;
ALTER TABLE page_template DROP CONSTRAINT page_template_space_id_fkey;
ALTER TABLE page_verification DROP CONSTRAINT page_verification_page_id_fkey;
ALTER TABLE page_version DROP CONSTRAINT page_version_page_id_fkey;
ALTER TABLE page_version DROP CONSTRAINT page_version_review_fk;
ALTER TABLE page_version_editor DROP CONSTRAINT page_version_editor_page_id_number_fkey;
ALTER TABLE page_view DROP CONSTRAINT page_view_page_id_fkey;
ALTER TABLE page_view DROP CONSTRAINT page_view_user_id_fkey;
ALTER TABLE page_view_tally DROP CONSTRAINT page_view_tally_page_id_fkey;
ALTER TABLE page_visit DROP CONSTRAINT page_visit_page_id_fkey;
ALTER TABLE page_visit DROP CONSTRAINT page_visit_user_id_fkey;
ALTER TABLE reaction DROP CONSTRAINT reaction_comment_id_page_id_fkey;
ALTER TABLE reaction DROP CONSTRAINT reaction_page_id_fkey;
ALTER TABLE reaction DROP CONSTRAINT reaction_user_id_fkey;
ALTER TABLE review_rule DROP CONSTRAINT review_rule_page_id_fkey;
ALTER TABLE review_rule DROP CONSTRAINT review_rule_space_id_fkey;
ALTER TABLE review_rule_subject DROP CONSTRAINT review_rule_subject_group_id_fkey;
ALTER TABLE review_rule_subject DROP CONSTRAINT review_rule_subject_rule_id_fkey;
ALTER TABLE review_rule_subject DROP CONSTRAINT review_rule_subject_user_id_fkey;
ALTER TABLE space DROP CONSTRAINT space_home_page_fkey;
ALTER TABLE space_export DROP CONSTRAINT space_export_space_id_fkey;
ALTER TABLE space_grant DROP CONSTRAINT space_grant_group_id_fkey;
ALTER TABLE space_grant DROP CONSTRAINT space_grant_space_id_fkey;
ALTER TABLE space_grant DROP CONSTRAINT space_grant_user_id_fkey;
ALTER TABLE space_import DROP CONSTRAINT space_import_space_id_fkey;
ALTER TABLE space_shortcut DROP CONSTRAINT space_shortcut_page_id_fkey;
ALTER TABLE space_shortcut DROP CONSTRAINT space_shortcut_space_id_fkey;
ALTER TABLE star DROP CONSTRAINT star_page_id_fkey;
ALTER TABLE star DROP CONSTRAINT star_space_id_fkey;
ALTER TABLE star DROP CONSTRAINT star_user_id_fkey;
ALTER TABLE theme_asset DROP CONSTRAINT theme_asset_theme_id_fkey;
ALTER TABLE user_theme DROP CONSTRAINT user_theme_theme_id_fkey;
ALTER TABLE watch DROP CONSTRAINT watch_page_id_fkey;
ALTER TABLE watch DROP CONSTRAINT watch_space_id_fkey;
ALTER TABLE watch DROP CONSTRAINT watch_user_id_fkey;
ALTER TABLE watch_optout DROP CONSTRAINT watch_optout_page_id_fkey;
ALTER TABLE watch_optout DROP CONSTRAINT watch_optout_user_id_fkey;
ALTER TABLE webhook_delivery DROP CONSTRAINT webhook_delivery_endpoint_id_fkey;
ALTER TABLE webhook_endpoint DROP CONSTRAINT webhook_endpoint_owner_id_fkey;
ALTER TABLE word_import DROP CONSTRAINT word_import_parent_id_fkey;
DROP INDEX attachment_version_idx;
DROP INDEX calendar_name_idx;
DROP INDEX example_job_one_open;
DROP INDEX global_grant_unique_idx;
DROP INDEX groups_org_name_idx;
DROP INDEX groups_org_ref_idx;
DROP INDEX oidc_group_role_ref_idx;
DROP INDEX page_one_root_idx;
DROP INDEX page_restriction_unique_idx;
DROP INDEX page_review_pending_idx;
DROP INDEX page_review_subject_idx;
DROP INDEX page_template_name_idx;
DROP INDEX page_view_once_idx;
DROP INDEX review_rule_page_idx;
DROP INDEX review_rule_space_idx;
DROP INDEX review_rule_subject_idx;
DROP INDEX space_one_example;
DROP INDEX space_one_personal;
DROP INDEX space_grant_unique_idx;
DROP INDEX star_one_per_page_idx;
DROP INDEX star_one_per_space_idx;
DROP INDEX theme_owner_name_idx;
DROP INDEX watch_one_per_page_idx;
DROP INDEX watch_one_per_space_idx;
DROP INDEX webhook_endpoint_name_idx;
ALTER TABLE calendar DROP CONSTRAINT calendar_space_id_id_key;
ALTER TABLE comment DROP CONSTRAINT comment_on_page;
ALTER TABLE comment_thread DROP CONSTRAINT comment_thread_id_page_id_key;
ALTER TABLE page DROP CONSTRAINT page_space_id_key;
ALTER TABLE page_collab DROP CONSTRAINT page_collab_page_id_id_key;
ALTER TABLE page_review_decision DROP CONSTRAINT page_review_decision_review_id_reviewer_id_key;
ALTER TABLE page_task DROP CONSTRAINT page_task_page_id_task_id_key;
ALTER TABLE reaction DROP CONSTRAINT reaction_page_id_comment_id_user_id_emoji_key;
ALTER TABLE space DROP CONSTRAINT space_key_unique;
ALTER TABLE armature_connection DROP CONSTRAINT armature_connection_pkey;
ALTER TABLE armature_remote_link DROP CONSTRAINT armature_remote_link_pkey;
ALTER TABLE armature_token DROP CONSTRAINT armature_token_pkey;
ALTER TABLE calendar DROP CONSTRAINT calendar_pkey;
ALTER TABLE calendar_event DROP CONSTRAINT calendar_event_pkey;
ALTER TABLE comment DROP CONSTRAINT comment_pkey;
ALTER TABLE comment_thread DROP CONSTRAINT comment_thread_pkey;
ALTER TABLE global_grant DROP CONSTRAINT global_grant_pkey;
ALTER TABLE notification DROP CONSTRAINT notification_pkey;
ALTER TABLE notification_digest DROP CONSTRAINT notification_digest_pkey;
ALTER TABLE notification_preference DROP CONSTRAINT notification_preference_pkey;
ALTER TABLE oidc_group_role DROP CONSTRAINT oidc_group_role_pkey;
ALTER TABLE oidc_provider DROP CONSTRAINT oidc_provider_pkey;
ALTER TABLE org_brand DROP CONSTRAINT org_brand_pkey;
ALTER TABLE org_join_request DROP CONSTRAINT org_join_request_pkey;
ALTER TABLE org_member DROP CONSTRAINT org_member_pkey;
ALTER TABLE page_collab DROP CONSTRAINT page_collab_pkey;
ALTER TABLE page_collab_update DROP CONSTRAINT page_collab_update_pkey;
ALTER TABLE page_draft DROP CONSTRAINT page_draft_pkey;
ALTER TABLE page_label DROP CONSTRAINT page_label_pkey;
ALTER TABLE page_link DROP CONSTRAINT page_link_pkey;
ALTER TABLE page_owner DROP CONSTRAINT page_owner_pkey;
ALTER TABLE page_read_ask DROP CONSTRAINT page_read_ask_pkey;
ALTER TABLE page_read_confirmation DROP CONSTRAINT page_read_confirmation_pkey;
ALTER TABLE page_restriction DROP CONSTRAINT page_restriction_pkey;
ALTER TABLE page_review DROP CONSTRAINT page_review_pkey;
ALTER TABLE page_review_decision DROP CONSTRAINT page_review_decision_pkey;
ALTER TABLE page_schedule DROP CONSTRAINT page_schedule_pkey;
ALTER TABLE page_share DROP CONSTRAINT page_share_pkey;
ALTER TABLE page_share_recipient DROP CONSTRAINT page_share_recipient_pkey;
ALTER TABLE page_task DROP CONSTRAINT page_task_pkey;
ALTER TABLE page_verification DROP CONSTRAINT page_verification_pkey;
ALTER TABLE page_version DROP CONSTRAINT page_version_pkey;
ALTER TABLE page_version_editor DROP CONSTRAINT page_version_editor_pkey;
ALTER TABLE page_view_tally DROP CONSTRAINT page_view_tally_pkey;
ALTER TABLE page_visit DROP CONSTRAINT page_visit_pkey;
ALTER TABLE reaction DROP CONSTRAINT reaction_pkey;
ALTER TABLE review_rule DROP CONSTRAINT review_rule_pkey;
ALTER TABLE space_grant DROP CONSTRAINT space_grant_pkey;
ALTER TABLE space_shortcut DROP CONSTRAINT space_shortcut_pkey;
ALTER TABLE star DROP CONSTRAINT star_pkey;
ALTER TABLE user_theme DROP CONSTRAINT user_theme_pkey;
ALTER TABLE watch DROP CONSTRAINT watch_pkey;
ALTER TABLE watch_optout DROP CONSTRAINT watch_optout_pkey;
ALTER TABLE armature_connection DROP COLUMN singleton;
ALTER TABLE oidc_provider DROP COLUMN singleton;
ALTER TABLE org_brand DROP COLUMN singleton;
-- org_id comes back filled with the one organization, as a default so no
-- row is rewritten through its triggers, then defaults to current_org_id().
-- +goose StatementBegin
DO $$
DECLARE
    one uuid := (SELECT id FROM org);
    t   text;
BEGIN
    FOREACH t IN ARRAY ARRAY['api_token', 'api_token_space', 'armature_connection', 'armature_remote_link', 'armature_token', 'attachment', 'attachment_preview', 'attachment_tombstone', 'audit_log', 'calendar', 'calendar_event', 'comment', 'comment_thread', 'example_job', 'example_page', 'global_grant', 'group_member', 'groups', 'notification', 'notification_digest', 'notification_preference', 'oidc_group_role', 'oidc_login', 'oidc_provider', 'org_brand', 'org_export', 'org_import', 'org_join_request', 'org_member', 'outbox_event', 'page', 'page_collab', 'page_collab_update', 'page_draft', 'page_label', 'page_link', 'page_owner', 'page_read_ask', 'page_read_confirmation', 'page_restriction', 'page_review', 'page_review_decision', 'page_review_subject', 'page_schedule', 'page_share', 'page_share_recipient', 'page_task', 'page_template', 'page_verification', 'page_version', 'page_version_editor', 'page_view', 'page_view_tally', 'page_visit', 'reaction', 'review_rule', 'review_rule_subject', 'space', 'space_export', 'space_grant', 'space_import', 'space_shortcut', 'star', 'theme', 'theme_asset', 'user_theme', 'watch', 'watch_optout', 'webhook_delivery', 'webhook_endpoint', 'word_import']
    LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN org_id uuid NOT NULL DEFAULT %L', t, one);
        EXECUTE format('ALTER TABLE %I ALTER COLUMN org_id SET DEFAULT current_org_id()', t);
    END LOOP;
END;
$$;
-- +goose StatementEnd
ALTER TABLE api_token ADD CONSTRAINT api_token_org_id_key UNIQUE (org_id, id);
ALTER TABLE attachment ADD CONSTRAINT attachment_org_id_key UNIQUE (org_id, id);
ALTER TABLE calendar ADD CONSTRAINT calendar_org_id_space_id_id_key UNIQUE (org_id, space_id, id);
ALTER TABLE comment ADD CONSTRAINT comment_on_page UNIQUE (org_id, id, page_id);
ALTER TABLE comment_thread ADD CONSTRAINT comment_thread_org_id_id_page_id_key UNIQUE (org_id, id, page_id);
ALTER TABLE groups ADD CONSTRAINT groups_org_id_key UNIQUE (org_id, id);
ALTER TABLE page ADD CONSTRAINT page_org_id_key UNIQUE (org_id, id);
ALTER TABLE page ADD CONSTRAINT page_space_id_key UNIQUE (org_id, space_id, id);
ALTER TABLE page_collab ADD CONSTRAINT page_collab_org_id_page_id_id_key UNIQUE (org_id, page_id, id);
ALTER TABLE page_review_decision ADD CONSTRAINT page_review_decision_org_id_review_id_reviewer_id_key UNIQUE (org_id, review_id, reviewer_id);
ALTER TABLE page_task ADD CONSTRAINT page_task_org_id_page_id_task_id_key UNIQUE (org_id, page_id, task_id);
ALTER TABLE reaction ADD CONSTRAINT reaction_org_id_page_id_comment_id_user_id_emoji_key UNIQUE NULLS NOT DISTINCT (org_id, page_id, comment_id, user_id, emoji);
ALTER TABLE space ADD CONSTRAINT space_key_unique UNIQUE (org_id, key);
ALTER TABLE space ADD CONSTRAINT space_org_id_key UNIQUE (org_id, id);
ALTER TABLE theme ADD CONSTRAINT theme_org_id_key UNIQUE (org_id, id);
ALTER TABLE webhook_endpoint ADD CONSTRAINT webhook_endpoint_org_id_id_key UNIQUE (org_id, id);
ALTER TABLE armature_connection ADD CONSTRAINT armature_connection_pkey PRIMARY KEY (org_id);
ALTER TABLE armature_remote_link ADD CONSTRAINT armature_remote_link_pkey PRIMARY KEY (org_id, page_id, issue_key);
ALTER TABLE armature_token ADD CONSTRAINT armature_token_pkey PRIMARY KEY (org_id, user_id);
ALTER TABLE calendar ADD CONSTRAINT calendar_pkey PRIMARY KEY (org_id, id);
ALTER TABLE calendar_event ADD CONSTRAINT calendar_event_pkey PRIMARY KEY (org_id, id);
ALTER TABLE comment ADD CONSTRAINT comment_pkey PRIMARY KEY (org_id, id);
ALTER TABLE comment_thread ADD CONSTRAINT comment_thread_pkey PRIMARY KEY (org_id, id);
ALTER TABLE global_grant ADD CONSTRAINT global_grant_pkey PRIMARY KEY (org_id, id);
ALTER TABLE notification ADD CONSTRAINT notification_pkey PRIMARY KEY (org_id, id);
ALTER TABLE notification_digest ADD CONSTRAINT notification_digest_pkey PRIMARY KEY (org_id, user_id, notification_id);
ALTER TABLE notification_preference ADD CONSTRAINT notification_preference_pkey PRIMARY KEY (org_id, user_id);
ALTER TABLE oidc_group_role ADD CONSTRAINT oidc_group_role_pkey PRIMARY KEY (org_id, id);
ALTER TABLE oidc_provider ADD CONSTRAINT oidc_provider_pkey PRIMARY KEY (org_id);
ALTER TABLE org_brand ADD CONSTRAINT org_brand_pkey PRIMARY KEY (org_id);
ALTER TABLE org_join_request ADD CONSTRAINT org_join_request_pkey PRIMARY KEY (org_id, user_id);
ALTER TABLE org_member ADD CONSTRAINT org_member_pkey PRIMARY KEY (org_id, user_id);
ALTER TABLE page_collab ADD CONSTRAINT page_collab_pkey PRIMARY KEY (org_id, page_id);
ALTER TABLE page_collab_update ADD CONSTRAINT page_collab_update_pkey PRIMARY KEY (org_id, room_id, seq);
ALTER TABLE page_draft ADD CONSTRAINT page_draft_pkey PRIMARY KEY (org_id, page_id, user_id);
ALTER TABLE page_label ADD CONSTRAINT page_label_pkey PRIMARY KEY (org_id, page_id, name);
ALTER TABLE page_link ADD CONSTRAINT page_link_pkey PRIMARY KEY (org_id, id);
ALTER TABLE page_owner ADD CONSTRAINT page_owner_pkey PRIMARY KEY (org_id, page_id);
ALTER TABLE page_read_ask ADD CONSTRAINT page_read_ask_pkey PRIMARY KEY (org_id, page_id);
ALTER TABLE page_read_confirmation ADD CONSTRAINT page_read_confirmation_pkey PRIMARY KEY (org_id, page_id, user_id);
ALTER TABLE page_restriction ADD CONSTRAINT page_restriction_pkey PRIMARY KEY (org_id, id);
ALTER TABLE page_review ADD CONSTRAINT page_review_pkey PRIMARY KEY (org_id, id);
ALTER TABLE page_review_decision ADD CONSTRAINT page_review_decision_pkey PRIMARY KEY (org_id, id);
ALTER TABLE page_schedule ADD CONSTRAINT page_schedule_pkey PRIMARY KEY (org_id, page_id);
ALTER TABLE page_share ADD CONSTRAINT page_share_pkey PRIMARY KEY (org_id, id);
ALTER TABLE page_share_recipient ADD CONSTRAINT page_share_recipient_pkey PRIMARY KEY (org_id, share_id, user_id);
ALTER TABLE page_task ADD CONSTRAINT page_task_pkey PRIMARY KEY (org_id, id);
ALTER TABLE page_verification ADD CONSTRAINT page_verification_pkey PRIMARY KEY (org_id, page_id);
ALTER TABLE page_version ADD CONSTRAINT page_version_pkey PRIMARY KEY (org_id, page_id, number);
ALTER TABLE page_version_editor ADD CONSTRAINT page_version_editor_pkey PRIMARY KEY (org_id, page_id, number, user_id);
ALTER TABLE page_view_tally ADD CONSTRAINT page_view_tally_pkey PRIMARY KEY (org_id, page_id);
ALTER TABLE page_visit ADD CONSTRAINT page_visit_pkey PRIMARY KEY (org_id, user_id, page_id);
ALTER TABLE reaction ADD CONSTRAINT reaction_pkey PRIMARY KEY (org_id, id);
ALTER TABLE review_rule ADD CONSTRAINT review_rule_pkey PRIMARY KEY (org_id, id);
ALTER TABLE space_grant ADD CONSTRAINT space_grant_pkey PRIMARY KEY (org_id, id);
ALTER TABLE space_shortcut ADD CONSTRAINT space_shortcut_pkey PRIMARY KEY (org_id, id);
ALTER TABLE star ADD CONSTRAINT star_pkey PRIMARY KEY (org_id, id);
ALTER TABLE user_theme ADD CONSTRAINT user_theme_pkey PRIMARY KEY (org_id, user_id);
ALTER TABLE watch ADD CONSTRAINT watch_pkey PRIMARY KEY (org_id, id);
ALTER TABLE watch_optout ADD CONSTRAINT watch_optout_pkey PRIMARY KEY (org_id, user_id, page_id);
CREATE UNIQUE INDEX attachment_version_idx ON public.attachment USING btree (org_id, page_id, lower(file_name), version);
CREATE UNIQUE INDEX calendar_name_idx ON public.calendar USING btree (org_id, space_id, lower(name));
CREATE UNIQUE INDEX example_job_one_open ON public.example_job USING btree (org_id) WHERE (state = ANY (ARRAY['queued'::text, 'running'::text]));
CREATE UNIQUE INDEX global_grant_unique_idx ON public.global_grant USING btree (org_id, permission, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX groups_org_name_idx ON public.groups USING btree (org_id, lower(name));
CREATE UNIQUE INDEX groups_org_ref_idx ON public.groups USING btree (org_id, external_ref) WHERE (external_ref IS NOT NULL);
CREATE UNIQUE INDEX oidc_group_role_ref_idx ON public.oidc_group_role USING btree (org_id, group_ref);
CREATE UNIQUE INDEX page_one_root_idx ON public.page USING btree (org_id, space_id) WHERE ((parent_id IS NULL) AND (kind <> 'post'::text));
CREATE UNIQUE INDEX page_restriction_unique_idx ON public.page_restriction USING btree (org_id, page_id, kind, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX page_review_pending_idx ON public.page_review USING btree (org_id, page_id) WHERE (state = 'pending'::text);
CREATE UNIQUE INDEX page_review_subject_idx ON public.page_review_subject USING btree (org_id, review_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX page_template_name_idx ON public.page_template USING btree (org_id, COALESCE(space_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));
CREATE UNIQUE INDEX page_view_once_idx ON public.page_view USING btree (org_id, page_id, user_id, day);
CREATE UNIQUE INDEX review_rule_page_idx ON public.review_rule USING btree (org_id, page_id) WHERE (page_id IS NOT NULL);
CREATE UNIQUE INDEX review_rule_space_idx ON public.review_rule USING btree (org_id, space_id) WHERE (space_id IS NOT NULL);
CREATE UNIQUE INDEX review_rule_subject_idx ON public.review_rule_subject USING btree (org_id, rule_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX space_one_example ON public.space USING btree (org_id) WHERE example;
CREATE UNIQUE INDEX space_one_personal ON public.space USING btree (org_id, owner_id) WHERE (owner_id IS NOT NULL);
CREATE UNIQUE INDEX space_grant_unique_idx ON public.space_grant USING btree (org_id, space_id, permission, subject_type, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX star_one_per_page_idx ON public.star USING btree (org_id, user_id, page_id) WHERE (page_id IS NOT NULL);
CREATE UNIQUE INDEX star_one_per_space_idx ON public.star USING btree (org_id, user_id, space_id) WHERE (space_id IS NOT NULL);
CREATE UNIQUE INDEX theme_owner_name_idx ON public.theme USING btree (org_id, owner_id, lower(name)) WHERE (armature_theme_id IS NULL);
CREATE UNIQUE INDEX watch_one_per_page_idx ON public.watch USING btree (org_id, user_id, page_id) WHERE (page_id IS NOT NULL);
CREATE UNIQUE INDEX watch_one_per_space_idx ON public.watch USING btree (org_id, user_id, space_id, kind) WHERE (space_id IS NOT NULL);
CREATE UNIQUE INDEX webhook_endpoint_name_idx ON public.webhook_endpoint USING btree (org_id, lower(name));
ALTER TABLE api_token ADD CONSTRAINT api_token_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE api_token ADD CONSTRAINT api_token_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE api_token_space ADD CONSTRAINT api_token_space_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE api_token_space ADD CONSTRAINT api_token_space_org_id_token_id_fkey FOREIGN KEY (org_id, token_id) REFERENCES api_token(org_id, id) ON DELETE CASCADE;
ALTER TABLE armature_connection ADD CONSTRAINT armature_connection_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE armature_remote_link ADD CONSTRAINT armature_remote_link_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE armature_token ADD CONSTRAINT armature_token_org_id_fkey FOREIGN KEY (org_id) REFERENCES armature_connection(org_id) ON DELETE CASCADE;
ALTER TABLE armature_token ADD CONSTRAINT armature_token_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE attachment ADD CONSTRAINT attachment_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE attachment ADD CONSTRAINT attachment_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE attachment_preview ADD CONSTRAINT attachment_preview_org_id_attachment_id_fkey FOREIGN KEY (org_id, attachment_id) REFERENCES attachment(org_id, id) ON DELETE CASCADE;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE calendar ADD CONSTRAINT calendar_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE calendar_event ADD CONSTRAINT calendar_event_org_id_space_id_calendar_id_fkey FOREIGN KEY (org_id, space_id, calendar_id) REFERENCES calendar(org_id, space_id, id) ON DELETE CASCADE;
ALTER TABLE comment ADD CONSTRAINT comment_org_id_thread_id_page_id_fkey FOREIGN KEY (org_id, thread_id, page_id) REFERENCES comment_thread(org_id, id, page_id) ON DELETE CASCADE;
ALTER TABLE comment_thread ADD CONSTRAINT comment_thread_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE example_job ADD CONSTRAINT example_job_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE example_page ADD CONSTRAINT example_page_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE global_grant ADD CONSTRAINT global_grant_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE global_grant ADD CONSTRAINT global_grant_org_id_group_id_fkey FOREIGN KEY (org_id, group_id) REFERENCES groups(org_id, id) ON DELETE CASCADE;
ALTER TABLE global_grant ADD CONSTRAINT global_grant_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE group_member ADD CONSTRAINT group_member_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE groups ADD CONSTRAINT groups_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE notification ADD CONSTRAINT notification_comment_fk FOREIGN KEY (org_id, comment_id) REFERENCES comment(org_id, id) ON DELETE CASCADE;
ALTER TABLE notification ADD CONSTRAINT notification_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE notification ADD CONSTRAINT notification_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE notification ADD CONSTRAINT notification_thread_fk FOREIGN KEY (org_id, thread_id) REFERENCES comment_thread(org_id, id) ON DELETE CASCADE;
ALTER TABLE notification_digest ADD CONSTRAINT notification_digest_org_id_notification_id_fkey FOREIGN KEY (org_id, notification_id) REFERENCES notification(org_id, id) ON DELETE CASCADE;
ALTER TABLE notification_preference ADD CONSTRAINT notification_preference_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE oidc_group_role ADD CONSTRAINT oidc_group_role_org_id_fkey FOREIGN KEY (org_id) REFERENCES oidc_provider(org_id) ON DELETE CASCADE;
ALTER TABLE oidc_login ADD CONSTRAINT oidc_login_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE oidc_provider ADD CONSTRAINT oidc_provider_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE org ADD CONSTRAINT org_hub_page_fkey FOREIGN KEY (id, hub_page_id) REFERENCES page(org_id, id) ON DELETE SET NULL (hub_page_id);
ALTER TABLE org_brand ADD CONSTRAINT org_brand_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE org_export ADD CONSTRAINT org_export_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE org_import ADD CONSTRAINT org_import_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE org_join_request ADD CONSTRAINT org_join_request_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE org_member ADD CONSTRAINT org_member_guest_space_fkey FOREIGN KEY (org_id, guest_space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE org_member ADD CONSTRAINT org_member_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE outbox_event ADD CONSTRAINT outbox_event_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE page ADD CONSTRAINT page_archive_fkey FOREIGN KEY (org_id, archive_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page ADD CONSTRAINT page_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE page ADD CONSTRAINT page_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE page ADD CONSTRAINT page_parent_fkey FOREIGN KEY (org_id, space_id, parent_id) REFERENCES page(org_id, space_id, id) ON UPDATE CASCADE ON DELETE CASCADE;
ALTER TABLE page ADD CONSTRAINT page_trash_fkey FOREIGN KEY (org_id, trash_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_collab ADD CONSTRAINT page_collab_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_collab_update ADD CONSTRAINT page_collab_update_org_id_page_id_room_id_fkey FOREIGN KEY (org_id, page_id, room_id) REFERENCES page_collab(org_id, page_id, id) ON DELETE CASCADE;
ALTER TABLE page_draft ADD CONSTRAINT page_draft_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_draft ADD CONSTRAINT page_draft_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE page_label ADD CONSTRAINT page_label_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_link ADD CONSTRAINT page_link_org_id_created_by_fkey FOREIGN KEY (org_id, created_by) REFERENCES org_member(org_id, user_id) ON DELETE SET NULL (created_by);
ALTER TABLE page_link ADD CONSTRAINT page_link_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_link ADD CONSTRAINT page_link_org_id_revoked_by_fkey FOREIGN KEY (org_id, revoked_by) REFERENCES org_member(org_id, user_id) ON DELETE SET NULL (revoked_by);
ALTER TABLE page_owner ADD CONSTRAINT page_owner_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_owner ADD CONSTRAINT page_owner_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE page_read_ask ADD CONSTRAINT page_read_ask_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_read_confirmation ADD CONSTRAINT page_read_confirmation_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_read_confirmation ADD CONSTRAINT page_read_confirmation_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE page_restriction ADD CONSTRAINT page_restriction_org_id_group_id_fkey FOREIGN KEY (org_id, group_id) REFERENCES groups(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_restriction ADD CONSTRAINT page_restriction_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_restriction ADD CONSTRAINT page_restriction_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE page_review ADD CONSTRAINT page_review_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_review_decision ADD CONSTRAINT page_review_decision_org_id_review_id_fkey FOREIGN KEY (org_id, review_id) REFERENCES page_review(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_review_subject ADD CONSTRAINT page_review_subject_org_id_group_id_fkey FOREIGN KEY (org_id, group_id) REFERENCES groups(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_review_subject ADD CONSTRAINT page_review_subject_org_id_review_id_fkey FOREIGN KEY (org_id, review_id) REFERENCES page_review(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_review_subject ADD CONSTRAINT page_review_subject_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE page_schedule ADD CONSTRAINT page_schedule_org_id_page_id_user_id_fkey FOREIGN KEY (org_id, page_id, user_id) REFERENCES page_draft(org_id, page_id, user_id) ON DELETE CASCADE;
ALTER TABLE page_share ADD CONSTRAINT page_share_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_share ADD CONSTRAINT page_share_org_id_sharer_id_fkey FOREIGN KEY (org_id, sharer_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE page_share_recipient ADD CONSTRAINT page_share_recipient_org_id_share_id_fkey FOREIGN KEY (org_id, share_id) REFERENCES page_share(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_share_recipient ADD CONSTRAINT page_share_recipient_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE page_task ADD CONSTRAINT page_task_org_id_assignee_id_fkey FOREIGN KEY (org_id, assignee_id) REFERENCES org_member(org_id, user_id) ON DELETE SET NULL (assignee_id);
ALTER TABLE page_task ADD CONSTRAINT page_task_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_template ADD CONSTRAINT page_template_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE page_template ADD CONSTRAINT page_template_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_verification ADD CONSTRAINT page_verification_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_version ADD CONSTRAINT page_version_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_version ADD CONSTRAINT page_version_review_fk FOREIGN KEY (org_id, review_id) REFERENCES page_review(org_id, id) ON DELETE SET NULL (review_id);
ALTER TABLE page_version_editor ADD CONSTRAINT page_version_editor_org_id_page_id_number_fkey FOREIGN KEY (org_id, page_id, number) REFERENCES page_version(org_id, page_id, number) ON DELETE CASCADE;
ALTER TABLE page_view ADD CONSTRAINT page_view_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_view ADD CONSTRAINT page_view_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE SET NULL (user_id);
ALTER TABLE page_view_tally ADD CONSTRAINT page_view_tally_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_visit ADD CONSTRAINT page_visit_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE page_visit ADD CONSTRAINT page_visit_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE reaction ADD CONSTRAINT reaction_org_id_comment_id_page_id_fkey FOREIGN KEY (org_id, comment_id, page_id) REFERENCES comment(org_id, id, page_id) ON DELETE CASCADE;
ALTER TABLE reaction ADD CONSTRAINT reaction_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE reaction ADD CONSTRAINT reaction_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE review_rule ADD CONSTRAINT review_rule_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE review_rule ADD CONSTRAINT review_rule_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE review_rule_subject ADD CONSTRAINT review_rule_subject_org_id_group_id_fkey FOREIGN KEY (org_id, group_id) REFERENCES groups(org_id, id) ON DELETE CASCADE;
ALTER TABLE review_rule_subject ADD CONSTRAINT review_rule_subject_org_id_rule_id_fkey FOREIGN KEY (org_id, rule_id) REFERENCES review_rule(org_id, id) ON DELETE CASCADE;
ALTER TABLE review_rule_subject ADD CONSTRAINT review_rule_subject_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE space ADD CONSTRAINT space_home_page_fkey FOREIGN KEY (org_id, id, home_page_id) REFERENCES page(org_id, space_id, id);
ALTER TABLE space ADD CONSTRAINT space_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE space_export ADD CONSTRAINT space_export_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE space_export ADD CONSTRAINT space_export_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE SET NULL (space_id);
ALTER TABLE space_grant ADD CONSTRAINT space_grant_org_id_group_id_fkey FOREIGN KEY (org_id, group_id) REFERENCES groups(org_id, id) ON DELETE CASCADE;
ALTER TABLE space_grant ADD CONSTRAINT space_grant_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE space_grant ADD CONSTRAINT space_grant_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE space_import ADD CONSTRAINT space_import_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE space_import ADD CONSTRAINT space_import_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE SET NULL (space_id);
ALTER TABLE space_shortcut ADD CONSTRAINT space_shortcut_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE space_shortcut ADD CONSTRAINT space_shortcut_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE star ADD CONSTRAINT star_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE star ADD CONSTRAINT star_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE star ADD CONSTRAINT star_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE theme ADD CONSTRAINT theme_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE theme_asset ADD CONSTRAINT theme_asset_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE theme_asset ADD CONSTRAINT theme_asset_org_id_theme_id_fkey FOREIGN KEY (org_id, theme_id) REFERENCES theme(org_id, id) ON DELETE CASCADE;
ALTER TABLE user_theme ADD CONSTRAINT user_theme_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE user_theme ADD CONSTRAINT user_theme_org_id_theme_id_fkey FOREIGN KEY (org_id, theme_id) REFERENCES theme(org_id, id) ON DELETE CASCADE;
ALTER TABLE watch ADD CONSTRAINT watch_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE watch ADD CONSTRAINT watch_org_id_space_id_fkey FOREIGN KEY (org_id, space_id) REFERENCES space(org_id, id) ON DELETE CASCADE;
ALTER TABLE watch ADD CONSTRAINT watch_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE watch_optout ADD CONSTRAINT watch_optout_org_id_page_id_fkey FOREIGN KEY (org_id, page_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
ALTER TABLE watch_optout ADD CONSTRAINT watch_optout_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_member(org_id, user_id) ON DELETE CASCADE;
ALTER TABLE webhook_delivery ADD CONSTRAINT webhook_delivery_org_id_endpoint_id_fkey FOREIGN KEY (org_id, endpoint_id) REFERENCES webhook_endpoint(org_id, id) ON DELETE CASCADE;
ALTER TABLE webhook_endpoint ADD CONSTRAINT webhook_endpoint_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE webhook_endpoint ADD CONSTRAINT webhook_endpoint_org_id_owner_id_fkey FOREIGN KEY (org_id, owner_id) REFERENCES org_member(org_id, user_id) ON DELETE SET NULL (owner_id);
ALTER TABLE word_import ADD CONSTRAINT word_import_org_id_fkey FOREIGN KEY (org_id) REFERENCES org(id) ON DELETE CASCADE;
ALTER TABLE word_import ADD CONSTRAINT word_import_org_id_parent_id_fkey FOREIGN KEY (org_id, parent_id) REFERENCES page(org_id, id) ON DELETE CASCADE;
GRANT INSERT (org_id) ON armature_connection TO stator_app;
GRANT INSERT (org_id) ON armature_token TO stator_app;
GRANT INSERT (org_id) ON calendar TO stator_app;
GRANT INSERT (org_id) ON calendar_event TO stator_app;
GRANT INSERT (org_id) ON example_job TO stator_app;
GRANT INSERT (org_id) ON org_export TO stator_app;
GRANT INSERT (org_id) ON org_import TO stator_app;
GRANT INSERT (org_id) ON page_owner TO stator_app;
GRANT INSERT (org_id) ON page_task TO stator_app;
GRANT INSERT (org_id) ON page_verification TO stator_app;
GRANT INSERT (org_id) ON space_export TO stator_app;
GRANT INSERT (org_id) ON space_import TO stator_app;
GRANT INSERT (org_id), SELECT (org_id) ON webhook_endpoint TO stator_app;
GRANT INSERT (org_id) ON word_import TO stator_app;

-- Every function back as it was, the two that took the organization with
-- their old arguments, and the policies that call them.
DROP FUNCTION page_search_rows(tsquery);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connection_forgets_links()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_remote_link WHERE org_id = OLD.org_id;
    END IF;
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_connection_moved()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_token WHERE org_id = OLD.org_id;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.armature_links_emit(roots uuid[], below boolean, trace text)
 RETURNS integer
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
    made  integer;
BEGIN
    IF actor IS NULL OR NOT perm_is_member(actor)
       OR NOT EXISTS (SELECT 1 FROM armature_connection) THEN
        RETURN 0;
    END IF;
    WITH RECURSIVE pages (id) AS (
        SELECT p.id FROM page p WHERE p.id = ANY (roots)
        UNION
        SELECT p.id FROM page p JOIN pages ON p.parent_id = pages.id
        WHERE below
    )
    INSERT INTO outbox_event (org_id, topic, payload, trace_parent)
    SELECT p.org_id, 'armature.links', jsonb_build_object('pageId', p.id, 'actorId', actor), NULLIF(trace, '')
    FROM pages JOIN page p ON p.id = pages.id
    WHERE armature_names_issue(p.body)
       OR EXISTS (SELECT 1 FROM armature_remote_link l WHERE l.page_id = p.id);
    GET DIAGNOSTICS made = ROW_COUNT;
    RETURN made;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_leave_tombstone()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    INSERT INTO attachment_tombstone (object_key, org_id) VALUES (OLD.object_key, OLD.org_id)
        ON CONFLICT (object_key) DO NOTHING;
    RETURN OLD;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_search_hits(q tsquery)
 RETURNS SETOF uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH hit AS MATERIALIZED (
        SELECT a.id, a.page_id, p.space_id FROM attachment a
        JOIN page p ON p.org_id = a.org_id AND p.id = a.page_id
        WHERE a.search_vector @@ q
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_stamp_version()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.page_id::text || '/' || lower(NEW.file_name), 0));
    SELECT COALESCE(max(a.version), 0) + 1 INTO NEW.version
    FROM attachment a
    WHERE a.org_id = NEW.org_id AND a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name);
    IF NEW.restored_from IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM attachment a
        WHERE a.org_id = NEW.org_id AND a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name)
          AND a.version = NEW.restored_from AND a.version < NEW.version - 1) THEN
        RAISE EXCEPTION 'A restore brings back an earlier version of the same file on the same page, and version % of % is not one.', NEW.restored_from, NEW.file_name
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.edited_from IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM attachment a
        WHERE a.org_id = NEW.org_id AND a.page_id = NEW.page_id AND lower(a.file_name) = lower(NEW.file_name)
          AND a.version = NEW.edited_from
          AND lower(btrim(split_part(a.content_type, ';', 1))) IN ('image/png', 'image/jpeg', 'image/webp')
          AND lower(btrim(split_part(a.content_type, ';', 1))) = lower(btrim(split_part(NEW.content_type, ';', 1)))) THEN
        RAISE EXCEPTION 'An edited picture is drawn on a PNG, JPEG or WebP version of the same file on the same page, and keeps its type; version % of % is not one.', NEW.edited_from, NEW.file_name
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.attachment_tombstone_leavable(org uuid, key text)
 RETURNS boolean
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.calendar_limit()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('calendar:' || NEW.space_id::text, 0));
    IF (SELECT count(*) FROM calendar WHERE org_id = NEW.org_id AND space_id = NEW.space_id) >= calendar_max() THEN
        RAISE EXCEPTION 'a space holds at most % calendars', calendar_max()
            USING ERRCODE = 'check_violation', CONSTRAINT = 'calendar_limit';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.comment_forget_notifications()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    DELETE FROM notification WHERE org_id = NEW.org_id AND comment_id = NEW.id;
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.comment_forget_reactions()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    DELETE FROM reaction WHERE org_id = NEW.org_id AND comment_id = NEW.id;
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.comment_search_hits(q tsquery)
 RETURNS SETOF uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH hit AS MATERIALIZED (
        SELECT c.id, c.page_id, p.space_id FROM comment c
        JOIN page p ON p.org_id = c.org_id AND p.id = c.page_id
        WHERE c.search_vector @@ q AND NOT current_anonymous()
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id FROM hit h JOIN judged j ON j.space_id = h.space_id
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.page_id, current_actor_id()) END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.current_org_id()
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT id FROM org
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.group_member_guest_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF EXISTS (SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id AND org_role = 'guest') THEN
        RAISE EXCEPTION 'A guest cannot join a group. Grant them their space by name instead.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'group_member_not_guest';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.group_member_scope_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF (SELECT org_id FROM groups WHERE id = NEW.group_id) IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a group member cannot cross organizations' USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id) THEN
        RAISE EXCEPTION 'only a member of the organization can join one of its groups' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.notification_comment_alive()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.comment_id IS NULL THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM comment c
    WHERE c.org_id = NEW.org_id AND c.id = NEW.comment_id AND c.deleted_at IS NULL
    FOR SHARE;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_anonymous_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF current_user = 'stator_app' AND NOT perm_is_admin(current_actor_id()) THEN
        RAISE EXCEPTION 'only an administrator of the organization lets people read without signing in'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_brand_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_default_grants()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    INSERT INTO global_grant (org_id, permission, subject_type) VALUES (NEW.id, 'use', 'everyone');
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_default_theme_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.default_theme_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.default_theme_id AND org_id = NEW.id AND shared
    ) THEN
        RAISE EXCEPTION 'the organization''s default theme has to be one of its shared themes'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_has_spaces()
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_hub_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_is_the_only_one()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF EXISTS (SELECT 1 FROM org) THEN
        RAISE EXCEPTION 'This instance already has its organization, and Stator serves one per instance. Import another organization''s archive into a fresh instance instead.'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_member_guest_fixed()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF (OLD.org_role = 'guest' OR NEW.org_role = 'guest')
       AND (NEW.org_role, NEW.guest_space_id) IS DISTINCT FROM (OLD.org_role, OLD.guest_space_id) THEN
        RAISE EXCEPTION 'A guest stays a guest of their one space. Remove them, then invite or let them in again.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'org_member_guest_fixed';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.org_member_owner_stays()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF OLD.org_role = 'owner' AND NEW.org_role <> 'owner' AND NEW.role_source = 'oidc' THEN
        RAISE EXCEPTION 'the identity provider cannot change the role of an organization''s owner'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_draft_not_live()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF EXISTS (SELECT 1 FROM page WHERE org_id = NEW.org_id AND id = NEW.page_id AND mode = 'live') THEN
        RAISE EXCEPTION 'A live page has no drafts; what is typed is the page.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_draft_not_live';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_edit_grant_guest_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gs uuid;
BEGIN
    IF NEW.kind <> 'editGrant' OR NEW.user_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT guest_space_id INTO gs FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id;
    IF gs IS NOT NULL AND gs IS DISTINCT FROM (SELECT space_id FROM page WHERE id = NEW.page_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'A guest belongs to the one space they were invited to, so they cannot edit a page of another. Pick somebody else, or make them a member of the organization first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_edit_grant_guest_one_space';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_edit_grants_stay()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    DELETE FROM page_restriction WHERE org_id = NEW.org_id AND page_id = NEW.id AND kind = 'editGrant';
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_link_closed(target uuid)
 RETURNS text
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH chain AS MATERIALIZED (SELECT * FROM perm_page_chain(target))
    SELECT CASE
        WHEN NOT EXISTS (SELECT 1 FROM page p WHERE p.id = target) THEN 'missing'
        WHEN EXISTS (SELECT 1 FROM page p JOIN space s ON s.org_id = p.org_id AND s.id = p.space_id
                     WHERE p.id = target AND s.owner_id IS NOT NULL) THEN 'personal'
        WHEN EXISTS (SELECT 1 FROM page p WHERE p.id = target AND p.kind = 'folder') THEN 'folder'
        WHEN EXISTS (SELECT 1 FROM page p WHERE p.id = target AND p.trashed_at IS NOT NULL) THEN 'trashed'
        WHEN EXISTS (SELECT 1 FROM chain WHERE version = 0) THEN 'unpublished'
        WHEN EXISTS (SELECT 1 FROM page_restriction r
                     WHERE r.kind = 'view' AND r.page_id IN (SELECT id FROM chain)) THEN 'restricted'
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_link_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF TG_OP = 'INSERT' THEN
        PERFORM pg_advisory_xact_lock(hashtextextended('page_link:' || NEW.org_id::text || ':' || NEW.page_id::text, 0));
        IF (SELECT count(*) FROM page_link
            WHERE org_id = NEW.org_id AND page_id = NEW.page_id
              AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())) >= page_link_max() THEN
            RAISE EXCEPTION 'a page has at most % live public links', page_link_max()
                USING ERRCODE = 'check_violation', CONSTRAINT = 'page_link_per_page';
        END IF;
        RETURN NEW;
    END IF;
    IF current_user = 'stator_app' THEN
        IF OLD.revoked_at IS NOT NULL THEN
            RAISE EXCEPTION 'a revoked link stays revoked'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'page_link_revoked_for_good';
        END IF;
        NEW.revoked_at := now();
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_link_refusal(target uuid, actor uuid)
 RETURNS text
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT CASE
        WHEN NOT perm_page_links_manageable(target, actor) THEN 'cannotManage'
        WHEN NOT EXISTS (SELECT 1 FROM org WHERE public_links) THEN 'off'
        ELSE page_link_closed(target)
    END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_live_drops_drafts()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    DELETE FROM page_draft WHERE org_id = NEW.org_id AND page_id = NEW.id;
    RETURN NULL;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_pending_drafts(page uuid)
 RETURNS TABLE(user_id uuid, name text)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT d.user_id, COALESCE(u.name, '')
    FROM page_draft d
    JOIN page p ON p.org_id = d.org_id AND p.id = d.page_id
    LEFT JOIN app_user u ON u.id = d.user_id
    WHERE d.page_id = page_pending_drafts.page
      AND (d.title, d.body) IS DISTINCT FROM (p.title, p.body)
      AND perm_page_editable(page_pending_drafts.page, current_actor_id())
    ORDER BY lower(COALESCE(u.name, '')), d.user_id;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_review_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF OLD.state <> 'pending' THEN
        RAISE EXCEPTION 'a review that was decided stays as it was decided' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'pending' THEN
        RETURN NEW;
    END IF;
    IF NEW.state = 'withdrawn' AND OLD.author_id IS DISTINCT FROM actor THEN
        RAISE EXCEPTION 'only its author withdraws a review' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'changes_requested' AND NOT EXISTS (
        SELECT 1 FROM page_review_decision d
        WHERE d.org_id = OLD.org_id AND d.review_id = OLD.id AND d.reviewer_id = actor AND d.decision = 'changes') THEN
        RAISE EXCEPTION 'only a reviewer who asked for changes sends a review back' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'published' AND page_review_approvals(OLD.id) < OLD.required THEN
        RAISE EXCEPTION 'a review is published only with its approvals' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'skipped' AND (NEW.skipped_by IS DISTINCT FROM actor OR NOT EXISTS (
        SELECT 1 FROM page p WHERE p.org_id = OLD.org_id AND p.id = OLD.page_id
          AND perm_space_holds(actor, p.space_id, 'administer'))) THEN
        RAISE EXCEPTION 'only an administrator of the space publishes a review without its approvals' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state IN ('published', 'skipped') AND NOT EXISTS (
        SELECT 1 FROM page_version v
        WHERE v.org_id = OLD.org_id AND v.page_id = OLD.page_id AND v.number = NEW.version AND v.review_id = OLD.id) THEN
        RAISE EXCEPTION 'a review goes out as a version that names it' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_search_rows(q tsquery)
 RETURNS TABLE(id uuid, org_id uuid, space_id uuid, parent_id uuid, title text, body jsonb, kind text, version integer, archived_at timestamp with time zone, trashed_at timestamp with time zone, changed_at timestamp with time zone, changed_by uuid, rank real)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    WITH hit AS MATERIALIZED (
        SELECT p.id, p.org_id, p.space_id, p.parent_id, p.title, p.body, p.kind, p.version, p.archived_at, p.trashed_at,
               ts_rank(p.search_vector, q) AS rank
        FROM page p WHERE p.search_vector @@ q
    ), judged AS MATERIALIZED (
        SELECT s.space_id, perm_space_plain(s.space_id, current_actor_id()) AS plain,
               perm_space_holds(current_actor_id(), s.space_id, 'view') AS holds
        FROM (SELECT DISTINCT space_id FROM hit) s
    )
    SELECT h.id, h.org_id, h.space_id, h.parent_id, h.title, h.body, h.kind, h.version, h.archived_at, h.trashed_at,
           v.created_at, v.created_by, h.rank
    FROM hit h
    JOIN judged j ON j.space_id = h.space_id
    JOIN page_version v ON v.page_id = h.id AND v.number = h.version
    WHERE CASE WHEN j.plain THEN j.holds ELSE perm_page_viewable(h.id, current_actor_id()) END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_share_brake()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('page_share:' || NEW.org_id::text || ':' || NEW.sharer_id::text, 0));
    IF (SELECT count(*) FROM page_share
        WHERE org_id = NEW.org_id AND sharer_id = NEW.sharer_id
          AND created_at > now() - interval '1 hour') >= page_share_per_hour() THEN
        RAISE EXCEPTION 'too many pages shared in the last hour'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_share_per_hour';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_task_stamp()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    reassigned boolean := TG_OP = 'INSERT' OR NEW.assignee_id IS DISTINCT FROM OLD.assignee_id;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.due_noticed_at IS DISTINCT FROM OLD.due_noticed_at AND NOT reassigned
       AND NEW.due_on IS NOT DISTINCT FROM OLD.due_on AND NEW.done = OLD.done THEN
        RETURN NEW;
    END IF;
    IF reassigned AND NEW.assignee_id IS NOT NULL
       AND NOT perm_page_viewable(NEW.page_id, NEW.assignee_id) THEN
        RAISE EXCEPTION 'a task can only be assigned to somebody who may view its page'
            USING ERRCODE = '42501';
    END IF;
    NEW.updated_at := now();
    IF reassigned THEN
        NEW.assigned_at := CASE WHEN NEW.assignee_id IS NULL THEN NULL ELSE now() END;
        NEW.assigned_by := CASE WHEN NEW.assignee_id IS NULL THEN NULL ELSE current_actor_id() END;
        NEW.assigned_version := CASE WHEN NEW.assignee_id IS NULL THEN NULL
            ELSE (SELECT p.version FROM page p WHERE p.org_id = NEW.org_id AND p.id = NEW.page_id) END;
    ELSE
        NEW.assigned_at := OLD.assigned_at;
        NEW.assigned_by := OLD.assigned_by;
        NEW.assigned_version := OLD.assigned_version;
    END IF;
    IF TG_OP = 'INSERT' OR NEW.done IS DISTINCT FROM OLD.done THEN
        NEW.done_at := CASE WHEN NEW.done THEN now() END;
    ELSE
        NEW.done_at := OLD.done_at;
    END IF;
    IF reassigned OR NEW.due_on IS DISTINCT FROM OLD.due_on THEN
        NEW.due_noticed_at := CASE WHEN NEW.due_on < task_today() THEN now() END;
    ELSE
        NEW.due_noticed_at := OLD.due_noticed_at;
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_verification_stamp()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.lapse_noticed_at IS DISTINCT FROM OLD.lapse_noticed_at
       AND NEW.verified_by IS NOT DISTINCT FROM OLD.verified_by AND NEW.expires_at = OLD.expires_at THEN
        NEW.verified_at := OLD.verified_at;
        NEW.version := OLD.version;
        RETURN NEW;
    END IF;
    NEW.verified_at := now();
    NEW.lapse_noticed_at := NULL;
    NEW.version := (SELECT p.version FROM page p WHERE p.id = NEW.page_id AND p.org_id = NEW.org_id);
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_version_next()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    current integer;
BEGIN
    SELECT version INTO current FROM page WHERE org_id = NEW.org_id AND id = NEW.page_id FOR UPDATE;
    IF current IS NULL OR NEW.number <> current + 1 THEN
        RAISE EXCEPTION 'version % of a page is not the one after its current version %', NEW.number, current
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_version_in_sequence';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_version_open(org uuid, page_id uuid, number integer)
 RETURNS boolean
 LANGUAGE sql
 STABLE
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM page_version v JOIN page p ON p.org_id = v.org_id AND p.id = v.page_id
        WHERE v.org_id = org AND v.page_id = page_version_open.page_id AND v.number = page_version_open.number
          AND v.live AND p.mode = 'live' AND p.version = v.number
          AND v.created_at > now() - live_version_span());
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.page_view_prune(cutoff date)
 RETURNS bigint
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gone bigint;
BEGIN
    IF cutoff > page_view_today() - page_view_min_days() THEN
        RAISE EXCEPTION 'page views are kept for at least % days', page_view_min_days() USING ERRCODE = '22023';
    END IF;
    WITH old AS (
        DELETE FROM page_view WHERE day < cutoff RETURNING org_id, page_id
    ), counted AS (
        SELECT org_id, page_id, count(*) AS n FROM old GROUP BY org_id, page_id
    ), added AS (
        INSERT INTO page_view_tally (org_id, page_id, views)
        SELECT org_id, page_id, n FROM counted
        ON CONFLICT (org_id, page_id) DO UPDATE SET views = page_view_tally.views + EXCLUDED.views
    )
    SELECT COALESCE(sum(n), 0) INTO gone FROM counted;
    RETURN gone;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_guest_space(actor uuid)
 RETURNS uuid
 LANGUAGE sql
 STABLE PARALLEL SAFE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT guest_space_id FROM org_member WHERE user_id = actor
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_is_member(actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM org_member WHERE user_id = actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_link_page()
 RETURNS uuid
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT l.page_id FROM page_link l JOIN org o ON o.id = l.org_id
    WHERE current_anonymous()
      AND l.token_hash = current_link_digest()
      AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now())
      AND o.public_links AND o.archived_at IS NULL
      AND page_link_closed(l.page_id) IS NULL
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_role_admin(actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM org_member
                   WHERE user_id = actor AND org_role IN ('owner', 'admin'))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_anonymous(space uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM org o WHERE o.anonymous_access)
       AND EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = space
                     AND g.subject_type = 'anonymous' AND g.permission = 'view')
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_person(space uuid, person uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT EXISTS (SELECT 1 FROM space_grant g
                   WHERE g.space_id = space
                     AND (g.user_id = person OR (g.group_id IS NOT NULL AND EXISTS (
                         SELECT 1 FROM group_member gm WHERE gm.group_id = g.group_id AND gm.user_id = person))))
        OR EXISTS (SELECT 1 FROM page p JOIN page_version v ON v.org_id = p.org_id AND v.page_id = p.id
                   WHERE p.space_id = space AND v.created_by = person)
        OR EXISTS (SELECT 1 FROM page p JOIN comment c ON c.org_id = p.org_id AND c.page_id = p.id
                   WHERE p.space_id = space AND c.author_id = person)
        OR EXISTS (SELECT 1 FROM page p JOIN page_owner o ON o.org_id = p.org_id AND o.page_id = p.id
                   WHERE p.space_id = space AND o.user_id = person)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.perm_space_plain(space uuid, actor uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
    SELECT actor IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM page_restriction r JOIN page p ON p.org_id = r.org_id AND p.id = r.page_id
                       WHERE p.space_id = space AND r.kind = 'view')
       AND NOT EXISTS (SELECT 1 FROM page p
                       WHERE p.space_id = space
                         AND p.version = 0 AND p.created_by IS DISTINCT FROM actor)
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.reaction_comment_alive()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.comment_id IS NULL THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM comment c
    WHERE c.org_id = NEW.org_id AND c.id = NEW.comment_id AND c.deleted_at IS NULL
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'a deleted comment takes no reactions' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.session_reaches(proof text, proof_org uuid, person uuid, target uuid)
 RETURNS boolean
 LANGUAGE sql
 STABLE
AS $function$
    SELECT proof = 'password'
        OR target = proof_org
        OR (proof = 'oidc' AND (
            EXISTS (SELECT 1 FROM org_member
                    WHERE org_id = target AND user_id = person AND org_role = 'owner')
            OR EXISTS (SELECT 1 FROM oidc_provider home
                       JOIN oidc_provider away ON away.issuer = home.issuer
                       WHERE home.org_id = proof_org AND away.org_id = target
                         AND home.enabled AND away.enabled)))
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.session_stays_home()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
    -- The organization going, which nulls the column, is the one way it changes.
    IF NEW.proof_org_id IS NOT NULL AND NEW.proof_org_id IS DISTINCT FROM OLD.proof_org_id THEN
        RAISE EXCEPTION 'a session keeps the organization it was proven for' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.current_org_id IS NOT NULL AND NEW.current_org_id IS DISTINCT FROM OLD.current_org_id
       AND NOT session_reaches(OLD.proof, OLD.proof_org_id, OLD.user_id, NEW.current_org_id) THEN
        RAISE EXCEPTION 'a session opened by % does not reach that organization', OLD.proof
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_default_grants()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_grant_anonymous_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF NEW.subject_type = 'anonymous'
       AND EXISTS (SELECT 1 FROM space WHERE org_id = NEW.org_id AND id = NEW.space_id AND owner_id IS NOT NULL) THEN
        RAISE EXCEPTION 'A personal space cannot be read without signing in. Move the pages to a team space to publish them.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_anonymous_team_space';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_grant_guest_guard()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
DECLARE
    gs uuid;
BEGIN
    IF NEW.user_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT guest_space_id INTO gs FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id;
    IF gs IS NULL THEN
        RETURN NEW;
    END IF;
    IF gs <> NEW.space_id THEN
        RAISE EXCEPTION 'A guest belongs to the one space they were invited to. Invite somebody else, or make them a member of the organization first.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_guest_one_space';
    END IF;
    IF NEW.permission = 'administer' THEN
        RAISE EXCEPTION 'A guest cannot administer a space. Give them view, add pages, add comments or delete instead.'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_grant_guest_not_administrator';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_grant_keeps_administrator()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.space_shortcut_limit()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('space_shortcut:' || NEW.space_id::text, 0));
    IF (SELECT count(*) FROM space_shortcut
        WHERE org_id = NEW.org_id AND space_id = NEW.space_id) >= space_shortcut_max() THEN
        RAISE EXCEPTION 'a space holds at most % shortcuts', space_shortcut_max()
            USING ERRCODE = 'check_violation', CONSTRAINT = 'space_shortcut_limit';
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.theme_unshared_default()
 RETURNS trigger
 LANGUAGE plpgsql
 SET search_path TO 'public', 'pg_temp'
AS $function$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE org SET default_theme_id = NULL WHERE id = NEW.org_id AND default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$function$;
-- +goose StatementEnd

ALTER POLICY theme_asset_removers ON theme_asset
    USING ((EXISTS ( SELECT 1
   FROM theme t
  WHERE ((t.org_id = theme_asset.org_id) AND (t.id = theme_asset.theme_id) AND ((t.owner_id = current_actor_id()) OR (t.shared AND perm_role_admin(current_actor_id())))))));
ALTER POLICY theme_asset_changers ON theme_asset
    USING ((EXISTS ( SELECT 1
   FROM theme t
  WHERE ((t.org_id = theme_asset.org_id) AND (t.id = theme_asset.theme_id) AND ((t.owner_id = current_actor_id()) OR (t.shared AND perm_role_admin(current_actor_id())))))))
    WITH CHECK ((EXISTS ( SELECT 1
   FROM theme t
  WHERE ((t.org_id = theme_asset.org_id) AND (t.id = theme_asset.theme_id) AND ((t.owner_id = current_actor_id()) OR (t.shared AND perm_role_admin(current_actor_id())))))));
ALTER POLICY theme_asset_makers ON theme_asset
    WITH CHECK ((EXISTS ( SELECT 1
   FROM theme t
  WHERE ((t.org_id = theme_asset.org_id) AND (t.id = theme_asset.theme_id) AND ((t.owner_id = current_actor_id()) OR (t.shared AND perm_role_admin(current_actor_id())))))));
ALTER POLICY theme_asset_viewers ON theme_asset
    USING ((EXISTS ( SELECT 1
   FROM theme t
  WHERE ((t.org_id = theme_asset.org_id) AND (t.id = theme_asset.theme_id)))));
ALTER POLICY user_theme_changers ON user_theme
    USING ((user_id = current_actor_id()))
    WITH CHECK (((user_id = current_actor_id()) AND ((theme_id IS NULL) OR (EXISTS ( SELECT 1
   FROM theme t
  WHERE ((t.org_id = user_theme.org_id) AND (t.id = user_theme.theme_id)))))));
ALTER POLICY user_theme_choosers ON user_theme
    WITH CHECK (((user_id = current_actor_id()) AND ((theme_id IS NULL) OR (EXISTS ( SELECT 1
   FROM theme t
  WHERE ((t.org_id = user_theme.org_id) AND (t.id = user_theme.theme_id)))))));
ALTER POLICY attachment_tombstone_left_by_deletes ON attachment_tombstone
    WITH CHECK (attachment_tombstone_leavable(org_id, object_key));
ALTER POLICY page_version_live_on_live_pages ON page_version
    WITH CHECK (((NOT live) OR (EXISTS ( SELECT 1
   FROM page p
  WHERE ((p.org_id = page_version.org_id) AND (p.id = page_version.page_id) AND (p.mode = 'live'::text))))));
ALTER POLICY page_version_amenders ON page_version
    USING ((live AND (created_at > (now() - live_version_span())) AND perm_page_editable(page_id, current_actor_id()) AND (EXISTS ( SELECT 1
   FROM page p
  WHERE ((p.org_id = page_version.org_id) AND (p.id = page_version.page_id) AND (p.mode = 'live'::text) AND (p.version = page_version.number))))))
    WITH CHECK ((live AND perm_page_editable(page_id, current_actor_id())));
ALTER POLICY api_token_space_removers ON api_token_space
    USING ((EXISTS ( SELECT 1
   FROM api_token t
  WHERE ((t.org_id = api_token_space.org_id) AND (t.id = api_token_space.token_id)))));
ALTER POLICY api_token_space_holders ON api_token_space
    USING ((EXISTS ( SELECT 1
   FROM api_token t
  WHERE ((t.org_id = api_token_space.org_id) AND (t.id = api_token_space.token_id)))));
ALTER POLICY attachment_preview_makers ON attachment_preview
    WITH CHECK ((EXISTS ( SELECT 1
   FROM attachment a
  WHERE ((a.org_id = attachment_preview.org_id) AND (a.id = attachment_preview.attachment_id)))));
ALTER POLICY attachment_preview_viewers ON attachment_preview
    USING ((EXISTS ( SELECT 1
   FROM attachment a
  WHERE ((a.org_id = attachment_preview.org_id) AND (a.id = attachment_preview.attachment_id)))));
ALTER POLICY page_version_editor_savers ON page_version_editor
    WITH CHECK (((user_id = current_actor_id()) AND perm_page_editable(page_id, current_actor_id()) AND page_version_open(org_id, page_id, number)));

REVOKE EXECUTE ON FUNCTION page_search_rows(tsquery), attachment_tombstone_leavable(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION page_search_rows(tsquery), attachment_tombstone_leavable(uuid, text) TO stator_app, stator_admin;
DROP FUNCTION attachment_tombstone_leavable(text);
DROP FUNCTION page_version_open(uuid, integer);

CREATE TRIGGER org_is_the_only_one BEFORE INSERT ON org
    FOR EACH ROW EXECUTE FUNCTION org_is_the_only_one();

ALTER TABLE user_session
    ADD COLUMN current_org_id uuid CONSTRAINT user_session_current_org_id_fkey REFERENCES org(id) ON DELETE SET NULL,
    ADD COLUMN proof_org_id uuid CONSTRAINT user_session_proof_org_id_fkey REFERENCES org(id) ON DELETE SET NULL;
UPDATE user_session SET current_org_id = (SELECT id FROM org), proof_org_id = (SELECT id FROM org);
CREATE TRIGGER session_stays_home BEFORE UPDATE OF current_org_id, proof, proof_org_id ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_stays_home();

ALTER TABLE attachment_tombstone ADD CONSTRAINT attachment_tombstone_own_prefix
    CHECK (starts_with(object_key, 'org/' || org_id::text || '/')) NOT VALID;

-- The earlier release reads object_key as a generated column and never
-- writes it, so the triggers go on filling it, with the old formula.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_object_key() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.object_key := 'org/' || NEW.org_id::text || '/page/' || NEW.page_id::text || '/' || NEW.id::text;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_preview_object_key() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.object_key := 'org/' || NEW.org_id::text || '/preview/' || NEW.attachment_id::text || '.pdf';
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION job_object_key() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.object_key := 'org/' || NEW.org_id::text || '/' || TG_ARGV[0] || '/' || NEW.id::text;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER org_export_object_key ON org_export;
DROP TRIGGER org_import_object_key ON org_import;
CREATE TRIGGER org_export_object_key BEFORE INSERT ON org_export
    FOR EACH ROW EXECUTE FUNCTION job_object_key('org-export');
CREATE TRIGGER org_import_object_key BEFORE INSERT ON org_import
    FOR EACH ROW EXECUTE FUNCTION job_object_key('org-import');

DROP TRIGGER theme_asset_object_key ON theme_asset;
DROP TRIGGER theme_asset_object_key_stays ON theme_asset;
DROP FUNCTION theme_asset_object_key();
ALTER TABLE theme_asset DROP COLUMN object_key;
DROP TRIGGER org_brand_logo_key ON org_brand;
DROP FUNCTION brand_logo_key();
ALTER TABLE org_brand DROP COLUMN logo_key;
