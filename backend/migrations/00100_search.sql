-- +goose Up
-- Full-text search over what a page publishes: its title, weighted A, and the
-- plain text of its body, weighted B, so a title match ranks above a body
-- match. Matching ignores case and accents but does not stem, as in Armature,
-- because a wiki's pages are written in more than one language.
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE TEXT SEARCH CONFIGURATION stator_search (COPY = pg_catalog.simple);
ALTER TEXT SEARCH CONFIGURATION stator_search
    ALTER MAPPING FOR hword, hword_part, word WITH unaccent, simple;

-- The words of a document the way document.PlainText reads them: one block per
-- line, table cells separated by tabs, mentions as @Name. It lives in the
-- database so every writer keeps the index true, whatever statement it runs;
-- the integration suite holds the two to the same answer.
-- +goose StatementBegin
CREATE FUNCTION page_plain_blocks(blocks jsonb, depth integer) RETURNS text
    LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE
AS $$
DECLARE
    n      jsonb;
    c      jsonb;
    cell   jsonb;
    line   text;
    cells  text[];
    out    text := '';
BEGIN
    IF depth > 40 OR jsonb_typeof(blocks) IS DISTINCT FROM 'array' THEN
        RETURN '';
    END IF;
    FOR n IN SELECT value FROM jsonb_array_elements(blocks) LOOP
        CASE n->>'type'
        WHEN 'paragraph', 'heading', 'codeBlock' THEN
            line := '';
            IF jsonb_typeof(n->'content') = 'array' THEN
                FOR c IN SELECT value FROM jsonb_array_elements(n->'content') LOOP
                    CASE c->>'type'
                    WHEN 'text' THEN line := line || COALESCE(c->>'text', '');
                    WHEN 'mention' THEN line := line || '@' || COALESCE(c->'attrs'->>'label', '');
                    WHEN 'hardBreak' THEN line := line || E'\n';
                    ELSE NULL;
                    END CASE;
                END LOOP;
            END IF;
            IF line <> '' THEN
                out := out || line || E'\n';
            END IF;
        WHEN 'tableRow' THEN
            cells := '{}';
            IF jsonb_typeof(n->'content') = 'array' THEN
                FOR cell IN SELECT value FROM jsonb_array_elements(n->'content') LOOP
                    cells := cells || replace(btrim(page_plain_blocks(cell->'content', depth + 1), E' \t\n\r'), E'\n', ' ');
                END LOOP;
            END IF;
            out := out || array_to_string(cells, E'\t') || E'\n';
        ELSE
            out := out || page_plain_blocks(n->'content', depth + 1);
        END CASE;
    END LOOP;
    RETURN out;
END;
$$;
-- +goose StatementEnd

CREATE FUNCTION page_plain_text(body jsonb) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    RETURN btrim(page_plain_blocks(body->'content', 0), E' \t\n\r');

-- A tsvector holds at most 1 MB of lexemes. A page may be 2 MB of JSON, so the
-- body's text is cut at 200000 characters, 800 KB at worst, and what lies past
-- that is not found; positions past 16383 collapse in any case.
ALTER TABLE page ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
    setweight(to_tsvector('stator_search'::regconfig, title), 'A') ||
    setweight(to_tsvector('stator_search'::regconfig, left(page_plain_text(body), 200000)), 'B')
) STORED;

CREATE INDEX page_search_idx ON page USING gin (search_vector);

-- A file is found by its name. The parser reads "plan_v2.pdf" as one path, so
-- the name is also indexed with every run of punctuation as a space, which
-- finds it by "plan" as well. The bytes are in the bucket, out of reach here.
ALTER TABLE attachment ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
    setweight(to_tsvector('stator_search'::regconfig,
        file_name || ' ' || regexp_replace(file_name, '[^[:alnum:]]+', ' ', 'g')), 'A')
) STORED;

CREATE INDEX attachment_search_idx ON attachment USING gin (search_vector);

-- The last time each person opened each page, for their recent pages.
CREATE TABLE page_visit (
    org_id     uuid NOT NULL,
    user_id    uuid NOT NULL,
    page_id    uuid NOT NULL,
    visited_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id, page_id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE
);

CREATE INDEX page_visit_recent_idx ON page_visit (org_id, user_id, visited_at DESC);
CREATE INDEX page_visit_page_idx ON page_visit (org_id, page_id);

ALTER TABLE page_visit ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_visit FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_visit_tenant_isolation ON page_visit
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_visit_admin_bypass ON page_visit TO stator_admin USING (true) WITH CHECK (true);

-- A person's visits are their own, and only of pages they may still view.
CREATE POLICY page_visit_viewer ON page_visit AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id()))
    WITH CHECK (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id()));

GRANT SELECT, INSERT, UPDATE, DELETE ON page_visit TO stator_app, stator_admin;

-- +goose Down
DROP TABLE IF EXISTS page_visit;
DROP INDEX IF EXISTS attachment_search_idx;
ALTER TABLE attachment DROP COLUMN IF EXISTS search_vector;
DROP INDEX IF EXISTS page_search_idx;
ALTER TABLE page DROP COLUMN IF EXISTS search_vector;
DROP FUNCTION IF EXISTS page_plain_text(jsonb);
DROP FUNCTION IF EXISTS page_plain_blocks(jsonb, integer);
DROP TEXT SEARCH CONFIGURATION IF EXISTS stator_search;
