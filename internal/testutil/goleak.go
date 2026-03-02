// goleak.go — goleak(goroutine 누수 검출기)의 공용 옵션을 정의한다.
//
// Fiber(fasthttp 기반)를 사용하는 테스트에서는 fasthttp가 내부적으로 생성하는
// goroutine을 goleak이 잡아내므로, 이를 무시 목록에 넣어야 한다.
// 이 파일에서 무시 목록을 한 곳에서 관리하여, 각 테스트 패키지에서 중복 정의를 방지한다.
//
// 사용법 (각 테스트 패키지의 TestMain에서):
//
//	func TestMain(m *testing.M) {
//	    goleak.VerifyTestMain(m, testutil.GoleakOptions()...)
//	}
//
// //go:build e2e 태그가 없으므로 일반 빌드에서도 접근 가능하다.
// E2E 테스트와 유닛 테스트 모두에서 사용할 수 있다.
package testutil

import "go.uber.org/goleak"

// GoleakOptions는 goleak.VerifyTestMain에 전달할 공용 옵션을 반환한다.
//
// 프레임워크가 내부적으로 생성하는 goroutine은 우리 코드의 누수가 아니므로
// 여기서 무시 목록을 관리한다. 라이브러리 버전이 올라가면서 함수 시그니처가
// 바뀌더라도 이 한 곳만 수정하면 된다.
//
// NestJS에서 Jest의 globalSetup에 공통 설정을 넣는 것과 같은 개념이다.
//
// 사용법: goleak.VerifyTestMain(m, testutil.GoleakOptions()...)
//
// 함수로 정의하는 이유:
// Uber 스타일 가이드의 "전역 변수 금지(gochecknoglobals)" 규칙을 준수한다.
// 전역 변수는 의도치 않은 변경(mutation)이 가능하지만,
// 함수는 매번 새 슬라이스를 반환하므로 호출자가 원본을 변경할 수 없다.
func GoleakOptions() []goleak.Option {
	return []goleak.Option{
		// fasthttp.updateServerDate는 HTTP Date 헤더를 1초마다 갱신하는 백그라운드 goroutine이다.
		// Fiber 앱이 생성되면 자동으로 시작되며, 앱 종료 후에도 멈추지 않는다.
		// fasthttp가 종료 API를 제공하지 않아 정리할 방법이 없다.
		// IgnoreAnyFunction은 goroutine 스택 어디에든 해당 함수가 있으면 무시한다.
		goleak.IgnoreAnyFunction("github.com/valyala/fasthttp.updateServerDate.func1"),
	}
}
