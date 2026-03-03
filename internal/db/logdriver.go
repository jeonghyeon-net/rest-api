// logdriver.go — 개발 환경 전용 SQL 쿼리 로깅 드라이버
//
// database/sql/driver 인터페이스를 구현하는 래퍼(wrapper)를 통해,
// 서비스·리포지토리 코드를 전혀 수정하지 않고도 모든 SQL 쿼리를 투명하게 로깅한다.
//
// 동작 원리:
//  1. newLoggedDB()가 sql.OpenDB()에 로깅 커넥터(logConnector)를 전달하여 DB를 연다.
//     sql.Register + init() 없이 driver.Connector 인터페이스를 직접 구현한다.
//  2. NewDB()에서 개발 환경이면 newLoggedDB()로, 프로덕션이면 기존 sql.Open()을 사용한다.
//  3. 쿼리가 실행될 때마다 zap 로거로 쿼리 문자열, 바인드 인자, 실행 시간을 출력한다.
//
// NestJS에서 TypeORM의 logging: true 옵션을 켜면 실행되는 모든 SQL이
// 콘솔에 출력되는 것과 같은 개념이다. 다만 Go에서는 ORM이 아닌 driver 레벨에서
// 래핑하여 구현한다.
//
// 래핑하는 인터페이스 목록:
//   - driver.Connector → logConnector (연결 생성 진입점, sql.OpenDB에 전달)
//   - driver.Conn      → logConn     (연결 레벨)
//   - driver.Stmt      → logStmt     (쿼리 실행 레벨, 여기서 로깅 발생)
package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// newLoggedDB는 쿼리 로깅이 활성화된 *sql.DB를 생성한다.
//
// sql.OpenDB()를 사용하여, 드라이버 등록(sql.Register) 없이
// logConnector를 직접 전달한다. 이 방식은:
//   - init() 함수 불필요 (gochecknoinits 위반 없음)
//   - 패키지 수준 전역 변수 불필요 (gochecknoglobals 위반 없음)
//   - 로거를 구조체 필드로 전달하여 DI 원칙 준수
//
// NestJS에서 new TypeOrmModule({ logging: true })로 로깅을 켜는 것과 유사하다.
// Go에서는 sql.OpenDB(connector)로 커스텀 연결 생성 로직을 주입한다.
//
// sql.OpenDB()는 sql.Open()과 동일한 *sql.DB 커넥션 풀을 반환하지만,
// 드라이버 이름 대신 driver.Connector 인터페이스를 직접 받는다.
// 드라이버 전역 레지스트리를 거치지 않으므로 더 유연하다.
func newLoggedDB(dsn string, logger *zap.Logger) (*sql.DB, error) {
	// "sqlite" 드라이버 인스턴스를 가져오기 위해 임시 DB를 연다.
	// sql.Open()은 lazy connection이므로 실제 연결을 만들지 않는다.
	// Driver()로 등록된 드라이버 인스턴스를 가져온 후 즉시 닫는다.
	tmpDB, err := sql.Open("sqlite", "")
	if err != nil {
		return nil, fmt.Errorf("sqlite 드라이버 로드 실패: %w", err)
	}

	connector := &logConnector{
		dsn:    dsn,
		origin: tmpDB.Driver(),
		logger: logger,
	}
	_ = tmpDB.Close()

	return sql.OpenDB(connector), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// logConnector — driver.Connector 인터페이스 구현
// ─────────────────────────────────────────────────────────────────────────────
//
// driver.Connector는 database/sql이 새 연결을 생성할 때 사용하는 인터페이스다.
// sql.OpenDB()에 전달되며, Connect(ctx)와 Driver() 두 메서드를 정의한다.
//
// sql.Register() + init()으로 글로벌 드라이버를 등록하는 대신,
// Connector를 sql.OpenDB()에 직접 전달하면 글로벌 상태 없이 동작한다.
// NestJS에서 useFactory로 프로바이더를 직접 생성하는 것과 비슷한 패턴이다.

// logConnector는 원본 SQLite 드라이버를 감싸는 로깅 커넥터다.
// 로거를 필드로 보유하여 logConn → logStmt까지 전달한다.
type logConnector struct {
	origin driver.Driver // 원본 드라이버 (modernc.org/sqlite)
	logger *zap.Logger   // 쿼리 로깅용 로거
	dsn    string        // 데이터 소스 이름 (SQLite 파일 경로)
}

// Connect는 원본 드라이버로 연결을 열고 로깅 래퍼(logConn)로 감싸서 반환한다.
//
// database/sql 커넥션 풀이 새 연결이 필요할 때 이 메서드를 호출한다.
// ctx를 통해 연결 생성 타임아웃이나 취소를 받을 수 있다.
// 원본 driver.Driver.Open()은 context를 받지 않으므로,
// 여기서는 ctx를 직접 활용하지 않지만 인터페이스 충족을 위해 받는다.
func (c *logConnector) Connect(_ context.Context) (driver.Conn, error) {
	conn, err := c.origin.Open(c.dsn)
	if err != nil {
		return nil, fmt.Errorf("sql logdriver 연결 생성: %w", err)
	}

	return &logConn{origin: conn, logger: c.logger}, nil
}

// Driver는 이 커넥터가 사용하는 원본 드라이버를 반환한다.
//
// driver.Connector 인터페이스의 필수 메서드다.
// database/sql이 드라이버 정보를 조회할 때 사용한다.
func (c *logConnector) Driver() driver.Driver {
	return c.origin
}

// ─────────────────────────────────────────────────────────────────────────────
// logConn — driver.Conn + driver.ConnPrepareContext + driver.ConnBeginTx 구현
// ─────────────────────────────────────────────────────────────────────────────
//
// driver.Conn은 하나의 DB 연결을 나타내는 인터페이스다.
// Prepare, Close, Begin 세 메서드를 정의한다.
//
// 추가로 ConnPrepareContext(context 지원 Prepare)와
// ConnBeginTx(context 지원 Begin)도 구현하여,
// database/sql이 context를 활용할 수 있게 한다.
//
// 중요: logConn은 ExecerContext/QueryerContext를 일부러 구현하지 않는다.
// 이 인터페이스들은 Prepare 없이 직접 쿼리를 실행하는 단축 경로(shortcut)인데,
// 구현하지 않으면 database/sql이 항상 Prepare → Exec/Query 경로를 사용한다.
// 이 덕분에 모든 쿼리가 logStmt를 거치므로, 한 곳에서 로깅할 수 있다.

// logConn은 원본 연결을 감싸는 로깅 연결이다.
type logConn struct {
	origin driver.Conn // 원본 연결
	logger *zap.Logger // 쿼리 로깅용 로거 (logStmt에 전달됨)
}

// Prepare는 SQL 쿼리를 준비(prepare)하고 logStmt로 감싸서 반환한다.
//
// Prepared Statement는 SQL을 미리 파싱해두고 나중에 인자만 바꿔 실행하는 방식이다.
// NestJS/TypeORM에서 parameterized query를 사용하는 것과 같은 개념이다.
// 여기서 query 문자열을 logStmt에 저장해두면, 실행 시점에 로깅할 수 있다.
func (c *logConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.origin.Prepare(query)
	if err != nil {
		return nil, fmt.Errorf("sql logdriver prepare: %w", err)
	}

	return &logStmt{origin: stmt, query: query, logger: c.logger}, nil
}

// PrepareContext는 Prepare의 context 지원 버전이다.
//
// driver.ConnPrepareContext 인터페이스를 구현한다.
// database/sql은 ConnPrepareContext를 구현하는 연결이 있으면
// Prepare 대신 이 메서드를 우선 호출한다.
//
// 원본 연결이 ConnPrepareContext를 지원하면 그것을 사용하고,
// 아니면 일반 Prepare로 폴백(fallback)한다.
//
// 타입 단언(type assertion): c.origin.(driver.ConnPrepareContext)
// Go에서 인터페이스 값이 특정 인터페이스를 추가로 구현하는지 확인하는 문법이다.
// ok가 true면 해당 인터페이스로 사용할 수 있다.
// NestJS에서 if (service instanceof ExtendedService) 패턴과 유사하다.
func (c *logConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var (
		stmt driver.Stmt
		err  error
	)

	if cc, ok := c.origin.(driver.ConnPrepareContext); ok {
		stmt, err = cc.PrepareContext(ctx, query)
	} else {
		stmt, err = c.origin.Prepare(query)
	}

	if err != nil {
		return nil, fmt.Errorf("sql logdriver prepare: %w", err)
	}

	return &logStmt{origin: stmt, query: query, logger: c.logger}, nil
}

// Close는 원본 연결을 닫는다.
func (c *logConn) Close() error {
	if err := c.origin.Close(); err != nil {
		return fmt.Errorf("sql logdriver close conn: %w", err)
	}

	return nil
}

// Begin은 트랜잭션을 시작한다.
//
// driver.Conn 인터페이스의 필수 메서드다.
// 이 메서드는 deprecated이지만, 인터페이스 충족을 위해 구현해야 한다.
// 실제로는 database/sql이 BeginTx를 우선 사용한다.
func (c *logConn) Begin() (driver.Tx, error) {
	tx, err := c.origin.Begin() //nolint:staticcheck // driver.Conn 인터페이스 충족 필수
	if err != nil {
		return nil, fmt.Errorf("sql logdriver begin: %w", err)
	}

	return tx, nil
}

// BeginTx는 context와 트랜잭션 옵션을 지원하는 트랜잭션 시작 메서드다.
//
// driver.ConnBeginTx 인터페이스를 구현한다.
// database/sql은 이 인터페이스를 구현하는 연결에서는 Begin() 대신 BeginTx()를 호출한다.
// context를 통해 타임아웃이나 취소를 트랜잭션에 전달할 수 있다.
//
// driver.TxOptions는 격리 수준(Isolation Level)과 읽기 전용 여부를 포함한다.
// SQLite는 격리 수준이 제한적이므로 대부분 기본값을 사용한다.
func (c *logConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if cc, ok := c.origin.(driver.ConnBeginTx); ok {
		tx, err := cc.BeginTx(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("sql logdriver begin tx: %w", err)
		}

		return tx, nil
	}

	tx, err := c.origin.Begin() //nolint:staticcheck // ConnBeginTx 미지원 시 폴백
	if err != nil {
		return nil, fmt.Errorf("sql logdriver begin: %w", err)
	}

	return tx, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// logStmt — driver.Stmt + driver.StmtExecContext + driver.StmtQueryContext 구현
// ─────────────────────────────────────────────────────────────────────────────
//
// driver.Stmt는 준비된(prepared) SQL 문을 나타내는 인터페이스다.
// Exec(인자 바인딩 후 실행)과 Query(인자 바인딩 후 조회) 메서드를 정의한다.
//
// logStmt는 실제 쿼리 로깅이 발생하는 핵심 구조체다.
// Exec/Query 호출 전후로 시간을 측정하고, 쿼리 문자열과 인자를 로깅한다.

// logStmt는 원본 prepared statement를 감싸는 로깅 래퍼다.
type logStmt struct {
	origin driver.Stmt // 원본 prepared statement
	logger *zap.Logger // 쿼리 로깅용 로거
	query  string      // 로깅용으로 저장해둔 SQL 쿼리 문자열
}

// Close는 원본 prepared statement를 닫는다.
func (s *logStmt) Close() error {
	if err := s.origin.Close(); err != nil {
		return fmt.Errorf("sql logdriver close stmt: %w", err)
	}

	return nil
}

// NumInput은 쿼리의 플레이스홀더(?) 개수를 반환한다.
//
// database/sql이 바인드 인자 개수를 검증할 때 사용한다.
// -1을 반환하면 검증을 건너뛴다 (일부 드라이버에서 사용).
func (s *logStmt) NumInput() int {
	return s.origin.NumInput()
}

// Exec는 INSERT/UPDATE/DELETE 등 결과 행이 없는 쿼리를 실행한다.
//
// driver.Stmt 인터페이스의 필수 메서드다.
// 이 메서드는 deprecated이지만, 인터페이스 충족을 위해 구현해야 한다.
// 실제로는 database/sql이 ExecContext를 우선 호출한다.
//
// driver.Value는 드라이버가 이해하는 값 타입의 인터페이스다.
// int64, float64, bool, []byte, string, time.Time 중 하나다.
func (s *logStmt) Exec(args []driver.Value) (driver.Result, error) {
	start := time.Now()

	result, err := s.origin.Exec(args) //nolint:staticcheck // driver.Stmt 인터페이스 충족 필수

	emitQueryLog(s.logger, s.query, valuesToAny(args), time.Since(start), err)

	if err != nil {
		return nil, fmt.Errorf("sql logdriver exec: %w", err)
	}

	return result, nil
}

// Query는 SELECT 등 결과 행이 있는 쿼리를 실행한다.
//
// driver.Stmt 인터페이스의 필수 메서드다.
// Exec와 마찬가지로 deprecated이지만, 인터페이스 충족을 위해 구현한다.
func (s *logStmt) Query(args []driver.Value) (driver.Rows, error) {
	start := time.Now()

	rows, err := s.origin.Query(args) //nolint:staticcheck // driver.Stmt 인터페이스 충족 필수

	emitQueryLog(s.logger, s.query, valuesToAny(args), time.Since(start), err)

	if err != nil {
		return nil, fmt.Errorf("sql logdriver query: %w", err)
	}

	return rows, nil
}

// ExecContext는 Exec의 context 지원 버전이다.
//
// driver.StmtExecContext 인터페이스를 구현한다.
// database/sql은 이 인터페이스를 구현하는 stmt에서는 Exec() 대신 이 메서드를 호출한다.
//
// driver.NamedValue는 이름 또는 순서(Ordinal)로 바인딩하는 인자 타입이다.
// 기존 driver.Value와 달리 Ordinal(1-based 순서)과 Name(이름) 필드가 추가되었다.
func (s *logStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	start := time.Now()

	var (
		result driver.Result
		err    error
	)

	// 원본 stmt가 StmtExecContext를 구현하면 사용하고, 아니면 Exec로 폴백한다.
	if sc, ok := s.origin.(driver.StmtExecContext); ok {
		result, err = sc.ExecContext(ctx, args)
	} else {
		result, err = s.origin.Exec(namedToValues(args)) //nolint:staticcheck // StmtExecContext 미지원 시 폴백
	}

	emitQueryLog(s.logger, s.query, namedToAny(args), time.Since(start), err)

	if err != nil {
		return nil, fmt.Errorf("sql logdriver exec: %w", err)
	}

	return result, nil
}

// QueryContext는 Query의 context 지원 버전이다.
//
// driver.StmtQueryContext 인터페이스를 구현한다.
// database/sql은 이 인터페이스를 구현하는 stmt에서는 Query() 대신 이 메서드를 호출한다.
func (s *logStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	start := time.Now()

	var (
		rows driver.Rows
		err  error
	)

	// 원본 stmt가 StmtQueryContext를 구현하면 사용하고, 아니면 Query로 폴백한다.
	if sc, ok := s.origin.(driver.StmtQueryContext); ok {
		rows, err = sc.QueryContext(ctx, args)
	} else {
		rows, err = s.origin.Query(namedToValues(args)) //nolint:staticcheck // StmtQueryContext 미지원 시 폴백
	}

	emitQueryLog(s.logger, s.query, namedToAny(args), time.Since(start), err)

	if err != nil {
		return nil, fmt.Errorf("sql logdriver query: %w", err)
	}

	return rows, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 헬퍼 함수들
// ─────────────────────────────────────────────────────────────────────────────

// emitQueryLog는 SQL 쿼리 실행 정보를 zap 로거로 출력한다.
//
// logger가 nil이면 아무것도 하지 않는다.
// 에러가 있으면 Warn 레벨, 정상이면 Debug 레벨로 출력한다.
//
// 출력 예시 (개발 환경, 컬러 콘솔):
//
//	DEBUG  SQL  {"query": "SELECT id, title FROM todos WHERE id = ?", "args": [1], "duration": "0.2ms"}
//	WARN   SQL  {"query": "INSERT INTO ...", "args": [...], "duration": "1ms", "error": "UNIQUE constraint failed"}
func emitQueryLog(logger *zap.Logger, query string, args []any, dur time.Duration, err error) {
	if logger == nil {
		return
	}

	// zap.String, zap.Any, zap.Duration은 구조화된 로그 필드를 생성한다.
	// zap.Duration은 개발 모드에서 "200µs", "1.5ms" 같은 읽기 좋은 형식으로 출력된다.
	fields := []zap.Field{
		zap.String("query", query),
		zap.Any("args", args),
		zap.Duration("duration", dur),
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		logger.Warn("SQL", fields...)
	} else {
		logger.Debug("SQL", fields...)
	}
}

// valuesToAny는 driver.Value 슬라이스를 []any로 변환한다.
//
// driver.Value는 interface{} 타입이지만, 슬라이스 타입이 다르므로
// 직접 캐스팅할 수 없다. Go에서는 []interface{}와 []ConcreteType 간
// 직접 변환이 불가능하여 요소별로 복사해야 한다.
// (이는 Go의 타입 시스템 제약 중 하나로, TypeScript와 다른 점이다)
func valuesToAny(args []driver.Value) []any {
	result := make([]any, len(args))
	for i, v := range args {
		result[i] = v
	}

	return result
}

// namedToAny는 driver.NamedValue 슬라이스에서 값만 추출하여 []any로 변환한다.
//
// NamedValue는 { Ordinal, Name, Value } 구조체인데,
// 로깅에는 Value(실제 바인드 값)만 필요하다.
func namedToAny(args []driver.NamedValue) []any {
	result := make([]any, len(args))
	for i, nv := range args {
		result[i] = nv.Value
	}

	return result
}

// namedToValues는 driver.NamedValue 슬라이스를 driver.Value 슬라이스로 변환한다.
//
// 원본 stmt가 StmtExecContext/StmtQueryContext를 구현하지 않을 때,
// 기존 Exec/Query 메서드의 인자 형식([]driver.Value)으로 변환하는 데 사용한다.
func namedToValues(args []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, len(args))
	for i, nv := range args {
		values[i] = nv.Value
	}

	return values
}
