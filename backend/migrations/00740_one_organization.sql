-- +goose Up
-- An instance serves one organization (#380). An instance that holds several
-- is refused here, before any later migration or the api assumes one: goose
-- runs this in a transaction, so the refusal leaves the database as it was
-- and migrate exits non-zero. The operator reads the message in migrate's
-- log, so it says what to do without the docs. An empty instance passes, and
-- bootstrap or seed makes its organization as before.
-- Not forced meanwhile, so that an owner that is no superuser counts every
-- organization too, and is refused here rather than by the index below with
-- nothing to act on; see 00070 (#423).
ALTER TABLE org NO FORCE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$
DECLARE
    holding integer;
    slugs   text;
BEGIN
    SELECT count(*), string_agg(slug, ', ' ORDER BY slug) INTO holding, slugs FROM org;
    IF holding > 1 THEN
        RAISE EXCEPTION 'This instance holds % organizations (%), and Stator now serves one organization per instance. Export every organization (Settings, Export and import; see docs/organization-transfer.md), delete all but one, then upgrade again.', holding, slugs
            USING ERRCODE = 'object_not_in_prerequisite_state',
                  HINT = 'Import each other organization''s archive into a fresh instance of its own. Nothing was changed by this attempt.';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE org FORCE ROW LEVEL SECURITY;

-- Holds the one organization through SQL as well as through the service,
-- for the superuser and stator_admin, whom no policy binds.
-- +goose StatementBegin
CREATE FUNCTION org_is_the_only_one() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM org) THEN
        RAISE EXCEPTION 'This instance already has its organization, and Stator serves one per instance. Import another organization''s archive into a fresh instance instead.'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER org_is_the_only_one BEFORE INSERT ON org
    FOR EACH ROW EXECUTE FUNCTION org_is_the_only_one();

-- The index holds it where the trigger cannot see the organization there is:
-- two inserts at once, or a caller row level security hides it from.
CREATE UNIQUE INDEX org_one_row ON org ((true));

-- +goose Down
DROP INDEX IF EXISTS org_one_row;
DROP TRIGGER IF EXISTS org_is_the_only_one ON org;
DROP FUNCTION IF EXISTS org_is_the_only_one();
