-- +goose Up
-- A status label is its words and a date its YYYY-MM-DD in the page's text, as
-- in document.PlainText, so search finds a page by them. No body held either
-- node before this, so no stored search vector is stale.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_plain_blocks(blocks jsonb, depth integer) RETURNS text
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
                    WHEN 'armatureIssue' THEN line := line || COALESCE(c->'attrs'->>'key', '');
                    WHEN 'status' THEN line := line || COALESCE(c->'attrs'->>'label', '');
                    WHEN 'date' THEN line := line || COALESCE(c->'attrs'->>'date', '');
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
        WHEN 'armatureIssueBlock' THEN
            IF COALESCE(n->'attrs'->>'key', '') <> '' THEN
                out := out || (n->'attrs'->>'key') || E'\n';
            END IF;
        WHEN 'expand' THEN
            IF jsonb_typeof(n->'attrs'->'title') = 'string' AND n->'attrs'->>'title' <> '' THEN
                out := out || (n->'attrs'->>'title') || E'\n';
            END IF;
            out := out || page_plain_blocks(n->'content', depth + 1);
        ELSE
            out := out || page_plain_blocks(n->'content', depth + 1);
        END CASE;
    END LOOP;
    RETURN out;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION page_plain_blocks(blocks jsonb, depth integer) RETURNS text
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
                    WHEN 'armatureIssue' THEN line := line || COALESCE(c->'attrs'->>'key', '');
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
        WHEN 'armatureIssueBlock' THEN
            IF COALESCE(n->'attrs'->>'key', '') <> '' THEN
                out := out || (n->'attrs'->>'key') || E'\n';
            END IF;
        WHEN 'expand' THEN
            IF jsonb_typeof(n->'attrs'->'title') = 'string' AND n->'attrs'->>'title' <> '' THEN
                out := out || (n->'attrs'->>'title') || E'\n';
            END IF;
            out := out || page_plain_blocks(n->'content', depth + 1);
        ELSE
            out := out || page_plain_blocks(n->'content', depth + 1);
        END CASE;
    END LOOP;
    RETURN out;
END;
$$;
-- +goose StatementEnd
