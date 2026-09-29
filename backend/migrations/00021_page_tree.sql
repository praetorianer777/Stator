-- +goose Up
-- A page may move anywhere in its tree except under itself or one of its own
-- pages, which would cut it and everything below it off from the home page.
-- The service asks first so it can answer in a sentence; this refuses the same
-- move made any other way.
--
-- Two moves in one space that are each fine alone can close a loop together,
-- so every change of a parent takes the space's lock first and checks against
-- what the other move has committed. Both spaces of a move between two are
-- locked in one order, so two such moves cannot wait on each other.
-- +goose StatementBegin
CREATE FUNCTION page_tree_guard() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.parent_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.space_id IS DISTINCT FROM NEW.space_id THEN
        PERFORM pg_advisory_xact_lock(hashtextextended('page-tree:' || least(OLD.space_id, NEW.space_id)::text, 0));
        PERFORM pg_advisory_xact_lock(hashtextextended('page-tree:' || greatest(OLD.space_id, NEW.space_id)::text, 0));
    ELSE
        PERFORM pg_advisory_xact_lock(hashtextextended('page-tree:' || NEW.space_id::text, 0));
    END IF;
    IF NEW.parent_id = NEW.id OR EXISTS (
        WITH RECURSIVE above (id, parent_id) AS (
            SELECT id, parent_id FROM page WHERE id = NEW.parent_id
            UNION
            SELECT p.id, p.parent_id FROM page p JOIN above a ON p.id = a.parent_id
        )
        SELECT 1 FROM above WHERE id = NEW.id
    ) THEN
        RAISE EXCEPTION 'a page cannot move under itself or one of its own pages'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'page_tree_no_cycle';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_tree_check BEFORE INSERT OR UPDATE OF parent_id, space_id ON page
    FOR EACH ROW EXECUTE FUNCTION page_tree_guard();

-- +goose Down
DROP TRIGGER IF EXISTS page_tree_check ON page;
DROP FUNCTION IF EXISTS page_tree_guard();
