package ruleset

// 이 파일은 "인터페이스 패턴 규칙"을 검사한다.
//
// DDD에서 인터페이스는 의존성 역전(Dependency Inversion)의 핵심이다.
// NestJS에서 @Injectable()로 DI하는 것과 비슷한 개념인데,
// Go에서는 데코레이터 대신 "인터페이스"를 사용해서 의존성을 느슨하게 만든다.
//
// Go의 인터페이스 패턴 규칙 (채널톡 컨벤션):
//
// 1. 공개(exported) 인터페이스 + 비공개(unexported) 구현체가 같은 파일에 있어야 한다.
//    예시:
//      type Repository interface { ... }  ← 공개 (대문자 시작)
//      type repository struct { ... }     ← 비공개 (소문자 시작)
//
//    NestJS 비유: interface + @Injectable() class가 같은 파일에 있는 것과 비슷
//
// 2. 생성자(New*) 함수는 구체 타입이 아닌 인터페이스를 반환해야 한다.
//    예시:
//      func New() Repository { return &repository{} }  ← ✓ 인터페이스 반환
//      func New() *repository { return &repository{} }  ← ✗ 구체 타입 반환
//
//    NestJS 비유: 프로바이더를 등록할 때 구체 클래스가 아닌 토큰(인터페이스)으로 등록하는 것과 비슷
//
// 3. 구현체 struct는 외부에 노출하지 않는다 (unexported).
//    외부에서는 인터페이스 타입만 알면 되고, 구현 세부사항은 몰라도 된다.

import (
	"fmt"
	"strings"
	"unicode"

	"rest-api/test/architecture/analyzer"
	"rest-api/test/architecture/report"
)

// CheckInterfacePatterns은 모든 파일의 인터페이스 패턴이 규칙을 지키는지 검사한다.
//
// 검사 대상은 "같은 파일 내"의 인터페이스, 구조체, 함수 관계다.
// 파일 단위로 검사하는 이유: Go에서 인터페이스와 그 구현체는 같은 파일에 두는 것이 관례다.
func CheckInterfacePatterns(files []*analyzer.FileInfo, cfg *Config) []report.Violation {
	var violations []report.Violation

	for _, f := range files {
		// 파일 안의 타입들을 인터페이스와 구조체로 분류한다
		var interfaces []analyzer.TypeInfo // 인터페이스 타입 목록
		var structs []analyzer.TypeInfo    // 구조체(struct) 타입 목록

		for _, t := range f.Types {
			if t.IsInterface {
				interfaces = append(interfaces, t)
			} else {
				structs = append(structs, t)
			}
		}

		// 규칙 1: 공개 인터페이스에 대응하는 비공개 구현체가 있는지 검사
		for _, iface := range interfaces {
			if !iface.IsExported {
				continue // 비공개 인터페이스는 검사 대상이 아님
			}
			if v := checkMissingImpl(f, iface, structs); v != nil {
				violations = append(violations, *v)
			}
		}

		// 규칙 2: 생성자(New*) 함수가 인터페이스를 반환하는지 검사
		for _, fn := range f.Functions {
			if v := checkConstructorReturn(f, fn, interfaces); v != nil {
				violations = append(violations, *v)
			}
		}

		// 규칙 3: 구현체 struct가 공개(exported)되어 있지 않은지 검사
		for _, s := range structs {
			if s.IsExported {
				if v := checkExportedImpl(f, s, interfaces); v != nil {
					violations = append(violations, *v)
				}
			}
		}
	}

	return violations
}

// ──────────────────────────────────────────────
// 규칙 1: 공개 인터페이스에 비공개 구현체가 있어야 한다
// ──────────────────────────────────────────────

// checkMissingImpl은 공개 인터페이스가 있을 때, 같은 파일에 비공개 구현체가 있는지 확인한다.
//
// 예시 (정상):
//
//	type Repository interface { ... }  ← 공개 인터페이스
//	type repository struct { ... }     ← 비공개 구현체 (있으니까 OK)
//
// 예시 (위반):
//
//	type Repository interface { ... }  ← 공개 인터페이스만 덩그러니
//	                                      (구현체가 없으니까 WARNING)
func checkMissingImpl(f *analyzer.FileInfo, iface analyzer.TypeInfo, structs []analyzer.TypeInfo) *report.Violation {
	// 같은 파일에 비공개 구조체가 하나라도 있으면 OK
	// (실제로 그 구조체가 인터페이스를 구현하는지까지는 AST만으로 확인이 어렵다)
	for _, s := range structs {
		if !s.IsExported {
			return nil // 비공개 구조체가 있다 = 구현체가 있다고 간주
		}
	}

	return &report.Violation{
		Rule:     "interface/missing-impl",
		Severity: report.Warning,
		Message:  fmt.Sprintf("exported interface %q has no unexported implementation in the same file", iface.Name),
		File:     f.Path,
		Line:     iface.Line,
		Fix:      fmt.Sprintf("add unexported struct %q implementing %s in the same file", toLowerFirst(iface.Name), iface.Name),
	}
}

// ──────────────────────────────────────────────
// 규칙 2: 생성자는 인터페이스를 반환해야 한다
// ──────────────────────────────────────────────

// checkConstructorReturn은 New* 함수가 인터페이스를 반환하는지 검사한다.
//
// "생성자"의 조건:
//   - 이름이 "New"로 시작 (Go의 생성자 컨벤션)
//   - 공개 함수 (대문자 시작)
//   - 리시버가 없는 일반 함수 (메서드가 아님)
//
// 좋은 예: func New() Repository { return &repository{} }  ← 인터페이스 반환 ✓
// 나쁜 예: func New() *repository { return &repository{} }  ← 구체 타입 반환 ✗
//
// NestJS 비유: 모듈에서 useClass 대신 useFactory로 인터페이스 토큰을 반환하는 것과 비슷
func checkConstructorReturn(f *analyzer.FileInfo, fn analyzer.FuncInfo, interfaces []analyzer.TypeInfo) *report.Violation {
	// 생성자 조건에 안 맞으면 검사 대상이 아님
	if !fn.IsExported || !strings.HasPrefix(fn.Name, "New") || fn.Receiver != "" {
		return nil
	}
	// 반환 타입이 없거나, 파일에 인터페이스가 없으면 검사할 수 없음
	if len(fn.ReturnTypes) == 0 || len(interfaces) == 0 {
		return nil
	}

	// 첫 번째 반환 타입을 확인한다
	// 포인터(*)가 붙어 있으면 제거해서 타입 이름만 남긴다
	firstReturn := strings.TrimPrefix(fn.ReturnTypes[0], "*")

	// 이미 인터페이스를 반환하고 있으면 OK
	for _, iface := range interfaces {
		if iface.Name == firstReturn {
			return nil // 정상: 인터페이스를 반환하고 있음
		}
	}

	// 구체 타입을 반환하는데, 같은 파일에 대응하는 인터페이스가 있으면 위반
	// 예: 인터페이스 "Repository"가 있는데, 생성자가 "repository"(소문자)를 반환하는 경우
	for _, iface := range interfaces {
		if !iface.IsExported {
			continue
		}
		// toLowerFirst: "Repository" → "repository"
		// 생성자의 반환 타입이 인터페이스의 소문자 버전과 같은지 비교
		if strings.EqualFold(toLowerFirst(iface.Name), firstReturn) {
			return &report.Violation{
				Rule:     "interface/constructor-return",
				Severity: report.Error,
				Message:  fmt.Sprintf("constructor %q returns concrete type %q instead of interface %q", fn.Name, firstReturn, iface.Name),
				File:     f.Path,
				Line:     fn.Line,
				Fix:      fmt.Sprintf("change return type to %s", iface.Name),
			}
		}
	}

	return nil
}

// ──────────────────────────────────────────────
// 규칙 3: 구현체 struct는 외부에 노출하지 않는다
// ──────────────────────────────────────────────

// checkExportedImpl은 공개된 struct가 인터페이스의 구현체처럼 보이는지 감지한다.
//
// 감지 패턴:
//   - {인터페이스명}Impl 형태: RepositoryImpl ✗
//   - Default{인터페이스명} 형태: DefaultRepository ✗
//
// 수정 방법: 소문자로 시작하는 비공개 이름 사용
//   - RepositoryImpl → repository
//   - DefaultRepository → repository
func checkExportedImpl(f *analyzer.FileInfo, s analyzer.TypeInfo, interfaces []analyzer.TypeInfo) *report.Violation {
	for _, iface := range interfaces {
		if !iface.IsExported {
			continue
		}
		// "인터페이스명 + Impl" 또는 "Default + 인터페이스명" 패턴 감지
		if s.Name == iface.Name+"Impl" || s.Name == "Default"+iface.Name {
			return &report.Violation{
				Rule:     "interface/exported-impl",
				Severity: report.Warning,
				Message:  fmt.Sprintf("struct %q appears to implement %q but is exported", s.Name, iface.Name),
				File:     f.Path,
				Line:     s.Line,
				Fix:      fmt.Sprintf("make the implementation struct unexported: %q", toLowerFirst(s.Name)),
			}
		}
	}
	return nil
}

// ──────────────────────────────────────────────
// 유틸리티 함수
// ──────────────────────────────────────────────

// toLowerFirst는 문자열의 첫 글자를 소문자로 바꾼다.
// Go에서 공개 이름(exported)을 비공개(unexported)로 바꿀 때 사용한다.
// 예: "Repository" → "repository", "UserService" → "userService"
//
// rune이란?
//
//	Go에서 하나의 유니코드 문자를 나타내는 타입이다.
//	한글, 이모지 같은 멀티바이트 문자도 하나의 rune으로 표현된다.
//	string은 byte의 배열이지만, rune은 문자 단위로 처리할 수 있다.
func toLowerFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)                   // 문자열을 유니코드 문자(rune) 배열로 변환
	runes[0] = unicode.ToLower(runes[0]) // 첫 번째 문자를 소문자로 변환
	return string(runes)                 // 다시 문자열로 변환
}
