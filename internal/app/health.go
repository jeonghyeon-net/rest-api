package app

import (
	"database/sql"

	"github.com/gofiber/fiber/v3"
)

// registerHealthRoutes는 쿠버네티스 스타일의 헬스체크 엔드포인트를 등록한다.
//
// 두 가지 엔드포인트를 제공한다:
//
//   - GET /livez (Liveness Probe)
//     프로세스가 살아있는지 확인한다. 응답이 없으면 프로세스가 데드락 상태이므로
//     쿠버네티스가 파드를 재시작한다.
//     NestJS에서 @HealthCheck() 데코레이터로 프로세스 생존을 확인하는 것과 같다.
//
//   - GET /readyz (Readiness Probe)
//     애플리케이션이 트래픽을 받을 준비가 되었는지 확인한다.
//     DB 연결 등 의존성이 정상이면 200, 아니면 503을 반환한다.
//     503이면 쿠버네티스가 이 파드를 서비스 엔드포인트에서 제거하여
//     트래픽이 다른 정상 파드로 라우팅된다 (파드를 재시작하지는 않는다).
//     NestJS에서 TypeOrmHealthIndicator로 DB 연결을 확인하는 것과 같다.
//
// 매개변수:
//   - app: Fiber 앱 인스턴스. 라우트를 등록할 대상이다.
//   - database: *sql.DB. readyz에서 DB 연결 상태를 확인하기 위해 필요하다.
func registerHealthRoutes(app *fiber.App, database *sql.DB) {
	// ─── Liveness Probe ─────────────────────────────────────────────────
	// 가장 가볍게 구현한다. HTTP 응답을 보낼 수 있다는 것 자체가
	// 프로세스가 살아있고 이벤트 루프(Go의 경우 고루틴 스케줄러)가
	// 정상 동작한다는 증거다.
	//
	// fiber.StatusOK는 HTTP 200 상태코드 상수다.
	// Go에서는 매직 넘버(200) 대신 이름이 있는 상수를 사용하는 것이 관례다.
	app.Get("/livez", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	// ─── Readiness Probe ────────────────────────────────────────────────
	// DB에 Ping을 보내서 실제로 쿼리가 가능한 상태인지 확인한다.
	//
	// db.PingContext()는 DB 연결 풀에서 연결 하나를 꺼내 서버에 핑을 보내고 반환한다.
	// context를 받으므로 요청 타임아웃이 자동으로 적용된다.
	//
	// 실패하는 경우:
	//   - DB 파일이 손상된 경우
	//   - 디스크가 가득 찬 경우
	//   - 연결 풀이 고갈된 경우 (MaxOpenConns 초과)
	//
	// fiber.StatusServiceUnavailable는 HTTP 503 상태코드다.
	// 503은 "서버가 일시적으로 요청을 처리할 수 없다"는 의미로,
	// 쿠버네티스가 이 파드를 서비스에서 일시 제외하는 트리거가 된다.
	app.Get("/readyz", func(c fiber.Ctx) error {
		if err := database.PingContext(c.Context()); err != nil {
			return c.SendStatus(fiber.StatusServiceUnavailable)
		}
		return c.SendStatus(fiber.StatusOK)
	})
}
