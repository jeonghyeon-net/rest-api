//go:build e2e

// 이 파일은 Todo 도메인의 HTTP 핸들러 E2E 테스트다.
//
// 프로덕션과 동일한 DI 그래프(in-memory SQLite)에서
// Todo CRUD, Tag CRUD, Todo-Tag 연결, 페이지네이션, 에러 케이스를 검증한다.
//
// testutil.NewTestApp()에 Todo 도메인 DI 옵션을 추가로 전달하여
// 프로덕션 main.go와 동일한 의존성 구조를 재현한다.
//
// NestJS에서 E2E 테스트를 작성할 때:
//
//	const module = await Test.createTestingModule({
//	  imports: [AppModule, TodoModule],
//	}).compile();
//
// 과 같은 구조다.
package http_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/suite"
	"go.uber.org/fx"
	"go.uber.org/goleak"

	"rest-api/internal/domain/todo"
	todohttp "rest-api/internal/domain/todo/handler/http"
	"rest-api/internal/domain/todo/subdomain/core/model"
	tagmodel "rest-api/internal/domain/todo/subdomain/tag/model"
	"rest-api/internal/testutil"
)

// TestMain은 이 패키지의 모든 테스트를 감싸는 진입점이다.
// goleak.VerifyTestMain으로 goroutine 누수를 검증한다.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, testutil.GoleakOptions()...)
}

// ──────────────────────────────────────────────────────────────────────────────
// 테스트 스위트 정의
// ──────────────────────────────────────────────────────────────────────────────

// TodoE2ESuite는 Todo 도메인 E2E 테스트를 묶는 테스트 스위트다.
// testify의 suite.Suite를 임베딩하여 라이프사이클 훅과 단언 메서드를 사용한다.
//
// NestJS의 describe('Todo E2E') 블록과 같다.
type TodoE2ESuite struct {
	suite.Suite

	// app은 테스트용 Fiber 앱 인스턴스다.
	// Todo 도메인 라우트가 등록되어 있어 HTTP 요청을 시뮬레이션할 수 있다.
	app *fiber.App

	// db는 in-memory SQLite DB다.
	// 테스트 데이터 직접 조회/검증에 사용할 수 있다.
	db *sql.DB
}

// SetupSuite는 스위트의 모든 테스트가 실행되기 전에 한 번 호출된다.
// NestJS의 beforeAll()과 같은 역할이다.
//
// testutil.NewTestApp()에 Todo 도메인 DI를 추가 옵션으로 전달한다.
// main.go에서 fx.Provide + fx.Invoke로 등록하는 것과 동일한 구성이다.
func (s *TodoE2ESuite) SetupSuite() {
	s.app, s.db = testutil.NewTestApp(s.T(),
		// Todo 도메인 서비스를 DI에 등록한다.
		// main.go의 fx.Provide(todo.NewService)와 동일하다.
		fx.Provide(todo.NewService),

		// Todo HTTP 핸들러를 DI에 등록한다.
		// main.go의 fx.Provide(todohttp.New)와 동일하다.
		fx.Provide(todohttp.New),

		// 핸들러의 라우트를 huma API에 등록한다.
		// huma.API는 AppModule의 fx.Provide(newHumaAPI)에서 주입된다.
		// main.go의 fx.Invoke(func(api huma.API, h todohttp.Handler) { ... })와 동일하다.
		fx.Invoke(func(api huma.API, h todohttp.Handler) {
			h.RegisterRoutes(api)
		}),
	)
}

// ──────────────────────────────────────────────────────────────────────────────
// 헬퍼 메서드
// ──────────────────────────────────────────────────────────────────────────────

// jsonBody는 구조체를 JSON 바이트 버퍼로 변환하는 헬퍼다.
// httptest.NewRequest의 body 파라미터에 전달할 때 사용한다.
// NestJS 테스트에서 request(app).post('/todos').send({ title: '...' })의 send 부분과 같다.
func (s *TodoE2ESuite) jsonBody(v any) *bytes.Buffer {
	b, err := json.Marshal(v)
	s.Require().NoError(err)

	return bytes.NewBuffer(b)
}

// doRequest는 HTTP 요청을 보내고 응답을 반환하는 헬퍼다.
// 모든 테스트에서 반복되는 요청 생성 → app.Test() 호출 패턴을 추출했다.
//
// body가 nil이면 바디 없는 요청(GET, DELETE)을 보낸다.
// body가 있으면 Content-Type: application/json을 자동으로 설정한다.
func (s *TodoE2ESuite) doRequest(method, path string, body *bytes.Buffer) *http.Response {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}

	resp, err := s.app.Test(req)
	s.Require().NoError(err)

	return resp
}

// decodeJSON은 HTTP 응답 바디를 지정한 타입으로 디코딩하는 헬퍼다.
// Go 제네릭을 사용하여 타입 안전하게 디코딩한다.
//
// 사용 예:
//
//	todo := decodeJSON[model.TodoWithTags](s, resp)
func decodeJSON[T any](s *TodoE2ESuite, resp *http.Response) T {
	defer resp.Body.Close()

	var result T
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&result))

	return result
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo CRUD 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestTodoCRUD는 Todo의 생성 → 조회 → 수정 → 삭제 전체 흐름을 검증한다.
// 하나의 테스트에서 순차적으로 실행하여 CRUD 시나리오를 검증한다.
func (s *TodoE2ESuite) TestTodoCRUD() {
	// ─── 1. 생성 (POST /todos) ──────────────────────────────────────────
	resp := s.doRequest(http.MethodPost, "/todos", s.jsonBody(map[string]string{
		"title": "E2E 테스트 할 일",
		"body":  "테스트 본문입니다",
	}))
	s.Equal(http.StatusCreated, resp.StatusCode)

	created := decodeJSON[model.TodoWithTags](s, resp)
	s.NotZero(created.ID)
	s.Equal("E2E 테스트 할 일", created.Title)
	s.Equal("테스트 본문입니다", created.Body)
	s.False(created.Done)
	s.Empty(created.Tags) // 새 할 일에는 태그 없음

	// ─── 2. 조회 (GET /todos/:id) ──────────────────────────────────────
	resp = s.doRequest(http.MethodGet, fmt.Sprintf("/todos/%d", created.ID), nil)
	s.Equal(http.StatusOK, resp.StatusCode)

	fetched := decodeJSON[model.TodoWithTags](s, resp)
	s.Equal(created.ID, fetched.ID)
	s.Equal(created.Title, fetched.Title)

	// ─── 3. 수정 (PATCH /todos/:id) ────────────────────────────────────
	resp = s.doRequest(http.MethodPatch, fmt.Sprintf("/todos/%d", created.ID), s.jsonBody(map[string]any{
		"title": "수정된 제목",
		"body":  "수정된 본문",
		"done":  true,
	}))
	s.Equal(http.StatusOK, resp.StatusCode)

	updated := decodeJSON[model.TodoWithTags](s, resp)
	s.Equal("수정된 제목", updated.Title)
	s.Equal("수정된 본문", updated.Body)
	s.True(updated.Done)

	// ─── 4. 삭제 (DELETE /todos/:id) ────────────────────────────────────
	resp = s.doRequest(http.MethodDelete, fmt.Sprintf("/todos/%d", created.ID), nil)
	s.Equal(http.StatusNoContent, resp.StatusCode)
	resp.Body.Close()

	// 삭제 후 조회하면 404
	resp = s.doRequest(http.MethodGet, fmt.Sprintf("/todos/%d", created.ID), nil)
	s.Equal(http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

// ──────────────────────────────────────────────────────────────────────────────
// Tag CRUD 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestTagCRUD는 Tag의 생성 → 목록 조회 → 수정 → 삭제 전체 흐름을 검증한다.
func (s *TodoE2ESuite) TestTagCRUD() {
	// ─── 1. 생성 (POST /tags) ───────────────────────────────────────────
	resp := s.doRequest(http.MethodPost, "/tags", s.jsonBody(map[string]string{
		"name": "긴급",
	}))
	s.Equal(http.StatusCreated, resp.StatusCode)

	created := decodeJSON[tagmodel.Tag](s, resp)
	s.NotZero(created.ID)
	s.Equal("긴급", created.Name)

	// ─── 2. 목록 조회 (GET /tags) ──────────────────────────────────────
	// 추가 태그 생성
	resp = s.doRequest(http.MethodPost, "/tags", s.jsonBody(map[string]string{
		"name": "중요",
	}))
	s.Equal(http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	resp = s.doRequest(http.MethodGet, "/tags", nil)
	s.Equal(http.StatusOK, resp.StatusCode)

	tags := decodeJSON[[]tagmodel.Tag](s, resp)
	s.GreaterOrEqual(len(tags), 2)

	// ─── 3. 수정 (PATCH /tags/:id) ─────────────────────────────────────
	resp = s.doRequest(http.MethodPatch, fmt.Sprintf("/tags/%d", created.ID), s.jsonBody(map[string]string{
		"name": "매우 긴급",
	}))
	s.Equal(http.StatusOK, resp.StatusCode)

	updatedTag := decodeJSON[tagmodel.Tag](s, resp)
	s.Equal("매우 긴급", updatedTag.Name)

	// ─── 4. 삭제 (DELETE /tags/:id) ─────────────────────────────────────
	resp = s.doRequest(http.MethodDelete, fmt.Sprintf("/tags/%d", created.ID), nil)
	s.Equal(http.StatusNoContent, resp.StatusCode)
	resp.Body.Close()
}

// ──────────────────────────────────────────────────────────────────────────────
// Todo-Tag 연결 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestTodoTagAssociation은 Todo에 Tag를 연결/해제하고
// GET /todos/:id 응답에 태그가 포함되는지 검증한다.
func (s *TodoE2ESuite) TestTodoTagAssociation() {
	// 테스트 데이터 준비: 할 일 1개 + 태그 2개
	resp := s.doRequest(http.MethodPost, "/todos", s.jsonBody(map[string]string{
		"title": "태그 테스트 할 일",
	}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	todoResult := decodeJSON[model.TodoWithTags](s, resp)

	resp = s.doRequest(http.MethodPost, "/tags", s.jsonBody(map[string]string{
		"name": "태그A",
	}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	tagA := decodeJSON[tagmodel.Tag](s, resp)

	resp = s.doRequest(http.MethodPost, "/tags", s.jsonBody(map[string]string{
		"name": "태그B",
	}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	tagB := decodeJSON[tagmodel.Tag](s, resp)

	// ─── 1. 태그 연결 (POST /todos/:id/tags) ───────────────────────────
	resp = s.doRequest(http.MethodPost, fmt.Sprintf("/todos/%d/tags", todoResult.ID),
		s.jsonBody(map[string]int64{"tagId": tagA.ID}))
	s.Equal(http.StatusNoContent, resp.StatusCode)
	resp.Body.Close()

	resp = s.doRequest(http.MethodPost, fmt.Sprintf("/todos/%d/tags", todoResult.ID),
		s.jsonBody(map[string]int64{"tagId": tagB.ID}))
	s.Equal(http.StatusNoContent, resp.StatusCode)
	resp.Body.Close()

	// ─── 2. 연결 확인 (GET /todos/:id) ─────────────────────────────────
	resp = s.doRequest(http.MethodGet, fmt.Sprintf("/todos/%d", todoResult.ID), nil)
	s.Equal(http.StatusOK, resp.StatusCode)

	todoWithTags := decodeJSON[model.TodoWithTags](s, resp)
	s.Len(todoWithTags.Tags, 2)

	// ─── 3. 태그 연결 해제 (DELETE /todos/:id/tags/:tagId) ──────────────
	resp = s.doRequest(http.MethodDelete,
		fmt.Sprintf("/todos/%d/tags/%d", todoResult.ID, tagA.ID), nil)
	s.Equal(http.StatusNoContent, resp.StatusCode)
	resp.Body.Close()

	// 해제 후 태그가 1개만 남았는지 확인
	resp = s.doRequest(http.MethodGet, fmt.Sprintf("/todos/%d", todoResult.ID), nil)
	s.Equal(http.StatusOK, resp.StatusCode)

	todoAfterRemove := decodeJSON[model.TodoWithTags](s, resp)
	s.Len(todoAfterRemove.Tags, 1)
	s.Equal(tagB.Name, todoAfterRemove.Tags[0].Name)
}

// ──────────────────────────────────────────────────────────────────────────────
// 페이지네이션 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestPagination은 Todo 목록의 페이지네이션(page/limit)을 검증한다.
func (s *TodoE2ESuite) TestPagination() {
	// 테스트 데이터: 할 일 5개 생성
	for i := range 5 {
		resp := s.doRequest(http.MethodPost, "/todos", s.jsonBody(map[string]string{
			"title": fmt.Sprintf("페이지네이션 테스트 %d", i+1),
		}))
		s.Equal(http.StatusCreated, resp.StatusCode)
		resp.Body.Close()
	}

	// page=1, limit=2로 조회
	resp := s.doRequest(http.MethodGet, "/todos?page=1&limit=2", nil)
	s.Equal(http.StatusOK, resp.StatusCode)

	list := decodeJSON[model.TodoList](s, resp)
	s.Len(list.Data, 2)
	s.Equal(2, list.Meta.Limit)
	s.Equal(1, list.Meta.Page)
	// 이 스위트의 다른 테스트에서도 할 일을 생성하므로 total >= 5 검증
	s.GreaterOrEqual(list.Meta.Total, int64(5))
}

// ──────────────────────────────────────────────────────────────────────────────
// 태그 필터 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestTagFilter는 GET /todos?tag=xxx 태그 필터링을 검증한다.
func (s *TodoE2ESuite) TestTagFilter() {
	// 테스트 데이터: 할 일 2개 + 태그 1개
	resp := s.doRequest(http.MethodPost, "/todos", s.jsonBody(map[string]string{
		"title": "필터 대상",
	}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	targetTodo := decodeJSON[model.TodoWithTags](s, resp)

	resp = s.doRequest(http.MethodPost, "/todos", s.jsonBody(map[string]string{
		"title": "필터 제외",
	}))
	s.Equal(http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	resp = s.doRequest(http.MethodPost, "/tags", s.jsonBody(map[string]string{
		"name": "필터태그",
	}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	filterTag := decodeJSON[tagmodel.Tag](s, resp)

	// targetTodo에만 태그 연결
	resp = s.doRequest(http.MethodPost, fmt.Sprintf("/todos/%d/tags", targetTodo.ID),
		s.jsonBody(map[string]int64{"tagId": filterTag.ID}))
	s.Equal(http.StatusNoContent, resp.StatusCode)
	resp.Body.Close()

	// tag 파라미터로 필터링
	resp = s.doRequest(http.MethodGet, "/todos?tag=필터태그", nil)
	s.Equal(http.StatusOK, resp.StatusCode)

	list := decodeJSON[model.TodoList](s, resp)
	// 필터된 결과에는 필터태그가 연결된 할 일만 포함
	s.GreaterOrEqual(len(list.Data), 1)

	for _, item := range list.Data {
		found := false
		for _, t := range item.Tags {
			if t.Name == "필터태그" {
				found = true

				break
			}
		}
		s.True(found, "필터된 결과에 해당 태그가 없음: todo ID=%d", item.ID)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 에러 케이스 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestNotFoundTodo는 존재하지 않는 Todo 조회 시 404를 반환하는지 검증한다.
func (s *TodoE2ESuite) TestNotFoundTodo() {
	resp := s.doRequest(http.MethodGet, "/todos/999999", nil)
	s.Equal(http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

// TestDuplicateTag는 중복된 태그 이름으로 생성 시 409를 반환하는지 검증한다.
func (s *TodoE2ESuite) TestDuplicateTag() {
	resp := s.doRequest(http.MethodPost, "/tags", s.jsonBody(map[string]string{
		"name": "유니크태그",
	}))
	s.Equal(http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	// 같은 이름으로 다시 생성 → 409 Conflict
	resp = s.doRequest(http.MethodPost, "/tags", s.jsonBody(map[string]string{
		"name": "유니크태그",
	}))
	s.Equal(http.StatusConflict, resp.StatusCode)
	resp.Body.Close()
}

// TestValidationError는 필수 필드 누락 시 422를 반환하는지 검증한다.
func (s *TodoE2ESuite) TestValidationError() {
	// title 없이 Todo 생성 → 422 Unprocessable Entity
	resp := s.doRequest(http.MethodPost, "/todos", s.jsonBody(map[string]string{
		"body": "제목 없음",
	}))
	s.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
	resp.Body.Close()

	// name 없이 Tag 생성 → 422
	resp = s.doRequest(http.MethodPost, "/tags", s.jsonBody(map[string]string{}))
	s.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
	resp.Body.Close()
}

// ──────────────────────────────────────────────────────────────────────────────
// 테스트 러너 진입점
// ──────────────────────────────────────────────────────────────────────────────

// TestTodoE2E는 Go 테스트 러너의 진입점이다.
// testify suite.Run이 TodoE2ESuite의 모든 Test* 메서드를 찾아 실행한다.
func TestTodoE2E(t *testing.T) {
	suite.Run(t, new(TodoE2ESuite))
}
