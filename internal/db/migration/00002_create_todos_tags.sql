-- +goose Up
-- Todo 도메인의 핵심 테이블을 생성한다.
-- todos: Todo 엔티티, tags: Tag 엔티티, todo_tags: N:M 연결 테이블

-- todos 테이블: Todo 엔티티를 저장한다.
-- done 컬럼은 INTEGER로 0(미완료)/1(완료)을 표현한다 (SQLite에 BOOLEAN 타입 없음).
-- created_at, updated_at은 ISO 8601 형식의 UTC 문자열이다.
CREATE TABLE todos (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    done       INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- tags 테이블: Tag 엔티티를 저장한다.
-- name에 UNIQUE 제약을 걸어 중복 태그명을 방지한다.
CREATE TABLE tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- todo_tags 테이블: Todo와 Tag의 N:M 관계를 표현하는 중간(junction) 테이블이다.
-- ON DELETE CASCADE로 Todo 또는 Tag가 삭제되면 연결도 자동 삭제된다.
-- 복합 기본키(todo_id, tag_id)로 중복 연결을 방지한다.
CREATE TABLE todo_tags (
    todo_id INTEGER NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
    PRIMARY KEY (todo_id, tag_id)
);

-- +goose Down
DROP TABLE IF EXISTS todo_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS todos;
