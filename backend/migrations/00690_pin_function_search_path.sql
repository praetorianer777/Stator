-- +goose Up
-- A dump restores with an empty search_path, and a PL/pgSQL function resolves
-- the names in its body when it runs, not when it is made: page_plain_blocks,
-- which the stored search vector of page calls while its rows are copied back,
-- could not find itself. Every PL/pgSQL function pins the path it was written
-- for; SQL functions are left alone, since a SET would stop them being inlined
-- into the policies that call them.
-- +goose StatementBegin
DO $$
DECLARE
    f regprocedure;
BEGIN
    FOR f IN
        SELECT p.oid::regprocedure
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        JOIN pg_language l ON l.oid = p.prolang
        WHERE n.nspname = 'public' AND l.lanname = 'plpgsql'
          AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')
          AND NOT EXISTS (SELECT 1 FROM unnest(p.proconfig) c WHERE c LIKE 'search_path=%')
    LOOP
        EXECUTE format('ALTER FUNCTION %s SET search_path = public, pg_temp', f);
    END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- The paths stay pinned: nothing depended on their absence.
SELECT 1;
