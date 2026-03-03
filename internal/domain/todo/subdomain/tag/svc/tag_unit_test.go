//go:build unit

// 이 파일은 tag 서비스 레이어의 유닛 테스트다.
// 같은 패키지(package svc)에 속하므로 비공개 타입(tag, repoToModel 등)에 직접 접근 가능하다.
// 이를 "화이트박스 테스트"라 한다 — NestJS에서 private 메서드를 직접 테스트하는 것과 같다.
//
// go-sqlmock를 사용하여 실제 DB 없이 SQL 쿼리 동작을 시뮬레이션한다.
// NestJS에서 jest.fn()으로 TypeORM Repository를 모킹하는 것과 같은 역할이다.
// go-sqlmock는 *sql.DB를 대체하는 mock DB를 생성하여,
// 기대하는 쿼리와 반환할 행을 미리 설정할 수 있게 해준다.
package svc

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"rest-api/internal/app"
	"rest-api/internal/domain/todo/subdomain/tag/repo"
)

// ──────────────────────────────────────────────────────────────────────────────
// 테스트 헬퍼
// ──────────────────────────────────────────────────────────────────────────────

// tagColumns는 태그 테이블의 컬럼 이름 목록이다.
// SELECT, INSERT RETURNING, UPDATE RETURNING 등에서 반환되는 컬럼이다.
// go-sqlmock에서 NewRows를 생성할 때 사용한다.
var tagColumns = []string{"id", "name", "created_at"}

// validTimestamp는 테스트에서 사용할 유효한 ISO 8601 시간 문자열이다.
// isoLayout("2006-01-02T15:04:05Z") 형식과 일치한다.
const validTimestamp = "2024-01-15T10:30:00Z"

// invalidTimestamp는 파싱에 실패하도록 의도적으로 잘못된 시간 문자열이다.
// time.Parse가 에러를 반환하는 경로를 테스트하기 위해 사용한다.
const invalidTimestamp = "not-a-valid-time"

// setupMock은 go-sqlmock DB와 mock 객체를 생성하는 헬퍼 함수다.
// 반환값:
//   - *sql.DB: mock이 적용된 가짜 DB 커넥션
//   - sqlmock.Sqlmock: 기대 쿼리를 설정하는 mock 컨트롤러
//   - 정리(cleanup) 함수: 테스트 종료 시 DB를 닫고 기대가 모두 충족됐는지 확인
//
// NestJS에서 beforeEach에서 mock을 설정하고 afterEach에서 정리하는 패턴과 같다.
func setupMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock, func()) {
	t.Helper() // 이 함수를 헬퍼로 표시하면, 테스트 실패 시 이 함수가 아닌 호출한 곳이 에러 위치로 표시된다.

	// sqlmock.New()는 mock이 적용된 *sql.DB와 mock 컨트롤러를 반환한다.
	// 실제 DB 드라이버 대신 mock 드라이버가 등록되어,
	// ExpectQuery/ExpectExec로 설정한 동작대로 응답한다.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	cleanup := func() {
		db.Close()
		// mock.ExpectationsWereMet()은 설정한 모든 기대가 실제로 호출됐는지 확인한다.
		// 예: ExpectQuery를 설정했는데 쿼리가 실행되지 않으면 에러를 반환한다.
		// NestJS에서 expect(mock).toHaveBeenCalled()와 같은 역할이다.
		require.NoError(t, mock.ExpectationsWereMet())
	}

	return db, mock, cleanup
}

// ──────────────────────────────────────────────────────────────────────────────
// Create 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestCreateError는 CreateTag 쿼리가 일반 에러를 반환할 때
// "태그 생성 실패" 메시지로 래핑되는지 검증한다.
func TestCreateError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// ExpectQuery는 정규식 패턴에 매칭되는 쿼리를 기대한다.
	// WillReturnError는 해당 쿼리가 실행될 때 지정한 에러를 반환하도록 설정한다.
	// NestJS에서 jest.fn().mockRejectedValue(new Error("db error"))와 같다.
	mock.ExpectQuery("INSERT INTO tags").
		WithArgs("test-tag").
		WillReturnError(errors.New("db error"))

	_, err := svc.Create(context.Background(), "test-tag")
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 생성 실패")
}

// TestCreateSuccess는 CreateTag 쿼리가 유효한 행을 반환할 때
// repoToModel 변환까지 성공하는 경로를 검증한다.
func TestCreateSuccess(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// WillReturnRows는 쿼리 결과로 반환할 행 데이터를 설정한다.
	// sqlmock.NewRows로 컬럼을 정의하고, AddRow로 데이터를 추가한다.
	// NestJS에서 jest.fn().mockResolvedValue({ id: 1, name: "tag", ... })와 같다.
	mock.ExpectQuery("INSERT INTO tags").
		WithArgs("test-tag").
		WillReturnRows(
			sqlmock.NewRows(tagColumns).AddRow(1, "test-tag", validTimestamp),
		)

	result, err := svc.Create(context.Background(), "test-tag")
	require.NoError(t, err)
	require.Equal(t, int64(1), result.ID)
	require.Equal(t, "test-tag", result.Name)

	// time.Parse로 기대 시간을 파싱하여 비교한다.
	// Go에서 time.Time은 == 연산자로 비교 가능하지만,
	// 타임존 정보까지 일치해야 하므로 require.Equal을 사용한다.
	expectedTime, _ := time.Parse(isoLayout, validTimestamp)
	require.Equal(t, expectedTime, result.CreatedAt)
}

// TestCreateInvalidTimestamp는 CreateTag가 반환한 행의 created_at이
// 잘못된 형식일 때 repoToModel에서 파싱 에러가 발생하는지 검증한다.
func TestCreateInvalidTimestamp(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("INSERT INTO tags").
		WithArgs("test-tag").
		WillReturnRows(
			sqlmock.NewRows(tagColumns).AddRow(1, "test-tag", invalidTimestamp),
		)

	_, err := svc.Create(context.Background(), "test-tag")
	require.Error(t, err)
	require.Contains(t, err.Error(), "created_at 파싱 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// Get 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestGetNotFound는 GetTag 쿼리가 sql.ErrNoRows를 반환할 때
// app.ErrNotFound로 변환되는지 검증한다.
// sql.ErrNoRows는 QueryRow 결과가 없을 때 반환되는 Go 표준 에러다.
// NestJS에서 findOneOrFail이 EntityNotFoundError를 던지는 것과 같다.
func TestGetNotFound(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM tags WHERE id").
		WithArgs(int64(999)).
		WillReturnError(sql.ErrNoRows)

	_, err := svc.Get(context.Background(), 999)
	require.Error(t, err)

	// errors.Is로 에러 체인에서 app.ErrNotFound를 찾는다.
	// fmt.Errorf("...: %w", err)로 래핑된 에러도 원본을 찾아낼 수 있다.
	// NestJS에서 expect(err).toBeInstanceOf(NotFoundException)와 같다.
	require.ErrorIs(t, err, app.ErrNotFound)
}

// TestGetError는 GetTag 쿼리가 sql.ErrNoRows가 아닌 다른 에러를 반환할 때
// "태그 조회 실패" 메시지로 래핑되는지 검증한다.
func TestGetError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM tags WHERE id").
		WithArgs(int64(1)).
		WillReturnError(errors.New("connection lost"))

	_, err := svc.Get(context.Background(), 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 조회 실패")
}

// TestGetSuccess는 GetTag 쿼리가 유효한 행을 반환할 때
// model.Tag로 올바르게 변환되는지 검증한다.
// 이 테스트는 Get 메서드의 성공 경로와 repoToModel 함수를 함께 커버한다.
func TestGetSuccess(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM tags WHERE id").
		WithArgs(int64(1)).
		WillReturnRows(
			sqlmock.NewRows(tagColumns).AddRow(1, "work", validTimestamp),
		)

	result, err := svc.Get(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.ID)
	require.Equal(t, "work", result.Name)

	expectedTime, _ := time.Parse(isoLayout, validTimestamp)
	require.Equal(t, expectedTime, result.CreatedAt)
}

// ──────────────────────────────────────────────────────────────────────────────
// List 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestListError는 ListTags 쿼리가 에러를 반환할 때
// "태그 목록 조회 실패" 메시지로 래핑되는지 검증한다.
func TestListError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM tags ORDER BY name").
		WillReturnError(errors.New("db error"))

	_, err := svc.List(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 목록 조회 실패")
}

// TestListInvalidTimestamp는 ListTags가 반환한 행 중 하나에
// 잘못된 created_at이 있을 때 repoSliceToModel에서 에러가 발생하는지 검증한다.
func TestListInvalidTimestamp(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// ListTags는 :many 쿼리이므로 QueryContext를 사용한다.
	// 여러 행을 반환할 수 있으며, 두 번째 행에 잘못된 시간을 넣는다.
	mock.ExpectQuery("SELECT .+ FROM tags ORDER BY name").
		WillReturnRows(
			sqlmock.NewRows(tagColumns).
				AddRow(1, "valid-tag", validTimestamp).
				AddRow(2, "bad-tag", invalidTimestamp),
		)

	_, err := svc.List(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "created_at 파싱 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// Update 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestUpdateNotFound는 UpdateTag 쿼리가 sql.ErrNoRows를 반환할 때
// app.ErrNotFound로 변환되는지 검증한다.
// 존재하지 않는 ID로 수정을 시도하면 RETURNING 절에서 행이 없으므로
// sql.ErrNoRows가 발생한다.
func TestUpdateNotFound(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("UPDATE tags SET").
		WithArgs("new-name", int64(999)).
		WillReturnError(sql.ErrNoRows)

	_, err := svc.Update(context.Background(), 999, "new-name")
	require.Error(t, err)
	require.ErrorIs(t, err, app.ErrNotFound)
}

// TestUpdateGenericError는 UpdateTag 쿼리가 sql.ErrNoRows도 아니고
// SQLite UNIQUE 에러도 아닌 일반 에러를 반환할 때
// "태그 수정 실패" 메시지로 래핑되는지 검증한다.
func TestUpdateGenericError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("UPDATE tags SET").
		WithArgs("new-name", int64(1)).
		WillReturnError(errors.New("disk full"))

	_, err := svc.Update(context.Background(), 1, "new-name")
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 수정 실패")
}

// TestUpdateInvalidTimestamp는 UpdateTag가 반환한 행의 created_at이
// 잘못된 형식일 때 repoToModel에서 파싱 에러가 발생하는지 검증한다.
func TestUpdateInvalidTimestamp(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("UPDATE tags SET").
		WithArgs("new-name", int64(1)).
		WillReturnRows(
			sqlmock.NewRows(tagColumns).AddRow(1, "new-name", invalidTimestamp),
		)

	_, err := svc.Update(context.Background(), 1, "new-name")
	require.Error(t, err)
	require.Contains(t, err.Error(), "created_at 파싱 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// Delete 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestDeleteError는 DeleteTag 쿼리가 에러를 반환할 때
// "태그 삭제 실패" 메시지로 래핑되는지 검증한다.
// DeleteTag는 :exec 쿼리이므로 ExpectExec을 사용한다.
func TestDeleteError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// :exec 쿼리는 ExecContext를 사용하므로 ExpectExec으로 기대를 설정한다.
	// ExpectQuery는 QueryRowContext/QueryContext용이고,
	// ExpectExec은 ExecContext용이다.
	mock.ExpectExec("DELETE FROM tags WHERE id").
		WithArgs(int64(1)).
		WillReturnError(errors.New("lock timeout"))

	err := svc.Delete(context.Background(), 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 삭제 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// AddTodoTag 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestAddTodoTagError는 AddTodoTag 쿼리가 에러를 반환할 때
// "할 일-태그 연결 실패" 메시지로 래핑되는지 검증한다.
func TestAddTodoTagError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectExec("INSERT OR IGNORE INTO todo_tags").
		WithArgs(int64(1), int64(2)).
		WillReturnError(errors.New("fk violation"))

	err := svc.AddTodoTag(context.Background(), 1, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일-태그 연결 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// RemoveTodoTag 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestRemoveTodoTagError는 RemoveTodoTag 쿼리가 에러를 반환할 때
// "할 일-태그 연결 해제 실패" 메시지로 래핑되는지 검증한다.
func TestRemoveTodoTagError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectExec("DELETE FROM todo_tags").
		WithArgs(int64(1), int64(2)).
		WillReturnError(errors.New("db error"))

	err := svc.RemoveTodoTag(context.Background(), 1, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일-태그 연결 해제 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// ListByTodoID 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestListByTodoIDError는 ListTagsByTodoID 쿼리가 에러를 반환할 때
// "할 일의 태그 목록 조회 실패" 메시지로 래핑되는지 검증한다.
func TestListByTodoIDError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM tags .+ INNER JOIN todo_tags").
		WithArgs(int64(1)).
		WillReturnError(errors.New("db error"))

	_, err := svc.ListByTodoID(context.Background(), 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "할 일의 태그 목록 조회 실패")
}

// TestListByTodoIDInvalidTimestamp는 ListTagsByTodoID가 반환한 행에
// 잘못된 created_at이 있을 때 repoSliceToModel에서 에러가 발생하는지 검증한다.
func TestListByTodoIDInvalidTimestamp(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM tags .+ INNER JOIN todo_tags").
		WithArgs(int64(1)).
		WillReturnRows(
			sqlmock.NewRows(tagColumns).
				AddRow(1, "tag", invalidTimestamp),
		)

	_, err := svc.ListByTodoID(context.Background(), 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "created_at 파싱 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// ListByTodoIDs 테스트
// ──────────────────────────────────────────────────────────────────────────────
//
// ListByTodoIDs는 SQLC 생성 쿼리가 아닌 직접 작성한 SQL을 사용한다.
// t.db.QueryContext를 직접 호출하므로 mock 기대도 db 레벨에서 설정한다.
// (다른 메서드들은 t.q.*를 호출하지만, ListByTodoIDs는 t.db를 직접 사용)

// TestListByTodoIDsEmpty는 빈 todoIDs 슬라이스를 전달했을 때
// DB 호출 없이 빈 map을 반환하는지 검증한다.
// 이 최적화는 불필요한 DB 왕복을 방지한다.
func TestListByTodoIDsEmpty(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// 빈 슬라이스를 전달하면 쿼리가 실행되지 않아야 한다.
	// mock에 ExpectQuery를 설정하지 않았으므로,
	// 만약 쿼리가 실행되면 ExpectationsWereMet에서 실패한다.
	_ = mock // mock에 기대를 설정하지 않음 — 쿼리가 실행되면 안 됨

	result, err := svc.ListByTodoIDs(context.Background(), []int64{})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result)
}

// TestListByTodoIDsQueryError는 db.QueryContext가 에러를 반환할 때
// "태그 배치 조회 실패" 메시지로 래핑되는지 검증한다.
func TestListByTodoIDsQueryError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// ListByTodoIDs는 t.db.QueryContext를 직접 호출하므로
	// mock에서도 해당 쿼리 패턴을 기대해야 한다.
	mock.ExpectQuery("SELECT tt.todo_id").
		WillReturnError(errors.New("query error"))

	_, err := svc.ListByTodoIDs(context.Background(), []int64{1, 2})
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 배치 조회 실패")
}

// TestListByTodoIDsScanError는 rows.Scan이 에러를 반환할 때
// "태그 배치 조회 행 스캔 실패" 메시지로 래핑되는지 검증한다.
// todo_id 컬럼에 int64로 변환할 수 없는 문자열을 넣어 Scan 에러를 유발한다.
func TestListByTodoIDsScanError(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// todo_id에 문자열("not-a-number")을 넣으면 int64로 Scan할 때 에러가 발생한다.
	// 이는 DB 데이터가 손상되었거나 스키마가 변경된 경우를 시뮬레이션한다.
	mock.ExpectQuery("SELECT tt.todo_id").
		WillReturnRows(
			sqlmock.NewRows([]string{"todo_id", "id", "name", "created_at"}).
				AddRow("not-a-number", 1, "tag", validTimestamp),
		)

	_, err := svc.ListByTodoIDs(context.Background(), []int64{1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 배치 조회 행 스캔 실패")
}

// TestListByTodoIDsInvalidCreatedAt는 rows에서 created_at이
// 잘못된 형식일 때 "created_at 파싱 실패" 에러가 발생하는지 검증한다.
func TestListByTodoIDsInvalidCreatedAt(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("SELECT tt.todo_id").
		WillReturnRows(
			sqlmock.NewRows([]string{"todo_id", "id", "name", "created_at"}).
				AddRow(1, 1, "tag", invalidTimestamp),
		)

	_, err := svc.ListByTodoIDs(context.Background(), []int64{1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "created_at 파싱 실패")
}

// TestListByTodoIDsRowsErr는 행 순회(iteration) 중 에러가 발생할 때
// "태그 배치 조회 순회 에러" 메시지로 래핑되는지 검증한다.
//
// rows.Err()는 rows.Next()가 false를 반환한 후 호출하여
// 순회 중 발생한 에러(네트워크 끊김 등)를 확인하는 Go DB 패턴이다.
// go-sqlmock의 RowError로 특정 행에서 에러를 시뮬레이션할 수 있다.
func TestListByTodoIDsRowsErr(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// RowError(0, ...)는 0번째 행을 읽은 후 순회 에러를 발생시킨다.
	// 이 에러는 rows.Next()가 false를 반환한 후 rows.Err()에서 반환된다.
	mock.ExpectQuery("SELECT tt.todo_id").
		WillReturnRows(
			sqlmock.NewRows([]string{"todo_id", "id", "name", "created_at"}).
				AddRow(1, 1, "tag", validTimestamp).
				RowError(0, errors.New("iteration error")),
		)

	_, err := svc.ListByTodoIDs(context.Background(), []int64{1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "태그 배치 조회 순회 에러")
}

// ──────────────────────────────────────────────────────────────────────────────
// 성공 경로 보완 테스트
// ──────────────────────────────────────────────────────────────────────────────
//
// 아래 테스트들은 각 메서드의 성공 경로(happy path)를 검증한다.
// E2E 테스트에서도 커버되지만, 유닛 테스트로 직접 확인하면
// 커버리지가 100%에 도달하고, 각 메서드의 정상 동작을 빠르게 검증할 수 있다.

// TestListSuccess는 ListTags가 유효한 행들을 반환할 때
// repoSliceToModel까지 성공하여 model.Tag 슬라이스가 반환되는지 검증한다.
func TestListSuccess(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM tags ORDER BY name").
		WillReturnRows(
			sqlmock.NewRows(tagColumns).
				AddRow(1, "alpha", validTimestamp).
				AddRow(2, "beta", validTimestamp),
		)

	result, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, "alpha", result[0].Name)
	require.Equal(t, "beta", result[1].Name)
}

// TestUpdateSuccess는 UpdateTag가 유효한 행을 반환할 때
// model.Tag로 올바르게 변환되는지 검증한다.
func TestUpdateSuccess(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("UPDATE tags SET").
		WithArgs("updated-name", int64(1)).
		WillReturnRows(
			sqlmock.NewRows(tagColumns).AddRow(1, "updated-name", validTimestamp),
		)

	result, err := svc.Update(context.Background(), 1, "updated-name")
	require.NoError(t, err)
	require.Equal(t, int64(1), result.ID)
	require.Equal(t, "updated-name", result.Name)
}

// TestDeleteSuccess는 DeleteTag가 에러 없이 완료되는 경로를 검증한다.
// ExpectExec + WillReturnResult로 성공적인 DELETE를 시뮬레이션한다.
func TestDeleteSuccess(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// sqlmock.NewResult(lastInsertId, rowsAffected)로 ExecResult를 생성한다.
	// DELETE 쿼리는 영향받은 행 수만 의미가 있으므로 lastInsertId는 0으로 둔다.
	mock.ExpectExec("DELETE FROM tags WHERE id").
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := svc.Delete(context.Background(), 1)
	require.NoError(t, err)
}

// TestAddTodoTagSuccess는 AddTodoTag가 에러 없이 완료되는 경로를 검증한다.
func TestAddTodoTagSuccess(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectExec("INSERT OR IGNORE INTO todo_tags").
		WithArgs(int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := svc.AddTodoTag(context.Background(), 1, 2)
	require.NoError(t, err)
}

// TestRemoveTodoTagSuccess는 RemoveTodoTag가 에러 없이 완료되는 경로를 검증한다.
func TestRemoveTodoTagSuccess(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectExec("DELETE FROM todo_tags").
		WithArgs(int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := svc.RemoveTodoTag(context.Background(), 1, 2)
	require.NoError(t, err)
}

// TestListByTodoIDSuccess는 ListByTodoID가 유효한 행들을 반환할 때
// 성공적으로 model.Tag 슬라이스가 반환되는지 검증한다.
func TestListByTodoIDSuccess(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	mock.ExpectQuery("SELECT .+ FROM tags .+ INNER JOIN todo_tags").
		WithArgs(int64(1)).
		WillReturnRows(
			sqlmock.NewRows(tagColumns).
				AddRow(1, "important", validTimestamp).
				AddRow(2, "urgent", validTimestamp),
		)

	result, err := svc.ListByTodoID(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, "important", result[0].Name)
	require.Equal(t, "urgent", result[1].Name)
}

// TestListByTodoIDsSuccess는 ListByTodoIDs의 전체 성공 경로를 검증한다.
// 여러 todoID에 대해 각각 태그를 매핑하여 map[int64][]model.Tag를 반환한다.
func TestListByTodoIDsSuccess(t *testing.T) {
	db, mock, cleanup := setupMock(t)
	defer cleanup()

	svc := New(db)

	// 두 개의 todoID(1, 2)에 대해 각각 태그가 있는 결과를 반환한다.
	// todoID=1에는 "work" 태그, todoID=2에는 "personal" 태그가 연결되어 있다.
	mock.ExpectQuery("SELECT tt.todo_id").
		WillReturnRows(
			sqlmock.NewRows([]string{"todo_id", "id", "name", "created_at"}).
				AddRow(1, 10, "work", validTimestamp).
				AddRow(2, 20, "personal", validTimestamp),
		)

	result, err := svc.ListByTodoIDs(context.Background(), []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, result, 2)

	// todoID=1의 태그 확인
	require.Len(t, result[1], 1)
	require.Equal(t, "work", result[1][0].Name)

	// todoID=2의 태그 확인
	require.Len(t, result[2], 1)
	require.Equal(t, "personal", result[2][0].Name)
}

// ──────────────────────────────────────────────────────────────────────────────
// repoToModel 직접 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestRepoToModelInvalidCreatedAt는 repoToModel 함수에 잘못된 created_at을 전달할 때
// 파싱 에러가 발생하는지 직접 검증한다.
// 화이트박스 테스트이므로 비공개 함수(repoToModel)에 직접 접근 가능하다.
func TestRepoToModelInvalidCreatedAt(t *testing.T) {
	// repo.Tag는 SQLC가 생성한 구조체로, CreatedAt이 string 타입이다.
	// 도메인 모델의 time.Time으로 변환할 때 파싱이 필요하다.
	input := repo.Tag{
		ID:        1,
		Name:      "test",
		CreatedAt: invalidTimestamp,
	}

	_, err := repoToModel(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "created_at 파싱 실패")
}

// ──────────────────────────────────────────────────────────────────────────────
// repoSliceToModel 직접 테스트
// ──────────────────────────────────────────────────────────────────────────────

// TestRepoSliceToModelInvalidTimestamp는 repoSliceToModel에서
// 슬라이스 내 한 항목이라도 잘못된 created_at을 가지면
// 에러가 전파되는지 검증한다.
func TestRepoSliceToModelInvalidTimestamp(t *testing.T) {
	// 첫 번째 항목은 유효, 두 번째 항목은 잘못된 시간 → 두 번째에서 에러 발생
	input := []repo.Tag{
		{ID: 1, Name: "good", CreatedAt: validTimestamp},
		{ID: 2, Name: "bad", CreatedAt: invalidTimestamp},
	}

	_, err := repoSliceToModel(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "created_at 파싱 실패")
}
