package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/joho/godotenv"
	// automaxprocs는 우버가 만든 라이브러리로, 앱 시작 시 GOMAXPROCS를 자동으로 설정한다.
	// GOMAXPROCS는 Go 런타임이 동시에 사용할 수 있는 OS 스레드 수를 결정하는 값이다.
	//
	// 왜 필요한가?
	// Docker/K8s에서 컨테이너에 CPU를 2코어만 할당해도, Go는 기본적으로 호스트 머신의
	// 전체 CPU 수(예: 64코어)를 GOMAXPROCS로 설정한다.
	// 이러면 64개 스레드가 2코어를 두고 경쟁하면서 컨텍스트 스위칭 오버헤드가 발생하고,
	// 오히려 성능이 떨어진다.
	//
	// automaxprocs는 cgroup(리눅스 컨테이너의 리소스 제한 메커니즘)을 읽어서
	// 컨테이너에 할당된 실제 CPU 수에 맞게 GOMAXPROCS를 자동 조정한다.
	//
	// blank import(_)는 패키지를 직접 사용하지 않지만, init() 함수의 부수효과(side effect)를
	// 위해 임포트하는 Go 관용 패턴이다. automaxprocs의 init()이 자동으로 GOMAXPROCS를 설정한다.
	_ "go.uber.org/automaxprocs"
	"go.uber.org/fx"

	"rest-api/internal/app"
	"rest-api/internal/config"
	"rest-api/internal/domain/todo"
	todohttp "rest-api/internal/domain/todo/handler/http"
)

// main은 애플리케이션의 진입점이다.
// fx.New()로 DI 컨테이너를 생성하고, Run()으로 애플리케이션을 시작한다.
//
// NestJS와 비교하면:
//   - fx.New()는 NestApp.create()와 같은 역할
//   - fx.Supply()는 @Module()의 providers에서 useValue로 값을 등록하는 것과 같은 역할
//   - app.AppModule()은 @Module()의 imports에 다른 모듈을 넣는 것과 같은 역할
//   - fx.Invoke()는 모듈이 로드된 후 실행되는 onModuleInit()과 유사
//   - Run()은 app.listen()과 같은 역할 (graceful shutdown 포함)
//
// 이 파일은 진입점 역할만 담당한다.
// 설정, 로거, 서버, 에러 처리, 검증 등 핵심 로직은 internal/app 패키지에 있다.
// 이렇게 분리하면 테스트(internal/testutil)에서도 동일한 DI 그래프를 재사용할 수 있다.
func main() {
	// 서버 전체의 기본 타임존을 UTC로 고정한다.
	// time.Local은 Go의 전역 타임존 설정으로,
	// time.Now()가 반환하는 시간의 기준 시간대를 결정한다.
	// UTC로 고정하면 서버가 어느 리전(한국, 미국 등)에서 실행되든 동일한 시간을 사용한다.
	// NestJS에서 dayjs.tz.setDefault('UTC')와 비슷한 개념이다.
	time.Local = time.UTC

	// .env 파일에서 환경변수를 로드한다.
	// NestJS의 ConfigModule.forRoot()와 비슷한 역할이다.
	//
	// godotenv.Load()는 .env 파일이 없으면 에러를 반환하지만,
	// 운영 환경에서는 .env 파일 없이 실제 환경변수를 사용하므로 에러를 무시한다.
	// (Docker, K8s 등에서 환경변수를 직접 주입하는 것이 일반적)
	if err := godotenv.Load(); err != nil {
		// .env 파일이 없으면 에러가 발생하지만, 운영 환경에서는 정상이므로 로그만 남긴다.
		// DI 컨테이너가 아직 초기화되지 않아 zap 로거를 사용할 수 없으므로 fmt.Printf를 사용한다.
		fmt.Printf(".env 파일 로드 건너뜀: %v\n", err)
	}

	// Config를 fx 바깥에서 먼저 생성한다.
	// fx.StopTimeout에 Config.ShutdownTimeout 값을 전달하기 위해
	// fx 컨테이너 생성 전에 Config가 필요하다.
	//
	// config.NewConfig()는 internal/config 패키지의 생성자다.
	// 환경변수에서 설정값을 읽어 *config.Config를 반환한다.
	// Config를 별도 패키지로 분리하여 app, db 등 여러 패키지에서 공유한다.
	cfg := config.NewConfig()

	fx.New(
		// fx.StopTimeout은 앱 종료 시 OnStop 훅들이 완료될 때까지 기다리는 최대 시간이다.
		// Config에서 값을 가져와 한 곳에서 관리한다.
		fx.StopTimeout(cfg.ShutdownTimeout),

		// cfg를 DI 컨테이너에 등록한다.
		// fx.Supply는 이미 생성된 값을 등록하는 함수다.
		// fx.Provide(app.NewConfig)와 달리, 함수를 호출하지 않고 cfg 인스턴스를 그대로 등록한다.
		fx.Supply(cfg),

		// AppModule()은 로거, Fiber 앱, DB 연결 등
		// 프로덕션에 필요한 인프라 의존성을 하나의 fx.Option으로 묶어 제공한다.
		// NestJS에서 AppModule을 imports에 넣는 것과 같다.
		app.AppModule(),

		// ─── Todo 도메인 모듈 ────────────────────────────────────────────
		// Todo 도메인의 서비스, 핸들러, 라우트 등록을 DI에 추가한다.
		// NestJS에서 AppModule의 imports에 도메인 모듈을 나열하는 것과 같다:
		//   @Module({ imports: [TodoModule, UserModule, ...] })
		//
		// app.AppModule()에 직접 넣지 않는 이유:
		// 도메인 서비스가 internal/app의 에러 타입(AppError)을 import하므로
		// app → todo → app 순환 참조(import cycle)가 발생한다.
		// Go에서는 패키지 간 순환 참조가 절대 허용되지 않는다.
		// main.go에서 별도로 등록하면 의존 방향이 한쪽(todo → app)으로 유지된다.
		//
		// todo.NewService: alias.go의 생성자. fx가 *sql.DB를 자동 주입한다.
		// todohttp.New: HTTP 핸들러 생성자. fx가 todo.Service를 자동 주입한다.
		fx.Provide(todo.NewService),
		fx.Provide(todohttp.New),

		// Todo 도메인의 라우트를 Fiber 앱에 등록한다.
		// fx.Invoke로 등록하면 앱 시작 시 자동으로 실행된다.
		// fx가 *fiber.App과 todohttp.Handler를 자동 주입한다.
		//
		// newFiberApp에 직접 todohttp.Handler를 매개변수로 추가하지 않는 이유:
		// newFiberApp은 internal/app 패키지에 있고, todohttp는 todo 도메인에 있으므로
		// app → todo 의존이 생기면 순환 참조가 된다.
		// fx.Invoke를 사용하면 main.go가 두 패키지를 조합하므로 순환을 피한다.
		fx.Invoke(func(fiberApp *fiber.App, h todohttp.Handler) {
			h.RegisterRoutes(fiberApp)
		}),

		// StartServer 함수를 호출한다.
		// fx.Invoke()에 등록된 함수는 앱 시작 시 자동 실행된다.
		// 매개변수(fx.Lifecycle, *fiber.App 등)는 DI 컨테이너에서 자동으로 주입받는다.
		fx.Invoke(app.StartServer),
	).Run()
}
