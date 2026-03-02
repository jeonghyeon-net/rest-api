package ruleset

// 이 파일은 "디렉토리 구조 규칙"을 검사한다.
//
// DDD에서 모든 도메인이 동일한 디렉토리 구조를 갖는 것을 "대칭성(symmetry)"이라고 한다.
// NestJS에서 모든 모듈이 controller, service, module 파일을 갖는 것과 비슷하게,
// 여기서는 모든 도메인이 정해진 디렉토리 구조를 따라야 한다.
//
// 검사하는 5가지 규칙:
//
// 1. alias.go 존재 여부 (missing-alias)
//    - 모든 도메인은 alias.go를 가져야 한다.
//    - alias.go는 외부에서 이 도메인에 접근할 때 쓰는 유일한 진입점.
//    - NestJS 비유: module.ts에서 exports에 명시하는 것과 비슷
//
// 2. 허용된 도메인 하위 디렉토리 (invalid-domain-dir)
//    - subdomain, svc, handler, infra만 허용
//
// 3. 서브도메인 레이어 구조 (invalid-subdomain-layer)
//    - 서브도메인 안에는 model, repo, svc만 허용
//
// 4. 핸들러 프로토콜 디렉토리 (invalid-handler-protocol)
//    - handler/ 안에는 http, grpc, jsonrpc만 허용
//
// 5. Saga 디렉토리 구조 (saga-*)
//    - internal/saga/{이름}/ 안에는 .go 파일만 있어야 한다 (하위 디렉토리 금지)
//    - Saga는 단일 패키지로 유지: 복잡해지면 도메인으로 분리해야 한다는 신호

import (
	"fmt"
	"os"
	"path/filepath"

	"rest-api/test/architecture/report"
)

// CheckStructure는 internal/domain/ 과 internal/saga/ 의 디렉토리 구조를 검사한다.
//
// 해당 디렉토리가 아직 없으면 검사를 건너뛴다 (프로젝트 초기 상태).
func CheckStructure(cfg *Config) []report.Violation {
	var violations []report.Violation

	// ── 도메인 구조 검사 ──
	domainRoot := filepath.Join(cfg.ProjectRoot, "internal", "domain")
	if _, err := os.Stat(domainRoot); err == nil {
		entries, err := os.ReadDir(domainRoot)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				domainName := entry.Name()
				domainDir := filepath.Join(domainRoot, domainName)

				violations = append(violations, checkAliasFile(domainDir, domainName)...)
				violations = append(violations, checkDomainDirs(domainDir, domainName)...)
				violations = append(violations, checkSubdomains(domainDir, domainName)...)
				violations = append(violations, checkHandlerProtocols(domainDir, domainName)...)
			}
		}
	}

	// ── Saga 구조 검사 ──
	violations = append(violations, checkSagaStructure(cfg)...)

	return violations
}

// ──────────────────────────────────────────────
// 규칙 1: alias.go 존재 여부
// ──────────────────────────────────────────────

// checkAliasFile은 도메인 루트에 alias.go 파일이 있는지 검사한다.
//
// alias.go는 도메인의 "공개 API"를 정의한다. 다른 도메인에서 이 도메인을 사용할 때
// alias.go에 정의된 타입 별칭(type alias)만 사용해야 한다.
//
// alias.go 예시:
//
//	package user
//	import "rest-api/internal/domain/user/svc"
//	type Public = svc.Public  // 외부에서는 user.Public으로 접근
//
// NestJS 비유: module의 exports 배열과 비슷. 외부에 공개할 것만 명시적으로 노출.
func checkAliasFile(domainDir, domainName string) []report.Violation {
	aliasPath := filepath.Join(domainDir, "alias.go")
	// os.Stat: 파일이 존재하는지 확인한다. os.IsNotExist: 파일이 없으면 true.
	if _, err := os.Stat(aliasPath); os.IsNotExist(err) {
		return []report.Violation{{
			Rule:     "structure/missing-alias",
			Severity: report.Warning,
			Message:  fmt.Sprintf("domain %q is missing alias.go", domainName),
			File:     domainDir,
			Fix:      "create alias.go with type aliases for public interfaces",
		}}
	}
	return nil
}

// ──────────────────────────────────────────────
// 규칙 2: 허용된 도메인 하위 디렉토리
// ──────────────────────────────────────────────

// checkDomainDirs는 도메인 디렉토리 안에 허용된 디렉토리만 있는지 검사한다.
//
// 허용 목록: subdomain, svc, handler, infra
//
// 예: internal/domain/user/utils/ ← "utils"는 허용 목록에 없으므로 위반!
func checkDomainDirs(domainDir, domainName string) []report.Violation {
	var violations []report.Violation

	entries, err := os.ReadDir(domainDir)
	if err != nil {
		return nil
	}

	// 허용된 디렉토리를 map으로 만들어서 빠르게 조회할 수 있게 한다.
	// map[string]bool은 "집합(Set)" 용도로 자주 쓰인다.
	// TypeScript의 Set<string>과 비슷하지만, Go에는 Set이 없어서 map으로 대체한다.
	allowed := make(map[string]bool)
	for _, d := range AllowedDomainDirs {
		allowed[d] = true
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // 파일은 건너뜀 (alias.go 같은 파일은 허용)
		}
		// 허용 목록에 없는 디렉토리면 위반
		if !allowed[entry.Name()] {
			violations = append(violations, report.Violation{
				Rule:     "structure/invalid-domain-dir",
				Severity: report.Error,
				Message:  fmt.Sprintf("domain %q contains unexpected directory %q", domainName, entry.Name()),
				File:     filepath.Join(domainDir, entry.Name()),
				Fix:      fmt.Sprintf("allowed directories: %v", AllowedDomainDirs),
			})
		}
	}

	return violations
}

// ──────────────────────────────────────────────
// 규칙 3: 서브도메인 레이어 구조
// ──────────────────────────────────────────────

// checkSubdomains는 각 서브도메인 안에 허용된 레이어 디렉토리만 있는지 검사한다.
//
// 허용 목록: model, repo, svc
//
// 예: internal/domain/user/subdomain/core/controller/ ← "controller"는 허용 목록에 없으므로 위반!
// (Go DDD에서는 controller 대신 handler/ 디렉토리를 도메인 루트에 둔다)
func checkSubdomains(domainDir, domainName string) []report.Violation {
	var violations []report.Violation

	subdomainDir := filepath.Join(domainDir, "subdomain")
	if _, err := os.Stat(subdomainDir); os.IsNotExist(err) {
		return nil // subdomain 디렉토리가 없으면 검사할 것이 없다
	}

	entries, err := os.ReadDir(subdomainDir)
	if err != nil {
		return nil
	}

	// 허용된 레이어 목록을 Set으로 만든다
	allowed := make(map[string]bool)
	for _, l := range AllowedSubdomainLayers {
		allowed[l] = true
	}

	for _, entry := range entries {
		// subdomain/ 바로 아래에 파일이 있으면 안 된다
		// 파일은 반드시 서브도메인/레이어/ 안에 있어야 한다
		if !entry.IsDir() {
			violations = append(violations, report.Violation{
				Rule:     "structure/file-in-subdomain-root",
				Severity: report.Error,
				Message:  fmt.Sprintf("unexpected file in subdomain directory of domain %q", domainName),
				File:     filepath.Join(subdomainDir, entry.Name()),
				Fix:      "files should be in subdomain layer directories (model/, repo/, svc/)",
			})
			continue
		}

		// 각 서브도메인(예: core, role) 안의 디렉토리를 검사
		subDir := filepath.Join(subdomainDir, entry.Name())
		layerEntries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}

		for _, le := range layerEntries {
			if !le.IsDir() {
				continue // 서브도메인 루트의 파일은 허용 (예: core/README.md)
			}
			// 허용된 레이어 디렉토리가 아니면 위반
			if !allowed[le.Name()] {
				violations = append(violations, report.Violation{
					Rule:     "structure/invalid-subdomain-layer",
					Severity: report.Error,
					Message:  fmt.Sprintf("subdomain %q in domain %q contains unexpected directory %q", entry.Name(), domainName, le.Name()),
					File:     filepath.Join(subDir, le.Name()),
					Fix:      fmt.Sprintf("allowed layers: %v", AllowedSubdomainLayers),
				})
			}
		}
	}

	return violations
}

// ──────────────────────────────────────────────
// 규칙 4: 핸들러 프로토콜 디렉토리
// ──────────────────────────────────────────────

// ──────────────────────────────────────────────
// 규칙 5: Saga 디렉토리 구조
// ──────────────────────────────────────────────

// checkSagaStructure는 internal/saga/ 디렉토리의 구조를 검사한다.
//
// Saga 디렉토리 규칙:
//   - internal/saga/ 바로 아래에는 디렉토리만 있어야 한다 (각 디렉토리가 하나의 Saga)
//   - 각 Saga 디렉토리 안에는 .go 파일만 있어야 한다 (하위 디렉토리 금지)
//
// Saga를 단일 패키지로 강제하는 이유:
//
//	Saga가 복잡해져서 하위 디렉토리가 필요해지면,
//	그건 Saga가 아니라 새로운 도메인으로 분리해야 한다는 신호다.
//	Saga는 "조율자"이지 "비즈니스 로직 구현자"가 아니다.
//
// 기대 구조:
//
//	internal/saga/
//	├── create_order/        ← Saga 하나 = 디렉토리 하나
//	│   └── saga.go          ← .go 파일만 허용
//	└── cancel_order/
//	    └── saga.go
func checkSagaStructure(cfg *Config) []report.Violation {
	var violations []report.Violation

	sagaRoot := filepath.Join(cfg.ProjectRoot, "internal", "saga")
	if _, err := os.Stat(sagaRoot); os.IsNotExist(err) {
		return nil // saga 디렉토리가 없으면 검사할 것이 없다
	}

	entries, err := os.ReadDir(sagaRoot)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		// saga/ 바로 아래에 파일이 있으면 안 된다
		if !entry.IsDir() {
			violations = append(violations, report.Violation{
				Rule:     "structure/file-in-saga-root",
				Severity: report.Error,
				Message:  "unexpected file in saga root directory",
				File:     filepath.Join(sagaRoot, entry.Name()),
				Fix:      "files should be inside a saga directory (e.g., internal/saga/create_order/saga.go)",
			})
			continue
		}

		// 각 Saga 디렉토리 안에 하위 디렉토리가 있으면 안 된다
		sagaDir := filepath.Join(sagaRoot, entry.Name())
		subEntries, err := os.ReadDir(sagaDir)
		if err != nil {
			continue
		}

		for _, se := range subEntries {
			if se.IsDir() {
				violations = append(violations, report.Violation{
					Rule:     "structure/saga-nested-dir",
					Severity: report.Error,
					Message:  fmt.Sprintf("saga %q contains nested directory %q", entry.Name(), se.Name()),
					File:     filepath.Join(sagaDir, se.Name()),
					Fix:      "sagas must be a single flat package; if complex, consider extracting into a domain",
				})
			}
		}
	}

	return violations
}

// ──────────────────────────────────────────────
// 규칙 4: 핸들러 프로토콜 디렉토리
// ──────────────────────────────────────────────

// checkHandlerProtocols는 handler/ 안에 허용된 프로토콜 디렉토리만 있는지 검사한다.
//
// 허용 목록: http, grpc, jsonrpc
//
// NestJS 비유: NestJS에서는 @Controller()에서 HTTP를 처리하고, gRPC는 별도 패키지를 쓴다.
// 여기서는 프로토콜별로 디렉토리를 분리한다.
func checkHandlerProtocols(domainDir, domainName string) []report.Violation {
	var violations []report.Violation

	handlerDir := filepath.Join(domainDir, "handler")
	if _, err := os.Stat(handlerDir); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(handlerDir)
	if err != nil {
		return nil
	}

	allowed := make(map[string]bool)
	for _, p := range AllowedHandlerProtocols {
		allowed[p] = true
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !allowed[entry.Name()] {
			violations = append(violations, report.Violation{
				Rule:     "structure/invalid-handler-protocol",
				Severity: report.Error,
				Message:  fmt.Sprintf("handler in domain %q contains unexpected protocol %q", domainName, entry.Name()),
				File:     filepath.Join(handlerDir, entry.Name()),
				Fix:      fmt.Sprintf("allowed protocols: %v", AllowedHandlerProtocols),
			})
		}
	}

	return violations
}
