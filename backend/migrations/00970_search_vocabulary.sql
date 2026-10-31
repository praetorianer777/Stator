-- +goose Up
-- Search corrects a word that matches nothing to the nearest word Stator holds
-- (#598). The vocabulary is the lexemes of every page, comment and file name,
-- as their search_vector holds them, so a correction matches the way the word
-- itself would, and pg_trgm finds the nearest by a trigram index. A word stays
-- after whatever held it is gone: search only offers a correction when the
-- search it makes finds something the reader may view.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Short lexemes have too few trigrams to be near anything, long ones are paths
-- and hashes, and a number is never a misspelt word.
-- +goose StatementBegin
CREATE FUNCTION search_word_kept(word text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT char_length(word) BETWEEN 3 AND 40 AND word !~ '^[[:digit:][:punct:]]+$'
$$;
-- +goose StatementEnd

-- A lexeme as a tsquery operand of its own, taken as it is: casting text to
-- tsquery never normalizes, so a correction is not parsed or stemmed again.
-- +goose StatementBegin
CREATE FUNCTION search_lexeme(word text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT '''' || replace(replace(word, '\', '\\'), '''', '''''') || ''''
$$;
-- +goose StatementEnd

-- In C order, so that the words a prefix starts are one range of the key.
CREATE TABLE search_word (
    word text COLLATE "C" PRIMARY KEY
);

CREATE INDEX search_word_trgm_idx ON search_word USING gin (word gin_trgm_ops);

-- Not forced meanwhile, so that an owner that is no superuser reads every row
-- the fill takes its words from; see 00070 (#423).
ALTER TABLE page NO FORCE ROW LEVEL SECURITY;
ALTER TABLE comment NO FORCE ROW LEVEL SECURITY;
ALTER TABLE attachment NO FORCE ROW LEVEL SECURITY;

INSERT INTO search_word (word)
SELECT w FROM (
    SELECT unnest(tsvector_to_array(search_vector)) FROM page
    UNION
    SELECT unnest(tsvector_to_array(search_vector)) FROM comment
    UNION
    SELECT unnest(tsvector_to_array(search_vector)) FROM attachment
) AS l (w)
WHERE search_word_kept(w)
ON CONFLICT DO NOTHING;

ALTER TABLE attachment FORCE ROW LEVEL SECURITY;
ALTER TABLE comment FORCE ROW LEVEL SECURITY;
ALTER TABLE page FORCE ROW LEVEL SECURITY;

-- Every writer keeps the vocabulary, whatever statement it runs. An edit adds
-- only the lexemes it brought, so saving a long page probes the index for its
-- new words, not all of them, and a save that changed no word writes nothing.
-- Sorted, so two saves that bring the same words never wait on each other in
-- a circle. As the owner, because readers never reach the table itself.
-- +goose StatementBegin
CREATE FUNCTION search_word_learn() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO search_word (word)
        SELECT w FROM unnest(tsvector_to_array(NEW.search_vector)) AS l (w)
        WHERE search_word_kept(w) ORDER BY w
        ON CONFLICT DO NOTHING;
    ELSIF NEW.search_vector IS DISTINCT FROM OLD.search_vector THEN
        INSERT INTO search_word (word)
        SELECT w FROM (
            SELECT unnest(tsvector_to_array(NEW.search_vector))
            EXCEPT
            SELECT unnest(tsvector_to_array(OLD.search_vector))
        ) AS l (w)
        WHERE search_word_kept(w) ORDER BY w
        ON CONFLICT DO NOTHING;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- A generated column is never in an UPDATE's SET list, so the triggers watch
-- the columns search_vector is made of.
CREATE TRIGGER page_search_words AFTER INSERT OR UPDATE OF title, body ON page
    FOR EACH ROW EXECUTE FUNCTION search_word_learn();
CREATE TRIGGER comment_search_words AFTER INSERT OR UPDATE OF body ON comment
    FOR EACH ROW EXECUTE FUNCTION search_word_learn();
CREATE TRIGGER attachment_search_words AFTER INSERT OR UPDATE OF file_name ON attachment
    FOR EACH ROW EXECUTE FUNCTION search_word_learn();

-- The nearest word to each of words that the vocabulary does not hold, or
-- with prefix, that no word of it starts with, as quick search's words are
-- the starts of words. A word held, or with nothing near, is left out. pg_trgm
-- finds candidates by its similarity_threshold, 0.3, so threshold is no lower.
-- +goose StatementBegin
CREATE FUNCTION search_word_nearest(words text[], threshold real, prefix boolean)
    RETURNS TABLE (typed text, nearest text)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT w.typed, n.word
    FROM (SELECT DISTINCT u FROM unnest(words) AS u) AS w (typed)
    CROSS JOIN LATERAL (
        SELECT v.word FROM search_word v
        WHERE v.word % w.typed AND similarity(v.word, w.typed) >= threshold
        ORDER BY similarity(v.word, w.typed) DESC, v.word
        LIMIT 1
    ) AS n
    WHERE search_word_kept(w.typed)
      AND NOT EXISTS (SELECT 1 FROM search_word k
                      WHERE k.word >= w.typed
                        AND k.word <= CASE WHEN prefix THEN w.typed || chr(1114111) ELSE w.typed END)
$$;
-- +goose StatementEnd

-- The full search's words q with each word the vocabulary does not hold
-- replaced by its nearest: as text, written as typed but for the
-- replacements, and as the tsquery websearch_to_tsquery makes of q with the
-- replacements made in it. No row when no word was replaced, or when the
-- corrected words find nothing the actor may view, so that nobody learns a
-- word from a page they may not read, not even straight through SQL.
-- +goose StatementBegin
CREATE FUNCTION search_corrected(q text, threshold real)
    RETURNS TABLE (corrected_words text, corrected_query tsquery)
    LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
DECLARE
    rewritten tsquery := websearch_to_tsquery('stator_search'::regconfig, q);
    misspelt text[];
    meant text[];
BEGIN
    SELECT array_agg(n.typed), array_agg(n.nearest) INTO misspelt, meant
    FROM search_word_nearest(
        tsvector_to_array(to_tsvector('stator_search'::regconfig, q)),
        threshold, false) AS n;
    IF misspelt IS NULL THEN
        RETURN;
    END IF;
    FOR i IN 1 .. cardinality(misspelt) LOOP
        rewritten := ts_rewrite(rewritten, search_lexeme(misspelt[i])::tsquery, search_lexeme(meant[i])::tsquery);
    END LOOP;
    IF NOT (EXISTS (SELECT 1 FROM page_search_rows(rewritten) p WHERE p.trashed_at IS NULL AND p.version > 0)
            OR EXISTS (SELECT 1 FROM attachment_search_hits(rewritten))
            OR EXISTS (SELECT 1 FROM comment_search_hits(rewritten))) THEN
        RETURN;
    END IF;
    -- A compound token is followed by its parts, which spell it out again.
    -- ts_debug names a token's lexeme as unaccent leaves it, before simple
    -- lowercases it, so each token is read again as to_tsvector reads it.
    SELECT string_agg(COALESCE(meant[array_position(misspelt, (tsvector_to_array(to_tsvector('stator_search'::regconfig, d.token)))[1])],
                               d.token), '' ORDER BY d.n)
    INTO corrected_words
    FROM ts_debug('stator_search'::regconfig, q)
         WITH ORDINALITY AS d (alias, description, token, dictionaries, dictionary, lexemes, n)
    WHERE d.alias NOT IN ('asciihword', 'hword', 'numhword', 'url');
    corrected_query := rewritten;
    RETURN NEXT;
END;
$$;
-- +goose StatementEnd

-- Quick search's words, each the start of a title word, with each that no
-- word of the vocabulary starts with replaced by its nearest, as a tsquery
-- asking for every word as the prefix of a title word. NULL when no word was
-- replaced, or when no page the actor may view has them, as for
-- search_corrected.
-- +goose StatementBegin
CREATE FUNCTION search_prefix_corrected(words text[], threshold real) RETURNS tsquery
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH typed AS (
        SELECT u.i, l.lexeme
        FROM unnest(words) WITH ORDINALITY AS u (word, i)
        CROSS JOIN LATERAL unnest(tsvector_to_array(to_tsvector('stator_search'::regconfig, u.word))) AS l (lexeme)
    ), near AS (
        SELECT n.typed, n.nearest FROM search_word_nearest(ARRAY(SELECT lexeme FROM typed), threshold, true) AS n
    ), corrected AS (
        SELECT string_agg(search_lexeme(COALESCE(n.nearest, t.lexeme)) || ':*A', ' & ' ORDER BY t.i, t.lexeme)::tsquery AS q
        FROM typed t LEFT JOIN near n ON n.typed = t.lexeme
        HAVING count(n.nearest) > 0
    )
    SELECT c.q FROM corrected c
    WHERE EXISTS (SELECT 1 FROM page_search_rows(c.q) p WHERE p.trashed_at IS NULL AND p.version > 0)
$$;
-- +goose StatementEnd

-- Readers never list the vocabulary, nor ask search_word_nearest: they reach
-- it only through the corrections above, which answer with words of what
-- they may view.
ALTER TABLE search_word ENABLE ROW LEVEL SECURITY;
ALTER TABLE search_word FORCE ROW LEVEL SECURITY;
CREATE POLICY search_word_app ON search_word USING (true) WITH CHECK (true);
CREATE POLICY search_word_admin_bypass ON search_word TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY search_word_not_anonymous ON search_word AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
REVOKE ALL ON search_word FROM stator_app, stator_admin;

-- 00001's default privileges grant every new function to both roles, so
-- revoking it from PUBLIC alone leaves them theirs.
REVOKE EXECUTE ON FUNCTION search_word_nearest(text[], real, boolean), search_corrected(text, real),
    search_prefix_corrected(text[], real), search_word_learn() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION search_word_nearest(text[], real, boolean), search_word_learn() FROM stator_app, stator_admin;
GRANT EXECUTE ON FUNCTION search_corrected(text, real), search_prefix_corrected(text[], real) TO stator_app, stator_admin;

-- +goose Down
-- pg_trgm stays, as unaccent does after 00100: it may have been there before.
DROP TRIGGER IF EXISTS attachment_search_words ON attachment;
DROP TRIGGER IF EXISTS comment_search_words ON comment;
DROP TRIGGER IF EXISTS page_search_words ON page;
DROP FUNCTION IF EXISTS search_prefix_corrected(text[], real);
DROP FUNCTION IF EXISTS search_corrected(text, real);
DROP FUNCTION IF EXISTS search_word_nearest(text[], real, boolean);
DROP FUNCTION IF EXISTS search_word_learn();
DROP TABLE IF EXISTS search_word;
DROP FUNCTION IF EXISTS search_lexeme(text);
DROP FUNCTION IF EXISTS search_word_kept(text);
