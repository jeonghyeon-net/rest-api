//go:build unit

package svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	coremodel "rest-api/internal/domain/todo/subdomain/core/model"
	coresvc "rest-api/internal/domain/todo/subdomain/core/svc"
	tagmodel "rest-api/internal/domain/todo/subdomain/tag/model"
	tagsvc "rest-api/internal/domain/todo/subdomain/tag/svc"
)

// ──────────────────────────────────────────────────────────────────────────────
// Mock 구현체
// ──────────────────────────────────────────────────────────────────────────────
//
// Go에서 인터페이스를 mock하는 대표적인 패턴이다.
// 구조체에 함수 타입 필드를 두고, 인터페이스 메서드에서 이 필드를 호출한다.
// NestJS에서 jest.fn()으로 모킹하는 것과 비슷하지만,
// Go에서는 이처럼 명시적으로 함수 필드를 선언해야 한다.
//
// 함수 필드가 nil이면 panic이 발생하므로,
// 테스트에서 반드시 필요한 함수 필드만 초기화하면 된다.
// 호출되지 않아야 하는 메서드를 nil로 남겨두면
// 실수로 호출될 경우 panic으로 즉시 알 수 있다.

// mockTodo는 coresvc.Todo 인터페이스의 mock 구현체다.
// 각 메서드에 대응하는 함수 필드를 가지며, 테스트마다 원하는 동작을 주입한다.
type mockTodo struct {
	createFn    func(ctx context.Context, title, body string) (coremodel.Todo, error)
	getFn       func(ctx context.Context, id int64) (coremodel.Todo, error)
	listFn      func(ctx context.Context, page, limit int) ([]coremodel.Todo, int64, error)
	listByTagFn func(ctx context.Context, tagName string, page, limit int) ([]coremodel.Todo, int64, error)
	updateFn    func(ctx context.Context, id int64, title, body string, done bool) (coremodel.Todo, error)
	deleteFn    func(ctx context.Context, id int64) error
}

// 컴파일 타임에 mockTodo가 coresvc.Todo 인터페이스를 구현하는지 검증한다.
// Go에서는 인터페이스 구현이 암묵적(implicit)이므로, 이 패턴으로 컴파일 시점에 확인한다.
// NestJS에서 implements를 명시하면 컴파일러가 체크하는 것과 같은 효과다.
// 빈 인터페이스 변수에 구현체 포인터를 대입하여 타입 호환성을 검증한다.
var _ coresvc.Todo = (*mockTodo)(nil)

func (m *mockTodo) Create(ctx context.Context, title, body string) (coremodel.Todo, error) {
	return m.createFn(ctx, title, body)
}

func (m *mockTodo) Get(ctx context.Context, id int64) (coremodel.Todo, error) {
	return m.getFn(ctx, id)
}

func (m *mockTodo) List(ctx context.Context, page, limit int) ([]coremodel.Todo, int64, error) {
	return m.listFn(ctx, page, limit)
}

func (m *mockTodo) ListByTag(ctx context.Context, tagName string, page, limit int) ([]coremodel.Todo, int64, error) {
	return m.listByTagFn(ctx, tagName, page, limit)
}

func (m *mockTodo) Update(ctx context.Context, id int64, title, body string, done bool) (coremodel.Todo, error) {
	return m.updateFn(ctx, id, title, body, done)
}

func (m *mockTodo) Delete(ctx context.Context, id int64) error {
	return m.deleteFn(ctx, id)
}

// mockTag는 tagsvc.Tag 인터페이스의 mock 구현체다.
// mockTodo와 동일한 함수 필드 패턴을 사용한다.
type mockTag struct {
	createFn        func(ctx context.Context, name string) (tagmodel.Tag, error)
	getFn           func(ctx context.Context, id int64) (tagmodel.Tag, error)
	listFn          func(ctx context.Context) ([]tagmodel.Tag, error)
	updateFn        func(ctx context.Context, id int64, name string) (tagmodel.Tag, error)
	deleteFn        func(ctx context.Context, id int64) error
	addTodoTagFn    func(ctx context.Context, todoID, tagID int64) error
	removeTodoTagFn func(ctx context.Context, todoID, tagID int64) error
	listByTodoIDFn  func(ctx context.Context, todoID int64) ([]tagmodel.Tag, error)
	listByTodoIDsFn func(ctx context.Context, todoIDs []int64) (map[int64][]tagmodel.Tag, error)
}

// 컴파일 타임에 mockTag가 tagsvc.Tag 인터페이스를 구현하는지 검증한다.
var _ tagsvc.Tag = (*mockTag)(nil)

func (m *mockTag) Create(ctx context.Context, name string) (tagmodel.Tag, error) {
	return m.createFn(ctx, name)
}

func (m *mockTag) Get(ctx context.Context, id int64) (tagmodel.Tag, error) {
	return m.getFn(ctx, id)
}

func (m *mockTag) List(ctx context.Context) ([]tagmodel.Tag, error) {
	return m.listFn(ctx)
}

func (m *mockTag) Update(ctx context.Context, id int64, name string) (tagmodel.Tag, error) {
	return m.updateFn(ctx, id, name)
}

func (m *mockTag) Delete(ctx context.Context, id int64) error {
	return m.deleteFn(ctx, id)
}

func (m *mockTag) AddTodoTag(ctx context.Context, todoID, tagID int64) error {
	return m.addTodoTagFn(ctx, todoID, tagID)
}

func (m *mockTag) RemoveTodoTag(ctx context.Context, todoID, tagID int64) error {
	return m.removeTodoTagFn(ctx, todoID, tagID)
}

func (m *mockTag) ListByTodoID(ctx context.Context, todoID int64) ([]tagmodel.Tag, error) {
	return m.listByTodoIDFn(ctx, todoID)
}

func (m *mockTag) ListByTodoIDs(ctx context.Context, todoIDs []int64) (map[int64][]tagmodel.Tag, error) {
	return m.listByTodoIDsFn(ctx, todoIDs)
}

// ──────────────────────────────────────────────────────────────────────────────
// CreateTodo 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestCreateTodoError는 todoSvc.Create가 에러를 반환할 때
// public service의 CreateTodo가 래핑된 에러를 반환하는지 검증한다.
// Go에서 에러 래핑은 fmt.Errorf("... %w", err) 패턴을 사용한다.
// NestJS에서 try-catch로 에러를 잡아 새로운 HttpException으로 감싸는 것과 같다.
func TestCreateTodoError(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{
			// createFn만 설정. 나머지 메서드는 nil이므로 호출 시 panic 발생 → 테스트 실패.
			createFn: func(_ context.Context, _, _ string) (coremodel.Todo, error) {
				return coremodel.Todo{}, errors.New("db error")
			},
		},
		tagSvc: &mockTag{},
	}

	_, err := svc.CreateTodo(context.Background(), "제목", "본문")
	// require.Error는 err가 nil이 아닌지 확인한다. testify 라이브러리의 assertion이다.
	// NestJS/Jest에서 expect(err).toBeDefined()와 같다.
	require.Error(t, err)
	// require.Contains는 문자열에 특정 부분 문자열이 포함되는지 확인한다.
	// fmt.Errorf("할 일 생성 실패: %w", err)로 래핑된 에러 메시지를 검증한다.
	require.Contains(t, err.Error(), "할 일 생성 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// GetTodo 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestGetTodoError_TodoSvc는 todoSvc.Get이 에러를 반환할 때
// 첫 번째 에러 분기("할 일 조회 실패")를 검증한다.
func TestGetTodoError_TodoSvc(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{
			getFn: func(_ context.Context, _ int64) (coremodel.Todo, error) {
				return coremodel.Todo{}, errors.New("not found")
			},
		},
		tagSvc: &mockTag{},
	}

	_, err := svc.GetTodo(context.Background(), 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일 조회 실패")
}

// TestGetTodoError_TagSvc는 todoSvc.Get은 성공하지만
// tagSvc.ListByTodoID가 에러를 반환할 때 두 번째 에러 분기를 검증한다.
// 이 테스트에서는 todoSvc.Get이 정상 응답을 반환해야
// 코드가 tagSvc.ListByTodoID 호출까지 도달한다.
func TestGetTodoError_TagSvc(t *testing.T) {
	now := time.Now()

	svc := &service{
		todoSvc: &mockTodo{
			// Get은 성공해야 다음 단계(태그 조회)까지 코드가 진행된다.
			getFn: func(_ context.Context, _ int64) (coremodel.Todo, error) {
				return coremodel.Todo{
					ID:        1,
					Title:     "제목",
					Body:      "본문",
					Done:      false,
					CreatedAt: now,
					UpdatedAt: now,
				}, nil
			},
		},
		tagSvc: &mockTag{
			listByTodoIDFn: func(_ context.Context, _ int64) ([]tagmodel.Tag, error) {
				return nil, errors.New("tag db error")
			},
		},
	}

	_, err := svc.GetTodo(context.Background(), 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일 태그 목록 조회 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// ListTodos 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestListTodosError_NoTag는 tag 파라미터가 빈 문자열일 때
// todoSvc.List가 에러를 반환하는 경우를 검증한다.
func TestListTodosError_NoTag(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{
			listFn: func(_ context.Context, _, _ int) ([]coremodel.Todo, int64, error) {
				return nil, 0, errors.New("db error")
			},
		},
		tagSvc: &mockTag{},
	}

	_, err := svc.ListTodos(context.Background(), 1, 10, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일 목록 조회 실패")
}

// TestListTodosError_WithTag는 tag 파라미터가 있을 때
// todoSvc.ListByTag가 에러를 반환하는 경우를 검증한다.
// Go에는 메서드 오버로딩이 없으므로 tag 유무에 따라 if-else로 분기한다.
func TestListTodosError_WithTag(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{
			listByTagFn: func(_ context.Context, _ string, _, _ int) ([]coremodel.Todo, int64, error) {
				return nil, 0, errors.New("db error")
			},
		},
		tagSvc: &mockTag{},
	}

	_, err := svc.ListTodos(context.Background(), 1, 10, "important")
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일 목록 조회 실패")
}

// TestListTodosError_TagBatch는 todoSvc.List는 성공하지만
// tagSvc.ListByTodoIDs(배치 태그 조회)가 에러를 반환하는 경우를 검증한다.
// N+1 문제를 해결하기 위해 도입된 배치 조회의 에러 분기를 커버한다.
//
// todoSvc.List가 1개 이상의 Todo를 반환해야
// 코드가 ListByTodoIDs 호출까지 도달한다.
func TestListTodosError_TagBatch(t *testing.T) {
	now := time.Now()

	svc := &service{
		todoSvc: &mockTodo{
			// List는 정상 Todo 1개를 반환하여 배치 태그 조회까지 도달하게 한다.
			listFn: func(_ context.Context, _, _ int) ([]coremodel.Todo, int64, error) {
				return []coremodel.Todo{
					{ID: 1, Title: "제목", Body: "본문", Done: false, CreatedAt: now, UpdatedAt: now},
				}, 1, nil
			},
		},
		tagSvc: &mockTag{
			listByTodoIDsFn: func(_ context.Context, _ []int64) (map[int64][]tagmodel.Tag, error) {
				return nil, errors.New("batch tag error")
			},
		},
	}

	_, err := svc.ListTodos(context.Background(), 1, 10, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 배치 조회 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// UpdateTodo 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestUpdateTodoError_TodoSvc는 todoSvc.Update가 에러를 반환할 때
// 첫 번째 에러 분기("할 일 수정 실패")를 검증한다.
func TestUpdateTodoError_TodoSvc(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{
			updateFn: func(_ context.Context, _ int64, _, _ string, _ bool) (coremodel.Todo, error) {
				return coremodel.Todo{}, errors.New("update failed")
			},
		},
		tagSvc: &mockTag{},
	}

	_, err := svc.UpdateTodo(context.Background(), 1, "제목", "본문", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일 수정 실패")
}

// TestUpdateTodoError_TagSvc는 todoSvc.Update는 성공하지만
// tagSvc.ListByTodoID가 에러를 반환할 때 두 번째 에러 분기를 검증한다.
func TestUpdateTodoError_TagSvc(t *testing.T) {
	now := time.Now()

	svc := &service{
		todoSvc: &mockTodo{
			// Update는 성공해야 다음 단계(태그 조회)까지 코드가 진행된다.
			updateFn: func(_ context.Context, _ int64, _, _ string, _ bool) (coremodel.Todo, error) {
				return coremodel.Todo{
					ID:        1,
					Title:     "수정된 제목",
					Body:      "수정된 본문",
					Done:      true,
					CreatedAt: now,
					UpdatedAt: now,
				}, nil
			},
		},
		tagSvc: &mockTag{
			listByTodoIDFn: func(_ context.Context, _ int64) ([]tagmodel.Tag, error) {
				return nil, errors.New("tag query error")
			},
		},
	}

	_, err := svc.UpdateTodo(context.Background(), 1, "수정된 제목", "수정된 본문", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일 태그 목록 조회 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// DeleteTodo 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestDeleteTodoError는 todoSvc.Delete가 에러를 반환할 때
// 래핑된 에러("할 일 삭제 실패")가 반환되는지 검증한다.
func TestDeleteTodoError(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{
			deleteFn: func(_ context.Context, _ int64) error {
				return errors.New("delete failed")
			},
		},
		tagSvc: &mockTag{},
	}

	err := svc.DeleteTodo(context.Background(), 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일 삭제 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// GetTag 테스트 (기존 커버리지 0%)
// ──────────────────────────────────────────────────────────────────────────────

// TestGetTagError는 tagSvc.Get이 에러를 반환할 때
// 래핑된 에러("태그 조회 실패")가 반환되는지 검증한다.
// 이 메서드는 기존에 커버리지가 0%였으므로 에러/성공 양쪽 모두 테스트한다.
func TestGetTagError(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{},
		tagSvc: &mockTag{
			getFn: func(_ context.Context, _ int64) (tagmodel.Tag, error) {
				return tagmodel.Tag{}, errors.New("tag not found")
			},
		},
	}

	_, err := svc.GetTag(context.Background(), 99)
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 조회 실패")
}

// TestGetTagSuccess는 tagSvc.Get이 성공적으로 태그를 반환할 때
// public service가 올바른 결과를 그대로 전달하는지 검증한다.
// 커버리지 0%인 GetTag의 정상 경로를 커버한다.
func TestGetTagSuccess(t *testing.T) {
	now := time.Now()

	svc := &service{
		todoSvc: &mockTodo{},
		tagSvc: &mockTag{
			getFn: func(_ context.Context, _ int64) (tagmodel.Tag, error) {
				return tagmodel.Tag{
					ID:        1,
					Name:      "중요",
					CreatedAt: now,
				}, nil
			},
		},
	}

	tag, err := svc.GetTag(context.Background(), 1)
	// require.NoError는 err가 nil인지 확인한다.
	// NestJS/Jest에서 expect(err).toBeNull()과 같다.
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.ID)
	require.Equal(t, "중요", tag.Name)
	require.Equal(t, now, tag.CreatedAt)
}

// ──────────────────────────────────────────────────────────────────────────────
// ListTags 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestListTagsError는 tagSvc.List가 에러를 반환할 때
// 래핑된 에러("태그 목록 조회 실패")가 반환되는지 검증한다.
func TestListTagsError(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{},
		tagSvc: &mockTag{
			listFn: func(_ context.Context) ([]tagmodel.Tag, error) {
				return nil, errors.New("list failed")
			},
		},
	}

	_, err := svc.ListTags(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 목록 조회 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// UpdateTag 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestUpdateTagError는 tagSvc.Update가 에러를 반환할 때
// 래핑된 에러("태그 수정 실패")가 반환되는지 검증한다.
func TestUpdateTagError(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{},
		tagSvc: &mockTag{
			updateFn: func(_ context.Context, _ int64, _ string) (tagmodel.Tag, error) {
				return tagmodel.Tag{}, errors.New("update failed")
			},
		},
	}

	_, err := svc.UpdateTag(context.Background(), 1, "새이름")
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 수정 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// DeleteTag 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestDeleteTagError는 tagSvc.Delete가 에러를 반환할 때
// 래핑된 에러("태그 삭제 실패")가 반환되는지 검증한다.
func TestDeleteTagError(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{},
		tagSvc: &mockTag{
			deleteFn: func(_ context.Context, _ int64) error {
				return errors.New("delete failed")
			},
		},
	}

	err := svc.DeleteTag(context.Background(), 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 삭제 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// AddTodoTag 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestAddTodoTagError는 tagSvc.AddTodoTag가 에러를 반환할 때
// 래핑된 에러("할 일-태그 연결 실패")가 반환되는지 검증한다.
func TestAddTodoTagError(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{},
		tagSvc: &mockTag{
			addTodoTagFn: func(_ context.Context, _, _ int64) error {
				return errors.New("link failed")
			},
		},
	}

	err := svc.AddTodoTag(context.Background(), 1, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일-태그 연결 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// RemoveTodoTag 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestRemoveTodoTagError는 tagSvc.RemoveTodoTag가 에러를 반환할 때
// 래핑된 에러("할 일-태그 연결 해제 실패")가 반환되는지 검증한다.
func TestRemoveTodoTagError(t *testing.T) {
	svc := &service{
		todoSvc: &mockTodo{},
		tagSvc: &mockTag{
			removeTodoTagFn: func(_ context.Context, _, _ int64) error {
				return errors.New("unlink failed")
			},
		},
	}

	err := svc.RemoveTodoTag(context.Background(), 1, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일-태그 연결 해제 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// toTodoTags 헬퍼 함수 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestToTodoTags_Nil은 nil 슬라이스가 입력될 때
// 빈 슬라이스(null이 아닌 [])가 반환되는지 검증한다.
// Go에서 nil 슬라이스와 빈 슬라이스는 다르다:
//   - nil 슬라이스: var s []int → JSON으로 직렬화하면 null
//   - 빈 슬라이스: s := []int{} 또는 make([]int, 0) → JSON으로 직렬화하면 []
//
// API 응답에서 tags 필드가 null이 아닌 []로 나와야 하므로
// toTodoTags는 nil 입력에도 빈 슬라이스를 반환해야 한다.
func TestToTodoTags_Nil(t *testing.T) {
	result := toTodoTags(nil)
	// nil이 아닌 빈 슬라이스인지 확인한다.
	// require.NotNil은 포인터/슬라이스가 nil이 아닌지 확인한다.
	require.NotNil(t, result)
	require.Empty(t, result)
}

// TestToTodoTags_Empty는 빈 슬라이스가 입력될 때
// 빈 슬라이스가 반환되는지 검증한다.
func TestToTodoTags_Empty(t *testing.T) {
	result := toTodoTags([]tagmodel.Tag{})
	require.NotNil(t, result)
	require.Empty(t, result)
}

// TestToTodoTags_WithTags는 실제 태그가 있을 때
// tagmodel.Tag → coremodel.TodoTag 변환이 올바르게 수행되는지 검증한다.
// for 루프 내부의 변환 로직(ID, Name 매핑)을 커버한다.
// TypeScript에서 tags.map(t => ({ id: t.id, name: t.name }))과 같은 변환이다.
func TestToTodoTags_WithTags(t *testing.T) {
	now := time.Now()

	// 2개의 태그를 입력하여 루프가 실제로 실행되는지 확인한다.
	input := []tagmodel.Tag{
		{ID: 1, Name: "중요", CreatedAt: now},
		{ID: 2, Name: "긴급", CreatedAt: now},
	}

	result := toTodoTags(input)

	require.Len(t, result, 2)

	// 첫 번째 태그 변환 검증
	require.Equal(t, int64(1), result[0].ID)
	require.Equal(t, "중요", result[0].Name)

	// 두 번째 태그 변환 검증
	require.Equal(t, int64(2), result[1].ID)
	require.Equal(t, "긴급", result[1].Name)
}
