//go:build unit

package svc

// ─────────────────────────────────────────────────────────────────────────────
// todo_unit_test.go — Todo 서비스의 단위 테스트 (화이트박스)
// ─────────────────────────────────────────────────────────────────────────────
//
// 같은 패키지(package svc)에 작성하여 비공개 함수(repoToModel, repoSliceToModel)도
// 직접 테스트할 수 있다. NestJS에서는 private 메서드 테스트가 어렵지만,
// Go에서는 같은 패키지 내 테스트 파일에서 소문자 함수에 바로 접근 가능하다.
//
// go-sqlmock을 사용하여 실제 DB 없이 SQL 쿼리 결과를 모킹한다.
// NestJS에서 Jest의 jest.mock()으로 Repository를 모킹하는 것과 비슷하지만,
// Go에서는 database/sql 드라이버 수준에서 모킹하므로 SQLC 생성 코드까지
// 통합하여 테스트할 수 있다.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"rest-api/internal/app"
	"rest-api/internal/domain/todo/subdomain/core/repo"
)

// TestMain은 이 패키지의 모든 테스트를 감싸는 진입점이다.
// goleak.VerifyTestMain(m)은 Uber의 goroutine 누수 검출기로,
// 테스트 종료 후 정리되지 않은 goroutine이 있으면 테스트를 실패시킨다.
// NestJS에서 Jest의 globalSetup/globalTeardown과 유사하다.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// ── 공통 상수 및 헬퍼 ────────────────────────────────────────────────────────

// validTime은 테스트에서 사용하는 유효한 ISO 8601 타임스탬프다.
// isoLayout("2006-01-02T15:04:05Z") 형식에 맞는 올바른 값이다.
const validTime = "2024-01-01T00:00:00Z"

// invalidTime은 파싱 실패를 유발하기 위한 잘못된 타임스탬프 문자열이다.
const invalidTime = "invalid-time"

// todoColumns는 SQLC가 생성한 Todo 구조체의 필드 순서에 맞는 컬럼명 슬라이스다.
// sqlmock.NewRows에 전달하여 모킹된 결과 행의 컬럼을 정의한다.
var todoColumns = []string{"id", "title", "body", "done", "created_at", "updated_at"}

// ── Create 테스트 ────────────────────────────────────────────────────────────

// TestCreate_QueryError는 q.CreateTodo가 DB 에러를 반환할 때
// 서비스가 "할 일 생성 실패" 메시지로 감싸서 반환하는지 검증한다.
//
// sqlmock.New()는 모킹된 *sql.DB와 기대값(expectation)을 설정하는 mock 객체를 반환한다.
// NestJS에서 jest.fn().mockRejectedValue(new Error("db error"))와 비슷하다.
func TestCreate_QueryError(t *testing.T) {
	t.Parallel()

	// sqlmock.New()는 3개의 값을 반환한다:
	// 1. db: 모킹된 *sql.DB (실제 DB 연결 없이 동작)
	// 2. mock: 기대값(expectation)을 설정하는 컨트롤러
	// 3. err: 초기화 에러
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	// defer는 함수가 끝날 때 실행되는 정리 코드를 등록한다.
	// NestJS에서 afterEach(() => db.close())와 같은 역할이다.
	defer db.Close()

	// New(db)로 서비스를 생성하면 내부적으로 repo.New(db)를 호출하여
	// SQLC의 Queries 구조체를 초기화한다. 모킹된 db가 주입되므로
	// 모든 SQL 호출은 sqlmock의 기대값에 따라 동작한다.
	svc := New(db)

	// ExpectQuery는 특정 정규식 패턴과 일치하는 SQL 쿼리가 실행될 것을 기대한다.
	// WillReturnError는 해당 쿼리가 에러를 반환하도록 설정한다.
	// NestJS에서 jest.spyOn(repository, 'save').mockRejectedValue(...)와 유사하다.
	mock.ExpectQuery("INSERT INTO todos").WillReturnError(errors.New("db error"))

	_, err = svc.Create(context.Background(), "test", "body")

	// require.Error는 err가 nil이 아닌지 확인한다. nil이면 테스트를 즉시 중단한다.
	require.Error(t, err)

	// assert.Contains는 에러 메시지에 특정 문자열이 포함되어 있는지 확인한다.
	// fmt.Errorf("할 일 생성 실패: %w", err)로 감싸졌으므로 이 문자열이 포함된다.
	assert.Contains(t, err.Error(), "할 일 생성 실패")

	// mock.ExpectationsWereMet()은 설정한 모든 기대값이 실제로 충족되었는지 검증한다.
	// 기대한 쿼리가 실행되지 않았거나, 기대하지 않은 쿼리가 실행되면 에러를 반환한다.
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestCreate_InvalidCreatedAt는 q.CreateTodo가 반환한 행의 created_at이
// 유효하지 않은 타임스탬프일 때 time.Parse 에러가 발생하는지 검증한다.
func TestCreate_InvalidCreatedAt(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// WillReturnRows는 쿼리가 성공적으로 행을 반환하도록 설정한다.
	// 여기서는 created_at에 파싱할 수 없는 문자열("invalid-time")을 넣어서
	// repoToModel에서 time.Parse 에러가 발생하도록 유도한다.
	mock.ExpectQuery("INSERT INTO todos").WillReturnRows(
		sqlmock.NewRows(todoColumns).AddRow(1, "test", "body", 0, invalidTime, validTime),
	)

	_, err = svc.Create(context.Background(), "test", "body")

	require.Error(t, err)
	// repoToModel에서 "created_at 파싱 실패: ..."로 감싸진 에러가 반환된다.
	assert.Contains(t, err.Error(), "created_at 파싱 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// ── Get 테스트 ───────────────────────────────────────────────────────────────

// TestGet_NotFound는 q.GetTodo가 sql.ErrNoRows를 반환할 때
// 서비스가 app.ErrNotFound로 변환하는지 검증한다.
// NestJS에서 findOneOrFail + NotFoundException 패턴과 같다.
func TestGet_NotFound(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// sql.ErrNoRows는 QueryRow.Scan에서 결과가 없을 때 반환되는 Go 표준 에러다.
	// 서비스에서 이를 app.ErrNotFound(HTTP 404)로 변환하는 로직을 테스트한다.
	mock.ExpectQuery("SELECT .+ FROM todos WHERE id").WillReturnError(sql.ErrNoRows)

	_, err = svc.Get(context.Background(), 1)

	// errors.Is는 에러 체인을 탐색하여 특정 에러와 일치하는지 확인한다.
	// TypeScript에서 instanceof 체크와 유사하지만, 감싸진(wrapped) 에러도 찾아낸다.
	require.ErrorIs(t, err, app.ErrNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGet_OtherError는 q.GetTodo가 sql.ErrNoRows가 아닌 다른 에러를 반환할 때
// "할 일 조회 실패" 메시지로 감싸서 반환하는지 검증한다.
func TestGet_OtherError(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM todos WHERE id").WillReturnError(errors.New("connection lost"))

	_, err = svc.Get(context.Background(), 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "할 일 조회 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGet_InvalidTimestamp는 q.GetTodo가 반환한 행의 타임스탬프가
// 유효하지 않을 때 repoToModel에서 파싱 에러가 발생하는지 검증한다.
func TestGet_InvalidTimestamp(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// created_at에 잘못된 타임스탬프를 넣어서 repoToModel 에러를 유발한다.
	mock.ExpectQuery("SELECT .+ FROM todos WHERE id").WillReturnRows(
		sqlmock.NewRows(todoColumns).AddRow(1, "test", "body", 0, invalidTime, validTime),
	)

	_, err = svc.Get(context.Background(), 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "created_at 파싱 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// ── List 테스트 ──────────────────────────────────────────────────────────────

// TestList_QueryError는 q.ListTodos가 에러를 반환할 때
// "할 일 목록 조회 실패" 메시지로 감싸서 반환하는지 검증한다.
func TestList_QueryError(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// ListTodos는 :many 태그이므로 QueryContext를 사용한다.
	// sqlmock에서는 ExpectQuery로 SELECT 쿼리를 기대한다.
	mock.ExpectQuery("SELECT .+ FROM todos ORDER BY id DESC LIMIT").
		WillReturnError(errors.New("query failed"))

	_, _, err = svc.List(context.Background(), 1, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "할 일 목록 조회 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestList_CountError는 q.ListTodos는 성공하지만 q.CountTodos가 에러를 반환할 때
// "할 일 개수 조회 실패" 메시지로 감싸서 반환하는지 검증한다.
func TestList_CountError(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// ListTodos 쿼리는 빈 결과를 정상 반환하도록 설정한다.
	// sqlmock.NewRows에 행을 추가하지 않으면 빈 결과셋이 반환된다.
	mock.ExpectQuery("SELECT .+ FROM todos ORDER BY id DESC LIMIT").
		WillReturnRows(sqlmock.NewRows(todoColumns))

	// CountTodos 쿼리는 에러를 반환하도록 설정한다.
	mock.ExpectQuery("SELECT COUNT").WillReturnError(errors.New("count failed"))

	_, _, err = svc.List(context.Background(), 1, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "할 일 개수 조회 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestList_InvalidTimestamp는 q.ListTodos가 반환한 행 중 하나의 타임스탬프가
// 유효하지 않을 때 repoSliceToModel에서 에러가 발생하는지 검증한다.
func TestList_InvalidTimestamp(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// ListTodos가 잘못된 타임스탬프를 가진 행을 반환하도록 설정한다.
	mock.ExpectQuery("SELECT .+ FROM todos ORDER BY id DESC LIMIT").
		WillReturnRows(
			sqlmock.NewRows(todoColumns).
				AddRow(1, "test", "body", 0, invalidTime, validTime),
		)

	// CountTodos는 정상 반환하도록 설정한다.
	// sqlmock.NewRows에 "count" 컬럼 1개를 추가하고 값으로 1을 넣는다.
	mock.ExpectQuery("SELECT COUNT").WillReturnRows(
		sqlmock.NewRows([]string{"count"}).AddRow(1),
	)

	_, _, err = svc.List(context.Background(), 1, 10)

	require.Error(t, err)
	// repoSliceToModel은 내부적으로 repoToModel을 호출하므로 같은 에러 메시지가 나온다.
	assert.Contains(t, err.Error(), "created_at 파싱 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// ── ListByTag 테스트 ─────────────────────────────────────────────────────────

// TestListByTag_QueryError는 q.ListTodosByTag가 에러를 반환할 때
// "태그별 할 일 목록 조회 실패" 메시지로 감싸서 반환하는지 검증한다.
func TestListByTag_QueryError(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// ListTodosByTag SQL에는 INNER JOIN todo_tags가 포함되어 있다.
	mock.ExpectQuery("SELECT .+ FROM todos .+ INNER JOIN todo_tags").
		WillReturnError(errors.New("query failed"))

	_, _, err = svc.ListByTag(context.Background(), "urgent", 1, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "태그별 할 일 목록 조회 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestListByTag_CountError는 q.ListTodosByTag는 성공하지만
// q.CountTodosByTag가 에러를 반환할 때 올바른 에러 메시지를 반환하는지 검증한다.
func TestListByTag_CountError(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM todos .+ INNER JOIN todo_tags").
		WillReturnRows(sqlmock.NewRows(todoColumns))

	mock.ExpectQuery("SELECT COUNT").WillReturnError(errors.New("count failed"))

	_, _, err = svc.ListByTag(context.Background(), "urgent", 1, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "태그별 할 일 개수 조회 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestListByTag_InvalidTimestamp는 q.ListTodosByTag가 반환한 행의 타임스탬프가
// 유효하지 않을 때 repoSliceToModel에서 에러가 발생하는지 검증한다.
func TestListByTag_InvalidTimestamp(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM todos .+ INNER JOIN todo_tags").
		WillReturnRows(
			sqlmock.NewRows(todoColumns).
				AddRow(1, "test", "body", 0, invalidTime, validTime),
		)

	mock.ExpectQuery("SELECT COUNT").WillReturnRows(
		sqlmock.NewRows([]string{"count"}).AddRow(1),
	)

	_, _, err = svc.ListByTag(context.Background(), "urgent", 1, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "created_at 파싱 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// ── Update 테스트 ────────────────────────────────────────────────────────────

// TestUpdate_NotFound는 q.UpdateTodo가 sql.ErrNoRows를 반환할 때
// 서비스가 app.ErrNotFound로 변환하는지 검증한다.
func TestUpdate_NotFound(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// UpdateTodo는 RETURNING 절이 있으므로 :one 태그다. ExpectQuery를 사용한다.
	mock.ExpectQuery("UPDATE todos SET").WillReturnError(sql.ErrNoRows)

	_, err = svc.Update(context.Background(), 999, "title", "body", false)

	require.ErrorIs(t, err, app.ErrNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdate_OtherError는 q.UpdateTodo가 sql.ErrNoRows가 아닌 다른 에러를 반환할 때
// "할 일 수정 실패" 메시지로 감싸서 반환하는지 검증한다.
func TestUpdate_OtherError(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	mock.ExpectQuery("UPDATE todos SET").WillReturnError(errors.New("constraint violation"))

	_, err = svc.Update(context.Background(), 1, "title", "body", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "할 일 수정 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdate_InvalidTimestamp는 q.UpdateTodo가 반환한 행의 타임스탬프가
// 유효하지 않을 때 repoToModel에서 파싱 에러가 발생하는지 검증한다.
func TestUpdate_InvalidTimestamp(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	mock.ExpectQuery("UPDATE todos SET").WillReturnRows(
		sqlmock.NewRows(todoColumns).AddRow(1, "test", "body", 0, invalidTime, validTime),
	)

	_, err = svc.Update(context.Background(), 1, "test", "body", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "created_at 파싱 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdate_DoneTrue는 done=true로 업데이트할 때 doneInt=1 분기가
// 올바르게 실행되는지 검증한다.
//
// Go에는 삼항 연산자(ternary)가 없으므로 if-else로 bool→int64 변환한다.
// 이 테스트는 done=true일 때 doneInt=1이 되는 분기를 커버한다.
func TestUpdate_DoneTrue(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// done=1인 행을 반환하여 done=true 업데이트가 성공하는 시나리오를 테스트한다.
	mock.ExpectQuery("UPDATE todos SET").WillReturnRows(
		sqlmock.NewRows(todoColumns).AddRow(1, "test", "body", 1, validTime, validTime),
	)

	result, err := svc.Update(context.Background(), 1, "test", "body", true)

	require.NoError(t, err)
	// 반환된 모델의 Done이 true인지 확인한다 (int64=1 → bool=true 변환 검증).
	assert.True(t, result.Done)
	require.NoError(t, mock.ExpectationsWereMet())
}

// ── Delete 테스트 ────────────────────────────────────────────────────────────

// TestDelete_Error는 q.DeleteTodo가 에러를 반환할 때
// "할 일 삭제 실패" 메시지로 감싸서 반환하는지 검증한다.
//
// DeleteTodo는 :exec 태그로 생성되어 ExecContext를 사용한다.
// 따라서 ExpectQuery가 아닌 ExpectExec을 사용해야 한다.
func TestDelete_Error(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	svc := New(db)

	// DeleteTodo는 :exec이므로 ExecContext를 호출한다.
	// sqlmock에서는 ExpectExec으로 매칭한다.
	mock.ExpectExec("DELETE FROM todos WHERE id").WillReturnError(errors.New("delete failed"))

	err = svc.Delete(context.Background(), 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "할 일 삭제 실패")
	require.NoError(t, mock.ExpectationsWereMet())
}

// ── repoToModel 직접 테스트 ──────────────────────────────────────────────────

// TestRepoToModel_InvalidCreatedAt는 repo.Todo의 CreatedAt이 잘못된 형식일 때
// "created_at 파싱 실패" 에러를 반환하는지 직접 검증한다.
//
// repoToModel은 비공개(unexported) 함수이지만 같은 패키지에서 직접 호출 가능하다.
// 이것이 화이트박스 테스트의 장점이다.
func TestRepoToModel_InvalidCreatedAt(t *testing.T) {
	t.Parallel()

	// repo.Todo를 직접 생성하여 repoToModel에 전달한다.
	// 서비스 메서드를 거치지 않고 변환 함수만 단독으로 테스트한다.
	input := repo.Todo{
		ID:        1,
		Title:     "test",
		Body:      "body",
		Done:      0,
		CreatedAt: invalidTime, // 파싱할 수 없는 값
		UpdatedAt: validTime,
	}

	_, err := repoToModel(input)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "created_at 파싱 실패")
}

// TestRepoToModel_InvalidUpdatedAt는 CreatedAt은 유효하지만 UpdatedAt이
// 잘못된 형식일 때 "updated_at 파싱 실패" 에러를 반환하는지 검증한다.
//
// 이 테스트는 repoToModel에서 두 번째 time.Parse 호출의 에러 분기를 커버한다.
// CreatedAt 파싱은 통과하고 UpdatedAt 파싱에서 실패하는 경로를 검증한다.
func TestRepoToModel_InvalidUpdatedAt(t *testing.T) {
	t.Parallel()

	input := repo.Todo{
		ID:        1,
		Title:     "test",
		Body:      "body",
		Done:      0,
		CreatedAt: validTime,   // 유효한 값 (첫 번째 파싱 통과)
		UpdatedAt: invalidTime, // 파싱할 수 없는 값 (두 번째 파싱 실패)
	}

	_, err := repoToModel(input)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "updated_at 파싱 실패")
}
