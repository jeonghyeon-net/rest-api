//go:build unit

// 이 파일은 HTTP 핸들러의 에러 분기(error branch)를 화이트박스 테스트한다.
// 같은 패키지(package http)에 속하므로 비공개 타입(handler, wrapError)에 직접 접근 가능하다.
//
// NestJS에서 서비스를 모킹(mock)하고 컨트롤러만 테스트하는 단위 테스트와 같다.
// 예: jest.spyOn(service, 'create').mockRejectedValue(new Error('fail'))
//
// Go에서는 인터페이스를 구현하는 mock 구조체를 직접 만들어 DI로 주입한다.
// NestJS의 { provide: TodoService, useValue: mockService } 패턴과 같다.
package http

import (
	"context"
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"

	"rest-api/internal/app"
	"rest-api/internal/domain/todo/subdomain/core/model"
	tagmodel "rest-api/internal/domain/todo/subdomain/tag/model"
)

// ──────────────────────────────────────────────────────────────────────────────
// Mock Service
// ──────────────────────────────────────────────────────────────────────────────
//
// mockService는 todo.Service 인터페이스를 구현하는 테스트용 목(mock) 구조체다.
// 각 메서드에 대응하는 함수 필드(function field)를 가지고 있어,
// 테스트마다 원하는 동작을 주입할 수 있다.
//
// NestJS에서 jest.fn()으로 모킹하는 것과 같은 역할이다.
// 예: const mockCreate = jest.fn().mockResolvedValue(result)
//
// Go에서는 jest 같은 프레임워크 없이 구조체의 함수 필드로 직접 모킹한다.
// 이 패턴을 "함수 필드 mock" 또는 "stub" 패턴이라고 한다.
type mockService struct {
	createTodoFn    func(ctx context.Context, title, body string) (model.TodoWithTags, error)
	getTodoFn       func(ctx context.Context, id int64) (model.TodoWithTags, error)
	listTodosFn     func(ctx context.Context, page, limit int, tag string) (model.TodoList, error)
	updateTodoFn    func(ctx context.Context, id int64, title, body string, done bool) (model.TodoWithTags, error)
	deleteTodoFn    func(ctx context.Context, id int64) error
	createTagFn     func(ctx context.Context, name string) (tagmodel.Tag, error)
	getTagFn        func(ctx context.Context, id int64) (tagmodel.Tag, error)
	listTagsFn      func(ctx context.Context) ([]tagmodel.Tag, error)
	updateTagFn     func(ctx context.Context, id int64, name string) (tagmodel.Tag, error)
	deleteTagFn     func(ctx context.Context, id int64) error
	addTodoTagFn    func(ctx context.Context, todoID, tagID int64) error
	removeTodoTagFn func(ctx context.Context, todoID, tagID int64) error
}

// 아래 12개 메서드는 todo.Service(= svc.Service) 인터페이스를 구현한다.
// 각 메서드는 대응하는 함수 필드에 호출을 위임(delegate)한다.
//
// Go의 인터페이스 구현 방식:
//   - NestJS에서는 class MockService implements TodoService { ... }처럼
//     implements 키워드를 사용하지만,
//   - Go에서는 메서드 시그니처만 맞으면 자동으로 인터페이스를 충족한다 (덕 타이핑).
//   - 즉, mockService가 Service의 모든 메서드를 구현하면 별도의 선언 없이 Service로 사용 가능하다.

// CreateTodo는 목 서비스의 할 일 생성 메서드다.
func (m *mockService) CreateTodo(ctx context.Context, title, body string) (model.TodoWithTags, error) {
	return m.createTodoFn(ctx, title, body)
}

// GetTodo는 목 서비스의 할 일 단건 조회 메서드다.
func (m *mockService) GetTodo(ctx context.Context, id int64) (model.TodoWithTags, error) {
	return m.getTodoFn(ctx, id)
}

// ListTodos는 목 서비스의 할 일 목록 조회 메서드다.
func (m *mockService) ListTodos(ctx context.Context, page, limit int, tag string) (model.TodoList, error) {
	return m.listTodosFn(ctx, page, limit, tag)
}

// UpdateTodo는 목 서비스의 할 일 수정 메서드다.
func (m *mockService) UpdateTodo(ctx context.Context, id int64, title, body string, done bool) (model.TodoWithTags, error) {
	return m.updateTodoFn(ctx, id, title, body, done)
}

// DeleteTodo는 목 서비스의 할 일 삭제 메서드다.
func (m *mockService) DeleteTodo(ctx context.Context, id int64) error {
	return m.deleteTodoFn(ctx, id)
}

// CreateTag는 목 서비스의 태그 생성 메서드다.
func (m *mockService) CreateTag(ctx context.Context, name string) (tagmodel.Tag, error) {
	return m.createTagFn(ctx, name)
}

// GetTag는 목 서비스의 태그 단건 조회 메서드다.
func (m *mockService) GetTag(ctx context.Context, id int64) (tagmodel.Tag, error) {
	return m.getTagFn(ctx, id)
}

// ListTags는 목 서비스의 태그 목록 조회 메서드다.
func (m *mockService) ListTags(ctx context.Context) ([]tagmodel.Tag, error) {
	return m.listTagsFn(ctx)
}

// UpdateTag는 목 서비스의 태그 수정 메서드다.
func (m *mockService) UpdateTag(ctx context.Context, id int64, name string) (tagmodel.Tag, error) {
	return m.updateTagFn(ctx, id, name)
}

// DeleteTag는 목 서비스의 태그 삭제 메서드다.
func (m *mockService) DeleteTag(ctx context.Context, id int64) error {
	return m.deleteTagFn(ctx, id)
}

// AddTodoTag는 목 서비스의 할 일-태그 연결 메서드다.
func (m *mockService) AddTodoTag(ctx context.Context, todoID, tagID int64) error {
	return m.addTodoTagFn(ctx, todoID, tagID)
}

// RemoveTodoTag는 목 서비스의 할 일-태그 연결 해제 메서드다.
func (m *mockService) RemoveTodoTag(ctx context.Context, todoID, tagID int64) error {
	return m.removeTodoTagFn(ctx, todoID, tagID)
}

// ──────────────────────────────────────────────────────────────────────────────
// 핸들러 에러 분기 테스트
// ──────────────────────────────────────────────────────────────────────────────
//
// 각 핸들러 메서드가 서비스에서 에러를 반환받았을 때
// nil 출력과 함께 에러를 반환하는지 검증한다.
//
// NestJS 테스트에서 mockService.create.mockRejectedValue(error)로
// 에러 시나리오를 테스트하는 것과 같다.

// TestCreateTodoError는 createTodo 핸들러의 에러 분기를 테스트한다.
// 서비스가 에러를 반환하면 핸들러도 nil 출력 + 에러를 반환해야 한다.
func TestCreateTodoError(t *testing.T) {
	// 목 서비스: CreateTodo가 항상 ErrInternal을 반환하도록 설정한다.
	mockSvc := &mockService{
		createTodoFn: func(_ context.Context, _, _ string) (model.TodoWithTags, error) {
			return model.TodoWithTags{}, app.ErrInternal
		},
	}

	// handler 구조체에 목 서비스를 주입한다.
	// NestJS에서 Test.createTestingModule({ providers: [{ provide: Service, useValue: mock }] })와 같다.
	h := &handler{svc: mockSvc}

	// 유효한 입력을 구성한다.
	// huma가 실제 요청에서는 JSON 바디를 파싱하여 이 구조체에 채워주지만,
	// 단위 테스트에서는 직접 생성한다.
	input := &CreateTodoInput{}
	input.Body.Title = "test"
	input.Body.Body = "body"

	// 핸들러 호출 — 서비스 에러가 그대로 전달되는지 확인한다.
	out, err := h.createTodo(context.Background(), input)

	// require.Nil: 에러 시 출력이 nil이어야 한다.
	// require.Error: err가 nil이 아닌 에러여야 한다.
	// require는 testify 라이브러리로, 실패 시 즉시 테스트를 중단한다.
	// NestJS의 expect(result).toBeNull()과 같다.
	require.Nil(t, out)
	require.Error(t, err)
}

// TestListTodosError는 listTodos 핸들러의 에러 분기를 테스트한다.
func TestListTodosError(t *testing.T) {
	mockSvc := &mockService{
		listTodosFn: func(_ context.Context, _, _ int, _ string) (model.TodoList, error) {
			return model.TodoList{}, app.ErrInternal
		},
	}

	h := &handler{svc: mockSvc}

	// ListTodosInput은 쿼리 파라미터로 page, limit, tag를 받는다.
	// 기본값이 태그를 통해 설정되지만, 단위 테스트에서는 직접 값을 넣는다.
	input := &ListTodosInput{Page: 1, Limit: 20}

	out, err := h.listTodos(context.Background(), input)
	require.Nil(t, out)
	require.Error(t, err)
}

// TestUpdateTodoError는 updateTodo 핸들러의 에러 분기를 테스트한다.
func TestUpdateTodoError(t *testing.T) {
	mockSvc := &mockService{
		updateTodoFn: func(_ context.Context, _ int64, _, _ string, _ bool) (model.TodoWithTags, error) {
			return model.TodoWithTags{}, app.ErrInternal
		},
	}

	h := &handler{svc: mockSvc}

	// UpdateTodoInput은 IDParam을 임베딩하고 있으므로
	// input.ID로 경로 파라미터에 접근할 수 있다.
	// Go의 구조체 임베딩은 NestJS의 extends와 비슷하다.
	input := &UpdateTodoInput{}
	input.ID = 1
	input.Body.Title = "test"

	out, err := h.updateTodo(context.Background(), input)
	require.Nil(t, out)
	require.Error(t, err)
}

// TestDeleteTodoError는 deleteTodo 핸들러의 에러 분기를 테스트한다.
func TestDeleteTodoError(t *testing.T) {
	mockSvc := &mockService{
		deleteTodoFn: func(_ context.Context, _ int64) error {
			return app.ErrInternal
		},
	}

	h := &handler{svc: mockSvc}

	input := &DeleteTodoInput{}
	input.ID = 1

	out, err := h.deleteTodo(context.Background(), input)
	require.Nil(t, out)
	require.Error(t, err)
}

// TestAddTodoTagError는 addTodoTag 핸들러의 에러 분기를 테스트한다.
func TestAddTodoTagError(t *testing.T) {
	mockSvc := &mockService{
		addTodoTagFn: func(_ context.Context, _, _ int64) error {
			return app.ErrInternal
		},
	}

	h := &handler{svc: mockSvc}

	// AddTodoTagInput은 IDParam 임베딩 + Body 안에 TagID를 가진다.
	// 할 일 ID는 경로 파라미터, 태그 ID는 요청 바디에서 받는 구조다.
	input := &AddTodoTagInput{}
	input.ID = 1
	input.Body.TagID = 1

	out, err := h.addTodoTag(context.Background(), input)
	require.Nil(t, out)
	require.Error(t, err)
}

// TestRemoveTodoTagError는 removeTodoTag 핸들러의 에러 분기를 테스트한다.
func TestRemoveTodoTagError(t *testing.T) {
	mockSvc := &mockService{
		removeTodoTagFn: func(_ context.Context, _, _ int64) error {
			return app.ErrInternal
		},
	}

	h := &handler{svc: mockSvc}

	// RemoveTodoTagInput은 IDParam을 임베딩하지 않고 ID, TagID를 직접 필드로 갖는다.
	// 구조체 리터럴로 바로 초기화할 수 있다.
	input := &RemoveTodoTagInput{ID: 1, TagID: 1}

	out, err := h.removeTodoTag(context.Background(), input)
	require.Nil(t, out)
	require.Error(t, err)
}

// TestListTagsError는 listTags 핸들러의 에러 분기를 테스트한다.
func TestListTagsError(t *testing.T) {
	mockSvc := &mockService{
		listTagsFn: func(_ context.Context) ([]tagmodel.Tag, error) {
			return nil, app.ErrInternal
		},
	}

	h := &handler{svc: mockSvc}

	// listTags의 입력 타입은 *struct{}다.
	// huma에서 입력이 필요 없는 핸들러에 사용하는 빈 구조체 패턴이다.
	// Go의 struct{}는 메모리를 0바이트 차지하는 타입이다.
	// NestJS에서 파라미터 없이 @Get() 핸들러를 만드는 것과 같다.
	out, err := h.listTags(context.Background(), &struct{}{})
	require.Nil(t, out)
	require.Error(t, err)
}

// TestUpdateTagError는 updateTag 핸들러의 에러 분기를 테스트한다.
func TestUpdateTagError(t *testing.T) {
	mockSvc := &mockService{
		updateTagFn: func(_ context.Context, _ int64, _ string) (tagmodel.Tag, error) {
			return tagmodel.Tag{}, app.ErrInternal
		},
	}

	h := &handler{svc: mockSvc}

	input := &UpdateTagInput{}
	input.ID = 1
	input.Body.Name = "test"

	out, err := h.updateTag(context.Background(), input)
	require.Nil(t, out)
	require.Error(t, err)
}

// TestDeleteTagError는 deleteTag 핸들러의 에러 분기를 테스트한다.
func TestDeleteTagError(t *testing.T) {
	mockSvc := &mockService{
		deleteTagFn: func(_ context.Context, _ int64) error {
			return app.ErrInternal
		},
	}

	h := &handler{svc: mockSvc}

	input := &DeleteTagInput{}
	input.ID = 1

	out, err := h.deleteTag(context.Background(), input)
	require.Nil(t, out)
	require.Error(t, err)
}

// ──────────────────────────────────────────────────────────────────────────────
// wrapError 함수 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestWrapErrorNonAppError는 wrapError가 AppError가 아닌 일반 에러를 받았을 때
// HTTP 500 상태 코드의 huma StatusError로 변환하는지 검증한다.
//
// wrapError의 두 분기:
//  1. AppError인 경우 → 그대로 반환 (핸들러 에러 테스트에서 간접적으로 커버)
//  2. AppError가 아닌 경우 → huma.Error500InternalServerError로 감싸서 반환
//
// 이 테스트는 2번 분기를 직접 테스트한다.
func TestWrapErrorNonAppError(t *testing.T) {
	// errors.New로 AppError가 아닌 일반 에러를 생성한다.
	// 이는 예상치 못한 에러(DB 연결 실패, 네트워크 타임아웃 등)를 시뮬레이션한다.
	err := errors.New("unexpected error")

	// wrapError를 호출하여 에러를 변환한다.
	wrapped := wrapError(err)

	// 반환된 에러가 nil이 아닌지 확인한다.
	require.Error(t, wrapped)

	// huma.StatusError 인터페이스로 타입 단언(type assertion)하여
	// HTTP 상태 코드가 500인지 확인한다.
	//
	// 타입 단언은 Go에서 인터페이스 값의 실제 타입을 확인하는 방법이다.
	// NestJS/TypeScript의 instanceof 연산자와 비슷하다.
	// 예: if (err instanceof HttpException) { err.getStatus() }
	//
	// value, ok := err.(SomeInterface) 형태로 사용하며,
	// ok가 true이면 해당 인터페이스를 구현하고 있다는 뜻이다.
	// 이를 "comma ok" 패턴이라 부르며, Go에서 안전한 타입 확인에 널리 사용된다.
	statusErr, ok := wrapped.(huma.StatusError)
	require.True(t, ok, "반환된 에러가 huma.StatusError 인터페이스를 구현해야 한다")
	require.Equal(t, 500, statusErr.GetStatus(), "AppError가 아닌 에러는 500 상태 코드로 변환되어야 한다")
}
