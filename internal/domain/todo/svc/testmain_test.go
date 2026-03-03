//go:build unit

// testmain_test.go — 이 패키지의 모든 테스트를 감싸는 진입점이다.
// goleak.VerifyTestMain으로 goroutine 누수를 검증한다.
//
// 아키텍처 규칙(testing/missing-goleak)에 의해 테스트 파일이 있는
// 모든 패키지에 TestMain + goleak.VerifyTestMain이 필수다.
package svc

import (
	"testing"

	"go.uber.org/goleak"

	"rest-api/internal/testutil"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, testutil.GoleakOptions()...)
}
