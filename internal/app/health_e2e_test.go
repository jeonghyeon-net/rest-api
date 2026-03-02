//go:build e2e

// 이 파일은 헬스체크 엔드포인트(/livez, /readyz)의 E2E 테스트다.
//
// E2E(End-to-End) 테스트는 프로덕션과 거의 동일한 환경에서 실제 HTTP 요청을
// 시뮬레이션하여 전체 흐름이 올바르게 동작하는지 검증한다.
// 유닛 테스트가 개별 함수를 고립시켜 테스트하는 것과 달리,
// E2E 테스트는 DI 그래프, 미들웨어, 라우터, DB 마이그레이션까지 통합 검증한다.
//
// testutil.NewTestApp()으로 프로덕션 DI 그래프를 그대로 사용하되
// DB만 in-memory SQLite로 교체하여 테스트 격리를 보장한다.
//
// NestJS에서 app.e2e-spec.ts 파일로 E2E 테스트를 작성하는 것과 같다:
//
//	describe('HealthCheck (e2e)', () => {
//	  let app: INestApplication;
//	  beforeAll(async () => {
//	    const module = await Test.createTestingModule({
//	      imports: [AppModule],
//	    }).compile();
//	    app = module.createNestApplication();
//	    await app.init();
//	  });
//	  it('/livez (GET)', () => request(app.getHttpServer()).get('/livez').expect(200));
//	});
//
// //go:build e2e 빌드 태그에 의해 일반 테스트(go test)에는 포함되지 않는다.
// go test -tags=e2e로 명시적으로 실행해야 한다.
package app_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/suite"
	"go.uber.org/goleak"

	"rest-api/internal/testutil"
)

// TestMain은 이 패키지의 모든 테스트를 감싸는 진입점이다.
//
// goleak.VerifyTestMain(m)은 모든 테스트가 끝난 후
// 아직 실행 중인 goroutine이 있으면 테스트를 실패시킨다.
// fx, Fiber 등이 내부적으로 goroutine을 생성하므로,
// 테스트 정리(cleanup)가 제대로 되는지 검증하는 데 유용하다.
func TestMain(m *testing.M) {
	// testutil.GoleakOptions에 정의된 공용 무시 목록을 사용한다.
	// fasthttp 등 프레임워크가 생성하는 goroutine은 우리 코드의 누수가 아니므로
	// 한 곳(testutil)에서 관리하여 각 테스트 패키지에서 중복 정의를 방지한다.
	// ... 연산자로 슬라이스를 가변 인자로 전개(spread)한다.
	goleak.VerifyTestMain(m, testutil.GoleakOptions()...)
}

// HealthE2ESuite는 헬스체크 E2E 테스트를 묶는 테스트 스위트다.
//
// testify의 suite.Suite를 임베딩(embedding)하여 테스트 라이프사이클 훅
// (SetupSuite, TearDownSuite 등)과 단언(assertion) 메서드를 상속받는다.
//
// Go의 임베딩은 NestJS/TypeScript의 extends와 비슷하다:
//
//	class HealthE2ESuite extends suite.Suite { ... }
//
// 스위트 패턴의 장점:
//   - SetupSuite()에서 앱을 한 번만 초기화하고 모든 테스트가 공유
//   - 테스트 간 상태(app, db)를 구조체 필드로 깔끔하게 관리
//   - s.Require().NoError() 같은 체이닝 단언으로 가독성 향상
//
// NestJS의 describe() 블록 안에서 beforeAll()로 앱을 초기화하고
// 여러 it()에서 공유하는 패턴과 같다.
type HealthE2ESuite struct {
	suite.Suite

	// app은 테스트용 Fiber 앱 인스턴스다.
	// 프로덕션과 동일한 미들웨어, 라우터, 에러 핸들러가 설정되어 있다.
	app *fiber.App

	// db는 in-memory SQLite DB다.
	// 헬스체크 테스트에서는 직접 사용하지 않지만,
	// NewTestApp의 반환값을 받아두어 DB 관련 테스트에서 활용할 수 있다.
	db *sql.DB
}

// SetupSuite는 스위트의 모든 테스트가 실행되기 전에 한 번 호출된다.
// NestJS의 beforeAll()과 같은 역할이다.
//
// testutil.NewTestApp()이 프로덕션 DI 그래프를 구성하고,
// t.Cleanup()으로 정리 함수를 자동 등록하므로
// TearDownSuite()를 별도로 구현할 필요가 없다.
func (s *HealthE2ESuite) SetupSuite() {
	s.app, s.db = testutil.NewTestApp(s.T())
}

// TestHealthEndpoints는 헬스체크 엔드포인트를 테이블 주도 테스트로 검증한다.
//
// 테이블 주도 테스트(table-driven test)는 Go에서 가장 널리 쓰이는 테스트 패턴이다.
// 테스트 케이스를 구조체 슬라이스로 정의하고, 반복문으로 각 케이스를 실행한다.
//
// NestJS/Jest의 it.each()와 같은 개념이다:
//
//	it.each([
//	  ['/livez', 200],
//	  ['/readyz', 200],
//	])('GET %s should return %d', (path, status) => { ... });
//
// 장점:
//   - 새 엔드포인트 추가 시 구조체만 추가하면 된다 (테스트 로직 변경 불필요)
//   - 각 케이스가 t.Run()의 서브테스트로 실행되어 개별 실패 추적이 가능하다
//   - go test -run TestHealthE2E/TestHealthEndpoints/livez 로 특정 케이스만 실행 가능
func (s *HealthE2ESuite) TestHealthEndpoints() {
	tests := []struct {
		name       string // 서브테스트 이름 — 실패 시 어떤 엔드포인트인지 식별
		method     string // HTTP 메서드 (GET, POST 등)
		path       string // 요청 경로
		wantStatus int    // 기대하는 HTTP 상태 코드
	}{
		// /livez — Liveness Probe
		// 프로세스가 살아있는지 확인하는 쿠버네티스 표준 엔드포인트다.
		// 외부 의존성(DB 등) 장애와 무관하게 항상 200 OK를 반환해야 한다.
		{
			name:       "livez",
			method:     http.MethodGet,
			path:       "/livez",
			wantStatus: http.StatusOK,
		},
		// /readyz — Readiness Probe
		// 애플리케이션이 트래픽을 받을 준비가 되었는지 확인한다.
		// 내부적으로 DB에 Ping을 보내 연결 상태를 확인한다.
		{
			name:       "readyz",
			method:     http.MethodGet,
			path:       "/readyz",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// httptest.NewRequest()는 Go 표준 라이브러리의 테스트용 HTTP 요청 생성 함수다.
			// 실제 네트워크를 거치지 않고 Fiber 앱에 직접 요청을 주입한다.
			// NestJS에서 supertest의 request(app.getHttpServer()).get(path)와 같다.
			req := httptest.NewRequest(tt.method, tt.path, nil)

			// app.Test()는 Fiber의 테스트 헬퍼로, 실제 서버를 띄우지 않고
			// 내부적으로 요청을 처리하여 응답을 반환한다.
			resp, err := s.app.Test(req)
			s.Require().NoError(err)
			defer resp.Body.Close()

			s.Equal(tt.wantStatus, resp.StatusCode)
		})
	}
}

// TestHealthE2E는 Go 테스트 러너의 진입점이다.
//
// Go의 테스트 러너는 Test로 시작하는 함수만 인식한다.
// testify의 suite.Run()은 스위트 구조체의 모든 Test* 메서드를 찾아
// 자동으로 실행한다. SetupSuite → Test* 메서드들 → TearDownSuite 순서로 실행된다.
//
// NestJS에서 describe('HealthCheck')가 여러 it()을 묶는 것과 같다.
func TestHealthE2E(t *testing.T) {
	suite.Run(t, new(HealthE2ESuite))
}
