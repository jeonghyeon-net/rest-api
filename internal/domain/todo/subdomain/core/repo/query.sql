-- name: CreateTodo :one
INSERT INTO todos (title, body)
VALUES (?, ?)
RETURNING id, title, body, done, created_at, updated_at;

-- name: GetTodo :one
SELECT id, title, body, done, created_at, updated_at
FROM todos
WHERE id = ?;

-- name: ListTodos :many
SELECT id, title, body, done, created_at, updated_at
FROM todos
ORDER BY id DESC
LIMIT ? OFFSET ?;

-- name: CountTodos :one
SELECT COUNT(*) FROM todos;

-- name: ListTodosByTag :many
SELECT t.id, t.title, t.body, t.done, t.created_at, t.updated_at
FROM todos t
INNER JOIN todo_tags tt ON t.id = tt.todo_id
WHERE tt.tag_id = (SELECT id FROM tags WHERE name = ?)
ORDER BY t.id DESC
LIMIT ? OFFSET ?;

-- name: CountTodosByTag :one
SELECT COUNT(*)
FROM todos t
INNER JOIN todo_tags tt ON t.id = tt.todo_id
WHERE tt.tag_id = (SELECT id FROM tags WHERE name = ?);

-- name: UpdateTodo :one
UPDATE todos
SET title = ?, body = ?, done = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?
RETURNING id, title, body, done, created_at, updated_at;

-- name: DeleteTodo :exec
DELETE FROM todos WHERE id = ?;
