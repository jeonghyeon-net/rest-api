// Package http는 Todo 도메인의 HTTP 핸들러를 제공한다.
// Fiber 웹 프레임워크를 사용하여 REST API 엔드포인트를 등록한다.
//
// NestJS에서 @Controller() 데코레이터가 붙은 클래스와 같은 역할이다.
// NestJS 컨트롤러가 서비스를 주입받듯이, 이 핸들러도 todo.Service를 주입받는다.
//
// 3-tuple 패턴을 따른다:
//   - 공개 인터페이스(Handler) — 외부에 노출되는 계약
//   - 비공개 구현체(handler)   — 실제 요청 처리 로직을 담은 구조체
//   - 생성자 함수(New)         — 구현체를 생성하여 인터페이스로 반환
package http

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"rest-api/internal/app"
	"rest-api/internal/domain/todo"
)

// defaultLimit는 할 일 목록 조회 시 기본 페이지 크기다.
// 매직 넘버를 상수로 추출하여 의미를 명확히 한다.
const defaultLimit = 20

// maxLimit는 한 번에 조회할 수 있는 최대 항목 수다.
// 너무 큰 값이 들어오면 이 값으로 제한한다.
const maxLimit = 100

// ──────────────────────────────────────────────────────────────────────────────
// 요청 바디 구조체
// ──────────────────────────────────────────────────────────────────────────────
//
// 각 구조체의 필드에 붙은 태그(tag)는 두 가지 역할을 한다:
//   - json:"title"     → JSON 직렬화/역직렬화 시 필드 이름을 지정한다.
//   - validate:"..."   → go-playground/validator 라이브러리의 검증 규칙이다.
//
// NestJS의 DTO + class-validator 데코레이터와 같은 역할이다:
//
//	class CreateTodoDto {
//	    @IsString() @MinLength(1) @MaxLength(200) title: string;
//	    @IsString() @MaxLength(5000) @IsOptional() body: string;
//	}

// createTodoReq는 할 일 생성 요청 바디다.
type createTodoReq struct {
	Title string `json:"title" validate:"required,min=1,max=200"`
	Body  string `json:"body"  validate:"max=5000"`
}

// updateTodoReq는 할 일 수정 요청 바디다.
// Done 필드는 bool이므로 기본값이 false다 — 클라이언트가 명시하지 않으면 false로 설정된다.
type updateTodoReq struct {
	Title string `json:"title" validate:"required,min=1,max=200"`
	Body  string `json:"body"  validate:"max=5000"`
	Done  bool   `json:"done"`
}

// createTagReq는 태그 생성 요청 바디다.
type createTagReq struct {
	Name string `json:"name" validate:"required,min=1,max=50"`
}

// updateTagReq는 태그 수정 요청 바디다.
type updateTagReq struct {
	Name string `json:"name" validate:"required,min=1,max=50"`
}

// addTodoTagReq는 할 일-태그 연결 요청 바디다.
// TagID를 JSON에서 받아 해당 태그를 할 일에 연결한다.
type addTodoTagReq struct {
	TagID int64 `json:"tagId" validate:"required,gt=0"`
}

// ──────────────────────────────────────────────────────────────────────────────
// Handler 인터페이스 + 구현체
// ──────────────────────────────────────────────────────────────────────────────

// Handler는 Todo 도메인의 HTTP 핸들러 인터페이스다.
// RegisterRoutes로 Fiber 앱에 라우트를 등록한다.
//
// NestJS에서 컨트롤러가 @Controller('todos') 데코레이터로
// 라우트를 자동 등록하는 것과 달리,
// Go에서는 RegisterRoutes 메서드를 통해 명시적으로 등록한다.
type Handler interface {
	RegisterRoutes(app *fiber.App)
}

// handler는 Handler 인터페이스의 비공개 구현체다.
// todo.Service(alias.go에서 정의한 Public Service)를 의존성으로 가진다.
//
// NestJS에서 컨트롤러가 constructor(private todoService: TodoService)로
// 서비스를 주입받는 것과 같다.
type handler struct {
	svc todo.Service
}

// New는 Todo HTTP 핸들러를 생성한다.
// fx.Provide에 등록하여 DI 컨테이너가 자동으로 todo.Service를 주입하게 한다.
//
// 반환 타입이 구현체(handler)가 아닌 인터페이스(Handler)인 점에 주의.
// 이렇게 하면 테스트에서 Handler를 모킹할 수 있고,
// 외부에서 구현 세부사항에 의존하지 않게 된다.
func New(svc todo.Service) Handler {
	return &handler{svc: svc}
}

// RegisterRoutes는 Todo 도메인의 모든 HTTP 엔드포인트를 Fiber 앱에 등록한다.
//
// NestJS에서는 @Controller 데코레이터와 @Get(), @Post() 등으로 자동 등록되지만,
// Go에서는 이렇게 명시적으로 라우트를 정의한다.
//
// app.Group()은 NestJS의 @Controller('todos')에서 경로 접두사를 지정하는 것과 같다.
// :id<int>는 Fiber의 경로 파라미터 제약 조건이다 — 정수만 매칭된다.
// NestJS의 @Param('id', ParseIntPipe)와 유사한 역할이다.
func (h *handler) RegisterRoutes(fiberApp *fiber.App) {
	// ─── Todo CRUD ────────────────────────────────────────────────────────
	// StrictRouting=true 설정에서 Group("/todos") + Post("/")는 "/todos/"로 등록되어
	// POST /todos 요청이 404가 된다.
	// 빈 문자열("")을 사용하면 "/todos"로 정확히 등록된다.
	// REST API 표준: POST /todos, GET /todos?page=1 (트레일링 슬래시 없음)
	todos := fiberApp.Group("/todos")
	todos.Post("", h.createTodo)
	todos.Get("", h.listTodos)
	todos.Get("/:id<int>", h.getTodo)
	todos.Patch("/:id<int>", h.updateTodo)
	todos.Delete("/:id<int>", h.deleteTodo)

	// ─── Todo-Tag 연결 ───────────────────────────────────────────────────
	// 특정 할 일에 태그를 추가/제거하는 엔드포인트다.
	// REST에서 다대다(M:N) 관계의 표준 패턴:
	//   POST   /todos/:id/tags      → 태그 연결
	//   DELETE /todos/:id/tags/:tagId → 태그 연결 해제
	todos.Post("/:id<int>/tags", h.addTodoTag)
	todos.Delete("/:id<int>/tags/:tagId<int>", h.removeTodoTag)

	// ─── Tag CRUD ─────────────────────────────────────────────────────────
	tags := fiberApp.Group("/tags")
	tags.Post("", h.createTag)
	tags.Get("", h.listTags)
	tags.Patch("/:id<int>", h.updateTag)
	tags.Delete("/:id<int>", h.deleteTag)
}

// ──────────────────────────────────────────────────────────────────────────────
// 내부 헬퍼 함수
// ──────────────────────────────────────────────────────────────────────────────

// paramInt는 경로 파라미터를 int64로 파싱하는 헬퍼 함수다.
// Fiber v3에서는 ParamsInt 메서드가 제거되었으므로,
// c.Params()로 문자열을 가져온 뒤 strconv.ParseInt로 변환한다.
//
// 변환 실패 시 (0, error)를 반환한다.
// 호출자는 에러 발생 시 app.ErrBadRequest를 반환하면 된다.
func paramInt(c fiber.Ctx, key string) (int64, error) {
	// c.Params(key)는 URL 경로 파라미터를 문자열로 반환한다.
	// NestJS의 @Param('id')와 같다.
	raw := c.Params(key)

	// strconv.ParseInt는 문자열을 정수로 변환한다.
	// 두 번째 인자(10)는 10진수, 세 번째 인자(64)는 64비트 정수를 의미한다.
	// JavaScript의 parseInt(raw, 10)과 유사하지만, 비트 크기를 명시해야 한다.
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("경로 파라미터 %s 파싱 실패: %w", key, err)
	}

	return val, nil
}

// queryInt는 쿼리 파라미터를 정수로 파싱하는 헬퍼 함수다.
// Fiber v3에서는 QueryInt 메서드가 제거되었으므로,
// c.Query()로 문자열을 가져온 뒤 strconv.Atoi로 변환한다.
//
// 파라미터가 없거나 파싱 실패 시 기본값(defaultVal)을 반환한다.
// NestJS의 @Query('page', new DefaultValuePipe(1), ParseIntPipe)와 유사하다.
func queryInt(c fiber.Ctx, key string, defaultVal int) int {
	raw := c.Query(key)
	if raw == "" {
		return defaultVal
	}

	// strconv.Atoi는 문자열을 int로 변환한다.
	// ParseInt의 축약 버전으로, 10진수 + 시스템 기본 비트 크기를 사용한다.
	val, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}

	return val
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo 핸들러 메서드
// ──────────────────────────────────────────────────────────────────────────────

// createTodo는 새로운 할 일을 생성하는 핸들러다.
// POST /todos
//
// c.Bind().Body(req)는 JSON 파싱과 검증을 한 번에 수행한다.
// NestJS의 @Body() + ValidationPipe가 자동으로 DTO를 파싱+검증하는 것과 같다.
// 검증 실패 시 반환되는 에러는 글로벌 에러 핸들러(errors.go)가 422 응답으로 변환한다.
func (h *handler) createTodo(c fiber.Ctx) error {
	// new()는 Go에서 구조체의 제로값 포인터를 생성하는 내장 함수다.
	// &createTodoReq{}와 동일하지만 더 간결하다.
	req := new(createTodoReq)

	// Bind().Body()는 요청 바디를 구조체로 파싱한 뒤,
	// StructValidator(server.go에서 등록)를 사용하여 validate 태그를 검증한다.
	// 파싱 실패나 검증 실패 시 에러를 반환하며,
	// 글로벌 에러 핸들러가 적절한 HTTP 응답으로 변환한다.
	//
	// %w로 래핑하면 errors.As/errors.Is가 원본 에러 타입을 찾을 수 있어서
	// 글로벌 에러 핸들러의 validator.ValidationErrors 감지가 정상 동작한다.
	if err := c.Bind().Body(req); err != nil {
		return fmt.Errorf("요청 바디 파싱 실패: %w", err)
	}

	result, err := h.svc.CreateTodo(c.Context(), req.Title, req.Body)
	if err != nil {
		return fmt.Errorf("할 일 생성 실패: %w", err)
	}

	// fiber.StatusCreated는 HTTP 201이다.
	// REST 관례상 리소스 생성 성공 시 201을 반환한다.
	return c.Status(fiber.StatusCreated).JSON(result) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// listTodos는 할 일 목록을 조회하는 핸들러다.
// GET /todos?page=1&limit=20&tag=urgent
//
// 쿼리 파라미터로 페이지네이션과 태그 필터를 지원한다.
// NestJS에서 @Query('page') page: number와 같다.
func (h *handler) listTodos(c fiber.Ctx) error {
	// queryInt 헬퍼로 쿼리 파라미터를 정수로 파싱한다.
	// 파라미터가 없거나 파싱 실패 시 기본값을 사용한다.
	page := queryInt(c, "page", 1)
	limit := queryInt(c, "limit", defaultLimit)
	tag := c.Query("tag")

	// 유효성 검사: 비정상적인 값을 안전한 범위로 보정한다.
	// Go에는 Math.min/max가 없으므로 if문으로 직접 처리한다.
	if page < 1 {
		page = 1
	}

	if limit < 1 {
		limit = 1
	}

	if limit > maxLimit {
		limit = maxLimit
	}

	result, err := h.svc.ListTodos(c.Context(), page, limit, tag)
	if err != nil {
		return fmt.Errorf("할 일 목록 조회 실패: %w", err)
	}

	// 기본 200 OK로 응답한다.
	return c.JSON(result) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// getTodo는 특정 할 일을 ID로 조회하는 핸들러다.
// GET /todos/:id
//
// paramInt 헬퍼로 URL 경로 파라미터를 int64로 파싱한다.
// :id<int> 제약 조건이 있어도, 파싱 에러 처리는 안전하게 수행한다.
func (h *handler) getTodo(c fiber.Ctx) error {
	id, err := paramInt(c, "id")
	if err != nil {
		return app.ErrBadRequest
	}

	result, err := h.svc.GetTodo(c.Context(), id)
	if err != nil {
		return fmt.Errorf("할 일 조회 실패: %w", err)
	}

	return c.JSON(result) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// updateTodo는 할 일을 수정하는 핸들러다.
// PATCH /todos/:id
//
// 경로 파라미터(id)와 요청 바디(title, body, done)를 모두 사용한다.
func (h *handler) updateTodo(c fiber.Ctx) error {
	id, err := paramInt(c, "id")
	if err != nil {
		return app.ErrBadRequest
	}

	req := new(updateTodoReq)
	if parseErr := c.Bind().Body(req); parseErr != nil {
		return fmt.Errorf("요청 바디 파싱 실패: %w", parseErr)
	}

	result, err := h.svc.UpdateTodo(c.Context(), id, req.Title, req.Body, req.Done)
	if err != nil {
		return fmt.Errorf("할 일 수정 실패: %w", err)
	}

	return c.JSON(result) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// deleteTodo는 할 일을 삭제하는 핸들러다.
// DELETE /todos/:id
//
// 삭제 성공 시 204 No Content를 반환한다.
// REST 관례상 삭제 성공 시 응답 바디 없이 204를 반환한다.
func (h *handler) deleteTodo(c fiber.Ctx) error {
	id, err := paramInt(c, "id")
	if err != nil {
		return app.ErrBadRequest
	}

	if deleteErr := h.svc.DeleteTodo(c.Context(), id); deleteErr != nil {
		return fmt.Errorf("할 일 삭제 실패: %w", deleteErr)
	}

	// SendStatus는 상태 코드만 설정하고 바디 없이 응답한다.
	// fiber.StatusNoContent는 HTTP 204다.
	return c.SendStatus(fiber.StatusNoContent) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo-Tag 연결 핸들러 메서드
// ──────────────────────────────────────────────────────────────────────────────

// addTodoTag는 할 일에 태그를 연결하는 핸들러다.
// POST /todos/:id/tags
//
// 경로 파라미터(id)로 할 일을, 요청 바디(tagId)로 태그를 지정한다.
// 성공 시 204 No Content를 반환한다 (새로운 리소스를 생성하는 것이 아니라
// 기존 리소스 간의 관계를 설정하는 것이므로 204가 적절하다).
func (h *handler) addTodoTag(c fiber.Ctx) error {
	id, err := paramInt(c, "id")
	if err != nil {
		return app.ErrBadRequest
	}

	req := new(addTodoTagReq)
	if parseErr := c.Bind().Body(req); parseErr != nil {
		return fmt.Errorf("요청 바디 파싱 실패: %w", parseErr)
	}

	if addErr := h.svc.AddTodoTag(c.Context(), id, req.TagID); addErr != nil {
		return fmt.Errorf("할 일-태그 연결 실패: %w", addErr)
	}

	return c.SendStatus(fiber.StatusNoContent) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// removeTodoTag는 할 일에서 태그 연결을 해제하는 핸들러다.
// DELETE /todos/:id/tags/:tagId
//
// 두 개의 경로 파라미터(id, tagId)를 사용한다.
// 성공 시 204 No Content를 반환한다.
func (h *handler) removeTodoTag(c fiber.Ctx) error {
	id, err := paramInt(c, "id")
	if err != nil {
		return app.ErrBadRequest
	}

	tagID, err := paramInt(c, "tagId")
	if err != nil {
		return app.ErrBadRequest
	}

	if removeErr := h.svc.RemoveTodoTag(c.Context(), id, tagID); removeErr != nil {
		return fmt.Errorf("할 일-태그 연결 해제 실패: %w", removeErr)
	}

	return c.SendStatus(fiber.StatusNoContent) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// ──────────────────────────────────────────────────────────────────────────────
// Tag 핸들러 메서드
// ──────────────────────────────────────────────────────────────────────────────

// createTag는 새로운 태그를 생성하는 핸들러다.
// POST /tags
func (h *handler) createTag(c fiber.Ctx) error {
	req := new(createTagReq)
	if err := c.Bind().Body(req); err != nil {
		return fmt.Errorf("요청 바디 파싱 실패: %w", err)
	}

	result, err := h.svc.CreateTag(c.Context(), req.Name)
	if err != nil {
		return fmt.Errorf("태그 생성 실패: %w", err)
	}

	return c.Status(fiber.StatusCreated).JSON(result) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// listTags는 전체 태그 목록을 조회하는 핸들러다.
// GET /tags
//
// 태그는 수가 적으므로 페이지네이션 없이 전체 목록을 반환한다.
func (h *handler) listTags(c fiber.Ctx) error {
	result, err := h.svc.ListTags(c.Context())
	if err != nil {
		return fmt.Errorf("태그 목록 조회 실패: %w", err)
	}

	return c.JSON(result) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// updateTag는 태그 이름을 수정하는 핸들러다.
// PATCH /tags/:id
func (h *handler) updateTag(c fiber.Ctx) error {
	id, err := paramInt(c, "id")
	if err != nil {
		return app.ErrBadRequest
	}

	req := new(updateTagReq)
	if parseErr := c.Bind().Body(req); parseErr != nil {
		return fmt.Errorf("요청 바디 파싱 실패: %w", parseErr)
	}

	result, err := h.svc.UpdateTag(c.Context(), id, req.Name)
	if err != nil {
		return fmt.Errorf("태그 수정 실패: %w", err)
	}

	return c.JSON(result) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}

// deleteTag는 태그를 삭제하는 핸들러다.
// DELETE /tags/:id
//
// 태그 삭제 시 todo_tags 연결 레코드는 CASCADE로 자동 삭제된다.
func (h *handler) deleteTag(c fiber.Ctx) error {
	id, err := paramInt(c, "id")
	if err != nil {
		return app.ErrBadRequest
	}

	if deleteErr := h.svc.DeleteTag(c.Context(), id); deleteErr != nil {
		return fmt.Errorf("태그 삭제 실패: %w", deleteErr)
	}

	return c.SendStatus(fiber.StatusNoContent) //nolint:wrapcheck // Fiber 응답 에러는 래핑 불필요
}
