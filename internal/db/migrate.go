package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// ─────────────────────────────────────────────────────────────────────────────
// [go:embed] — 빌드 시점에 파일을 바이너리에 포함시키는 Go 내장 기능
// ─────────────────────────────────────────────────────────────────────────────
//
// NestJS에서는 정적 파일(템플릿, SQL 등)을 서빙하려면 별도 설정이 필요하다.
// 예: ServeStaticModule이나 webpack 플러그인으로 assets를 번들링.
//
// Go의 go:embed는 컴파일 타임에 지정한 파일/디렉터리를 바이너리 안에 직접 삽입한다.
// 배포할 때 .sql 파일을 따로 복사할 필요 없이, 실행 파일 하나만 있으면 된다.
//
// embed.FS는 임베드된 파일들을 읽기 전용 파일 시스템(fs.FS 인터페이스)으로 제공한다.
// 즉, 일반 파일을 읽는 것처럼 임베드된 파일에 접근할 수 있다.
// ─────────────────────────────────────────────────────────────────────────────

//go:embed migration/*.sql
var migrationFS embed.FS

// RunMigrations는 goose를 사용하여 데이터베이스 마이그레이션을 실행한다.
//
// NestJS에서 TypeORM의 migration:run이나 Prisma의 prisma migrate deploy와 같은 역할이다.
// 앱 시작 시 자동으로 실행되어, DB 스키마를 최신 상태로 유지한다.
//
// 대문자로 시작하는 함수명(RunMigrations)은 Go에서 "exported"(공개) 함수를 의미한다.
// NestJS에서 export class/function으로 외부에 공개하는 것과 같다.
// 소문자로 시작하면 패키지 내부에서만 사용 가능하다(unexported/비공개).
//
// 매개변수:
//   - db: database/sql 패키지의 *sql.DB — Go 표준 라이브러리의 DB 연결 객체.
//     NestJS의 DataSource나 PrismaClient처럼 DB와 통신하는 핸들.
func RunMigrations(db *sql.DB) error {
	// ─────────────────────────────────────────────────────────────────────
	// fs.Sub — 임베드된 파일 시스템에서 하위 디렉터리를 추출
	// ─────────────────────────────────────────────────────────────────────
	//
	// go:embed로 "migration/*.sql"을 임베드하면, 파일 경로가 "migration/00001_init.sql"
	// 형태로 저장된다. 하지만 goose는 루트에 .sql 파일이 바로 있기를 기대한다.
	//
	// fs.Sub(migrationFS, "migration")은 "migration" 디렉터리를 루트로 하는
	// 새로운 파일 시스템을 만든다. 결과적으로 "00001_init.sql"처럼 접근할 수 있다.
	//
	// Node.js로 비유하면 path.join(__dirname, 'migration')으로 서브 디렉터리를
	// 기준 경로로 설정하는 것과 비슷하다.
	// ─────────────────────────────────────────────────────────────────────
	fsys, err := fs.Sub(migrationFS, "migration")
	if err != nil {
		return fmt.Errorf("마이그레이션 FS 로드 실패: %w", err)
	}

	// ─────────────────────────────────────────────────────────────────────
	// goose.NewProvider — 새로운 goose v3 API로 마이그레이션 프로바이더 생성
	// ─────────────────────────────────────────────────────────────────────
	//
	// 레거시 goose API(goose.Up, goose.SetDialect 등)는 글로벌 상태에 의존했다.
	// NewProvider는 이를 개선하여, 인스턴스 기반으로 동작한다.
	// NestJS에서 글로벌 설정 대신 모듈별 설정을 선호하는 것과 같은 맥락이다.
	//
	// 매개변수:
	//   - goose.DialectSQLite3: 사용할 DB 방언(dialect). SQLite3를 사용.
	//   - db: 마이그레이션을 실행할 DB 연결.
	//   - fsys: 마이그레이션 SQL 파일이 들어 있는 파일 시스템.
	// ─────────────────────────────────────────────────────────────────────
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return fmt.Errorf("goose provider 생성 실패: %w", err)
	}

	// ─────────────────────────────────────────────────────────────────────
	// provider.Up — 아직 적용되지 않은 모든 마이그레이션을 순서대로 실행
	// ─────────────────────────────────────────────────────────────────────
	//
	// NestJS + TypeORM에서 migration:run을 실행하면 pending 마이그레이션이
	// 순서대로 적용되는 것과 동일하다. 이미 적용된 마이그레이션은 건너뛴다.
	//
	// context.Background()는 Go의 컨텍스트(Context) 패턴에서 "빈 컨텍스트"를 의미한다.
	// Go에서 컨텍스트는 요청의 생명주기, 타임아웃, 취소 신호 등을 전달하는 메커니즘이다.
	// NestJS에서 Request 객체가 요청 범위 데이터를 운반하는 것과 유사하다.
	//
	// 여기서는 앱 시작 시 한 번 실행되는 작업이므로, 특별한 타임아웃이나 취소 조건 없이
	// 기본(빈) 컨텍스트를 사용한다.
	// ─────────────────────────────────────────────────────────────────────
	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("마이그레이션 실행 실패: %w", err)
	}

	return nil
}
