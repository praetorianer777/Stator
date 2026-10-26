-- +goose Up
-- A send by hand took the event's next attempt number, which the automatic
-- chain's pending retry needed when it failed in turn, so the retry was
-- dropped (#531). The automatic attempts keep one number each, 1 to
-- MaxAttempts; the sends by hand are numbered among themselves.
ALTER TABLE webhook_delivery DROP CONSTRAINT webhook_delivery_once;
CREATE UNIQUE INDEX webhook_delivery_once ON webhook_delivery (endpoint_id, event_id, attempt) WHERE NOT manual;

UPDATE webhook_delivery d SET attempt = n.attempt
FROM (
    SELECT id, row_number() OVER (PARTITION BY endpoint_id, event_id ORDER BY created_at, id) AS attempt
    FROM webhook_delivery WHERE manual
) n
WHERE d.id = n.id AND d.attempt <> n.attempt;

-- +goose Down
UPDATE webhook_delivery d SET attempt = n.attempt
FROM (
    SELECT m.id,
           COALESCE((SELECT max(a.attempt) FROM webhook_delivery a
                     WHERE a.endpoint_id = m.endpoint_id AND a.event_id = m.event_id AND NOT a.manual), 0)
           + row_number() OVER (PARTITION BY m.endpoint_id, m.event_id ORDER BY m.created_at, m.id) AS attempt
    FROM webhook_delivery m WHERE m.manual
) n
WHERE d.id = n.id;
DROP INDEX IF EXISTS webhook_delivery_once;
ALTER TABLE webhook_delivery ADD CONSTRAINT webhook_delivery_once UNIQUE (endpoint_id, event_id, attempt);
