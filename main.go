package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/bytedance/sonic"
	"github.com/gofiber/fiber/v3"
	"github.com/joho/godotenv"
	"go.uber.org/fx"
)

// main은 애플리케이션의 진입점이다.
// fx.New()로 DI 컨테이너를 생성하고, Run()으로 애플리케이션을 시작한다.
//
// NestJS와 비교하면:
//   - fx.New()는 NestApp.create()와 같은 역할
//   - fx.Provide()는 @Module()의 providers 배열과 같은 역할
//   - fx.Invoke()는 모듈이 로드된 후 실행되는 onModuleInit()과 유사
//   - Run()은 app.listen()과 같은 역할 (graceful shutdown 포함)
func main() {
	// .env 파일에서 환경변수를 로드한다.
	// NestJS의 ConfigModule.forRoot()와 비슷한 역할이다.
	//
	// godotenv.Load()는 .env 파일이 없으면 에러를 반환하지만,
	// 운영 환경에서는 .env 파일 없이 실제 환경변수를 사용하므로 에러를 무시한다.
	// (Docker, K8s 등에서 환경변수를 직접 주입하는 것이 일반적)
	_ = godotenv.Load()

	fx.New(
		// newFiberApp 함수를 DI 컨테이너에 등록한다.
		// fx는 이 함수의 반환 타입(*fiber.App)을 보고,
		// 다른 곳에서 *fiber.App을 요청하면 이 함수를 호출해서 주입한다.
		fx.Provide(newFiberApp),

		// startServer 함수를 호출한다.
		// fx.Invoke()에 등록된 함수는 앱 시작 시 자동 실행된다.
		// 매개변수(fx.Lifecycle, *fiber.App)는 DI 컨테이너에서 자동으로 주입받는다.
		fx.Invoke(startServer),
	).Run()
}

// newFiberApp은 Fiber 애플리케이션 인스턴스를 생성하고 라우트를 등록한다.
// fx.Provide()에 의해 DI 컨테이너에 등록되며,
// *fiber.App 타입이 필요한 곳에 자동으로 주입된다.
//
// NestJS에서 Express/Fastify 인스턴스를 설정하는 것과 비슷하다.
func newFiberApp() *fiber.App {
	// fiber.New()로 새로운 Fiber 앱을 생성한다.
	// NestJS의 NestFactory.create()에서 내부적으로 Express 인스턴스를 만드는 것과 같다.
	//
	// sonic은 ByteDance(틱톡)가 만든 초고속 JSON 라이브러리다.
	// Go 표준 encoding/json 대비 2~3배 빠른 직렬화/역직렬화 성능을 제공한다.
	// JIT(Just-In-Time) 컴파일을 사용하여 런타임에 최적화된 코덱을 생성한다.
	// Fiber의 JSONEncoder/JSONDecoder를 교체하면 c.JSON(), c.BodyParser() 등
	// 모든 JSON 처리에 sonic이 사용된다.
	app := fiber.New(fiber.Config{
		JSONEncoder: sonic.Marshal,
		JSONDecoder: sonic.Unmarshal,
	})

	// 헬스체크 라우트: 서버가 정상 동작하는지 확인하는 엔드포인트
	// NestJS에서 @Get('/') @HealthCheck() 데코레이터를 사용하는 것과 유사하다.
	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("OK")
	})

	return app
}

// getEnv는 환경변수를 조회하고, 없으면 기본값을 반환하는 헬퍼 함수다.
// NestJS의 configService.get('KEY', 'default')와 같은 역할이다.
//
// os.LookupEnv는 Go 표준 라이브러리 함수로, (값, 존재여부) 두 값을 반환한다.
// Go에서는 이렇게 두 번째 반환값으로 "값이 있는지 없는지"를 알려주는 패턴이 매우 흔하다.
// (map 조회, 타입 단언 등에서도 동일한 패턴 사용)
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// startServer는 fx.Lifecycle 훅을 사용하여 서버의 시작과 종료를 관리한다.
//
// fx.Lifecycle은 NestJS의 OnModuleInit / OnModuleDestroy 라이프사이클 훅과 비슷하다.
//   - OnStart: 앱 시작 시 호출 → 서버를 고루틴으로 띄운다
//   - OnStop: 앱 종료 시 호출 → 서버를 gracefully 종료한다
//
// 매개변수 lc(fx.Lifecycle)와 app(*fiber.App)은 fx가 DI 컨테이너에서 자동 주입한다.
//
// fx.Shutdowner는 fx가 자동으로 DI 컨테이너에 등록하는 인터페이스다.
// 고루틴 안에서 서버 에러가 발생했을 때, Shutdown()을 호출하면
// fx의 모든 OnStop 훅이 역순으로 실행되며 앱이 gracefully 종료된다.
// 이것이 fx 공식 문서에서 권장하는 "post-startup 에러 처리" 패턴이다.
func startServer(lc fx.Lifecycle, shutdowner fx.Shutdowner, app *fiber.App) {
	// 환경변수에서 포트를 읽는다. 없으면 기본값 3000을 사용한다.
	port := getEnv("PORT", "3000")

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
			ln, err := net.Listen("tcp", ":"+port)
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
					fmt.Printf("서버 에러: %v\n", err)
					shutdowner.Shutdown() //nolint:errcheck
				}
			}()

			// net.Listen이 성공하면 OS 커널이 즉시 TCP 커넥션을 큐잉하기 시작한다.
			// 따라서 별도의 대기(sleep) 없이 바로 return해도 서버는 요청을 받을 준비가 된 상태다.
			return nil
		},
		// OnStop은 앱이 종료될 때(Ctrl+C, SIGTERM 등) 호출된다.
		// Fiber의 Shutdown()은 현재 처리 중인 요청을 완료한 후 서버를 종료한다.
		// NestJS의 app.close()와 같은 역할이다.
		OnStop: func(ctx context.Context) error {
			return app.Shutdown()
		},
	})
}
