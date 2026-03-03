package svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"rest-api/internal/app"
	"rest-api/internal/domain/todo/subdomain/tag/model"
	"rest-api/internal/domain/todo/subdomain/tag/repo"
)

// isoLayout는 SQLite에서 사용하는 ISO 8601 시간 형식이다.
// Go에서 시간 포맷은 "2006-01-02T15:04:05Z"라는 특수한 참조 시간(reference time)을 사용한다.
// 이 날짜(2006년 1월 2일 15:04:05)는 Go 개발팀이 정한 고정 참조값으로,
// 다른 언어의 "YYYY-MM-DDTHH:mm:ssZ" 패턴과 같은 역할이다.
const isoLayout = "2006-01-02T15:04:05Z"

// Tag는 태그 비즈니스 로직을 정의하는 인터페이스다.
// 3-tuple 패턴: 이 인터페이스 + 아래 비공개 구현체 + New 생성자로 구성된다.
// NestJS의 @Injectable() 서비스 인터페이스와 같다.
//
// 각 메서드의 역할:
//   - Create: 새 태그를 생성한다. 중복 이름은 409 에러를 반환한다.
//   - Get: ID로 태그 하나를 조회한다.
//   - List: 전체 태그 목록을 반환한다 (페이지네이션 없음).
//   - Update: 태그 이름을 수정한다. 중복 이름은 409 에러를 반환한다.
//   - Delete: 태그를 삭제한다.
//   - AddTodoTag: 할 일에 태그를 연결한다 (다대다 관계).
//   - RemoveTodoTag: 할 일에서 태그 연결을 해제한다.
//   - ListByTodoID: 특정 할 일에 연결된 태그 목록을 반환한다.
//   - ListByTodoIDs: 여러 할 일에 연결된 태그를 한 번에 조회한다 (N+1 방지).
type Tag interface {
	Create(ctx context.Context, name string) (model.Tag, error)
	Get(ctx context.Context, id int64) (model.Tag, error)
	List(ctx context.Context) ([]model.Tag, error)
	Update(ctx context.Context, id int64, name string) (model.Tag, error)
	Delete(ctx context.Context, id int64) error
	AddTodoTag(ctx context.Context, todoID, tagID int64) error
	RemoveTodoTag(ctx context.Context, todoID, tagID int64) error
	ListByTodoID(ctx context.Context, todoID int64) ([]model.Tag, error)
	ListByTodoIDs(ctx context.Context, todoIDs []int64) (map[int64][]model.Tag, error)
}

// tag는 Tag 인터페이스의 비공개 구현체다.
// Go에서 소문자로 시작하는 타입은 패키지 외부에서 접근할 수 없다(unexported).
// 외부에서는 반드시 Tag 인터페이스를 통해서만 사용한다.
//
// q는 SQLC가 생성한 쿼리 메서드를 제공하고,
// db는 SQLC가 생성할 수 없는 배치 쿼리(json_each 등)에 직접 사용한다.
type tag struct {
	q  *repo.Queries
	db *sql.DB
}

// New는 Tag 서비스를 생성한다.
// *sql.DB를 받아서 내부적으로 SQLC의 repo.Queries를 생성한다.
// db도 별도 보관하여 SQLC가 지원하지 않는 배치 쿼리에 활용한다.
// 반환 타입이 구현체(tag)가 아닌 인터페이스(Tag)인 점에 주의.
// NestJS에서 @Injectable() 데코레이터가 붙은 클래스의 생성자와 같다.
//
// *sql.DB는 repo.DBTX 인터페이스를 구현(implement)하므로 repo.New에 바로 전달 가능하다.
func New(db *sql.DB) Tag {
	return &tag{q: repo.New(db), db: db}
}

// Create는 새로운 태그를 생성한다.
// DB의 UNIQUE 제약 조건으로 인해 이미 존재하는 태그 이름을 입력하면 에러가 발생한다.
// SQLite의 UNIQUE constraint 에러를 감지하여 409 Conflict 응답으로 변환한다.
//
// NestJS에서 TypeORM의 QueryFailedError를 catch하여 ConflictException으로 변환하는 패턴과 같다.
func (t *tag) Create(ctx context.Context, name string) (model.Tag, error) {
	row, err := t.q.CreateTag(ctx, name)
	if err != nil {
		// SQLite UNIQUE constraint 위반 감지.
		// modernc.org/sqlite 드라이버는 타입이 있는 에러(*sqlite.Error)를 반환한다.
		// errors.As로 에러 체인에서 *sqlite.Error를 추출한 뒤,
		// Code()로 확장 에러 코드(extended error code)를 확인한다.
		//
		// sqlite3.SQLITE_CONSTRAINT_UNIQUE(2067)는
		// UNIQUE 제약 조건 위반 시 반환되는 확장 에러 코드다.
		// 문자열 비교(strings.Contains)보다 안전하다 — 드라이버 업데이트로
		// 에러 메시지가 바뀌어도 코드 값은 변하지 않기 때문이다.
		//
		// errors.As는 Go 1.13에서 도입된 에러 래핑 체인 탐색 함수다.
		// errors.Is가 값 비교라면, errors.As는 타입 비교다.
		// NestJS에서 catch(e) { if (e instanceof QueryFailedError) }와 유사하다.
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return model.Tag{}, app.NewAppError(
				fiber.StatusConflict, "TAG_DUPLICATE", "이미 존재하는 태그입니다",
			)
		}

		return model.Tag{}, fmt.Errorf("태그 생성 실패: %w", err)
	}

	return repoToModel(row)
}

// Get은 ID로 태그를 조회한다.
// sql.ErrNoRows 에러를 app.ErrNotFound로 변환한다.
// NestJS에서 findOneOrFail + NotFoundException 패턴과 같다.
func (t *tag) Get(ctx context.Context, id int64) (model.Tag, error) {
	row, err := t.q.GetTag(ctx, id)
	if err != nil {
		// sql.ErrNoRows: DB에서 결과가 없을 때 반환되는 표준 에러다.
		// 이를 비즈니스 에러(app.ErrNotFound)로 변환하여 HTTP 404 응답으로 연결한다.
		if errors.Is(err, sql.ErrNoRows) {
			return model.Tag{}, app.ErrNotFound
		}

		return model.Tag{}, fmt.Errorf("태그 조회 실패: %w", err)
	}

	return repoToModel(row)
}

// List는 전체 태그 목록을 반환한다.
// 태그는 개수가 많지 않으므로 페이지네이션 없이 전체를 반환한다.
// NestJS에서 this.repository.find()와 같다.
func (t *tag) List(ctx context.Context) ([]model.Tag, error) {
	rows, err := t.q.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("태그 목록 조회 실패: %w", err)
	}

	tags, err := repoSliceToModel(rows)
	if err != nil {
		return nil, err
	}

	return tags, nil
}

// Update는 태그 이름을 수정한다.
// Create와 마찬가지로 UNIQUE constraint 위반을 409로 변환한다.
// sql.ErrNoRows는 존재하지 않는 태그 수정 시도이므로 404로 변환한다.
func (t *tag) Update(ctx context.Context, id int64, name string) (model.Tag, error) {
	row, err := t.q.UpdateTag(ctx, repo.UpdateTagParams{
		ID:   id,
		Name: name,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Tag{}, app.ErrNotFound
		}

		// UNIQUE constraint 위반: 다른 태그와 이름이 중복됨.
		// Create 메서드와 동일한 패턴으로 타입 기반 에러 감지를 사용한다.
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return model.Tag{}, app.NewAppError(
				fiber.StatusConflict, "TAG_DUPLICATE", "이미 존재하는 태그입니다",
			)
		}

		return model.Tag{}, fmt.Errorf("태그 수정 실패: %w", err)
	}

	return repoToModel(row)
}

// Delete는 태그를 삭제한다.
// SQLC의 DeleteTag는 :exec 태그로 생성되어 영향받은 행 수를 반환하지 않는다.
// 따라서 존재하지 않는 ID를 삭제해도 에러가 발생하지 않는다 (멱등성).
func (t *tag) Delete(ctx context.Context, id int64) error {
	if err := t.q.DeleteTag(ctx, id); err != nil {
		return fmt.Errorf("태그 삭제 실패: %w", err)
	}

	return nil
}

// AddTodoTag는 할 일에 태그를 연결한다 (다대다 관계의 중간 테이블에 행 추가).
// SQL에서 INSERT OR IGNORE를 사용하므로, 이미 연결되어 있으면 무시된다 (멱등).
// NestJS에서 ManyToMany 관계의 relation을 추가하는 것과 같다.
func (t *tag) AddTodoTag(ctx context.Context, todoID, tagID int64) error {
	if err := t.q.AddTodoTag(ctx, repo.AddTodoTagParams{
		TodoID: todoID,
		TagID:  tagID,
	}); err != nil {
		return fmt.Errorf("할 일-태그 연결 실패: %w", err)
	}

	return nil
}

// RemoveTodoTag는 할 일에서 태그 연결을 해제한다 (다대다 관계의 중간 테이블에서 행 삭제).
// 존재하지 않는 연결을 삭제해도 에러가 발생하지 않는다 (멱등).
func (t *tag) RemoveTodoTag(ctx context.Context, todoID, tagID int64) error {
	if err := t.q.RemoveTodoTag(ctx, repo.RemoveTodoTagParams{
		TodoID: todoID,
		TagID:  tagID,
	}); err != nil {
		return fmt.Errorf("할 일-태그 연결 해제 실패: %w", err)
	}

	return nil
}

// ListByTodoID는 특정 할 일에 연결된 태그 목록을 반환한다.
// SQL에서 todo_tags 중간 테이블을 JOIN하여 태그 정보를 가져온다.
// NestJS에서 ManyToMany relation을 eager/lazy 로딩하는 것과 같다.
func (t *tag) ListByTodoID(ctx context.Context, todoID int64) ([]model.Tag, error) {
	rows, err := t.q.ListTagsByTodoID(ctx, todoID)
	if err != nil {
		return nil, fmt.Errorf("할 일의 태그 목록 조회 실패: %w", err)
	}

	tags, err := repoSliceToModel(rows)
	if err != nil {
		return nil, err
	}

	return tags, nil
}

// ListByTodoIDs는 여러 할 일에 연결된 태그를 한 번의 쿼리로 조회한다.
// 반환값은 map[todoID][]model.Tag 형태로, 각 할 일 ID에 대응하는 태그 목록이다.
//
// 이 메서드는 N+1 쿼리 문제를 해결한다:
//   - Before: todos 10개 → ListByTodoID 10번 호출 (N+1)
//   - After:  todos 10개 → ListByTodoIDs 1번 호출 (배치)
//
// NestJS에서 DataLoader 패턴으로 N+1을 해결하는 것과 같은 역할이다.
// DataLoader가 개별 load(id) 호출을 모아서 loadMany([ids])로 배치하듯이,
// 이 메서드도 여러 todoID를 모아서 한 번의 SQL 쿼리로 처리한다.
// listTagsByTodoIDsQuery는 여러 할 일 ID에 연결된 태그를 한 번에 조회하는 SQL이다.
// SQLC가 생성할 수 없는 쿼리이므로 svc 레이어에서 직접 작성한다.
// (아키텍처 규칙: repo/에는 SQLC 자동 생성 파일만 허용)
//
// json_each()는 JSON 배열을 테이블처럼 펼치는 SQLite 테이블 값 함수다.
// 예: json_each('[1,2,3]') → 행 3개 (value=1, value=2, value=3)
// 이를 IN 절의 서브쿼리로 사용하면 단일 파라미터로 여러 ID를 전달할 수 있다.
// SQLC의 SQLite 엔진이 sqlc.slice()를 지원하지 않아서 이 방식을 사용한다.
const listTagsByTodoIDsQuery = `
SELECT tt.todo_id, t.id, t.name, t.created_at
FROM tags t
INNER JOIN todo_tags tt ON t.id = tt.tag_id
WHERE tt.todo_id IN (SELECT value FROM json_each(?1))
ORDER BY tt.todo_id, t.name ASC`

func (t *tag) ListByTodoIDs(ctx context.Context, todoIDs []int64) (map[int64][]model.Tag, error) {
	// 빈 슬라이스면 빈 map을 반환한다. 불필요한 DB 호출을 방지한다.
	if len(todoIDs) == 0 {
		return make(map[int64][]model.Tag), nil
	}

	// Go 슬라이스를 JSON 배열 문자열로 변환한다.
	// json.Marshal은 []int64{1, 2, 3}을 "[1,2,3]" 바이트 슬라이스로 변환한다.
	// 이 문자열이 SQLite json_each()의 입력이 된다.
	//
	// []int64는 JSON 기본 타입이므로 직렬화가 실패할 수 없다.
	// json.Marshal은 채널(chan), 함수(func) 등 비직렬화 타입에서만 에러를 반환한다.
	idsJSON, _ := json.Marshal(todoIDs) //nolint:errcheck // []int64는 직렬화 실패 불가능

	rows, err := t.db.QueryContext(ctx, listTagsByTodoIDsQuery, string(idsJSON))
	if err != nil {
		return nil, fmt.Errorf("태그 배치 조회 실패: %w", err)
	}
	// defer rows.Close()는 함수가 끝날 때 반드시 커서를 닫도록 보장한다.
	// DB 커넥션 누수를 방지하는 Go의 필수 패턴이다.
	defer rows.Close()

	// 결과를 todoID 기준으로 그룹핑한다.
	// map의 키는 todoID, 값은 해당 할 일에 연결된 태그 슬라이스다.
	result := make(map[int64][]model.Tag, len(todoIDs))

	for rows.Next() {
		var (
			todoID    int64
			tagID     int64
			name      string
			createdAt string
		)

		// Scan의 인자 순서는 SELECT 절의 컬럼 순서(todo_id, id, name, created_at)와 일치해야 한다.
		if scanErr := rows.Scan(&todoID, &tagID, &name, &createdAt); scanErr != nil {
			return nil, fmt.Errorf("태그 배치 조회 행 스캔 실패: %w", scanErr)
		}

		parsedTime, parseErr := time.Parse(isoLayout, createdAt)
		if parseErr != nil {
			return nil, fmt.Errorf("created_at 파싱 실패: %w", parseErr)
		}

		result[todoID] = append(result[todoID], model.Tag{
			ID:        tagID,
			Name:      name,
			CreatedAt: parsedTime,
		})
	}

	// rows.Err()는 순회 중 발생한 에러를 반환한다.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("태그 배치 조회 순회 에러: %w", err)
	}

	return result, nil
}

// repoToModel은 SQLC가 생성한 repo.Tag를 도메인 모델 model.Tag로 변환한다.
// 주요 변환: CreatedAt string → time.Time (ISO 8601 파싱)
//
// SQLC는 SQLite의 TEXT 컬럼을 string으로 생성하므로
// 도메인 모델의 time.Time과 맞추려면 파싱이 필요하다.
func repoToModel(repoTag repo.Tag) (model.Tag, error) {
	// time.Parse는 참조 시간 포맷을 기준으로 문자열을 파싱한다.
	// NestJS에서 dayjs("2024-01-01T00:00:00Z").toDate()와 유사하다.
	createdAt, err := time.Parse(isoLayout, repoTag.CreatedAt)
	if err != nil {
		return model.Tag{}, fmt.Errorf("created_at 파싱 실패: %w", err)
	}

	return model.Tag{
		ID:        repoTag.ID,
		Name:      repoTag.Name,
		CreatedAt: createdAt,
	}, nil
}

// repoSliceToModel은 repo.Tag 슬라이스를 model.Tag 슬라이스로 변환한다.
// Go에서는 배열/슬라이스의 map 함수가 내장되어 있지 않아서
// for 루프로 하나씩 변환해야 한다.
// TypeScript에서는 rows.map(r => toModel(r))로 간단히 할 수 있지만,
// Go에서는 이렇게 명시적으로 반복해야 한다.
func repoSliceToModel(rows []repo.Tag) ([]model.Tag, error) {
	// make로 정확한 크기의 슬라이스를 미리 할당한다.
	// 이렇게 하면 append 시 내부 배열 재할당이 발생하지 않아 성능이 좋다.
	tags := make([]model.Tag, 0, len(rows))

	for _, r := range rows {
		m, err := repoToModel(r)
		if err != nil {
			return nil, err
		}
		tags = append(tags, m)
	}

	return tags, nil
}
