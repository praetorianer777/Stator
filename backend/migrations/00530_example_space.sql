-- +goose Up
-- The example space (#288): a space that explains Stator, made by an
-- administrator of the organization in one click. It is an ordinary space
-- marked as the example, so the organization has at most one and a second
-- click finds it, whatever its key turned out to be.
ALTER TABLE space
    ADD COLUMN example boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT space_example_shared CHECK (NOT example OR owner_id IS NULL);

CREATE UNIQUE INDEX space_one_example ON space (org_id) WHERE example;

-- Anybody who may create spaces makes ordinary ones; the example is the
-- organization's administrators' alone, with a token for the whole of it.
CREATE POLICY space_example_creators ON space AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (NOT example OR (perm_is_admin(current_actor_id()) AND perm_token_whole(current_actor_id())));

-- +goose StatementBegin
-- A space is the example from its creation to its deletion: marking another
-- one would move the example from under whoever reads it as such.
CREATE FUNCTION space_example_fixed() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.example IS DISTINCT FROM OLD.example THEN
        RAISE EXCEPTION 'A space is the example space from its creation on, or never. Delete the example space to make it again.'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER space_example_fixed BEFORE UPDATE OF example ON space
    FOR EACH ROW EXECUTE FUNCTION space_example_fixed();

-- +goose Down
DROP TRIGGER IF EXISTS space_example_fixed ON space;
DROP FUNCTION IF EXISTS space_example_fixed();
DROP POLICY IF EXISTS space_example_creators ON space;
DROP INDEX IF EXISTS space_one_example;
ALTER TABLE space
    DROP CONSTRAINT IF EXISTS space_example_shared,
    DROP COLUMN IF EXISTS example;
