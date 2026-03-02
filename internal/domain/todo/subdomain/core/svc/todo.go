package svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"rest-api/internal/app"
	"rest-api/internal/domain/todo/subdomain/core/model"
	"rest-api/internal/domain/todo/subdomain/core/repo"
)

// isoLayout는 SQLite에서 사용하는 ISO 8601 시간 형식이다.
// Go에서 시간 포맷은 "2006-01-02T15:04:05Z"라는 특수한 참조 시간(reference time)을 사용한다.
// 이 날짜(2006년 1월 2일 15:04:05)는 Go 개발팀이 정한 고정 참조값으로,
// 다른 언어의 "YYYY-MM-DDTHH:mm:ssZ" 패턴과 같은 역할이다.
const isoLayout = "2006-01-02T15:04:05Z"

// Todo는 할 일 비즈니스 로직을 정의하는 인터페이스다.
// 3-tuple 패턴: 이 인터페이스 + 아래 비공개 구현체 + New 생성자로 구성된다.
// NestJS의 @Injectable() 서비스 인터페이스와 같다.
//
// 각 메서드의 역할:
//   - Create: 새 할 일을 생성한다.
//   - Get: ID로 할 일 하나를 조회한다.
//   - List: 페이지네이션된 할 일 목록과 전체 개수를 반환한다.
//   - ListByTag: 특정 태그가 붙은 할 일을 페이지네이션하여 반환한다.
//   - Update: 할 일을 수정한다.
//   - Delete: 할 일을 삭제한다.
type Todo interface {
	Create(ctx context.Context, title, body string) (model.Todo, error)
	Get(ctx context.Context, id int64) (model.Todo, error)
	List(ctx context.Context, page, limit int) ([]model.Todo, int64, error)
	ListByTag(ctx context.Context, tagName string, page, limit int) ([]model.Todo, int64, error)
	Update(ctx context.Context, id int64, title, body string, done bool) (model.Todo, error)
	Delete(ctx context.Context, id int64) error
}

// todo는 Todo 인터페이스의 비공개 구현체다.
// Go에서 소문자로 시작하는 타입은 패키지 외부에서 접근할 수 없다(unexported).
// 외부에서는 반드시 Todo 인터페이스를 통해서만 사용한다.
// NestJS에서 private class가 없으므로, 이 패턴은 Go 특유의 캡슐화 방식이다.
type todo struct {
	q *repo.Queries
}

// New는 Todo 서비스를 생성한다.
// *sql.DB를 받아서 내부적으로 SQLC의 repo.Queries를 생성한다.
// 반환 타입이 구현체(todo)가 아닌 인터페이스(Todo)인 점에 주의.
// NestJS에서 @Injectable() 데코레이터가 붙은 클래스의 생성자와 같다.
//
// *sql.DB는 repo.DBTX 인터페이스를 구현(implement)하므로 repo.New에 바로 전달 가능하다.
// Go에서는 인터페이스를 명시적으로 implements 하지 않아도, 필요한 메서드만 갖추면 자동 충족된다.
func New(db *sql.DB) Todo {
	return &todo{q: repo.New(db)}
}

// Create는 새로운 할 일을 생성한다.
// SQLC가 생성한 q.CreateTodo를 호출하고, repo 타입을 도메인 모델로 변환하여 반환한다.
// NestJS에서 this.repository.save(entity)와 같은 패턴이다.
func (t *todo) Create(ctx context.Context, title, body string) (model.Todo, error) {
	// repo.CreateTodoParams는 SQLC가 SQL 쿼리의 파라미터를 기반으로 자동 생성한 구조체다.
	row, err := t.q.CreateTodo(ctx, repo.CreateTodoParams{
		Title: title,
		Body:  body,
	})
	if err != nil {
		return model.Todo{}, fmt.Errorf("할 일 생성 실패: %w", err)
	}

	return repoToModel(row)
}

// Get은 ID로 할 일을 조회한다.
// sql.ErrNoRows 에러를 app.ErrNotFound로 변환한다.
// NestJS에서 findOneOrFail + NotFoundException 패턴과 같다.
//
// errors.Is는 에러 체인을 탐색하여 특정 에러와 일치하는지 확인한다.
// TypeScript에서 instanceof 체크와 유사하지만, 감싸진(wrapped) 에러도 찾아낸다.
func (t *todo) Get(ctx context.Context, id int64) (model.Todo, error) {
	row, err := t.q.GetTodo(ctx, id)
	if err != nil {
		// sql.ErrNoRows: DB에서 결과가 없을 때 반환되는 표준 에러다.
		// 이를 비즈니스 에러(app.ErrNotFound)로 변환하여 HTTP 404 응답으로 연결한다.
		if errors.Is(err, sql.ErrNoRows) {
			return model.Todo{}, app.ErrNotFound
		}

		return model.Todo{}, fmt.Errorf("할 일 조회 실패: %w", err)
	}

	return repoToModel(row)
}

// List는 페이지네이션된 할 일 목록을 반환한다.
// 목록 데이터([]model.Todo)와 전체 개수(int64)를 함께 반환한다.
// Go에서는 다중 반환값을 사용하여 NestJS의 { data, total } 객체와 같은 효과를 낸다.
//
// page는 1부터 시작하는 페이지 번호, limit는 페이지당 항목 수다.
// offset = (page - 1) * limit 으로 계산한다.
func (t *todo) List(ctx context.Context, page, limit int) ([]model.Todo, int64, error) {
	// offset 기반 페이지네이션: 1페이지는 offset=0, 2페이지는 offset=limit, ...
	offset := int64((page - 1) * limit)

	rows, err := t.q.ListTodos(ctx, repo.ListTodosParams{
		Limit:  int64(limit),
		Offset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("할 일 목록 조회 실패: %w", err)
	}

	// 전체 개수를 별도 쿼리로 조회한다.
	// NestJS + TypeORM에서 findAndCount()가 내부적으로 하는 것과 같다.
	total, err := t.q.CountTodos(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("할 일 개수 조회 실패: %w", err)
	}

	// repo 타입 슬라이스를 model 타입 슬라이스로 변환한다.
	todos, err := repoSliceToModel(rows)
	if err != nil {
		return nil, 0, err
	}

	return todos, total, nil
}

// ListByTag는 특정 태그가 붙은 할 일을 페이지네이션하여 반환한다.
// List와 동일한 패턴이지만, 태그 이름으로 필터링한다.
func (t *todo) ListByTag(ctx context.Context, tagName string, page, limit int) ([]model.Todo, int64, error) {
	offset := int64((page - 1) * limit)

	rows, err := t.q.ListTodosByTag(ctx, repo.ListTodosByTagParams{
		Name:   tagName,
		Limit:  int64(limit),
		Offset: offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("태그별 할 일 목록 조회 실패: %w", err)
	}

	total, err := t.q.CountTodosByTag(ctx, tagName)
	if err != nil {
		return nil, 0, fmt.Errorf("태그별 할 일 개수 조회 실패: %w", err)
	}

	todos, err := repoSliceToModel(rows)
	if err != nil {
		return nil, 0, err
	}

	return todos, total, nil
}

// Update는 할 일을 수정한다.
// bool 타입의 done 값을 int64로 변환해야 한다 (SQLite에는 boolean 타입이 없다).
// sql.ErrNoRows를 app.ErrNotFound로 변환한다.
func (t *todo) Update(ctx context.Context, id int64, title, body string, done bool) (model.Todo, error) {
	// Go에는 삼항 연산자(ternary)가 없으므로 if-else로 bool→int64 변환한다.
	// TypeScript: const doneInt = done ? 1 : 0
	var doneInt int64
	if done {
		doneInt = 1
	}

	row, err := t.q.UpdateTodo(ctx, repo.UpdateTodoParams{
		ID:    id,
		Title: title,
		Body:  body,
		Done:  doneInt,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Todo{}, app.ErrNotFound
		}

		return model.Todo{}, fmt.Errorf("할 일 수정 실패: %w", err)
	}

	return repoToModel(row)
}

// Delete는 할 일을 삭제한다.
// SQLC의 DeleteTodo는 :exec 태그로 생성되어 영향받은 행 수를 반환하지 않는다.
// 따라서 존재하지 않는 ID를 삭제해도 에러가 발생하지 않는다 (멱등성).
func (t *todo) Delete(ctx context.Context, id int64) error {
	if err := t.q.DeleteTodo(ctx, id); err != nil {
		return fmt.Errorf("할 일 삭제 실패: %w", err)
	}

	return nil
}

// repoToModel은 SQLC가 생성한 repo.Todo를 도메인 모델 model.Todo로 변환한다.
// 주요 변환:
//   - CreatedAt, UpdatedAt: string → time.Time (ISO 8601 파싱)
//   - Done: int64 → bool (0이 아니면 true)
//
// SQLC는 SQLite의 TEXT 컬럼을 string으로, INTEGER 컬럼을 int64로 생성하므로
// 도메인 모델의 time.Time, bool과 맞추려면 변환이 필요하다.
func repoToModel(repoTodo repo.Todo) (model.Todo, error) {
	// time.Parse는 참조 시간 포맷을 기준으로 문자열을 파싱한다.
	// NestJS에서 dayjs("2024-01-01T00:00:00Z").toDate()와 유사하다.
	createdAt, err := time.Parse(isoLayout, repoTodo.CreatedAt)
	if err != nil {
		return model.Todo{}, fmt.Errorf("created_at 파싱 실패: %w", err)
	}

	updatedAt, err := time.Parse(isoLayout, repoTodo.UpdatedAt)
	if err != nil {
		return model.Todo{}, fmt.Errorf("updated_at 파싱 실패: %w", err)
	}

	return model.Todo{
		ID:        repoTodo.ID,
		Title:     repoTodo.Title,
		Body:      repoTodo.Body,
		Done:      repoTodo.Done != 0, // int64 → bool: 0이면 false, 나머지는 true
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

// repoSliceToModel은 repo.Todo 슬라이스를 model.Todo 슬라이스로 변환한다.
// Go에서는 배열/슬라이스의 map 함수가 내장되어 있지 않아서
// for 루프로 하나씩 변환해야 한다.
// TypeScript에서는 rows.map(r => toModel(r))로 간단히 할 수 있지만,
// Go에서는 이렇게 명시적으로 반복해야 한다.
func repoSliceToModel(rows []repo.Todo) ([]model.Todo, error) {
	// make로 정확한 크기의 슬라이스를 미리 할당한다.
	// 이렇게 하면 append 시 내부 배열 재할당이 발생하지 않아 성능이 좋다.
	todos := make([]model.Todo, 0, len(rows))

	for _, r := range rows {
		m, err := repoToModel(r)
		if err != nil {
			return nil, err
		}
		todos = append(todos, m)
	}

	return todos, nil
}
