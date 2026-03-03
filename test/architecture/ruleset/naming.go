package ruleset

// 이 파일은 "네이밍 규칙"을 검사한다.
//
// 좋은 이름은 코드 가독성의 핵심이다. 특히 Go에서는 패키지명이 타입/함수 앞에 붙기 때문에
// 네이밍 규칙이 더 중요하다.
//
// 검사하는 5가지 규칙:
//
// 1. 금지된 패키지명 (forbidden-package)
//    - util, common, misc, helper, shared, lib 같은 "아무거나 넣는" 패키지명 금지
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
//
// 4. 파일명-인터페이스명 일치 (file-interface-match)
//    - svc/, repo/ 파일에 공개 인터페이스가 있으면 파일명과 일치해야 한다.
//    - 예: install.go → Install 인터페이스가 있어야 한다.
//
// 5. 레이어 접미사 파일명 금지 (layer-suffix-filename)
//    - 파일명에 레이어명을 접미사로 붙이지 않는다.
//    - 예: install_svc.go ✗ → install.go ✓ (디렉토리가 이미 레이어를 표현)

import (
	"fmt"
	"path/filepath"
	"strings"

	"rest-api/test/architecture/analyzer"
	"rest-api/test/architecture/report"
)

// CheckNaming은 모든 파일의 패키지명과 타입명이 네이밍 규칙을 지키는지 검사한다.
func CheckNaming(files []*analyzer.FileInfo, cfg *Config) []report.Violation {
	var violations []report.Violation

	for _, file := range files {
		// 규칙 1: 금지된 패키지명 검사
		if v := checkForbiddenPackageName(file); v != nil {
			violations = append(violations, *v)
		}

		// 파일 안의 각 타입에 대해 검사
		for _, t := range file.Types {
			// 규칙 2: 패키지 스터터 검사
			if v := checkPackageStutter(file, t); v != nil {
				violations = append(violations, *v)
			}
			// 규칙 3: Impl 접미사 검사
			if v := checkImplSuffix(file, t); v != nil {
				violations = append(violations, *v)
			}
		}

		// 파일 경로 기반 규칙은 도메인 경로 정보가 필요하다
		relPath, err := filepath.Rel(cfg.ProjectRoot, file.Path)
		if err != nil {
			continue
		}
		dp := analyzer.ParseDomainPath(relPath)
		if dp != nil {
			// 규칙 4: 파일명-인터페이스명 일치 검사
			if v := checkFileInterfaceMatch(file, dp); v != nil {
				violations = append(violations, *v)
			}
			// 규칙 5: 레이어 접미사 파일명 검사
			if v := checkLayerSuffixFilename(file, dp); v != nil {
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
func checkForbiddenPackageName(file *analyzer.FileInfo) *report.Violation {
	for _, forbidden := range ForbiddenPackageNames {
		// strings.EqualFold: 대소문자 구분 없이 비교. "UTIL" == "util" → true
		if strings.EqualFold(file.Package, forbidden) {
			return &report.Violation{
				Rule:     "naming/forbidden-package",
				Severity: report.Error,
				Message:  fmt.Sprintf("package name %q is forbidden", file.Package),
				File:     file.Path,
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
func checkPackageStutter(file *analyzer.FileInfo, typeInfo analyzer.TypeInfo) *report.Violation {
	// 비공개 타입(소문자 시작)은 외부에서 "패키지명.타입명"으로 안 쓰므로 검사 불필요
	if !typeInfo.IsExported {
		return nil
	}

	pkg := strings.ToLower(file.Package) // 대소문자 무시 비교를 위해 소문자로 변환
	nameLower := strings.ToLower(typeInfo.Name)

	// 접두사 스터터 검사: 타입 이름이 패키지 이름으로 시작하는 경우
	// 예: package "repo", type "RepoManager" → "repomanager"가 "repo"로 시작
	if len(typeInfo.Name) > len(pkg) && strings.HasPrefix(nameLower, pkg) {
		suggested := typeInfo.Name[len(pkg):] // "RepoManager" → "Manager"
		return &report.Violation{
			Rule:     "naming/package-stutter",
			Severity: report.Warning,
			Message:  fmt.Sprintf("type %q stutters with package name %q (%s.%s)", typeInfo.Name, file.Package, file.Package, typeInfo.Name),
			File:     file.Path,
			Line:     typeInfo.Line,
			Fix:      fmt.Sprintf("rename to %q (callers use %s.%s)", suggested, file.Package, suggested),
		}
	}

	// 접미사 스터터 검사: 타입 이름이 패키지 이름으로 끝나는 경우
	// 예: package "repo", type "AppRepo" → "apprepo"가 "repo"로 끝남
	if len(typeInfo.Name) > len(pkg) && strings.HasSuffix(nameLower, pkg) {
		suggested := typeInfo.Name[:len(typeInfo.Name)-len(pkg)] // "AppRepo" → "App"
		return &report.Violation{
			Rule:     "naming/package-stutter",
			Severity: report.Warning,
			Message:  fmt.Sprintf("type %q stutters with package name %q (%s.%s)", typeInfo.Name, file.Package, file.Package, typeInfo.Name),
			File:     file.Path,
			Line:     typeInfo.Line,
			Fix:      fmt.Sprintf("rename to %q (callers use %s.%s)", suggested, file.Package, suggested),
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
func checkImplSuffix(file *analyzer.FileInfo, typeInfo analyzer.TypeInfo) *report.Violation {
	if strings.HasSuffix(typeInfo.Name, "Impl") {
		return &report.Violation{
			Rule:     "naming/impl-suffix",
			Severity: report.Error,
			Message:  fmt.Sprintf("type %q has forbidden 'Impl' suffix", typeInfo.Name),
			File:     file.Path,
			Line:     typeInfo.Line,
			Fix:      "use an unexported name for the implementation struct",
		}
	}
	return nil
}

// ──────────────────────────────────────────────
// 규칙 4: 파일명-인터페이스명 일치
// ──────────────────────────────────────────────

// checkFileInterfaceMatch는 파일에 공개 인터페이스가 있을 때
// 파일명과 인터페이스명이 일치하는지 검사한다.
//
// svc/와 repo/ 레이어에서만 적용된다 (인터페이스가 핵심인 레이어).
// model/ 레이어는 struct가 주력이므로 제외.
//
// 파일명 → 인터페이스명 변환 규칙:
//   - snake_case → PascalCase
//   - install.go → Install
//   - create_order.go → CreateOrder
//
// 예: svc/install.go 안에 Install 인터페이스가 없으면 위반.
func checkFileInterfaceMatch(file *analyzer.FileInfo, dp *analyzer.DomainPath) *report.Violation {
	// svc/와 repo/ 레이어에서만 적용
	if dp.Layer != "svc" && dp.Layer != "repo" {
		return nil
	}

	// 파일에 공개 인터페이스가 하나도 없으면 검사하지 않음
	// (struct만 있는 파일, 유틸리티 함수만 있는 파일 등)
	hasExportedInterface := false
	for _, t := range file.Types {
		if t.IsInterface && t.IsExported {
			hasExportedInterface = true
			break
		}
	}
	if !hasExportedInterface {
		return nil
	}

	// 파일명에서 기대하는 인터페이스명을 도출
	baseName := strings.TrimSuffix(filepath.Base(file.Path), ".go")
	expected := snakeToPascal(baseName)

	// 파일 안에 기대하는 이름의 인터페이스가 있는지 확인
	for _, t := range file.Types {
		if t.IsInterface && t.IsExported && t.Name == expected {
			return nil // 일치하는 인터페이스 발견
		}
	}

	// 공개 인터페이스가 있지만 파일명과 일치하는 것이 없음
	return &report.Violation{
		Rule:     "naming/file-interface-match",
		Severity: report.Warning,
		Message:  fmt.Sprintf("file %q should contain interface %q", filepath.Base(file.Path), expected),
		File:     file.Path,
		Fix:      fmt.Sprintf("rename file or interface so they match (e.g., %s.go → %s interface)", baseName, expected),
	}
}

// snakeToPascal은 snake_case 문자열을 PascalCase로 변환한다.
//
// 예:
//
//	"install"      → "Install"
//	"create_order" → "CreateOrder"
//	"user"         → "User"
func snakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// ──────────────────────────────────────────────
// 규칙 5: 레이어 접미사 파일명 금지
// ──────────────────────────────────────────────

// checkLayerSuffixFilename은 파일명에 레이어 이름이 접미사로 포함되었는지 검사한다.
//
// 파일이 어떤 레이어에 속하는지는 디렉토리 경로가 이미 표현하고 있다.
// 파일명에 또 레이어를 붙이면 정보가 중복된다.
//
// 나쁜 예:
//   - subdomain/core/svc/install_svc.go  ← "_svc"가 불필요 (이미 svc/ 안에 있음)
//   - subdomain/core/model/user_model.go ← "_model"이 불필요
//   - subdomain/core/repo/user_repo.go   ← "_repo"가 불필요
//
// 좋은 예:
//   - subdomain/core/svc/install.go
//   - subdomain/core/model/user.go
//   - subdomain/core/repo/user.go
func checkLayerSuffixFilename(file *analyzer.FileInfo, _ *analyzer.DomainPath) *report.Violation {
	baseName := strings.TrimSuffix(filepath.Base(file.Path), ".go")

	// 각 레이어 이름을 접미사로 검사
	for _, layer := range AllowedSubdomainLayers {
		suffix := "_" + layer
		// CutSuffix는 접미사가 있으면 제거한 문자열과 true를 반환한다.
		// HasSuffix+TrimSuffix 조합보다 간결한 Go 1.20+ 스타일이다.
		if cleaned, found := strings.CutSuffix(baseName, suffix); found {
			return &report.Violation{
				Rule:     "naming/layer-suffix-filename",
				Severity: report.Error,
				Message:  fmt.Sprintf("filename %q contains layer suffix %q", baseName+".go", suffix),
				File:     file.Path,
				Fix:      fmt.Sprintf("rename to %q (directory already expresses the layer)", cleaned+".go"),
			}
		}
	}

	return nil
}
