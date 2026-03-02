// Package report는 아키텍처 규칙 위반 사항을 표현하고 보고하는 패키지다.
//
// 아키텍처 테스트에서 규칙 위반이 발견되면, 이 패키지의 Violation 구조체로 만들어서
// 어떤 파일의 몇 번째 줄에서, 어떤 규칙을 위반했고, 어떻게 고쳐야 하는지를 담는다.
package report

import (
	"fmt"
	"strings"
)

// Severity는 위반의 심각도를 나타내는 타입이다.
// Go에서는 이렇게 string 기반의 커스텀 타입을 만들어서 "타입 안전성"을 확보한다.
// 예: Severity 타입에는 Error 또는 Warning만 들어갈 수 있다.
type Severity string

const (
	// Error는 반드시 수정해야 하는 심각한 위반이다. 테스트가 실패(FAIL)한다.
	Error Severity = "ERROR"
	// Warning은 권장 사항 위반이다. 테스트 로그에 표시되지만 테스트는 통과(PASS)한다.
	Warning Severity = "WARNING"
)

// Violation은 아키텍처 규칙 위반 하나를 나타내는 구조체다.
//
// 예시 출력:
//
//	[ERROR] dependency/cross-subdomain: subdomain "role" imports subdomain "httpfn" directly
//	  file: internal/domain/app/subdomain/role/svc/token.go:15
//	  fix: use Public Service or move logic to core/
type Violation struct {
	Rule     string   // 위반한 규칙의 이름 (예: "dependency/cross-subdomain")
	Severity Severity // 심각도: Error 또는 Warning
	Message  string   // 위반 내용을 설명하는 메시지
	File     string   // 위반이 발생한 파일 경로
	Line     int      // 위반이 발생한 줄 번호 (0이면 줄 번호 없음)
	Fix      string   // 어떻게 고치면 되는지 안내하는 메시지
}

// String은 Violation을 사람이 읽기 좋은 문자열로 변환한다.
// Go에서 String() 메서드를 정의하면 fmt.Println 등에서 자동으로 호출된다.
// 이를 "Stringer 인터페이스를 구현한다"고 한다.
func (v Violation) String() string {
	// fmt.Sprintf는 포맷 문자열을 사용해서 문자열을 만드는 함수다.
	// %s = 문자열 삽입, %d = 정수 삽입
	s := fmt.Sprintf("[%s] %s: %s\n  file: %s", v.Severity, v.Rule, v.Message, v.File)

	// 줄 번호가 있으면 파일 경로 뒤에 :줄번호 형태로 붙인다.
	if v.Line > 0 {
		s += fmt.Sprintf(":%d", v.Line)
	}

	// 수정 안내가 있으면 추가한다.
	if v.Fix != "" {
		s += fmt.Sprintf("\n  fix: %s", v.Fix)
	}

	return s
}

// Summary는 모든 위반 사항을 종합해서 요약 리포트 문자열을 만든다.
// 에러/경고 개수와 규칙별 위반 횟수를 보여주고, 각 위반 상세 내용을 나열한다.
func Summary(violations []Violation) string {
	// 위반이 없으면 깨끗하다는 메시지를 반환한다.
	if len(violations) == 0 {
		return "No architecture violations found."
	}

	// 에러와 경고 개수를 세고, 규칙별로 몇 건인지 집계한다.
	errors := 0
	warnings := 0
	byRule := make(map[string]int) // map은 키-값 쌍을 저장하는 자료구조 (Python의 dict와 비슷)
	for _, v := range violations {
		if v.Severity == Error {
			errors++
		} else {
			warnings++
		}
		byRule[v.Rule]++ // 해당 규칙의 카운트를 1 증가
	}

	// strings.Builder는 문자열을 효율적으로 이어붙이기 위한 도구다.
	// + 연산자로 문자열을 반복해서 이어붙이면 매번 새 문자열이 생성되어 느리다.
	// Builder를 쓰면 내부 버퍼에 쌓아두고 한 번에 문자열로 변환하므로 빠르다.
	var b strings.Builder
	fmt.Fprintf(&b, "Architecture violations: %d error(s), %d warning(s)\n\n", errors, warnings)

	// 규칙별 위반 횟수 출력
	for rule, count := range byRule {
		fmt.Fprintf(&b, "  %s: %d violation(s)\n", rule, count)
	}
	fmt.Fprintln(&b) // 빈 줄 추가

	// 각 위반의 상세 내용 출력
	for _, v := range violations {
		fmt.Fprintln(&b, v.String())
		fmt.Fprintln(&b) // 위반 사이에 빈 줄 추가
	}

	return b.String()
}

// HasErrors는 위반 목록에 Error 심각도가 하나라도 있으면 true를 반환한다.
// 테스트에서 이 함수가 true를 반환하면 테스트를 실패시킨다.
func HasErrors(violations []Violation) bool {
	for _, v := range violations {
		if v.Severity == Error {
			return true
		}
	}
	return false
}
