// Package http는 Todo 도메인의 HTTP 핸들러를 제공한다.
// huma 프레임워크를 사용하여 OpenAPI 3.1 스펙을 자동 생성하는 REST API 엔드포인트를 등록한다.
//
// huma는 Input/Output 구조체의 태그에서 OpenAPI 스펙을 자동으로 추출하므로,
// 코드가 곧 API 문서가 된다 (Code-first 접근법).
//
// NestJS에서 @Controller() + @ApiTags() + @ApiOperation()으로
// Swagger 문서를 생성하는 것과 같은 역할이다.
//
// 3-tuple 패턴을 따른다:
//   - 공개 인터페이스(Handler) — 외부에 노출되는 계약
//   - 비공개 구현체(handler)   — 실제 요청 처리 로직을 담은 구조체
//   - 생성자 함수(New)         — 구현체를 생성하여 인터페이스로 반환
package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"rest-api/internal/app"
	"rest-api/internal/domain/todo"
	"rest-api/internal/domain/todo/subdomain/core/model"
	tagmodel "rest-api/internal/domain/todo/subdomain/tag/model"
)

// ──────────────────────────────────────────────────────────────────────────────
// huma Input/Output 구조체 정의
// ──────────────────────────────────────────────────────────────────────────────
//
// huma는 Input/Output 구조체의 태그에서 OpenAPI 스펙을 자동 추출한다.
// NestJS에서 DTO에 @ApiProperty()를 붙이는 것과 같은 역할이다.
//
// Input 구조체 태그 규칙:
//   - path:"id"              → URL 경로 파라미터 (항상 필수)
//   - query:"page"           → 쿼리 파라미터 (기본 선택)
//   - Body struct { ... }    → 요청 바디 (필드는 기본 필수)
//   - required:"true"        → 필수 필드 표시
//   - doc:"설명"              → OpenAPI description
//   - minimum:"1"            → 최솟값
//   - maximum:"100"          → 최댓값
//   - minLength:"1"          → 문자열 최소 길이
//   - maxLength:"200"        → 문자열 최대 길이
//   - default:"20"           → 기본값
//
// Output 구조체 규칙:
//   - Body 필드가 JSON 응답 바디로 직렬화됨

// ─── Todo CRUD ───────────────────────────────────────────────────────────────

// CreateTodoInput은 할 일 생성 요청의 입력 구조체다.
// NestJS의 CreateTodoDto + @ApiBody()와 같다.
type CreateTodoInput struct {
	Body struct {
		Title string `doc:"할 일 제목"         json:"title"         maxLength:"200" minLength:"1"    required:"true"`
		Body  string `default:""           doc:"할 일 내용"         json:"body"     maxLength:"5000"`
	}
}

// CreateTodoOutput은 할 일 생성 응답의 출력 구조체다.
type CreateTodoOutput struct {
	Body model.TodoWithTags
}

// ListTodosInput은 할 일 목록 조회 요청의 입력 구조체다.
// 쿼리 파라미터로 페이지네이션과 태그 필터를 지원한다.
//
// fieldalignment: string 필드를 먼저 배치하여 메모리 정렬을 최적화한다.
// Go의 구조체는 필드 순서에 따라 메모리 패딩이 달라질 수 있다.
type ListTodosInput struct {
	Tag   string `doc:"태그 이름으로 필터링"                   query:"tag"`
	Page  int    `default:"1"                         doc:"페이지 번호"                  minimum:"1"   query:"page"`
	Limit int    `default:"20"                        doc:"페이지당 항목 수"               maximum:"100" minimum:"1"  query:"limit"`
}

// ListTodosOutput은 할 일 목록 조회 응답의 출력 구조체다.
type ListTodosOutput struct {
	Body model.TodoList
}

// IDParam은 경로 파라미터에서 ID를 추출하는 공통 구조체다.
// Go의 구조체 임베딩으로 여러 Input에서 재사용한다.
// NestJS에서 @Param('id', ParseIntPipe)와 같다.
type IDParam struct {
	ID int64 `doc:"리소스 ID" path:"id"`
}

// GetTodoInput은 할 일 단건 조회 요청의 입력 구조체다.
type GetTodoInput struct {
	IDParam
}

// GetTodoOutput은 할 일 단건 조회 응답의 출력 구조체다.
type GetTodoOutput struct {
	Body model.TodoWithTags
}

// UpdateTodoInput은 할 일 수정 요청의 입력 구조체다.
//
// fieldalignment: Body(익명 구조체, 포인터 포함)를 IDParam(int64) 앞에 배치하여
// 메모리 정렬을 최적화한다.
type UpdateTodoInput struct {
	Body struct {
		Title string `doc:"할 일 제목"         json:"title" maxLength:"200"  minLength:"1" required:"true"`
		Body  string `doc:"할 일 내용"         json:"body"  maxLength:"5000"`
		Done  bool   `doc:"완료 여부"          json:"done"`
	}
	IDParam
}

// UpdateTodoOutput은 할 일 수정 응답의 출력 구조체다.
type UpdateTodoOutput struct {
	Body model.TodoWithTags
}

// DeleteTodoInput은 할 일 삭제 요청의 입력 구조체다.
type DeleteTodoInput struct {
	IDParam
}

// ─── Todo-Tag 연결 ───────────────────────────────────────────────────────────

// AddTodoTagInput은 할 일에 태그를 연결하는 요청의 입력 구조체다.
type AddTodoTagInput struct {
	IDParam
	Body struct {
		TagID int64 `doc:"연결할 태그 ID" json:"tagId" minimum:"1" required:"true"`
	}
}

// RemoveTodoTagInput은 할 일에서 태그를 해제하는 요청의 입력 구조체다.
type RemoveTodoTagInput struct {
	ID    int64 `doc:"할 일 ID"     path:"id"`
	TagID int64 `doc:"태그 ID"      path:"tagId"`
}

// ─── Tag CRUD ────────────────────────────────────────────────────────────────

// CreateTagInput은 태그 생성 요청의 입력 구조체다.
type CreateTagInput struct {
	Body struct {
		Name string `doc:"태그 이름" json:"name" maxLength:"50" minLength:"1" required:"true"`
	}
}

// CreateTagOutput은 태그 생성 응답의 출력 구조체다.
type CreateTagOutput struct {
	Body tagmodel.Tag
}

// ListTagsOutput은 태그 목록 조회 응답의 출력 구조체다.
type ListTagsOutput struct {
	Body []tagmodel.Tag
}

// UpdateTagInput은 태그 수정 요청의 입력 구조체다.
//
// fieldalignment: Body(익명 구조체)를 IDParam(int64) 앞에 배치하여
// 메모리 정렬을 최적화한다.
type UpdateTagInput struct {
	Body struct {
		Name string `doc:"태그 이름" json:"name" maxLength:"50" minLength:"1" required:"true"`
	}
	IDParam
}

// UpdateTagOutput은 태그 수정 응답의 출력 구조체다.
type UpdateTagOutput struct {
	Body tagmodel.Tag
}

// DeleteTagInput은 태그 삭제 요청의 입력 구조체다.
type DeleteTagInput struct {
	IDParam
}

// ──────────────────────────────────────────────────────────────────────────────
// Handler 인터페이스 + 구현체
// ──────────────────────────────────────────────────────────────────────────────

// Handler는 Todo 도메인의 HTTP 핸들러 인터페이스다.
// RegisterRoutes로 huma API에 오퍼레이션을 등록한다.
//
// 기존에는 *fiber.App을 받았지만, huma 도입으로 huma.API를 받도록 변경되었다.
// huma.API에 오퍼레이션을 등록하면 OpenAPI 스펙이 자동으로 생성된다.
type Handler interface {
	RegisterRoutes(api huma.API)
}

// handler는 Handler 인터페이스의 비공개 구현체다.
type handler struct {
	svc todo.Service
}

// New는 Todo HTTP 핸들러를 생성한다.
func New(svc todo.Service) Handler {
	return &handler{svc: svc}
}

// RegisterRoutes는 Todo 도메인의 모든 HTTP 오퍼레이션을 huma API에 등록한다.
//
// huma.Register()는 Operation 메타데이터와 핸들러 함수를 받아
// 라우트 등록 + OpenAPI 스펙 생성을 동시에 수행한다.
//
// 기존 Fiber의 app.Group() + todos.Get("/") 패턴 대신
// huma.Register(api, Operation{Path: "/todos"}, handler) 패턴을 사용한다.
//
// NestJS에서 @Get(), @Post() 데코레이터 + @ApiOperation()으로
// 라우트와 Swagger 문서를 동시에 등록하는 것과 같다.
func (h *handler) RegisterRoutes(api huma.API) {
	h.registerTodoRoutes(api)
	h.registerTodoTagRoutes(api)
	h.registerTagRoutes(api)
}

// registerTodoRoutes는 Todo CRUD 오퍼레이션을 등록한다.
func (h *handler) registerTodoRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-todo",
		Method:        http.MethodPost,
		Path:          "/todos",
		Summary:       "할 일 생성",
		Description:   "새로운 할 일을 생성한다. 생성 직후에는 태그가 없으므로 빈 태그 목록과 함께 반환한다.",
		Tags:          []string{"Todos"},
		DefaultStatus: http.StatusCreated,
	}, h.createTodo)

	huma.Register(api, huma.Operation{
		OperationID: "list-todos",
		Method:      http.MethodGet,
		Path:        "/todos",
		Summary:     "할 일 목록 조회",
		Description: "페이지네이션된 할 일 목록을 반환한다. 태그 이름으로 필터링할 수 있다.",
		Tags:        []string{"Todos"},
	}, h.listTodos)

	huma.Register(api, huma.Operation{
		OperationID: "get-todo",
		Method:      http.MethodGet,
		Path:        "/todos/{id}",
		Summary:     "할 일 단건 조회",
		Description: "ID로 할 일을 조회하고, 연결된 태그 목록도 함께 반환한다.",
		Tags:        []string{"Todos"},
	}, h.getTodo)

	huma.Register(api, huma.Operation{
		OperationID: "update-todo",
		Method:      http.MethodPatch,
		Path:        "/todos/{id}",
		Summary:     "할 일 수정",
		Description: "할 일의 제목, 내용, 완료 여부를 수정한다.",
		Tags:        []string{"Todos"},
	}, h.updateTodo)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-todo",
		Method:        http.MethodDelete,
		Path:          "/todos/{id}",
		Summary:       "할 일 삭제",
		Description:   "할 일을 삭제한다. 연결된 태그 관계(todo_tags)는 CASCADE로 자동 삭제된다.",
		Tags:          []string{"Todos"},
		DefaultStatus: http.StatusNoContent,
	}, h.deleteTodo)
}

// registerTodoTagRoutes는 Todo-Tag 연결 오퍼레이션을 등록한다.
func (h *handler) registerTodoTagRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "add-todo-tag",
		Method:        http.MethodPost,
		Path:          "/todos/{id}/tags",
		Summary:       "할 일에 태그 연결",
		Description:   "할 일에 태그를 연결한다. 이미 연결된 태그를 다시 연결하면 409 Conflict를 반환한다.",
		Tags:          []string{"Todos"},
		DefaultStatus: http.StatusNoContent,
	}, h.addTodoTag)

	huma.Register(api, huma.Operation{
		OperationID:   "remove-todo-tag",
		Method:        http.MethodDelete,
		Path:          "/todos/{id}/tags/{tagId}",
		Summary:       "할 일에서 태그 해제",
		Description:   "할 일에서 태그 연결을 해제한다.",
		Tags:          []string{"Todos"},
		DefaultStatus: http.StatusNoContent,
	}, h.removeTodoTag)
}

// registerTagRoutes는 Tag CRUD 오퍼레이션을 등록한다.
func (h *handler) registerTagRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-tag",
		Method:        http.MethodPost,
		Path:          "/tags",
		Summary:       "태그 생성",
		Description:   "새로운 태그를 생성한다. 태그 이름은 고유해야 하며, 중복 시 409 Conflict를 반환한다.",
		Tags:          []string{"Tags"},
		DefaultStatus: http.StatusCreated,
	}, h.createTag)

	huma.Register(api, huma.Operation{
		OperationID: "list-tags",
		Method:      http.MethodGet,
		Path:        "/tags",
		Summary:     "태그 목록 조회",
		Description: "전체 태그 목록을 반환한다. 태그는 수가 적으므로 페이지네이션 없이 전체를 반환한다.",
		Tags:        []string{"Tags"},
	}, h.listTags)

	huma.Register(api, huma.Operation{
		OperationID: "update-tag",
		Method:      http.MethodPatch,
		Path:        "/tags/{id}",
		Summary:     "태그 수정",
		Description: "태그 이름을 수정한다.",
		Tags:        []string{"Tags"},
	}, h.updateTag)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-tag",
		Method:        http.MethodDelete,
		Path:          "/tags/{id}",
		Summary:       "태그 삭제",
		Description:   "태그를 삭제한다. 이 태그와 연결된 todo_tags 레코드는 CASCADE로 자동 삭제된다.",
		Tags:          []string{"Tags"},
		DefaultStatus: http.StatusNoContent,
	}, h.deleteTag)
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo 핸들러 메서드
// ──────────────────────────────────────────────────────────────────────────────
//
// huma 핸들러 시그니처: func(ctx context.Context, input *Input) (*Output, error)
//
// 기존 Fiber 핸들러와 비교:
//   - 기존: func(c fiber.Ctx) error → 직접 파싱, 검증, 응답 전송
//   - huma: func(ctx, input) (output, error) → huma가 파싱/검증/직렬화 자동 처리
//
// huma에서 nil을 반환하면:
//   - (*Output)(nil), nil → DefaultStatus로 응답 (바디 없음, 204 등에 적합)
//   - output, nil → 200(또는 DefaultStatus)으로 output.Body를 JSON 응답
//   - nil, error → 에러 응답 (GetStatus()가 있으면 해당 상태 코드, 없으면 500)

// createTodo는 새로운 할 일을 생성한다.
// POST /todos
func (h *handler) createTodo(ctx context.Context, input *CreateTodoInput) (*CreateTodoOutput, error) {
	result, err := h.svc.CreateTodo(ctx, input.Body.Title, input.Body.Body)
	if err != nil {
		return nil, wrapError(err)
	}

	return &CreateTodoOutput{Body: result}, nil
}

// listTodos는 할 일 목록을 조회한다.
// GET /todos?page=1&limit=20&tag=urgent
func (h *handler) listTodos(ctx context.Context, input *ListTodosInput) (*ListTodosOutput, error) {
	result, err := h.svc.ListTodos(ctx, input.Page, input.Limit, input.Tag)
	if err != nil {
		return nil, wrapError(err)
	}

	return &ListTodosOutput{Body: result}, nil
}

// getTodo는 특정 할 일을 ID로 조회한다.
// GET /todos/{id}
func (h *handler) getTodo(ctx context.Context, input *GetTodoInput) (*GetTodoOutput, error) {
	result, err := h.svc.GetTodo(ctx, input.ID)
	if err != nil {
		return nil, wrapError(err)
	}

	return &GetTodoOutput{Body: result}, nil
}

// updateTodo는 할 일을 수정한다.
// PATCH /todos/{id}
func (h *handler) updateTodo(ctx context.Context, input *UpdateTodoInput) (*UpdateTodoOutput, error) {
	result, err := h.svc.UpdateTodo(ctx, input.ID, input.Body.Title, input.Body.Body, input.Body.Done)
	if err != nil {
		return nil, wrapError(err)
	}

	return &UpdateTodoOutput{Body: result}, nil
}

// deleteTodo는 할 일을 삭제한다.
// DELETE /todos/{id}
//
// (*Output)(nil), nil을 반환하면 huma가 DefaultStatus(204)로 응답한다.
// huma의 204 응답 패턴에서 nil, nil 반환은 의도된 동작이다.
//
//nolint:nilnil // huma 프레임워크의 204 No Content 패턴: nil output + nil error = DefaultStatus 응답
func (h *handler) deleteTodo(ctx context.Context, input *DeleteTodoInput) (*struct{}, error) {
	if err := h.svc.DeleteTodo(ctx, input.ID); err != nil {
		return nil, wrapError(err)
	}

	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo-Tag 연결 핸들러 메서드
// ──────────────────────────────────────────────────────────────────────────────

// addTodoTag는 할 일에 태그를 연결한다.
// POST /todos/{id}/tags
//
//nolint:nilnil // huma 프레임워크의 204 No Content 패턴: nil output + nil error = DefaultStatus 응답
func (h *handler) addTodoTag(ctx context.Context, input *AddTodoTagInput) (*struct{}, error) {
	if err := h.svc.AddTodoTag(ctx, input.ID, input.Body.TagID); err != nil {
		return nil, wrapError(err)
	}

	return nil, nil
}

// removeTodoTag는 할 일에서 태그 연결을 해제한다.
// DELETE /todos/{id}/tags/{tagId}
//
//nolint:nilnil // huma 프레임워크의 204 No Content 패턴: nil output + nil error = DefaultStatus 응답
func (h *handler) removeTodoTag(ctx context.Context, input *RemoveTodoTagInput) (*struct{}, error) {
	if err := h.svc.RemoveTodoTag(ctx, input.ID, input.TagID); err != nil {
		return nil, wrapError(err)
	}

	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Tag 핸들러 메서드
// ──────────────────────────────────────────────────────────────────────────────

// createTag는 새로운 태그를 생성한다.
// POST /tags
func (h *handler) createTag(ctx context.Context, input *CreateTagInput) (*CreateTagOutput, error) {
	result, err := h.svc.CreateTag(ctx, input.Body.Name)
	if err != nil {
		return nil, wrapError(err)
	}

	return &CreateTagOutput{Body: result}, nil
}

// listTags는 전체 태그 목록을 조회한다.
// GET /tags
func (h *handler) listTags(ctx context.Context, _ *struct{}) (*ListTagsOutput, error) {
	result, err := h.svc.ListTags(ctx)
	if err != nil {
		return nil, wrapError(err)
	}

	return &ListTagsOutput{Body: result}, nil
}

// updateTag는 태그 이름을 수정한다.
// PATCH /tags/{id}
func (h *handler) updateTag(ctx context.Context, input *UpdateTagInput) (*UpdateTagOutput, error) {
	result, err := h.svc.UpdateTag(ctx, input.ID, input.Body.Name)
	if err != nil {
		return nil, wrapError(err)
	}

	return &UpdateTagOutput{Body: result}, nil
}

// deleteTag는 태그를 삭제한다.
// DELETE /tags/{id}
//
//nolint:nilnil // huma 프레임워크의 204 No Content 패턴: nil output + nil error = DefaultStatus 응답
func (h *handler) deleteTag(ctx context.Context, input *DeleteTagInput) (*struct{}, error) {
	if err := h.svc.DeleteTag(ctx, input.ID); err != nil {
		return nil, wrapError(err)
	}

	return nil, nil
}

// wrapError는 서비스 에러를 huma 에러로 변환하는 헬퍼다.
// AppError가 아닌 일반 에러는 500 Internal Server Error로 변환한다.
//
// 참고: AppError는 GetStatus() 메서드를 구현하므로
// huma가 자동으로 상태 코드를 추출한다.
// 이 함수는 AppError가 아닌 에러를 처리할 때 사용한다.
func wrapError(err error) error {
	var appErr *app.AppError
	if errors.As(err, &appErr) {
		// AppError는 GetStatus()를 구현하므로 huma가 자동 처리한다.
		return appErr
	}

	// 예상치 못한 에러는 500으로 반환한다.
	return huma.Error500InternalServerError("서버 내부 오류가 발생했습니다", err)
}
