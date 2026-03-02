package svc

import (
	"context"
	"database/sql"
	"fmt"

	"rest-api/internal/domain/todo/subdomain/core/model"
	coresvc "rest-api/internal/domain/todo/subdomain/core/svc"
	tagmodel "rest-api/internal/domain/todo/subdomain/tag/model"
	tagsvc "rest-api/internal/domain/todo/subdomain/tag/svc"
)

// Service는 Todo 도메인의 공개(Public) 서비스 인터페이스다.
// 외부 패키지(handler, saga 등)는 이 인터페이스를 통해서만 Todo 도메인에 접근한다.
// NestJS에서 모듈의 exports에 등록하는 서비스와 같다.
//
// 내부적으로 core(Todo)와 tag(Tag) 서브도메인 서비스를 조합하여
// 비즈니스 유스케이스를 구현한다.
//
// 3-tuple 패턴:
//   - 공개 인터페이스(Service) — 외부에 노출되는 계약
//   - 비공개 구현체(service)   — 실제 로직을 담은 구조체
//   - 생성자 함수(New)         — 구현체를 생성하여 인터페이스로 반환
type Service interface {
	// === Todo 관련 ===

	// CreateTodo는 새로운 할 일을 생성한다.
	// 생성 직후에는 태그가 없으므로 빈 태그 목록과 함께 반환한다.
	CreateTodo(ctx context.Context, title, body string) (model.TodoWithTags, error)

	// GetTodo는 ID로 할 일을 조회하고, 연결된 태그 목록도 함께 반환한다.
	GetTodo(ctx context.Context, id int64) (model.TodoWithTags, error)

	// ListTodos는 페이지네이션된 할 일 목록을 반환한다.
	// tag 파라미터가 비어있으면 전체 목록, 값이 있으면 해당 태그로 필터링한다.
	ListTodos(ctx context.Context, page, limit int, tag string) (model.TodoList, error)

	// UpdateTodo는 할 일을 수정하고, 연결된 태그 목록도 함께 반환한다.
	UpdateTodo(ctx context.Context, id int64, title, body string, done bool) (model.TodoWithTags, error)

	// DeleteTodo는 할 일을 삭제한다.
	DeleteTodo(ctx context.Context, id int64) error

	// === Tag 관련 ===

	// CreateTag는 새로운 태그를 생성한다.
	CreateTag(ctx context.Context, name string) (tagmodel.Tag, error)

	// GetTag는 ID로 태그를 조회한다.
	GetTag(ctx context.Context, id int64) (tagmodel.Tag, error)

	// ListTags는 전체 태그 목록을 반환한다.
	ListTags(ctx context.Context) ([]tagmodel.Tag, error)

	// UpdateTag는 태그 이름을 수정한다.
	UpdateTag(ctx context.Context, id int64, name string) (tagmodel.Tag, error)

	// DeleteTag는 태그를 삭제한다.
	DeleteTag(ctx context.Context, id int64) error

	// === Todo-Tag 연결 ===

	// AddTodoTag는 할 일에 태그를 연결한다 (다대다 관계).
	AddTodoTag(ctx context.Context, todoID, tagID int64) error

	// RemoveTodoTag는 할 일에서 태그 연결을 해제한다.
	RemoveTodoTag(ctx context.Context, todoID, tagID int64) error
}

// service는 Service 인터페이스의 비공개 구현체다.
// core(Todo) 서브도메인과 tag(Tag) 서브도메인의 서비스를 합성(composition)한다.
// NestJS에서 여러 서비스를 @Inject()로 주입받는 Facade 서비스와 같다.
type service struct {
	todoSvc coresvc.Todo // core 서브도메인의 Todo 서비스
	tagSvc  tagsvc.Tag   // tag 서브도메인의 Tag 서비스
}

// New는 Todo 도메인의 Public Service를 생성한다.
// *sql.DB를 받아서 내부적으로 core 서비스와 tag 서비스를 각각 생성한다.
// NestJS에서 모듈의 providers에 여러 서비스를 등록하고,
// Facade 서비스가 이를 주입받는 것과 같다.
//
// 반환 타입이 구현체(service)가 아닌 인터페이스(Service)인 점에 주의.
// Go의 캡슐화 패턴: 외부에서는 구현 세부사항을 알 수 없다.
func New(db *sql.DB) Service {
	return &service{
		todoSvc: coresvc.New(db),
		tagSvc:  tagsvc.New(db),
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo 관련 메서드
// ──────────────────────────────────────────────────────────────────────────────

// CreateTodo는 새로운 할 일을 생성한다.
// core 서비스에 생성을 위임하고, 빈 태그 목록과 함께 TodoWithTags로 반환한다.
// 새로 생성된 할 일에는 아직 태그가 연결되지 않았으므로 빈 슬라이스를 사용한다.
func (s *service) CreateTodo(ctx context.Context, title, body string) (model.TodoWithTags, error) {
	todo, err := s.todoSvc.Create(ctx, title, body)
	if err != nil {
		return model.TodoWithTags{}, fmt.Errorf("할 일 생성 실패: %w", err)
	}

	return model.TodoWithTags{
		Todo: todo,
		Tags: []model.TodoTag{}, // 새 Todo에는 태그 없음
	}, nil
}

// GetTodo는 ID로 할 일을 조회하고, 연결된 태그 목록도 함께 가져온다.
// 두 서브도메인(core + tag)을 조합하는 오케스트레이션 로직이다.
// NestJS에서 여러 Repository를 조회하여 하나의 DTO로 조합하는 것과 같다.
func (s *service) GetTodo(ctx context.Context, id int64) (model.TodoWithTags, error) {
	// 1. core 서비스로 할 일 조회
	todo, err := s.todoSvc.Get(ctx, id)
	if err != nil {
		return model.TodoWithTags{}, fmt.Errorf("할 일 조회 실패: %w", err)
	}

	// 2. tag 서비스로 해당 할 일에 연결된 태그 목록 조회
	tags, err := s.tagSvc.ListByTodoID(ctx, id)
	if err != nil {
		return model.TodoWithTags{}, fmt.Errorf("할 일 태그 목록 조회 실패: %w", err)
	}

	return model.TodoWithTags{
		Todo: todo,
		Tags: toTodoTags(tags), // tagmodel.Tag → model.TodoTag 변환
	}, nil
}

// ListTodos는 페이지네이션된 할 일 목록을 반환한다.
// tag 파라미터가 비어있으면 전체 목록, 값이 있으면 해당 태그로 필터링한다.
//
// 각 할 일마다 태그 목록을 별도 조회한다 (N+1 패턴).
// sqlc.slice()가 SQLite에서 동작하지 않아 IN 쿼리로 한 번에 가져올 수 없으므로
// 현재는 이 방식을 사용한다. 성능이 문제되면 추후 최적화할 수 있다.
func (s *service) ListTodos(ctx context.Context, page, limit int, tag string) (model.TodoList, error) {
	var (
		todos []model.Todo
		total int64
		err   error
	)

	// tag 필터 유무에 따라 다른 메서드를 호출한다.
	// Go에는 메서드 오버로딩이 없으므로 if-else로 분기한다.
	if tag == "" {
		// 태그 필터 없음: 전체 목록 조회
		todos, total, err = s.todoSvc.List(ctx, page, limit)
	} else {
		// 태그 이름으로 필터링된 목록 조회
		todos, total, err = s.todoSvc.ListByTag(ctx, tag, page, limit)
	}

	if err != nil {
		return model.TodoList{}, fmt.Errorf("할 일 목록 조회 실패: %w", err)
	}

	// 각 할 일에 대해 태그 목록을 조회하여 TodoWithTags로 조합한다.
	// N+1 쿼리 패턴: todos가 N개면 태그 조회 쿼리가 N번 추가 실행된다.
	items := make([]model.TodoWithTags, 0, len(todos))

	for _, todo := range todos {
		tags, err := s.tagSvc.ListByTodoID(ctx, todo.ID)
		if err != nil {
			return model.TodoList{}, fmt.Errorf("할 일(ID=%d) 태그 목록 조회 실패: %w", todo.ID, err)
		}

		items = append(items, model.TodoWithTags{
			Todo: todo,
			Tags: toTodoTags(tags),
		})
	}

	// totalPages 계산: ceiling division (올림 나눗셈).
	// (total + limit - 1) / limit는 올림 나눗셈의 정수 연산 공식이다.
	// 예: total=11, limit=5 → (11+4)/5 = 3페이지
	// total이 0이면 나눗셈 결과도 0이므로 별도 처리가 불필요하다.
	var totalPages int64
	if total > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}

	return model.TodoList{
		Data: items,
		Meta: model.PageMeta{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// UpdateTodo는 할 일을 수정하고, 연결된 태그 목록도 함께 반환한다.
// GetTodo와 마찬가지로 core + tag 서비스를 조합한다.
func (s *service) UpdateTodo(ctx context.Context, id int64, title, body string, done bool) (model.TodoWithTags, error) {
	// 1. core 서비스로 할 일 수정
	todo, err := s.todoSvc.Update(ctx, id, title, body, done)
	if err != nil {
		return model.TodoWithTags{}, fmt.Errorf("할 일 수정 실패: %w", err)
	}

	// 2. tag 서비스로 연결된 태그 목록 조회
	tags, err := s.tagSvc.ListByTodoID(ctx, id)
	if err != nil {
		return model.TodoWithTags{}, fmt.Errorf("할 일 태그 목록 조회 실패: %w", err)
	}

	return model.TodoWithTags{
		Todo: todo,
		Tags: toTodoTags(tags),
	}, nil
}

// DeleteTodo는 할 일을 삭제한다.
// core 서비스에 직접 위임한다. 태그 연결(todo_tags)은 CASCADE로 자동 삭제된다.
func (s *service) DeleteTodo(ctx context.Context, id int64) error {
	if err := s.todoSvc.Delete(ctx, id); err != nil {
		return fmt.Errorf("할 일 삭제 실패: %w", err)
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Tag 관련 메서드 — tag 서브도메인 서비스에 직접 위임한다.
// ──────────────────────────────────────────────────────────────────────────────

// CreateTag는 새로운 태그를 생성한다. tag 서비스에 위임한다.
func (s *service) CreateTag(ctx context.Context, name string) (tagmodel.Tag, error) {
	tag, err := s.tagSvc.Create(ctx, name)
	if err != nil {
		return tagmodel.Tag{}, fmt.Errorf("태그 생성 실패: %w", err)
	}

	return tag, nil
}

// GetTag는 ID로 태그를 조회한다. tag 서비스에 위임한다.
func (s *service) GetTag(ctx context.Context, id int64) (tagmodel.Tag, error) {
	tag, err := s.tagSvc.Get(ctx, id)
	if err != nil {
		return tagmodel.Tag{}, fmt.Errorf("태그 조회 실패: %w", err)
	}

	return tag, nil
}

// ListTags는 전체 태그 목록을 반환한다. tag 서비스에 위임한다.
func (s *service) ListTags(ctx context.Context) ([]tagmodel.Tag, error) {
	tags, err := s.tagSvc.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("태그 목록 조회 실패: %w", err)
	}

	return tags, nil
}

// UpdateTag는 태그 이름을 수정한다. tag 서비스에 위임한다.
func (s *service) UpdateTag(ctx context.Context, id int64, name string) (tagmodel.Tag, error) {
	tag, err := s.tagSvc.Update(ctx, id, name)
	if err != nil {
		return tagmodel.Tag{}, fmt.Errorf("태그 수정 실패: %w", err)
	}

	return tag, nil
}

// DeleteTag는 태그를 삭제한다. tag 서비스에 위임한다.
func (s *service) DeleteTag(ctx context.Context, id int64) error {
	if err := s.tagSvc.Delete(ctx, id); err != nil {
		return fmt.Errorf("태그 삭제 실패: %w", err)
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo-Tag 연결 메서드 — tag 서브도메인 서비스에 직접 위임한다.
// ──────────────────────────────────────────────────────────────────────────────

// AddTodoTag는 할 일에 태그를 연결한다. tag 서비스에 위임한다.
func (s *service) AddTodoTag(ctx context.Context, todoID, tagID int64) error {
	if err := s.tagSvc.AddTodoTag(ctx, todoID, tagID); err != nil {
		return fmt.Errorf("할 일-태그 연결 실패: %w", err)
	}

	return nil
}

// RemoveTodoTag는 할 일에서 태그 연결을 해제한다. tag 서비스에 위임한다.
func (s *service) RemoveTodoTag(ctx context.Context, todoID, tagID int64) error {
	if err := s.tagSvc.RemoveTodoTag(ctx, todoID, tagID); err != nil {
		return fmt.Errorf("할 일-태그 연결 해제 실패: %w", err)
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// 내부 헬퍼 함수
// ──────────────────────────────────────────────────────────────────────────────

// toTodoTags는 tag 서브도메인의 []tagmodel.Tag를 core 서브도메인의 []model.TodoTag로 변환한다.
// 서브도메인 간 모델 변환은 Public Service(이 파일)에서 담당한다.
//
// core 서브도메인은 tag 서브도메인의 모델을 직접 import할 수 없으므로
// (아키텍처 규칙: 서브도메인 간 의존은 core 방향만 허용),
// TodoTag라는 별도 구조체를 통해 필요한 필드만 전달한다.
//
// Go에는 배열/슬라이스의 map 함수가 내장되어 있지 않아서
// for 루프로 하나씩 변환해야 한다.
// TypeScript에서는 tags.map(t => ({ id: t.id, name: t.name }))로 간단히 할 수 있다.
func toTodoTags(tags []tagmodel.Tag) []model.TodoTag {
	// make로 정확한 크기의 슬라이스를 미리 할당한다.
	// len=0, cap=len(tags)로 생성하여 append 시 재할당을 방지한다.
	result := make([]model.TodoTag, 0, len(tags))

	for _, t := range tags {
		result = append(result, model.TodoTag{
			ID:   t.ID,
			Name: t.Name,
		})
	}

	return result
}
