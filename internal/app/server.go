package app

import (
	"context"
	"database/sql"
	"fmt"
	"net"

	"github.com/bytedance/sonic"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/etag"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"rest-api/internal/config"
)

// newFiberApp은 Fiber 애플리케이션 인스턴스를 생성하고 라우트를 등록한다.
// AppModule 내에서 fx.Provide()로 등록되며,
// *fiber.App 타입이 필요한 곳에 자동으로 주입된다.
//
// NestJS에서 Express/Fastify 인스턴스를 설정하는 것과 비슷하다.
func newFiberApp(cfg *config.Config, logger *zap.Logger, database *sql.DB) *fiber.App {
	// fiber.New()로 새로운 Fiber 앱을 생성한다.
	// NestJS의 NestFactory.create()에서 내부적으로 Express 인스턴스를 만드는 것과 같다.
	//
	// Config 구조체에서 타임아웃, 바디 제한 등의 설정을 읽어온다.
	// 하드코딩 대신 cfg를 사용하면 테스트에서 설정을 자유롭게 교체할 수 있다.
	app := fiber.New(fiber.Config{
		// === JSON 엔진 ===
		// sonic은 ByteDance(틱톡)가 만든 초고속 JSON 라이브러리다.
		// Go 표준 encoding/json 대비 2~3배 빠른 직렬화/역직렬화 성능을 제공한다.
		// JIT(Just-In-Time) 컴파일을 사용하여 런타임에 최적화된 코덱을 생성한다.
		// Fiber의 JSONEncoder/JSONDecoder를 교체하면 c.JSON(), c.BodyParser() 등
		// 모든 JSON 처리에 sonic이 사용된다.
		JSONEncoder: sonic.Marshal,
		JSONDecoder: sonic.Unmarshal,

		// === 타임아웃 설정 ===
		// NestJS에서는 @nestjs/platform-express가 내부적으로 처리하지만,
		// Go에서는 서버 레벨에서 직접 설정해야 한다.
		// Config 구조체에서 값을 가져와 한 곳에서 관리한다.

		// ReadTimeout: 클라이언트가 요청 헤더+바디를 보내는 데 허용되는 최대 시간.
		// Slowloris 공격(헤더를 아주 천천히 보내 서버 자원을 점유하는 공격) 방어에 효과적이다.
		ReadTimeout: cfg.ReadTimeout,

		// WriteTimeout: 서버가 응답을 보내는 데 허용되는 최대 시간.
		// 무한정 응답을 대기하는 상황을 방지한다.
		WriteTimeout: cfg.WriteTimeout,

		// IdleTimeout: Keep-Alive 연결에서 다음 요청을 기다리는 최대 유휴 시간.
		// HTTP/1.1은 기본적으로 Keep-Alive(연결 재사용)를 사용하는데,
		// 유휴 시간 초과 시 연결을 닫아 서버 리소스를 회수한다.
		IdleTimeout: cfg.IdleTimeout,

		// === 보안 설정 ===

		// BodyLimit: 요청 바디의 최대 크기를 제한한다.
		// NestJS에서 app.use(express.json({ limit: '4mb' }))와 같은 역할이다.
		// 비정상적으로 큰 요청으로 서버 메모리를 고갈시키는 공격을 방어한다.
		BodyLimit: cfg.BodyLimit,

		// ServerHeader: 빈 문자열로 설정하면 응답 헤더에 "Server: Fiber" 정보가 포함되지 않는다.
		// 공격자가 서버 기술 스택을 파악하기 어렵게 만드는 보안 관례(Security by Obscurity)다.
		// NestJS에서 app.use(helmet())이 X-Powered-By 헤더를 제거하는 것과 같은 맥락이다.
		ServerHeader: "",

		// === 라우팅 설정 ===

		// StrictRouting: /api/users와 /api/users/를 다른 경로로 취급한다.
		// false(기본값)면 트레일링 슬래시 유무에 관계없이 같은 핸들러가 매칭된다.
		// REST API에서는 URL의 의미를 명확히 하기 위해 true로 설정하는 것이 좋다.
		StrictRouting: true,

		// CaseSensitive: /api/Users와 /api/users를 다른 경로로 취급한다.
		// false(기본값)면 대소문자 구분 없이 매칭된다.
		// REST API에서 URL 해석의 일관성을 위해 true로 설정한다.
		CaseSensitive: true,

		// === 요청 검증 ===

		// StructValidator: 요청 바디를 구조체로 파싱한 뒤 자동으로 검증을 실행한다.
		// NestJS의 app.useGlobalPipes(new ValidationPipe())과 같은 역할이다.
		// c.Bind().Body(&req) 호출 시 파싱 -> 검증이 한 번에 이루어진다.
		// 검증 규칙은 구조체의 validate 태그로 정의한다.
		// (예: `validate:"required,email"`)
		StructValidator: newStructValidator(),

		// === 에러 처리 ===

		// ErrorHandler: 핸들러에서 반환된 에러를 JSON 응답으로 변환하는 전역 에러 핸들러다.
		// NestJS의 전역 ExceptionFilter(@Catch())와 같은 역할이다.
		// AppError, fiber.Error, ValidationErrors 등 에러 타입별로 적절한 응답을 생성한다.
		ErrorHandler: newErrorHandler(logger),
	})

	// 보안/유틸리티 미들웨어를 등록한다.
	// NestJS에서 main.ts에 app.use()로 전역 미들웨어를 등록하는 것과 동일한 패턴이다.
	setupMiddleware(app)

	// ─── 헬스체크 라우트 ─────────────────────────────────────────────────
	// Kubernetes의 프로브(Probe) 패턴을 따르는 두 가지 헬스체크 엔드포인트를 등록한다.
	// NestJS에서 @nestjs/terminus 라이브러리로 HealthCheckService를 구성하는 것과 같다.
	//
	// 쿠버네티스는 두 가지 프로브로 파드(Pod)의 상태를 관리한다:
	//   - livenessProbe → /livez: 프로세스가 살아있는지 확인
	//   - readinessProbe → /readyz: 트래픽을 받을 준비가 되었는지 확인
	//
	// 경로명의 z 접미사는 쿠버네티스 API 관례다.
	// (예: /healthz, /livez, /readyz — k8s API 서버도 동일한 패턴 사용)
	registerHealthRoutes(app, database)

	return app
}

// setupMiddleware는 보안 및 유틸리티 미들웨어를 등록한다.
// NestJS의 main.ts에서 app.use(helmet()), app.enableCors() 등을 설정하는 것과 같다.
//
// 미들웨어는 등록 순서대로 실행된다. 순서가 중요한 이유:
//
//	recover → 가장 먼저: 이후 미들웨어에서 패닉이 발생해도 서버가 죽지 않도록 보호
//	requestid → 요청 추적 ID 생성 (이후 로깅에 활용)
//	helmet → 보안 헤더 설정
//	compress → 응답 압축
//	etag → 캐싱 지원
//	cors → CORS 정책 설정
func setupMiddleware(app *fiber.App) {
	// recover: 핸들러에서 panic이 발생해도 서버 전체가 크래시하지 않도록 보호한다.
	// Go에서 panic은 NestJS의 throw new Error()와 비슷하지만, 처리하지 않으면 프로세스가 종료된다.
	// NestJS는 기본적으로 예외를 잡아주지만, Go에서는 직접 recover 미들웨어를 등록해야 한다.
	app.Use(recover.New())

	// requestid: 각 요청에 고유한 UUID를 부여한다.
	// 응답 헤더(X-Request-ID)에 포함되어, 로그 추적이나 디버깅 시 특정 요청을 식별할 수 있다.
	// NestJS에서 직접 구현하거나 cls-hooked 같은 라이브러리로 처리하던 것을 미들웨어 하나로 해결한다.
	app.Use(requestid.New())

	// helmet: 보안 관련 HTTP 응답 헤더를 자동으로 설정한다.
	// NestJS에서 app.use(helmet())과 정확히 같은 역할이다.
	// X-Content-Type-Options, X-Frame-Options, X-XSS-Protection 등의 헤더를 설정하여
	// XSS, 클릭재킹, MIME 스니핑 등 일반적인 웹 공격을 방어한다.
	app.Use(helmet.New())

	// compress: 응답 바디를 gzip/brotli/deflate로 압축한다.
	// 클라이언트의 Accept-Encoding 헤더를 보고 지원하는 압축 방식을 자동 선택한다.
	// NestJS에서 app.use(compression())과 같다.
	// API 응답 크기를 줄여 네트워크 대역폭을 절약하고 응답 속도를 개선한다.
	app.Use(compress.New())

	// etag: 응답에 ETag(Entity Tag) 헤더를 자동으로 추가한다.
	// 응답 바디의 해시값을 기반으로 ETag를 생성하여, 동일한 데이터에 대해
	// 클라이언트가 "If-None-Match" 헤더로 캐시 유효성을 검증할 수 있게 한다.
	// 데이터가 변경되지 않았으면 304 Not Modified를 반환하여 대역폭을 절약한다.
	app.Use(etag.New())

	// cors: Cross-Origin Resource Sharing 정책을 설정한다.
	// NestJS에서 app.enableCors()와 같다.
	// 기본 설정은 모든 오리진을 허용한다 (프로덕션에서는 AllowOrigins를 제한해야 한다).
	app.Use(cors.New())
}

// StartServer는 fx.Lifecycle 훅을 사용하여 서버의 시작과 종료를 관리한다.
//
// 대문자로 시작하므로 패키지 외부에서 접근 가능하다(exported/공개).
// main.go에서 fx.Invoke(app.StartServer)로 호출된다.
//
// fx.Lifecycle은 NestJS의 OnModuleInit / OnModuleDestroy 라이프사이클 훅과 비슷하다.
//   - OnStart: 앱 시작 시 호출 -> 서버를 고루틴으로 띄운다
//   - OnStop: 앱 종료 시 호출 -> 서버를 gracefully 종료한다
//
// 매개변수 lc(fx.Lifecycle)와 app(*fiber.App)은 fx가 DI 컨테이너에서 자동 주입한다.
// logger(*zap.Logger)는 newLogger가 생성한 로거로, fx가 자동 주입한다.
//
// fx.Shutdowner는 fx가 자동으로 DI 컨테이너에 등록하는 인터페이스다.
// 고루틴 안에서 서버 에러가 발생했을 때, Shutdown()을 호출하면
// fx의 모든 OnStop 훅이 역순으로 실행되며 앱이 gracefully 종료된다.
// 이것이 fx 공식 문서에서 권장하는 "post-startup 에러 처리" 패턴이다.
func StartServer(lc fx.Lifecycle, shutdowner fx.Shutdowner, app *fiber.App, logger *zap.Logger, cfg *config.Config) {
	// Config 구조체에서 포트를 읽는다.
	// 이전에는 getEnv("PORT", "42001")를 직접 호출했지만,
	// 이제 Config에서 일괄 관리하므로 설정 변경이 한 곳에서만 이루어진다.
	port := cfg.Port

	lc.Append(fx.Hook{
		// OnStart는 앱이 시작될 때 호출된다.
		//
		// 핵심 패턴 (fx 공식 문서 권장): net.Listen()으로 포트를 먼저 확보한 뒤,
		// 고루틴에서 서버를 시작한다.
		//
		// 왜 이렇게 분리하는가?
		//   "go app.Listen()"만 하면, Listen이 고루틴 안에서 실패해도
		//   OnStart는 이미 nil을 반환한 뒤이므로 에러를 알 수 없다.
		//   (fx는 정상 시작된 줄 알고, 실제로는 서버가 안 떠 있는 상태가 된다)
		//
		// net.Listen()은 Go 표준 라이브러리 함수로,
		// 지정한 포트에 TCP 리스너(소켓)를 미리 열어두는 역할이다.
		// Fiber의 app.Listen()이 내부적으로 하는 일을 두 단계로 분리한 것이다:
		//   1단계(동기): 포트 바인딩 — 실패하면 OnStart가 에러를 반환 → fx 앱 시작 중단
		//   2단계(비동기): 요청 수신 — 이미 포트가 열려 있으므로 실패할 일이 거의 없다
		OnStart: func(ctx context.Context) error {
			// (&net.ListenConfig{}).Listen은 net.Listen의 context 지원 버전이다.
			// ctx에 fx.StartTimeout 데드라인이 설정되어 있으므로,
			// 포트 바인딩이 지정 시간 내에 완료되지 않으면 자동 취소된다.
			ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", ":"+port)
			if err != nil {
				return fmt.Errorf("포트 %s 바인딩 실패: %w", port, err)
			}

			// 고루틴(goroutine)은 Go 특유의 경량 스레드 개념이다.
			// NestJS에는 직접 대응하는 개념이 없지만,
			// 메인 흐름을 블로킹하지 않고 병렬로 실행되는 비동기 작업이라고 이해하면 된다.
			//
			// app.Listener()는 이미 열린 리스너를 사용하여 HTTP 요청을 수신한다.
			// 서버가 종료될 때까지 블로킹되므로 반드시 고루틴에서 실행해야 한다.
			// (OnStart 훅은 블로킹하면 안 된다 — fx 공식 규칙)
			go func() {
				if err := app.Listener(ln); err != nil {
					// 서버가 비정상적으로 종료된 경우 fx 앱 전체를 gracefully 종료한다.
					// shutdowner.Shutdown() 호출 시 모든 OnStop 훅이 역순으로 실행된다.
					//
					// zap.Error(err)는 에러를 구조화된 필드로 기록한다.
					// fmt.Printf("%v", err)와 달리 에러 타입, 스택 정보가 JSON 필드로 분리되어
					// 로그 수집 시스템에서 에러별 필터링/검색이 가능하다.
					logger.Error("서버 에러", zap.Error(err))
					if shutdownErr := shutdowner.Shutdown(); shutdownErr != nil {
						logger.Error("shutdown 에러", zap.Error(shutdownErr))
					}
				}
			}()

			// net.Listen이 성공하면 OS 커널이 즉시 TCP 커넥션을 큐잉하기 시작한다.
			// 따라서 별도의 대기(sleep) 없이 바로 return해도 서버는 요청을 받을 준비가 된 상태다.
			logger.Info("서버 시작", zap.String("port", port))
			return nil
		},
		// OnStop은 앱이 종료될 때(Ctrl+C, SIGTERM 등) 호출된다.
		// NestJS의 app.close()와 같은 역할이다.
		//
		// ShutdownWithContext는 Shutdown()과 달리 context를 받아서,
		// 지정된 시간(fx.StopTimeout으로 설정한 30초) 내에 종료되지 않으면
		// 강제로 연결을 끊는다. 이것이 "graceful shutdown"의 핵심이다:
		//   1. 새로운 요청 수신을 중단한다
		//   2. 현재 처리 중인 요청이 완료될 때까지 대기한다
		//   3. 타임아웃(30초)이 지나면 남은 연결을 강제 종료한다
		//
		// ctx는 fx가 전달하는 context로, fx.StopTimeout 만큼의 데드라인이 설정되어 있다.
		OnStop: func(ctx context.Context) error {
			logger.Info("서버 종료 중...")
			return app.ShutdownWithContext(ctx)
		},
	})
}
