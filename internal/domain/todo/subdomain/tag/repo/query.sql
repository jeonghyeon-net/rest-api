-- name: CreateTag :one
INSERT INTO tags (name)
VALUES (?)
RETURNING id, name, created_at;

-- name: GetTag :one
SELECT id, name, created_at
FROM tags
WHERE id = ?;

-- name: GetTagByName :one
SELECT id, name, created_at
FROM tags
WHERE name = ?;

-- name: ListTags :many
SELECT id, name, created_at
FROM tags
ORDER BY name ASC;

-- name: UpdateTag :one
UPDATE tags
SET name = ?
WHERE id = ?
RETURNING id, name, created_at;

-- name: DeleteTag :exec
DELETE FROM tags WHERE id = ?;

-- name: AddTodoTag :exec
INSERT OR IGNORE INTO todo_tags (todo_id, tag_id) VALUES (?, ?);

-- name: RemoveTodoTag :exec
DELETE FROM todo_tags WHERE todo_id = ? AND tag_id = ?;

-- name: ListTagsByTodoID :many
SELECT t.id, t.name, t.created_at
FROM tags t
INNER JOIN todo_tags tt ON t.id = tt.tag_id
WHERE tt.todo_id = ?
ORDER BY t.name ASC;
