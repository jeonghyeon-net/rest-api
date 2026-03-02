package ruleset

// 이 파일은 "네이밍 규칙"을 검사한다.
//
// 좋은 이름은 코드 가독성의 핵심이다. 특히 Go에서는 패키지명이 타입/함수 앞에 붙기 때문에
// 네이밍 규칙이 더 중요하다.
//
// 검사하는 3가지 규칙:
//
// 1. 금지된 패키지명 (forbidden-package)
//    - util, common, misc, helper 같은 "아무거나 넣는" 패키지명 금지
//    - 이런 패키지는 책임이 불분명해서 점점 비대해진다 (God Object 패턴)
//
// 2. 패키지 스터터 (package-stutter)
//    - 타입 이름에 패키지 이름이 반복되는 것을 감지한다.
//    - 예: repo.AppRepo → 호출시 repo.AppRepo로 "repo"가 두 번 나옴
//    - 수정: repo.App → 호출시 repo.App으로 깔끔
//
// 3. Impl 접미사 금지 (impl-suffix)
//    - 타입 이름에 "Impl"을 붙이지 않는다.
//    - 구현체는 소문자로 시작하는 비공개(unexported) 이름을 사용한다.
//    - 예: UserServiceImpl ✗ → userService ✓

import (
	"fmt"
	"strings"

	"rest-api/test/architecture/analyzer"
	"rest-api/test/architecture/report"
)

// CheckNaming은 모든 파일의 패키지명과 타입명이 네이밍 규칙을 지키는지 검사한다.
func CheckNaming(files []*analyzer.FileInfo, cfg *Config) []report.Violation {
	var violations []report.Violation

	for _, f := range files {
		// 규칙 1: 금지된 패키지명 검사
		if v := checkForbiddenPackageName(f); v != nil {
			violations = append(violations, *v)
		}

		// 파일 안의 각 타입에 대해 검사
		for _, t := range f.Types {
			// 규칙 2: 패키지 스터터 검사
			if v := checkPackageStutter(f, t); v != nil {
				violations = append(violations, *v)
			}
			// 규칙 3: Impl 접미사 검사
			if v := checkImplSuffix(f, t); v != nil {
				violations = append(violations, *v)
			}
		}
	}

	return violations
}

// ──────────────────────────────────────────────
// 규칙 1: 금지된 패키지명
// ──────────────────────────────────────────────

// checkForbiddenPackageName은 패키지명이 금지 목록에 있는지 검사한다.
//
// 금지 목록: util, utils, common, misc, helper, helpers
//
// 이런 패키지명이 나쁜 이유:
//   - "util"에 뭘 넣을까? → 아무거나 다 넣게 됨 → 패키지가 비대해짐
//   - 대안: 구체적인 이름 사용. 예: "util" 대신 "timeformat", "validator" 등
func checkForbiddenPackageName(f *analyzer.FileInfo) *report.Violation {
	for _, forbidden := range ForbiddenPackageNames {
		// strings.EqualFold: 대소문자 구분 없이 비교. "UTIL" == "util" → true
		if strings.EqualFold(f.Package, forbidden) {
			return &report.Violation{
				Rule:     "naming/forbidden-package",
				Severity: report.Error,
				Message:  fmt.Sprintf("package name %q is forbidden", f.Package),
				File:     f.Path,
				Fix:      "rename package to something more specific and descriptive",
			}
		}
	}
	return nil
}

// ──────────────────────────────────────────────
// 규칙 2: 패키지 스터터
// ──────────────────────────────────────────────

// checkPackageStutter는 타입 이름에 패키지 이름이 반복되는 것을 감지한다.
//
// Go에서 외부 패키지의 타입을 사용할 때는 "패키지명.타입명" 형태로 쓴다.
// 예: repo.AppRepo → "repo"가 두 번 나와서 읽기 거슬림 (이것을 "stutter"라 함)
// 수정: repo.App → "repo"가 한 번만 나와서 깔끔
//
// 접두사 스터터: package "repo", type "RepoManager" → repo.RepoManager ✗ → repo.Manager ✓
// 접미사 스터터: package "repo", type "AppRepo" → repo.AppRepo ✗ → repo.App ✓
func checkPackageStutter(f *analyzer.FileInfo, t analyzer.TypeInfo) *report.Violation {
	// 비공개 타입(소문자 시작)은 외부에서 "패키지명.타입명"으로 안 쓰므로 검사 불필요
	if !t.IsExported {
		return nil
	}

	pkg := strings.ToLower(f.Package) // 대소문자 무시 비교를 위해 소문자로 변환
	nameLower := strings.ToLower(t.Name)

	// 접두사 스터터 검사: 타입 이름이 패키지 이름으로 시작하는 경우
	// 예: package "repo", type "RepoManager" → "repomanager"가 "repo"로 시작
	if len(t.Name) > len(pkg) && strings.HasPrefix(nameLower, pkg) {
		suggested := t.Name[len(pkg):] // "RepoManager" → "Manager"
		return &report.Violation{
			Rule:     "naming/package-stutter",
			Severity: report.Warning,
			Message:  fmt.Sprintf("type %q stutters with package name %q (%s.%s)", t.Name, f.Package, f.Package, t.Name),
			File:     f.Path,
			Line:     t.Line,
			Fix:      fmt.Sprintf("rename to %q (callers use %s.%s)", suggested, f.Package, suggested),
		}
	}

	// 접미사 스터터 검사: 타입 이름이 패키지 이름으로 끝나는 경우
	// 예: package "repo", type "AppRepo" → "apprepo"가 "repo"로 끝남
	if len(t.Name) > len(pkg) && strings.HasSuffix(nameLower, pkg) {
		suggested := t.Name[:len(t.Name)-len(pkg)] // "AppRepo" → "App"
		return &report.Violation{
			Rule:     "naming/package-stutter",
			Severity: report.Warning,
			Message:  fmt.Sprintf("type %q stutters with package name %q (%s.%s)", t.Name, f.Package, f.Package, t.Name),
			File:     f.Path,
			Line:     t.Line,
			Fix:      fmt.Sprintf("rename to %q (callers use %s.%s)", suggested, f.Package, suggested),
		}
	}

	return nil
}

// ──────────────────────────────────────────────
// 규칙 3: Impl 접미사 금지
// ──────────────────────────────────────────────

// checkImplSuffix는 타입 이름이 "Impl"로 끝나는지 검사한다.
//
// "Impl" 접미사가 나쁜 이유:
//   - 인터페이스의 구현체는 당연히 "Impl"인데, 이름에 또 쓸 필요가 없다.
//   - Go에서는 구현체를 소문자로 시작하는 비공개 타입으로 만드는 것이 관례다.
//
// 나쁜 예: type UserServiceImpl struct{} ← 공개 + Impl 접미사
// 좋은 예: type userService struct{}    ← 비공개 (소문자 시작)
func checkImplSuffix(f *analyzer.FileInfo, t analyzer.TypeInfo) *report.Violation {
	if strings.HasSuffix(t.Name, "Impl") {
		return &report.Violation{
			Rule:     "naming/impl-suffix",
			Severity: report.Error,
			Message:  fmt.Sprintf("type %q has forbidden 'Impl' suffix", t.Name),
			File:     f.Path,
			Line:     t.Line,
			Fix:      "use an unexported name for the implementation struct",
		}
	}
	return nil
}
