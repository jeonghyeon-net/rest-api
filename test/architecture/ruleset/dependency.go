package ruleset

// 이 파일은 "의존성 규칙"을 검사한다.
//
// DDD(도메인 주도 설계)에서 가장 중요한 규칙은 "의존성 방향"이다.
// 잘못된 방향으로 의존하면 코드가 서로 얽혀서(커플링) 변경이 어려워진다.
//
// 검사하는 8가지 규칙:
//
// 1. 서브도메인 간 직접 의존 금지 (cross-subdomain)
//    - 같은 도메인 안의 서브도메인끼리도 직접 import하면 안 된다.
//    - 예외: 모든 서브도메인은 "core" 서브도메인에 의존할 수 있다.
//    - 예: role → core ✓, role → billing ✗
//
// 2. 서브도메인에서 도메인 레이어 import 금지 (subdomain-imports-domain-layer)
//    - 서브도메인은 같은 도메인의 상위 레이어(svc/, handler/, infra/)를 직접 import하면 안 된다.
//    - alias.go(도메인 루트)만 허용.
//
// 3. 도메인 간 직접 의존 금지 (cross-domain)
//    - 다른 도메인의 내부 패키지를 직접 import하면 안 된다.
//    - alias.go(도메인 루트)만 사용 가능.
//
// 4. 서브도메인에서 다른 도메인 직접 import 금지 (cross-domain-from-subdomain)
//    - 서브도메인은 다른 도메인을 직접 import하면 안 된다.
//
// 5. 레이어 역방향 의존 금지 (layer-direction)
//    - model ← repo ← svc 방향으로만 의존 가능.
//
// 6. Saga가 도메인 내부 import 금지 (saga-internal-import)
//    - Saga는 도메인의 alias.go만 import 가능.
//    - Saga가 도메인 내부(subdomain, svc 등)를 직접 import하면 안 된다.
//
// 7. Saga 간 직접 의존 금지 (saga-cross-saga)
//    - Saga끼리 서로 import하면 안 된다 (각 Saga는 독립적).
//
// 8. 서브도메인에서 Saga import 금지 (subdomain-imports-saga)
//    - 서브도메인은 Saga를 import할 수 없다.
//    - Saga 호출은 Public Service 레이어에서만 가능.

import (
	"fmt"
	"path/filepath"

	"rest-api/test/architecture/analyzer"
	"rest-api/test/architecture/report"
)

// layerRoot는 도메인 루트(alias.go가 있는 곳)를 나타내는 레이어 이름이다.
const layerRoot = "root"

// CheckDependencies는 모든 파일의 import문을 검사해서 의존성 규칙 위반을 찾는다.
//
// 동작 방식:
//  1. 각 파일의 경로를 분석해서 "이 파일이 어디에 있는지" 파악 (src)
//  2. 각 import 경로를 분석해서 "무엇을 import하는지" 파악 (target)
//  3. src와 target의 관계가 규칙에 맞는지 검사
func CheckDependencies(files []*analyzer.FileInfo, cfg *Config) []report.Violation {
	var violations []report.Violation

	for _, file := range files {
		// 파일의 절대 경로를 프로젝트 루트 기준 상대 경로로 변환
		relPath, err := filepath.Rel(cfg.ProjectRoot, file.Path)
		if err != nil {
			continue
		}

		// 파일 경로에서 도메인/Saga 구조 정보를 추출한다
		src := analyzer.ParseDomainPath(relPath)
		if src == nil {
			continue // 도메인/Saga 디렉토리가 아닌 파일은 건너뜀
		}

		// 이 파일의 모든 import문을 검사
		for _, imp := range file.Imports {
			// import 경로에서 도메인/Saga 구조 정보를 추출
			target := analyzer.ImportToDomainPath(imp.Path, cfg.ModuleName)
			if target == nil {
				continue // 외부 패키지는 건너뜀
			}

			// ── Saga 관련 규칙 ──
			if v := checkSagaDependency(src, target, file.Path, imp); v != nil {
				violations = append(violations, *v)
				continue // Saga 규칙에 걸렸으면 도메인 규칙은 검사하지 않음
			}
			if v := checkSubdomainImportsSaga(src, target, file.Path, imp); v != nil {
				violations = append(violations, *v)
				continue
			}

			// ── 도메인 관련 규칙 (기존) ──
			if v := checkCrossSubdomain(src, target, file.Path, imp); v != nil {
				violations = append(violations, *v)
			}
			if v := checkSubdomainImportsDomainLayer(src, target, file.Path, imp); v != nil {
				violations = append(violations, *v)
			}
			if v := checkCrossDomain(src, target, file.Path, imp); v != nil {
				violations = append(violations, *v)
			}
			if v := checkLayerDirection(src, target, file.Path, imp); v != nil {
				violations = append(violations, *v)
			}
		}
	}

	return violations
}

// ──────────────────────────────────────────────
// 규칙 4: Saga 의존성 규칙
// ──────────────────────────────────────────────

// checkSagaDependency는 Saga 파일이 올바른 대상만 import하는지 검사한다.
//
// Saga는 여러 도메인의 Public Service를 "조율(orchestrate)"하는 역할이다.
// 따라서 도메인의 공개 인터페이스만 사용해야 하며, 내부 구현에 직접 접근하면 안 된다.
//
// NestJS 비유:
//
//	Saga는 여러 모듈의 서비스를 주입받아서 조합하는 "Application Service"와 비슷하다.
//	NestJS에서 다른 모듈의 서비스를 쓰려면 exports에 등록된 것만 가능하듯이,
//	여기서도 alias.go나 svc/에 등록된 것만 가능하다.
//
// 허용:
//   - Saga → 도메인 root (alias.go) ✓  ← 유일한 진입점
//
// 금지:
//   - Saga → 도메인 svc/ 직접 접근 ✗ (alias.go를 통해야 함)
//   - Saga → 도메인 subdomain 내부 ✗
//   - Saga → 도메인 handler, infra 등 ✗
//   - Saga → 다른 Saga ✗ (Saga끼리 의존하면 순환 위험)
func checkSagaDependency(src, target *analyzer.DomainPath, file string, imp analyzer.ImportInfo) *report.Violation {
	// 이 검사는 src가 Saga 파일일 때만 적용
	if !src.IsSaga {
		return nil
	}

	// Saga가 다른 Saga를 import하는 경우 → 금지
	// 각 Saga는 독립적이어야 한다. Saga 간에 공유할 로직이 있으면
	// 도메인 서비스로 빼야 한다.
	if target.IsSaga {
		return &report.Violation{
			Rule:     "dependency/saga-cross-saga",
			Severity: report.Error,
			Message:  fmt.Sprintf("saga %q imports saga %q directly", src.SagaName, target.SagaName),
			File:     file,
			Line:     imp.Line,
			Fix:      "sagas should be independent; extract shared logic into a domain service",
		}
	}

	// Saga가 도메인을 import하는 경우:
	// 오직 도메인 root(alias.go)만 허용. svc/ 직접 접근도 금지.
	// alias.go가 유일한 진입점이다.
	if target.Domain != "" {
		if target.Layer == layerRoot {
			return nil // 허용: alias.go를 통한 접근
		}

		// svc/ 포함, 그 외 모든 도메인 내부 패키지 import 금지
		return &report.Violation{
			Rule:     "dependency/saga-internal-import",
			Severity: report.Error,
			Message:  fmt.Sprintf("saga %q imports internal package of domain %q (layer: %s)", src.SagaName, target.Domain, target.Layer),
			File:     file,
			Line:     imp.Line,
			Fix:      fmt.Sprintf("import %q (alias.go) instead of accessing internal packages directly", target.Domain),
		}
	}

	return nil
}

// ──────────────────────────────────────────────
// 규칙 5: 서브도메인에서 Saga import 금지
// ──────────────────────────────────────────────

// checkSubdomainImportsSaga는 서브도메인이 Saga를 import하는 것을 감지한다.
//
// 서브도메인은 "순수한 비즈니스 로직"만 담아야 한다.
// Saga를 호출하는 것은 Public Service(svc/) 레이어의 역할이다.
//
// 의존성 매트릭스에서:
//   - subdomain → saga ✗ (서브도메인은 Saga를 모름)
//   - svc (Public Service) → saga ✓ (Public Service가 Saga를 호출)
//   - handler → saga ✓ (핸들러에서 Saga를 트리거)
func checkSubdomainImportsSaga(src, target *analyzer.DomainPath, file string, imp analyzer.ImportInfo) *report.Violation {
	// src가 서브도메인이고 target이 Saga인 경우만 검사
	if src.Subdomain == "" || !target.IsSaga {
		return nil
	}

	return &report.Violation{
		Rule:     "dependency/subdomain-imports-saga",
		Severity: report.Error,
		Message:  fmt.Sprintf("subdomain %q in domain %q imports saga %q", src.Subdomain, src.Domain, target.SagaName),
		File:     file,
		Line:     imp.Line,
		Fix:      "subdomains cannot depend on sagas; move saga invocation to Public Service layer",
	}
}

// ──────────────────────────────────────────────
// 규칙 1: 서브도메인 간 직접 의존 금지
// ──────────────────────────────────────────────

// checkCrossSubdomain은 같은 도메인 내에서 서브도메인 간 직접 import를 감지한다.
//
// 허용되는 경우:
//   - 같은 서브도메인 내의 import (예: core/svc → core/model) ✓
//   - core 서브도메인을 import (예: role/svc → core/model) ✓
//
// 금지되는 경우:
//   - core가 아닌 다른 서브도메인을 import (예: role → billing) ✗
func checkCrossSubdomain(src, target *analyzer.DomainPath, file string, imp analyzer.ImportInfo) *report.Violation {
	// Saga 파일은 이 규칙의 대상이 아님 (Saga 규칙에서 별도 처리)
	if src.IsSaga || target.IsSaga {
		return nil
	}
	// 다른 도메인이면 이 규칙의 대상이 아님 (cross-domain 규칙에서 처리)
	if src.Domain != target.Domain {
		return nil
	}
	// src나 target이 서브도메인이 아니면 이 규칙의 대상이 아님
	if src.Subdomain == "" || target.Subdomain == "" {
		return nil
	}
	// 같은 서브도메인 내의 import는 당연히 허용
	if src.Subdomain == target.Subdomain {
		return nil
	}
	// core 서브도메인은 모든 서브도메인이 의존할 수 있다
	if target.Subdomain == "core" {
		return nil
	}

	return &report.Violation{
		Rule:     "dependency/cross-subdomain",
		Severity: report.Error,
		Message:  fmt.Sprintf("subdomain %q imports subdomain %q directly", src.Subdomain, target.Subdomain),
		File:     file,
		Line:     imp.Line,
		Fix:      "use Public Service or move shared logic to core/",
	}
}

// ──────────────────────────────────────────────
// 규칙 2: 서브도메인에서 도메인 레이어 import 금지
// ──────────────────────────────────────────────

// checkSubdomainImportsDomainLayer는 서브도메인이 같은 도메인의 상위 레이어를
// import하는 것을 감지한다.
//
// 서브도메인은 도메인의 "구현 세부사항"이다.
// 도메인 수준의 svc/, handler/, infra/는 서브도메인 위에서 조율하는 레이어이므로,
// 서브도메인이 이들을 import하면 의존성 방향이 뒤집힌다.
//
// NestJS 비유:
//
//	NestJS에서 Repository(데이터 계층)가 Controller(요청 처리 계층)를 import하면
//	이상하듯이, 서브도메인(내부 구현)이 도메인 수준 서비스(외부 조율)를 import하면 안 된다.
//
// 허용:
//   - subdomain → domain root (alias.go): ✓ (Public 타입 접근)
//
// 금지:
//   - subdomain → domain svc/ (App Service): ✗
//   - subdomain → domain handler/: ✗
//   - subdomain → domain infra/: ✗
func checkSubdomainImportsDomainLayer(src, target *analyzer.DomainPath, file string, imp analyzer.ImportInfo) *report.Violation {
	// Saga 파일은 이 규칙의 대상이 아님
	if src.IsSaga || target.IsSaga {
		return nil
	}
	// src가 서브도메인 파일이 아니면 이 규칙의 대상이 아님
	if src.Subdomain == "" {
		return nil
	}
	// 같은 도메인 내에서만 적용
	if src.Domain != target.Domain {
		return nil
	}
	// target이 서브도메인이면 다른 규칙(checkCrossSubdomain)에서 처리
	if target.Subdomain != "" {
		return nil
	}
	// domain root (alias.go)는 허용 — 블로그 매트릭스에서 "Public: ✓"
	if target.Layer == layerRoot {
		return nil
	}

	// svc/, handler/, infra/ 등 도메인 수준 레이어 import는 금지
	return &report.Violation{
		Rule:     "dependency/subdomain-imports-domain-layer",
		Severity: report.Error,
		Message:  fmt.Sprintf("subdomain %q in domain %q imports domain-level %s/", src.Subdomain, src.Domain, target.Layer),
		File:     file,
		Line:     imp.Line,
		Fix:      "subdomains cannot depend on domain-level layers (svc/, handler/, infra/)",
	}
}

// ──────────────────────────────────────────────
// 규칙 3: 도메인 간 직접 의존 금지
// ──────────────────────────────────────────────

// checkCrossDomain은 다른 도메인의 내부 패키지를 직접 import하는 것을 감지한다.
//
// 핵심 원칙: 다른 도메인에 접근할 때는 반드시 alias.go(도메인 루트)를 통해야 한다.
// alias.go가 유일한 진입점이다. svc/를 직접 import하는 것도 금지.
//
// 허용되는 경우:
//   - 같은 도메인 내의 import ✓
//   - 다른 도메인의 root(alias.go)를 import ✓  ← 유일한 진입점
//
// 금지되는 경우:
//   - 다른 도메인의 svc/ 직접 import ✗ (alias.go를 통해야 함)
//   - 다른 도메인의 subdomain/ 내부를 직접 import ✗
//   - 다른 도메인의 handler/, infra/ 등을 import ✗
func checkCrossDomain(src, target *analyzer.DomainPath, file string, imp analyzer.ImportInfo) *report.Violation {
	// Saga 파일은 이 규칙의 대상이 아님
	if src.IsSaga || target.IsSaga {
		return nil
	}
	// 같은 도메인이면 이 규칙의 대상이 아님
	if src.Domain == target.Domain {
		return nil
	}
	// 도메인 루트(alias.go가 있는 곳)만 허용
	if target.Layer == layerRoot {
		return nil
	}

	// 서브도메인 안에서 다른 도메인에 직접 접근하는 경우 (더 심각)
	if src.Subdomain != "" {
		return &report.Violation{
			Rule:     "dependency/cross-domain-from-subdomain",
			Severity: report.Error,
			Message:  fmt.Sprintf("subdomain %q in domain %q imports domain %q directly", src.Subdomain, src.Domain, target.Domain),
			File:     file,
			Line:     imp.Line,
			Fix:      "subdomains cannot depend on other domains; move dependency to Public Service layer",
		}
	}

	// 서브도메인 바깥(handler, svc, infra 등)에서 다른 도메인 내부에 접근하는 경우
	return &report.Violation{
		Rule:     "dependency/cross-domain",
		Severity: report.Error,
		Message:  fmt.Sprintf("domain %q imports internal package of domain %q (layer: %s)", src.Domain, target.Domain, target.Layer),
		File:     file,
		Line:     imp.Line,
		Fix:      fmt.Sprintf("import %q (alias.go) instead of accessing internal packages directly", target.Domain),
	}
}

// ──────────────────────────────────────────────
// 규칙 3: 레이어 역방향 의존 금지
// ──────────────────────────────────────────────

// checkLayerDirection은 같은 서브도메인 내에서 레이어 의존 방향을 검사한다.
//
// 레이어 의존 방향: model(0) ← repo(1) ← svc(2)
//
// "안쪽 레이어가 바깥쪽 레이어를 import하면 위반"
//   - svc(2) → model(0): 2 > 0 이므로 바깥→안쪽 = OK ✓
//   - repo(1) → model(0): 1 > 0 이므로 바깥→안쪽 = OK ✓
//   - model(0) → svc(2): 0 < 2 이므로 안쪽→바깥쪽 = 위반! ✗
func checkLayerDirection(src, target *analyzer.DomainPath, file string, imp analyzer.ImportInfo) *report.Violation {
	// Saga 파일은 이 규칙의 대상이 아님
	if src.IsSaga || target.IsSaga {
		return nil
	}
	// 같은 도메인의 같은 서브도메인 내에서만 적용
	if src.Domain != target.Domain || src.Subdomain != target.Subdomain {
		return nil
	}
	if src.Subdomain == "" {
		return nil
	}

	srcIdx := layerIndex(src.Layer)
	targetIdx := layerIndex(target.Layer)
	if srcIdx < 0 || targetIdx < 0 {
		return nil
	}

	// src의 인덱스가 target보다 작으면 "안쪽이 바깥쪽을 import" = 역방향 의존
	if srcIdx < targetIdx {
		return &report.Violation{
			Rule:     "dependency/layer-direction",
			Severity: report.Error,
			Message:  fmt.Sprintf("layer %q cannot depend on layer %q (reverse dependency)", src.Layer, target.Layer),
			File:     file,
			Line:     imp.Line,
			Fix:      "dependency direction must be: model <- repo <- svc",
		}
	}

	return nil
}

// layerIndex는 레이어 이름을 LayerOrder 배열에서의 인덱스로 변환한다.
func layerIndex(layer string) int {
	for i, l := range LayerOrder {
		if l == layer {
			return i
		}
	}
	return -1
}
