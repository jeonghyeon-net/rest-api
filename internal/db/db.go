// Package db는 SQLite 데이터베이스 연결을 관리한다.
//
// 이 패키지는 두 가지 핵심 역할을 수행한다:
//  1. SQLite 데이터베이스 연결 생성 및 최적 설정 (openDB)
//  2. fx DI 컨테이너에 *sql.DB를 제공하는 생성자 (NewDB)
//
// NestJS에서 TypeOrmModule.forRoot()가 DB 연결을 설정하고 DI에 등록하는 것과 같다.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	// ─────────────────────────────────────────────────────────────────────
	// blank import(빈 임포트)로 SQLite 드라이버를 등록한다.
	// ─────────────────────────────────────────────────────────────────────
	//
	// Go의 database/sql 패키지는 "드라이버 인터페이스"만 정의하고,
	// 실제 DB 드라이버는 별도 패키지로 제공된다.
	// 드라이버 패키지를 blank import하면, 해당 패키지의 init() 함수가 실행되어
	// sql.Register()로 드라이버를 전역 레지스트리에 등록한다.
	//
	// NestJS에서 TypeORM 사용 시 별도의 DB 드라이버(pg, mysql2 등)를
	// npm install하는 것과 같은 개념이다.
	// TypeORM이 내부적으로 드라이버를 로드하듯, Go의 database/sql이
	// 등록된 드라이버를 이름("sqlite")으로 찾아 사용한다.
	//
	// modernc.org/sqlite는 순수 Go로 구현된 SQLite 드라이버다.
	// CGO(C 코드 연동)가 필요 없어서, CGO_ENABLED=0 빌드가 가능하다.
	// Docker 멀티스테이지 빌드에서 C 컴파일러 없이도 빌드할 수 있다.
	"go.uber.org/fx"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"

	"rest-api/internal/config"
)

// openDB는 지정된 경로에 SQLite 데이터베이스 연결을 생성하고 최적 설정을 적용한다.
//
// 소문자로 시작하므로 패키지 외부에서 접근할 수 없다(unexported/비공개).
// 이렇게 분리하면 fx.Lifecycle 없이도 단위 테스트가 가능하다.
// NestJS에서 private 메서드를 분리하여 테스트 가능성을 높이는 것과 같은 패턴이다.
//
// 매개변수:
//   - path: SQLite DB 파일 경로. 예: "./data/app.db"
//   - logger: SQL 쿼리 로깅용 로거. nil이면 로깅 없이 일반 드라이버를 사용한다.
//     개발 환경에서는 NewDB()가 zap.Logger를 전달하여 쿼리 로깅을 활성화하고,
//     프로덕션에서는 nil을 전달하여 로깅 없이 동작한다.
//
// 반환값:
//   - *sql.DB: Go 표준 라이브러리의 DB 연결 풀(connection pool) 객체.
//     NestJS의 DataSource나 PrismaClient와 유사한 역할.
//   - error: 에러가 있으면 반환, 없으면 nil.
//     Go에서는 예외(exception) 대신 에러를 반환값으로 전달한다.
//
// dirPerm은 DB 파일의 상위 디렉터리를 생성할 때 사용하는 파일 시스템 권한이다.
// 0o750 = rwxr-x--- (소유자: 읽기+쓰기+실행, 그룹: 읽기+실행, 기타: 접근 불가)
const dirPerm = 0o750

func openDB(ctx context.Context, path string, logger *zap.Logger) (*sql.DB, error) {
	// ─────────────────────────────────────────────────────────────────────
	// 1. DB 파일의 상위 디렉터리를 자동 생성
	// ─────────────────────────────────────────────────────────────────────
	//
	// filepath.Dir()은 경로에서 디렉터리 부분만 추출한다.
	// 예: "./data/app.db" → "./data"
	//
	// os.MkdirAll()은 Node.js의 fs.mkdirSync(dir, { recursive: true })와 같다.
	// 중간 경로가 없어도 모든 디렉터리를 재귀적으로 생성한다.
	// 이미 존재하면 에러 없이 넘어간다.
	//
	// 0o750은 Go의 8진수 리터럴로, 디렉터리 권한을 설정한다.
	// - 소유자: rwx (읽기+쓰기+실행)
	// - 그룹: r-x (읽기+실행)
	// - 기타: --- (접근 불가)
	// NestJS/Node.js에서는 보통 권한을 신경 쓰지 않지만,
	// Go에서는 파일/디렉터리 생성 시 권한을 명시적으로 지정해야 한다.
	// filepath.Clean()으로 경로를 정규화한다.
	// 예: "./data/../data/app.db" → "data/app.db"
	// 경로 조작(path traversal) 공격을 방지하기 위해 입력 경로를 정리한다.
	// gosec(보안 린터)가 외부 입력 경로를 직접 사용하면 경고(G703)를 내므로,
	// Clean으로 안전하게 정규화한 후 사용한다.
	cleanPath := filepath.Clean(path)
	dir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		// fmt.Errorf의 %w는 에러 래핑(wrapping) 동사다.
		// 원본 에러를 감싸면서 추가 컨텍스트를 붙인다.
		// 나중에 errors.Is()나 errors.Unwrap()으로 원본 에러를 꺼낼 수 있다.
		// NestJS에서 new InternalServerErrorException('...', { cause: originalError })와 유사.
		return nil, fmt.Errorf("DB 디렉터리 생성 실패 (%s): %w", dir, err)
	}

	// ─────────────────────────────────────────────────────────────────────
	// 2. SQLite 데이터베이스 연결 열기
	// ─────────────────────────────────────────────────────────────────────
	//
	// sql.Open()은 실제 연결을 즉시 만들지 않는다 (lazy connection).
	// 연결 풀(pool)을 초기화하고, 실제 쿼리 시점에 연결을 생성한다.
	// NestJS에서 TypeORM의 createConnection()이 pool을 설정하는 것과 같다.
	//
	// ─── DB 연결 열기: 로깅 여부에 따라 분기 ──────────────────────────
	//
	// logger가 nil이 아니면(개발 환경), newLoggedDB()로 쿼리 로깅이 활성화된 DB를 연다.
	// newLoggedDB()는 sql.OpenDB()에 logConnector를 전달하여, 드라이버 등록 없이
	// 모든 쿼리를 로깅하는 래퍼를 구성한다. (logdriver.go 참조)
	//
	// logger가 nil이면(프로덕션), 기존 sql.Open("sqlite", ...)을 그대로 사용한다.
	var db *sql.DB
	if logger != nil {
		var err error
		db, err = newLoggedDB(cleanPath, logger)
		if err != nil {
			return nil, fmt.Errorf("DB 열기 실패 (%s): %w", cleanPath, err)
		}
	} else {
		var err error
		db, err = sql.Open("sqlite", cleanPath)
		if err != nil {
			return nil, fmt.Errorf("DB 열기 실패 (%s): %w", cleanPath, err)
		}
	}

	// ─────────────────────────────────────────────────────────────────────
	// 3. SQLite PRAGMA 설정 — 성능과 안정성을 위한 필수 설정
	// ─────────────────────────────────────────────────────────────────────
	//
	// PRAGMA는 SQLite의 런타임 설정 명령어다.
	// PostgreSQL/MySQL과 달리 SQLite는 서버가 아니라 파일 기반 DB이므로,
	// 앱 시작 시 매번 PRAGMA로 설정을 적용해야 한다.

	// ─── journal_mode=WAL ───────────────────────────────────────────────
	// WAL(Write-Ahead Logging)은 SQLite의 동시성을 크게 개선하는 저널링 모드다.
	//
	// 기본 모드(DELETE)에서는 쓰기 중 읽기가 차단되지만,
	// WAL 모드에서는 읽기와 쓰기가 동시에 가능하다.
	// 웹 서버처럼 여러 요청이 동시에 DB에 접근하는 환경에 적합하다.
	//
	// WAL 모드는 한 번 설정하면 DB 파일에 영구 저장된다 (다른 PRAGMA와 다름).
	// 그래도 명시적으로 매번 설정하는 것이 안전하다.
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		// db.Close()로 이미 열린 연결을 정리한 후 에러를 반환한다.
		_ = db.Close()
		return nil, fmt.Errorf("PRAGMA journal_mode 설정 실패: %w", err)
	}

	// ─── foreign_keys=ON ────────────────────────────────────────────────
	// SQLite는 기본적으로 외래 키 제약(foreign key constraint)을 강제하지 않는다.
	// 즉, 존재하지 않는 부모 행을 참조하는 데이터를 삽입해도 에러가 나지 않는다.
	//
	// PostgreSQL/MySQL에서는 기본 활성화지만, SQLite에서는 매 연결마다 명시적으로
	// 활성화해야 한다. 이 설정을 빠뜨리면 데이터 무결성이 깨질 수 있다.
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("PRAGMA foreign_keys 설정 실패: %w", err)
	}

	// ─── busy_timeout=5000 ──────────────────────────────────────────────
	// SQLite는 파일 기반 DB이므로, 한 프로세스가 쓰기 중이면 다른 쓰기가 차단된다.
	// busy_timeout은 잠금(lock) 충돌 시 즉시 실패하지 않고, 지정한 밀리초만큼
	// 대기한 후 재시도하게 한다.
	//
	// 5000ms(5초) 동안 기다려도 잠금이 풀리지 않으면 "database is locked" 에러를 반환.
	// 웹 서버에서 동시 요청이 많을 때 일시적인 잠금 충돌을 자연스럽게 해소한다.
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("PRAGMA busy_timeout 설정 실패: %w", err)
	}

	// ─────────────────────────────────────────────────────────────────────
	// 4. 최대 연결 수를 1로 제한
	// ─────────────────────────────────────────────────────────────────────
	//
	// SQLite는 서버형 DB(PostgreSQL, MySQL)와 달리 파일 수준 잠금을 사용한다.
	// 여러 연결이 동시에 쓰기를 시도하면 "database is locked" 에러가 발생한다.
	//
	// MaxOpenConns(1)로 설정하면:
	// - 동시에 하나의 연결만 사용하므로 잠금 충돌이 원천적으로 방지된다.
	// - Go의 database/sql이 내부적으로 요청을 직렬화(serialize)한다.
	//
	// NestJS + TypeORM에서 PostgreSQL 연결 풀을 poolSize: 10으로 설정하는 것과 반대로,
	// SQLite에서는 1개로 제한하는 것이 최적이다.
	db.SetMaxOpenConns(1)

	return db, nil
}

// NewDB는 fx DI 컨테이너에 *sql.DB를 제공하는 생성자(constructor) 함수다.
//
// 대문자로 시작하므로 패키지 외부에서 접근 가능하다(exported/공개).
// AppModule에서 fx.Provide(db.NewDB)로 직접 등록된다.
//
// NestJS에서 @Module({ providers: [DatabaseService] })로 등록하고
// constructor(private db: DatabaseService)로 주입받는 것과 같다.
//
// 매개변수:
//   - lc: fx.Lifecycle — fx의 생명주기 관리자.
//     OnStart/OnStop 훅을 등록하여 앱 시작/종료 시 실행할 로직을 정의한다.
//     NestJS의 OnModuleInit/OnModuleDestroy 인터페이스와 같은 역할이다.
//   - logger: *zap.Logger — 구조화된 로거. fx가 자동으로 주입한다.
//   - cfg: *config.Config — 애플리케이션 설정. DBPath 등 DB 관련 설정을 포함한다.
//     internal/config 패키지를 직접 import하므로 래퍼 함수 없이 fx가 자동 주입한다.
//     NestJS에서 @Inject(ConfigService) config: ConfigService로 주입하는 것과 유사하다.
func NewDB(lc fx.Lifecycle, logger *zap.Logger, cfg *config.Config) (*sql.DB, error) {
	dbPath := cfg.DBPath
	logger.Info("SQLite 데이터베이스 연결 시작", zap.String("path", dbPath))

	// ─── 개발 환경 SQL 쿼리 로깅 설정 ────────────────────────────────────
	//
	// 프로덕션이 아닌 환경(development, test 등)에서는 로거를 openDB에 전달하여
	// 쿼리 로깅을 활성화한다. openDB는 내부적으로 newLoggedDB()를 호출하여
	// sql.OpenDB(logConnector)로 래핑된 DB를 생성한다.
	// 프로덕션에서는 nil을 전달하여 일반 sql.Open("sqlite", ...)을 사용한다.
	//
	// NestJS에서 TypeORM의 logging: process.env.NODE_ENV !== 'production' 설정과 같다.
	var queryLog *zap.Logger
	if cfg.AppEnv != "production" {
		queryLog = logger
		logger.Info("SQL 쿼리 로깅 활성화")
	}

	// openDB를 호출하여 DB 연결을 생성한다.
	// 이 함수는 위에서 정의한 비공개(unexported) 함수다.
	// PRAGMA 설정까지 모두 완료된 *sql.DB를 반환한다.
	// context.Background()를 사용하는 이유:
	// openDB는 앱 초기화 시 1회 호출되며, 요청 스코프(request-scoped) context가 아니다.
	// fx.Lifecycle의 OnStart에서 호출되므로 fx의 StartTimeout이 타임아웃을 관리한다.
	db, err := openDB(context.Background(), dbPath, queryLog)
	if err != nil {
		return nil, fmt.Errorf("DB 연결 실패: %w", err)
	}

	logger.Info("SQLite 데이터베이스 연결 완료")

	// 마이그레이션은 이 함수에서 실행하지 않는다.
	// fx.Invoke(RunMigrations)로 별도 단계에서 실행한다.
	// 이렇게 분리하면 테스트에서 fx.Replace로 DB를 교체한 후에도
	// 교체된 DB에 마이그레이션이 정상적으로 실행된다.

	// ─────────────────────────────────────────────────────────────────────
	// fx.Lifecycle에 종료 훅(OnStop) 등록
	// ─────────────────────────────────────────────────────────────────────
	//
	// fx.Lifecycle은 앱의 생명주기(시작/종료)에 훅을 등록하는 메커니즘이다.
	//
	// NestJS에서 OnModuleDestroy 인터페이스를 구현하면
	// 앱 종료 시 자동으로 onModuleDestroy() 메서드가 호출되는 것과 같다.
	// 예: class DatabaseService implements OnModuleDestroy {
	//       async onModuleDestroy() { await this.db.close(); }
	//     }
	//
	// fx에서는 lc.Append()로 Hook을 등록하면,
	// 앱이 종료(SIGTERM, SIGINT 등)될 때 OnStop 함수가 자동으로 호출된다.
	//
	// context.Context는 Go의 요청 생명주기 관리 패턴이다.
	// 여기서는 fx가 종료 타임아웃을 관리하기 위해 전달한다.
	// fx.StopTimeout으로 설정한 시간 내에 OnStop이 완료되지 않으면 강제 종료된다.
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			logger.Info("SQLite 데이터베이스 연결 종료 중...")
			return db.Close()
		},
	})

	return db, nil
}
