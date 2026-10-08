-- +goose Up
-- The trigger that lets everyone use a new organization runs as the role that
-- ran the migrations. Where that is a superuser, as in the development stack,
-- row level security does not apply to it; where it is an ordinary owner, as
-- under CloudNativePG, the tenant policy on global_grant refuses the row,
-- because nothing has named the new organization as the tenant yet. So the
-- function names it for the one insert, then puts back what was there.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION org_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    before text := current_setting('app.org_id', true);
BEGIN
    PERFORM set_config('app.org_id', NEW.id::text, true);
    INSERT INTO global_grant (org_id, permission, subject_type) VALUES (NEW.id, 'use', 'everyone');
    PERFORM set_config('app.org_id', COALESCE(before, ''), true);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION org_default_grants() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    INSERT INTO global_grant (org_id, permission, subject_type) VALUES (NEW.id, 'use', 'everyone');
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
