//go:build e2e

// Package testutil은 E2E 테스트를 위한 공유 헬퍼를 제공한다.
//
// 이 패키지의 핵심은 NewTestApp() 함수로, 프로덕션과 동일한 DI 그래프를 구성하되
// DB만 in-memory SQLite로 교체하여 격리된 테스트 환경을 만든다.
//
// NestJS에서 Test.createTestingModule()으로 테스트 모듈을 만들고
// .overrideProvider()로 특정 의존성을 교체하는 패턴과 같다:
//
//	const module = await Test.createTestingModule({
//	  imports: [AppModule],
//	})
//	.overrideProvider(DatabaseService)
//	.useValue(mockDatabase)
//	.compile();
//
// //go:build e2e 빌드 태그에 의해 일반 빌드(go build)나 일반 테스트(go test)에는
// 포함되지 않는다. -tags=e2e 플래그를 명시적으로 전달해야만 컴파일된다.
// NestJS에서 jest --testPathPattern=e2e로 E2E 테스트만 분리 실행하는 것과 같다.
package testutil

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	// modernc.org/sqlite 드라이버를 등록한다.
	// blank import(_)로 드라이버의 init() 함수만 실행시킨다.
	// 이 패키지에서 직접 sql.Open("sqlite", ...)을 호출하므로 드라이버 등록이 필요하다.
	_ "modernc.org/sqlite"

	"rest-api/internal/app"
)

// NewTestApp은 E2E 테스트용 Fiber 앱과 in-memory DB를 생성하여 반환한다.
//
// 프로덕션과 동일한 DI 그래프(app.AppModule)를 사용하되, 다음 두 가지를 교체한다:
//   - Config: 테스트에 적합한 설정(포트 0, 짧은 타임아웃 등)
//   - *sql.DB: 파일 기반 DB 대신 in-memory SQLite를 사용
//
// NestJS의 Test.createTestingModule() + .overrideProvider() 패턴과 같다.
//
// 매개변수:
//   - t: Go의 테스트 컨텍스트. fxtest가 테스트 실패 시 t.Fatal()을 호출하고,
//     t.Cleanup()으로 리소스 정리를 등록한다.
//   - opts: 추가 fx.Option. 테스트별로 추가 의존성을 등록할 수 있다.
//     NestJS에서 .overrideProvider()를 여러 번 체이닝하는 것과 같다.
//
// 반환값:
//   - *fiber.App: 프로덕션과 동일하게 구성된 Fiber 앱 인스턴스.
//     httptest.NewRequest()와 app.Test()로 HTTP 요청을 시뮬레이션할 수 있다.
//   - *sql.DB: in-memory SQLite DB. 테스트 데이터 조회/검증에 사용한다.
func NewTestApp(t *testing.T, opts ...fx.Option) (*fiber.App, *sql.DB) {
	t.Helper() // 테스트 실패 시 이 함수가 아닌 호출자의 위치를 에러 메시지에 표시

	// ─── 1단계: 테스트용 Config 생성 ──────────────────────────────────────
	//
	// 프로덕션 Config(app.NewConfig)와 같은 구조체를 사용하되,
	// 테스트에 적합한 값으로 변경한다:
	//   - AppEnv: "test" — 테스트 환경임을 명시
	//   - Port: "0" — OS가 사용 가능한 임의의 포트를 할당 (포트 충돌 방지)
	//   - DBPath: ":memory:" — 실제 파일 대신 메모리에만 존재하는 DB
	//   - 짧은 타임아웃 — 테스트가 빠르게 실패하도록
	cfg := &app.Config{
		AppEnv:          "test",
		Port:            "0",
		DBPath:          ":memory:",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		IdleTimeout:     5 * time.Second,
		ShutdownTimeout: 5 * time.Second,
		BodyLimit:       1 * 1024 * 1024, // 1MB — 테스트에는 작은 크기로 충분
	}

	// ─── 2단계: in-memory SQLite DB 생성 ─────────────────────────────────
	//
	// sql.Open("sqlite", ":memory:")는 파일 없이 메모리에만 존재하는 DB를 연다.
	// 테스트마다 독립된 DB가 생성되므로 테스트 간 데이터 격리가 보장된다.
	//
	// NestJS에서 TypeORM의 { type: 'sqlite', database: ':memory:' } 설정과 같다.
	//
	// 프로덕션의 db.NewDB()는 파일 경로에 디렉터리 생성, WAL 모드 설정 등
	// 파일 기반 DB에 특화된 로직이 포함되어 있어 in-memory DB와 호환되지 않는다.
	// 따라서 여기서 직접 sql.Open()으로 DB를 생성한다.
	memDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err, "in-memory SQLite 열기 실패")

	// ─── PRAGMA 설정 ────────────────────────────────────────────────────
	//
	// SQLite는 기본적으로 외래 키 제약을 강제하지 않는다.
	// 프로덕션(db.openDB)에서는 journal_mode=WAL, foreign_keys=ON, busy_timeout=5000을
	// 모두 설정하지만, in-memory DB에서는 foreign_keys=ON만 필요하다:
	//   - journal_mode=WAL: 메모리 DB에는 저널 파일이 없으므로 불필요
	//   - busy_timeout: 단일 연결(MaxOpenConns=1)이므로 잠금 충돌이 없어 불필요
	_, err = memDB.Exec("PRAGMA foreign_keys=ON")
	require.NoError(t, err, "PRAGMA foreign_keys 설정 실패")

	// 최대 연결 수를 1로 제한한다.
	// 프로덕션과 동일한 설정으로, SQLite의 파일 수준 잠금 충돌을 방지한다.
	memDB.SetMaxOpenConns(1)

	// ─── 3단계: fxtest로 DI 그래프 구성 ──────────────────────────────────
	//
	// fxtest.New()는 fx.New()의 테스트 전용 버전이다.
	// testing.T를 받아서, DI 그래프 구성에 실패하면 t.Fatal()로 테스트를 즉시 중단한다.
	// 프로덕션의 fx.New()가 os.Exit(1)을 호출하는 것과 달리,
	// fxtest는 Go 테스트 프레임워크와 통합되어 깔끔한 에러 리포트를 제공한다.
	//
	// NestJS에서 Test.createTestingModule()이 테스트 환경에 맞게 모듈을 구성하는 것과 같다.
	//
	// DI 옵션 구성:
	//   1. fx.Supply(cfg)    — 테스트용 Config를 DI에 등록
	//   2. app.AppModule()   — 프로덕션 DI 그래프 전체를 가져옴 (로거, Fiber, DB, 마이그레이션)
	//   3. fx.Decorate(...)  — AppModule이 제공하는 *sql.DB를 in-memory DB로 교체
	//   4. fx.Options(opts)  — 테스트별 추가 옵션
	//   5. fx.Populate(...)  — DI 컨테이너에서 값을 꺼내 로컬 변수에 저장
	//
	// fx.Decorate vs fx.Replace:
	//   fx.Replace는 fx.Supply와 같은 레벨에서 값을 교체하지만,
	//   AppModule의 fx.Provide(db.NewDB wrapper)와 같은 타입(*sql.DB)이 충돌할 수 있다.
	//   fx.Decorate는 기존 provider가 생성한 값을 "데코레이트"하여 교체하므로,
	//   provider가 먼저 실행된 후 결과를 대체한다. 하지만 여기서는 provider 자체가
	//   파일 기반 DB를 열려고 하므로(":memory:"가 Config에 있어도 디렉터리 생성 시도),
	//   provider 실행 자체를 우회해야 한다.
	//
	//   해결: fx.Replace로 이미 생성된 *sql.DB 인스턴스를 제공하면,
	//   fx는 같은 타입의 provider를 실행하지 않고 Replace된 값을 사용한다.
	var fiberApp *fiber.App

	fxApp := fxtest.New(t,
		// 테스트용 Config를 DI에 등록한다.
		// AppModule의 newLogger, newFiberApp 등이 이 Config를 주입받는다.
		fx.Supply(cfg),

		// 프로덕션 DI 그래프 전체를 가져온다.
		// 로거 생성, Fiber 앱 생성, 헬스체크 라우트 등 모든 인프라가 포함된다.
		// DB provider(db.NewDB wrapper)도 포함되지만, fx.Replace로 교체된다.
		app.AppModule(),

		// AppModule이 등록한 *sql.DB provider 대신 in-memory DB를 사용한다.
		// fx.Replace()는 같은 타입의 기존 provider를 무시하고,
		// 여기서 제공하는 값으로 대체한다.
		//
		// NestJS에서 .overrideProvider(DatabaseService).useValue(mockDb)와 같다.
		fx.Replace(memDB),

		// 테스트별 추가 옵션을 적용한다.
		// 예: 특정 도메인 모듈을 추가하거나, mock 서비스를 등록할 수 있다.
		fx.Options(opts...),

		// DI 컨테이너에서 *fiber.App을 꺼내 로컬 변수에 저장한다.
		// fx.Populate()는 포인터를 받아서, DI 컨테이너의 해당 타입 값을 대입한다.
		// NestJS에서 module.get(FiberApp)으로 인스턴스를 꺼내는 것과 같다.
		fx.Populate(&fiberApp),
	)

	// ─── 4단계: DI 그래프 시작 ───────────────────────────────────────────
	//
	// fxtest의 RequireStart()는 DI 그래프의 모든 OnStart 훅을 실행한다.
	// 실패하면 t.Fatal()로 테스트를 즉시 중단한다.
	// 여기서 실행되는 훅:
	//   - db.RunMigrations: memDB에 마이그레이션 적용 (테이블 생성 등)
	//   - (StartServer는 포함되지 않음 — main.go에서만 fx.Invoke)
	fxApp.RequireStart()

	// ─── 5단계: 리소스 정리 등록 ─────────────────────────────────────────
	//
	// t.Cleanup()은 테스트 함수가 종료된 후 자동으로 실행되는 정리 함수를 등록한다.
	// NestJS에서 afterAll(() => app.close())과 같은 역할이다.
	//
	// 등록 순서와 실행 순서:
	// t.Cleanup은 LIFO(Last In, First Out) 순서로 실행된다.
	// 여기서는 하나의 Cleanup에서 순서를 직접 제어한다:
	//   1. fxApp.RequireStop() — DI 그래프의 모든 OnStop 훅 실행 (서버 종료 등)
	//   2. memDB.Close() — in-memory DB 연결 종료 (메모리 해제)
	t.Cleanup(func() {
		fxApp.RequireStop()
		memDB.Close()
	})

	return fiberApp, memDB
}
