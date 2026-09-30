-- +goose Up
-- A template's hints are text carrying the mark hint. They guide whoever fills
-- the page in and are never content, so nothing published keeps them: a
-- version, and a page once it has one, lose every piece of hinted text on the
-- way in. It happens here rather than in the service so that every writer is
-- held to it, the copy of an unpublished page and raw SQL included.
-- +goose StatementBegin
CREATE FUNCTION document_without_hints(node jsonb, depth integer) RETURNS jsonb
    LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE
AS $$
BEGIN
    IF depth > 40 OR jsonb_typeof(node -> 'content') IS DISTINCT FROM 'array' THEN
        RETURN node;
    END IF;
    RETURN jsonb_set(node, '{content}', COALESCE((
        SELECT jsonb_agg(document_without_hints(c.value, depth + 1) ORDER BY c.n)
        FROM jsonb_array_elements(node -> 'content') WITH ORDINALITY AS c (value, n)
        WHERE NOT (c.value ->> 'type' = 'text'
                   AND jsonb_path_exists(c.value, 'lax $.marks[*] ? (@.type == "hint")'))
    ), '[]'::jsonb));
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION strip_published_hints() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF jsonb_path_exists(NEW.body, 'lax $.**.marks[*] ? (@.type == "hint")') THEN
        NEW.body := document_without_hints(NEW.body, 0);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_version_strip_hints BEFORE INSERT OR UPDATE OF body ON page_version
    FOR EACH ROW EXECUTE FUNCTION strip_published_hints();

-- An unpublished page, version 0, keeps its template's hints: it is what its
-- creator goes on editing until the first publish.
CREATE TRIGGER page_strip_hints BEFORE INSERT OR UPDATE OF body, version ON page
    FOR EACH ROW WHEN (NEW.version > 0) EXECUTE FUNCTION strip_published_hints();

-- +goose Down
DROP TRIGGER IF EXISTS page_strip_hints ON page;
DROP TRIGGER IF EXISTS page_version_strip_hints ON page_version;
DROP FUNCTION IF EXISTS strip_published_hints();
DROP FUNCTION IF EXISTS document_without_hints(jsonb, integer);
