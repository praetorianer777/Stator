-- +goose Up
-- A task's notification names the task, so its link can open the page at it.
-- No key: the task is a node of the page's document and the page may change.
ALTER TABLE notification ADD COLUMN task_id uuid;

-- +goose Down
ALTER TABLE notification DROP COLUMN IF EXISTS task_id;
